//go:build !windows

package catalog

import (
	"os"

	"github.com/gofrs/flock"
)

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
