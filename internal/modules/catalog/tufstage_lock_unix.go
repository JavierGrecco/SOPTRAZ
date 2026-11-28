//go:build darwin || linux

package catalog

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"golang.org/x/sys/unix"
)

// unixTUFRepositoryLock keeps the descriptor used by flock alive for the
// entire lifetime of the persistent TUF client. O_NOFOLLOW closes the gap
// between the caller's path inspection and the actual open operation.
type unixTUFRepositoryLock struct {
	mu     sync.Mutex
	file   *os.File
	path   string
	locked bool
}

func (lock *unixTUFRepositoryLock) Path() string {
	if lock == nil {
		return ""
	}
	return lock.path
}

func (lock *unixTUFRepositoryLock) Locked() bool {
	if lock == nil {
		return false
	}
	lock.mu.Lock()
	defer lock.mu.Unlock()
	return lock.locked
}

func (lock *unixTUFRepositoryLock) Unlock() error {
	if lock == nil {
		return nil
	}
	lock.mu.Lock()
	defer lock.mu.Unlock()
	if !lock.locked {
		return nil
	}

	var unlockErr error
	if lock.file != nil {
		unlockErr = unix.Flock(int(lock.file.Fd()), unix.LOCK_UN)
		closeErr := lock.file.Close()
		lock.file = nil
		lock.locked = false
		return errors.Join(unlockErr, closeErr)
	}
	lock.locked = false
	return unlockErr
}

func acquireTUFOSLock(path string) (tufRepositoryLock, bool, error) {
	// O_NOFOLLOW is enforced by the kernel on the final path component. The
	// non-blocking open also prevents a substituted FIFO from hanging startup;
	// fstat below rejects every non-regular object before flock is attempted.
	fd, err := unix.Open(
		path,
		unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK,
		0o600,
	)
	if err != nil {
		return nil, false, err
	}

	closeFD := func() {
		_ = unix.Close(fd)
	}
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		closeFD()
		return nil, false, fmt.Errorf("inspeccionar descriptor del lock TUF: %w", err)
	}
	if info.Mode&unix.S_IFMT != unix.S_IFREG {
		closeFD()
		return nil, false, fmt.Errorf("el lock TUF abierto no es un archivo regular")
	}
	if err := unix.Fchmod(fd, 0o600); err != nil {
		closeFD()
		return nil, false, fmt.Errorf("proteger archivo de lock TUF: %w", err)
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		closeFD()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, false, nil
		}
		return nil, false, err
	}

	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Flock(fd, unix.LOCK_UN)
		closeFD()
		return nil, false, fmt.Errorf("no se pudo envolver el descriptor del lock TUF")
	}
	return &unixTUFRepositoryLock{
		file:   file,
		path:   path,
		locked: true,
	}, true, nil
}
