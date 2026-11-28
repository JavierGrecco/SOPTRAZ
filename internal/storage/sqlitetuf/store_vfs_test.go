package sqlitetuf

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	sqlitevfs "modernc.org/sqlite/vfs"
)

// injectedSQLiteVFS is a test-only VFS backed by real files. It fails one
// journal write with vfs.ErrFull or one journal Sync with an I/O error, so
// SQLite exercises its own rollback path rather than a SQL trigger/page cap.
type injectedSQLiteVFS struct {
	root string

	mu                 sync.Mutex
	failJournalWrite   bool
	failJournalSync    bool
	journalWritesFailed int
	journalSyncsFailed  int
}

func (vfs *injectedSQLiteVFS) armJournalWriteFailure() {
	vfs.mu.Lock()
	defer vfs.mu.Unlock()
	vfs.failJournalWrite = true
}

func (vfs *injectedSQLiteVFS) armJournalSyncFailure() {
	vfs.mu.Lock()
	defer vfs.mu.Unlock()
	vfs.failJournalSync = true
}

func (vfs *injectedSQLiteVFS) consumeJournalWriteFailure(name string) bool {
	vfs.mu.Lock()
	defer vfs.mu.Unlock()
	if !strings.HasSuffix(name, "-journal") || !vfs.failJournalWrite {
		return false
	}
	vfs.failJournalWrite = false
	vfs.journalWritesFailed++
	return true
}

func (vfs *injectedSQLiteVFS) consumeJournalSyncFailure(name string) bool {
	vfs.mu.Lock()
	defer vfs.mu.Unlock()
	if !strings.HasSuffix(name, "-journal") || !vfs.failJournalSync {
		return false
	}
	vfs.failJournalSync = false
	vfs.journalSyncsFailed++
	return true
}

func (vfs *injectedSQLiteVFS) failureCounts() (writes, syncs int) {
	vfs.mu.Lock()
	defer vfs.mu.Unlock()
	return vfs.journalWritesFailed, vfs.journalSyncsFailed
}

func (vfs *injectedSQLiteVFS) resolve(name string) (string, error) {
	if strings.HasPrefix(name, "file:") {
		name = strings.TrimPrefix(name, "file:")
	}
	if runtime.GOOS == "windows" && len(name) >= 3 && name[0] == '/' && name[2] == ':' {
		name = name[1:]
	}
	if !filepath.IsAbs(name) {
		name = filepath.Join(vfs.root, name)
	}
	absolute, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(vfs.root, absolute)
	if err != nil {
		return "", err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("test VFS path escaped its private root")
	}
	return absolute, nil
}

func (vfs *injectedSQLiteVFS) FullPathname(name string) (string, error) {
	return vfs.resolve(name)
}

func (vfs *injectedSQLiteVFS) Open(name string, flags sqlitevfs.OpenFlag) (sqlitevfs.File, sqlitevfs.OpenFlag, error) {
	path, err := vfs.resolve(name)
	if err != nil {
		return nil, 0, err
	}
	openFlags := os.O_RDONLY
	if flags&sqlitevfs.OpenReadWrite != 0 {
		openFlags = os.O_RDWR
	}
	if flags&sqlitevfs.OpenCreate != 0 {
		openFlags |= os.O_CREATE
	}
	if flags&sqlitevfs.OpenExclusive != 0 {
		openFlags |= os.O_EXCL
	}
	file, err := os.OpenFile(path, openFlags, 0o600)
	if err != nil {
		return nil, 0, err
	}
	return &injectedSQLiteVFSFile{
		File:           file,
		owner:          vfs,
		path:           path,
		deleteOnClose:  flags&sqlitevfs.OpenDeleteOnClose != 0,
	}, flags, nil
}

func (vfs *injectedSQLiteVFS) Delete(name string, syncDir bool) error {
	path, err := vfs.resolve(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fs.ErrNotExist
		}
		return err
	}
	// Directory handles cannot be flushed portably by os.File on Windows.
	// The fault tests target journal WriteAt/Sync, not directory-sync behavior.
	if syncDir && runtime.GOOS != "windows" {
		directory, err := os.Open(filepath.Dir(path))
		if err != nil {
			return err
		}
		syncErr := directory.Sync()
		closeErr := directory.Close()
		return errors.Join(syncErr, closeErr)
	}
	return nil
}

func (vfs *injectedSQLiteVFS) Access(name string, flags sqlitevfs.AccessFlag) (bool, error) {
	path, err := vfs.resolve(name)
	if err != nil {
		return false, err
	}
	if flags == sqlitevfs.AccessExists {
		_, err := os.Stat(path)
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return err == nil, err
	}
	openFlags := os.O_RDONLY
	if flags == sqlitevfs.AccessReadWrite {
		openFlags = os.O_RDWR
	}
	file, err := os.OpenFile(path, openFlags, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_ = file.Close()
	return true, nil
}

type injectedSQLiteVFSFile struct {
	*os.File
	owner         *injectedSQLiteVFS
	path          string
	deleteOnClose bool
}

func (file *injectedSQLiteVFSFile) WriteAt(data []byte, offset int64) (int, error) {
	if file.owner.consumeJournalWriteFailure(file.path) {
		return 0, sqlitevfs.ErrFull
	}
	return file.File.WriteAt(data, offset)
}

func (file *injectedSQLiteVFSFile) Sync(_ sqlitevfs.SyncFlag) error {
	if file.owner.consumeJournalSyncFailure(file.path) {
		return errors.New("injected journal sync I/O failure")
	}
	return file.File.Sync()
}

func (file *injectedSQLiteVFSFile) Size() (int64, error) {
	info, err := file.File.Stat()
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func (file *injectedSQLiteVFSFile) Close() error {
	err := file.File.Close()
	if file.deleteOnClose {
		err = errors.Join(err, os.Remove(file.path))
	}
	return err
}

func openInjectedVFSStore(t *testing.T) (*Store, *injectedSQLiteVFS, RootState) {
	t.Helper()
	root := t.TempDir()
	faultVFS := &injectedSQLiteVFS{root: root}
	name, registration, err := sqlitevfs.Register(faultVFS)
	if err != nil {
		t.Fatalf("register test SQLite VFS: %v", err)
	}
	store, err := openWithVFS(context.Background(), filepath.Join(root, "catalog-trust.sqlite"), name)
	if err != nil {
		_ = registration.Close()
		t.Fatalf("open trust store with injected VFS: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close injected-VFS trust store: %v", err)
		}
		if err := registration.Close(); err != nil {
			t.Errorf("unregister injected SQLite VFS: %v", err)
		}
	})
	_, anchor := registerTestRepository(t, store)
	return store, faultVFS, anchor
}

func assertRootChainStillAtBootstrap(t *testing.T, store *Store, anchor RootState) {
	t.Helper()
	state, err := store.RootState(context.Background(), "soptraz-tools")
	if err != nil {
		t.Fatalf("read root state after injected storage failure: %v", err)
	}
	if state != anchor {
		t.Fatalf("root state after injected storage failure = %#v, want bootstrap %#v", state, anchor)
	}
	chain, loaded, err := store.LoadRootChain(context.Background(), "soptraz-tools")
	if err != nil {
		t.Fatalf("load root chain after injected storage failure: %v", err)
	}
	if len(chain) != 0 || loaded != anchor {
		t.Fatalf("partial root rotation survived injected storage failure: chain=%d state=%#v", len(chain), loaded)
	}
}

func TestCommitVerifiedRootsRollsBackWhenVFSJournalWriteReportsFull(t *testing.T) {
	store, faultVFS, anchor := openInjectedVFSStore(t)
	faultVFS.armJournalWriteFailure()

	_, err := store.CommitVerifiedRoots(context.Background(), "soptraz-tools", anchor, []VerifiedRoot{{
		Version:        anchor.Version + 1,
		PreviousSHA256: anchor.SHA256,
		Metadata:       []byte("verified-root-v2"),
	}})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "full") {
		t.Fatalf("CommitVerifiedRoots() error = %v, want SQLite FULL from journal write", err)
	}
	writes, _ := faultVFS.failureCounts()
	if writes != 1 {
		t.Fatalf("injected journal write failures = %d, want 1", writes)
	}
	assertRootChainStillAtBootstrap(t, store, anchor)
}

func TestCommitVerifiedRootsRollsBackWhenVFSJournalSyncFails(t *testing.T) {
	store, faultVFS, anchor := openInjectedVFSStore(t)
	faultVFS.armJournalSyncFailure()

	_, err := store.CommitVerifiedRoots(context.Background(), "soptraz-tools", anchor, []VerifiedRoot{{
		Version:        anchor.Version + 1,
		PreviousSHA256: anchor.SHA256,
		Metadata:       []byte("verified-root-v2"),
	}})
	if err == nil {
		t.Fatal("CommitVerifiedRoots() succeeded despite an injected journal sync failure")
	}
	_, syncs := faultVFS.failureCounts()
	if syncs != 1 {
		t.Fatalf("injected journal sync failures = %d, want 1", syncs)
	}
	assertRootChainStillAtBootstrap(t, store, anchor)
}
