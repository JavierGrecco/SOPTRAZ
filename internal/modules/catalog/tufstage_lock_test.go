package catalog

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

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

func TestCleanupOrphanedStagesRejectsLockForDifferentRepository(t *testing.T) {
	store := openStageLockTestStore(t, filepath.Join(t.TempDir(), "catalog-trust.sqlite"))
	cacheRoot := t.TempDir()
	firstStage, err := os.MkdirTemp(cacheRoot, tufMetadataStagePrefixForRepository("repository-one")+"*")
	if err != nil {
		t.Fatalf("create first repository stage: %v", err)
	}
	if _, err := os.MkdirTemp(cacheRoot, tufMetadataStagePrefixForRepository("repository-two")+"*"); err != nil {
		t.Fatalf("create second repository stage: %v", err)
	}
	lock, err := acquirePersistentTUFRepositoryLock(store, "repository-one")
	if err != nil {
		t.Fatalf("acquire first repository lock: %v", err)
	}
	defer lock.Unlock()

	if _, err := cleanupOrphanedTUFStages(cacheRoot, lock, "repository-two"); err == nil {
		t.Fatal("cleanup accepted a lock owned by another repository")
	}
	if _, err := os.Stat(firstStage); err != nil {
		t.Fatalf("mismatched cleanup changed the first repository stage: %v", err)
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
	readyPath := os.Getenv("SOPTRAZ_TUF_LOCK_READY")
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
	if err := os.WriteFile(readyPath, []byte("LOCK_READY"), 0o600); err != nil {
		os.Exit(94)
	}

	// Keep the lock and SQLite connection alive. The parent forcibly terminates
	// this process to model kill/fatal shutdown without Close or Unlock.
	done := make(chan struct{})
	<-done
	runtime.KeepAlive(repositoryLock)
}

func TestTUFRepositoryLockAndOrphanCleanupAfterAbruptProcessExit(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "catalog-trust.sqlite")
	cacheRoot := filepath.Join(root, "metadata")
	readyPath := filepath.Join(root, "ready")
	if err := os.MkdirAll(cacheRoot, 0o700); err != nil {
		t.Fatalf("create cache root: %v", err)
	}
	store := openStageLockTestStore(t, storePath)

	command := exec.Command(os.Args[0], "-test.run=^TestTUFRepositoryLockHelperProcess$")
	command.Env = append(os.Environ(),
		"SOPTRAZ_TUF_LOCK_HELPER=1",
		"SOPTRAZ_TUF_LOCK_STORE="+storePath,
		"SOPTRAZ_TUF_LOCK_CACHE="+cacheRoot,
		"SOPTRAZ_TUF_LOCK_READY="+readyPath,
	)
	if err := command.Start(); err != nil {
		t.Fatalf("start lock-owning process: %v", err)
	}
	processExited := false
	defer func() {
		if !processExited {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()

	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(readyPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child process did not acquire the repository lock in time")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := acquirePersistentTUFRepositoryLock(store, "abrupt-exit-repository"); !errors.Is(err, ErrTUFRepositoryInUse) {
		t.Fatalf("second process lock error = %v, want ErrTUFRepositoryInUse", err)
	}

	if err := command.Process.Kill(); err != nil {
		t.Fatalf("kill lock-owning process: %v", err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("forcibly terminated helper process exited successfully")
	}
	processExited = true

	lock, err := acquirePersistentTUFRepositoryLock(store, "abrupt-exit-repository")
	if err != nil {
		t.Fatalf("OS did not release repository lock after abrupt process exit: %v", err)
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