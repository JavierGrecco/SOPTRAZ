//go:build windows

package catalog

import (
	"errors"
	"sync"

	"golang.org/x/sys/windows"
)

// windowsTUFRepositoryLock uses an exclusive Windows file-open share mode.
// Unlike a byte-range lock, shareMode=0 prevents another process from opening
// the lock file at all. The kernel releases this handle after any process exit.
type windowsTUFRepositoryLock struct {
	mu     sync.Mutex
	handle windows.Handle
	path   string
	locked bool
}

func (lock *windowsTUFRepositoryLock) Path() string {
	if lock == nil {
		return ""
	}
	return lock.path
}

func (lock *windowsTUFRepositoryLock) Locked() bool {
	if lock == nil {
		return false
	}
	lock.mu.Lock()
	defer lock.mu.Unlock()
	return lock.locked
}

func (lock *windowsTUFRepositoryLock) Unlock() error {
	if lock == nil {
		return nil
	}
	lock.mu.Lock()
	defer lock.mu.Unlock()
	if !lock.locked {
		return nil
	}
	if err := windows.CloseHandle(lock.handle); err != nil {
		return err
	}
	lock.handle = 0
	lock.locked = false
	return nil
}

func acquireTUFOSLock(path string) (tufRepositoryLock, bool, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, false, err
	}
	handle, err := windows.CreateFile(
		name,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0, // deny read/write/delete sharing while this process owns the lock
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		if errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return &windowsTUFRepositoryLock{
		handle: handle,
		path:   path,
		locked: true,
	}, true, nil
}
