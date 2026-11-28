//go:build darwin || linux

package sqlitetuf

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func validateTrustStoreParent(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("la ruta padre debe ser un directorio real")
	}
	// A sticky shared directory such as /tmp is acceptable: the sticky bit
	// prevents another user from removing/replacing entries owned by this user.
	// A non-sticky group/other-writable parent can replace the DB and its journals.
	if info.Mode().Perm()&0o022 != 0 && info.Mode()&os.ModeSticky == 0 {
		return fmt.Errorf("el directorio padre permite escritura a grupo/otros sin sticky-bit")
	}
	return nil
}

func prepareTrustStoreFile(path string) error {
	flags := unix.O_RDWR | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
	fd, err := unix.Open(path, flags|unix.O_CREAT|unix.O_EXCL, 0o600)
	if errors.Is(err, unix.EEXIST) {
		fd, err = unix.Open(path, flags, 0)
	}
	if err != nil {
		return err
	}
	defer unix.Close(fd)

	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		return err
	}
	if info.Mode&unix.S_IFMT != unix.S_IFREG {
		return errTrustStorePathNotRegular
	}
	if err := unix.Fchmod(fd, 0o600); err != nil {
		return fmt.Errorf("proteger el archivo SQLite: %w", err)
	}
	return nil
}
