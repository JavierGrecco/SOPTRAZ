package catalog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JavierGrecco/SOPTRAZ/internal/diagnostics"
	"github.com/JavierGrecco/SOPTRAZ/internal/storage/sqlitetuf"
)

func TestPersistentTUFClientWritesBindingIncidentToLocalDiagnosticsReport(t *testing.T) {
	fixture := makeDelegatedRepositoryFixture(t)
	storePath := filepath.Join(t.TempDir(), "catalog-trust.sqlite")
	store, err := sqlitetuf.Open(context.Background(), storePath)
	if err != nil {
		t.Fatalf("open persistent TUF store: %v", err)
	}
	defer store.Close()

	anchor, err := NewPinnedTrustAnchor(fixture.rootJSON, rootPin(fixture.rootJSON))
	if err != nil {
		t.Fatalf("create bootstrap trust anchor: %v", err)
	}
	_, bootstrapState, err := anchor.initialRootSnapshot()
	if err != nil {
		t.Fatalf("read bootstrap trust anchor: %v", err)
	}

	const repositoryID = "diagnostics-binding-test"
	persistedURL := "http://127.0.0.1:17771/metadata"
	configuredURL := "http://127.0.0.1:17771/other"
	if err := store.RegisterRepository(context.Background(), sqlitetuf.RepositoryConfig{
		ID:            repositoryID,
		MetadataURL:   persistedURL,
		AnchorVersion: bootstrapState.Version,
		AnchorSHA256:  bootstrapState.SHA256,
	}); err != nil {
		t.Fatalf("register original repository binding: %v", err)
	}

	reportDirectory := t.TempDir()
	logger, err := diagnostics.Open(reportDirectory, "tuf-integration-test")
	if err != nil {
		t.Fatalf("open local diagnostics: %v", err)
	}
	defer logger.Close()

	cacheRoot := t.TempDir()
	config := persistentClientConfig(configuredURL, cacheRoot)
	config.Diagnostics = logger
	config.AllowInsecureLoopbackHTTP = true

	_, err = NewPersistentTUFClient(anchor, config, store, repositoryID)
	if err == nil || !errors.Is(err, ErrInvalidTUFClientConfig) {
		t.Fatalf("NewPersistentTUFClient() error = %v, want binding conflict", err)
	}

	raw, err := os.ReadFile(logger.Path())
	if err != nil {
		t.Fatalf("read local diagnostics report: %v", err)
	}
	report := string(raw)
	for _, expected := range []string{
		"CRITICAL",
		"Componente: catalog.tuf",
		"Operación: bootstrap",
		"Código: tuf.repository_binding_conflict",
		"conflicto de identidad del repositorio",
	} {
		if !strings.Contains(report, expected) {
			t.Errorf("local report missing %q: %s", expected, report)
		}
	}
	for _, sensitive := range []string{persistedURL, configuredURL, storePath, reportDirectory, cacheRoot} {
		if strings.Contains(report, sensitive) {
			t.Errorf("local report exposed configuration or filesystem detail %q: %s", sensitive, report)
		}
	}

	// Failed construction must release the repository lock even when the
	// diagnostics logger is connected to the client configuration.
	lock, err := acquirePersistentTUFRepositoryLock(store, repositoryID)
	if err != nil {
		t.Fatalf("repository lock remained held after constructor failure: %v", err)
	}
	if err := lock.Unlock(); err != nil {
		t.Fatalf("release lock after integration assertion: %v", err)
	}
}
