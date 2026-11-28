package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	moduleprofile "github.com/JavierGrecco/SOPTRAZ/internal/modules/deviceprofile"
	platformprofile "github.com/JavierGrecco/SOPTRAZ/internal/platform/deviceprofile"
)

func TestRunDeviceProfileJSONProducesReadOnlyReport(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"device-profile", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run() code = %d; stderr=%q", code, stderr.String())
	}

	var report struct {
		CollectionStatus string                        `json:"collection_status"`
		Observations     moduleprofile.Observations    `json:"observations"`
		Profile          moduleprofile.Profile         `json:"profile"`
		SourceErrors     []platformprofile.SourceError `json:"source_errors"`
		MutationsApplied bool                          `json:"mutations_applied"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid device profile JSON: %v; output=%s", err, stdout.String())
	}
	if report.CollectionStatus != "collected" && report.CollectionStatus != "partial" && report.CollectionStatus != "unavailable" {
		t.Fatalf("unexpected collection status %q", report.CollectionStatus)
	}
	if report.MutationsApplied {
		t.Fatal("device-profile command must never apply mutations")
	}
	for _, value := range []moduleprofile.TriState{
		report.Observations.Capabilities.BatteryPresent,
		report.Observations.Capabilities.Touch,
		report.Observations.Capabilities.Pen,
		report.Profile.Capabilities.BatteryPresent,
		report.Profile.Capabilities.Touch,
		report.Profile.Capabilities.Pen,
	} {
		if value > moduleprofile.Yes {
			t.Fatalf("invalid tri-state value %d in JSON report", value)
		}
	}
}


func TestDeviceCollectionStatusTreatsZeroValueEnumsAsUnknown(t *testing.T) {
	result := platformprofile.Result{
		SourceErrors: []platformprofile.SourceError{{Source: "windows.smbios", Message: "unavailable"}},
	}
	if got := deviceCollectionStatus(result); got != "unavailable" {
		t.Fatalf("zero-value observations status = %q, want unavailable", got)
	}
}

func TestDeviceProfileTextCommandIsLocalizedAndReadOnly(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"device-profile"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run() code = %d; stderr=%q", code, stderr.String())
	}
	for _, expected := range []string{"Tipo de equipo:", "Confianza:", "Capacidades observadas:", "Cambios aplicados: no"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Errorf("text report missing %q: %s", expected, stdout.String())
		}
	}
}
