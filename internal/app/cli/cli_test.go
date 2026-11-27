package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validManifest = `{
  "schemaVersion":"1.0.0",
  "moduleId":"soptraz.example.readonly",
  "moduleVersion":"0.1.0",
  "displayName":"Example Reader",
  "description":"A sample module used by CLI contract tests.",
  "supportStatus":"experimental",
  "platforms":[{"os":"linux","architectures":["amd64"]}],
  "capabilities":["host.inventory.read"],
  "permissions":["host.read"],
  "dependencies":[],
  "license":"Apache-2.0",
  "integrity":{"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
  "createdAt":"2026-11-27T19:00:00Z"
}`

func writeTempJSON(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "contract.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestHelpIsReadOnlyAndSuccessful(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run() code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "No modifica el equipo") || !strings.Contains(stdout.String(), "checkpoint") {
		t.Fatalf("unexpected help output: %q", stdout.String())
	}
}

func TestVersionJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"version", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run() code = %d; stderr=%q", code, stderr.String())
	}
	var got map[string]string
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got["name"] != "SOPTRAZ" || got["version"] == "" {
		t.Fatalf("unexpected version response: %#v", got)
	}
}

func TestHostInfoJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"host-info", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run() code = %d; stderr=%q", code, stderr.String())
	}
	var got map[string]string
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, key := range []string{"os", "architecture", "go_version"} {
		if got[key] == "" {
			t.Errorf("field %q is empty", key)
		}
	}
}

func TestValidateAcceptsValidManifest(t *testing.T) {
	path := writeTempJSON(t, validManifest)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"validate", "manifest", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run() code = %d; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Contrato manifest válido") {
		t.Fatalf("unexpected output: %q", stdout.String())
	}
}

func TestValidateAcceptsCheckpointExample(t *testing.T) {
	examplePath := filepath.Join("..", "..", "..", "examples", "contracts", "audit-checkpoint.example.json")
	raw, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatalf("read checkpoint example: %v", err)
	}
	path := writeTempJSON(t, string(raw))
	var stdout, stderr bytes.Buffer
	code := Run([]string{"validate", "checkpoint", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run() code = %d; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Contrato checkpoint válido") {
		t.Fatalf("unexpected output: %q", stdout.String())
	}
}

func TestValidateRejectsUnknownKindAndInvalidDocument(t *testing.T) {
	path := writeTempJSON(t, validManifest)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"validate", "shell", path}, &stdout, &stderr); code != 1 {
		t.Fatalf("unknown kind code = %d, want 1", code)
	}

	stderr.Reset()
	invalid := writeTempJSON(t, `{"schemaVersion":"1.0.0","unexpected":true}`)
	if code := Run([]string{"validate", "manifest", invalid}, &stdout, &stderr); code != 1 {
		t.Fatalf("invalid document code = %d, want 1", code)
	}
}

func TestValidateRejectsMissingFileAndBadArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"validate", "manifest"}, &stdout, &stderr); code != 2 {
		t.Fatalf("bad arguments code = %d, want 2", code)
	}
	stderr.Reset()
	if code := Run([]string{"validate", "manifest", filepath.Join(t.TempDir(), "missing.json")}, &stdout, &stderr); code != 1 {
		t.Fatalf("missing file code = %d, want 1", code)
	}
}

func TestUnknownCommandReturnsUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"repair-everything"}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "comando desconocido") {
		t.Fatalf("unexpected result: code=%d stderr=%q", code, stderr.String())
	}
}
