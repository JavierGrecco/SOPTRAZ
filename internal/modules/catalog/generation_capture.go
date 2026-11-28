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
	"sort"
	"time"

	"github.com/JavierGrecco/SOPTRAZ/internal/storage/sqlitetuf"
	"github.com/theupdateframework/go-tuf/v2/metadata"
	tuftrusted "github.com/theupdateframework/go-tuf/v2/metadata/trustedmetadata"
	tufupdater "github.com/theupdateframework/go-tuf/v2/metadata/updater"
)

type generationDelegationVisit struct {
	role   string
	parent string
	path   []string
}

// persistGenerationLocked stores the exact role metadata currently accepted
// by the official updater. It must be called while TUFClient.mu is held.
func (client *TUFClient) persistGenerationLocked(officialUpdater *tufupdater.Updater) error {
	persistence := client.persistence
	if persistence == nil {
		return nil
	}
	if officialUpdater == nil || persistence.store == nil || persistence.repositoryID == "" {
		return ErrTUFClientUnavailable
	}

	trusted := officialUpdater.GetTrustedMetadataSet()
	if trusted.Root == nil || trusted.Timestamp == nil || trusted.Snapshot == nil || trusted.Targets[metadata.TARGETS] == nil {
		return fmt.Errorf("%w: go-tuf no tiene todos los roles superiores cargados", ErrTUFClientUnavailable)
	}
	if persistence.rootState.Version < 1 || len(persistence.latestRootJSON) == 0 {
		return fmt.Errorf("%w: falta la raíz persistente vigente", ErrTUFClientUnavailable)
	}
	rootDigest := sha256.Sum256(persistence.latestRootJSON)
	if hex.EncodeToString(rootDigest[:]) != persistence.rootState.SHA256 {
		return fmt.Errorf("%w: los bytes del root persistente no coinciden con su high-water", ErrTUFClientUnavailable)
	}

	rootDocument := metadata.Root()
	if _, err := rootDocument.FromBytes(persistence.latestRootJSON); err != nil {
		return fmt.Errorf("parsear root vigente para generación: %w", err)
	}
	if rootDocument.Signed.Version != persistence.rootState.Version {
		return fmt.Errorf("%w: root persistente v%d y documento v%d",
			ErrTUFClientUnavailable, persistence.rootState.Version, rootDocument.Signed.Version)
	}
	rootBytes, err := rootDocument.ToBytes(false)
	if err != nil {
		return fmt.Errorf("serializar root persistente: %w", err)
	}
	trustedRootBytes, err := trusted.Root.ToBytes(false)
	if err != nil {
		return fmt.Errorf("serializar root aceptado por go-tuf: %w", err)
	}
	if !bytes.Equal(rootBytes, trustedRootBytes) || trusted.Root.Signed.Version != persistence.rootState.Version {
		return fmt.Errorf("%w: root aceptado por go-tuf no coincide con la cadena persistente", ErrTUFClientUnavailable)
	}

	timestampRaw, err := readCachedRoleMetadata(client.config.LocalMetadataDir, metadata.TIMESTAMP, sqlitetuf.MaxTimestampMetadataSize)
	if err != nil {
		return fmt.Errorf("leer bytes exactos de timestamp.json: %w", err)
	}
	snapshotRaw, err := readCachedRoleMetadata(client.config.LocalMetadataDir, metadata.SNAPSHOT, sqlitetuf.MaxSnapshotMetadataSize)
	if err != nil {
		return fmt.Errorf("leer bytes exactos de snapshot.json: %w", err)
	}
	timestampDocument := metadata.Timestamp()
	if _, err := timestampDocument.FromBytes(timestampRaw); err != nil {
		return fmt.Errorf("parsear timestamp.json persistido por go-tuf: %w", err)
	}
	snapshotDocument := metadata.Snapshot()
	if _, err := snapshotDocument.FromBytes(snapshotRaw); err != nil {
		return fmt.Errorf("parsear snapshot.json persistido por go-tuf: %w", err)
	}
	if timestampDocument.Signed.Type != metadata.TIMESTAMP ||
		snapshotDocument.Signed.Type != metadata.SNAPSHOT ||
		timestampDocument.Signed.Version != trusted.Timestamp.Signed.Version ||
		snapshotDocument.Signed.Version != trusted.Snapshot.Signed.Version {
		return fmt.Errorf("%w: las versiones de timestamp/snapshot en caché no coinciden con go-tuf", ErrTUFClientUnavailable)
	}
	timestampCanonical, err := timestampDocument.ToBytes(false)
	if err != nil {
		return fmt.Errorf("serializar timestamp.json: %w", err)
	}
	trustedTimestampCanonical, err := trusted.Timestamp.ToBytes(false)
	if err != nil {
		return fmt.Errorf("serializar timestamp confiable: %w", err)
	}
	snapshotCanonical, err := snapshotDocument.ToBytes(false)
	if err != nil {
		return fmt.Errorf("serializar snapshot.json: %w", err)
	}
	trustedSnapshotCanonical, err := trusted.Snapshot.ToBytes(false)
	if err != nil {
		return fmt.Errorf("serializar snapshot confiable: %w", err)
	}
	if !bytes.Equal(timestampCanonical, trustedTimestampCanonical) ||
		!bytes.Equal(snapshotCanonical, trustedSnapshotCanonical) {
		return fmt.Errorf("%w: los bytes de timestamp/snapshot en caché no representan el estado aceptado por go-tuf", ErrTUFClientUnavailable)
	}

	targetPaths := make([]string, 0, len(client.resolvedTargets))
	for targetPath := range client.resolvedTargets {
		targetPaths = append(targetPaths, targetPath)
	}
	sort.Strings(targetPaths)
	parentsByRole, includedRoles, err := collectGenerationDelegationGraph(
		trusted,
		targetPaths,
		client.maxDelegations,
	)
	if err != nil {
		return err
	}

	roleNames := make([]string, 0, len(includedRoles))
	for roleName := range includedRoles {
		roleNames = append(roleNames, roleName)
	}
	sort.Strings(roleNames)

	roleBytes := make(map[string][]byte, len(roleNames))
	roleDocuments := make(map[string]*metadata.Metadata[metadata.TargetsType], len(roleNames))
	for _, roleName := range roleNames {
		raw, err := readCachedRoleMetadata(client.config.LocalMetadataDir, roleName, sqlitetuf.MaxTargetsMetadataSize)
		if err != nil {
			return fmt.Errorf("leer bytes exactos del rol %q: %w", roleName, err)
		}
		document := metadata.Targets()
		if _, err := document.FromBytes(raw); err != nil {
			return fmt.Errorf("parsear metadata del rol %q: %w", roleName, err)
		}
		trustedDocument := trusted.Targets[roleName]
		if trustedDocument == nil {
			return fmt.Errorf("%w: el rol %q no está cargado en go-tuf", ErrTUFClientUnavailable, roleName)
		}
		if document.Signed.Type != metadata.TARGETS ||
			document.Signed.Version != trustedDocument.Signed.Version {
			return fmt.Errorf("%w: tipo/versión del rol %q no coincide con go-tuf", ErrTUFClientUnavailable, roleName)
		}
		cachedCanonical, err := document.ToBytes(false)
		if err != nil {
			return fmt.Errorf("serializar metadata del rol %q: %w", roleName, err)
		}
		trustedCanonical, err := trustedDocument.ToBytes(false)
		if err != nil {
			return fmt.Errorf("serializar metadata confiable del rol %q: %w", roleName, err)
		}
		if !bytes.Equal(cachedCanonical, trustedCanonical) {
			return fmt.Errorf("%w: la caché del rol %q no coincide con el documento aceptado por go-tuf", ErrTUFClientUnavailable, roleName)
		}
		roleBytes[roleName] = raw
		roleDocuments[roleName] = document
	}

	replayed, err := tuftrusted.New(persistence.latestRootJSON)
	if err != nil {
		return fmt.Errorf("crear replay de metadata TUF para generación: %w", err)
	}
	if _, err := replayed.UpdateTimestamp(timestampRaw); err != nil {
		return fmt.Errorf("revalidar timestamp con go-tuf: %w", err)
	}
	if _, err := replayed.UpdateSnapshot(snapshotRaw, false); err != nil {
		return fmt.Errorf("revalidar snapshot contra timestamp: %w", err)
	}
	if _, err := replayed.UpdateDelegatedTargets(roleBytes[metadata.TARGETS], metadata.TARGETS, metadata.ROOT); err != nil {
		return fmt.Errorf("revalidar targets contra snapshot y root: %w", err)
	}

	delegatedNames := make([]string, 0, len(roleNames)-1)
	for _, roleName := range roleNames {
		if roleName != metadata.TARGETS {
			delegatedNames = append(delegatedNames, roleName)
		}
	}
	verifiedRoles := map[string]struct{}{metadata.TARGETS: {}}
	remaining := append([]string(nil), delegatedNames...)
	for len(remaining) > 0 {
		progress := false
		next := make([]string, 0, len(remaining))
		for _, roleName := range remaining {
			parents := append([]string(nil), parentsByRole[roleName]...)
			sort.Strings(parents)
			if len(parents) == 0 {
				return fmt.Errorf("%w: el rol delegado %q no tiene una ruta de confianza observada", ErrTUFClientUnavailable, roleName)
			}
			ready := true
			for _, parent := range parents {
				if _, ok := verifiedRoles[parent]; !ok {
					ready = false
					break
				}
			}
			if !ready {
				next = append(next, roleName)
				continue
			}
			for _, parent := range parents {
				if _, err := replayed.UpdateDelegatedTargets(roleBytes[roleName], roleName, parent); err != nil {
					return fmt.Errorf("revalidar delegación %q desde %q: %w", roleName, parent, err)
				}
			}
			verifiedRoles[roleName] = struct{}{}
			progress = true
		}
		if !progress {
			return fmt.Errorf("%w: el grafo de delegaciones no puede reproducirse desde targets", ErrTUFClientUnavailable)
		}
		remaining = next
	}

	if len(replayed.Targets) != len(includedRoles) {
		return fmt.Errorf("%w: el replay contiene %d roles y se esperaban %d",
			ErrTUFClientUnavailable, len(replayed.Targets), len(includedRoles))
	}
	for roleName := range includedRoles {
		replayedDocument := replayed.Targets[roleName]
		trustedDocument := trusted.Targets[roleName]
		if replayedDocument == nil || trustedDocument == nil {
			return fmt.Errorf("%w: falta el rol %q después del replay", ErrTUFClientUnavailable, roleName)
		}
		replayedCanonical, err := replayedDocument.ToBytes(false)
		if err != nil {
			return fmt.Errorf("serializar rol %q revalidado: %w", roleName, err)
		}
		trustedCanonical, err := trustedDocument.ToBytes(false)
		if err != nil {
			return fmt.Errorf("serializar rol %q de go-tuf: %w", roleName, err)
		}
		if !bytes.Equal(replayedCanonical, trustedCanonical) {
			return fmt.Errorf("%w: el replay del rol %q no coincide con go-tuf", ErrTUFClientUnavailable, roleName)
		}
	}

	verifiedRolesForStore := make([]sqlitetuf.VerifiedRoleMetadata, 0, len(roleNames)+2)
	verifiedRolesForStore = append(verifiedRolesForStore,
		sqlitetuf.VerifiedRoleMetadata{
			Role:      metadata.TIMESTAMP,
			Version:   timestampDocument.Signed.Version,
			ExpiresAt: timestampDocument.Signed.Expires,
			Metadata:  timestampRaw,
		},
		sqlitetuf.VerifiedRoleMetadata{
			Role:      metadata.SNAPSHOT,
			Version:   snapshotDocument.Signed.Version,
			ExpiresAt: snapshotDocument.Signed.Expires,
			Metadata:  snapshotRaw,
		},
	)
	for _, roleName := range roleNames {
		document := roleDocuments[roleName]
		verifiedRolesForStore = append(verifiedRolesForStore, sqlitetuf.VerifiedRoleMetadata{
			Role:      roleName,
			Parents:   append([]string(nil), parentsByRole[roleName]...),
			Version:   document.Signed.Version,
			ExpiresAt: document.Signed.Expires,
			Metadata:  roleBytes[roleName],
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	activeID, err := persistence.store.ActiveGenerationID(ctx, persistence.repositoryID)
	if err != nil {
		return fmt.Errorf("leer generación TUF activa: %w", err)
	}
	if activeID > 0 {
		active, loadErr := persistence.store.LoadActiveGeneration(ctx, persistence.repositoryID)
		if loadErr == nil && generationContentMatches(active, persistence.rootState, verifiedRolesForStore) {
			return nil
		}
		if loadErr != nil &&
			!errors.Is(loadErr, sqlitetuf.ErrGenerationRootMismatch) &&
			!errors.Is(loadErr, sqlitetuf.ErrGenerationNotFound) {
			return fmt.Errorf("validar generación TUF activa anterior: %w", loadErr)
		}
	}
	committed, err := persistence.store.CommitGeneration(
		ctx,
		persistence.repositoryID,
		activeID,
		persistence.rootState,
		verifiedRolesForStore,
	)
	if err != nil {
		return fmt.Errorf("comprometer generación TUF verificada: %w", err)
	}
	if committed.ID == 0 || committed.Root != persistence.rootState {
		return fmt.Errorf("%w: SQLite devolvió una generación activa inconsistente", ErrTUFClientUnavailable)
	}
	return nil
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
		if !ok || stored.Version != role.Version || !stored.ExpiresAt.Equal(role.ExpiresAt) ||
			!bytes.Equal(stored.Metadata, role.Metadata) || !sameRoleParents(stored.Parents, role.Parents) {
			return false
		}
	}
	return true
}

func sameRoleParents(left, right []string) bool {
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

func collectGenerationDelegationGraph(
	trusted tuftrusted.TrustedMetadata,
	targetPaths []string,
	maxDelegations int,
) (map[string][]string, map[string]struct{}, error) {
	if trusted.Targets[metadata.TARGETS] == nil {
		return nil, nil, fmt.Errorf("%w: falta el rol targets", ErrTUFClientUnavailable)
	}
	parentSets := make(map[string]map[string]struct{})
	included := map[string]struct{}{metadata.TARGETS: {}}
	for _, targetPath := range targetPaths {
		stack := []generationDelegationVisit{{role: metadata.TARGETS, parent: metadata.ROOT}}
		visited := 0
		found := false
		for visited <= maxDelegations && len(stack) > 0 {
			last := len(stack) - 1
			visit := stack[last]
			stack = stack[:last]
			if containsRole(visit.path, visit.role) {
				continue
			}
			roleDocument := trusted.Targets[visit.role]
			if roleDocument == nil {
				return nil, nil, fmt.Errorf("%w: go-tuf no conserva metadata del rol %q visitado para %q",
					ErrTUFClientUnavailable, visit.role, targetPath)
			}
			included[visit.role] = struct{}{}
			if visit.role != metadata.TARGETS {
				if parentSets[visit.role] == nil {
					parentSets[visit.role] = make(map[string]struct{})
				}
				parentSets[visit.role][visit.parent] = struct{}{}
			}
			if target := roleDocument.Signed.Targets[targetPath]; target != nil {
				found = true
				break
			}
			visited++
			if roleDocument.Signed.Delegations == nil {
				continue
			}
			childRoles := roleDocument.Signed.Delegations.GetRolesForTarget(targetPath)
			childPath := append(append([]string(nil), visit.path...), visit.role)
			children := make([]generationDelegationVisit, 0, len(childRoles))
			terminating := false
			for _, child := range childRoles {
				children = append(children, generationDelegationVisit{
					role:   child.Name,
					parent: visit.role,
					path:   childPath,
				})
				if child.Terminating {
					terminating = true
					break
				}
			}
			if terminating {
				stack = nil
			}
			for index := len(children) - 1; index >= 0; index-- {
				stack = append(stack, children[index])
			}
		}
		if !found {
			return nil, nil, fmt.Errorf("%w: el updater resolvió %q pero el grafo autenticado no reproduce esa resolución",
				ErrTUFClientUnavailable, targetPath)
		}
	}

	parents := make(map[string][]string, len(parentSets))
	for roleName, parentSet := range parentSets {
		roleParents := make([]string, 0, len(parentSet))
		for parent := range parentSet {
			roleParents = append(roleParents, parent)
		}
		sort.Strings(roleParents)
		parents[roleName] = roleParents
	}
	return parents, included, nil
}

func containsRole(roles []string, role string) bool {
	for _, candidate := range roles {
		if candidate == role {
			return true
		}
	}
	return false
}

func readCachedRoleMetadata(cacheDir, roleName string, maxBytes int64) ([]byte, error) {
	if cacheDir == "" || roleName == "" || maxBytes < 1 {
		return nil, fmt.Errorf("ruta de caché, rol o límite inválido")
	}
	filePath := filepath.Join(cacheDir, url.PathEscape(roleName)+".json")
	info, err := os.Lstat(filePath)
	if err != nil {
		return nil, fmt.Errorf("metadata cache %q: %w", roleName, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("metadata cache %q no es un archivo regular", roleName)
	}
	if info.Size() < 1 || info.Size() > maxBytes {
		return nil, fmt.Errorf("metadata cache %q tiene %d bytes; límite %d", roleName, info.Size(), maxBytes)
	}
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("abrir metadata cache %q: %w", roleName, err)
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("leer metadata cache %q: %w", roleName, err)
	}
	if len(raw) == 0 || int64(len(raw)) > maxBytes {
		return nil, fmt.Errorf("metadata cache %q excede el límite", roleName)
	}
	return raw, nil
}
