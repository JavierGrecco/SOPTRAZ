package catalog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JavierGrecco/SOPTRAZ/internal/storage/sqlitetuf"
	tufconfig "github.com/theupdateframework/go-tuf/v2/metadata/config"
	tufupdater "github.com/theupdateframework/go-tuf/v2/metadata/updater"
)

func persistentClientConfig(metadataURL, cacheRoot string) TUFClientConfig {
	return TUFClientConfig{
		MetadataURL:               metadataURL,
		MetadataCacheDir:          filepath.Join(cacheRoot, "metadata"),
		TargetCacheDir:            filepath.Join(cacheRoot, "targets"),
		RequestTimeout:            3 * time.Second,
		RetryCount:                1,
		MaxRootRotations:          4,
		MaxDelegations:            4,
		AllowInsecureLoopbackHTTP: true,
	}
}

func TestPersistentTUFClientReplaysStoredRootAfterRestartWhenLaterMetadataFails(t *testing.T) {
	fixture := makeDelegatedRepositoryFixture(t)
	rootV2 := makeRotatedRootMetadata(t, fixture, true)
	fixture.metadata["2.root.json"] = rootV2
	delete(fixture.metadata, "timestamp.json")

	server, requests, requestsMu := startDelegatedMetadataServer(t, fixture)
	store, err := sqlitetuf.Open(context.Background(), filepath.Join(t.TempDir(), "catalog-trust.sqlite"))
	if err != nil {
		t.Fatalf("open persistent TUF store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close persistent TUF store: %v", err)
		}
	})

	bootstrapAnchor, err := NewPinnedTrustAnchor(fixture.rootJSON, rootPin(fixture.rootJSON))
	if err != nil {
		t.Fatalf("create bootstrap trust anchor: %v", err)
	}
	firstCache := t.TempDir()
	if _, err := NewPersistentTUFClient(
		bootstrapAnchor,
		persistentClientConfig(server.URL+"/metadata", firstCache),
		store,
		"test-repository",
	); err == nil {
		t.Fatal("NewPersistentTUFClient() succeeded despite missing timestamp metadata")
	}

	chain, storedState, err := store.LoadRootChain(context.Background(), "test-repository")
	if err != nil {
		t.Fatalf("load root chain after failed refresh: %v", err)
	}
	if storedState.Version != 2 || storedState.SHA256 != rootPin(rootV2) {
		t.Fatalf("persisted root state = %#v, want root v2 %s", storedState, rootPin(rootV2))
	}
	if len(chain) != 1 || chain[0].Version != 2 || string(chain[0].Metadata) != string(rootV2) {
		t.Fatalf("persisted root chain = %#v, want exact root v2 bytes", chain)
	}

	requestsMu.Lock()
	fetchedRootV2BeforeFailure := false
	fetchedTimestampBeforeFailure := false
	for _, requestPath := range *requests {
		if requestPath == "/metadata/2.root.json" {
			fetchedRootV2BeforeFailure = true
		}
		if requestPath == "/metadata/timestamp.json" {
			fetchedTimestampBeforeFailure = true
		}
	}
	*requests = nil
	requestsMu.Unlock()
	if !fetchedRootV2BeforeFailure || !fetchedTimestampBeforeFailure {
		t.Fatalf("first refresh did not request both root v2 and timestamp")
	}

	// Simulate a new process: the bootstrap pin is reconstructed from the
	// original v1 bytes, the on-disk metadata cache is empty, and only SQLite
	// can supply the already verified v2 root.
	fixture.metadata["timestamp.json"] = fixture.timestampJSON
	secondAnchor, err := NewPinnedTrustAnchor(fixture.rootJSON, rootPin(fixture.rootJSON))
	if err != nil {
		t.Fatalf("recreate original bootstrap anchor: %v", err)
	}
	requestsMu.Lock()
	*requests = nil
	requestsMu.Unlock()

	secondCache := t.TempDir()
	client, err := NewPersistentTUFClient(
		secondAnchor,
		persistentClientConfig(server.URL+"/metadata", secondCache),
		store,
		"test-repository",
	)
	if err != nil {
		t.Fatalf("NewPersistentTUFClient() after restart: %v", err)
	}
	if _, err := client.ResolveDescriptor(fixture.descriptor); err != nil {
		t.Fatalf("ResolveDescriptor() after persistent root replay: %v", err)
	}

	requestsMu.Lock()
	defer requestsMu.Unlock()
	fetchedRootV2AfterRestart := false
	fetchedRootV3Probe := false
	for _, requestPath := range *requests {
		if requestPath == "/metadata/2.root.json" {
			fetchedRootV2AfterRestart = true
		}
		if requestPath == "/metadata/3.root.json" {
			fetchedRootV3Probe = true
		}
	}
	if fetchedRootV2AfterRestart {
		t.Fatalf("new process re-downloaded already persisted root v2; requests = %v", *requests)
	}
	if !fetchedRootV3Probe {
		t.Fatalf("new process did not continue root rotation checks from v2; requests = %v", *requests)
	}
}

func TestPersistentTUFClientRejectsStoredRootWithoutValidTUFSignatures(t *testing.T) {
	fixture := makeDelegatedRepositoryFixture(t)
	server, requests, requestsMu := startDelegatedMetadataServer(t, fixture)
	store, err := sqlitetuf.Open(context.Background(), filepath.Join(t.TempDir(), "catalog-trust.sqlite"))
	if err != nil {
		t.Fatalf("open persistent TUF store: %v", err)
	}
	defer store.Close()

	anchor, err := NewPinnedTrustAnchor(fixture.rootJSON, rootPin(fixture.rootJSON))
	if err != nil {
		t.Fatalf("create bootstrap trust anchor: %v", err)
	}
	_, bootstrapState, err := anchor.initialRootSnapshot()
	if err != nil {
		t.Fatalf("read bootstrap trust anchor: %v", err)
	}
	if err := store.RegisterRepository(context.Background(), sqlitetuf.RepositoryConfig{
		ID:            "test-repository",
		MetadataURL:   server.URL + "/metadata",
		AnchorVersion: bootstrapState.Version,
		AnchorSHA256:  bootstrapState.SHA256,
	}); err != nil {
		t.Fatalf("register test repository: %v", err)
	}
	_, err = store.CommitVerifiedRoots(
		context.Background(),
		"test-repository",
		sqlitetuf.RootState{Version: bootstrapState.Version, SHA256: bootstrapState.SHA256},
		[]sqlitetuf.VerifiedRoot{{
			Version:        bootstrapState.Version + 1,
			PreviousSHA256: bootstrapState.SHA256,
			Metadata:       []byte("not signed TUF metadata"),
		}},
	)
	if err != nil {
		t.Fatalf("write corrupt-but-well-linked fixture into storage layer: %v", err)
	}

	_, err = NewPersistentTUFClient(
		anchor,
		persistentClientConfig(server.URL+"/metadata", t.TempDir()),
		store,
		"test-repository",
	)
	if err == nil || !errors.Is(err, ErrInvalidTUFClientConfig) || !strings.Contains(err.Error(), "autenticar la cadena") {
		t.Fatalf("NewPersistentTUFClient() error = %v, want fail-closed root-chain authentication error", err)
	}
	requestsMu.Lock()
	defer requestsMu.Unlock()
	if len(*requests) != 0 {
		t.Fatalf("client performed network requests before authenticating stored roots: %v", *requests)
	}
}

func TestPersistentRootReplayRejectsVersionAndDigestMismatch(t *testing.T) {
	fixture := makeDelegatedRepositoryFixture(t)
	bootstrapState := TrustState{Version: 1, SHA256: rootPin(fixture.rootJSON)}
	validRootV2 := makeRotatedRootMetadata(t, fixture, true)
	for _, record := range []sqlitetuf.RootRecord{
		{Version: 3, PreviousSHA256: bootstrapState.SHA256, SHA256: rootPin(validRootV2), Metadata: validRootV2},
		{Version: 2, PreviousSHA256: bootstrapState.SHA256, SHA256: rootPin([]byte("wrong digest")), Metadata: validRootV2},
	} {
		if _, _, err := replayStoredRootChain(fixture.rootJSON, bootstrapState, []sqlitetuf.RootRecord{record}); err == nil {
			t.Fatalf("replayStoredRootChain() accepted invalid record %#v", record)
		}
	}
}

func TestPersistentRefreshDoesNotCommitRootBeforeOfficialUpdaterAcceptsIt(t *testing.T) {
	fixture := makeDelegatedRepositoryFixture(t)
	rootV2 := makeRotatedRootMetadata(t, fixture, true)
	bootstrapAnchor, err := NewPinnedTrustAnchor(fixture.rootJSON, rootPin(fixture.rootJSON))
	if err != nil {
		t.Fatalf("create bootstrap trust anchor: %v", err)
	}
	_, bootstrapState, err := bootstrapAnchor.initialRootSnapshot()
	if err != nil {
		t.Fatalf("read bootstrap root: %v", err)
	}

	store, err := sqlitetuf.Open(context.Background(), filepath.Join(t.TempDir(), "catalog-trust.sqlite"))
	if err != nil {
		t.Fatalf("open persistent trust store: %v", err)
	}
	defer store.Close()
	const repositoryID = "test-repository"
	const metadataURL = "https://metadata.example.test/repository"
	if err := store.RegisterRepository(context.Background(), sqlitetuf.RepositoryConfig{
		ID:            repositoryID,
		MetadataURL:   metadataURL,
		AnchorVersion: bootstrapState.Version,
		AnchorSHA256:  bootstrapState.SHA256,
	}); err != nil {
		t.Fatalf("register repository: %v", err)
	}

	cacheRoot := t.TempDir()
	cfg, err := tufconfig.New(metadataURL, fixture.rootJSON)
	if err != nil {
		t.Fatalf("create updater config: %v", err)
	}
	cfg.LocalMetadataDir = filepath.Join(cacheRoot, "metadata")
	cfg.LocalTargetsDir = filepath.Join(cacheRoot, "targets")
	officialUpdater, err := tufupdater.New(cfg)
	if err != nil {
		t.Fatalf("create official updater: %v", err)
	}
	capture, err := newRootMetadataCaptureTransport(metadataURL, nil)
	if err != nil {
		t.Fatalf("create root capture transport: %v", err)
	}
	capture.recordRoot(2, rootV2)

	client := &TUFClient{
		config: cfg,
		persistence: &persistentTUFTrust{
			store:          store,
			repositoryID:   repositoryID,
			rootState:      sqlitetuf.RootState{Version: bootstrapState.Version, SHA256: bootstrapState.SHA256},
			latestRootJSON: append([]byte(nil), fixture.rootJSON...),
			capture:        capture,
		},
	}
	if err := client.finishPersistentRefresh(officialUpdater, nil); err == nil {
		t.Fatal("finishPersistentRefresh() accepted a captured root the official updater had not accepted")
	}

	chain, state, err := store.LoadRootChain(context.Background(), repositoryID)
	if err != nil {
		t.Fatalf("load trust state after rejected promotion: %v", err)
	}
	if state.Version != bootstrapState.Version || state.SHA256 != bootstrapState.SHA256 || len(chain) != 0 {
		t.Fatalf("unaccepted root was committed: state=%#v chain=%#v", state, chain)
	}
}

func TestPersistentTUFClientCommitsVerifiedGenerationAndDelegationMetadata(t *testing.T) {
	fixture := makeDelegatedRepositoryFixture(t)
	server, _, _ := startDelegatedMetadataServer(t, fixture)
	cacheRoot := t.TempDir()
	store, err := sqlitetuf.Open(context.Background(), filepath.Join(t.TempDir(), "catalog-trust.sqlite"))
	if err != nil {
		t.Fatalf("open persistent TUF store: %v", err)
	}
	defer store.Close()

	anchor, err := NewPinnedTrustAnchor(fixture.rootJSON, rootPin(fixture.rootJSON))
	if err != nil {
		t.Fatalf("create pinned anchor: %v", err)
	}
	client, err := NewPersistentTUFClient(
		anchor,
		persistentClientConfig(server.URL+"/metadata", cacheRoot),
		store,
		"test-repository",
	)
	if err != nil {
		t.Fatalf("NewPersistentTUFClient(): %v", err)
	}

	initial, err := store.LoadActiveGeneration(context.Background(), "test-repository")
	if err != nil {
		t.Fatalf("load top-level generation: %v", err)
	}
	if len(initial.Roles) != 3 {
		t.Fatalf("initial generation has %d roles, want timestamp/snapshot/targets only", len(initial.Roles))
	}
	initialID := initial.ID
	for _, required := range []string{"timestamp", "snapshot", "targets"} {
		if !hasGenerationRole(initial.Roles, required) {
			t.Fatalf("initial generation is missing role %q", required)
		}
	}

	verified, err := client.ResolveDescriptor(fixture.descriptor)
	if err != nil {
		t.Fatalf("ResolveDescriptor(): %v", err)
	}
	if verified.Path != fixture.targetPath {
		t.Fatalf("verified target path = %q, want %q", verified.Path, fixture.targetPath)
	}

	delegated, err := store.LoadActiveGeneration(context.Background(), "test-repository")
	if err != nil {
		t.Fatalf("load generation with delegated metadata: %v", err)
	}
	if delegated.ID != initialID+1 || len(delegated.Roles) != 4 {
		t.Fatalf("delegated generation = ID %d with %d roles, want ID %d with 4 roles",
			delegated.ID, len(delegated.Roles), initialID+1)
	}
	var delegatedRole *sqlitetuf.VerifiedRoleMetadata
	for index := range delegated.Roles {
		if delegated.Roles[index].Role == "osquery" {
			delegatedRole = &delegated.Roles[index]
			break
		}
	}
	if delegatedRole == nil {
		t.Fatal("generation does not contain delegated osquery metadata")
	}
	if len(delegatedRole.Parents) != 1 || delegatedRole.Parents[0] != "targets" {
		t.Fatalf("delegated role parent chain = %v, want [targets]", delegatedRole.Parents)
	}
	if string(delegatedRole.Metadata) != string(fixture.metadata["osquery.json"]) {
		t.Fatal("generation did not preserve the exact delegated metadata bytes")
	}

	// A second successful lookup over the same authenticated metadata set must
	// not create a new immutable generation with identical role bytes.
	if _, err := client.ResolveDescriptor(fixture.descriptor); err != nil {
		t.Fatalf("ResolveDescriptor() repeated: %v", err)
	}
	afterRepeat, err := store.LoadActiveGeneration(context.Background(), "test-repository")
	if err != nil {
		t.Fatalf("load generation after repeated resolution: %v", err)
	}
	if afterRepeat.ID != delegated.ID || afterRepeat.ManifestSHA256 != delegated.ManifestSHA256 {
		t.Fatalf("repeated resolution created a redundant generation: before=%d after=%d",
			delegated.ID, afterRepeat.ID)
	}
}

func TestPersistentTUFGenerationRejectsCacheBytesChangedAfterUpdaterAcceptedThem(t *testing.T) {
	fixture := makeDelegatedRepositoryFixture(t)
	server, _, _ := startDelegatedMetadataServer(t, fixture)
	cacheRoot := t.TempDir()
	store, err := sqlitetuf.Open(context.Background(), filepath.Join(t.TempDir(), "catalog-trust.sqlite"))
	if err != nil {
		t.Fatalf("open persistent TUF store: %v", err)
	}
	defer store.Close()

	anchor, err := NewPinnedTrustAnchor(fixture.rootJSON, rootPin(fixture.rootJSON))
	if err != nil {
		t.Fatalf("create pinned anchor: %v", err)
	}
	client, err := NewPersistentTUFClient(
		anchor,
		persistentClientConfig(server.URL+"/metadata", cacheRoot),
		store,
		"test-repository",
	)
	if err != nil {
		t.Fatalf("NewPersistentTUFClient(): %v", err)
	}
	if _, err := client.ResolveDescriptor(fixture.descriptor); err != nil {
		t.Fatalf("ResolveDescriptor(): %v", err)
	}
	before, err := store.LoadActiveGeneration(context.Background(), "test-repository")
	if err != nil {
		t.Fatalf("load committed generation: %v", err)
	}

	delegatedCache := filepath.Join(cacheRoot, "metadata", "osquery.json")
	if err := os.WriteFile(delegatedCache, []byte("{\"signed\":{\"_type\":\"targets\"}}"), 0o600); err != nil {
		t.Fatalf("replace delegated cache fixture: %v", err)
	}
	client.mu.Lock()
	err = client.persistGenerationLocked(client.updater)
	client.mu.Unlock()
	if err == nil {
		t.Fatal("persistGenerationLocked() accepted cache bytes changed after go-tuf validation")
	}
	after, loadErr := store.LoadActiveGeneration(context.Background(), "test-repository")
	if loadErr != nil {
		t.Fatalf("load active generation after rejected cache mutation: %v", loadErr)
	}
	if after.ID != before.ID || after.ManifestSHA256 != before.ManifestSHA256 {
		t.Fatalf("untrusted cache mutation changed active generation: before=%d after=%d", before.ID, after.ID)
	}
}

func hasGenerationRole(roles []sqlitetuf.VerifiedRoleMetadata, name string) bool {
	for _, role := range roles {
		if role.Role == name {
			return true
		}
	}
	return false
}
