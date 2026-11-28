//go:build !windows && !darwin && !linux

package catalog

import (
	"os"

	"github.com/gofrs/flock"
)

// Keep the portable implementation for less common GOOS targets. The
// supported desktop targets (Linux and macOS) use O_NOFOLLOW in the
// platform-specific implementation.
func acquireTUFOSLock(path string) (tufRepositoryLock, bool, error) {
	lock := flock.New(path, flock.SetFlag(os.O_CREATE|os.O_RDWR), flock.SetPermissions(0o600))
	locked, err := lock.TryLock()
	if err != nil {
		_ = lock.Unlock()
		return nil, false, err
	}
	if !locked {
		_ = lock.Unlock()
		return nil, false, nil
	}
	return lock, true, nil
}
