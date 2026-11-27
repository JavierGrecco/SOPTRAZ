package sqliteaudit

import (
	"context"
	"fmt"
	"time"

	"github.com/JavierGrecco/SOPTRAZ/internal/audit"
)

// CreateCheckpoint builds an unsigned checkpoint from one verified SQLite
// snapshot and an externally managed public signer identity. The private key
// never enters this adapter.
//
// previousCheckpointHash must come from a previously verified checkpoint or
// an independent trust anchor. This method does not establish trust in it.
func (s *Store) CreateCheckpoint(
	ctx context.Context,
	interventionID string,
	previousCheckpointHash string,
	signerIdentity audit.CheckpointKeyIdentity,
	createdAt time.Time,
) (audit.Checkpoint, error) {
	snapshot, err := s.VerifiedSnapshot(ctx, interventionID)
	if err != nil {
		return audit.Checkpoint{}, fmt.Errorf("obtener instantánea verificada para checkpoint: %w", err)
	}
	if snapshot.Sequence < 1 || len(snapshot.Events) == 0 || snapshot.HeadHash == "" {
		return audit.Checkpoint{}, fmt.Errorf("no se puede crear un checkpoint para una intervención sin eventos")
	}

	checkpoint, err := audit.NewCheckpointWithIdentity(
		snapshot.InterventionID,
		uint64(snapshot.Sequence),
		snapshot.HeadHash,
		previousCheckpointHash,
		signerIdentity,
		createdAt,
	)
	if err != nil {
		return audit.Checkpoint{}, fmt.Errorf("construir checkpoint desde la instantánea verificada: %w", err)
	}
	return checkpoint, nil
}
