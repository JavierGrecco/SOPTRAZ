package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestHelpIsReadOnlyAndSuccessful(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run() code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "No modifica el equipo") {
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

func TestUnknownCommandReturnsUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"repair-everything"}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "comando desconocido") {
		t.Fatalf("unexpected result: code=%d stderr=%q", code, stderr.String())
	}
}
