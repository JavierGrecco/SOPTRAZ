package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	moduleprofile "github.com/JavierGrecco/SOPTRAZ/internal/modules/deviceprofile"
	platformprofile "github.com/JavierGrecco/SOPTRAZ/internal/platform/deviceprofile"
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
	if !strings.Contains(stdout.String(), "No modifica el equipo") || !strings.Contains(stdout.String(), "checkpoint") || !strings.Contains(stdout.String(), "artifact") || !strings.Contains(stdout.String(), "device-profile") || !strings.Contains(stdout.String(), "time-status") || !strings.Contains(stdout.String(), "process-profile") {
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

func TestValidateAcceptsArtifactDescriptorExample(t *testing.T) {
	examplePath := filepath.Join("..", "..", "..", "examples", "contracts", "artifact-descriptor.example.json")
	raw, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatalf("read artifact descriptor example: %v", err)
	}
	path := writeTempJSON(t, string(raw))
	var stdout, stderr bytes.Buffer
	code := Run([]string{"validate", "artifact", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run() code = %d; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Contrato artifact válido") {
		t.Fatalf("unexpected output: %q", stdout.String())
	}
}
func TestValidateArtifactRejectsUnsafePathAndCredentials(t *testing.T) {
	examplePath := filepath.Join("..", "..", "..", "examples", "contracts", "artifact-descriptor.example.json")
	raw, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatalf("read artifact descriptor example: %v", err)
	}
	cases := []struct {
		name    string
		invalid string
	}{
		{name: "path traversal", invalid: strings.Replace(string(raw), `"relativePath": "osquery/5.24.1/linux-amd64/osqueryi"`, `"relativePath": "../escape.exe"`, 1)},
		{name: "URI credentials", invalid: strings.Replace(string(raw), `"sourceUri": "https://github.com/osquery/osquery/releases/tag/5.24.1"`, `"sourceUri": "https://user:secret@example.com/osqueryi"`, 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTempJSON(t, tc.invalid)
			var stdout, stderr bytes.Buffer
			code := Run([]string{"validate", "artifact", path}, &stdout, &stderr)
			if code != 1 {
				t.Fatalf("Run() code = %d, want 1; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), "descriptor de artefacto inválido") {
				t.Fatalf("unexpected error: %q", stderr.String())
			}
		})
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

func TestDeviceProfileReportSerializesUnknownCapabilitiesExplicitly(t *testing.T) {
	report := buildDeviceProfileReport(platformprofile.Result{})
	var stdout bytes.Buffer
	if code := writeJSON(&stdout, report); code != 0 {
		t.Fatalf("writeJSON() code = %d", code)
	}

	var decoded map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	profile, ok := decoded["profile"].(map[string]any)
	if !ok {
		t.Fatalf("profile has unexpected type: %T", decoded["profile"])
	}
	capabilities, ok := profile["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("capabilities have unexpected type: %T", profile["capabilities"])
	}
	for _, key := range []string{"battery_present", "touch", "pen", "physical_keyboard", "dedicated_gpu", "thermal_sensors"} {
		if got := capabilities[key]; got != "unknown" {
			t.Errorf("capability %q = %#v, want unknown", key, got)
		}
	}
	if decoded["mutations_applied"] != false {
		t.Fatalf("mutations_applied = %#v, want false", decoded["mutations_applied"])
	}
	if decoded["collection_status"] != "collected" {
		t.Fatalf("collection_status = %#v, want collected", decoded["collection_status"])
	}
}

func TestDeviceCollectionStatusDistinguishesPartialAndUnavailable(t *testing.T) {
	errorValue := platformprofile.SourceError{Source: "windows.smbios", Message: "firmware API unavailable"}
	if got := deviceCollectionStatus(platformprofile.Result{SourceErrors: []platformprofile.SourceError{errorValue}}); got != "unavailable" {
		t.Fatalf("status without usable observations = %q, want unavailable", got)
	}

	partial := platformprofile.Result{
		Observations: moduleprofile.Observations{
			Capabilities: moduleprofile.Capabilities{
				Touch: moduleprofile.No,
			},
		},
		SourceErrors: []platformprofile.SourceError{errorValue},
	}
	if got := deviceCollectionStatus(partial); got != "partial" {
		t.Fatalf("status with usable observations and an error = %q, want partial", got)
	}
}

func TestDeviceProfileTextReportShowsUnknownAndNoMutation(t *testing.T) {
	report := buildDeviceProfileReport(platformprofile.Result{})
	var stdout bytes.Buffer
	writeDeviceProfileText(&stdout, report)
	output := stdout.String()
	for _, expected := range []string{
		"Tipo de equipo: Desconocido",
		"Confianza: Baja",
		"Pantalla táctil: Desconocido",
		"GPU dedicada: Desconocido",
		"Cambios aplicados: no",
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("device profile report does not contain %q: %s", expected, output)
		}
	}
}

func TestDeviceProfileCommandRejectsInvalidArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"device-profile", "--apply"}, &stdout, &stderr); code != 2 {
		t.Fatalf("invalid flag code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "uso: soptraz device-profile [--json]") {
		t.Fatalf("unexpected usage: %q", stderr.String())
	}
}

func TestDeviceProfileReportRetainsObservedFormFactorEvidence(t *testing.T) {
	report := buildDeviceProfileReport(platformprofile.Result{
		Observations: moduleprofile.Observations{
			SMBIOSChassisTypes: []uint16{30},
			Capabilities: moduleprofile.Capabilities{
				Touch:          moduleprofile.Yes,
				BatteryPresent: moduleprofile.Yes,
				PowerSource:    moduleprofile.PowerSourceAC,
			},
		},
	})
	if report.Profile.FormFactor != moduleprofile.FormFactorTablet {
		t.Fatalf("form factor = %q, want tablet", report.Profile.FormFactor)
	}
	if report.Profile.Confidence != moduleprofile.ConfidenceMedium {
		t.Fatalf("confidence = %q, want medium", report.Profile.Confidence)
	}
	if len(report.Profile.Evidence) != 1 || report.Profile.Evidence[0].Value != "30" {
		t.Fatalf("unexpected evidence: %#v", report.Profile.Evidence)
	}
}


func TestValidateAcceptsReadOnlyProcessProfileExample(t *testing.T) {
	examplePath := filepath.Join("..", "..", "..", "examples", "contracts", "process-profile.example.json")
	raw, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatalf("read process profile example: %v", err)
	}
	path := writeTempJSON(t, string(raw))
	var stdout, stderr bytes.Buffer
	code := Run([]string{"validate", "process-profile", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run() code = %d; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Contrato process-profile válido") {
		t.Fatalf("unexpected output: %q", stdout.String())
	}
}
