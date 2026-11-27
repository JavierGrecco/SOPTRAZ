package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func writeArtifact(t *testing.T, root, relative string, content []byte) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir artifact directory: %v", err)
	}
	if err := os.WriteFile(path, content, 0o500); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	return path
}

func testDescriptor(content []byte) Descriptor {
	digest := sha256.Sum256(content)
	return Descriptor{
		SchemaVersion: ArtifactDescriptorSchemaVersion,
		ID:            "soptraz.osquery",
		Version:       "5.24.1",
		OS:            "linux",
		Architecture:  "amd64",
		RelativePath:  "osquery/5.24.1/linux-amd64/osqueryi",
		SHA256:        hex.EncodeToString(digest[:]),
		SizeBytes:     int64(len(content)),
		License:       "Apache-2.0 OR GPL-2.0-only",
		SourceURI:     "https://github.com/osquery/osquery/releases/tag/5.24.1",
	}
}

func newTestResolver(t *testing.T, root string, platform Platform) *Resolver {
	t.Helper()
	resolver, err := newResolver(root, platform, DefaultMaxArtifactBytes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := resolver.Close(); err != nil {
			t.Errorf("close artifact resolver: %v", err)
		}
	})
	return resolver
}

func newTestResolverWithLimit(t *testing.T, root string, platform Platform, maxBytes int64) *Resolver {
	t.Helper()
	resolver, err := newResolver(root, platform, maxBytes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := resolver.Close(); err != nil {
			t.Errorf("close artifact resolver: %v", err)
		}
	})
	return resolver
}

func TestPlatformValidateSupportedOperatingSystemArchitectures(t *testing.T) {
	supported := []Platform{
		{OS: "windows", Architecture: "amd64"},
		{OS: "windows", Architecture: "arm64"},
		{OS: "darwin", Architecture: "amd64"},
		{OS: "darwin", Architecture: "arm64"},
		{OS: "linux", Architecture: "amd64"},
		{OS: "linux", Architecture: "arm64"},
		{OS: "linux", Architecture: "386"},
		{OS: "linux", Architecture: "arm"},
	}
	for _, platform := range supported {
		t.Run(platform.OS+"/"+platform.Architecture, func(t *testing.T) {
			if err := platform.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestPlatformValidateRejectsUnsupportedOperatingSystemArchitectures(t *testing.T) {
	unsupported := []Platform{
		{OS: "windows", Architecture: "386"},
		{OS: "windows", Architecture: "arm"},
		{OS: "darwin", Architecture: "386"},
		{OS: "darwin", Architecture: "arm"},
		{OS: "linux", Architecture: "loong64"},
		{OS: "plan9", Architecture: "amd64"},
	}
	for _, platform := range unsupported {
		t.Run(platform.OS+"/"+platform.Architecture, func(t *testing.T) {
			if err := platform.Validate(); !errors.Is(err, ErrUnsupportedPlatform) {
				t.Fatalf("Validate() error = %v, want ErrUnsupportedPlatform", err)
			}
		})
	}
}

func TestCurrentPlatformMatchesGoRuntime(t *testing.T) {
	platform := CurrentPlatform()
	if platform.OS != runtime.GOOS || platform.Architecture != runtime.GOARCH {
		t.Fatalf("CurrentPlatform() = %#v, runtime = %s/%s", platform, runtime.GOOS, runtime.GOARCH)
	}
}
func TestDescriptorRejectsMalformedSemanticVersion(t *testing.T) {
	base := testDescriptor([]byte("bytes"))
	for _, version := range []string{"5.24.", "5.24", "05.24.1", "v5.24.1", "5.24.1.2"} {
		t.Run(version, func(t *testing.T) {
			value := base
			value.Version = version
			if err := value.Validate(); err == nil {
				t.Fatalf("version %q must be rejected", version)
			}
		})
	}
}

func TestDescriptorRejectsInvalidExpectedSize(t *testing.T) {
	base := testDescriptor([]byte("bytes"))
	for _, size := range []int64{0, -1, DefaultMaxArtifactBytes + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			value := base
			value.SizeBytes = size
			if err := value.Validate(); err == nil {
				t.Fatalf("sizeBytes %d must be rejected", size)
			}
		})
	}
}

func TestResolveRejectsDeclaredSizeMismatch(t *testing.T) {
	root := t.TempDir()
	content := []byte("artifact bytes")
	writeArtifact(t, root, "osquery/5.24.1/linux-amd64/osqueryi", content)
	resolver := newTestResolver(t, root, Platform{OS: "linux", Architecture: "amd64"})
	value := testDescriptor(content)
	value.SizeBytes++
	if _, err := resolver.Resolve(value); !errors.Is(err, ErrArtifactIntegrity) {
		t.Fatalf("Resolve() error = %v, want ErrArtifactIntegrity", err)
	}
}
func TestResolverCloseIsIdempotentAndRejectsFurtherResolutions(t *testing.T) {
	root := t.TempDir()
	content := []byte("trusted artifact")
	writeArtifact(t, root, "osquery/5.24.1/linux-amd64/osqueryi", content)
	resolver := newTestResolver(t, root, Platform{OS: "linux", Architecture: "amd64"})
	if err := resolver.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := resolver.Close(); err != nil {
		t.Fatalf("second Close() error = %v, want idempotent close", err)
	}
	if _, err := resolver.Resolve(testDescriptor(content)); !errors.Is(err, ErrResolverClosed) {
		t.Fatalf("Resolve() after Close() error = %v, want ErrResolverClosed", err)
	}
}

func TestResolverCloseIsSafeDuringConcurrentResolutions(t *testing.T) {
	root := t.TempDir()
	content := []byte("trusted artifact")
	writeArtifact(t, root, "osquery/5.24.1/linux-amd64/osqueryi", content)
	resolver := newTestResolver(t, root, Platform{OS: "linux", Architecture: "amd64"})
	const workers = 8
	start := make(chan struct{})
	var group sync.WaitGroup
	errorsFound := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			for attempt := 0; attempt < 20; attempt++ {
				_, err := resolver.Resolve(testDescriptor(content))
				if errors.Is(err, ErrResolverClosed) {
					return
				}
				if err != nil {
					errorsFound <- err
					return
				}
			}
		}()
	}
	close(start)
	if err := resolver.Close(); err != nil {
		t.Fatalf("Close() during concurrent Resolve() error = %v", err)
	}
	group.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Errorf("unexpected concurrent Resolve() error: %v", err)
	}
}
func TestResolveVerifiesInstalledArtifact(t *testing.T) {
	root := t.TempDir()
	content := []byte("trusted osquery test executable bytes")
	path := writeArtifact(t, root, "osquery/5.24.1/linux-amd64/osqueryi", content)
	resolver := newTestResolver(t, root, Platform{OS: "linux", Architecture: "amd64"})
	resolved, err := resolver.Resolve(testDescriptor(content))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	canonicalPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("canonicalize expected artifact path: %v", err)
	}
	if resolved.Path != canonicalPath || resolved.Size != int64(len(content)) || resolved.Descriptor.ID != "soptraz.osquery" {
		t.Fatalf("unexpected resolved artifact: %#v", resolved)
	}
}

func TestResolveRejectsUnsupportedPlatformWithoutFallback(t *testing.T) {
	root := t.TempDir()
	content := []byte("artifact")
	writeArtifact(t, root, "osquery/5.24.1/linux-amd64/osqueryi", content)
	resolver := newTestResolver(t, root, Platform{OS: "windows", Architecture: "amd64"})
	_, err := resolver.Resolve(testDescriptor(content))
	if !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("Resolve() error = %v, want ErrUnsupportedPlatform", err)
	}
}

func TestResolveRejectsChangedArtifactBytes(t *testing.T) {
	root := t.TempDir()
	writeArtifact(t, root, "osquery/5.24.1/linux-amd64/osqueryi", []byte("tampered bytes"))
	resolver := newTestResolver(t, root, Platform{OS: "linux", Architecture: "amd64"})
	_, err := resolver.Resolve(testDescriptor([]byte("original bytes")))
	if !errors.Is(err, ErrArtifactIntegrity) {
		t.Fatalf("Resolve() error = %v, want ErrArtifactIntegrity", err)
	}
}

func TestDescriptorRejectsTraversalAndNonCanonicalPaths(t *testing.T) {
	base := testDescriptor([]byte("bytes"))
	for _, candidate := range []string{"../escape", "osquery/../escape", "/absolute/path", `osquery\binary`, "osquery//binary", "osquery/./binary", "osquery/"} {
		t.Run(candidate, func(t *testing.T) {
			value := base
			value.RelativePath = candidate
			if err := value.Validate(); err == nil {
				t.Fatal("unsafe or ambiguous path must be rejected")
			}
		})
	}
}

func TestDescriptorRejectsWindowsSpecialPathComponents(t *testing.T) {
	base := testDescriptor([]byte("artifact"))
	unsafePaths := []string{
		`C:relative.exe`,
		`osqueryi.exe:alternate-stream`,
		`NUL.txt`,
		`CON`,
		`COM1.exe`,
		`LPT9.log`,
		`COM¹`,
		`folder.`,
		`folder `,
		`bad|name`,
		`bad<name`,
	}
	for _, candidate := range unsafePaths {
		t.Run(candidate, func(t *testing.T) {
			value := base
			value.RelativePath = candidate
			if err := value.Validate(); err == nil {
				t.Fatalf("non-portable Windows path component %q must be rejected", candidate)
			}
		})
	}
}

func TestDescriptorRejectsInvalidDigestAndSourceURI(t *testing.T) {
	base := testDescriptor([]byte("bytes"))
	badDigest := base
	badDigest.SHA256 = strings.ToUpper(badDigest.SHA256)
	if err := badDigest.Validate(); err == nil {
		t.Fatal("uppercase digest must be rejected")
	}
	badSource := base
	badSource.SourceURI = "file:///tmp/artifact"
	if err := badSource.Validate(); err == nil {
		t.Fatal("non-HTTPS source must be rejected")
	}
}

func TestResolveRejectsDirectoryLinkEscapingManagedRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("la creación de enlaces de directorio puede requerir privilegios en Windows")
	}
	root := t.TempDir()
	outside := t.TempDir()
	content := []byte("outside trusted root")
	target := writeArtifact(t, outside, "linux-amd64/osqueryi", content)
	link := filepath.Join(root, "osquery")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("create directory symlink fixture: %v", err)
	}
	value := testDescriptor(content)
	value.RelativePath = "osquery/linux-amd64/osqueryi"
	resolver := newTestResolver(t, root, Platform{OS: "linux", Architecture: "amd64"})
	_, err := resolver.Resolve(value)
	if !errors.Is(err, ErrArtifactPath) {
		t.Fatalf("Resolve() path %q (target %q) error = %v, want ErrArtifactPath", value.RelativePath, target, err)
	}
}

func TestResolveRejectsOversizedArtifact(t *testing.T) {
	root := t.TempDir()
	content := []byte("too large")
	writeArtifact(t, root, "osquery/5.24.1/linux-amd64/osqueryi", content)
	resolver := newTestResolverWithLimit(t, root, Platform{OS: "linux", Architecture: "amd64"}, 4)
	_, err := resolver.Resolve(testDescriptor(content))
	if err == nil || !strings.Contains(err.Error(), "límite") {
		t.Fatalf("Resolve() error = %v, want size limit error", err)
	}
}

func TestResolveRejectsSymlinkedArtifactPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("la creación de symlinks puede requerir Developer Mode o privilegios en Windows")
	}
	root := t.TempDir()
	content := []byte("trusted")
	target := writeArtifact(t, root, "payloads/osqueryi", content)
	if err := os.MkdirAll(filepath.Join(root, "osquery"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "osquery", "osqueryi")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create symlink fixture: %v", err)
	}
	value := testDescriptor(content)
	value.RelativePath = "osquery/osqueryi"
	resolver := newTestResolver(t, root, Platform{OS: "linux", Architecture: "amd64"})
	_, err := resolver.Resolve(value)
	if !errors.Is(err, ErrArtifactPath) {
		t.Fatalf("Resolve() error = %v, want ErrArtifactPath", err)
	}
}
