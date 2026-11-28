package sqlitetuf

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// OfflineSnapshot is a structurally consistent, read-only view of the local
// trust store. Its bytes are not cryptographically trusted until the catalog
// layer replays the root chain and generation through go-tuf trustedmetadata.
type OfflineSnapshot struct {
	Repository RepositoryConfig
	RootChain  []RootRecord
	LatestRoot RootState
	Generation Generation
}

// LoadOfflineSnapshot loads the repository pin, root chain, high-water state,
// active generation, metadata blobs and delegation edges inside one SQLite
// read transaction. This prevents an online root commit or generation switch
// from producing a mixed view assembled from separate database snapshots.
// The method performs no network access and does not authenticate TUF metadata.
func (store *Store) LoadOfflineSnapshot(ctx context.Context, repositoryID string) (OfflineSnapshot, error) {
	if store == nil || store.db == nil {
		return OfflineSnapshot{}, ErrInvalidTrustStore
	}
	if ctx == nil {
		return OfflineSnapshot{}, fmt.Errorf("%w: el contexto no puede ser nil", ErrInvalidGeneration)
	}
	if repositoryID == "" {
		return OfflineSnapshot{}, fmt.Errorf("%w: ID de repositorio vacío", ErrInvalidRepository)
	}

	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return OfflineSnapshot{}, fmt.Errorf("%w: iniciar snapshot offline: %v", ErrInvalidTrustStore, err)
	}
	defer tx.Rollback()

	var snapshot OfflineSnapshot
	err = tx.QueryRowContext(ctx,
		"SELECT id, metadata_url, anchor_version, anchor_sha256 FROM repositories WHERE id = ?",
		repositoryID,
	).Scan(
		&snapshot.Repository.ID,
		&snapshot.Repository.MetadataURL,
		&snapshot.Repository.AnchorVersion,
		&snapshot.Repository.AnchorSHA256,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return OfflineSnapshot{}, ErrRepositoryNotFound
	}
	if err != nil {
		return OfflineSnapshot{}, fmt.Errorf("%w: leer configuración del repositorio: %v", ErrInvalidTrustStore, err)
	}
	if err := validateRepositoryConfig(snapshot.Repository); err != nil {
		return OfflineSnapshot{}, fmt.Errorf("%w: configuración persistida inválida: %v", ErrCorruptTrustStore, err)
	}

	snapshot.RootChain, snapshot.LatestRoot, err = loadRootChainFromTx(ctx, tx, repositoryID)
	if err != nil {
		return OfflineSnapshot{}, err
	}
	snapshot.Generation, err = loadActiveGenerationFromTx(ctx, tx, repositoryID)
	if err != nil {
		return OfflineSnapshot{}, err
	}
	if snapshot.Generation.Root != snapshot.LatestRoot {
		return OfflineSnapshot{}, ErrGenerationRootMismatch
	}

	if err := tx.Commit(); err != nil {
		return OfflineSnapshot{}, fmt.Errorf("%w: confirmar snapshot offline: %v", ErrInvalidTrustStore, err)
	}
	return snapshot, nil
}
