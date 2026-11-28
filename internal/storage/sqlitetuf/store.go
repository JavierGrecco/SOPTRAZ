// Package sqlitetuf stores the durable local trust state for authenticated TUF
// repositories. Cryptographic verification remains the responsibility of the
// catalog package; this package only persists metadata that the caller has
// already verified.
package sqlitetuf

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	schemaVersion          = 2
	busyTimeoutMS          = 5000
	MaxRootMetadataBytes   = 512_000
	MaxRootsPerCommit      = 256
	maxRepositoryIDLength  = 128
	maxRepositoryURLLength = 2048
)

var (
	ErrInvalidTrustStore  = errors.New("almacén de confianza TUF inválido")
	ErrInvalidRepository  = errors.New("repositorio TUF inválido")
	ErrRepositoryNotFound = errors.New("repositorio TUF no registrado")
	ErrRepositoryConflict = errors.New("la configuración del repositorio TUF no coincide")
	ErrTrustStateConflict = errors.New("el estado de confianza TUF cambió concurrentemente")
	ErrInvalidRootChain   = errors.New("cadena de raíces TUF inválida")
	ErrCorruptTrustStore  = errors.New("el almacén de confianza TUF está dañado")
	ErrSchemaTooNew       = errors.New("el esquema del almacén de confianza TUF es más reciente")
)

// RepositoryConfig binds a durable trust record to one repository origin and
// its independently provisioned bootstrap root. The bootstrap root itself
// remains supplied by the verified SOPTRAZ installation, not by this store.
type RepositoryConfig struct {
	ID            string
	MetadataURL   string
	AnchorVersion int64
	AnchorSHA256  string
}

// RootState is the latest root version and exact-byte digest committed locally.
type RootState struct {
	Version int64
	SHA256  string
}

// VerifiedRoot is a root metadata document already authenticated by the
// catalog layer using go-tuf trustedmetadata.UpdateRoot. This store deliberately
// does not reimplement or replace that cryptographic verification.
type VerifiedRoot struct {
	Version        int64
	PreviousSHA256 string
	Metadata       []byte
}

// RootRecord is an immutable root-chain entry returned by LoadRootChain.
type RootRecord struct {
	Version        int64
	PreviousSHA256 string
	SHA256         string
	Metadata       []byte
	CommittedAt    string
}

// Store persists root rotations independently from audit events and target
// data. The SQLite file is a local durability boundary, not a hardware-backed
// anti-rollback anchor.
type Store struct {
	db *sql.DB
}

// Open creates or opens a dedicated catalog-trust SQLite database. It uses a
// single connection and rollback journaling so the trust-store transaction
// semantics stay simple and portable across Windows, macOS and Linux.
func Open(ctx context.Context, path string) (*Store, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: el contexto no puede ser nil", ErrInvalidTrustStore)
	}
	if path == "" || path == ":memory:" {
		return nil, fmt.Errorf("%w: se requiere una ruta de archivo local", ErrInvalidTrustStore)
	}

	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("%w: resolver la ruta: %v", ErrInvalidTrustStore, err)
	}
	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o700); err != nil {
		return nil, fmt.Errorf("%w: crear el directorio: %v", ErrInvalidTrustStore, err)
	}

	db, err := sql.Open("sqlite", sqliteDSN(absolutePath))
	if err != nil {
		return nil, fmt.Errorf("%w: abrir SQLite: %v", ErrInvalidTrustStore, err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	store := &Store{db: db}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("%w: conectar con SQLite: %v", ErrInvalidTrustStore, err)
	}
	if err := store.configure(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func sqliteDSN(path string) string {
	if runtime.GOOS == "windows" {
		path = strings.ReplaceAll(path, "\\", "/")
		if hasDriveLetter(path) {
			path = "/" + path
		}
	}
	fileURL := url.URL{Scheme: "file", Path: path}
	query := fileURL.Query()
	query.Set("_txlock", "immediate")
	fileURL.RawQuery = query.Encode()
	return fileURL.String()
}

func hasDriveLetter(path string) bool {
	if len(path) < 3 || path[1] != ':' {
		return false
	}
	drive := path[0]
	if !((drive >= 'a' && drive <= 'z') || (drive >= 'A' && drive <= 'Z')) {
		return false
	}
	return path[2] == '/' || path[2] == '\\'
}

func (store *Store) configure(ctx context.Context) error {
	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		fmt.Sprintf("PRAGMA busy_timeout = %d", busyTimeoutMS),
		"PRAGMA synchronous = FULL",
		"PRAGMA trusted_schema = OFF",
		"PRAGMA journal_mode = DELETE",
	}
	for _, statement := range pragmas {
		if _, err := store.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("%w: configurar SQLite (%s): %v", ErrInvalidTrustStore, statement, err)
		}
	}
	return nil
}

func (store *Store) migrate(ctx context.Context) error {
	var current int
	if err := store.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("%w: leer versión del esquema: %v", ErrInvalidTrustStore, err)
	}
	if current > schemaVersion {
		return fmt.Errorf("%w: versión %d, compatible hasta %d", ErrSchemaTooNew, current, schemaVersion)
	}
	if current < 1 {
		if err := store.migrateV1(ctx); err != nil {
			return err
		}
		current = 1
	}
	if current < 2 {
		if err := store.migrateV2(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (store *Store) migrateV1(ctx context.Context) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: iniciar migración v1: %v", ErrInvalidTrustStore, err)
	}
	defer tx.Rollback()

	statements := []string{
		"CREATE TABLE repositories (" +
			"id TEXT PRIMARY KEY CHECK(length(id) BETWEEN 1 AND 128), " +
			"metadata_url TEXT NOT NULL CHECK(length(metadata_url) BETWEEN 1 AND 2048), " +
			"anchor_version INTEGER NOT NULL CHECK(anchor_version >= 1), " +
			"anchor_sha256 TEXT NOT NULL CHECK(length(anchor_sha256) = 64), " +
			"latest_root_version INTEGER NOT NULL CHECK(latest_root_version >= anchor_version), " +
			"latest_root_sha256 TEXT NOT NULL CHECK(length(latest_root_sha256) = 64), " +
			"created_at TEXT NOT NULL)",
		"CREATE TABLE root_chain (" +
			"repository_id TEXT NOT NULL, " +
			"version INTEGER NOT NULL CHECK(version >= 2), " +
			"previous_sha256 TEXT NOT NULL CHECK(length(previous_sha256) = 64), " +
			"sha256 TEXT NOT NULL CHECK(length(sha256) = 64), " +
			"root_json BLOB NOT NULL CHECK(length(root_json) BETWEEN 1 AND 512000), " +
			"committed_at TEXT NOT NULL, " +
			"PRIMARY KEY(repository_id, version), " +
			"UNIQUE(repository_id, sha256), " +
			"FOREIGN KEY(repository_id) REFERENCES repositories(id))",
		"CREATE TRIGGER root_chain_no_update BEFORE UPDATE ON root_chain " +
			"BEGIN SELECT RAISE(ABORT, 'SOPTRAZ TUF roots are immutable'); END",
		"CREATE TRIGGER root_chain_no_delete BEFORE DELETE ON root_chain " +
			"BEGIN SELECT RAISE(ABORT, 'SOPTRAZ TUF roots are append-only'); END",
		"CREATE INDEX idx_root_chain_digest ON root_chain(repository_id, sha256)",
		"PRAGMA user_version = 1",
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("%w: aplicar migración v1: %v", ErrInvalidTrustStore, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%w: confirmar migración v1: %v", ErrInvalidTrustStore, err)
	}
	return nil
}

func (store *Store) migrateV2(ctx context.Context) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: iniciar migración v2: %v", ErrInvalidTrustStore, err)
	}
	defer tx.Rollback()

	statements := []string{
		"CREATE TABLE generations (" +
			"repository_id TEXT NOT NULL, " +
			"generation_id INTEGER NOT NULL CHECK(generation_id >= 1), " +
			"parent_generation_id INTEGER, " +
			"root_version INTEGER NOT NULL CHECK(root_version >= 1), " +
			"root_sha256 TEXT NOT NULL CHECK(length(root_sha256) = 64), " +
			"manifest_sha256 TEXT NOT NULL CHECK(length(manifest_sha256) = 64), " +
			"role_count INTEGER NOT NULL CHECK(role_count BETWEEN 3 AND 128), " +
			"created_at TEXT NOT NULL, " +
			"PRIMARY KEY(repository_id, generation_id), " +
			"FOREIGN KEY(repository_id) REFERENCES repositories(id), " +
			"FOREIGN KEY(repository_id, parent_generation_id) " +
			"REFERENCES generations(repository_id, generation_id))",
		"CREATE TABLE generation_metadata (" +
			"repository_id TEXT NOT NULL, " +
			"generation_id INTEGER NOT NULL, " +
			"role_name TEXT NOT NULL CHECK(length(role_name) BETWEEN 1 AND 512), " +
			"version INTEGER NOT NULL CHECK(version >= 1), " +
			"expires_at TEXT NOT NULL, " +
			"sha256 TEXT NOT NULL CHECK(length(sha256) = 64), " +
			"metadata_json BLOB NOT NULL CHECK(length(metadata_json) BETWEEN 1 AND 5242880), " +
			"PRIMARY KEY(repository_id, generation_id, role_name), " +
			"FOREIGN KEY(repository_id, generation_id) " +
			"REFERENCES generations(repository_id, generation_id))",
		"CREATE TABLE generation_role_parents (" +
			"repository_id TEXT NOT NULL, " +
			"generation_id INTEGER NOT NULL, " +
			"child_role TEXT NOT NULL, " +
			"parent_role TEXT NOT NULL, " +
			"CHECK(child_role <> parent_role), " +
			"PRIMARY KEY(repository_id, generation_id, child_role, parent_role), " +
			"FOREIGN KEY(repository_id, generation_id, child_role) " +
			"REFERENCES generation_metadata(repository_id, generation_id, role_name), " +
			"FOREIGN KEY(repository_id, generation_id, parent_role) " +
			"REFERENCES generation_metadata(repository_id, generation_id, role_name))",
		"CREATE TABLE role_high_water (" +
			"repository_id TEXT NOT NULL, " +
			"role_name TEXT NOT NULL CHECK(length(role_name) BETWEEN 1 AND 512), " +
			"version INTEGER NOT NULL CHECK(version >= 1), " +
			"sha256 TEXT NOT NULL CHECK(length(sha256) = 64), " +
			"expires_at TEXT NOT NULL, " +
			"PRIMARY KEY(repository_id, role_name), " +
			"FOREIGN KEY(repository_id) REFERENCES repositories(id))",
		"CREATE TABLE active_generations (" +
			"repository_id TEXT PRIMARY KEY, " +
			"generation_id INTEGER NOT NULL CHECK(generation_id >= 1), " +
			"activated_at TEXT NOT NULL, " +
			"FOREIGN KEY(repository_id, generation_id) " +
			"REFERENCES generations(repository_id, generation_id))",
		"CREATE TRIGGER generations_no_update BEFORE UPDATE ON generations " +
			"BEGIN SELECT RAISE(ABORT, 'SOPTRAZ TUF generations are immutable'); END",
		"CREATE TRIGGER generations_no_delete BEFORE DELETE ON generations " +
			"BEGIN SELECT RAISE(ABORT, 'SOPTRAZ TUF generations are append-only'); END",
		"CREATE TRIGGER generation_metadata_no_update BEFORE UPDATE ON generation_metadata " +
			"BEGIN SELECT RAISE(ABORT, 'SOPTRAZ TUF generation metadata are immutable'); END",
		"CREATE TRIGGER generation_metadata_no_delete BEFORE DELETE ON generation_metadata " +
			"BEGIN SELECT RAISE(ABORT, 'SOPTRAZ TUF generation metadata are append-only'); END",
		"CREATE TRIGGER generation_parents_no_update BEFORE UPDATE ON generation_role_parents " +
			"BEGIN SELECT RAISE(ABORT, 'SOPTRAZ TUF generation delegations are immutable'); END",
		"CREATE TRIGGER generation_parents_no_delete BEFORE DELETE ON generation_role_parents " +
			"BEGIN SELECT RAISE(ABORT, 'SOPTRAZ TUF generation delegations are append-only'); END",
		"PRAGMA user_version = 2",
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("%w: aplicar migración v2: %v", ErrInvalidTrustStore, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%w: confirmar migración v2: %v", ErrInvalidTrustStore, err)
	}
	return nil
}

// RegisterRepository pins the expected bootstrap identity. Reopening a store
// with a different origin or anchor is a conflict, never an implicit re-enrolment.
func (store *Store) RegisterRepository(ctx context.Context, config RepositoryConfig) error {
	if store == nil || store.db == nil {
		return ErrInvalidTrustStore
	}
	if ctx == nil {
		return fmt.Errorf("%w: el contexto no puede ser nil", ErrInvalidRepository)
	}
	if err := validateRepositoryConfig(config); err != nil {
		return err
	}

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: iniciar registro: %v", ErrInvalidTrustStore, err)
	}
	defer tx.Rollback()

	var metadataURL, anchorSHA256 string
	var anchorVersion int64
	err = tx.QueryRowContext(ctx,
		"SELECT metadata_url, anchor_version, anchor_sha256 FROM repositories WHERE id = ?",
		config.ID,
	).Scan(&metadataURL, &anchorVersion, &anchorSHA256)
	if err == nil {
		if metadataURL != config.MetadataURL ||
			anchorVersion != config.AnchorVersion ||
			anchorSHA256 != config.AnchorSHA256 {
			return ErrRepositoryConflict
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: leer repositorio: %v", ErrInvalidTrustStore, err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx,
		"INSERT INTO repositories("+
			"id, metadata_url, anchor_version, anchor_sha256, latest_root_version, latest_root_sha256, created_at"+
			") VALUES(?, ?, ?, ?, ?, ?, ?)",
		config.ID,
		config.MetadataURL,
		config.AnchorVersion,
		config.AnchorSHA256,
		config.AnchorVersion,
		config.AnchorSHA256,
		now,
	)
	if err != nil {
		return fmt.Errorf("%w: registrar repositorio: %v", ErrInvalidTrustStore, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%w: confirmar registro: %v", ErrInvalidTrustStore, err)
	}
	return nil
}

func validateRepositoryConfig(config RepositoryConfig) error {
	if config.ID == "" ||
		config.ID != strings.TrimSpace(config.ID) ||
		len(config.ID) > maxRepositoryIDLength {
		return fmt.Errorf("%w: ID vacío, demasiado largo o con espacios externos", ErrInvalidRepository)
	}
	if len(config.MetadataURL) == 0 || len(config.MetadataURL) > maxRepositoryURLLength {
		return fmt.Errorf("%w: URL de metadata vacía o demasiado larga", ErrInvalidRepository)
	}
	parsed, err := url.Parse(config.MetadataURL)
	if err != nil {
		return fmt.Errorf("%w: URL de metadata inválida: %v", ErrInvalidRepository, err)
	}
	if (parsed.Scheme != "https" && parsed.Scheme != "http") ||
		parsed.Host == "" ||
		parsed.User != nil ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" {
		return fmt.Errorf("%w: la URL debe ser HTTP(S), absoluta y sin credenciales, query ni fragmento", ErrInvalidRepository)
	}
	if config.AnchorVersion < 1 {
		return fmt.Errorf("%w: versión del root inicial inválida", ErrInvalidRepository)
	}
	if err := validateSHA256(config.AnchorSHA256); err != nil {
		return fmt.Errorf("%w: pin inicial: %v", ErrInvalidRepository, err)
	}
	return nil
}

func validateSHA256(value string) error {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return fmt.Errorf("se requiere SHA-256 hexadecimal en minúsculas")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return fmt.Errorf("SHA-256 hexadecimal inválido: %v", err)
	}
	return nil
}

// RootState returns the most recently committed root state for a registered
// repository. Call LoadRootChain before replaying that state into go-tuf.
func (store *Store) RootState(ctx context.Context, repositoryID string) (RootState, error) {
	if store == nil || store.db == nil {
		return RootState{}, ErrInvalidTrustStore
	}
	if ctx == nil {
		return RootState{}, fmt.Errorf("%w: el contexto no puede ser nil", ErrInvalidTrustStore)
	}
	var state RootState
	err := store.db.QueryRowContext(ctx,
		"SELECT latest_root_version, latest_root_sha256 FROM repositories WHERE id = ?",
		repositoryID,
	).Scan(&state.Version, &state.SHA256)
	if errors.Is(err, sql.ErrNoRows) {
		return RootState{}, ErrRepositoryNotFound
	}
	if err != nil {
		return RootState{}, fmt.Errorf("%w: leer estado de root: %v", ErrInvalidTrustStore, err)
	}
	return state, nil
}

// CommitVerifiedRoots atomically appends a consecutive set of roots previously
// verified by the catalog layer. expected is a compare-and-swap guard against
// stale writers. Every candidate must name the exact previous root digest.
// If any candidate or write fails, no root in this batch is committed.
func (store *Store) CommitVerifiedRoots(
	ctx context.Context,
	repositoryID string,
	expected RootState,
	roots []VerifiedRoot,
) (RootState, error) {
	if store == nil || store.db == nil {
		return RootState{}, ErrInvalidTrustStore
	}
	if ctx == nil {
		return RootState{}, fmt.Errorf("%w: el contexto no puede ser nil", ErrInvalidRootChain)
	}
	if repositoryID == "" {
		return RootState{}, fmt.Errorf("%w: ID de repositorio vacío", ErrInvalidRootChain)
	}
	if expected.Version < 1 || validateSHA256(expected.SHA256) != nil {
		return RootState{}, fmt.Errorf("%w: estado esperado inválido", ErrInvalidRootChain)
	}
	if len(roots) > MaxRootsPerCommit {
		return RootState{}, fmt.Errorf("%w: demasiadas rotaciones en un commit", ErrInvalidRootChain)
	}
	if len(roots) == 0 {
		current, err := store.RootState(ctx, repositoryID)
		if err != nil {
			return RootState{}, err
		}
		if current != expected {
			return RootState{}, ErrTrustStateConflict
		}
		return current, nil
	}

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return RootState{}, fmt.Errorf("%w: iniciar commit de roots: %v", ErrInvalidTrustStore, err)
	}
	defer tx.Rollback()

	var current RootState
	err = tx.QueryRowContext(ctx,
		"SELECT latest_root_version, latest_root_sha256 FROM repositories WHERE id = ?",
		repositoryID,
	).Scan(&current.Version, &current.SHA256)
	if errors.Is(err, sql.ErrNoRows) {
		return RootState{}, ErrRepositoryNotFound
	}
	if err != nil {
		return RootState{}, fmt.Errorf("%w: leer estado previo: %v", ErrInvalidTrustStore, err)
	}
	if current != expected {
		return RootState{}, ErrTrustStateConflict
	}

	nextState := current
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for index, root := range roots {
		if root.Version != nextState.Version+1 {
			return RootState{}, fmt.Errorf("%w: rotación %d, versión recibida %d y esperada %d",
				ErrInvalidRootChain, index+1, root.Version, nextState.Version+1)
		}
		if root.PreviousSHA256 != nextState.SHA256 {
			return RootState{}, fmt.Errorf("%w: la rotación %d no enlaza con la raíz previa",
				ErrInvalidRootChain, index+1)
		}
		if len(root.Metadata) == 0 || int64(len(root.Metadata)) > MaxRootMetadataBytes {
			return RootState{}, fmt.Errorf("%w: tamaño de root.json fuera del límite", ErrInvalidRootChain)
		}

		digest := sha256.Sum256(root.Metadata)
		digestHex := hex.EncodeToString(digest[:])
		if digestHex == nextState.SHA256 {
			return RootState{}, fmt.Errorf("%w: una rotación no puede repetir los bytes de la raíz anterior", ErrInvalidRootChain)
		}
		_, err := tx.ExecContext(ctx,
			"INSERT INTO root_chain(repository_id, version, previous_sha256, sha256, root_json, committed_at) "+
				"VALUES(?, ?, ?, ?, ?, ?)",
			repositoryID,
			root.Version,
			root.PreviousSHA256,
			digestHex,
			append([]byte(nil), root.Metadata...),
			now,
		)
		if err != nil {
			return RootState{}, fmt.Errorf("%w: insertar root v%d: %v", ErrInvalidTrustStore, root.Version, err)
		}
		nextState = RootState{Version: root.Version, SHA256: digestHex}
	}

	result, err := tx.ExecContext(ctx,
		"UPDATE repositories SET latest_root_version = ?, latest_root_sha256 = ? "+
			"WHERE id = ? AND latest_root_version = ? AND latest_root_sha256 = ?",
		nextState.Version,
		nextState.SHA256,
		repositoryID,
		expected.Version,
		expected.SHA256,
	)
	if err != nil {
		return RootState{}, fmt.Errorf("%w: actualizar high-water root: %v", ErrInvalidTrustStore, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return RootState{}, fmt.Errorf("%w: comprobar actualización high-water: %v", ErrInvalidTrustStore, err)
	}
	if affected != 1 {
		return RootState{}, ErrTrustStateConflict
	}
	if err := tx.Commit(); err != nil {
		return RootState{}, fmt.Errorf("%w: confirmar cadena de roots: %v", ErrInvalidTrustStore, err)
	}
	return nextState, nil
}

// LoadRootChain returns every root after the independently pinned anchor. It
// verifies exact-byte hashes, consecutive versions and previous-root links;
// callers must still replay the chain through go-tuf before trusting it.
func (store *Store) LoadRootChain(ctx context.Context, repositoryID string) ([]RootRecord, RootState, error) {
	if store == nil || store.db == nil {
		return nil, RootState{}, ErrInvalidTrustStore
	}
	if ctx == nil {
		return nil, RootState{}, fmt.Errorf("%w: el contexto no puede ser nil", ErrInvalidTrustStore)
	}

	// Read the anchor, high-water and chain from one database snapshot. Without
	// this transaction a concurrent root commit could make two valid reads
	// appear inconsistent.
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, RootState{}, fmt.Errorf("%w: iniciar lectura consistente: %v", ErrInvalidTrustStore, err)
	}
	defer tx.Rollback()

	var anchor RootState
	var latest RootState
	err = tx.QueryRowContext(ctx,
		"SELECT anchor_version, anchor_sha256, latest_root_version, latest_root_sha256 "+
			"FROM repositories WHERE id = ?",
		repositoryID,
	).Scan(&anchor.Version, &anchor.SHA256, &latest.Version, &latest.SHA256)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, RootState{}, ErrRepositoryNotFound
	}
	if err != nil {
		return nil, RootState{}, fmt.Errorf("%w: leer ancla y estado: %v", ErrInvalidTrustStore, err)
	}

	rows, err := tx.QueryContext(ctx,
		"SELECT version, previous_sha256, sha256, root_json, committed_at "+
			"FROM root_chain WHERE repository_id = ? ORDER BY version ASC",
		repositoryID,
	)
	if err != nil {
		return nil, RootState{}, fmt.Errorf("%w: leer cadena de roots: %v", ErrInvalidTrustStore, err)
	}

	chain := make([]RootRecord, 0)
	observed := anchor
	for rows.Next() {
		var record RootRecord
		if err := rows.Scan(
			&record.Version,
			&record.PreviousSHA256,
			&record.SHA256,
			&record.Metadata,
			&record.CommittedAt,
		); err != nil {
			_ = rows.Close()
			return nil, RootState{}, fmt.Errorf("%w: leer root: %v", ErrCorruptTrustStore, err)
		}
		if record.Version != observed.Version+1 || record.PreviousSHA256 != observed.SHA256 {
			_ = rows.Close()
			return nil, RootState{}, fmt.Errorf("%w: discontinuidad antes de root v%d", ErrCorruptTrustStore, record.Version)
		}
		if len(record.Metadata) == 0 || int64(len(record.Metadata)) > MaxRootMetadataBytes {
			_ = rows.Close()
			return nil, RootState{}, fmt.Errorf("%w: tamaño inválido en root v%d", ErrCorruptTrustStore, record.Version)
		}
		digest := sha256.Sum256(record.Metadata)
		if hex.EncodeToString(digest[:]) != record.SHA256 {
			_ = rows.Close()
			return nil, RootState{}, fmt.Errorf("%w: SHA-256 incorrecto en root v%d", ErrCorruptTrustStore, record.Version)
		}
		observed = RootState{Version: record.Version, SHA256: record.SHA256}
		record.Metadata = append([]byte(nil), record.Metadata...)
		chain = append(chain, record)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, RootState{}, fmt.Errorf("%w: recorrer cadena: %v", ErrCorruptTrustStore, err)
	}
	if err := rows.Close(); err != nil {
		return nil, RootState{}, fmt.Errorf("%w: cerrar lectura de cadena: %v", ErrCorruptTrustStore, err)
	}
	if observed != latest {
		return nil, RootState{}, fmt.Errorf("%w: la cadena no coincide con el high-water registrado", ErrCorruptTrustStore)
	}
	if err := tx.Commit(); err != nil {
		return nil, RootState{}, fmt.Errorf("%w: confirmar lectura consistente: %v", ErrInvalidTrustStore, err)
	}
	return chain, latest, nil
}

// Close releases the underlying SQLite connection.
func (store *Store) Close() error {
	if store == nil || store.db == nil {
		return nil
	}
	return store.db.Close()
}
