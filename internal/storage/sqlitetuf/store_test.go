package sqlitetuf

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func testRepositoryConfig(anchor []byte) RepositoryConfig {
	return RepositoryConfig{
		ID:            "soptraz-tools",
		MetadataURL:   "https://metadata.example.test/repository",
		AnchorVersion: 1,
		AnchorSHA256:  digestHex(anchor),
	}
}

func digestHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func openTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "catalog-trust.sqlite")
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return store, path
}

func registerTestRepository(t *testing.T, store *Store) (RepositoryConfig, RootState) {
	t.Helper()
	anchor := []byte("independently-pinned-root-v1")
	config := testRepositoryConfig(anchor)
	if err := store.RegisterRepository(context.Background(), config); err != nil {
		t.Fatalf("RegisterRepository() error = %v", err)
	}
	return config, RootState{Version: config.AnchorVersion, SHA256: config.AnchorSHA256}
}

func TestCommitAndLoadRootChainAfterReopen(t *testing.T) {
	ctx := context.Background()
	store, path := openTestStore(t)
	_, anchor := registerTestRepository(t, store)

	rootV2 := []byte("verified-root-v2")
	rootV3 := []byte("verified-root-v3")
	expectedV3 := RootState{Version: 3, SHA256: digestHex(rootV3)}

	state, err := store.CommitVerifiedRoots(ctx, "soptraz-tools", anchor, []VerifiedRoot{
		{Version: 2, PreviousSHA256: anchor.SHA256, Metadata: rootV2},
		{Version: 3, PreviousSHA256: digestHex(rootV2), Metadata: rootV3},
	})
	if err != nil {
		t.Fatalf("CommitVerifiedRoots() error = %v", err)
	}
	if state != expectedV3 {
		t.Fatalf("committed state = %#v, want %#v", state, expectedV3)
	}

	chain, loadedState, err := store.LoadRootChain(ctx, "soptraz-tools")
	if err != nil {
		t.Fatalf("LoadRootChain() error = %v", err)
	}
	if loadedState != expectedV3 {
		t.Fatalf("loaded state = %#v, want %#v", loadedState, expectedV3)
	}
	if len(chain) != 2 {
		t.Fatalf("root chain length = %d, want 2", len(chain))
	}
	if chain[0].Version != 2 || chain[0].PreviousSHA256 != anchor.SHA256 ||
		chain[0].SHA256 != digestHex(rootV2) || string(chain[0].Metadata) != string(rootV2) {
		t.Fatalf("unexpected first root record: %#v", chain[0])
	}
	if chain[1].Version != 3 || chain[1].PreviousSHA256 != chain[0].SHA256 ||
		chain[1].SHA256 != digestHex(rootV3) || string(chain[1].Metadata) != string(rootV3) {
		t.Fatalf("unexpected second root record: %#v", chain[1])
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() before reopen error = %v", err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open() after close error = %v", err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Errorf("reopened Close() error = %v", err)
		}
	})
	reopenedChain, reopenedState, err := reopened.LoadRootChain(ctx, "soptraz-tools")
	if err != nil {
		t.Fatalf("LoadRootChain() after reopen error = %v", err)
	}
	if reopenedState != expectedV3 || len(reopenedChain) != 2 {
		t.Fatalf("reopened state/chain = %#v/%d, want %#v/2", reopenedState, len(reopenedChain), expectedV3)
	}
}

func TestCommitVerifiedRootsIsAtomicWhenLaterRotationIsInvalid(t *testing.T) {
	ctx := context.Background()
	store, _ := openTestStore(t)
	_, anchor := registerTestRepository(t, store)

	rootV2 := []byte("verified-root-v2")
	rootV4 := []byte("verified-root-v4-with-a-gap")
	_, err := store.CommitVerifiedRoots(ctx, "soptraz-tools", anchor, []VerifiedRoot{
		{Version: 2, PreviousSHA256: anchor.SHA256, Metadata: rootV2},
		{Version: 4, PreviousSHA256: digestHex(rootV2), Metadata: rootV4},
	})
	if !errors.Is(err, ErrInvalidRootChain) {
		t.Fatalf("CommitVerifiedRoots() error = %v, want ErrInvalidRootChain", err)
	}

	state, err := store.RootState(ctx, "soptraz-tools")
	if err != nil {
		t.Fatalf("RootState() after rejected batch error = %v", err)
	}
	if state != anchor {
		t.Fatalf("state after rejected batch = %#v, want anchor %#v", state, anchor)
	}
	chain, loadedState, err := store.LoadRootChain(ctx, "soptraz-tools")
	if err != nil {
		t.Fatalf("LoadRootChain() after rejected batch error = %v", err)
	}
	if len(chain) != 0 || loadedState != anchor {
		t.Fatalf("partial root batch persisted: chain length %d, state %#v", len(chain), loadedState)
	}
}

func TestCommitVerifiedRootsRejectsWrongPreviousDigest(t *testing.T) {
	ctx := context.Background()
	store, _ := openTestStore(t)
	_, anchor := registerTestRepository(t, store)

	_, err := store.CommitVerifiedRoots(ctx, "soptraz-tools", anchor, []VerifiedRoot{
		{Version: 2, PreviousSHA256: digestHex([]byte("not-the-anchor")), Metadata: []byte("root-v2")},
	})
	if !errors.Is(err, ErrInvalidRootChain) {
		t.Fatalf("CommitVerifiedRoots() error = %v, want ErrInvalidRootChain", err)
	}
	state, err := store.RootState(ctx, "soptraz-tools")
	if err != nil {
		t.Fatalf("RootState() error = %v", err)
	}
	if state != anchor {
		t.Fatalf("state changed after rejected root: %#v", state)
	}
}

func TestCommitVerifiedRootsUsesCompareAndSwap(t *testing.T) {
	ctx := context.Background()
	store, _ := openTestStore(t)
	_, anchor := registerTestRepository(t, store)

	rootV2 := []byte("verified-root-v2")
	stateV2, err := store.CommitVerifiedRoots(ctx, "soptraz-tools", anchor, []VerifiedRoot{
		{Version: 2, PreviousSHA256: anchor.SHA256, Metadata: rootV2},
	})
	if err != nil {
		t.Fatalf("initial CommitVerifiedRoots() error = %v", err)
	}

	_, err = store.CommitVerifiedRoots(ctx, "soptraz-tools", anchor, []VerifiedRoot{
		{Version: 2, PreviousSHA256: anchor.SHA256, Metadata: []byte("different-root-v2")},
	})
	if !errors.Is(err, ErrTrustStateConflict) {
		t.Fatalf("stale CommitVerifiedRoots() error = %v, want ErrTrustStateConflict", err)
	}
	current, err := store.RootState(ctx, "soptraz-tools")
	if err != nil {
		t.Fatalf("RootState() error = %v", err)
	}
	if current != stateV2 {
		t.Fatalf("stale writer changed state: got %#v, want %#v", current, stateV2)
	}
}

func TestRegisterRepositoryDoesNotSilentlyChangeTrustAnchor(t *testing.T) {
	store, _ := openTestStore(t)
	config, _ := registerTestRepository(t, store)

	config.AnchorSHA256 = digestHex([]byte("different-independent-anchor"))
	if err := store.RegisterRepository(context.Background(), config); !errors.Is(err, ErrRepositoryConflict) {
		t.Fatalf("RegisterRepository() error = %v, want ErrRepositoryConflict", err)
	}
}

func TestRegisterRepositoryRejectsCredentialBearingURL(t *testing.T) {
	store, _ := openTestStore(t)
	config := testRepositoryConfig([]byte("anchor"))
	config.MetadataURL = "https://user:secret@metadata.example.test/repository"
	if err := store.RegisterRepository(context.Background(), config); !errors.Is(err, ErrInvalidRepository) {
		t.Fatalf("RegisterRepository() error = %v, want ErrInvalidRepository", err)
	}
}

func TestLoadRootChainDetectsStoredMetadataTampering(t *testing.T) {
	ctx := context.Background()
	store, _ := openTestStore(t)
	_, anchor := registerTestRepository(t, store)

	rootV2 := []byte("verified-root-v2")
	_, err := store.CommitVerifiedRoots(ctx, "soptraz-tools", anchor, []VerifiedRoot{
		{Version: 2, PreviousSHA256: anchor.SHA256, Metadata: rootV2},
	})
	if err != nil {
		t.Fatalf("CommitVerifiedRoots() error = %v", err)
	}

	if _, err := store.db.ExecContext(ctx, "DROP TRIGGER root_chain_no_update"); err != nil {
		t.Fatalf("drop immutable trigger for corruption fixture: %v", err)
	}
	if _, err := store.db.ExecContext(ctx,
		"UPDATE root_chain SET root_json = ? WHERE repository_id = ? AND version = 2",
		[]byte("tampered-root-v2"),
		"soptraz-tools",
	); err != nil {
		t.Fatalf("tamper root fixture: %v", err)
	}

	if _, _, err := store.LoadRootChain(ctx, "soptraz-tools"); !errors.Is(err, ErrCorruptTrustStore) {
		t.Fatalf("LoadRootChain() error = %v, want ErrCorruptTrustStore", err)
	}
}

func TestOpenRejectsMemoryDatabase(t *testing.T) {
	if _, err := Open(context.Background(), ":memory:"); !errors.Is(err, ErrInvalidTrustStore) {
		t.Fatalf("Open(:memory:) error = %v, want ErrInvalidTrustStore", err)
	}
}

func TestOpenCreatesPrivateParentDirectoryWhereSupported(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows directory permissions are controlled by ACLs, not Unix mode bits")
	}
	root := t.TempDir()
	path := filepath.Join(root, "private", "nested", "catalog-trust.sqlite")
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("Stat() directory error = %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("trust-store directory permissions = %o, want no group/other access where supported", info.Mode().Perm())
	}
}

func TestDatabasePathReturnsAbsoluteResolvedPath(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "nested", "catalog-trust.sqlite")
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	got := store.DatabasePath()
	want, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("Abs() error = %v", err)
	}
	if got != want {
		t.Fatalf("DatabasePath() = %q, want %q", got, want)
	}
}

func TestTrustStoreUsesExtraSynchronousRollbackJournal(t *testing.T) {
	store, _ := openTestStore(t)
	ctx := context.Background()

	var journalMode string
	if err := store.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	if !strings.EqualFold(journalMode, "delete") {
		t.Fatalf("journal_mode = %q, want DELETE", journalMode)
	}

	var synchronous int
	if err := store.db.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&synchronous); err != nil {
		t.Fatalf("read synchronous: %v", err)
	}
	if synchronous != 3 {
		t.Fatalf("synchronous = %d, want 3 (EXTRA)", synchronous)
	}
}

func TestCommitVerifiedRootsRollsBackWhenLatestRootUpdateFails(t *testing.T) {
	ctx := context.Background()
	store, _ := openTestStore(t)
	_, anchor := registerTestRepository(t, store)

	if _, err := store.db.ExecContext(ctx,
		"CREATE TRIGGER reject_latest_root_update "+
			"BEFORE UPDATE OF latest_root_version, latest_root_sha256 ON repositories "+
			"BEGIN SELECT RAISE(ABORT, 'simulated latest-root pointer failure'); END",
	); err != nil {
		t.Fatalf("create latest-root failure trigger: %v", err)
	}

	_, err := store.CommitVerifiedRoots(ctx, "soptraz-tools", anchor, []VerifiedRoot{{
		Version:        anchor.Version + 1,
		PreviousSHA256: anchor.SHA256,
		Metadata:       []byte("verified-root-v2"),
	}})
	if err == nil {
		t.Fatal("CommitVerifiedRoots() succeeded despite forced latest-root update failure")
	}

	state, err := store.RootState(ctx, "soptraz-tools")
	if err != nil {
		t.Fatalf("RootState() after failed commit: %v", err)
	}
	if state != anchor {
		t.Fatalf("root state after failed commit = %#v, want bootstrap anchor %#v", state, anchor)
	}
	chain, loadedState, err := store.LoadRootChain(ctx, "soptraz-tools")
	if err != nil {
		t.Fatalf("LoadRootChain() after failed commit: %v", err)
	}
	if len(chain) != 0 || loadedState != anchor {
		t.Fatalf("partial root rotation escaped rollback: chain=%d state=%#v", len(chain), loadedState)
	}
}
