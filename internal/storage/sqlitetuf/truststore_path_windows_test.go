//go:build windows

package sqlitetuf

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func setEveryoneFullControl(t *testing.T, path string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;WD)")
	if err != nil {
		t.Fatalf("build broad writable DACL: %v", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatalf("read fixture DACL: %v", err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatalf("apply broad writable DACL fixture: %v", err)
	}
}

func TestOpenRejectsBroadWritableDirectoryDACL(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("create parent: %v", err)
	}
	setEveryoneFullControl(t, parent)
	if _, err := Open(context.Background(), filepath.Join(parent, "catalog-trust.sqlite")); err == nil {
		t.Fatal("Open() accepted a directory granting write access to Everyone")
	}
}

func TestOpenRejectsBroadWritableAncestorDACL(t *testing.T) {
	root := t.TempDir()
	ancestor := filepath.Join(root, "writable-ancestor")
	parent := filepath.Join(ancestor, "private")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatalf("create nested private parent: %v", err)
	}
	setEveryoneFullControl(t, ancestor)
	if _, err := Open(context.Background(), filepath.Join(parent, "catalog-trust.sqlite")); err == nil {
		t.Fatal("Open() accepted a private immediate parent beneath an Everyone-writable ancestor")
	}
}
