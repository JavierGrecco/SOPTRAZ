package sqlitetuf

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func generationTestRoles(timestampVersion, snapshotVersion, targetsVersion int64) []VerifiedRoleMetadata {
	expiration := time.Date(2035, time.March, 20, 12, 0, 0, 0, time.UTC)
	return []VerifiedRoleMetadata{
		{
			Role:      "timestamp",
			Version:   timestampVersion,
			ExpiresAt: expiration,
			Metadata:  []byte(fmt.Sprintf("timestamp-v%d", timestampVersion)),
		},
		{
			Role:      "snapshot",
			Version:   snapshotVersion,
			ExpiresAt: expiration,
			Metadata:  []byte(fmt.Sprintf("snapshot-v%d", snapshotVersion)),
		},
		{
			Role:      "targets",
			Version:   targetsVersion,
			ExpiresAt: expiration,
			Metadata:  []byte(fmt.Sprintf("targets-v%d", targetsVersion)),
		},
	}
}

func openGenerationTestStore(t *testing.T) (*Store, string, RootState) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "catalog-trust.sqlite")
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	_, anchor := registerTestRepository(t, store)
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return store, path, anchor
}

func TestCommitAndLoadActiveGenerationAfterReopen(t *testing.T) {
	ctx := context.Background()
	store, path, anchor := openGenerationTestStore(t)

	roles := append(generationTestRoles(4, 7, 9),
		VerifiedRoleMetadata{
			Role:      "z-parent",
			Parents:   []string{"targets"},
			Version:   2,
			ExpiresAt: time.Date(2035, time.March, 20, 12, 0, 0, 0, time.UTC),
			Metadata:  []byte("delegated-parent-v2"),
		},
		VerifiedRoleMetadata{
			Role:      "a-child",
			Parents:   []string{"z-parent"},
			Version:   3,
			ExpiresAt: time.Date(2035, time.March, 20, 12, 0, 0, 0, time.UTC),
			Metadata:  []byte("delegated-child-v3"),
		},
	)

	generation, err := store.CommitGeneration(ctx, "soptraz-tools", 0, anchor, roles)
	if err != nil {
		t.Fatalf("CommitGeneration() error = %v", err)
	}
	if generation.ID != 1 || generation.ParentID != 0 || generation.Root != anchor || len(generation.Roles) != 5 {
		t.Fatalf("committed generation = %#v", generation)
	}
	if generation.ManifestSHA256 == "" {
		t.Fatal("CommitGeneration() returned empty manifest digest")
	}
	activeID, err := store.ActiveGenerationID(ctx, "soptraz-tools")
	if err != nil {
		t.Fatalf("ActiveGenerationID() error = %v", err)
	}
	if activeID != generation.ID {
		t.Fatalf("active generation ID = %d, want %d", activeID, generation.ID)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() before reopen error = %v", err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open() after restart error = %v", err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Errorf("reopened Close() error = %v", err)
		}
	})

	loaded, err := reopened.LoadActiveGeneration(ctx, "soptraz-tools")
	if err != nil {
		t.Fatalf("LoadActiveGeneration() after reopen error = %v", err)
	}
	if loaded.ID != generation.ID ||
		loaded.ParentID != generation.ParentID ||
		loaded.Root != anchor ||
		loaded.ManifestSHA256 != generation.ManifestSHA256 ||
		len(loaded.Roles) != len(roles) {
		t.Fatalf("loaded generation = %#v, want committed generation %#v", loaded, generation)
	}
	parents := make(map[string][]string)
	for _, role := range loaded.Roles {
		parents[role.Role] = role.Parents
	}
	if len(parents["a-child"]) != 1 || parents["a-child"][0] != "z-parent" ||
		len(parents["z-parent"]) != 1 || parents["z-parent"][0] != "targets" {
		t.Fatalf("delegation relationships after reopen = %#v", parents)
	}
}

func TestCommitGenerationRejectsRoleRollbackAndEquivocationAtomically(t *testing.T) {
	ctx := context.Background()
	store, _, anchor := openGenerationTestStore(t)

	firstRoles := generationTestRoles(10, 20, 30)
	first, err := store.CommitGeneration(ctx, "soptraz-tools", 0, anchor, firstRoles)
	if err != nil {
		t.Fatalf("first CommitGeneration() error = %v", err)
	}

	lowerRoles := generationTestRoles(9, 21, 31)
	if _, err := store.CommitGeneration(ctx, "soptraz-tools", first.ID, anchor, lowerRoles); !errors.Is(err, ErrRoleRollback) {
		t.Fatalf("lower-version CommitGeneration() error = %v, want ErrRoleRollback", err)
	}

	equivocating := generationTestRoles(10, 21, 31)
	equivocating[0].Metadata = []byte("timestamp-v10-with-different-bytes")
	if _, err := store.CommitGeneration(ctx, "soptraz-tools", first.ID, anchor, equivocating); !errors.Is(err, ErrRoleEquivocation) {
		t.Fatalf("same-version/different-bytes CommitGeneration() error = %v, want ErrRoleEquivocation", err)
	}

	active, err := store.LoadActiveGeneration(ctx, "soptraz-tools")
	if err != nil {
		t.Fatalf("LoadActiveGeneration() after rejected generations error = %v", err)
	}
	if active.ID != first.ID || active.ManifestSHA256 != first.ManifestSHA256 {
		t.Fatalf("rejected generation changed active state: got ID=%d manifest=%s", active.ID, active.ManifestSHA256)
	}
}

func TestCommitGenerationAllowsSameVersionSameBytesAndAdvancesActiveCAS(t *testing.T) {
	ctx := context.Background()
	store, _, anchor := openGenerationTestStore(t)
	roles := generationTestRoles(2, 3, 4)

	first, err := store.CommitGeneration(ctx, "soptraz-tools", 0, anchor, roles)
	if err != nil {
		t.Fatalf("first CommitGeneration() error = %v", err)
	}
	if _, err := store.CommitGeneration(ctx, "soptraz-tools", 0, anchor, roles); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("stale active generation commit error = %v, want ErrGenerationConflict", err)
	}

	second, err := store.CommitGeneration(ctx, "soptraz-tools", first.ID, anchor, roles)
	if err != nil {
		t.Fatalf("same-version/same-bytes CommitGeneration() error = %v", err)
	}
	if second.ID != first.ID+1 || second.ParentID != first.ID {
		t.Fatalf("second generation = %#v, want child of generation %d", second, first.ID)
	}
	active, err := store.LoadActiveGeneration(ctx, "soptraz-tools")
	if err != nil {
		t.Fatalf("LoadActiveGeneration() error = %v", err)
	}
	if active.ID != second.ID {
		t.Fatalf("active ID = %d, want %d", active.ID, second.ID)
	}
}

func TestCommitGenerationRequiresCurrentRootAndBlocksOldActiveGeneration(t *testing.T) {
	ctx := context.Background()
	store, _, anchor := openGenerationTestStore(t)
	first, err := store.CommitGeneration(ctx, "soptraz-tools", 0, anchor, generationTestRoles(2, 2, 2))
	if err != nil {
		t.Fatalf("initial CommitGeneration() error = %v", err)
	}

	rootV2 := []byte("verified-root-v2")
	newRoot, err := store.CommitVerifiedRoots(ctx, "soptraz-tools", anchor, []VerifiedRoot{
		{Version: 2, PreviousSHA256: anchor.SHA256, Metadata: rootV2},
	})
	if err != nil {
		t.Fatalf("CommitVerifiedRoots() error = %v", err)
	}
	if _, err := store.CommitGeneration(ctx, "soptraz-tools", first.ID, anchor, generationTestRoles(3, 3, 3)); !errors.Is(err, ErrGenerationRootMismatch) {
		t.Fatalf("old-root generation commit error = %v, want ErrGenerationRootMismatch", err)
	}
	if _, err := store.LoadActiveGeneration(ctx, "soptraz-tools"); !errors.Is(err, ErrGenerationRootMismatch) {
		t.Fatalf("LoadActiveGeneration() after root advance error = %v, want ErrGenerationRootMismatch", err)
	}

	second, err := store.CommitGeneration(ctx, "soptraz-tools", first.ID, newRoot, generationTestRoles(3, 3, 3))
	if err != nil {
		t.Fatalf("CommitGeneration() under latest root: %v", err)
	}
	if second.Root != newRoot {
		t.Fatalf("new generation root = %#v, want %#v", second.Root, newRoot)
	}
}

func TestCommitGenerationRejectsIncompleteAndInvalidDelegationSets(t *testing.T) {
	ctx := context.Background()
	store, _, anchor := openGenerationTestStore(t)

	incomplete := generationTestRoles(1, 1, 1)[:2]
	if _, err := store.CommitGeneration(ctx, "soptraz-tools", 0, anchor, incomplete); !errors.Is(err, ErrGenerationIncomplete) {
		t.Fatalf("incomplete generation error = %v, want ErrGenerationIncomplete", err)
	}

	unknownParent := append(generationTestRoles(1, 1, 1), VerifiedRoleMetadata{
		Role:      "child",
		Parents:   []string{"missing-parent"},
		Version:   1,
		ExpiresAt: time.Now().Add(24 * time.Hour),
		Metadata:  []byte("child"),
	})
	if _, err := store.CommitGeneration(ctx, "soptraz-tools", 0, anchor, unknownParent); !errors.Is(err, ErrInvalidGeneration) {
		t.Fatalf("unknown-parent generation error = %v, want ErrInvalidGeneration", err)
	}

	cycle := append(generationTestRoles(1, 1, 1),
		VerifiedRoleMetadata{Role: "role-a", Parents: []string{"role-b"}, Version: 1, ExpiresAt: time.Now().Add(time.Hour), Metadata: []byte("a")},
		VerifiedRoleMetadata{Role: "role-b", Parents: []string{"role-a"}, Version: 1, ExpiresAt: time.Now().Add(time.Hour), Metadata: []byte("b")},
	)
	if _, err := store.CommitGeneration(ctx, "soptraz-tools", 0, anchor, cycle); !errors.Is(err, ErrInvalidGeneration) {
		t.Fatalf("cyclic generation error = %v, want ErrInvalidGeneration", err)
	}

	duplicate := append(generationTestRoles(1, 1, 1), VerifiedRoleMetadata{
		Role:      "targets",
		Version:   1,
		ExpiresAt: time.Now().Add(time.Hour),
		Metadata:  []byte("duplicate"),
	})
	if _, err := store.CommitGeneration(ctx, "soptraz-tools", 0, anchor, duplicate); !errors.Is(err, ErrInvalidGeneration) {
		t.Fatalf("duplicate-role generation error = %v, want ErrInvalidGeneration", err)
	}
}

func TestGenerationCommitRollsBackAllRowsWhenMetadataWriteFails(t *testing.T) {
	ctx := context.Background()
	store, _, anchor := openGenerationTestStore(t)

	if _, err := store.db.ExecContext(ctx,
		"CREATE TRIGGER reject_targets_insert BEFORE INSERT ON generation_metadata "+
			"WHEN NEW.role_name = 'targets' BEGIN SELECT RAISE(ABORT, 'simulated storage failure'); END",
	); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}
	if _, err := store.CommitGeneration(ctx, "soptraz-tools", 0, anchor, generationTestRoles(2, 2, 2)); err == nil {
		t.Fatal("CommitGeneration() succeeded despite forced metadata write failure")
	}

	activeID, err := store.ActiveGenerationID(ctx, "soptraz-tools")
	if err != nil {
		t.Fatalf("ActiveGenerationID() after failed commit: %v", err)
	}
	if activeID != 0 {
		t.Fatalf("active generation after failed transaction = %d, want 0", activeID)
	}
	var generations, metadataRows, highWaterRows int
	for query, destination := range map[string]*int{
		"SELECT COUNT(*) FROM generations WHERE repository_id = ?":         &generations,
		"SELECT COUNT(*) FROM generation_metadata WHERE repository_id = ?": &metadataRows,
		"SELECT COUNT(*) FROM role_high_water WHERE repository_id = ?":     &highWaterRows,
	} {
		if err := store.db.QueryRowContext(ctx, query, "soptraz-tools").Scan(destination); err != nil {
			t.Fatalf("count rows after failed commit: %v", err)
		}
	}
	if generations != 0 || metadataRows != 0 || highWaterRows != 0 {
		t.Fatalf("partial generation escaped rollback: generations=%d metadata=%d high-water=%d",
			generations, metadataRows, highWaterRows)
	}
}

func TestLoadActiveGenerationDetectsStoredMetadataTampering(t *testing.T) {
	ctx := context.Background()
	store, _, anchor := openGenerationTestStore(t)
	generation, err := store.CommitGeneration(ctx, "soptraz-tools", 0, anchor, generationTestRoles(1, 1, 1))
	if err != nil {
		t.Fatalf("CommitGeneration() error = %v", err)
	}
	if _, err := store.db.ExecContext(ctx, "DROP TRIGGER generation_metadata_no_update"); err != nil {
		t.Fatalf("drop immutable trigger for corruption fixture: %v", err)
	}
	if _, err := store.db.ExecContext(ctx,
		"UPDATE generation_metadata SET metadata_json = ? WHERE repository_id = ? AND generation_id = ? AND role_name = ?",
		[]byte("tampered-timestamp"),
		"soptraz-tools",
		generation.ID,
		"timestamp",
	); err != nil {
		t.Fatalf("tamper generation fixture: %v", err)
	}
	if _, err := store.LoadActiveGeneration(ctx, "soptraz-tools"); !errors.Is(err, ErrGenerationCorrupt) {
		t.Fatalf("LoadActiveGeneration() error = %v, want ErrGenerationCorrupt", err)
	}
}

func TestOpenMigratesExistingVersionOneTrustDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy-trust.sqlite")
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatalf("sql.Open() legacy database: %v", err)
	}
	statements := []string{
		"CREATE TABLE repositories (" +
			"id TEXT PRIMARY KEY CHECK(length(id) BETWEEN 1 AND 128), " +
			"metadata_url TEXT NOT NULL CHECK(length(metadata_url) BETWEEN 1 AND 2048), " +
			"anchor_version INTEGER NOT NULL CHECK(anchor_version >= 1), " +
			"anchor_sha256 TEXT NOT NULL CHECK(length(anchor_sha256) = 64), " +
			"latest_root_version INTEGER NOT NULL CHECK(latest_root_version >= anchor_version), " +
			"latest_root_sha256 TEXT NOT NULL CHECK(length(latest_root_sha256) = 64), " +
			"created_at TEXT NOT NULL)",
		"CREATE TABLE root_chain (" +
			"repository_id TEXT NOT NULL, version INTEGER NOT NULL, previous_sha256 TEXT NOT NULL, " +
			"sha256 TEXT NOT NULL, root_json BLOB NOT NULL, committed_at TEXT NOT NULL, " +
			"PRIMARY KEY(repository_id, version), UNIQUE(repository_id, sha256), " +
			"FOREIGN KEY(repository_id) REFERENCES repositories(id))",
		"CREATE TRIGGER root_chain_no_update BEFORE UPDATE ON root_chain " +
			"BEGIN SELECT RAISE(ABORT, 'SOPTRAZ TUF roots are immutable'); END",
		"CREATE TRIGGER root_chain_no_delete BEFORE DELETE ON root_chain " +
			"BEGIN SELECT RAISE(ABORT, 'SOPTRAZ TUF roots are append-only'); END",
		"CREATE INDEX idx_root_chain_digest ON root_chain(repository_id, sha256)",
		"INSERT INTO repositories(id, metadata_url, anchor_version, anchor_sha256, latest_root_version, latest_root_sha256, created_at) "+
			"VALUES('soptraz-tools', 'https://metadata.example.test/repository', 1, ?, 1, ?, '2026-11-28T00:00:00Z')",
		"PRAGMA user_version = 1",
	}
	anchorSHA := digestHex([]byte("legacy-bootstrap"))
	for index, statement := range statements {
		if index == 5 {
			if _, err := db.ExecContext(ctx, statement, anchorSHA, anchorSHA); err != nil {
				_ = db.Close()
				t.Fatalf("insert legacy repository: %v", err)
			}
			continue
		}
		if _, err := db.ExecContext(ctx, statement); err != nil {
			_ = db.Close()
			t.Fatalf("apply legacy schema statement %d: %v", index+1, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open() should migrate v1 database: %v", err)
	}
	defer store.Close()

	var schema int
	if err := store.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&schema); err != nil {
		t.Fatalf("read migrated schema version: %v", err)
	}
	if schema != 2 {
		t.Fatalf("schema version = %d, want 2", schema)
	}
	state, err := store.RootState(ctx, "soptraz-tools")
	if err != nil {
		t.Fatalf("RootState() after v2 migration: %v", err)
	}
	if state.Version != 1 || state.SHA256 != anchorSHA {
		t.Fatalf("root state lost by migration: %#v", state)
	}
	activeID, err := store.ActiveGenerationID(ctx, "soptraz-tools")
	if err != nil {
		t.Fatalf("ActiveGenerationID() after migration: %v", err)
	}
	if activeID != 0 {
		t.Fatalf("legacy repository unexpectedly has active generation %d", activeID)
	}
}
