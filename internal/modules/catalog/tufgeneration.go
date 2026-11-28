// Package catalog coordinates TUF's verified in-memory metadata with the
// durable generation store. It never promotes cache files merely because they
// exist: every byte is compared with go-tuf's trusted state and replayed
// through the official trustedmetadata verification API before persistence.
package catalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"

	"github.com/JavierGrecco/SOPTRAZ/internal/storage/sqlitetuf"
	"github.com/theupdateframework/go-tuf/v2/metadata"
	"github.com/theupdateframework/go-tuf/v2/metadata/trustedmetadata"
)

var (
	ErrGenerationPersistenceUnavailable = errors.New("persistencia de generaciones TUF no disponible")
	ErrGenerationCapture                = errors.New("no se pudo capturar una generación TUF verificada")
)

type generationDelegationVisit struct {
	role   string
	parent string
	path   []string
}

// CommitVerifiedGeneration persists the exact metadata bytes authenticated by
// the current official go-tuf Updater. Call it after resolving every target
// descriptor that must be available from this generation. The method replays
// top-level and delegated metadata with trustedmetadata before the atomic
// SQLite commit. A successful call does not by itself enable offline use.
func (client *TUFClient) CommitVerifiedGeneration(ctx context.Context) (sqlitetuf.Generation, error) {
	if client == nil {
		return sqlitetuf.Generation{}, ErrTUFClientUnavailable
	}
	if ctx == nil {
		return sqlitetuf.Generation{}, fmt.Errorf("%w: el contexto no puede ser nil", ErrGenerationCapture)
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	return client.commitVerifiedGenerationLocked(ctx)
}

// commitVerifiedGenerationLocked must be called while client.mu is held.
func (client *TUFClient) commitVerifiedGenerationLocked(ctx context.Context) (sqlitetuf.Generation, error) {
	if client == nil {
		return sqlitetuf.Generation{}, ErrTUFClientUnavailable
	}
	if ctx == nil {
		return sqlitetuf.Generation{}, fmt.Errorf("%w: el contexto no puede ser nil", ErrGenerationCapture)
	}
	if client.persistence == nil || client.persistence.store == nil {
		return sqlitetuf.Generation{}, ErrGenerationPersistenceUnavailable
	}
	if client.updater == nil {
		return sqlitetuf.Generation{}, ErrTUFClientUnavailable
	}
	if len(client.resolvedTargets) == 0 {
		return sqlitetuf.Generation{}, fmt.Errorf("%w: primero deben resolverse los descriptores requeridos", ErrGenerationCapture)
	}

	paths := make([]string, 0, len(client.resolvedTargets))
	for targetPath := range client.resolvedTargets {
		paths = append(paths, targetPath)
	}
	sort.Strings(paths)

	trusted := client.updater.GetTrustedMetadataSet()
	roles, err := captureAndVerifyGenerationMetadata(
		client.config.LocalMetadataDir,
		client.persistence.latestRootJSON,
		client.persistence.rootState,
		trusted,
		paths,
		client.config.MaxDelegations,
	)
	if err != nil {
		return sqlitetuf.Generation{}, err
	}

	activeID, err := client.persistence.store.ActiveGenerationID(ctx, client.persistence.repositoryID)
	if err != nil {
		return sqlitetuf.Generation{}, fmt.Errorf("%w: leer generación activa: %v", ErrGenerationCapture, err)
	}
	if activeID > 0 {
		active, loadErr := client.persistence.store.LoadActiveGeneration(ctx, client.persistence.repositoryID)
		if loadErr == nil && generationContentMatches(active, client.persistence.rootState, roles) {
			return active, nil
		}
		if loadErr != nil &&
			!errors.Is(loadErr, sqlitetuf.ErrGenerationRootMismatch) &&
			!errors.Is(loadErr, sqlitetuf.ErrGenerationNotFound) {
			return sqlitetuf.Generation{}, fmt.Errorf("%w: validar generación activa anterior: %v", ErrGenerationCapture, loadErr)
		}
	}

	generation, err := client.persistence.store.CommitGeneration(
		ctx,
		client.persistence.repositoryID,
		activeID,
		sqlitetuf.RootState{
			Version: client.persistence.rootState.Version,
			SHA256:  client.persistence.rootState.SHA256,
		},
		roles,
	)
	if err != nil {
		return sqlitetuf.Generation{}, fmt.Errorf("comprometer generación TUF verificada: %w", err)
	}
	return generation, nil
}

func generationContentMatches(
	active sqlitetuf.Generation,
	root sqlitetuf.RootState,
	candidate []sqlitetuf.VerifiedRoleMetadata,
) bool {
	if active.Root != root || len(active.Roles) != len(candidate) {
		return false
	}
	activeRoles := make(map[string]sqlitetuf.VerifiedRoleMetadata, len(active.Roles))
	for _, role := range active.Roles {
		activeRoles[role.Role] = role
	}
	for _, role := range candidate {
		stored, ok := activeRoles[role.Role]
		if !ok || stored.Version != role.Version ||
			!stored.ExpiresAt.Equal(role.ExpiresAt) ||
			!bytes.Equal(stored.Metadata, role.Metadata) ||
			!sameGenerationRoleParents(stored.Parents, role.Parents) {
			return false
		}
	}
	return true
}

func sameGenerationRoleParents(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	leftSorted := append([]string(nil), left...)
	rightSorted := append([]string(nil), right...)
	sort.Strings(leftSorted)
	sort.Strings(rightSorted)
	for index := range leftSorted {
		if leftSorted[index] != rightSorted[index] {
			return false
		}
	}
	return true
}

func captureAndVerifyGenerationMetadata(
	metadataCacheDir string,
	rootJSON []byte,
	rootState sqlitetuf.RootState,
	trusted trustedmetadata.TrustedMetadata,
	resolvedPaths []string,
	maxDelegations int,
) ([]sqlitetuf.VerifiedRoleMetadata, error) {
	if metadataCacheDir == "" || len(rootJSON) == 0 ||
		rootState.Version < 1 || trusted.Root == nil ||
		trusted.Timestamp == nil || trusted.Snapshot == nil ||
		trusted.Targets[metadata.TARGETS] == nil {
		return nil, fmt.Errorf("%w: falta metadata top-level confiable", ErrGenerationCapture)
	}

	rootDigest := sha256.Sum256(rootJSON)
	if int64(len(rootJSON)) > sqlitetuf.MaxRootMetadataBytes ||
		int64(trusted.Root.Signed.Version) != rootState.Version ||
		hex.EncodeToString(rootDigest[:]) != rootState.SHA256 {
		return nil, fmt.Errorf("%w: root exacto y root persistido no coinciden", ErrGenerationCapture)
	}
	parsedRoot, err := metadata.Root().FromBytes(rootJSON)
	if err != nil {
		return nil, fmt.Errorf("%w: analizar root persistida: %v", ErrGenerationCapture, err)
	}
	if !reflect.DeepEqual(parsedRoot, trusted.Root) {
		return nil, fmt.Errorf("%w: root persistida distinta de la aceptada por go-tuf", ErrGenerationCapture)
	}

	timestampBytes, err := readCachedMetadata(metadataCacheDir, metadata.TIMESTAMP, sqlitetuf.MaxTimestampMetadataSize)
	if err != nil {
		return nil, err
	}
	timestamp, err := metadata.Timestamp().FromBytes(timestampBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: analizar timestamp: %v", ErrGenerationCapture, err)
	}
	if !reflect.DeepEqual(timestamp, trusted.Timestamp) {
		return nil, fmt.Errorf("%w: timestamp de caché distinto del aceptado por go-tuf", ErrGenerationCapture)
	}

	snapshotBytes, err := readCachedMetadata(metadataCacheDir, metadata.SNAPSHOT, sqlitetuf.MaxSnapshotMetadataSize)
	if err != nil {
		return nil, err
	}
	snapshot, err := metadata.Snapshot().FromBytes(snapshotBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: analizar snapshot: %v", ErrGenerationCapture, err)
	}
	if !reflect.DeepEqual(snapshot, trusted.Snapshot) {
		return nil, fmt.Errorf("%w: snapshot de caché distinto del aceptado por go-tuf", ErrGenerationCapture)
	}

	targetsBytes, err := readCachedMetadata(metadataCacheDir, metadata.TARGETS, sqlitetuf.MaxTargetsMetadataSize)
	if err != nil {
		return nil, err
	}
	targets, err := metadata.Targets().FromBytes(targetsBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: analizar targets: %v", ErrGenerationCapture, err)
	}
	if !reflect.DeepEqual(targets, trusted.Targets[metadata.TARGETS]) {
		return nil, fmt.Errorf("%w: targets de caché distintos de los aceptados por go-tuf", ErrGenerationCapture)
	}

	parentsByRole, selectedRoles, err := collectResolvedDelegations(trusted, resolvedPaths, maxDelegations)
	if err != nil {
		return nil, err
	}

	roleBytes := make(map[string][]byte, len(selectedRoles))
	roleValues := make(map[string]*metadata.Metadata[metadata.TargetsType], len(selectedRoles))
	roleNames := make([]string, 0, len(selectedRoles))
	for roleName := range selectedRoles {
		roleNames = append(roleNames, roleName)
	}
	sort.Strings(roleNames)
	for _, roleName := range roleNames {
		if roleName == metadata.TARGETS {
			roleBytes[roleName] = targetsBytes
			roleValues[roleName] = targets
			continue
		}
		raw, err := readCachedMetadata(metadataCacheDir, roleName, sqlitetuf.MaxTargetsMetadataSize)
		if err != nil {
			return nil, err
		}
		parsed, err := metadata.Targets().FromBytes(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: analizar rol delegado %q: %v", ErrGenerationCapture, roleName, err)
		}
		expected, ok := trusted.Targets[roleName]
		if !ok || expected == nil || !reflect.DeepEqual(parsed, expected) {
			return nil, fmt.Errorf("%w: rol delegado %q de caché distinto del aceptado por go-tuf", ErrGenerationCapture, roleName)
		}
		roleBytes[roleName] = raw
		roleValues[roleName] = parsed
	}

	// Replay exact bytes from a clean trust state. The reference time is copied
	// from the official updater so the replay tests the same expiry instant.
	replayed, err := trustedmetadata.New(rootJSON)
	if err != nil {
		return nil, fmt.Errorf("%w: reconstruir estado root: %v", ErrGenerationCapture, err)
	}
	replayed.RefTime = trusted.RefTime
	if _, err := replayed.UpdateTimestamp(timestampBytes); err != nil {
		return nil, fmt.Errorf("%w: revalidar timestamp con go-tuf: %v", ErrGenerationCapture, err)
	}
	if _, err := replayed.UpdateSnapshot(snapshotBytes, false); err != nil {
		return nil, fmt.Errorf("%w: revalidar snapshot con go-tuf: %v", ErrGenerationCapture, err)
	}
	if _, err := replayed.UpdateTargets(targetsBytes); err != nil {
		return nil, fmt.Errorf("%w: revalidar targets con go-tuf: %v", ErrGenerationCapture, err)
	}
	if !reflect.DeepEqual(replayed.Timestamp, trusted.Timestamp) ||
		!reflect.DeepEqual(replayed.Snapshot, trusted.Snapshot) ||
		!reflect.DeepEqual(replayed.Targets[metadata.TARGETS], trusted.Targets[metadata.TARGETS]) {
		return nil, fmt.Errorf("%w: el replay top-level no coincide con el updater oficial", ErrGenerationCapture)
	}

	pending := make([]string, 0, len(selectedRoles)-1)
	for _, roleName := range roleNames {
		if roleName != metadata.TARGETS {
			pending = append(pending, roleName)
		}
	}
	verified := map[string]bool{metadata.TARGETS: true}
	for len(pending) > 0 {
		progress := false
		remaining := make([]string, 0, len(pending))
		for _, roleName := range pending {
			parents := parentsByRole[roleName]
			ready := len(parents) > 0
			for _, parent := range parents {
				if !verified[parent] {
					ready = false
					break
				}
			}
			if !ready {
				remaining = append(remaining, roleName)
				continue
			}

			for _, parent := range parents {
				if _, err := replayed.UpdateDelegatedTargets(roleBytes[roleName], roleName, parent); err != nil {
					return nil, fmt.Errorf("%w: revalidar delegación %q desde %q: %v",
						ErrGenerationCapture, roleName, parent, err)
				}
			}
			if !reflect.DeepEqual(replayed.Targets[roleName], roleValues[roleName]) {
				return nil, fmt.Errorf("%w: replay del rol delegado %q no coincide con go-tuf", ErrGenerationCapture, roleName)
			}
			verified[roleName] = true
			progress = true
		}
		if !progress {
			return nil, fmt.Errorf("%w: delegaciones no verificables o grafo incompleto", ErrGenerationCapture)
		}
		pending = remaining
	}
	if len(verified) != len(selectedRoles) {
		return nil, fmt.Errorf("%w: faltan roles delegados verificados", ErrGenerationCapture)
	}

	result := make([]sqlitetuf.VerifiedRoleMetadata, 0, 2+len(selectedRoles))
	result = append(result,
		sqlitetuf.VerifiedRoleMetadata{
			Role:      metadata.TIMESTAMP,
			Version:   timestamp.Signed.Version,
			ExpiresAt: timestamp.Signed.Expires,
			Metadata:  append([]byte(nil), timestampBytes...),
		},
		sqlitetuf.VerifiedRoleMetadata{
			Role:      metadata.SNAPSHOT,
			Version:   snapshot.Signed.Version,
			ExpiresAt: snapshot.Signed.Expires,
			Metadata:  append([]byte(nil), snapshotBytes...),
		},
	)
	for _, roleName := range roleNames {
		role := roleValues[roleName]
		parents := make([]string, 0, len(parentsByRole[roleName]))
		for parent := range parentsByRole[roleName] {
			parents = append(parents, parent)
		}
		sort.Strings(parents)
		result = append(result, sqlitetuf.VerifiedRoleMetadata{
			Role:      roleName,
			Parents:   parents,
			Version:   role.Signed.Version,
			ExpiresAt: role.Signed.Expires,
			Metadata:  append([]byte(nil), roleBytes[roleName]...),
		})
	}
	return result, nil
}

func readCachedMetadata(cacheDir, roleName string, maxBytes int64) ([]byte, error) {
	if cacheDir == "" || roleName == "" || maxBytes < 1 {
		return nil, fmt.Errorf("%w: ubicación de metadata inválida", ErrGenerationCapture)
	}
	filePath := filepath.Join(cacheDir, url.PathEscape(roleName)+".json")
	before, err := os.Lstat(filePath)
	if err != nil {
		return nil, fmt.Errorf("%w: localizar metadata %q: %v", ErrGenerationCapture, roleName, err)
	}
	if !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 ||
		before.Size() < 1 || before.Size() > maxBytes {
		return nil, fmt.Errorf("%w: metadata %q no es un archivo regular dentro del límite", ErrGenerationCapture, roleName)
	}
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("%w: abrir metadata %q: %v", ErrGenerationCapture, roleName, err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("%w: inspeccionar metadata %q: %v", ErrGenerationCapture, roleName, err)
	}
	if !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return nil, fmt.Errorf("%w: metadata %q cambió durante la apertura", ErrGenerationCapture, roleName)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: leer metadata %q: %v", ErrGenerationCapture, roleName, err)
	}
	if len(data) == 0 || int64(len(data)) > maxBytes || int64(len(data)) != opened.Size() {
		return nil, fmt.Errorf("%w: metadata %q excede el límite o cambió durante la lectura", ErrGenerationCapture, roleName)
	}
	return data, nil
}

// collectResolvedDelegations mirrors go-tuf v2.5.0's pre-order delegation
// walk, but only over the in-memory metadata that the official updater loaded.
// This captures parent edges actually traversed for successful target paths;
// it does not claim to discover the repository's complete delegation graph.
func collectResolvedDelegations(
	trusted trustedmetadata.TrustedMetadata,
	targetPaths []string,
	maxDelegations int,
) (map[string][]string, map[string]bool, error) {
	if trusted.Targets[metadata.TARGETS] == nil || len(targetPaths) == 0 || maxDelegations < 1 {
		return nil, nil, fmt.Errorf("%w: falta el rol targets o rutas resueltas", ErrGenerationCapture)
	}

	parentSets := make(map[string]map[string]bool)
	selected := map[string]bool{metadata.TARGETS: true}
	for _, targetPath := range targetPaths {
		stack := []generationDelegationVisit{{
			role:   metadata.TARGETS,
			parent: metadata.ROOT,
		}}
		visitedRoles := 0
		found := false

		for visitedRoles <= maxDelegations && len(stack) > 0 {
			visit := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if slices.Contains(visit.path, visit.role) {
				continue
			}

			role, ok := trusted.Targets[visit.role]
			if !ok || role == nil {
				return nil, nil, fmt.Errorf("%w: go-tuf no conserva el rol recorrido %q", ErrGenerationCapture, visit.role)
			}
			selected[visit.role] = true
			if visit.role != metadata.TARGETS {
				if visit.parent == "" || visit.parent == metadata.ROOT {
					return nil, nil, fmt.Errorf("%w: rol delegado %q sin padre targets", ErrGenerationCapture, visit.role)
				}
				if parentSets[visit.role] == nil {
					parentSets[visit.role] = make(map[string]bool)
				}
				parentSets[visit.role][visit.parent] = true
			}

			if target, ok := role.Signed.Targets[targetPath]; ok && target != nil {
				found = true
				break
			}
			visitedRoles++

			if role.Signed.Delegations == nil {
				continue
			}
			childRoles := make([]generationDelegationVisit, 0)
			childPath := append(append([]string(nil), visit.path...), visit.role)
			for _, delegated := range role.Signed.Delegations.GetRolesForTarget(targetPath) {
				childRoles = append(childRoles, generationDelegationVisit{
					role:   delegated.Name,
					parent: visit.role,
					path:   childPath,
				})
				if delegated.Terminating {
					stack = stack[:0]
					break
				}
			}
			slices.Reverse(childRoles)
			stack = append(stack, childRoles...)
		}
		if !found {
			return nil, nil, fmt.Errorf("%w: no se pudo reproducir el recorrido delegado del target %q", ErrGenerationCapture, targetPath)
		}
	}

	parents := make(map[string][]string, len(parentSets))
	for roleName, set := range parentSets {
		for parent := range set {
			parents[roleName] = append(parents[roleName], parent)
		}
		sort.Strings(parents[roleName])
	}
	return parents, selected, nil
}
