package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/JavierGrecco/SOPTRAZ/internal/platform/trustedtime"
)

type fakeTimeDiagnosticsProvider struct {
	diagnostics trustedtime.SystemTimeDiagnostics
	err         error
}

func (p fakeTimeDiagnosticsProvider) Observe(context.Context) (trustedtime.SystemTimeDiagnostics, error) {
	return p.diagnostics, p.err
}

func validTimeStatusDiagnostics() trustedtime.SystemTimeDiagnostics {
	return trustedtime.SystemTimeDiagnostics{
		WallClockObservedAt:  time.Date(2026, time.November, 28, 12, 0, 0, 0, time.FixedZone("UTC-03", -3*60*60)),
		ServiceState:         trustedtime.ServiceStateActive,
		SynchronizationState: trustedtime.SynchronizationUnknown,
		Mechanism:            trustedtime.TimeMechanismLinuxKernel,
	}
}

func TestTimeStatusTextReportIsReadOnlyAndExplicit(t *testing.T) {
	var stdout, stderr bytes.Buffer
	provider := fakeTimeDiagnosticsProvider{diagnostics: validTimeStatusDiagnostics()}

	code := runTimeStatusWithProvider([]string{"time-status"}, &stdout, &stderr, provider)
	if code != 0 {
		t.Fatalf("runTimeStatusWithProvider() code = %d, stderr=%q", code, stderr.String())
	}
	output := stdout.String()
	for _, expected := range []string{
		"Servicio horario: activo",
		"Sincronización informada: desconocido",
		"Mecanismo: kernel de Linux (adjtimex)",
		"Última sincronización: desconocida",
		"No es evidencia temporal criptográfica",
		"no modifica",
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("text report missing %q: %s", expected, output)
		}
	}
	if strings.Contains(stderr.String(), "error") {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestTimeStatusJSONReportHasStableFields(t *testing.T) {
	var stdout, stderr bytes.Buffer
	provider := fakeTimeDiagnosticsProvider{
		diagnostics: validTimeStatusDiagnostics(),
	}
	code := runTimeStatusWithProvider([]string{"time-status", "--json"}, &stdout, &stderr, provider)
	if code != 0 {
		t.Fatalf("runTimeStatusWithProvider() code = %d, stderr=%q", code, stderr.String())
	}

	var report map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if report["schema_version"] != "1.0.0" || report["status"] != "available" {
		t.Fatalf("unexpected status envelope: %#v", report)
	}
	if report["service_state"] != "active" ||
		report["synchronization_state"] != "unknown" ||
		report["mechanism"] != "linux-kernel" {
		t.Fatalf("unexpected diagnostic fields: %#v", report)
	}
	if _, exists := report["last_sync_at"]; exists {
		t.Fatalf("last_sync_at should be omitted when unknown: %#v", report)
	}
	if !strings.Contains(report["note"].(string), "no valida expiración TUF") {
		t.Fatalf("missing trust limitation: %#v", report["note"])
	}
}

func TestTimeStatusUnavailableIsNonFatalAndReportsUnknown(t *testing.T) {
	var stdout, stderr bytes.Buffer
	provider := fakeTimeDiagnosticsProvider{
		diagnostics: trustedtime.SystemTimeDiagnostics{
			WallClockObservedAt:  time.Date(2026, time.November, 28, 12, 0, 0, 0, time.UTC),
			ServiceState:         trustedtime.ServiceStateUnknown,
			SynchronizationState: trustedtime.SynchronizationUnknown,
			Mechanism:            trustedtime.TimeMechanismUnknown,
		},
		err: trustedtime.ErrSystemTimeDiagnosticsUnavailable,
	}

	code := runTimeStatusWithProvider([]string{"time-status", "--json"}, &stdout, &stderr, provider)
	if code != 0 {
		t.Fatalf("unavailable diagnostics must not be fatal: code=%d stderr=%q", code, stderr.String())
	}
	var report timeStatusReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if report.Status != "unavailable" ||
		report.ServiceState != trustedtime.ServiceStateUnknown ||
		report.SynchronizationState != trustedtime.SynchronizationUnknown {
		t.Fatalf("unexpected unavailable report: %#v", report)
	}
}

func TestTimeStatusSanitizesUnexpectedProviderErrors(t *testing.T) {
	var stdout, stderr bytes.Buffer
	raw := errors.New("permission denied: internal kernel detail")
	provider := fakeTimeDiagnosticsProvider{err: raw}
	code := runTimeStatusWithProvider([]string{"time-status"}, &stdout, &stderr, provider)
	if code != 1 {
		t.Fatalf("unexpected provider failure code = %d, want 1", code)
	}
	if strings.Contains(stderr.String(), raw.Error()) || strings.Contains(stdout.String(), raw.Error()) {
		t.Fatal("raw provider error leaked to CLI output")
	}
}

func TestTimeStatusRejectsInvalidProviderDiagnostics(t *testing.T) {
	var stdout, stderr bytes.Buffer
	provider := fakeTimeDiagnosticsProvider{}
	code := runTimeStatusWithProvider([]string{"time-status"}, &stdout, &stderr, provider)
	if code != 1 {
		t.Fatalf("invalid diagnostics code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "diagnóstico horario inválido") {
		t.Fatalf("unexpected error message: %q", stderr.String())
	}
}

func TestTimeStatusRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{
		{"time-status", "--verbose"},
		{"time-status", "--json", "extra"},
	} {
		var stdout, stderr bytes.Buffer
		if code := runTimeStatusWithProvider(args, &stdout, &stderr, fakeTimeDiagnosticsProvider{}); code != 2 {
			t.Errorf("args %v: code = %d, want 2", args, code)
		}
	}
}
