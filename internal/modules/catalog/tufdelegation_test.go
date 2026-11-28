package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/theupdateframework/go-tuf/v2/metadata"
)

func syntheticTargetsRole(paths []string, delegations []metadata.DelegatedRole) *metadata.Metadata[metadata.TargetsType] {
	role := metadata.Targets(time.Now().UTC().Add(time.Hour))
	for _, targetPath := range paths {
		role.Signed.Targets[targetPath] = &metadata.TargetFiles{}
	}
	if len(delegations) > 0 {
		role.Signed.Delegations = &metadata.Delegations{Roles: delegations}
	}
	return role
}

func syntheticRoleLoader(roles map[string]*metadata.Metadata[metadata.TargetsType]) targetRoleLoader {
	return func(roleName, parentRole string) (*metadata.Metadata[metadata.TargetsType], error) {
		role, ok := roles[roleName]
		if !ok {
			return nil, fmt.Errorf("metadata %q missing (parent %q)", roleName, parentRole)
		}
		return role, nil
	}
}

func TestResolveTargetPathMatchesOfficialGoTUFUpdater(t *testing.T) {
	fixture := makeDelegatedRepositoryFixture(t)
	server, _, _ := startDelegatedMetadataServer(t, fixture)
	client := newFixtureTUFClient(t, fixture, server.URL+"/metadata", 4)
	defer func() {
		if err := client.Close(); err != nil {
			t.Errorf("close TUF client: %v", err)
		}
	}()

	// The official updater is the reference implementation. Resolve the same
	// path through it first, then compare the shared walk over its authenticated
	// metadata set.
	officialTarget, err := client.updater.GetTargetInfo(fixture.targetPath)
	if err != nil {
		t.Fatalf("official go-tuf GetTargetInfo(): %v", err)
	}
	trusted := client.updater.GetTrustedMetadataSet()
	resolved, err := resolveTargetPath(
		fixture.targetPath,
		4,
		func(roleName, parentRole string) (*metadata.Metadata[metadata.TargetsType], error) {
			role, ok := trusted.Targets[roleName]
			if !ok || role == nil {
				return nil, fmt.Errorf("authenticated role %q unavailable from %q", roleName, parentRole)
			}
			return role, nil
		},
	)
	if err != nil {
		t.Fatalf("resolveTargetPath(): %v", err)
	}
	if !reflect.DeepEqual(resolved.target, officialTarget) {
		t.Fatalf("shared resolver target differs from official updater: got %#v, want %#v", resolved.target, officialTarget)
	}
	if resolved.role != "osquery" {
		t.Fatalf("resolved role = %q, want osquery", resolved.role)
	}
	if len(resolved.visited) != 2 ||
		resolved.visited[0].role != metadata.TARGETS ||
		resolved.visited[0].parent != metadata.ROOT ||
		resolved.visited[1].role != "osquery" ||
		resolved.visited[1].parent != metadata.TARGETS {
		t.Fatalf("unexpected canonical delegation walk: %#v", resolved.visited)
	}

	parents, selected, err := collectResolvedDelegations(trusted, []string{fixture.targetPath}, 4)
	if err != nil {
		t.Fatalf("collectResolvedDelegations(): %v", err)
	}
	if !selected[metadata.TARGETS] || !selected["osquery"] ||
		!reflect.DeepEqual(parents["osquery"], []string{metadata.TARGETS}) {
		t.Fatalf("generation capture did not retain the exact authenticated delegation path: selected=%#v parents=%#v", selected, parents)
	}
}

func TestResolveTargetPathUsesGoTUFPathHashPrefixesAndSignedOrder(t *testing.T) {
	targetPath := "tools/osquery/5.24.1/osqueryi"
	digest := sha256.Sum256([]byte(targetPath))
	prefix := hex.EncodeToString(digest[:])[:16]

	roles := map[string]*metadata.Metadata[metadata.TargetsType]{
		metadata.TARGETS: syntheticTargetsRole(nil, []metadata.DelegatedRole{
			{Name: "wrong-prefix", Terminating: true, PathHashPrefixes: []string{"0000000000000000"}},
			{Name: "hash-bin", Terminating: true, PathHashPrefixes: []string{prefix}},
			{Name: "later-glob", Terminating: true, Paths: []string{"tools/*/*/*"}},
		}),
		"hash-bin": syntheticTargetsRole([]string{targetPath}, nil),
		"later-glob": syntheticTargetsRole([]string{targetPath}, nil),
	}

	resolved, err := resolveTargetPath(targetPath, 8, syntheticRoleLoader(roles))
	if err != nil {
		t.Fatalf("resolveTargetPath() error = %v", err)
	}
	if resolved.role != "hash-bin" {
		t.Fatalf("resolved role = %q, want hash-bin (signed priority order)", resolved.role)
	}
	if len(resolved.visited) != 2 || resolved.visited[1].role != "hash-bin" {
		t.Fatalf("terminating hash-bin should stop later candidates: %#v", resolved.visited)
	}
}

func TestResolveTargetPathHonorsTerminatingDelegation(t *testing.T) {
	targetPath := "packages/tool/1.0/tool"
	roles := map[string]*metadata.Metadata[metadata.TargetsType]{
		metadata.TARGETS: syntheticTargetsRole(nil, []metadata.DelegatedRole{
			{Name: "first", Terminating: true, Paths: []string{"packages/*/*/*"}},
			{Name: "second", Terminating: true, Paths: []string{"packages/*/*/*"}},
		}),
		"first":  syntheticTargetsRole(nil, nil),
		"second": syntheticTargetsRole([]string{targetPath}, nil),
	}

	_, err := resolveTargetPath(targetPath, 8, syntheticRoleLoader(roles))
	if !errors.Is(err, ErrTargetPathNotFound) {
		t.Fatalf("resolveTargetPath() error = %v, want terminating role to prevent fallback", err)
	}
}

func TestResolveTargetPathFailsClosedWhenCandidateMetadataIsMissing(t *testing.T) {
	targetPath := "packages/tool/1.0/tool"
	roles := map[string]*metadata.Metadata[metadata.TargetsType]{
		metadata.TARGETS: syntheticTargetsRole(nil, []metadata.DelegatedRole{
			{Name: "missing", Paths: []string{"packages/*/*/*"}},
			{Name: "present", Terminating: true, Paths: []string{"packages/*/*/*"}},
		}),
		"present": syntheticTargetsRole([]string{targetPath}, nil),
	}

	_, err := resolveTargetPath(targetPath, 8, syntheticRoleLoader(roles))
	if !errors.Is(err, ErrTargetDelegationUnavailable) {
		t.Fatalf("resolveTargetPath() error = %v, want missing candidate metadata to fail closed", err)
	}
}

func TestResolveTargetPathSkipsOnlyCyclesOnCurrentDelegationPath(t *testing.T) {
	targetPath := "packages/tool/1.0/tool"
	roles := map[string]*metadata.Metadata[metadata.TargetsType]{
		metadata.TARGETS: syntheticTargetsRole(nil, []metadata.DelegatedRole{
			{Name: "child", Paths: []string{"packages/*/*/*"}},
		}),
		"child": syntheticTargetsRole(nil, []metadata.DelegatedRole{
			{Name: metadata.TARGETS, Terminating: true, Paths: []string{"packages/*/*/*"}},
		}),
	}

	_, err := resolveTargetPath(targetPath, 8, syntheticRoleLoader(roles))
	if !errors.Is(err, ErrTargetPathNotFound) {
		t.Fatalf("resolveTargetPath() error = %v, want cycle to terminate as target-not-found", err)
	}
}

func TestResolveTargetPathEnforcesDelegationLimit(t *testing.T) {
	targetPath := "packages/tool/1.0/tool"
	roles := map[string]*metadata.Metadata[metadata.TargetsType]{
		metadata.TARGETS: syntheticTargetsRole(nil, []metadata.DelegatedRole{
			{Name: "first", Paths: []string{"packages/*/*/*"}},
		}),
		"first": syntheticTargetsRole(nil, []metadata.DelegatedRole{
			{Name: "second", Paths: []string{"packages/*/*/*"}},
		}),
		"second": syntheticTargetsRole(nil, []metadata.DelegatedRole{
			{Name: "third", Paths: []string{"packages/*/*/*"}},
		}),
		"third": syntheticTargetsRole([]string{targetPath}, nil),
	}

	_, err := resolveTargetPath(targetPath, 1, syntheticRoleLoader(roles))
	if !errors.Is(err, ErrTargetDelegationLimitExceeded) {
		t.Fatalf("resolveTargetPath() error = %v, want delegation limit error", err)
	}
}

func TestResolveTargetPathUsesDepthFirstPreOrder(t *testing.T) {
	targetPath := "packages/tool/1.0/tool"
	roles := map[string]*metadata.Metadata[metadata.TargetsType]{
		metadata.TARGETS: syntheticTargetsRole(nil, []metadata.DelegatedRole{
			{Name: "first", Paths: []string{"packages/*/*/*"}},
			{Name: "second", Terminating: true, Paths: []string{"packages/*/*/*"}},
		}),
		"first": syntheticTargetsRole(nil, []metadata.DelegatedRole{
			{Name: "nested", Terminating: true, Paths: []string{"packages/*/*/*"}},
		}),
		"nested": syntheticTargetsRole([]string{targetPath}, nil),
		"second": syntheticTargetsRole([]string{targetPath}, nil),
	}

	resolved, err := resolveTargetPath(targetPath, 8, syntheticRoleLoader(roles))
	if err != nil {
		t.Fatalf("resolveTargetPath() error = %v", err)
	}
	if resolved.role != "nested" {
		t.Fatalf("resolved role = %q, want nested role before later sibling", resolved.role)
	}
	want := []string{metadata.TARGETS, "first", "nested"}
	got := make([]string, 0, len(resolved.visited))
	for _, visit := range resolved.visited {
		got = append(got, visit.role)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pre-order walk = %#v, want %#v", got, want)
	}
}

func TestResolveTargetPathRevisitsRoleForDifferentParents(t *testing.T) {
	targetPath := "packages/tool/1.0/tool"
	roles := map[string]*metadata.Metadata[metadata.TargetsType]{
		metadata.TARGETS: syntheticTargetsRole(nil, []metadata.DelegatedRole{
			{Name: "left", Paths: []string{"packages/*/*/*"}},
			{Name: "right", Paths: []string{"packages/*/*/*"}},
		}),
		"left": syntheticTargetsRole(nil, []metadata.DelegatedRole{
			{Name: "shared", Terminating: true, Paths: []string{"packages/*/*/*"}},
		}),
		"right": syntheticTargetsRole(nil, []metadata.DelegatedRole{
			{Name: "shared", Terminating: true, Paths: []string{"packages/*/*/*"}},
		}),
		"shared": syntheticTargetsRole(nil, nil),
	}
	var sharedParents []string
	loader := func(roleName, parentRole string) (*metadata.Metadata[metadata.TargetsType], error) {
		if roleName == "shared" {
			sharedParents = append(sharedParents, parentRole)
		}
		role, ok := roles[roleName]
		if !ok {
			return nil, fmt.Errorf("metadata %q missing (parent %q)", roleName, parentRole)
		}
		return role, nil
	}

	_, err := resolveTargetPath(targetPath, 8, loader)
	if !errors.Is(err, ErrTargetPathNotFound) {
		t.Fatalf("resolveTargetPath() error = %v, want target-not-found after both paths", err)
	}
	if !reflect.DeepEqual(sharedParents, []string{"left", "right"}) {
		t.Fatalf("shared role parent visits = %#v, want left then right", sharedParents)
	}
}

func TestResolveTargetPathSupportsSuccinctRoles(t *testing.T) {
	targetPath := "packages/tool/1.0/tool"
	digest := sha256.Sum256([]byte(targetPath))
	bitLength := uint(5)
	bin := uint32(digest[0])<<24 | uint32(digest[1])<<16 | uint32(digest[2])<<8 | uint32(digest[3])
	bin >>= 32 - bitLength
	roleName := fmt.Sprintf("bin-%x", bin)

	top := syntheticTargetsRole(nil, nil)
	top.Signed.Delegations = &metadata.Delegations{
		SuccinctRoles: &metadata.SuccinctRoles{
			BitLength:  int(bitLength),
			NamePrefix: "bin",
		},
	}
	roles := map[string]*metadata.Metadata[metadata.TargetsType]{
		metadata.TARGETS: top,
		roleName:         syntheticTargetsRole([]string{targetPath}, nil),
	}

	resolved, err := resolveTargetPath(targetPath, 8, syntheticRoleLoader(roles))
	if err != nil {
		t.Fatalf("resolveTargetPath() error = %v", err)
	}
	if resolved.role != roleName {
		t.Fatalf("resolved role = %q, want succinct bin %q", resolved.role, roleName)
	}
}
