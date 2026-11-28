//go:build darwin || linux

package sqlitetuf

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func validateTrustStoreParent(path string) error {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		return fmt.Errorf("el directorio padre debe ser una ruta absoluta")
	}

	// Walk from the filesystem root using directory descriptors. Every segment
	// is opened relative to the already-validated parent with O_NOFOLLOW, so a
	// symlink inserted into an ancestor cannot redirect a later path lookup.
	currentFD, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("abrir raíz del sistema de archivos: %w", err)
	}
	defer func() { _ = unix.Close(currentFD) }()

	relative := strings.TrimPrefix(clean, string(filepath.Separator))
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		if component == ".." {
			return fmt.Errorf("la ruta de directorio contiene un componente padre")
		}

		nextFD, openErr := unix.Openat(
			currentFD,
			component,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
			0,
		)
		if openErr != nil {
			return fmt.Errorf("abrir componente de directorio %q sin seguir enlaces: %w", component, openErr)
		}
		var stat unix.Stat_t
		statErr := unix.Fstat(nextFD, &stat)
		if statErr != nil {
			_ = unix.Close(nextFD)
			return fmt.Errorf("inspeccionar componente de directorio %q: %w", component, statErr)
		}
		if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
			_ = unix.Close(nextFD)
			return fmt.Errorf("el componente %q no es un directorio", component)
		}

		mode := uint32(stat.Mode)
		// A sticky shared directory such as /tmp is acceptable: the sticky bit
		// prevents another user from removing/replacing entries owned by this user.
		// Every ancestor matters, not just the immediate DB parent.
		if mode&0o022 != 0 && mode&uint32(unix.S_ISVTX) == 0 {
			_ = unix.Close(nextFD)
			return fmt.Errorf("el directorio ancestro %q permite escritura a grupo/otros sin sticky-bit", component)
		}

		_ = unix.Close(currentFD)
		currentFD = nextFD
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
