package catalog

import (
	"errors"
	"fmt"
	"time"

	"github.com/JavierGrecco/SOPTRAZ/internal/modules/artifact"
	"github.com/JavierGrecco/SOPTRAZ/internal/storage/sqlitetuf"
	"github.com/theupdateframework/go-tuf/v2/metadata"
	"github.com/theupdateframework/go-tuf/v2/metadata/trustedmetadata"
)

var ErrOfflineGenerationReplay = errors.New("no se pudo autenticar la generación TUF offline")

type offlineTargetReplay struct {
	trusted    *trustedmetadata.TrustedMetadata
	resolution targetPathResolution
}

// replayOfflineTargetMetadata authenticates one target path using only a
// structurally consistent SQLite snapshot and an independently provisioned
// bootstrap root pin. It performs no filesystem, HTTP, DNS or socket I/O.
//
// The generation's Parents relations are intentionally ignored: they are
// mutable local claims, not TUF authority. Every delegated role is loaded and
// authenticated on demand with go-tuf against the exact signed parent that
// the canonical resolver reached. referenceTime must come from the accepted
// trusted-time policy; it is never inferred from SQLite or the wall clock.
//
// This primitive returns authenticated target metadata only. Artifact v1
// descriptor binding and issuance of a VerifiedTarget remain a separate gate.
func replayOfflineTargetMetadata(
	snapshot sqlitetuf.OfflineSnapshot,
	bootstrapRootJSON []byte,
	bootstrapState TrustState,
	referenceTime time.Time,
	targetPath string,
	maxDelegations int,
) (offlineTargetReplay, error) {
	if referenceTime.IsZero() {
		return offlineTargetReplay{}, fmt.Errorf("%w: referencia temporal ausente", ErrOfflineGenerationReplay)
	}
	if err := artifact.ValidateRelativePath(targetPath); err != nil {
		return offlineTargetReplay{}, fmt.Errorf("%w: ruta de target inválida: %v", ErrOfflineGenerationReplay, err)
	}
	if maxDelegations < 1 || maxDelegations > sqlitetuf.MaxGenerationRoles {
		return offlineTargetReplay{}, fmt.Errorf("%w: límite de delegaciones fuera de rango", ErrOfflineGenerationReplay)
	}
	if snapshot.Repository.ID == "" || snapshot.Generation.ID < 1 {
		return offlineTargetReplay{}, fmt.Errorf("%w: snapshot sin repositorio o generación activa", ErrOfflineGenerationReplay)
	}

	// The bootstrap pin is supplied independently by the caller. A pin stored
	// only in the same SQLite database would not establish an independent root
	// of trust, so the database copy must match the external value exactly.
	anchor, err := NewPinnedTrustAnchor(bootstrapRootJSON, bootstrapState.SHA256)
	if err != nil {
		return offlineTargetReplay{}, fmt.Errorf("%w: bootstrap root/pin inválido: %w", ErrOfflineGenerationReplay, err)
	}
	actualBootstrapState, err := anchor.State()
	if err != nil || actualBootstrapState != bootstrapState {
		return offlineTargetReplay{}, fmt.Errorf("%w: bootstrap root no coincide con el estado provisionado", ErrOfflineGenerationReplay)
	}
	if snapshot.Repository.AnchorVersion != bootstrapState.Version ||
		snapshot.Repository.AnchorSHA256 != bootstrapState.SHA256 {
		return offlineTargetReplay{}, fmt.Errorf("%w: el pin persistido no coincide con el ancla independiente", ErrOfflineGenerationReplay)
	}

	latestRootJSON, latestRoot, err := replayStoredRootChain(
		bootstrapRootJSON,
		bootstrapState,
		snapshot.RootChain,
	)
	if err != nil {
		return offlineTargetReplay{}, fmt.Errorf("%w: replay de root-chain: %w", ErrOfflineGenerationReplay, err)
	}
	persistedLatestRoot := sqlitetuf.RootState{
		Version: latestRoot.Version,
		SHA256:  latestRoot.SHA256,
	}
	if persistedLatestRoot != snapshot.LatestRoot || snapshot.Generation.Root != persistedLatestRoot {
		return offlineTargetReplay{}, fmt.Errorf("%w: root-chain, high-water y generación no coinciden", ErrOfflineGenerationReplay)
	}

	if len(snapshot.Generation.Roles) < 3 ||
		len(snapshot.Generation.Roles) > sqlitetuf.MaxGenerationRoles {
		return offlineTargetReplay{}, fmt.Errorf("%w: cantidad de roles fuera de límite", ErrOfflineGenerationReplay)
	}
	rolesByName := make(map[string]sqlitetuf.VerifiedRoleMetadata, len(snapshot.Generation.Roles))
	var totalBytes int64
	for _, role := range snapshot.Generation.Roles {
		if err := validateReplayRoleName(role.Role); err != nil {
			return offlineTargetReplay{}, fmt.Errorf("%w: nombre de rol inválido: %w", ErrOfflineGenerationReplay, err)
		}
		if role.Role == metadata.ROOT {
			return offlineTargetReplay{}, fmt.Errorf("%w: root debe provenir de root-chain", ErrOfflineGenerationReplay)
		}
		if _, exists := rolesByName[role.Role]; exists {
			return offlineTargetReplay{}, fmt.Errorf("%w: rol duplicado %q", ErrOfflineGenerationReplay, role.Role)
		}
		maxBytes := replayRoleSizeLimit(role.Role)
		if len(role.Metadata) == 0 || int64(len(role.Metadata)) > maxBytes {
			return offlineTargetReplay{}, fmt.Errorf("%w: metadata de %q ausente o fuera de límite", ErrOfflineGenerationReplay, role.Role)
		}
		totalBytes += int64(len(role.Metadata))
		if totalBytes > sqlitetuf.MaxGenerationTotalBytes {
			return offlineTargetReplay{}, fmt.Errorf("%w: tamaño total de metadata fuera de límite", ErrOfflineGenerationReplay)
		}
		if role.Version < 1 || role.ExpiresAt.IsZero() {
			return offlineTargetReplay{}, fmt.Errorf("%w: versión o expiración ausente en %q", ErrOfflineGenerationReplay, role.Role)
		}
		// Parents is deliberately not read here. The signed delegations in
		// authenticated targets metadata determine the only acceptable edges.
		rolesByName[role.Role] = role
	}
	for _, required := range []string{metadata.TIMESTAMP, metadata.SNAPSHOT, metadata.TARGETS} {
		if _, ok := rolesByName[required]; !ok {
			return offlineTargetReplay{}, fmt.Errorf("%w: falta metadata %q", ErrOfflineGenerationReplay, required)
		}
	}

	trusted, err := trustedmetadata.New(latestRootJSON)
	if err != nil {
		return offlineTargetReplay{}, fmt.Errorf("%w: inicializar go-tuf con la root autenticada: %w", ErrOfflineGenerationReplay, err)
	}
	// trustedmetadata.New initializes RefTime from time.Now(). Replace that
	// implicit default before the first operation that can check expiration.
	trusted.RefTime = referenceTime.UTC().Round(0)

	timestampRole := rolesByName[metadata.TIMESTAMP]
	timestamp, err := metadata.Timestamp().FromBytes(timestampRole.Metadata)
	if err != nil {
		return offlineTargetReplay{}, fmt.Errorf("%w: analizar timestamp: %w", ErrOfflineGenerationReplay, err)
	}
	if timestamp.Signed.Version != timestampRole.Version ||
		!timestamp.Signed.Expires.Equal(timestampRole.ExpiresAt) {
		return offlineTargetReplay{}, fmt.Errorf("%w: campos de timestamp persistidos no coinciden con los bytes firmados", ErrOfflineGenerationReplay)
	}
	if _, err := trusted.UpdateTimestamp(timestampRole.Metadata); err != nil {
		return offlineTargetReplay{}, fmt.Errorf("%w: verificar timestamp: %w", ErrOfflineGenerationReplay, err)
	}

	snapshotRole := rolesByName[metadata.SNAPSHOT]
	snapshotMetadata, err := metadata.Snapshot().FromBytes(snapshotRole.Metadata)
	if err != nil {
		return offlineTargetReplay{}, fmt.Errorf("%w: analizar snapshot: %w", ErrOfflineGenerationReplay, err)
	}
	if snapshotMetadata.Signed.Version != snapshotRole.Version ||
		!snapshotMetadata.Signed.Expires.Equal(snapshotRole.ExpiresAt) {
		return offlineTargetReplay{}, fmt.Errorf("%w: campos de snapshot persistidos no coinciden con los bytes firmados", ErrOfflineGenerationReplay)
	}
	if _, err := trusted.UpdateSnapshot(snapshotRole.Metadata, false); err != nil {
		return offlineTargetReplay{}, fmt.Errorf("%w: verificar snapshot: %w", ErrOfflineGenerationReplay, err)
	}

	targetsRole := rolesByName[metadata.TARGETS]
	targetsMetadata, err := metadata.Targets().FromBytes(targetsRole.Metadata)
	if err != nil {
		return offlineTargetReplay{}, fmt.Errorf("%w: analizar targets: %w", ErrOfflineGenerationReplay, err)
	}
	if targetsMetadata.Signed.Version != targetsRole.Version ||
		!targetsMetadata.Signed.Expires.Equal(targetsRole.ExpiresAt) {
		return offlineTargetReplay{}, fmt.Errorf("%w: campos de targets persistidos no coinciden con los bytes firmados", ErrOfflineGenerationReplay)
	}
	if _, err := trusted.UpdateTargets(targetsRole.Metadata); err != nil {
		return offlineTargetReplay{}, fmt.Errorf("%w: verificar targets top-level: %w", ErrOfflineGenerationReplay, err)
	}

	resolution, err := resolveTargetPath(
		targetPath,
		maxDelegations,
		func(roleName, parentRole string) (*metadata.Metadata[metadata.TargetsType], error) {
			if roleName == metadata.TARGETS {
				if parentRole != metadata.ROOT {
					return nil, fmt.Errorf("targets top-level solicitado desde un delegador inesperado %q", parentRole)
				}
				if trusted.Targets[metadata.TARGETS] == nil {
					return nil, errors.New("go-tuf no conservó targets top-level verificado")
				}
				return trusted.Targets[metadata.TARGETS], nil
			}
			if roleName == metadata.ROOT || roleName == metadata.TIMESTAMP || roleName == metadata.SNAPSHOT {
				return nil, fmt.Errorf("nombre de rol reservado %q en delegación", roleName)
			}
			role, ok := rolesByName[roleName]
			if !ok {
				return nil, fmt.Errorf("metadata %q no está incluida en la generación", roleName)
			}
			parsed, err := metadata.Targets().FromBytes(role.Metadata)
			if err != nil {
				return nil, fmt.Errorf("analizar rol %q: %w", roleName, err)
			}
			if parsed.Signed.Version != role.Version ||
				!parsed.Signed.Expires.Equal(role.ExpiresAt) {
				return nil, fmt.Errorf("campos persistidos del rol %q no coinciden con los bytes firmados", roleName)
			}
			updated, err := trusted.UpdateDelegatedTargets(role.Metadata, roleName, parentRole)
			if err != nil {
				return nil, fmt.Errorf("go-tuf rechazó el rol %q desde %q: %w", roleName, parentRole, err)
			}
			return updated, nil
		},
	)
	if err != nil {
		return offlineTargetReplay{}, fmt.Errorf("%w: resolver %q: %w", ErrOfflineGenerationReplay, targetPath, err)
	}
	return offlineTargetReplay{
		trusted:    trusted,
		resolution: resolution,
	}, nil
}
