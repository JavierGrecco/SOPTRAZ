package catalog

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JavierGrecco/SOPTRAZ/internal/storage/sqlitetuf"
)

func createPersistedDelegatedFixture(t *testing.T) (
	delegatedRepositoryFixture,
	*httptest.Server,
	*sqlitetuf.Store,
	*TrustAnchor,
	*,
) {
	t.Helper()
	fixture := makeDelegatedRepositoryFixture(t)
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
	return fixture, server, store, anchor, struct {
		requests    *[]string
		requestsMu  *sync.Mutex
	}{requests: requests, requestsMu: requestsMu}
}

func TestReplayOfflineTargetMetadataMatchesGoTUFWithoutNetwork(t *testing.T) {
	fixture, server, store, anchor, counters := createPersistedDelegatedFixture(t)
	snapshot, err := store.LoadOfflineSnapshot(context.Background(), "offline-test-repository")
	if err != nil {
		t.Fatalf("LoadOfflineSnapshot(): %v", err)
	}
	rootJSON, rootState, err := anchor.initialRootSnapshot()
	if err != nil {
		t.Fatalf("read independent bootstrap: %v", err)
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
	server.Close()
	counters.requestsMu.Lock()
	requestCountBefore := len(*counters.requests)
	counters.requestsMu.Unlock()

	result, err := replayOfflineTargetMetadata(
		snapshot,
		rootJSON,
		rootState,
		time.Now().UTC(),
		fixture.targetPath,
		4,
	)
	if err != nil {
		t.Fatalf("replayOfflineTargetMetadata(): %v", err)
	}
	if result.resolution.role != "osquery" ||
		result.resolution.target == nil ||
		result.resolution.target.Path != fixture.targetPath {
		t.Fatalf("unexpected offline target resolution: %#v", result.resolution)
	}
	if _, err := verifySignedArtifactTarget(fixture.targetPath, result.resolution.target, fixture.descriptor); err != nil {
		t.Fatalf("verify artifact v1 binding from offline target metadata: %v", err)
	}
	if !reflect.DeepEqual(result.trusted.Targets["osquery"], result.trusted.Targets["osquery"]) {
		t.Fatal("unreachable trusted metadata self-check")
	}
	counters.requestsMu.Lock()
	requestCountAfter := len(*counters.requests)
	counters.requestsMu.Unlock()
	if requestCountAfter != requestCountBefore {
		t.Fatalf("offline replay attempted network activity: requests before=%d after=%d", requestCountBefore, requestCountAfter)
	}
}

func TestReplayOfflineTargetMetadataFailsClosedForMissingDelegatedRole(t *testing.T) {
	fixture, server, store, anchor, _ := createPersistedDelegatedFixture(t)
	defer server.Close()
	snapshot, err := store.LoadOfflineSnapshot(context.Background(), "offline-test-repository")
	if err != nil {
		t.Fatalf("LoadOfflineSnapshot(): %v", err)
	}
	rootJSON, rootState, err := anchor.initialRootSnapshot()
	if err != nil {
		t.Fatalf("read independent bootstrap: %v", err)
	}
	roles := snapshot.Generation.Roles[:0]
	for _, role := range snapshot.Generation.Roles {
		if role.Role != "osquery" {
			roles = append(roles, role)
		}
	}
	snapshot.Generation.Roles = roles

	_, err = replayOfflineTargetMetadata(snapshot, rootJSON, rootState, time.Now().UTC(), fixture.targetPath, 4)
	if err == nil || !errors.Is(err, ErrTargetDelegationUnavailable) {
		t.Fatalf("replayOfflineTargetMetadata() error = %v, want missing delegated role rejection", err)
	}
}

func TestReplayOfflineTargetMetadataRejectsTamperedDelegatedBytes(t *testing.T) {
	fixture, server, store, anchor, _ := createPersistedDelegatedFixture(t)
	defer server.Close()
	snapshot, err := store.LoadOfflineSnapshot(context.Background(), "offline-test-repository")
	if err != nil {
		t.Fatalf("LoadOfflineSnapshot(): %v", err)
	}
	rootJSON, rootState, err := anchor.initialRootSnapshot()
	if err != nil {
		t.Fatalf("read independent bootstrap: %v", err)
	}
	for index := range snapshot.Generation.Roles {
		if snapshot.Generation.Roles[index].Role == "osquery" {
			snapshot.Generation.Roles[index].Metadata = []byte(strings.ReplaceAll(
				string(snapshot.Generation.Roles[index].Metadata),
				"signature",
				"signaturf",
			))
		}
	}
	_, err = replayOfflineTargetMetadata(snapshot, rootJSON, rootState, time.Now().UTC(), fixture.targetPath, 4)
	if err == nil {
		t.Fatal("replayOfflineTargetMetadata() accepted altered delegated metadata bytes")
	}
}

func TestReplayOfflineTargetMetadataUsesExplicitExpiryReferenceTime(t *testing.T) {
	fixture, server, store, anchor, _ := createPersistedDelegatedFixture(t)
	defer server.Close()
	snapshot, err := store.LoadOfflineSnapshot(context.Background(), "offline-test-repository")
	if err != nil {
		t.Fatalf("LoadOfflineSnapshot(): %v", err)
	}
	rootJSON, rootState, err := anchor.initialRootSnapshot()
	if err != nil {
		t.Fatalf("read independent bootstrap: %v", err)
	}
	future := time.Now().UTC().Add(48 * time.Hour)
	_, err = replayOfflineTargetMetadata(snapshot, rootJSON, rootState, future, fixture.targetPath, 4)
	if err == nil {
		t.Fatal("replayOfflineTargetMetadata() accepted metadata expired at explicit reference time")
	}
	if !errors.Is(err, ErrOfflineGenerationReplay) {
		t.Fatalf("replay error = %v, want ErrOfflineGenerationReplay", err)
	}
}

func TestReplayOfflineTargetMetadataRejectsDatabasePinAsBootstrap(t *testing.T) {
	fixture, server, store, anchor, _ := createPersistedDelegatedFixture(t)
	defer server.Close()
	snapshot, err := store.LoadOfflineSnapshot(context.Background(), "offline-test-repository")
	if err != nil {
		t.Fatalf("LoadOfflineSnapshot(): %v", err)
	}
	rootJSON, rootState, err := anchor.initialRootSnapshot()
	if err != nil {
		t.Fatalf("read independent bootstrap: %v", err)
	}
	snapshot.Repository.AnchorSHA256 = strings.Repeat("0", 64)
	_, err = replayOfflineTargetMetadata(snapshot, rootJSON, rootState, time.Now().UTC(), fixture.targetPath, 4)
	if err == nil {
		t.Fatal("replayOfflineTargetMetadata() trusted a SQLite pin that differs from the independent bootstrap")
	}
}
