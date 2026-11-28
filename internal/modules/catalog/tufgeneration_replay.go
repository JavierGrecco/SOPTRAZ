package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/JavierGrecco/SOPTRAZ/internal/storage/sqlitetuf"
	"github.com/theupdateframework/go-tuf/v2/metadata"
	"github.com/theupdateframework/go-tuf/v2/metadata/trustedmetadata"
)

var ErrGenerationReplay = errors.New("replay criptográfico de generación TUF rechazado")

// replayVerifiedGeneration authenticates one immutable metadata generation
// using go-tuf's supported trustedmetadata API. rootJSON must already be an
// independently authenticated current root (for example after replaying the
// bootstrap root chain). referenceTime is mandatory and fixed for the entire
// operation; it replaces go-tuf's constructor default before any metadata
// update, so expiry decisions never use an implicit wall-clock reference.
// The function performs no network I/O.
//
// This is the shared cryptographic primitive for online generation capture and
// future strict-offline verification. It authenticates metadata and delegation
// edges, but does not resolve a target path or compare an artifact descriptor.
func replayVerifiedGeneration(
	rootJSON []byte,
	rootState sqlitetuf.RootState,
	referenceTime time.Time,
	roles []sqlitetuf.VerifiedRoleMetadata,
	maxDelegationDepth int,
) (*trustedmetadata.TrustedMetadata, error) {
	if len(rootJSON) == 0 || int64(len(rootJSON)) > sqlitetuf.MaxRootMetadataBytes {
		return nil, fmt.Errorf("%w: root JSON ausente o fuera de límite", ErrGenerationReplay)
	}
	if rootState.Version < 1 || referenceTime.IsZero() {
		return nil, fmt.Errorf("%w: root state o referencia temporal ausentes", ErrGenerationReplay)
	}
	if maxDelegationDepth < 1 || maxDelegationDepth > sqlitetuf.MaxGenerationRoles {
		return nil, fmt.Errorf("%w: límite de delegación inválido", ErrGenerationReplay)
	}
	rootDigest := sha256.Sum256(rootJSON)
	if hex.EncodeToString(rootDigest[:]) != rootState.SHA256 {
		return nil, fmt.Errorf("%w: root JSON no coincide con el estado fijado", ErrGenerationReplay)
	}
	root, err := metadata.Root().FromBytes(rootJSON)
	if err != nil {
		return nil, fmt.Errorf("%w: analizar root JSON: %w", ErrGenerationReplay, err)
	}
	if int64(root.Signed.Version) != rootState.Version {
		return nil, fmt.Errorf("%w: root version %d, esperada %d", ErrGenerationReplay, root.Signed.Version, rootState.Version)
	}

	replayed, err := trustedmetadata.New(rootJSON)
	if err != nil {
		return nil, fmt.Errorf("%w: inicializar go-tuf desde root: %w", ErrGenerationReplay, err)
	}
	if replayed.Root == nil || !reflect.DeepEqual(replayed.Root, root) {
		return nil, fmt.Errorf("%w: go-tuf no conservó la root exacta", ErrGenerationReplay)
	}
	replayed.RefTime = referenceTime.UTC().Round(0)

	if len(roles) < 3 || len(roles) > sqlitetuf.MaxGenerationRoles {
		return nil, fmt.Errorf("%w: cantidad de roles fuera de límite", ErrGenerationReplay)
	}

	roleBytes := make(map[string][]byte, len(roles))
	roleParents := make(map[string][]string, len(roles))
	targetValues := make(map[string]*metadata.Metadata[metadata.TargetsType], len(roles)-2)
	var timestampValue *metadata.Metadata[metadata.TimestampType]
	var snapshotValue *metadata.Metadata[metadata.SnapshotType]
	var totalBytes int64

	for _, role := range roles {
		if err := validateReplayRoleName(role.Role); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrGenerationReplay, err)
		}
		if _, exists := roleBytes[role.Role]; exists {
			return nil, fmt.Errorf("%w: rol duplicado %q", ErrGenerationReplay, role.Role)
		}
		limit := replayRoleSizeLimit(role.Role)
		if len(role.Metadata) == 0 || int64(len(role.Metadata)) > limit {
			return nil, fmt.Errorf("%w: tamaño de metadata inválido para %q", ErrGenerationReplay, role.Role)
		}
		totalBytes += int64(len(role.Metadata))
		if totalBytes > sqlitetuf.MaxGenerationTotalBytes {
			return nil, fmt.Errorf("%w: metadata total supera el límite", ErrGenerationReplay)
		}
		if role.Version < 1 || role.ExpiresAt.IsZero() {
			return nil, fmt.Errorf("%w: versión o expiración ausente en %q", ErrGenerationReplay, role.Role)
		}

		roleBytes[role.Role] = append([]byte(nil), role.Metadata...)
		parents := append([]string(nil), role.Parents...)
		sort.Strings(parents)
		for index, parent := range parents {
			if err := validateReplayRoleName(parent); err != nil {
				return nil, fmt.Errorf("%w: delegador de %q: %w", ErrGenerationReplay, role.Role, err)
			}
			if parent == role.Role || (index > 0 && parents[index-1] == parent) {
				return nil, fmt.Errorf("%w: delegación propia o duplicada en %q", ErrGenerationReplay, role.Role)
			}
		}
		roleParents[role.Role] = parents

		switch role.Role {
		case metadata.TIMESTAMP:
			if len(parents) != 0 {
				return nil, fmt.Errorf("%w: timestamp no admite delegador", ErrGenerationReplay)
			}
			parsed, err := metadata.Timestamp().FromBytes(role.Metadata)
			if err != nil {
				return nil, fmt.Errorf("%w: parsear timestamp: %w", ErrGenerationReplay, err)
			}
			if parsed.Signed.Version != role.Version || !parsed.Signed.Expires.Equal(role.ExpiresAt) {
				return nil, fmt.Errorf("%w: versión/expiración almacenada de timestamp no coincide", ErrGenerationReplay)
			}
			timestampValue = parsed

		case metadata.SNAPSHOT:
			if len(parents) != 0 {
				return nil, fmt.Errorf("%w: snapshot no admite delegador", ErrGenerationReplay)
			}
			parsed, err := metadata.Snapshot().FromBytes(role.Metadata)
			if err != nil {
				return nil, fmt.Errorf("%w: parsear snapshot: %w", ErrGenerationReplay, err)
			}
			if parsed.Signed.Version != role.Version || !parsed.Signed.Expires.Equal(role.ExpiresAt) {
				return nil, fmt.Errorf("%w: versión/expiración almacenada de snapshot no coincide", ErrGenerationReplay)
			}
			snapshotValue = parsed

		default:
			if role.Role == metadata.ROOT {
				return nil, fmt.Errorf("%w: root pertenece a root-chain, no a generation roles", ErrGenerationReplay)
			}
			if role.Role == metadata.TARGETS {
				if len(parents) != 0 {
					return nil, fmt.Errorf("%w: targets top-level no admite delegador", ErrGenerationReplay)
				}
			} else if len(parents) == 0 {
				return nil, fmt.Errorf("%w: rol delegado %q sin delegador", ErrGenerationReplay, role.Role)
			}
			parsed, err := metadata.Targets().FromBytes(role.Metadata)
			if err != nil {
				return nil, fmt.Errorf("%w: parsear targets %q: %w", ErrGenerationReplay, role.Role, err)
			}
			if parsed.Signed.Version != role.Version || !parsed.Signed.Expires.Equal(role.ExpiresAt) {
				return nil, fmt.Errorf("%w: versión/expiración almacenada de %q no coincide", ErrGenerationReplay, role.Role)
			}
			targetValues[role.Role] = parsed
		}
	}

	if timestampValue == nil || snapshotValue == nil || targetValues[metadata.TARGETS] == nil {
		return nil, fmt.Errorf("%w: faltan timestamp, snapshot o targets top-level", ErrGenerationReplay)
	}

	roleNames := make([]string, 0, len(roleParents))
	for roleName := range roleParents {
		roleNames = append(roleNames, roleName)
	}
	sort.Strings(roleNames)
	for _, roleName := range roleNames {
		if roleName == metadata.TIMESTAMP || roleName == metadata.SNAPSHOT || roleName == metadata.TARGETS {
			continue
		}
		for _, parent := range roleParents[roleName] {
			if parent == metadata.TIMESTAMP || parent == metadata.SNAPSHOT {
				return nil, fmt.Errorf("%w: solo un rol targets puede delegar metadata targets", ErrGenerationReplay)
			}
			if _, exists := roleBytes[parent]; !exists {
				return nil, fmt.Errorf("%w: el delegador %q de %q falta en la generación", ErrGenerationReplay, parent, roleName)
			}
		}
	}

	if _, err := replayed.UpdateTimestamp(roleBytes[metadata.TIMESTAMP]); err != nil {
		return nil, fmt.Errorf("%w: validar timestamp con go-tuf: %w", ErrGenerationReplay, err)
	}
	if _, err := replayed.UpdateSnapshot(roleBytes[metadata.SNAPSHOT], false); err != nil {
		return nil, fmt.Errorf("%w: validar snapshot con go-tuf: %w", ErrGenerationReplay, err)
	}
	if _, err := replayed.UpdateTargets(roleBytes[metadata.TARGETS]); err != nil {
		return nil, fmt.Errorf("%w: validar targets top-level con go-tuf: %w", ErrGenerationReplay, err)
	}
	if !reflect.DeepEqual(replayed.Timestamp, timestampValue) ||
		!reflect.DeepEqual(replayed.Snapshot, snapshotValue) ||
		!reflect.DeepEqual(replayed.Targets[metadata.TARGETS], targetValues[metadata.TARGETS]) {
		return nil, fmt.Errorf("%w: replay top-level no coincide con los bytes suministrados", ErrGenerationReplay)
	}

	verified := map[string]bool{metadata.TARGETS: true}
	depth := map[string]int{metadata.TARGETS: 0}
	pending := make([]string, 0, len(targetValues)-1)
	for roleName := range targetValues {
		if roleName != metadata.TARGETS {
			pending = append(pending, roleName)
		}
	}
	sort.Strings(pending)

	for len(pending) > 0 {
		progress := false
		remaining := make([]string, 0, len(pending))
		for _, roleName := range pending {
			parents := roleParents[roleName]
			ready := len(parents) > 0
			roleDepth := 1
			for _, parent := range parents {
				if !verified[parent] {
					ready = false
					break
				}
				if depth[parent]+1 > roleDepth {
					roleDepth = depth[parent] + 1
				}
			}
			if !ready {
				remaining = append(remaining, roleName)
				continue
			}
			if roleDepth > maxDelegationDepth {
				return nil, fmt.Errorf("%w: profundidad de delegación de %q supera %d", ErrGenerationReplay, roleName, maxDelegationDepth)
			}
			for _, parent := range parents {
				if _, err := replayed.UpdateDelegatedTargets(roleBytes[roleName], roleName, parent); err != nil {
					return nil, fmt.Errorf("%w: validar rol delegado %q desde %q: %w", ErrGenerationReplay, roleName, parent, err)
				}
			}
			if !reflect.DeepEqual(replayed.Targets[roleName], targetValues[roleName]) {
				return nil, fmt.Errorf("%w: replay delegado %q no coincide con los bytes", ErrGenerationReplay, roleName)
			}
			verified[roleName] = true
			depth[roleName] = roleDepth
			progress = true
		}
		if !progress {
			return nil, fmt.Errorf("%w: ciclo o grafo de delegaciones incompleto", ErrGenerationReplay)
		}
		pending = remaining
	}
	if len(verified) != len(targetValues) {
		return nil, fmt.Errorf("%w: no se autenticaron todos los roles delegados", ErrGenerationReplay)
	}
	return replayed, nil
}

func replayRoleSizeLimit(role string) int64 {
	switch role {
	case metadata.TIMESTAMP:
		return sqlitetuf.MaxTimestampMetadataSize
	case metadata.SNAPSHOT:
		return sqlitetuf.MaxSnapshotMetadataSize
	default:
		return sqlitetuf.MaxTargetsMetadataSize
	}
}

func validateReplayRoleName(role string) error {
	if role == "" || len(role) > sqlitetuf.MaxRoleNameLength || role != strings.TrimSpace(role) || role == "." || role == ".." || !utf8.ValidString(role) {
		return fmt.Errorf("nombre de rol vacío, no normalizado o fuera de límite")
	}
	for _, character := range role {
		if unicode.IsControl(character) {
			return fmt.Errorf("nombre de rol contiene caracteres de control")
		}
	}
	return nil
}
