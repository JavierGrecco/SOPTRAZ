package trustedtime

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func validSystemTimeDiagnostics() SystemTimeDiagnostics {
	return SystemTimeDiagnostics{
		WallClockObservedAt:  time.Date(2026, time.November, 28, 15, 0, 0, 0, time.UTC),
		ServiceState:         ServiceStateActive,
		SynchronizationState: SynchronizationUnknown,
		Mechanism:            TimeMechanismLinuxKernel,
	}
}

func TestSystemTimeDiagnosticsValidate(t *testing.T) {
	if err := validSystemTimeDiagnostics().Validate(); err != nil {
		t.Fatalf("valid diagnostics rejected: %v", err)
	}
}

func TestSystemTimeDiagnosticsValidateAllowsUnknownStates(t *testing.T) {
	diagnostics := SystemTimeDiagnostics{
		WallClockObservedAt:  time.Date(2026, time.November, 28, 15, 0, 0, 0, time.UTC),
		ServiceState:         ServiceStateUnknown,
		SynchronizationState: SynchronizationUnknown,
		Mechanism:            TimeMechanismUnknown,
	}
	if err := diagnostics.Validate(); err != nil {
		t.Fatalf("unknown diagnostic states must be representable: %v", err)
	}
}

func TestSystemTimeDiagnosticsValidateDoesNotInferSynchronizationFromService(t *testing.T) {
	tests := []struct {
		name            string
		service         ServiceState
		synchronization SynchronizationState
	}{
		{
			name:            "active service but not synchronized",
			service:         ServiceStateActive,
			synchronization: SynchronizationNotSynchronized,
		},
		{
			name:            "active service with unknown synchronization",
			service:         ServiceStateActive,
			synchronization: SynchronizationUnknown,
		},
		{
			name:            "inactive service and unknown synchronization",
			service:         ServiceStateInactive,
			synchronization: SynchronizationUnknown,
		},
		{
			name:            "inactive service with an independent kernel synchronization signal",
			service:         ServiceStateInactive,
			synchronization: SynchronizationSynchronized,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			diagnostics := validSystemTimeDiagnostics()
			diagnostics.ServiceState = test.service
			diagnostics.SynchronizationState = test.synchronization
			if err := diagnostics.Validate(); err != nil {
				t.Fatalf("independent states must not be inferred or cross-rejected: %v", err)
			}
		})
	}
}

func TestSystemTimeDiagnosticsValidateRejectsMalformedValues(t *testing.T) {
	tests := []struct {
		name string
		edit func(*SystemTimeDiagnostics)
	}{
		{
			name: "missing wall clock observation",
			edit: func(d *SystemTimeDiagnostics) { d.WallClockObservedAt = time.Time{} },
		},
		{
			name: "invalid service state",
			edit: func(d *SystemTimeDiagnostics) { d.ServiceState = "running-ish" },
		},
		{
			name: "invalid synchronization state",
			edit: func(d *SystemTimeDiagnostics) { d.SynchronizationState = "probably-synced" },
		},
		{
			name: "invalid mechanism",
			edit: func(d *SystemTimeDiagnostics) { d.Mechanism = TimeMechanism("w32tm output: host.example") },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			diagnostics := validSystemTimeDiagnostics()
			test.edit(&diagnostics)
			if err := diagnostics.Validate(); !errors.Is(err, ErrInvalidDiagnostics) {
				t.Fatalf("Validate() error = %v, want ErrInvalidDiagnostics", err)
			}
		})
	}
}

func TestSystemTimeDiagnosticsCanonicalNormalizesTimestampsWithoutMutatingInput(t *testing.T) {
	zone := time.FixedZone("UTC-03", -3*60*60)
	observed := time.Date(2026, time.November, 28, 12, 0, 0, 0, zone)
	lastSync := observed.Add(-5 * time.Minute)
	diagnostics := SystemTimeDiagnostics{
		WallClockObservedAt:  observed,
		ServiceState:         ServiceStateActive,
		SynchronizationState: SynchronizationSynchronized,
		Mechanism:            TimeMechanismSystemdTimedate1,
		LastSyncAt:           lastSync,
	}

	canonical, err := diagnostics.Canonical()
	if err != nil {
		t.Fatalf("Canonical() error = %v", err)
	}
	if canonical.WallClockObservedAt.Location() != time.UTC || canonical.LastSyncAt.Location() != time.UTC {
		t.Fatal("canonical timestamps must use UTC")
	}
	if !canonical.WallClockObservedAt.Equal(observed) || !canonical.LastSyncAt.Equal(lastSync) {
		t.Fatal("canonicalization changed the represented instants")
	}
	if diagnostics.WallClockObservedAt.Location() != zone || diagnostics.LastSyncAt.Location() != zone {
		t.Fatal("Canonical() mutated its receiver")
	}
}

func TestSystemTimeDiagnosticsCanonicalStripsMonotonicReadings(t *testing.T) {
	now := time.Now()
	diagnostics := SystemTimeDiagnostics{
		WallClockObservedAt:  now,
		ServiceState:         ServiceStateUnknown,
		SynchronizationState: SynchronizationUnknown,
		Mechanism:            TimeMechanismUnknown,
		LastSyncAt:           now.Add(-time.Minute),
	}

	canonical, err := diagnostics.Canonical()
	if err != nil {
		t.Fatalf("Canonical() error = %v", err)
	}
	if strings.Contains(canonical.WallClockObservedAt.String(), "m=") ||
		strings.Contains(canonical.LastSyncAt.String(), "m=") {
		t.Fatal("canonical diagnostics must not retain process-local monotonic readings")
	}
}

func TestSystemTimeDiagnosticsCanonicalAllowsClockAnomalyForReporting(t *testing.T) {
	observed := time.Date(2026, time.November, 28, 10, 0, 0, 0, time.UTC)
	diagnostics := SystemTimeDiagnostics{
		WallClockObservedAt:  observed,
		ServiceState:         ServiceStateInactive,
		SynchronizationState: SynchronizationUnknown,
		Mechanism:            TimeMechanismCustom,
		// LastSyncAt may be later than the current wall clock when a clock
		// anomaly exists. Preserve it as diagnostic data; do not "correct" it.
		LastSyncAt: observed.Add(24 * time.Hour),
	}
	if _, err := diagnostics.Canonical(); err != nil {
		t.Fatalf("clock anomaly must remain reportable: %v", err)
	}
}
