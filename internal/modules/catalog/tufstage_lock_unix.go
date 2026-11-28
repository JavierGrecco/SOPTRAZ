//go:build !windows

package catalog

import (
	"github.com/gofrs/flock"
)

func acquireTUFOSLock(path string) (tufRepositoryLock, bool, error) {
	lock := flock.New(path, flock.SetFlag(0x40|0x2), flock.SetPermissions(0o600))
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
