package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/JavierGrecco/SOPTRAZ/internal/storage/sqlitetuf"
)

const (
	tufMetadataStagePrefix = ".soptraz-tuf-stage-"
	tufRepositoryLockDir   = ".soptraz-locks"
)

type tufRepositoryLock interface {
	Locked() bool
	Path() string
	Unlock() error
}

func tufMetadataStagePrefixForRepository(repositoryID string) string {
	digest := sha256.Sum256([]byte(repositoryID))
	return tufMetadataStagePrefix + hex.EncodeToString(digest[:]) + "-"
}

// acquirePersistentTUFRepositoryLock serializes persistent clients across
// processes and across different metadata-cache paths that share one SQLite
// trust store. The lock file is stable: it is deliberately never unlinked,
// because unlinking a locked file could let a second process lock a new inode.
func acquirePersistentTUFRepositoryLock(store *sqlitetuf.Store, repositoryID string) (tufRepositoryLock, error) {
	if store == nil || store.DatabasePath() == "" {
		return nil, fmt.Errorf("%w: la ruta de SQLite no está disponible", ErrInvalidTUFClientConfig)
	}
	if strings.TrimSpace(repositoryID) == "" || len(repositoryID) > 128 {
		return nil, fmt.Errorf("%w: repositoryID inválido", ErrInvalidTUFClientConfig)
	}

	lockDir := filepath.Join(filepath.Dir(store.DatabasePath()), tufRepositoryLockDir)
	if err := os.MkdirAll(lockDir, 0o700); err != nil {
		return nil, fmt.Errorf("crear carpeta privada de locks TUF: %w", err)
	}
	dirInfo, err := os.Lstat(lockDir)
	if err != nil {
		return nil, fmt.Errorf("inspeccionar carpeta de locks TUF: %w", err)
	}
	if dirInfo.Mode()&os.ModeSymlink != 0 || !dirInfo.IsDir() {
		return nil, fmt.Errorf("la carpeta de locks TUF debe ser un directorio real")
	}
	if err := os.Chmod(lockDir, 0o700); err != nil {
		return nil, fmt.Errorf("proteger carpeta de locks TUF: %w", err)
	}

	repositoryDigest := sha256.Sum256([]byte(repositoryID))
	lockPath := filepath.Join(lockDir, hex.EncodeToString(repositoryDigest[:])+".lock")
	return acquireTUFFileLock(lockPath)
}

// acquireTUFFileLock takes a non-blocking OS lock on a regular file. The lock
// is released by the operating system when the owning process exits, including
// after an abrupt termination. The lock file itself remains on disk.
func acquireTUFFileLock(lockPath string) (tufRepositoryLock, error) {
	info, err := os.Lstat(lockPath)
	if errors.Is(err, os.ErrNotExist) {
		file, createErr := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if createErr != nil && !errors.Is(createErr, os.ErrExist) {
			return nil, fmt.Errorf("crear archivo de lock TUF: %w", createErr)
		}
		if createErr == nil {
			if closeErr := file.Close(); closeErr != nil {
				return nil, fmt.Errorf("cerrar archivo de lock TUF: %w", closeErr)
			}
		}
		info, err = os.Lstat(lockPath)
	}
	if err != nil {
		return nil, fmt.Errorf("inspeccionar archivo de lock TUF: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("el archivo de lock TUF debe ser un archivo regular, no un symlink ni un archivo especial")
	}

	lock, locked, err := acquireTUFOSLock(lockPath)
	if err != nil {
		return nil, fmt.Errorf("adquirir lock TUF: %w", err)
	}
	if !locked {
		return nil, fmt.Errorf("%w: otra instancia está usando el mismo repositorio TUF", ErrTUFRepositoryInUse)
	}
	return lock, nil
}

// cleanupOrphanedTUFStages removes only application-owned staging directories.
// The caller must already hold the repository lock; this is what makes it safe
// to distinguish an abandoned stage from a stage still being written by a
// healthy process. Symlinks and unexpected file types fail closed.
func cleanupOrphanedTUFStages(metadataCacheRoot string, lock *flock.Flock, repositoryID string) (int, error) {
	if lock == nil || !lock.Locked() {
		return 0, fmt.Errorf("%w: se requiere el lock exclusivo del repositorio antes de limpiar stages", ErrTUFClientUnavailable)
	}
	if strings.TrimSpace(metadataCacheRoot) == "" {
		return 0, fmt.Errorf("%w: carpeta de metadata ausente", ErrInvalidTUFClientConfig)
	}
	if strings.TrimSpace(repositoryID) == "" || len(repositoryID) > 128 {
		return 0, fmt.Errorf("%w: repositoryID inválido para recuperar stages", ErrInvalidTUFClientConfig)
	}
	repositoryDigest := sha256.Sum256([]byte(repositoryID))
	expectedLockName := hex.EncodeToString(repositoryDigest[:]) + ".lock"
	if filepath.Base(lock.Path()) != expectedLockName ||
		filepath.Base(filepath.Dir(lock.Path())) != tufRepositoryLockDir {
		return 0, fmt.Errorf("%w: el lock no corresponde al repositoryID solicitado", ErrTUFClientUnavailable)
	}
	stagePrefix := tufMetadataStagePrefixForRepository(repositoryID)

	rootInfo, err := os.Lstat(metadataCacheRoot)
	if err != nil {
		return 0, fmt.Errorf("inspeccionar carpeta de metadata TUF: %w", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return 0, fmt.Errorf("la carpeta de metadata TUF debe ser un directorio real")
	}

	entries, err := os.ReadDir(metadataCacheRoot)
	if err != nil {
		return 0, fmt.Errorf("listar stages TUF: %w", err)
	}

	removed := 0
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), stagePrefix) {
			continue
		}
		if filepath.Base(entry.Name()) != entry.Name() {
			return removed, fmt.Errorf("nombre inesperado de stage TUF: %q", entry.Name())
		}
		stagePath := filepath.Join(metadataCacheRoot, entry.Name())
		info, err := os.Lstat(stagePath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return removed, fmt.Errorf("inspeccionar stage TUF %q: %w", entry.Name(), err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return removed, fmt.Errorf("stage TUF %q no es un directorio real; se cancela la limpieza", entry.Name())
		}
		if err := os.RemoveAll(stagePath); err != nil {
			return removed, fmt.Errorf("eliminar stage TUF abandonado %q: %w", entry.Name(), err)
		}
		removed++
	}
	return removed, nil
}
