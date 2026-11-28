//go:build darwin || linux

package sqlitetuf

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenRejectsNonStickyGroupWritableParent(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("create parent: %v", err)
	}
	if err := os.Chmod(parent, 0o777); err != nil {
		t.Fatalf("chmod parent: %v", err)
	}
	if _, err := Open(context.Background(), filepath.Join(parent, "catalog-trust.sqlite")); err == nil {
		t.Fatal("Open() accepted a non-sticky group/other-writable parent")
	}
}

func TestOpenRejectsNonStickyWritableAncestorOfPrivateParent(t *testing.T) {
	root := t.TempDir()
	ancestor := filepath.Join(root, "writable-ancestor")
	parent := filepath.Join(ancestor, "private")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatalf("create nested private parent: %v", err)
	}
	if err := os.Chmod(ancestor, 0o777); err != nil {
		t.Fatalf("chmod ancestor: %v", err)
	}
	if _, err := Open(context.Background(), filepath.Join(parent, "catalog-trust.sqlite")); err == nil {
		t.Fatal("Open() accepted a private immediate parent beneath a non-sticky group/other-writable ancestor")
	}
}

func TestOpenAllowsStickySharedParentAndProtectsDatabaseFile(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "sticky")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("create parent: %v", err)
	}
	if err := os.Chmod(parent, os.ModeSticky|0o777); err != nil {
		t.Fatalf("chmod sticky parent: %v", err)
	}
	path := filepath.Join(parent, "catalog-trust.sqlite")
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open() under sticky parent: %v", err)
	}
	defer store.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat database: %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("database permissions = %o, want 0600 or stricter", info.Mode().Perm())
	}
}
