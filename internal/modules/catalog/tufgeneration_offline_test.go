package catalog

import (
	"context"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JavierGrecco/SOPTRAZ/internal/storage/sqlitetuf"
	"github.com/theupdateframework/go-tuf/v2/metadata"
)

type persistedDelegatedFixture struct {
	repository delegatedRepositoryFixture
	server     *httptest.Server
	store      *sqlitetuf.Store
	anchor     *TrustAnchor
	client     *TUFClient
	requests   *[]string
	requestsMu *sync.Mutex
}

func createPersistedDelegatedFixture(t *testing.T) persistedDelegatedFixture {
	return createPersistedDelegatedFixtureVariant(t, false, false)
}

func createPersistedDelegatedFixtureWithRootRotation(t *testing.T) persistedDelegatedFixture {
	return createPersistedDelegatedFixtureVariant(t, true, false)
}

func createPersistedDelegatedFixtureWithLongerRootExpiry(t *testing.T) persistedDelegatedFixture {
	return createPersistedDelegatedFixtureVariant(t, false, true)
}

func createPersistedDelegatedFixtureVariant(t *testing.T, rotateRoot, extendRootExpiry bool) persistedDelegatedFixture {
	t.Helper()
	fixture := makeDelegatedRepositoryFixture(t)
	if extendRootExpiry {
		root, err := metadata.Root().FromBytes(fixture.rootJSON)
		if err != nil {
			t.Fatalf("parse fixture root for longer expiry: %v", err)
		}
		root.Signed.Expires = root.Signed.Expires.Add(24 * time.Hour)
		fixture.rootJSON = signedMetadataBytes(t, root, fixture.rootSigner)
	}
	if rotateRoot {
		fixture.metadata["2.root.json"] = makeRotatedRootMetadata(t, fixture, true)
	}
	server, requests, requestsMu := startDelegatedMetadataServer(t, fixture)
	store, err := sqlitetuf.Open(context.Background(), filepath.Join(t.TempDir(), "offline-tuf.sqlite"))
	if err != nil {
		t.Fatalf("open offline trust store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close offline trust store: %v", err)
		}
	})
	anchor, err := NewPinnedTrustAnchor(fixture.rootJSON, rootPin(fixture.rootJSON))
	if err != nil {
		t.Fatalf("create independently pinned bootstrap: %v", err)
	}
	client, err := NewPersistentTUFClient(
		anchor,
		persistentClientConfig(server.URL+"/metadata", t.TempDir()),
		store,
		"offline-test-repository",
	)
	if err != nil {
		t.Fatalf("create persistent online client: %v", err)
	}
	if _, err := client.ResolveDescriptor(fixture.descriptor); err != nil {
		t.Fatalf("persist authenticated delegated target generation: %v", err)
	}
	return persistedDelegatedFixture{
		repository: fixture,
		server:     server,
		store:      store,
		anchor:     anchor,
		client:     client,
		requests:   requests,
		requestsMu: requestsMu,
	}
}

func loadPersistedFixtureSnapshot(t *testing.T, fixture persistedDelegatedFixture) (sqlitetuf.OfflineSnapshot, []byte, TrustState) {
	t.Helper()
	snapshot, err := fixture.store.LoadOfflineSnapshot(context.Background(), "offline-test-repository")
	if err != nil {
		t.Fatalf("LoadOfflineSnapshot(): %v", err)
	}
	rootJSON, rootState, err := fixture.anchor.initialRootSnapshot()
	if err != nil {
		t.Fatalf("read independent bootstrap: %v", err)
	}
	return snapshot, rootJSON, rootState
}

func TestReplayOfflineTargetMetadataMatchesGoTUFWithoutNetwork(t *testing.T) {
	fixture := createPersistedDelegatedFixture(t)
	snapshot, rootJSON, rootState := loadPersistedFixtureSnapshot(t, fixture)

	officialTarget, err := fixture.client.updater.GetTargetInfo(fixture.repository.targetPath)
	if err != nil {
		t.Fatalf("official updater GetTargetInfo(): %v", err)
	}

	// The database relation table is not authority. Even a forged relationship
	// must not change the signed path that is independently reconstructed.
	foundDelegatedRole := false
	for index := range snapshot.Generation.Roles {
		if snapshot.Generation.Roles[index].Role == "osquery" {
			snapshot.Generation.Roles[index].Parents = []string{"attacker-controlled-parent"}
			foundDelegatedRole = true
		}
	}
	if !foundDelegatedRole {
		t.Fatal("persisted fixture did not include osquery delegated metadata")
	}

	// Close the only metadata server before replay. A successful result proves
	// this path does not fall back to HTTP or touch the network.
	fixture.server.Close()
	fixture.requestsMu.Lock()
	requestCountBefore := len(*fixture.requests)
	fixture.requestsMu.Unlock()

	result, err := replayOfflineTargetMetadata(
		snapshot,
		rootJSON,
		rootState,
		time.Now().UTC(),
		fixture.repository.targetPath,
		4,
	)
	if err != nil {
		t.Fatalf("replayOfflineTargetMetadata(): %v", err)
	}
	if result.resolution.role != "osquery" ||
		result.resolution.target == nil ||
		result.resolution.target.Path != fixture.repository.targetPath {
		t.Fatalf("unexpected offline target resolution: %#v", result.resolution)
	}
	if !reflect.DeepEqual(result.resolution.target, officialTarget) {
		t.Fatalf("offline target differs from official updater: got %#v, want %#v",
			result.resolution.target, officialTarget)
	}
	if _, err := verifySignedArtifactTarget(
		fixture.repository.targetPath,
		result.resolution.target,
		fixture.repository.descriptor,
	); err != nil {
		t.Fatalf("verify artifact v1 binding from offline target metadata: %v", err)
	}

	fixture.requestsMu.Lock()
	requestCountAfter := len(*fixture.requests)
	fixture.requestsMu.Unlock()
	if requestCountAfter != requestCountBefore {
		t.Fatalf("offline replay attempted network activity: requests before=%d after=%d", requestCountBefore, requestCountAfter)
	}
}

func TestReplayOfflineTargetMetadataReplaysRootRotationFromIndependentBootstrap(t *testing.T) {
	fixture := createPersistedDelegatedFixtureWithRootRotation(t)
	snapshot, bootstrapRootJSON, bootstrapState := loadPersistedFixtureSnapshot(t, fixture)

	if bootstrapState.Version != 1 {
		t.Fatalf("independent bootstrap version = %d, want 1", bootstrapState.Version)
	}
	if snapshot.Repository.AnchorVersion != 1 {
		t.Fatalf("persisted anchor version = %d, want original bootstrap v1", snapshot.Repository.AnchorVersion)
	}
	if snapshot.LatestRoot.Version != 2 || snapshot.Generation.Root.Version != 2 {
		t.Fatalf("snapshot latest/generation root = %#v/%#v, want v2",
			snapshot.LatestRoot, snapshot.Generation.Root)
	}
	if len(snapshot.RootChain) != 1 || snapshot.RootChain[0].Version != 2 {
		t.Fatalf("persisted root chain = %#v, want one consecutive v2 rotation", snapshot.RootChain)
	}

	officialTarget, err := fixture.client.updater.GetTargetInfo(fixture.repository.targetPath)
	if err != nil {
		t.Fatalf("official updater GetTargetInfo(): %v", err)
	}

	fixture.server.Close()
	fixture.requestsMu.Lock()
	requestCountBefore := len(*fixture.requests)
	fixture.requestsMu.Unlock()

	result, err := replayOfflineTargetMetadata(
		snapshot,
		bootstrapRootJSON,
		bootstrapState,
		time.Now().UTC(),
		fixture.repository.targetPath,
		4,
	)
	if err != nil {
		t.Fatalf("offline replay after root rotation: %v", err)
	}
	if result.trusted.Root == nil || result.trusted.Root.Signed.Version != 2 {
		t.Fatalf("offline replay trusted root = %#v, want rotated root v2", result.trusted.Root)
	}
	if result.resolution.role != "osquery" || !reflect.DeepEqual(result.resolution.target, officialTarget) {
		t.Fatalf("offline delegated target differs from updater: role=%q target=%#v official=%#v",
			result.resolution.role, result.resolution.target, officialTarget)
	}

	fixture.requestsMu.Lock()
	requestCountAfter := len(*fixture.requests)
	fixture.requestsMu.Unlock()
	if requestCountAfter != requestCountBefore {
		t.Fatalf("offline root-chain replay performed network activity: requests before=%d after=%d",
			requestCountBefore, requestCountAfter)
	}
}

func TestReplayOfflineTargetMetadataFailsClosedForMissingDelegatedRole(t *testing.T) {
	fixture := createPersistedDelegatedFixture(t)
	defer fixture.server.Close()
	snapshot, rootJSON, rootState := loadPersistedFixtureSnapshot(t, fixture)

	roles := make([]sqlitetuf.VerifiedRoleMetadata, 0, len(snapshot.Generation.Roles)-1)
	for _, role := range snapshot.Generation.Roles {
		if role.Role != "osquery" {
			roles = append(roles, role)
		}
	}
	snapshot.Generation.Roles = roles

	_, err := replayOfflineTargetMetadata(
		snapshot, rootJSON, rootState, time.Now().UTC(), fixture.repository.targetPath, 4,
	)
	if err == nil || !errors.Is(err, ErrTargetDelegationUnavailable) {
		t.Fatalf("replayOfflineTargetMetadata() error = %v, want missing delegated role rejection", err)
	}
}

func TestReplayOfflineTargetMetadataRejectsDelegatedSignatureFromUnauthorizedKey(t *testing.T) {
	fixture := createPersistedDelegatedFixture(t)
	defer fixture.server.Close()
	snapshot, rootJSON, rootState := loadPersistedFixtureSnapshot(t, fixture)

	var delegatedRole *sqlitetuf.VerifiedRoleMetadata
	var snapshotRole *sqlitetuf.VerifiedRoleMetadata
	var timestampRole *sqlitetuf.VerifiedRoleMetadata
	for index := range snapshot.Generation.Roles {
		switch snapshot.Generation.Roles[index].Role {
		case "osquery":
			delegatedRole = &snapshot.Generation.Roles[index]
		case metadata.SNAPSHOT:
			snapshotRole = &snapshot.Generation.Roles[index]
		case metadata.TIMESTAMP:
			timestampRole = &snapshot.Generation.Roles[index]
		}
	}
	if delegatedRole == nil || snapshotRole == nil || timestampRole == nil {
		t.Fatal("fixture is missing delegated, snapshot or timestamp metadata")
	}

	originalDelegated, err := metadata.Targets().FromBytes(delegatedRole.Metadata)
	if err != nil {
		t.Fatalf("parse original delegated metadata: %v", err)
	}
	originalTarget := originalDelegated.Signed.Targets[fixture.repository.targetPath]
	if originalTarget == nil {
		t.Fatal("original delegated metadata does not contain the fixture target")
	}
	unauthorizedSigner := newTestSigningKey(t)
	unauthorizedRole := metadata.Targets(originalDelegated.Signed.Expires)
	unauthorizedRole.Signed.Targets[fixture.repository.targetPath] = originalTarget
	unauthorizedBytes := signedMetadataBytes(t, unauthorizedRole, unauthorizedSigner)

	// Re-sign snapshot and timestamp with their legitimately authorized keys,
	// updating length/hash links so that only the delegated-role signature
	// should fail when the exact parent authorization is checked by go-tuf.
	snapshotMetadata, err := metadata.Snapshot().FromBytes(snapshotRole.Metadata)
	if err != nil {
		t.Fatalf("parse snapshot metadata: %v", err)
	}
	setMetaFile(t, snapshotMetadata.Signed.Meta, "osquery.json", unauthorizedBytes)
	updatedSnapshotBytes := signedMetadataBytes(t, snapshotMetadata, fixture.repository.snapshotSigner)

	timestampMetadata, err := metadata.Timestamp().FromBytes(timestampRole.Metadata)
	if err != nil {
		t.Fatalf("parse timestamp metadata: %v", err)
	}
	setMetaFile(t, timestampMetadata.Signed.Meta, "snapshot.json", updatedSnapshotBytes)
	updatedTimestampBytes := signedMetadataBytes(t, timestampMetadata, fixture.repository.timestampSigner)

	delegatedRole.Metadata = unauthorizedBytes
	snapshotRole.Metadata = updatedSnapshotBytes
	timestampRole.Metadata = updatedTimestampBytes

	_, err = replayOfflineTargetMetadata(
		snapshot,
		rootJSON,
		rootState,
		time.Now().UTC(),
		fixture.repository.targetPath,
		4,
	)
	if err == nil {
		t.Fatal("offline replay accepted delegated metadata signed by a key not authorized by its parent")
	}
	if !errors.Is(err, ErrOfflineGenerationReplay) {
		t.Fatalf("replay error = %v, want ErrOfflineGenerationReplay", err)
	}
}

func TestReplayOfflineTargetMetadataRejectsTamperedDelegatedBytes(t *testing.T) {
	fixture := createPersistedDelegatedFixture(t)
	defer fixture.server.Close()
	snapshot, rootJSON, rootState := loadPersistedFixtureSnapshot(t, fixture)

	for index := range snapshot.Generation.Roles {
		if snapshot.Generation.Roles[index].Role == "osquery" {
			raw := append([]byte(nil), snapshot.Generation.Roles[index].Metadata...)
			raw[0] = '!'
			snapshot.Generation.Roles[index].Metadata = raw
		}
	}
	_, err := replayOfflineTargetMetadata(
		snapshot, rootJSON, rootState, time.Now().UTC(), fixture.repository.targetPath, 4,
	)
	if err == nil {
		t.Fatal("replayOfflineTargetMetadata() accepted altered delegated metadata bytes")
	}
}


func TestReplayOfflineTargetMetadataRejectsTimestampAtExactExpiryBoundary(t *testing.T) {
	fixture := createPersistedDelegatedFixtureWithLongerRootExpiry(t)
	defer fixture.server.Close()
	snapshot, rootJSON, rootState := loadPersistedFixtureSnapshot(t, fixture)

	var timestampExpiry time.Time
	for _, role := range snapshot.Generation.Roles {
		if role.Role == metadata.TIMESTAMP {
			timestampExpiry = role.ExpiresAt
			break
		}
	}
	if timestampExpiry.IsZero() {
		t.Fatal("persisted generation is missing timestamp expiry")
	}
	root, err := metadata.Root().FromBytes(rootJSON)
	if err != nil {
		t.Fatalf("parse bootstrap root: %v", err)
	}
	if !root.Signed.Expires.After(timestampExpiry) {
		t.Fatalf("fixture root expiry %s must be later than timestamp expiry %s", root.Signed.Expires, timestampExpiry)
	}

	_, err = replayOfflineTargetMetadata(
		snapshot,
		rootJSON,
		rootState,
		timestampExpiry,
		fixture.repository.targetPath,
		4,
	)
	if err == nil || !errors.Is(err, ErrOfflineGenerationReplay) {
		t.Fatalf("replayOfflineTargetMetadata() at timestamp expiry boundary = %v, want ErrOfflineGenerationReplay", err)
	}
}

func TestReplayOfflineTargetMetadataUsesExplicitExpiryReferenceTime(t *testing.T) {
	fixture := createPersistedDelegatedFixture(t)
	defer fixture.server.Close()
	snapshot, rootJSON, rootState := loadPersistedFixtureSnapshot(t, fixture)

	future := time.Now().UTC().Add(48 * time.Hour)
	_, err := replayOfflineTargetMetadata(
		snapshot, rootJSON, rootState, future, fixture.repository.targetPath, 4,
	)
	if err == nil {
		t.Fatal("replayOfflineTargetMetadata() accepted metadata expired at explicit reference time")
	}
	if !errors.Is(err, ErrOfflineGenerationReplay) {
		t.Fatalf("replay error = %v, want ErrOfflineGenerationReplay", err)
	}
}

func TestReplayOfflineTargetMetadataRejectsDatabasePinAsBootstrap(t *testing.T) {
	fixture := createPersistedDelegatedFixture(t)
	defer fixture.server.Close()
	snapshot, rootJSON, rootState := loadPersistedFixtureSnapshot(t, fixture)

	snapshot.Repository.AnchorSHA256 = strings.Repeat("0", 64)
	_, err := replayOfflineTargetMetadata(
		snapshot, rootJSON, rootState, time.Now().UTC(), fixture.repository.targetPath, 4,
	)
	if err == nil {
		t.Fatal("replayOfflineTargetMetadata() trusted a SQLite pin that differs from the independent bootstrap")
	}
}

func TestReplayOfflineTargetMetadataRejectsDuplicateRoleName(t *testing.T) {
	fixture := createPersistedDelegatedFixture(t)
	defer fixture.server.Close()
	snapshot, rootJSON, rootState := loadPersistedFixtureSnapshot(t, fixture)

	// A signed targets role could name a reserved top-level role only if the
	// repository's signed delegation graph says so. The offline callback rejects
	// this namespace collision before it can overwrite trusted top-level state.
	for index := range snapshot.Generation.Roles {
		if snapshot.Generation.Roles[index].Role == "osquery" {
			snapshot.Generation.Roles[index].Role = "timestamp"
		}
	}
	_, err := replayOfflineTargetMetadata(
		snapshot, rootJSON, rootState, time.Now().UTC(), fixture.repository.targetPath, 4,
	)
	if err == nil {
		t.Fatal("replayOfflineTargetMetadata() accepted a reserved role collision")
	}
}
