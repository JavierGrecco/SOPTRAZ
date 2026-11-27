package catalog

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JavierGrecco/SOPTRAZ/internal/modules/artifact"
	"github.com/theupdateframework/go-tuf/v2/metadata"
)

type delegatedRepositoryFixture struct {
	rootJSON  []byte
	metadata  map[string][]byte
	descriptor artifact.Descriptor
	targetPath string
}

func addFixtureRootKey(t *testing.T, root *metadata.Metadata[metadata.RootType], key testSigningKey, role string) {
	t.Helper()
	publicKey, ok := key.private.Public().(ed25519.PublicKey)
	if !ok {
		t.Fatal("test signer returned a non-Ed25519 public key")
	}
	tufKey, err := metadata.KeyFromPublicKey(publicKey)
	if err != nil {
		t.Fatalf("convert public key for role %s: %v", role, err)
	}
	if err := root.Signed.AddKey(tufKey, role); err != nil {
		t.Fatalf("add root key for role %s: %v", role, err)
	}
}

func signedMetadataBytes[T metadata.Roles](t *testing.T, value *metadata.Metadata[T], signer testSigningKey) []byte {
	t.Helper()
	if _, err := value.Sign(signer.signer); err != nil {
		t.Fatalf("sign TUF metadata: %v", err)
	}
	raw, err := value.ToBytes(false)
	if err != nil {
		t.Fatalf("marshal TUF metadata: %v", err)
	}
	return raw
}

func setMetaFile(t *testing.T, values map[string]*metadata.MetaFiles, name string, raw []byte) {
	t.Helper()
	digest := sha256.Sum256(raw)
	values[name] = &metadata.MetaFiles{
		Version: 1,
		Length:  int64(len(raw)),
		Hashes:  metadata.Hashes{"sha256": metadata.HexBytes(digest[:])},
	}
}

func makeDelegatedRepositoryFixture(t *testing.T) delegatedRepositoryFixture {
	t.Helper()
	expiry := time.Now().UTC().Add(24 * time.Hour)
	rootKey := newTestSigningKey(t)
	timestampKey := newTestSigningKey(t)
	snapshotKey := newTestSigningKey(t)
	topTargetsKey := newTestSigningKey(t)
	delegatedTargetsKey := newTestSigningKey(t)

	root := metadata.Root(expiry)
	// Consistent snapshots are not required for this fixture; the production
	// updater still verifies snapshot links, signatures, expiry and versions.
	root.Signed.ConsistentSnapshot = false
	for _, entry := range []struct {
		key  testSigningKey
		role string
	}{
		{rootKey, metadata.ROOT},
		{timestampKey, metadata.TIMESTAMP},
		{snapshotKey, metadata.SNAPSHOT},
		{topTargetsKey, metadata.TARGETS},
	} {
		addFixtureRootKey(t, root, entry.key, entry.role)
	}
	rootJSON := signedMetadataBytes(t, root, rootKey)

	delegatedPublic, ok := delegatedTargetsKey.private.Public().(ed25519.PublicKey)
	if !ok {
		t.Fatal("delegated test signer returned a non-Ed25519 public key")
	}
	delegatedTUFKey, err := metadata.KeyFromPublicKey(delegatedPublic)
	if err != nil {
		t.Fatalf("convert delegated public key: %v", err)
	}
	delegatedKeyID, err := delegatedTUFKey.ID()
	if err != nil {
		t.Fatalf("calculate delegated key ID: %v", err)
	}

	targetPath := "osquery/5.24.1/linux-amd64/osqueryi"
	targetBytes := []byte("signed delegated osquery fixture payload")
	targetDigest := sha256.Sum256(targetBytes)
	descriptor := artifact.Descriptor{
		SchemaVersion: artifact.ArtifactDescriptorSchemaVersion,
		ID:            "soptraz.osquery",
		Version:       "5.24.1",
		OS:            "linux",
		Architecture:  "amd64",
		RelativePath:  targetPath,
		SHA256:        hex.EncodeToString(targetDigest[:]),
		SizeBytes:     int64(len(targetBytes)),
		License:       "Apache-2.0 OR GPL-2.0-only",
		SourceURI:     "https://github.com/osquery/osquery/releases/tag/5.24.1",
	}
	customBytes, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatalf("marshal artifact descriptor: %v", err)
	}
	custom := json.RawMessage(customBytes)

	delegatedTargets := metadata.Targets(expiry)
	delegatedTargets.Signed.Targets[targetPath] = &metadata.TargetFiles{
		Length: int64(len(targetBytes)),
		Hashes: metadata.Hashes{"sha256": metadata.HexBytes(targetDigest[:])},
		Custom: &custom,
	}
	delegatedTargetsJSON := signedMetadataBytes(t, delegatedTargets, delegatedTargetsKey)

	topTargets := metadata.Targets(expiry)
	topTargets.Signed.Delegations = &metadata.Delegations{
		Keys: map[string]*metadata.Key{
			delegatedKeyID: delegatedTUFKey,
		},
		Roles: []metadata.DelegatedRole{{
			Name:      "osquery",
			KeyIDs:    []string{delegatedKeyID},
			Threshold: 1,
			Terminating: true,
			Paths:     []string{"osquery/*/*/*"},
		}},
	}
	topTargetsJSON := signedMetadataBytes(t, topTargets, topTargetsKey)

	snapshot := metadata.Snapshot(expiry)
	setMetaFile(t, snapshot.Signed.Meta, "targets.json", topTargetsJSON)
	setMetaFile(t, snapshot.Signed.Meta, "osquery.json", delegatedTargetsJSON)
	snapshotJSON := signedMetadataBytes(t, snapshot, snapshotKey)

	timestamp := metadata.Timestamp(expiry)
	setMetaFile(t, timestamp.Signed.Meta, "snapshot.json", snapshotJSON)
	timestampJSON := signedMetadataBytes(t, timestamp, timestampKey)

	return delegatedRepositoryFixture{
		rootJSON: rootJSON,
		metadata: map[string][]byte{
			"timestamp.json": timestampJSON,
			"snapshot.json":  snapshotJSON,
			"targets.json":   topTargetsJSON,
			"osquery.json":   delegatedTargetsJSON,
		},
		descriptor: descriptor,
		targetPath: targetPath,
	}
}

func startDelegatedMetadataServer(t *testing.T, fixture delegatedRepositoryFixture) (*httptest.Server, *[]string, *sync.Mutex) {
	t.Helper()
	var requests []string
	var requestsMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestsMu.Lock()
		requests = append(requests, r.URL.Path)
		requestsMu.Unlock()

		if !strings.HasPrefix(r.URL.Path, "/metadata/") {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/metadata/")
		raw, ok := fixture.metadata[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(raw)
	}))
	t.Cleanup(server.Close)
	return server, &requests, &requestsMu
}

func newFixtureTUFClient(t *testing.T, fixture delegatedRepositoryFixture, metadataURL string, maxDelegations int) *TUFClient {
	t.Helper()
	anchor, err := NewPinnedTrustAnchor(fixture.rootJSON, rootPin(fixture.rootJSON))
	if err != nil {
		t.Fatalf("NewPinnedTrustAnchor() error = %v", err)
	}
	cacheRoot := t.TempDir()
	client, err := NewTUFClient(anchor, TUFClientConfig{
		MetadataURL:               metadataURL,
		MetadataCacheDir:          filepath.Join(cacheRoot, "metadata"),
		TargetCacheDir:            filepath.Join(cacheRoot, "targets"),
		RequestTimeout:            3 * time.Second,
		RetryCount:                1,
		MaxRootRotations:          4,
		MaxDelegations:            maxDelegations,
		AllowInsecureLoopbackHTTP: true,
	})
	if err != nil {
		t.Fatalf("NewTUFClient() error = %v", err)
	}
	return client
}

func TestTUFClientResolvesAndVerifiesDelegatedArtifactTarget(t *testing.T) {
	fixture := makeDelegatedRepositoryFixture(t)
	server, requests, requestsMu := startDelegatedMetadataServer(t, fixture)
	client := newFixtureTUFClient(t, fixture, server.URL+"/metadata", 4)

	verified, err := client.ResolveDescriptor(fixture.descriptor)
	if err != nil {
		t.Fatalf("ResolveDescriptor() error = %v", err)
	}
	if verified.Path != fixture.targetPath ||
		verified.Length != fixture.descriptor.SizeBytes ||
		verified.SHA256 != fixture.descriptor.SHA256 {
		t.Fatalf("unexpected verified delegated target: %#v", verified)
	}

	requestsMu.Lock()
	defer requestsMu.Unlock()
	foundDelegatedMetadata := false
	downloadedTarget := false
	for _, requestPath := range *requests {
		if requestPath == "/metadata/osquery.json" {
			foundDelegatedMetadata = true
		}
		if strings.HasPrefix(requestPath, "/targets/") {
			downloadedTarget = true
		}
	}
	if !foundDelegatedMetadata {
		t.Fatalf("official updater did not fetch delegated role metadata; requests = %v", *requests)
	}
	if downloadedTarget {
		t.Fatalf("metadata lookup unexpectedly downloaded an artifact target: %v", *requests)
	}

	// go-tuf permits one Refresh per Updater instance. TUFClient must create
	// a fresh updater and revalidate cached metadata before refreshing again.
	if err := client.Refresh(); err != nil {
		t.Fatalf("Refresh() on an existing client error = %v", err)
	}
	if _, err := client.ResolveDescriptor(fixture.descriptor); err != nil {
		t.Fatalf("ResolveDescriptor() after Refresh() error = %v", err)
	}
}

func TestTUFClientRejectsDescriptorMismatchForDelegatedTarget(t *testing.T) {
	fixture := makeDelegatedRepositoryFixture(t)
	server, _, _ := startDelegatedMetadataServer(t, fixture)
	client := newFixtureTUFClient(t, fixture, server.URL+"/metadata", 4)

	descriptor := fixture.descriptor
	descriptor.License = "MIT"
	if _, err := client.ResolveDescriptor(descriptor); err != ErrTargetDescriptorMismatch {
		t.Fatalf("ResolveDescriptor() error = %v, want ErrTargetDescriptorMismatch", err)
	}
}

func TestTUFClientSerializesConcurrentUpdaterAccess(t *testing.T) {
	fixture := makeDelegatedRepositoryFixture(t)
	server, _, _ := startDelegatedMetadataServer(t, fixture)
	client := newFixtureTUFClient(t, fixture, server.URL+"/metadata", 4)

	const workers = 8
	var wait sync.WaitGroup
	errorsFound := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			target, err := client.ResolveDescriptor(fixture.descriptor)
			if err != nil {
				errorsFound <- err
				return
			}
			if target.Path != fixture.targetPath {
				errorsFound <- fmt.Errorf("unexpected target path %q", target.Path)
			}
		}()
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Errorf("concurrent ResolveDescriptor() error: %v", err)
	}
}

func TestTUFClientRejectsPlainHTTPOutsideExplicitLoopbackMode(t *testing.T) {
	fixture := makeDelegatedRepositoryFixture(t)
	anchor, err := NewPinnedTrustAnchor(fixture.rootJSON, rootPin(fixture.rootJSON))
	if err != nil {
		t.Fatalf("NewPinnedTrustAnchor() error = %v", err)
	}
	cacheRoot := t.TempDir()
	_, err = NewTUFClient(anchor, TUFClientConfig{
		MetadataURL:      "http://example.com/tuf/metadata",
		MetadataCacheDir: filepath.Join(cacheRoot, "metadata"),
		TargetCacheDir:   filepath.Join(cacheRoot, "targets"),
		RequestTimeout:   3 * time.Second,
	})
	if err == nil {
		t.Fatal("NewTUFClient() accepted non-loopback HTTP")
	}
}
func TestTUFClientDoesNotFollowMetadataRedirects(t *testing.T) {
	fixture := makeDelegatedRepositoryFixture(t)
	var redirectedRequests int
	var redirectedMu sync.Mutex
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirectedMu.Lock()
		redirectedRequests++
		redirectedMu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(destination.Close)

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL+"/metadata/"+strings.TrimPrefix(r.URL.Path, "/metadata/"), http.StatusFound)
	}))
	t.Cleanup(source.Close)

	anchor, err := NewPinnedTrustAnchor(fixture.rootJSON, rootPin(fixture.rootJSON))
	if err != nil {
		t.Fatalf("NewPinnedTrustAnchor() error = %v", err)
	}
	cacheRoot := t.TempDir()
	_, err = NewTUFClient(anchor, TUFClientConfig{
		MetadataURL:               source.URL + "/metadata",
		MetadataCacheDir:          filepath.Join(cacheRoot, "metadata"),
		TargetCacheDir:            filepath.Join(cacheRoot, "targets"),
		RequestTimeout:            3 * time.Second,
		RetryCount:                1,
		AllowInsecureLoopbackHTTP: true,
	})
	if err == nil {
		t.Fatal("NewTUFClient() accepted redirected TUF metadata")
	}
	redirectedMu.Lock()
	defer redirectedMu.Unlock()
	if redirectedRequests != 0 {
		t.Fatalf("TUF client followed a metadata redirect %d time(s)", redirectedRequests)
	}
}
