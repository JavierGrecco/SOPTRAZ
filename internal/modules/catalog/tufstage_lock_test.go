package catalog

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/JavierGrecco/SOPTRAZ/internal/storage/sqlitetuf"
)

func openStageLockTestStore(t *testing.T, path string) *sqlitetuf.Store {
	t.Helper()
	store, err := sqlitetuf.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("open stage-lock SQLite store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close stage-lock SQLite store: %v", err)
		}
	})
	return store
}

func TestPersistentTUFRepositoryLockRejectsSecondWriter(t *testing.T) {
	store := openStageLockTestStore(t, filepath.Join(t.TempDir(), "catalog-trust.sqlite"))

	first, err := acquirePersistentTUFRepositoryLock(store, "repository-one")
	if err != nil {
		t.Fatalf("acquire first repository lock: %v", err)
	}
	defer first.Unlock()

	if _, err := acquirePersistentTUFRepositoryLock(store, "repository-one"); !errors.Is(err, ErrTUFRepositoryInUse) {
		t.Fatalf("second lock error = %v, want ErrTUFRepositoryInUse", err)
	}
}

func TestPersistentTUFRepositoryLocksAreScopedByRepositoryID(t *testing.T) {
	store := openStageLockTestStore(t, filepath.Join(t.TempDir(), "catalog-trust.sqlite"))

	first, err := acquirePersistentTUFRepositoryLock(store, "repository-one")
	if err != nil {
		t.Fatalf("acquire first repository lock: %v", err)
	}
	defer first.Unlock()

	second, err := acquirePersistentTUFRepositoryLock(store, "repository-two")
	if err != nil {
		t.Fatalf("different repository ID must have an independent lock: %v", err)
	}
	defer second.Unlock()
}

func TestCleanupOrphanedStagesOnlyRemovesStagesForLockedRepository(t *testing.T) {
	store := openStageLockTestStore(t, filepath.Join(t.TempDir(), "catalog-trust.sqlite"))
	cacheRoot := t.TempDir()

	const firstRepository = "repository-one"
	const secondRepository = "repository-two"
	firstStage, err := os.MkdirTemp(cacheRoot, tufMetadataStagePrefixForRepository(firstRepository)+"*")
	if err != nil {
		t.Fatalf("create first repository stage: %v", err)
	}
	secondStage, err := os.MkdirTemp(cacheRoot, tufMetadataStagePrefixForRepository(secondRepository)+"*")
	if err != nil {
		t.Fatalf("create second repository stage: %v", err)
	}
	unrelated, err := os.MkdirTemp(cacheRoot, ".soptraz-tuf-stage-unrelated-*")
	if err != nil {
		t.Fatalf("create unrelated directory: %v", err)
	}

	lock, err := acquirePersistentTUFRepositoryLock(store, firstRepository)
	if err != nil {
		t.Fatalf("acquire first repository lock: %v", err)
	}
	defer lock.Unlock()

	removed, err := cleanupOrphanedTUFStages(cacheRoot, lock, firstRepository)
	if err != nil {
		t.Fatalf("cleanup orphaned stages: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed stages = %d, want 1", removed)
	}
	for path, wantExists := range map[string]bool{
		firstStage:  false,
		secondStage: true,
		unrelated:   true,
	} {
		_, err := os.Stat(path)
		if wantExists && err != nil {
			t.Errorf("expected %q to remain, stat error = %v", path, err)
		}
		if !wantExists && !errors.Is(err, os.ErrNotExist) {
			t.Errorf("expected %q to be removed, stat error = %v", path, err)
		}
	}
}

func TestCleanupOrphanedStagesRequiresExclusiveRepositoryLock(t *testing.T) {
	store := openStageLockTestStore(t, filepath.Join(t.TempDir(), "catalog-trust.sqlite"))
	cacheRoot := t.TempDir()
	if _, err := os.MkdirTemp(cacheRoot, tufMetadataStagePrefixForRepository("repository-one")+"*"); err != nil {
		t.Fatalf("create orphan stage: %v", err)
	}

	if _, err := cleanupOrphanedTUFStages(cacheRoot, nil, "repository-one"); err == nil {
		t.Fatal("cleanup succeeded without a repository lock")
	}
}

func TestTUFRepositoryLockHelperProcess(t *testing.T) {
	if os.Getenv("SOPTRAZ_TUF_LOCK_HELPER") != "1" {
		return
	}
	storePath := os.Getenv("SOPTRAZ_TUF_LOCK_STORE")
	cacheRoot := os.Getenv("SOPTRAZ_TUF_LOCK_CACHE")
	repositoryID := "abrupt-exit-repository"

	store, err := sqlitetuf.Open(context.Background(), storePath)
	if err != nil {
		os.Exit(91)
	}
	repositoryLock, err := acquirePersistentTUFRepositoryLock(store, repositoryID)
	if err != nil || !repositoryLock.Locked() {
		os.Exit(92)
	}
	if _, err := os.MkdirTemp(cacheRoot, tufMetadataStagePrefixForRepository(repositoryID)+"*"); err != nil {
		os.Exit(93)
	}

	// Keep the lock reachable until process exit; deliberately skip Close/Unlock.
	runtime.KeepAlive(repositoryLock)
	os.Exit(0)
}

func TestTUFRepositoryLockAndOrphanCleanupAfterAbruptProcessExit(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "catalog-trust.sqlite")
	cacheRoot := filepath.Join(root, "metadata")
	if err := os.MkdirAll(cacheRoot, 0o700); err != nil {
		t.Fatalf("create cache root: %v", err)
	}

	command := exec.Command(os.Args[0], "-test.run=^TestTUFRepositoryLockHelperProcess$")
	command.Env = append(os.Environ(),
		"SOPTRAZ_TUF_LOCK_HELPER=1",
		"SOPTRAZ_TUF_LOCK_STORE="+storePath,
		"SOPTRAZ_TUF_LOCK_CACHE="+cacheRoot,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("helper process failed: %v; output=%s", err, output)
	}

	store := openStageLockTestStore(t, storePath)
	lock, err := acquirePersistentTUFRepositoryLock(store, "abrupt-exit-repository")
	if err != nil {
		t.Fatalf("OS did not release repository lock after process exit: %v", err)
	}
	defer lock.Unlock()

	removed, err := cleanupOrphanedTUFStages(cacheRoot, lock, "abrupt-exit-repository")
	if err != nil {
		t.Fatalf("cleanup after abrupt process exit: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed stages after abrupt exit = %d, want 1", removed)
	}
	entries, err := os.ReadDir(cacheRoot)
	if err != nil {
		t.Fatalf("read cache root after cleanup: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("orphan stages remain after cleanup: %v", entries)
	}
}
