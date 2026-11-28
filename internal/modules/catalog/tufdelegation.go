package catalog

import (
	"errors"
	"fmt"
	"slices"

	"github.com/theupdateframework/go-tuf/v2/metadata"
)

var (
	ErrTargetPathInvalid             = errors.New("ruta de target inválida")
	ErrTargetDelegationUnavailable   = errors.New("metadata de delegación requerida no disponible")
	ErrTargetDelegationLimitExceeded = errors.New("límite de delegaciones TUF alcanzado")
	ErrTargetPathNotFound            = errors.New("target no encontrado en metadata TUF")
)

type targetRoleLoader func(roleName, parentRole string) (*metadata.Metadata[metadata.TargetsType], error)

type generationDelegationVisit struct {
	role   string
	parent string
	path   []string
}

type targetPathResolution struct {
	target  *metadata.TargetFiles
	role    string
	visited []generationDelegationVisit
}

// resolveTargetPath walks already authenticated targets metadata using the
// same pre-order depth-first rules as go-tuf v2.5.0 Updater.GetTargetInfo.
// The loader is responsible for authenticating each (role, parent) pair before
// returning metadata. Delegation path matching and ordering remain owned by
// go-tuf's GetRolesForTarget implementation, including paths, path hash
// prefixes, and succinct roles. This function performs no I/O.
func resolveTargetPath(
	targetPath string,
	maxDelegations int,
	load targetRoleLoader,
) (targetPathResolution, error) {
	if targetPath == "" {
		return targetPathResolution{}, ErrTargetPathInvalid
	}
	if maxDelegations < 1 || load == nil {
		return targetPathResolution{}, fmt.Errorf("%w: loader o límite de delegaciones inválido", ErrTargetPathInvalid)
	}

	pending := []generationDelegationVisit{{
		role:   metadata.TARGETS,
		parent: metadata.ROOT,
	}}
	visitedRoles := 0
	visited := make([]generationDelegationVisit, 0, maxDelegations+1)

	for visitedRoles <= maxDelegations && len(pending) > 0 {
		visit := pending[len(pending)-1]
		pending = pending[:len(pending)-1]

		// TUF only skips a role already present on this delegation path. The
		// same role reached through a different parent path must be verified
		// again for that parent, exactly as go-tuf does.
		if slices.Contains(visit.path, visit.role) {
			continue
		}

		role, err := load(visit.role, visit.parent)
		if err != nil {
			return targetPathResolution{}, fmt.Errorf(
				"%w: rol %q delegado por %q: %w",
				ErrTargetDelegationUnavailable, visit.role, visit.parent, err,
			)
		}
		if role == nil {
			return targetPathResolution{}, fmt.Errorf(
				"%w: el loader devolvió metadata nula para %q",
				ErrTargetDelegationUnavailable, visit.role,
			)
		}
		visited = append(visited, visit)

		if target, ok := role.Signed.Targets[targetPath]; ok && target != nil {
			return targetPathResolution{
				target:  target,
				role:    visit.role,
				visited: visited,
			}, nil
		}

		// Match go-tuf's MaxDelegations accounting: count each role that was
		// checked and did not contain the requested target. A role skipped
		// because it is already on the current path is not counted.
		visitedRoles++

		if role.Signed.Delegations == nil {
			continue
		}

		children := make([]generationDelegationVisit, 0)
		childPath := append(append([]string(nil), visit.path...), visit.role)
		for _, delegated := range role.Signed.Delegations.GetRolesForTarget(targetPath) {
			children = append(children, generationDelegationVisit{
				role:   delegated.Name,
				parent: visit.role,
				path:   childPath,
			})
			if delegated.Terminating {
				// A terminating role stops backtracking to candidates already
				// queued by an earlier delegation in this parent role.
				pending = pending[:0]
				break
			}
		}
		slices.Reverse(children)
		pending = append(pending, children...)
	}

	if len(pending) > 0 {
		return targetPathResolution{}, fmt.Errorf(
			"%w: quedaron %d roles por visitar (máximo configurado: %d)",
			ErrTargetDelegationLimitExceeded, len(pending), maxDelegations,
		)
	}
	return targetPathResolution{}, fmt.Errorf("%w: %q", ErrTargetPathNotFound, targetPath)
}
