// Package sqlitetuf stores the durable local trust state for authenticated TUF
// repositories. Generation records are accepted only after the catalog layer
// has authenticated the supplied metadata with go-tuf.
package sqlitetuf

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	MaxGenerationRoles       = 128
	MaxGenerationTotalBytes  = 32 * 1024 * 1024
	MaxTimestampMetadataSize = 16 * 1024
	MaxSnapshotMetadataSize  = 2 * 1024 * 1024
	MaxTargetsMetadataSize   = 5 * 1024 * 1024
	MaxRoleNameLength        = 512
)

var (
	ErrInvalidGeneration      = errors.New("generación TUF inválida")
	ErrGenerationIncomplete   = errors.New("generación TUF incompleta")
	ErrGenerationNotFound     = errors.New("no hay generación TUF activa")
	ErrGenerationConflict     = errors.New("la generación TUF activa cambió concurrentemente")
	ErrGenerationRootMismatch = errors.New("la raíz actual no coincide con la generación TUF")
	ErrGenerationCorrupt      = errors.New("la generación TUF almacenada está dañada")
	ErrRoleRollback           = errors.New("rollback de versión de metadata TUF rechazado")
	ErrRoleEquivocation       = errors.New("la misma versión de metadata TUF tiene bytes diferentes")
)

// VerifiedRoleMetadata is one metadata role whose bytes and signature chain
// have already been authenticated by the catalog layer using go-tuf.
// Parents lists the TUF targets roles that delegate this role. The standard
// timestamp, snapshot and targets roles have no entries in Parents.
type VerifiedRoleMetadata struct {
	Role      string
	Parents   []string
	Version   int64
	ExpiresAt time.Time
	Metadata  []byte
}

// Generation is an immutable, committed snapshot of all metadata roles
// supplied by the caller. It is not automatically an offline-usable catalog:
// the catalog layer must replay and validate every role with go-tuf first.
type Generation struct {
	ID             int64
	ParentID       int64
	Root           RootState
	ManifestSHA256 string
	CreatedAt      time.Time
	Roles          []VerifiedRoleMetadata
}

// ActiveGenerationID returns the current committed generation ID. It returns
// zero when the repository has no active generation yet.
func (store *Store) ActiveGenerationID(ctx context.Context, repositoryID string) (int64, error) {
	if store == nil || store.db == nil {
		return 0, ErrInvalidTrustStore
	}
	if ctx == nil {
		return 0, fmt.Errorf("%w: el contexto no puede ser nil", ErrInvalidGeneration)
	}
	var generationID int64
	err := store.db.QueryRowContext(ctx,
		"SELECT generation_id FROM active_generations WHERE repository_id = ?",
		repositoryID,
	).Scan(&generationID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("%w: leer generación activa: %v", ErrInvalidTrustStore, err)
	}
	return generationID, nil
}

// CommitGeneration atomically inserts a complete verified role set, updates
// per-role high-water marks, and switches active_generations. The expected
// active ID is a compare-and-swap guard; pass zero for the first generation.
// Root must equal the repository's latest durably committed root.
func (store *Store) CommitGeneration(
	ctx context.Context,
	repositoryID string,
	expectedActiveID int64,
	root RootState,
	roles []VerifiedRoleMetadata,
) (Generation, error) {
	if store == nil || store.db == nil {
		return Generation{}, ErrInvalidTrustStore
	}
	if ctx == nil {
		return Generation{}, fmt.Errorf("%w: el contexto no puede ser nil", ErrInvalidGeneration)
	}
	if repositoryID == "" || expectedActiveID < 0 {
		return Generation{}, fmt.Errorf("%w: ID de repositorio o generación esperada inválidos", ErrInvalidGeneration)
	}
	if root.Version < 1 || validateSHA256(root.SHA256) != nil {
		return Generation{}, fmt.Errorf("%w: root de referencia inválida", ErrInvalidGeneration)
	}

	normalized, totalBytes, err := normalizeGenerationRoles(roles)
	if err != nil {
		return Generation{}, err
	}
	if totalBytes > MaxGenerationTotalBytes {
		return Generation{}, fmt.Errorf("%w: el conjunto supera %d bytes", ErrInvalidGeneration, MaxGenerationTotalBytes)
	}

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return Generation{}, fmt.Errorf("%w: iniciar commit de generación: %v", ErrInvalidTrustStore, err)
	}
	defer tx.Rollback()

	var latestRoot RootState
	err = tx.QueryRowContext(ctx,
		"SELECT latest_root_version, latest_root_sha256 FROM repositories WHERE id = ?",
		repositoryID,
	).Scan(&latestRoot.Version, &latestRoot.SHA256)
	if errors.Is(err, sql.ErrNoRows) {
		return Generation{}, ErrRepositoryNotFound
	}
	if err != nil {
		return Generation{}, fmt.Errorf("%w: leer latest-root: %v", ErrInvalidTrustStore, err)
	}
	if latestRoot != root {
		return Generation{}, ErrGenerationRootMismatch
	}

	var currentActiveID int64
	err = tx.QueryRowContext(ctx,
		"SELECT generation_id FROM active_generations WHERE repository_id = ?",
		repositoryID,
	).Scan(&currentActiveID)
	if errors.Is(err, sql.ErrNoRows) {
		currentActiveID = 0
	} else if err != nil {
		return Generation{}, fmt.Errorf("%w: leer generación activa: %v", ErrInvalidTrustStore, err)
	}
	if currentActiveID != expectedActiveID {
		return Generation{}, ErrGenerationConflict
	}

	var latestGenerationID int64
	err = tx.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(generation_id), 0) FROM generations WHERE repository_id = ?",
		repositoryID,
	).Scan(&latestGenerationID)
	if err != nil {
		return Generation{}, fmt.Errorf("%w: leer secuencia de generaciones: %v", ErrInvalidTrustStore, err)
	}
	if latestGenerationID != currentActiveID {
		return Generation{}, fmt.Errorf("%w: hay generaciones no activas fuera de secuencia", ErrGenerationCorrupt)
	}
	generationID := latestGenerationID + 1
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	manifestSHA256 := generationManifestDigest(repositoryID, generationID, expectedActiveID, root, normalized)

	for _, role := range normalized {
		var highWaterVersion int64
		var highWaterSHA256 string
		err := tx.QueryRowContext(ctx,
			"SELECT version, sha256 FROM role_high_water WHERE repository_id = ? AND role_name = ?",
			repositoryID,
			role.Role,
		).Scan(&highWaterVersion, &highWaterSHA256)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return Generation{}, fmt.Errorf("%w: leer high-water de %q: %v", ErrInvalidTrustStore, role.Role, err)
		}
		if err == nil {
			digest := sha256.Sum256(role.Metadata)
			receivedSHA256 := hex.EncodeToString(digest[:])
			switch {
			case role.Version < highWaterVersion:
				return Generation{}, fmt.Errorf("%w: rol %q, actual %d y recibido %d",
					ErrRoleRollback, role.Role, highWaterVersion, role.Version)
			case role.Version == highWaterVersion && receivedSHA256 != highWaterSHA256:
				return Generation{}, fmt.Errorf("%w: rol %q, versión %d",
					ErrRoleEquivocation, role.Role, role.Version)
			}
		}
	}

	_, err = tx.ExecContext(ctx,
		"INSERT INTO generations(repository_id, generation_id, parent_generation_id, root_version, root_sha256, manifest_sha256, role_count, created_at) "+
			"VALUES(?, ?, NULLIF(?, 0), ?, ?, ?, ?, ?)",
		repositoryID,
		generationID,
		expectedActiveID,
		root.Version,
		root.SHA256,
		manifestSHA256,
		len(normalized),
		nowText,
	)
	if err != nil {
		return Generation{}, fmt.Errorf("%w: insertar generación: %v", ErrInvalidTrustStore, err)
	}

	for _, role := range normalized {
		digest := sha256.Sum256(role.Metadata)
		roleSHA256 := hex.EncodeToString(digest[:])
		expiresAt := role.ExpiresAt.UTC().Format(time.RFC3339Nano)
		_, err = tx.ExecContext(ctx,
			"INSERT INTO generation_metadata(repository_id, generation_id, role_name, version, expires_at, sha256, metadata_json) "+
				"VALUES(?, ?, ?, ?, ?, ?, ?)",
			repositoryID,
			generationID,
			role.Role,
			role.Version,
			expiresAt,
			roleSHA256,
			append([]byte(nil), role.Metadata...),
		)
		if err != nil {
			return Generation{}, fmt.Errorf("%w: insertar metadata del rol %q: %v", ErrInvalidTrustStore, role.Role, err)
		}

		_, err = tx.ExecContext(ctx,
			"INSERT INTO role_high_water(repository_id, role_name, version, sha256, expires_at, source_generation_id) "+
				"VALUES(?, ?, ?, ?, ?, ?) "+
				"ON CONFLICT(repository_id, role_name) DO UPDATE SET "+
				"version = excluded.version, sha256 = excluded.sha256, expires_at = excluded.expires_at, "+
				"source_generation_id = excluded.source_generation_id "+
				"WHERE excluded.version > role_high_water.version",
			repositoryID,
			role.Role,
			role.Version,
			roleSHA256,
			expiresAt,
			generationID,
		)
		if err != nil {
			return Generation{}, fmt.Errorf("%w: actualizar high-water de %q: %v", ErrInvalidTrustStore, role.Role, err)
		}
	}

	// Insert delegation edges only after every role row exists. This keeps the
	// foreign-key contract independent of lexical ordering of child/parent names.
	for _, role := range normalized {
		for _, parent := range role.Parents {
			_, err = tx.ExecContext(ctx,
				"INSERT INTO generation_role_parents(repository_id, generation_id, child_role, parent_role) "+
					"VALUES(?, ?, ?, ?)",
				repositoryID,
				generationID,
				role.Role,
				parent,
			)
			if err != nil {
				return Generation{}, fmt.Errorf("%w: insertar delegación %q -> %q: %v", ErrInvalidTrustStore, parent, role.Role, err)
			}
		}
	}

	if expectedActiveID == 0 {
		_, err = tx.ExecContext(ctx,
			"INSERT INTO active_generations(repository_id, generation_id, activated_at) VALUES(?, ?, ?)",
			repositoryID,
			generationID,
			nowText,
		)
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx,
			"UPDATE active_generations SET generation_id = ?, activated_at = ? "+
				"WHERE repository_id = ? AND generation_id = ?",
			generationID,
			nowText,
			repositoryID,
			expectedActiveID,
		)
		if err == nil {
			var affected int64
			affected, err = result.RowsAffected()
			if err == nil && affected != 1 {
				err = ErrGenerationConflict
			}
		}
	}
	if err != nil {
		if errors.Is(err, ErrGenerationConflict) {
			return Generation{}, err
		}
		return Generation{}, fmt.Errorf("%w: activar generación: %v", ErrInvalidTrustStore, err)
	}
	if err := tx.Commit(); err != nil {
		return Generation{}, fmt.Errorf("%w: confirmar generación: %v", ErrInvalidTrustStore, err)
	}

	return Generation{
		ID:             generationID,
		ParentID:       expectedActiveID,
		Root:           root,
		ManifestSHA256: manifestSHA256,
		CreatedAt:      now,
		Roles:          cloneGenerationRoles(normalized),
	}, nil
}

// LoadActiveGeneration reads the active generation from one SQLite snapshot.
// It checks all blob digests, the manifest, role high-water values, required
// roles, and the current root. Cryptographic authentication of TUF signatures
// remains the caller's responsibility.
// LoadActiveGeneration reads and validates the active generation in one SQLite snapshot.
func (store *Store) LoadActiveGeneration(ctx context.Context, repositoryID string) (Generation, error) {
	if store == nil || store.db == nil {
		return Generation{}, ErrInvalidTrustStore
	}
	if ctx == nil {
		return Generation{}, fmt.Errorf("%w: el contexto no puede ser nil", ErrInvalidGeneration)
	}
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Generation{}, fmt.Errorf("%w: iniciar lectura de generación: %v", ErrInvalidTrustStore, err)
	}
	defer tx.Rollback()

	generation, err := loadActiveGenerationFromTx(ctx, tx, repositoryID)
	if err != nil {
		return Generation{}, err
	}
	if err := tx.Commit(); err != nil {
		return Generation{}, fmt.Errorf("%w: confirmar lectura de generación: %v", ErrInvalidTrustStore, err)
	}
	return generation, nil
}

func loadActiveGenerationFromTx(ctx context.Context, tx *sql.Tx, repositoryID string) (Generation, error) {
	var latestRoot RootState
	err = tx.QueryRowContext(ctx,
		"SELECT latest_root_version, latest_root_sha256 FROM repositories WHERE id = ?",
		repositoryID,
	).Scan(&latestRoot.Version, &latestRoot.SHA256)
	if errors.Is(err, sql.ErrNoRows) {
		return Generation{}, ErrRepositoryNotFound
	}
	if err != nil {
		return Generation{}, fmt.Errorf("%w: leer latest-root: %v", ErrInvalidTrustStore, err)
	}

	var generationID int64
	err = tx.QueryRowContext(ctx,
		"SELECT generation_id FROM active_generations WHERE repository_id = ?",
		repositoryID,
	).Scan(&generationID)
	if errors.Is(err, sql.ErrNoRows) {
		return Generation{}, ErrGenerationNotFound
	}
	if err != nil {
		return Generation{}, fmt.Errorf("%w: leer puntero activo: %v", ErrInvalidTrustStore, err)
	}

	var generation Generation
	var parentID sql.NullInt64
	var createdAt string
	var roleCount int
	err = tx.QueryRowContext(ctx,
		"SELECT generation_id, parent_generation_id, root_version, root_sha256, manifest_sha256, role_count, created_at "+
			"FROM generations WHERE repository_id = ? AND generation_id = ?",
		repositoryID,
		generationID,
	).Scan(
		&generation.ID,
		&parentID,
		&generation.Root.Version,
		&generation.Root.SHA256,
		&generation.ManifestSHA256,
		&roleCount,
		&createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Generation{}, fmt.Errorf("%w: el puntero referencia una generación inexistente", ErrGenerationCorrupt)
	}
	if err != nil {
		return Generation{}, fmt.Errorf("%w: leer generación: %v", ErrInvalidTrustStore, err)
	}
	if roleCount < 3 || roleCount > MaxGenerationRoles {
		return Generation{}, fmt.Errorf("%w: cantidad de roles fuera de límites", ErrGenerationCorrupt)
	}
	if parentID.Valid {
		generation.ParentID = parentID.Int64
	}
	generation.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return Generation{}, fmt.Errorf("%w: fecha de generación inválida", ErrGenerationCorrupt)
	}
	if generation.Root != latestRoot {
		return Generation{}, ErrGenerationRootMismatch
	}

	rows, err := tx.QueryContext(ctx,
		"SELECT role_name, version, expires_at, sha256, metadata_json "+
			"FROM generation_metadata WHERE repository_id = ? AND generation_id = ? ORDER BY role_name ASC",
		repositoryID,
		generationID,
	)
	if err != nil {
		return Generation{}, fmt.Errorf("%w: leer roles de generación: %v", ErrInvalidTrustStore, err)
	}

	generation.Roles = make([]VerifiedRoleMetadata, 0, roleCount)
	var totalRoleBytes int64
	for rows.Next() {
		var role VerifiedRoleMetadata
		var expiresText, storedSHA256 string
		if err := rows.Scan(&role.Role, &role.Version, &expiresText, &storedSHA256, &role.Metadata); err != nil {
			_ = rows.Close()
			return Generation{}, fmt.Errorf("%w: leer metadata de rol: %v", ErrGenerationCorrupt, err)
		}
		role.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiresText)
		if err != nil {
			_ = rows.Close()
			return Generation{}, fmt.Errorf("%w: expiración inválida en rol %q", ErrGenerationCorrupt, role.Role)
		}
		if role.Version < 1 || len(role.Metadata) == 0 || int64(len(role.Metadata)) > maxRoleMetadataBytes(role.Role) {
			_ = rows.Close()
			return Generation{}, fmt.Errorf("%w: tamaño o versión inválidos en rol %q", ErrGenerationCorrupt, role.Role)
		}
		totalRoleBytes += int64(len(role.Metadata))
		if totalRoleBytes > MaxGenerationTotalBytes {
			_ = rows.Close()
			return Generation{}, fmt.Errorf("%w: tamaño total de metadata excede el límite", ErrGenerationCorrupt)
		}
		digest := sha256.Sum256(role.Metadata)
		if hex.EncodeToString(digest[:]) != storedSHA256 {
			_ = rows.Close()
			return Generation{}, fmt.Errorf("%w: SHA-256 incorrecto en rol %q", ErrGenerationCorrupt, role.Role)
		}

		var highWaterVersion int64
		var highWaterSHA256 string
		if err := tx.QueryRowContext(ctx,
			"SELECT version, sha256 FROM role_high_water WHERE repository_id = ? AND role_name = ?",
			repositoryID,
			role.Role,
		).Scan(&highWaterVersion, &highWaterSHA256); err != nil {
			_ = rows.Close()
			return Generation{}, fmt.Errorf("%w: falta high-water para rol %q", ErrGenerationCorrupt, role.Role)
		}
		if highWaterVersion != role.Version || highWaterSHA256 != storedSHA256 {
			_ = rows.Close()
			return Generation{}, fmt.Errorf("%w: el high-water no coincide con el rol %q activo", ErrGenerationCorrupt, role.Role)
		}
		role.Metadata = append([]byte(nil), role.Metadata...)
		generation.Roles = append(generation.Roles, role)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return Generation{}, fmt.Errorf("%w: recorrer roles: %v", ErrGenerationCorrupt, err)
	}
	if err := rows.Close(); err != nil {
		return Generation{}, fmt.Errorf("%w: cerrar lectura de roles: %v", ErrGenerationCorrupt, err)
	}

	parentRows, err := tx.QueryContext(ctx,
		"SELECT child_role, parent_role FROM generation_role_parents "+
			"WHERE repository_id = ? AND generation_id = ? ORDER BY child_role, parent_role",
		repositoryID,
		generationID,
	)
	if err != nil {
		return Generation{}, fmt.Errorf("%w: leer relaciones de delegación: %v", ErrInvalidTrustStore, err)
	}
	roleIndexes := make(map[string]int, len(generation.Roles))
	for index := range generation.Roles {
		roleIndexes[generation.Roles[index].Role] = index
	}
	for parentRows.Next() {
		var child, parent string
		if err := parentRows.Scan(&child, &parent); err != nil {
			_ = parentRows.Close()
			return Generation{}, fmt.Errorf("%w: leer relación de delegación: %v", ErrGenerationCorrupt, err)
		}
		index, ok := roleIndexes[child]
		if !ok {
			_ = parentRows.Close()
			return Generation{}, fmt.Errorf("%w: delegación con rol hijo ausente", ErrGenerationCorrupt)
		}
		generation.Roles[index].Parents = append(generation.Roles[index].Parents, parent)
	}
	if err := parentRows.Err(); err != nil {
		_ = parentRows.Close()
		return Generation{}, fmt.Errorf("%w: recorrer delegaciones: %v", ErrGenerationCorrupt, err)
	}
	if err := parentRows.Close(); err != nil {
		return Generation{}, fmt.Errorf("%w: cerrar delegaciones: %v", ErrGenerationCorrupt, err)
	}

	// Every persistent high-water value must be backed by the immutable metadata
	// row from which it was raised, including roles omitted from the active
	// generation. This prevents orphaned counters from outliving their evidence.
	highWaterRows, err := tx.QueryContext(ctx,
		"SELECT h.role_name, h.version, h.sha256, h.expires_at, h.source_generation_id, "+
			"m.version, m.sha256, m.expires_at, m.metadata_json "+
			"FROM role_high_water h LEFT JOIN generation_metadata m "+
			"ON m.repository_id = h.repository_id AND m.generation_id = h.source_generation_id AND m.role_name = h.role_name "+
			"WHERE h.repository_id = ? ORDER BY h.role_name",
		repositoryID,
	)
	if err != nil {
		return Generation{}, fmt.Errorf("%w: leer procedencia de high-water: %v", ErrInvalidTrustStore, err)
	}
	for highWaterRows.Next() {
		var roleName, highWaterSHA, highWaterExpiry string
		var sourceGenerationID, highWaterVersion int64
		var evidenceVersion sql.NullInt64
		var evidenceSHA, evidenceExpiry sql.NullString
		var evidenceMetadata []byte
		if err := highWaterRows.Scan(
			&roleName, &highWaterVersion, &highWaterSHA, &highWaterExpiry, &sourceGenerationID,
			&evidenceVersion, &evidenceSHA, &evidenceExpiry, &evidenceMetadata,
		); err != nil {
			_ = highWaterRows.Close()
			return Generation{}, fmt.Errorf("%w: leer procedencia de high-water: %v", ErrGenerationCorrupt, err)
		}
		evidenceDigest := sha256.Sum256(evidenceMetadata)
		evidenceSHA256 := hex.EncodeToString(evidenceDigest[:])
		if sourceGenerationID < 1 || sourceGenerationID > generation.ID ||
			!evidenceVersion.Valid || !evidenceSHA.Valid || !evidenceExpiry.Valid ||
			evidenceVersion.Int64 != highWaterVersion ||
			evidenceSHA.String != highWaterSHA ||
			evidenceExpiry.String != highWaterExpiry ||
			evidenceSHA256 != highWaterSHA {
			_ = highWaterRows.Close()
			return Generation{}, fmt.Errorf("%w: high-water sin evidencia inmutable íntegra para rol %q", ErrGenerationCorrupt, roleName)
		}
	}
	if err := highWaterRows.Err(); err != nil {
		_ = highWaterRows.Close()
		return Generation{}, fmt.Errorf("%w: recorrer procedencia de high-water: %v", ErrGenerationCorrupt, err)
	}
	if err := highWaterRows.Close(); err != nil {
		return Generation{}, fmt.Errorf("%w: cerrar procedencia de high-water: %v", ErrGenerationCorrupt, err)
	}

	normalized, _, err := normalizeGenerationRoles(generation.Roles)
	if err != nil {
		return Generation{}, fmt.Errorf("%w: conjunto de roles inválido: %v", ErrGenerationCorrupt, err)
	}
	if len(normalized) != roleCount {
		return Generation{}, fmt.Errorf("%w: cantidad de roles inconsistente", ErrGenerationCorrupt)
	}
	if manifest := generationManifestDigest(repositoryID, generation.ID, generation.ParentID, generation.Root, normalized); manifest != generation.ManifestSHA256 {
		return Generation{}, fmt.Errorf("%w: digest del manifiesto incorrecto", ErrGenerationCorrupt)
	}
	generation.Roles = normalized


	return generation, nil
}

func normalizeGenerationRoles(roles []VerifiedRoleMetadata) ([]VerifiedRoleMetadata, int64, error) {
	if len(roles) < 3 {
		return nil, 0, ErrGenerationIncomplete
	}
	if len(roles) > MaxGenerationRoles {
		return nil, 0, fmt.Errorf("%w: cantidad de roles superior a %d", ErrInvalidGeneration, MaxGenerationRoles)
	}

	normalized := cloneGenerationRoles(roles)
	roleIndexes := make(map[string]int, len(normalized))
	var totalBytes int64
	for index := range normalized {
		role := &normalized[index]
		if err := validateRoleName(role.Role); err != nil {
			return nil, 0, err
		}
		if role.Role == "root" {
			return nil, 0, fmt.Errorf("%w: root se almacena en root_chain, no como role_metadata", ErrInvalidGeneration)
		}
		if _, exists := roleIndexes[role.Role]; exists {
			return nil, 0, fmt.Errorf("%w: rol duplicado %q", ErrInvalidGeneration, role.Role)
		}
		roleIndexes[role.Role] = index
		if role.Version < 1 {
			return nil, 0, fmt.Errorf("%w: versión inválida en rol %q", ErrInvalidGeneration, role.Role)
		}
		if role.ExpiresAt.IsZero() {
			return nil, 0, fmt.Errorf("%w: expiración ausente en rol %q", ErrInvalidGeneration, role.Role)
		}
		maxBytes := maxRoleMetadataBytes(role.Role)
		if len(role.Metadata) == 0 || int64(len(role.Metadata)) > maxBytes {
			return nil, 0, fmt.Errorf("%w: tamaño inválido en rol %q", ErrInvalidGeneration, role.Role)
		}
		totalBytes += int64(len(role.Metadata))
		if totalBytes > MaxGenerationTotalBytes {
			return nil, 0, fmt.Errorf("%w: el conjunto supera %d bytes", ErrInvalidGeneration, MaxGenerationTotalBytes)
		}
		parents := append([]string(nil), role.Parents...)
		sort.Strings(parents)
		for parentIndex, parent := range parents {
			if err := validateRoleName(parent); err != nil {
				return nil, 0, err
			}
			if parent == role.Role {
				return nil, 0, fmt.Errorf("%w: un rol no puede delegarse a sí mismo", ErrInvalidGeneration)
			}
			if parentIndex > 0 && parents[parentIndex-1] == parent {
				return nil, 0, fmt.Errorf("%w: relación de delegación duplicada", ErrInvalidGeneration)
			}
		}
		role.Parents = parents
		role.ExpiresAt = role.ExpiresAt.UTC()
		role.Metadata = append([]byte(nil), role.Metadata...)
	}

	for _, required := range []string{"timestamp", "snapshot", "targets"} {
		index, ok := roleIndexes[required]
		if !ok {
			return nil, 0, fmt.Errorf("%w: falta el rol %q", ErrGenerationIncomplete, required)
		}
		if len(normalized[index].Parents) != 0 {
			return nil, 0, fmt.Errorf("%w: el rol superior %q no puede tener delegador targets", ErrInvalidGeneration, required)
		}
	}
	for index := range normalized {
		role := &normalized[index]
		if role.Role == "timestamp" || role.Role == "snapshot" || role.Role == "targets" {
			continue
		}
		if len(role.Parents) == 0 {
			return nil, 0, fmt.Errorf("%w: el rol delegado %q no declara delegador", ErrInvalidGeneration, role.Role)
		}
		for _, parent := range role.Parents {
			if _, ok := roleIndexes[parent]; !ok {
				return nil, 0, fmt.Errorf("%w: el delegador %q de %q no está en la generación",
					ErrInvalidGeneration, parent, role.Role)
			}
			if parent == "timestamp" || parent == "snapshot" {
				return nil, 0, fmt.Errorf("%w: solo un rol targets puede delegar metadata targets", ErrInvalidGeneration)
			}
		}
	}
	if err := validateDelegationGraph(normalized); err != nil {
		return nil, 0, err
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i].Role < normalized[j].Role })
	return normalized, totalBytes, nil
}

func validateRoleName(role string) error {
	if role == "" || len(role) > MaxRoleNameLength || role != strings.TrimSpace(role) || role == "." || role == ".." {
		return fmt.Errorf("%w: nombre de rol vacío, demasiado largo o no normalizado", ErrInvalidGeneration)
	}
	for _, r := range role {
		if unicode.IsControl(r) {
			return fmt.Errorf("%w: el nombre de rol contiene caracteres de control", ErrInvalidGeneration)
		}
	}
	return nil
}

func validateDelegationGraph(roles []VerifiedRoleMetadata) error {
	parents := make(map[string][]string, len(roles))
	for _, role := range roles {
		parents[role.Role] = role.Parents
	}
	const (
		unvisited = 0
		visiting  = 1
		visited   = 2
	)
	state := make(map[string]int, len(roles))
	var visit func(string) error
	visit = func(role string) error {
		switch state[role] {
		case visiting:
			return fmt.Errorf("%w: ciclo en delegaciones de %q", ErrInvalidGeneration, role)
		case visited:
			return nil
		}
		state[role] = visiting
		for _, parent := range parents[role] {
			if err := visit(parent); err != nil {
				return err
			}
		}
		state[role] = visited
		return nil
	}
	for role := range parents {
		if err := visit(role); err != nil {
			return err
		}
	}
	return nil
}

func maxRoleMetadataBytes(role string) int64 {
	switch role {
	case "timestamp":
		return MaxTimestampMetadataSize
	case "snapshot":
		return MaxSnapshotMetadataSize
	default:
		return MaxTargetsMetadataSize
	}
}

func cloneGenerationRoles(roles []VerifiedRoleMetadata) []VerifiedRoleMetadata {
	cloned := make([]VerifiedRoleMetadata, len(roles))
	for index, role := range roles {
		cloned[index] = role
		cloned[index].Metadata = append([]byte(nil), role.Metadata...)
		cloned[index].Parents = append([]string(nil), role.Parents...)
	}
	return cloned
}

func generationManifestDigest(
	repositoryID string,
	generationID int64,
	parentGenerationID int64,
	root RootState,
	roles []VerifiedRoleMetadata,
) string {
	digest := sha256.New()
	_, _ = digest.Write([]byte("SOPTRAZ-TUF-GENERATION-v1\x00"))
	writeManifestString(digest, repositoryID)
	writeManifestInt64(digest, generationID)
	writeManifestInt64(digest, parentGenerationID)
	writeManifestInt64(digest, root.Version)
	writeManifestString(digest, root.SHA256)
	for _, role := range roles {
		writeManifestString(digest, role.Role)
		writeManifestInt64(digest, role.Version)
		writeManifestString(digest, role.ExpiresAt.UTC().Format(time.RFC3339Nano))
		writeManifestBytes(digest, role.Metadata)
		writeManifestInt64(digest, int64(len(role.Parents)))
		for _, parent := range role.Parents {
			writeManifestString(digest, parent)
		}
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func writeManifestString(digest hash.Hash, value string) {
	writeManifestBytes(digest, []byte(value))
}

func writeManifestBytes(digest hash.Hash, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = digest.Write(length[:])
	_, _ = digest.Write(value)
}

func writeManifestInt64(digest hash.Hash, value int64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(value))
	_, _ = digest.Write(encoded[:])
}
