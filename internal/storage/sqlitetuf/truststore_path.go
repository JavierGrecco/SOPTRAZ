package sqlitetuf

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// prepareTrustStorePath resolves parent-directory aliases, checks the directory's
// security policy, and atomically creates or safely opens the final database file
// before SQLite receives its path. It does not rewrite permissions on existing
// parent directories.
func prepareTrustStorePath(path string) (string, error) {
	if strings.TrimSpace(path) == "" || path == ":memory:" {
		return "", fmt.Errorf("%w: se requiere una ruta de archivo local", ErrInvalidTrustStore)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("%w: resolver la ruta: %v", ErrInvalidTrustStore, err)
	}
	absolute = filepath.Clean(absolute)
	parent := filepath.Dir(absolute)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", fmt.Errorf("%w: crear el directorio: %v", ErrInvalidTrustStore, err)
	}

	// Resolve intentional OS aliases (for example /var -> /private/var on macOS)
	// once and use the resulting canonical parent consistently for all checks
	// and for SQLite. This avoids treating a path spelling as the security object.
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", fmt.Errorf("%w: resolver directorio real del almacén: %v", ErrInvalidTrustStore, err)
	}
	resolvedParent, err = filepath.Abs(resolvedParent)
	if err != nil {
		return "", fmt.Errorf("%w: resolver directorio canónico del almacén: %v", ErrInvalidTrustStore, err)
	}
	resolvedParent = filepath.Clean(resolvedParent)
	if err := validateTrustStoreParent(resolvedParent); err != nil {
		return "", fmt.Errorf("%w: directorio del almacén no seguro: %v", ErrInvalidTrustStore, err)
	}

	name := filepath.Base(absolute)
	if name == "." || name == string(filepath.Separator) || name == "" {
		return "", fmt.Errorf("%w: nombre de archivo SQLite inválido", ErrInvalidTrustStore)
	}
	canonicalPath := filepath.Join(resolvedParent, name)
	if err := prepareTrustStoreFile(canonicalPath); err != nil {
		return "", fmt.Errorf("%w: archivo SQLite no seguro: %v", ErrInvalidTrustStore, err)
	}
	return canonicalPath, nil
}

var errTrustStorePathNotRegular = errors.New("la ruta SQLite debe ser un archivo regular, no un enlace ni un archivo especial")
