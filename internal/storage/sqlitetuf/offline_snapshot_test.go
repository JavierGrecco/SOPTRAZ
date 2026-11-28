package sqlitetuf

import (
	"context"
	"errors"
	"testing"
)

func TestLoadOfflineSnapshotReturnsPinnedRootChainAndActiveGeneration(t *testing.T) {
	ctx := context.Background()
	store, _, anchor := openGenerationTestStore(t)
	config := testRepositoryConfig([]byte("independently-pinned-root-v1"))

	rootV2 := []byte("verified-root-v2")
	latest, err := store.CommitVerifiedRoots(ctx, config.ID, anchor, []VerifiedRoot{
		{Version: 2, PreviousSHA256: anchor.SHA256, Metadata: rootV2},
	})
	if err != nil {
		t.Fatalf("CommitVerifiedRoots() error = %v", err)
	}

	committed, err := store.CommitGeneration(ctx, config.ID, 0, latest, generationTestRoles(4, 7, 9))
	if err != nil {
		t.Fatalf("CommitGeneration() error = %v", err)
	}

	snapshot, err := store.LoadOfflineSnapshot(ctx, config.ID)
	if err != nil {
		t.Fatalf("LoadOfflineSnapshot() error = %v", err)
	}
	if snapshot.Repository != config {
		t.Fatalf("repository config = %#v, want %#v", snapshot.Repository, config)
	}
	if snapshot.LatestRoot != latest {
		t.Fatalf("latest root = %#v, want %#v", snapshot.LatestRoot, latest)
	}
	if len(snapshot.RootChain) != 1 ||
		snapshot.RootChain[0].Version != 2 ||
		snapshot.RootChain[0].PreviousSHA256 != anchor.SHA256 ||
		snapshot.RootChain[0].SHA256 != latest.SHA256 ||
		string(snapshot.RootChain[0].Metadata) != string(rootV2) {
		t.Fatalf("root chain = %#v", snapshot.RootChain)
	}
	if snapshot.Generation.ID != committed.ID ||
		snapshot.Generation.Root != latest ||
		snapshot.Generation.ManifestSHA256 != committed.ManifestSHA256 ||
		len(snapshot.Generation.Roles) != 3 {
		t.Fatalf("active generation = %#v, want %#v", snapshot.Generation, committed)
	}
}

func TestLoadOfflineSnapshotFailsClosedWithoutActiveGeneration(t *testing.T) {
	ctx := context.Background()
	store, _, anchor := openGenerationTestStore(t)
	config := testRepositoryConfig([]byte("independently-pinned-root-v1"))

	rootV2 := []byte("verified-root-v2")
	_, err := store.CommitVerifiedRoots(ctx, config.ID, anchor, []VerifiedRoot{
		{Version: 2, PreviousSHA256: anchor.SHA256, Metadata: rootV2},
	})
	if err != nil {
		t.Fatalf("CommitVerifiedRoots() error = %v", err)
	}

	if _, err := store.LoadOfflineSnapshot(ctx, config.ID); !errors.Is(err, ErrGenerationNotFound) {
		t.Fatalf("LoadOfflineSnapshot() error = %v, want ErrGenerationNotFound", err)
	}
}

func TestLoadOfflineSnapshotRejectsGenerationFromPreviousRoot(t *testing.T) {
	ctx := context.Background()
	store, _, anchor := openGenerationTestStore(t)
	config := testRepositoryConfig([]byte("independently-pinned-root-v1"))

	if _, err := store.CommitGeneration(ctx, config.ID, 0, anchor, generationTestRoles(2, 3, 4)); err != nil {
		t.Fatalf("CommitGeneration() error = %v", err)
	}
	rootV2 := []byte("verified-root-v2")
	if _, err := store.CommitVerifiedRoots(ctx, config.ID, anchor, []VerifiedRoot{
		{Version: 2, PreviousSHA256: anchor.SHA256, Metadata: rootV2},
	}); err != nil {
		t.Fatalf("CommitVerifiedRoots() error = %v", err)
	}

	if _, err := store.LoadOfflineSnapshot(ctx, config.ID); !errors.Is(err, ErrGenerationRootMismatch) {
		t.Fatalf("LoadOfflineSnapshot() error = %v, want ErrGenerationRootMismatch", err)
	}
}

func TestLoadOfflineSnapshotRejectsCorruptRootChain(t *testing.T) {
	ctx := context.Background()
	store, _, anchor := openGenerationTestStore(t)
	config := testRepositoryConfig([]byte("independently-pinned-root-v1"))

	rootV2 := []byte("verified-root-v2")
	latest, err := store.CommitVerifiedRoots(ctx, config.ID, anchor, []VerifiedRoot{
		{Version: 2, PreviousSHA256: anchor.SHA256, Metadata: rootV2},
	})
	if err != nil {
		t.Fatalf("CommitVerifiedRoots() error = %v", err)
	}
	if _, err := store.CommitGeneration(ctx, config.ID, 0, latest, generationTestRoles(2, 3, 4)); err != nil {
		t.Fatalf("CommitGeneration() error = %v", err)
	}
	if _, err := store.db.ExecContext(ctx, "DROP TRIGGER root_chain_no_update"); err != nil {
		t.Fatalf("drop immutable trigger for corruption fixture: %v", err)
	}
	if _, err := store.db.ExecContext(ctx,
		"UPDATE root_chain SET root_json = ? WHERE repository_id = ? AND version = 2",
		[]byte("tampered-root-v2"),
		config.ID,
	); err != nil {
		t.Fatalf("tamper root fixture: %v", err)
	}
	if _, err := store.LoadOfflineSnapshot(ctx, config.ID); !errors.Is(err, ErrCorruptTrustStore) {
		t.Fatalf("LoadOfflineSnapshot() error = %v, want ErrCorruptTrustStore", err)
	}
}
