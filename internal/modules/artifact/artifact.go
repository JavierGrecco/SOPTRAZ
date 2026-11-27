// Package artifact resolves and verifies already-installed third-party executables.
// It never downloads, installs or launches an artifact. Descriptors must come from
// a separately trusted registry: matching SHA-256 proves byte correspondence to
// that descriptor, not publisher identity or release authenticity.
package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

const DefaultMaxArtifactBytes int64 = 512 << 20

var (
	ErrInvalidDescriptor   = errors.New("descriptor de artefacto inválido")
	ErrUnsupportedPlatform = errors.New("plataforma o arquitectura no compatible")
	ErrArtifactPath        = errors.New("ruta de artefacto no permitida")
	ErrArtifactIntegrity   = errors.New("integridad del artefacto no válida")
	ErrResolverClosed      = errors.New("el resolver de artefactos está cerrado")
)

var artifactIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*// Package artifact resolves and verifies already-installed third-party executables.
// It never downloads, installs or launches an artifact. Descriptors must come from
// a separately trusted registry: matching SHA-256 proves byte correspondence to
// that descriptor, not publisher identity or release authenticity.
package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

const DefaultMaxArtifactBytes int64 = 512 << 20

var (
	ErrInvalidDescriptor   = errors.New("descriptor de artefacto inválido")
	ErrUnsupportedPlatform = errors.New("plataforma o arquitectura no compatible")
	ErrArtifactPath        = errors.New("ruta de artefacto no permitida")
	ErrArtifactIntegrity   = errors.New("integridad del artefacto no válida")
	ErrResolverClosed      = errors.New("el resolver de artefactos está cerrado")
)

)

var artifactVersionPattern = regexp.MustCompile("^(0|[1-9][0-9]*)[.](0|[1-9][0-9]*)[.](0|[1-9]*)(-[0-9A-Za-z.-]+)?([+][0-9A-Za-z.-]+)?$")

const ArtifactDescriptorSchemaVersion = "1.0.0"

// Platform identifies a specific operating-system/architecture pair.
type Platform struct {
	OS           string
	Architecture string
}

func CurrentPlatform() Platform {
	return Platform{OS: runtime.GOOS, Architecture: runtime.GOARCH}
}

func (platform Platform) Validate() error {
	var supported bool
	switch platform.OS {
	case "windows":
		supported = platform.Architecture == "amd64" || platform.Architecture == "arm64"
	case "darwin":
		supported = platform.Architecture == "amd64" || platform.Architecture == "arm64"
	case "linux":
		supported = platform.Architecture == "amd64" || platform.Architecture == "arm64" ||
			platform.Architecture == "386" || platform.Architecture == "arm"
	default:
		return fmt.Errorf("%w: sistema operativo %q", ErrUnsupportedPlatform, platform.OS)
	}
	if !supported {
		return fmt.Errorf("%w: combinación %s/%s", ErrUnsupportedPlatform, platform.OS, platform.Architecture)
	}
	return nil
}

// Descriptor is trusted registry metadata, never data supplied by a caller/UI.
// RelativePath is slash-separated and rooted in the application-managed store.
type Descriptor struct {
	SchemaVersion string `json:"schemaVersion"`
	ID            string `json:"artifactId"`
	Version       string `json:"version"`
	OS            string `json:"os"`
	Architecture  string `json:"architecture"`
	RelativePath  string `json:"relativePath"`
	SHA256        string `json:"sha256"`
	SizeBytes     int64  `json:"sizeBytes"`
	License       string `json:"license"`
	SourceURI     string `json:"sourceUri"`
}

// portablePathComponent rejects names that can be interpreted specially by
// Windows filesystems, even when the registry is read on Linux or macOS.
func portablePathComponent(part string) bool {
	if part == "" || strings.TrimRight(part, " .") != part {
		return false
	}
	for _, character := range part {
		if character < 0x20 || strings.ContainsRune(`<>":|?*`, character) {
			return false
		}
	}
	base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$", "CLOCK$",
		"COM¹", "COM²", "COM³", "LPT¹", "LPT²", "LPT³":
		return false
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
		return false
	}
	return true
}
func (descriptor Descriptor) Validate() error {
	if descriptor.SchemaVersion != ArtifactDescriptorSchemaVersion {
		return fmt.Errorf("%w: schemaVersion debe ser %s", ErrInvalidDescriptor, ArtifactDescriptorSchemaVersion)
	}
	if !artifactIDPattern.MatchString(descriptor.ID) || len(descriptor.ID) > 128 {
		return fmt.Errorf("%w: module/artefacto ID inválido", ErrInvalidDescriptor)
	}
	if !artifactVersionPattern.MatchString(descriptor.Version) || len(descriptor.Version) > 80 {
		return fmt.Errorf("%w: versión debe usar SemVer", ErrInvalidDescriptor)
	}
	if strings.TrimSpace(descriptor.License) == "" || len(descriptor.License) > 120 {
		return fmt.Errorf("%w: licencia ausente o demasiado larga", ErrInvalidDescriptor)
	}
	if descriptor.SizeBytes <= 0 || descriptor.SizeBytes > DefaultMaxArtifactBytes {
		return fmt.Errorf("%w: sizeBytes debe estar entre 1 y %d", ErrInvalidDescriptor, DefaultMaxArtifactBytes)
	}
	if err := (Platform{OS: descriptor.OS, Architecture: descriptor.Architecture}).Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidDescriptor, err)
	}
	if len(descriptor.SHA256) != sha256.Size*2 || descriptor.SHA256 != strings.ToLower(descriptor.SHA256) {
		return fmt.Errorf("%w: SHA-256 debe tener 64 caracteres hexadecimales minúsculos", ErrInvalidDescriptor)
	}
	if _, err := hex.DecodeString(descriptor.SHA256); err != nil {
		return fmt.Errorf("%w: SHA-256 inválido: %v", ErrInvalidDescriptor, err)
	}
	if descriptor.RelativePath == "" || strings.Contains(descriptor.RelativePath, `\`) || strings.HasPrefix(descriptor.RelativePath, "/") || pathpkg.IsAbs(descriptor.RelativePath) {
		return fmt.Errorf("%w: se requiere una ruta relativa canónica con separadores '/'", ErrInvalidDescriptor)
	}
	if pathpkg.Clean(descriptor.RelativePath) != descriptor.RelativePath {
		return fmt.Errorf("%w: la ruta contiene segmentos ambiguos", ErrInvalidDescriptor)
	}
	for _, part := range strings.Split(descriptor.RelativePath, "/") {
		if part == "" || part == "." || part == ".." || !portablePathComponent(part) {
			return fmt.Errorf("%w: la ruta contiene un segmento no permitido o no portable", ErrInvalidDescriptor)
		}
	}
	u, err := url.Parse(descriptor.SourceURI)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return fmt.Errorf("%w: SourceURI debe ser HTTPS y no contener credenciales", ErrInvalidDescriptor)
	}
	return nil
}

// ResolvedArtifact is returned only after platform, path and SHA-256 checks.
type ResolvedArtifact struct {
	Descriptor Descriptor
	Path       string
	Size       int64
}

// Resolver verifies artifacts under one application-managed directory.
// It does not trust PATH, search other locations or fall back to another build.
type Resolver struct {
	root     *os.Root
	rootPath string
	platform Platform
	maxBytes int64

	mu       sync.RWMutex
	closed   bool
	closeErr error
}

// NewResolver binds artifact lookup to the platform on which SOPTRAZ is running.
// Callers cannot spoof a target platform to resolve a different OS binary.
func NewResolver(root string) (*Resolver, error) {
	return newResolver(root, CurrentPlatform(), DefaultMaxArtifactBytes)
}

func newResolver(root string, platform Platform, maxBytes int64) (*Resolver, error) {
	if err := platform.Validate(); err != nil {
		return nil, err
	}
	if maxBytes <= 0 || maxBytes > DefaultMaxArtifactBytes {
		return nil, fmt.Errorf("límite de artefacto debe estar entre 1 y %d bytes", DefaultMaxArtifactBytes)
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolver raíz de artefactos: %w", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(absoluteRoot)
	if err != nil {
		return nil, fmt.Errorf("resolver raíz administrada de artefactos: %w", err)
	}
	rootHandle, err := os.OpenRoot(resolvedRoot)
	if err != nil {
		return nil, fmt.Errorf("abrir raíz administrada de artefactos: %w", err)
	}
	info, err := rootHandle.Stat(".")
	if err != nil || !info.IsDir() {
		_ = rootHandle.Close()
		if err != nil {
			return nil, fmt.Errorf("inspeccionar raíz administrada de artefactos: %w", err)
		}
		return nil, fmt.Errorf("%w: la raíz no es un directorio", ErrArtifactPath)
	}
	return &Resolver{
		root: rootHandle, rootPath: resolvedRoot, platform: platform, maxBytes: maxBytes,
	}, nil
}

// Close releases the confined directory handle. It waits for in-flight
// resolutions, prevents new ones from starting, and is safe to call repeatedly.
func (resolver *Resolver) Close() error {
	if resolver == nil {
		return nil
	}
	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	if resolver.closed {
		return resolver.closeErr
	}
	resolver.closed = true
	if resolver.root != nil {
		resolver.closeErr = resolver.root.Close()
	}
	return resolver.closeErr
}

// Resolve accepts only an exact platform match and verifies the bytes on disk.
// No PATH lookup, download, extraction or process execution is performed.
func (resolver *Resolver) Resolve(descriptor Descriptor) (ResolvedArtifact, error) {
	if resolver == nil {
		return ResolvedArtifact{}, fmt.Errorf("resolver de artefactos no inicializado")
	}
	resolver.mu.RLock()
	defer resolver.mu.RUnlock()
	if resolver.closed || resolver.root == nil {
		return ResolvedArtifact{}, ErrResolverClosed
	}
	if err := descriptor.Validate(); err != nil {
		return ResolvedArtifact{}, err
	}
	if descriptor.OS != resolver.platform.OS || descriptor.Architecture != resolver.platform.Architecture {
		return ResolvedArtifact{}, fmt.Errorf("%w: requerido %s/%s; actual %s/%s", ErrUnsupportedPlatform, descriptor.OS, descriptor.Architecture, resolver.platform.OS, resolver.platform.Architecture)
	}

	parts := strings.Split(descriptor.RelativePath, "/")
	var finalInfo os.FileInfo
	for index := range parts {
		relativePart := filepath.FromSlash(strings.Join(parts[:index+1], "/"))
		info, err := resolver.root.Lstat(relativePart)
		if err != nil {
			return ResolvedArtifact{}, fmt.Errorf("localizar artefacto %q bajo raíz confinada: %w", descriptor.ID, err)
		}
		if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return ResolvedArtifact{}, fmt.Errorf("%w: no se permiten symlinks ni reparse points en la ruta del artefacto", ErrArtifactPath)
		}
		if index < len(parts)-1 && !info.IsDir() {
			return ResolvedArtifact{}, fmt.Errorf("%w: un componente intermedio no es directorio", ErrArtifactPath)
		}
		if index == len(parts)-1 {
			if !info.Mode().IsRegular() {
				return ResolvedArtifact{}, fmt.Errorf("%w: el artefacto debe ser un archivo regular", ErrArtifactPath)
			}
			finalInfo = info
		}
	}

	relativePath := filepath.FromSlash(descriptor.RelativePath)
	file, err := resolver.root.Open(relativePath)
	if err != nil {
		return ResolvedArtifact{}, fmt.Errorf("abrir artefacto confinado a la raíz: %w", err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(finalInfo, openedInfo) {
		return ResolvedArtifact{}, fmt.Errorf("%w: el archivo cambió entre la inspección y la apertura", ErrArtifactPath)
	}
	if openedInfo.Size() > resolver.maxBytes {
		return ResolvedArtifact{}, fmt.Errorf("%w: el tamaño del artefacto supera el límite", ErrInvalidDescriptor)
	}
	if openedInfo.Size() != descriptor.SizeBytes {
		return ResolvedArtifact{}, ErrArtifactIntegrity
	}

	hasher := sha256.New()
	bytesRead, err := io.Copy(hasher, io.LimitReader(file, resolver.maxBytes+1))
	if err != nil {
		return ResolvedArtifact{}, fmt.Errorf("calcular SHA-256 del artefacto: %w", err)
	}
	if bytesRead > resolver.maxBytes {
		return ResolvedArtifact{}, fmt.Errorf("%w: el archivo excede el límite de lectura", ErrInvalidDescriptor)
	}
	if bytesRead != descriptor.SizeBytes {
		return ResolvedArtifact{}, ErrArtifactIntegrity
	}
	if hex.EncodeToString(hasher.Sum(nil)) != descriptor.SHA256 {
		return ResolvedArtifact{}, ErrArtifactIntegrity
	}

	finalOpenedInfo, err := file.Stat()
	if err != nil || !os.SameFile(openedInfo, finalOpenedInfo) || openedInfo.Size() != finalOpenedInfo.Size() || !openedInfo.ModTime().Equal(finalOpenedInfo.ModTime()) {
		return ResolvedArtifact{}, fmt.Errorf("%w: el archivo cambió durante la verificación", ErrArtifactPath)
	}
	finalPathInfo, err := resolver.root.Lstat(relativePath)
	if err != nil || finalPathInfo.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 || !finalPathInfo.Mode().IsRegular() || !os.SameFile(finalOpenedInfo, finalPathInfo) {
		return ResolvedArtifact{}, fmt.Errorf("%w: la ruta fue sustituida durante la verificación", ErrArtifactPath)
	}

	return ResolvedArtifact{
		Descriptor: descriptor,
		Path:       filepath.Join(resolver.rootPath, relativePath),
		Size:       bytesRead,
	}, nil
}

// Root confines lookup and open operations beneath the held directory handle.
// It rejects symlinks that escape the root and resolves Windows reparse points
// within its platform implementation. The returned path is still a point-in-time
// name, not an executable capability: the supervisor must reopen it through a
// managed root immediately before process creation and protect the directory
// against untrusted writes.
