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
)

const DefaultMaxArtifactBytes int64 = 512 << 20

var (
	ErrInvalidDescriptor   = errors.New("descriptor de artefacto inválido")
	ErrUnsupportedPlatform = errors.New("plataforma o arquitectura no compatible")
	ErrArtifactPath        = errors.New("ruta de artefacto no permitida")
	ErrArtifactIntegrity   = errors.New("integridad del artefacto no válida")
)

var artifactIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$`)

// Platform identifies a specific operating-system/architecture pair.
type Platform struct {
	OS           string
	Architecture string
}

func CurrentPlatform() Platform {
	osName := runtime.GOOS
	if osName == "darwin" {
		osName = "darwin"
	}
	return Platform{OS: osName, Architecture: runtime.GOARCH}
}

func (platform Platform) Validate() error {
	switch platform.OS {
	case "windows", "darwin", "linux":
	default:
		return fmt.Errorf("%w: sistema operativo %q", ErrUnsupportedPlatform, platform.OS)
	}
	switch platform.Architecture {
	case "amd64", "arm64", "386", "arm":
	default:
		return fmt.Errorf("%w: arquitectura %q", ErrUnsupportedPlatform, platform.Architecture)
	}
	return nil
}

// Descriptor is trusted registry metadata, never data supplied by a caller/UI.
// RelativePath is slash-separated and rooted in the application-managed store.
type Descriptor struct {
	ID           string
	Version      string
	OS           string
	Architecture string
	RelativePath string
	SHA256       string
	License      string
	SourceURI    string
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
	if !artifactIDPattern.MatchString(descriptor.ID) || len(descriptor.ID) > 128 {
		return fmt.Errorf("%w: module/artefacto ID inválido", ErrInvalidDescriptor)
	}
	if strings.TrimSpace(descriptor.Version) == "" || len(descriptor.Version) > 80 {
		return fmt.Errorf("%w: versión ausente o demasiado larga", ErrInvalidDescriptor)
	}
	if strings.TrimSpace(descriptor.License) == "" || len(descriptor.License) > 120 {
		return fmt.Errorf("%w: licencia ausente o demasiado larga", ErrInvalidDescriptor)
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
	root     string
	platform Platform
	maxBytes int64
}

func NewResolver(root string, platform Platform) (*Resolver, error) {
	return newResolver(root, platform, DefaultMaxArtifactBytes)
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
		return nil, fmt.Errorf("resolver enlaces de la raíz de artefactos: %w", err)
	}
	info, err := os.Stat(resolvedRoot)
	if err != nil {
		return nil, fmt.Errorf("leer raíz de artefactos: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%w: la raíz no es un directorio", ErrArtifactPath)
	}
	return &Resolver{root: resolvedRoot, platform: platform, maxBytes: maxBytes}, nil
}

// Resolve accepts only an exact platform match and verifies the bytes on disk.
// No PATH lookup, download, extraction or process execution is performed.
func (resolver *Resolver) Resolve(descriptor Descriptor) (ResolvedArtifact, error) {
	if resolver == nil {
		return ResolvedArtifact{}, fmt.Errorf("resolver de artefactos no inicializado")
	}
	if err := descriptor.Validate(); err != nil {
		return ResolvedArtifact{}, err
	}
	if descriptor.OS != resolver.platform.OS || descriptor.Architecture != resolver.platform.Architecture {
		return ResolvedArtifact{}, fmt.Errorf("%w: requerido %s/%s; actual %s/%s", ErrUnsupportedPlatform, descriptor.OS, descriptor.Architecture, resolver.platform.OS, resolver.platform.Architecture)
	}

	parts := strings.Split(descriptor.RelativePath, "/")
	current := resolver.root
	var finalInfo os.FileInfo
	for index, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return ResolvedArtifact{}, fmt.Errorf("localizar artefacto %q: %w", descriptor.ID, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return ResolvedArtifact{}, fmt.Errorf("%w: no se permiten enlaces simbólicos en la ruta del artefacto", ErrArtifactPath)
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

	resolvedPath, err := filepath.EvalSymlinks(current)
	if err != nil {
		return ResolvedArtifact{}, fmt.Errorf("%w: no se pudo canonicalizar el destino del artefacto: %v", ErrArtifactPath, err)
	}
	if !pathWithinRoot(resolver.root, resolvedPath) {
		return ResolvedArtifact{}, fmt.Errorf("%w: el destino real escapa de la raíz administrada", ErrArtifactPath)
	}
	resolvedInfo, err := os.Stat(resolvedPath)
	if err != nil || !resolvedInfo.Mode().IsRegular() || !os.SameFile(finalInfo, resolvedInfo) {
		return ResolvedArtifact{}, fmt.Errorf("%w: el destino real no coincide con el archivo inspeccionado", ErrArtifactPath)
	}
	if resolvedInfo.Size() > resolver.maxBytes {
		return ResolvedArtifact{}, fmt.Errorf("%w: el tamaño del artefacto supera el límite de %d bytes", ErrInvalidDescriptor, resolver.maxBytes)
	}

	file, err := os.Open(resolvedPath)
	if err != nil {
		return ResolvedArtifact{}, fmt.Errorf("abrir artefacto verificado: %w", err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(finalInfo, openedInfo) {
		return ResolvedArtifact{}, fmt.Errorf("%w: el archivo cambió entre la inspección y la apertura", ErrArtifactPath)
	}
	if openedInfo.Size() > resolver.maxBytes {
		return ResolvedArtifact{}, fmt.Errorf("%w: el tamaño del artefacto supera el límite", ErrInvalidDescriptor)
	}

	hasher := sha256.New()
	bytesRead, err := io.Copy(hasher, io.LimitReader(file, resolver.maxBytes+1))
	if err != nil {
		return ResolvedArtifact{}, fmt.Errorf("calcular SHA-256 del artefacto: %w", err)
	}
	if bytesRead > resolver.maxBytes {
		return ResolvedArtifact{}, fmt.Errorf("%w: el archivo excede el límite de lectura", ErrInvalidDescriptor)
	}
	if hex.EncodeToString(hasher.Sum(nil)) != descriptor.SHA256 {
		return ResolvedArtifact{}, ErrArtifactIntegrity
	}

	finalOpenedInfo, err := file.Stat()
	if err != nil || !os.SameFile(openedInfo, finalOpenedInfo) || openedInfo.Size() != finalOpenedInfo.Size() || !openedInfo.ModTime().Equal(finalOpenedInfo.ModTime()) {
		return ResolvedArtifact{}, fmt.Errorf("%w: el archivo cambió durante la verificación", ErrArtifactPath)
	}
	finalResolvedPath, err := filepath.EvalSymlinks(current)
	if err != nil || !pathWithinRoot(resolver.root, finalResolvedPath) {
		return ResolvedArtifact{}, fmt.Errorf("%w: la ruta fue sustituida o redirigida fuera de la raíz durante la verificación", ErrArtifactPath)
	}
	finalPathInfo, err := os.Stat(finalResolvedPath)
	if err != nil || !os.SameFile(finalOpenedInfo, finalPathInfo) {
		return ResolvedArtifact{}, fmt.Errorf("%w: la ruta fue sustituida durante la verificación", ErrArtifactPath)
	}
	if filepath.Clean(finalResolvedPath) != filepath.Clean(resolvedPath) {
		return ResolvedArtifact{}, fmt.Errorf("%w: el destino del artefacto cambió durante la verificación", ErrArtifactPath)
	}

	return ResolvedArtifact{Descriptor: descriptor, Path: resolvedPath, Size: bytesRead}, nil
}

// pathWithinRoot checks containment using canonical filesystem paths, not only
// the lexical path supplied by the module descriptor. EvalSymlinks is called
// before this helper so links and reparse-point aliases are resolved first.
func pathWithinRoot(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	return relative != ".." && relative != "." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// The resolved path is a point-in-time verification result. The future process
// supervisor must execute only from a protected, application-managed directory
// and revalidate immediately before process creation to reduce TOCTOU exposure.
