package sqlitetuf

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenRejectsDatabaseSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.sqlite")
	link := filepath.Join(root, "catalog-trust.sqlite")
	if err := os.WriteFile(target, []byte("must remain untouched"), 0o600); err != nil {
		t.Fatalf("create target: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("creating a symlink is not permitted in this environment: %v", err)
	}
	if _, err := Open(context.Background(), link); err == nil {
		t.Fatal("Open() accepted a database symlink")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read symlink target: %v", err)
	}
	if string(got) != "must remain untouched" {
		t.Fatalf("symlink target was modified: %q", got)
	}
}

func TestOpenRejectsDatabaseDirectory(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "catalog-trust.sqlite")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("create directory at database path: %v", err)
	}
	if _, err := Open(context.Background(), path); err == nil {
		t.Fatal("Open() accepted a directory as SQLite database")
	}
}

func TestOpenResolvesParentDirectoryAlias(t *testing.T) {
	root := t.TempDir()
	realParent := filepath.Join(root, "real")
	aliasParent := filepath.Join(root, "alias")
	if err := os.Mkdir(realParent, 0o700); err != nil {
		t.Fatalf("create real parent: %v", err)
	}
	if err := os.Symlink(realParent, aliasParent); err != nil {
		t.Skipf("creating a directory symlink is not permitted in this environment: %v", err)
	}
	store, err := Open(context.Background(), filepath.Join(aliasParent, "catalog-trust.sqlite"))
	if err != nil {
		t.Fatalf("Open() through directory alias: %v", err)
	}
	defer store.Close()
	want := filepath.Join(realParent, "catalog-trust.sqlite")
	if store.DatabasePath() != want {
		t.Fatalf("DatabasePath() = %q, want canonical path %q", store.DatabasePath(), want)
	}
}
