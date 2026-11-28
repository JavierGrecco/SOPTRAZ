//go:build darwin || linux

package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTUFOSLockDoesNotFollowSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.lock")
	link := filepath.Join(root, "link.lock")
	if err := os.WriteFile(target, []byte("not a lock"), 0o600); err != nil {
		t.Fatalf("create target file: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	lock, locked, err := acquireTUFOSLock(link)
	if err == nil {
		if locked && lock != nil {
			_ = lock.Unlock()
		}
		t.Fatal("acquireTUFOSLock followed a symlink instead of rejecting it")
	}

	// The target remains a normal file and can be locked directly. This also
	// proves the rejected symlink attempt did not leave a lock on the target.
	targetLock, targetLocked, err := acquireTUFOSLock(target)
	if err != nil {
		t.Fatalf("lock target directly after symlink rejection: %v", err)
	}
	if !targetLocked || targetLock == nil {
		t.Fatal("expected to acquire direct target lock after symlink rejection")
	}
	if err := targetLock.Unlock(); err != nil {
		t.Fatalf("unlock target: %v", err)
	}
}
