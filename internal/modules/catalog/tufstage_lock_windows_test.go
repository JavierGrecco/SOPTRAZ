//go:build windows

package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTUFOSLockRejectsWindowsReparsePoint(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.lock")
	link := filepath.Join(root, "link.lock")
	if err := os.WriteFile(target, []byte("not a lock"), 0o600); err != nil {
		t.Fatalf("create target file: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("creating a Windows symlink is not permitted in this environment: %v", err)
	}

	lock, locked, err := acquireTUFOSLock(link)
	if err == nil {
		if locked && lock != nil {
			_ = lock.Unlock()
		}
		t.Fatal("acquireTUFOSLock accepted a Windows reparse point")
	}

	targetLock, targetLocked, err := acquireTUFOSLock(target)
	if err != nil {
		t.Fatalf("lock regular target after reparse-point rejection: %v", err)
	}
	if !targetLocked || targetLock == nil {
		t.Fatal("expected to acquire direct target lock after reparse-point rejection")
	}
	if err := targetLock.Unlock(); err != nil {
		t.Fatalf("unlock target: %v", err)
	}
}
