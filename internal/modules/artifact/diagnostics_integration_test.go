package artifact

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/JavierGrecco/SOPTRAZ/internal/diagnostics"
)

func TestResolverWritesSecurityIncidentToLocalDiagnosticsReport(t *testing.T) {
	root := t.TempDir()
	directory := t.TempDir()
	trustedBytes := []byte("trusted artifact bytes")
	writeArtifact(t, root, "osquery/5.24.1/linux-amd64/osqueryi", []byte("trussed artifact bytes"))

	logger, err := diagnostics.Open(directory, "integration-test")
	if err != nil {
		t.Fatalf("open local diagnostics: %v", err)
	}
	defer logger.Close()

	resolver, err := NewResolverWithDiagnostics(root, logger)
	if err != nil {
		_ = logger.Close()
		t.Fatalf("NewResolverWithDiagnostics() error = %v", err)
	}
	defer func() {
		if err := resolver.Close(); err != nil {
			t.Errorf("close resolver: %v", err)
		}
	}()

	descriptor := testDescriptor(trustedBytes)
	descriptor.OS = CurrentPlatform().OS
	descriptor.Architecture = CurrentPlatform().Architecture

	if _, err := resolver.Resolve(descriptor); !errors.Is(err, ErrArtifactIntegrity) {
		t.Fatalf("Resolve() error = %v, want ErrArtifactIntegrity", err)
	}

	reportPath := logger.Path()
	raw, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read local diagnostics report: %v", err)
	}
	report := string(raw)
	for _, expected := range []string{
		"CRITICAL",
		"Componente: artifact",
		"Operación: resolve",
		"Código: artifact.integrity_mismatch",
		"artefacto=soptraz.osquery",
		"El SHA-256 del archivo instalado no coincide",
	} {
		if !strings.Contains(report, expected) {
			t.Errorf("local report missing %q: %s", expected, report)
		}
	}
	if strings.Contains(report, root) || strings.Contains(report, directory) {
		t.Fatalf("local report exposed a temporary filesystem path: %s", report)
	}

	if err := logger.Close(); err != nil {
		t.Fatalf("close local diagnostics: %v", err)
	}

	// A closed or unavailable diagnostics sink must never replace the
	// integrity decision made by the artifact resolver.
	if _, err := resolver.Resolve(descriptor); !errors.Is(err, ErrArtifactIntegrity) {
		t.Fatalf("Resolve() with closed diagnostics error = %v, want ErrArtifactIntegrity", err)
	}
}
