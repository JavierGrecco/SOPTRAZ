package sqliteaudit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/JavierGrecco/SOPTRAZ/internal/audit"
	"github.com/JavierGrecco/SOPTRAZ/internal/contracts"
	_ "modernc.org/sqlite"
)

const (
	schemaVersion = 1
	busyTimeoutMS = 5000
)

var (
	ErrCorruptChain = errors.New("la cadena de auditoría está dañada")
	ErrHeadMismatch = errors.New("la cabecera de auditoría no coincide con los eventos")
)

// Store persists immutable audit events in a local SQLite database.
// One pooled connection and BEGIN IMMEDIATE serialize Field writes; Hub will
// provide a separate PostgreSQL adapter behind audit.Store.
type Store struct {
	db *sql.DB
}

// Open creates or opens a Field audit database. The directory is created with
// owner-only permissions where the operating system supports them.
func Open(ctx context.Context, path string) (*Store, error) {
	if ctx == nil {
		return nil, fmt.Errorf("el contexto de auditoría no puede ser nil")
	}
	if path == "" {
		return nil, fmt.Errorf("la ruta de la base de auditoría está vacía")
	}
	if path == ":memory:" {
		return nil, fmt.Errorf("la base de auditoría Field requiere una ruta de archivo")
	}

	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolver la ruta de auditoría: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o700); err != nil {
		return nil, fmt.Errorf("crear el directorio de auditoría: %w", err)
	}

	db, err := sql.Open("sqlite", sqliteDSN(absolutePath))
	if err != nil {
		return nil, fmt.Errorf("abrir la base SQLite de auditoría: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	store := &Store{db: db}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("conectar con la base SQLite de auditoría: %w", err)
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

// sqliteDSN converts a local absolute path into a SQLite file URI.
// Windows drive letters must be part of the URI path (file:///C:/...), not
// the authority (file://C:/...), otherwise SQLite rejects "C:" as a URI host.
func sqliteDSN(path string) string {
	return sqliteFileDSN(path, runtime.GOOS == "windows")
}

func sqliteFileDSN(path string, windows bool) string {
	fileURL := url.URL{
		Scheme: "file",
		Path:   sqliteURIPath(path, windows),
	}
	query := fileURL.Query()
	query.Set("_txlock", "immediate")
	fileURL.RawQuery = query.Encode()
	return fileURL.String()
}

// sqliteURIPath converts a filesystem path into the path component of a
// file: URI that SQLite maps back to the same file.
//
// url.URL.String() writes "file://" before the path. A path that does not
// begin with "/" therefore puts its first segment in the URI authority slot.
// SQLite only accepts an empty authority or "localhost"; any other authority
// is rejected with "invalid uri authority".
//
// Windows drive paths are normalized to /X:/path and UNC paths to
// //server/share/path. Relative paths are made absolute before URI encoding.
// The windows parameter keeps the Windows rules testable on every platform.
func sqliteURIPath(path string, windows bool) string {
	if !isRootedPath(path, windows) {
		if absolutePath, err := filepath.Abs(path); err == nil {
			path = absolutePath
		}
	}
	if windows {
		path = strings.ReplaceAll(path, "\\", "/")
		if hasDriveLetter(path) {
			path = "/" + path
		}
	}
	return path
}

func isRootedPath(path string, windows bool) bool {
	if strings.HasPrefix(path, "/") {
		return true
	}
	if !windows {
		return false
	}
	return strings.HasPrefix(path, "\\\\") || hasDriveLetter(path)
}

// hasDriveLetter reports whether path starts with an absolute Windows drive
// designator such as "C:/" or "C:\\".
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

func (s *Store) configure(ctx context.Context) error {
	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA recursive_triggers = ON",
		fmt.Sprintf("PRAGMA busy_timeout = %d", busyTimeoutMS),
		"PRAGMA synchronous = FULL",
	}
	for _, statement := range pragmas {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("configurar SQLite (%s): %w", statement, err)
		}
	}
	return nil
}

func (s *Store) migrate(ctx context.Context) error {
	var current int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("leer versión del esquema de auditoría: %w", err)
	}
	if current > schemaVersion {
		return fmt.Errorf("la base de auditoría usa una versión de esquema no compatible: %d", current)
	}
	if current == schemaVersion {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("iniciar migración de auditoría: %w", err)
	}
	defer tx.Rollback()

	statements := []string{
		"CREATE TABLE audit_heads (" +
			"intervention_id TEXT PRIMARY KEY, " +
			"sequence INTEGER NOT NULL CHECK(sequence >= 0), " +
			"head_hash TEXT NOT NULL DEFAULT '')",
		"CREATE TABLE audit_events (" +
			"event_id TEXT PRIMARY KEY, " +
			"intervention_id TEXT NOT NULL, " +
			"sequence INTEGER NOT NULL CHECK(sequence >= 1), " +
			"event_json TEXT NOT NULL, " +
			"hash TEXT NOT NULL CHECK(length(hash) = 64), " +
			"UNIQUE(intervention_id, sequence), " +
			"FOREIGN KEY(intervention_id) REFERENCES audit_heads(intervention_id))",
		"CREATE INDEX idx_audit_events_intervention_sequence " +
			"ON audit_events(intervention_id, sequence)",
		"CREATE TRIGGER audit_events_no_update BEFORE UPDATE ON audit_events " +
			"BEGIN SELECT RAISE(ABORT, 'SOPTRAZ audit events are append-only'); END",
		"CREATE TRIGGER audit_events_no_delete BEFORE DELETE ON audit_events " +
			"BEGIN SELECT RAISE(ABORT, 'SOPTRAZ audit events are append-only'); END",
		"PRAGMA user_version = 1",
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("aplicar migración de auditoría: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("confirmar migración de auditoría: %w", err)
	}
	return nil
}

// Append adds one event to the intervention chain. The sequence, previous hash
// and event hash are assigned atomically inside an immediate write transaction.
func (s *Store) Append(ctx context.Context, draft audit.Draft) (audit.Event, error) {
	if ctx == nil {
		return audit.Event{}, fmt.Errorf("el contexto de auditoría no puede ser nil")
	}
	payload, err := audit.NormalizePayload(draft.Payload)
	if err != nil {
		return audit.Event{}, err
	}

	eventID := draft.EventID
	if eventID == "" {
		eventID, err = audit.NewEventID()
		if err != nil {
			return audit.Event{}, err
		}
	}

	occurredAt := draft.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	} else {
		occurredAt = occurredAt.UTC()
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return audit.Event{}, fmt.Errorf("iniciar transacción de auditoría: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx,
		"INSERT INTO audit_heads(intervention_id, sequence, head_hash) "+
			"VALUES(?, 0, '') ON CONFLICT(intervention_id) DO NOTHING",
		draft.InterventionID,
	)
	if err != nil {
		return audit.Event{}, fmt.Errorf("preparar cabecera de auditoría: %w", err)
	}

	var previousSequence int64
	var previousHash string
	err = tx.QueryRowContext(ctx,
		"SELECT sequence, head_hash FROM audit_heads WHERE intervention_id = ?",
		draft.InterventionID,
	).Scan(&previousSequence, &previousHash)
	if err != nil {
		return audit.Event{}, fmt.Errorf("leer cabecera de auditoría: %w", err)
	}
	if previousSequence > 0 {
		var storedHeadHash string
		if err := tx.QueryRowContext(ctx,
			"SELECT hash FROM audit_events WHERE intervention_id = ? AND sequence = ?",
			draft.InterventionID,
			previousSequence,
		).Scan(&storedHeadHash); err != nil {
			return audit.Event{}, fmt.Errorf("%w: no se pudo confirmar el último evento antes de anexar: %v", ErrHeadMismatch, err)
		}
		if storedHeadHash != previousHash {
			return audit.Event{}, fmt.Errorf("%w: la cabecera no coincide con el último evento", ErrHeadMismatch)
		}
	}

	event := audit.Event{
		SchemaVersion:  audit.SchemaVersion,
		EventID:        eventID,
		InterventionID: draft.InterventionID,
		Sequence:       previousSequence + 1,
		EventType:      draft.EventType,
		OccurredAt:     occurredAt.Format(time.RFC3339Nano),
		Actor:          draft.Actor,
		Payload:        payload,
		PreviousHash:   previousHash,
	}

	event.Hash, err = audit.ComputeHash(event)
	if err != nil {
		return audit.Event{}, err
	}
	serialized, err := json.Marshal(event)
	if err != nil {
		return audit.Event{}, fmt.Errorf("serializar evento de auditoría: %w", err)
	}
	if err := contracts.ValidateDocument("event", serialized); err != nil {
		return audit.Event{}, fmt.Errorf("evento de auditoría rechazado: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		"INSERT INTO audit_events(event_id, intervention_id, sequence, event_json, hash) "+
			"VALUES(?, ?, ?, ?, ?)",
		event.EventID,
		event.InterventionID,
		event.Sequence,
		string(serialized),
		event.Hash,
	)
	if err != nil {
		return audit.Event{}, fmt.Errorf("agregar evento inmutable: %w", err)
	}

	result, err := tx.ExecContext(ctx,
		"UPDATE audit_heads SET sequence = ?, head_hash = ? "+
			"WHERE intervention_id = ? AND sequence = ? AND head_hash = ?",
		event.Sequence,
		event.Hash,
		event.InterventionID,
		previousSequence,
		previousHash,
	)
	if err != nil {
		return audit.Event{}, fmt.Errorf("actualizar cabecera de auditoría: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return audit.Event{}, fmt.Errorf("comprobar actualización de cabecera: %w", err)
	}
	if affected != 1 {
		return audit.Event{}, fmt.Errorf("conflicto de secuencia al agregar evento de auditoría")
	}

	if err := tx.Commit(); err != nil {
		return audit.Event{}, fmt.Errorf("confirmar evento de auditoría: %w", err)
	}
	return event, nil
}

// List returns the stored events in sequence order. Call Verify before relying
// on them as a trusted chain.
func (s *Store) List(ctx context.Context, interventionID string) ([]audit.Event, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT event_json, hash FROM audit_events "+
			"WHERE intervention_id = ? ORDER BY sequence ASC",
		interventionID,
	)
	if err != nil {
		return nil, fmt.Errorf("consultar eventos de auditoría: %w", err)
	}
	defer rows.Close()

	events := make([]audit.Event, 0)
	for rows.Next() {
		var raw string
		var storedHash string
		if err := rows.Scan(&raw, &storedHash); err != nil {
			return nil, fmt.Errorf("leer evento de auditoría: %w", err)
		}
		var event audit.Event
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			return nil, fmt.Errorf("%w: JSON de evento no legible: %v", ErrCorruptChain, err)
		}
		if event.Hash != storedHash {
			return nil, fmt.Errorf("%w: el hash indexado no coincide con el JSON del evento %s", ErrCorruptChain, event.EventID)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recorrer eventos de auditoría: %w", err)
	}
	return events, nil
}

// Verify validates every event, its sequence, its previous-hash link, its
// computed hash, and the persisted head checkpoint for one intervention.
func (s *Store) Verify(ctx context.Context, interventionID string) error {
	var headSequence int64
	var headHash string
	err := s.db.QueryRowContext(ctx,
		"SELECT sequence, head_hash FROM audit_heads WHERE intervention_id = ?",
		interventionID,
	).Scan(&headSequence, &headHash)
	if errors.Is(err, sql.ErrNoRows) {
		var count int
		if countErr := s.db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM audit_events WHERE intervention_id = ?",
			interventionID,
		).Scan(&count); countErr != nil {
			return fmt.Errorf("comprobar eventos sin cabecera: %w", countErr)
		}
		if count != 0 {
			return fmt.Errorf("%w: hay eventos sin cabecera", ErrCorruptChain)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("leer cabecera de verificación: %w", err)
	}

	events, err := s.List(ctx, interventionID)
	if err != nil {
		return err
	}

	var expectedSequence int64 = 1
	var expectedPreviousHash string
	for _, event := range events {
		if event.InterventionID != interventionID {
			return fmt.Errorf("%w: el evento pertenece a otra intervención", ErrCorruptChain)
		}
		if event.Sequence != expectedSequence {
			return fmt.Errorf("%w: secuencia esperada %d, recibida %d", ErrCorruptChain, expectedSequence, event.Sequence)
		}
		if event.PreviousHash != expectedPreviousHash {
			return fmt.Errorf("%w: enlace previo incorrecto en el evento %d", ErrCorruptChain, event.Sequence)
		}

		raw, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("%w: no se pudo serializar el evento %d: %v", ErrCorruptChain, event.Sequence, err)
		}
		if err := contracts.ValidateDocument("event", raw); err != nil {
			return fmt.Errorf("%w: evento %d no cumple su contrato: %v", ErrCorruptChain, event.Sequence, err)
		}
		computed, err := audit.ComputeHash(event)
		if err != nil {
			return fmt.Errorf("%w: no se pudo recalcular el hash del evento %d: %v", ErrCorruptChain, event.Sequence, err)
		}
		if computed != event.Hash {
			return fmt.Errorf("%w: hash incorrecto en el evento %d", ErrCorruptChain, event.Sequence)
		}

		expectedPreviousHash = event.Hash
		expectedSequence++
	}

	lastSequence := expectedSequence - 1
	if lastSequence != headSequence {
		return fmt.Errorf("%w: cabecera indica %d eventos y se encontraron %d", ErrHeadMismatch, headSequence, lastSequence)
	}
	if expectedPreviousHash != headHash {
		return fmt.Errorf("%w: el hash de cabecera no coincide con el último evento", ErrHeadMismatch)
	}
	return nil
}

// Close releases the underlying database resources.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}
