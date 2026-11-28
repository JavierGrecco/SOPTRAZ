//go:build windows

package trustedtime

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/sys/windows"
)

func TestMapWindowsW32TimeServiceState(t *testing.T) {
	tests := []struct {
		name  string
		state uint32
		want  ServiceState
	}{
		{"running", windows.SERVICE_RUNNING, ServiceStateActive},
		{"stopped", windows.SERVICE_STOPPED, ServiceStateInactive},
		{"paused", windows.SERVICE_PAUSED, ServiceStateInactive},
		{"start pending", windows.SERVICE_START_PENDING, ServiceStateUnknown},
		{"stop pending", windows.SERVICE_STOP_PENDING, ServiceStateUnknown},
		{"continue pending", windows.SERVICE_CONTINUE_PENDING, ServiceStateUnknown},
		{"pause pending", windows.SERVICE_PAUSE_PENDING, ServiceStateUnknown},
		{"unrecognized", 99, ServiceStateUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := mapWindowsW32TimeServiceState(test.state); got != test.want {
				t.Fatalf("state = %q, want %q", got, test.want)
			}
		})
	}
}

func TestWindowsSystemTimeDiagnosticsProviderReportsOnlyServiceState(t *testing.T) {
	called := false
	provider := windowsSystemTimeDiagnosticsProvider{
		queryServiceState: func() (ServiceState, error) {
			called = true
			return ServiceStateActive, nil
		},
	}
	diagnostics, err := provider.Observe(context.Background())
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if !called {
		t.Fatal("service state query was not called")
	}
	if diagnostics.WallClockObservedAt.IsZero() {
		t.Fatal("WallClockObservedAt must be populated")
	}
	if diagnostics.ServiceState != ServiceStateActive {
		t.Fatalf("ServiceState = %q, want active", diagnostics.ServiceState)
	}
	if diagnostics.SynchronizationState != SynchronizationUnknown {
		t.Fatalf("SynchronizationState = %q, want unknown", diagnostics.SynchronizationState)
	}
	if diagnostics.Mechanism != TimeMechanismWindowsW32Time {
		t.Fatalf("Mechanism = %q, want windows-w32time", diagnostics.Mechanism)
	}
	if !diagnostics.LastSyncAt.IsZero() {
		t.Fatal("provider must not invent a last synchronization timestamp")
	}
	if err := diagnostics.Validate(); err != nil {
		t.Fatalf("returned diagnostics invalid: %v", err)
	}
}

func TestWindowsSystemTimeDiagnosticsProviderSanitizesQueryErrors(t *testing.T) {
	rawErr := errors.New("access denied: sensitive SCM descriptor detail")
	provider := windowsSystemTimeDiagnosticsProvider{
		queryServiceState: func() (ServiceState, error) {
			return ServiceStateUnknown, rawErr
		},
	}
	diagnostics, err := provider.Observe(context.Background())
	if !errors.Is(err, ErrSystemTimeDiagnosticsUnavailable) {
		t.Fatalf("Observe() error = %v, want ErrSystemTimeDiagnosticsUnavailable", err)
	}
	if err.Error() == rawErr.Error() {
		t.Fatal("raw SCM error escaped diagnostics")
	}
	if diagnostics.ServiceState != ServiceStateUnknown ||
		diagnostics.SynchronizationState != SynchronizationUnknown {
		t.Fatal("failed query must return unknown states")
	}
}

func TestWindowsSystemTimeDiagnosticsProviderContextCancellation(t *testing.T) {
	called := false
	provider := windowsSystemTimeDiagnosticsProvider{
		queryServiceState: func() (ServiceState, error) {
			called = true
			return ServiceStateActive, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := provider.Observe(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Observe() error = %v, want context.Canceled", err)
	}
	if called {
		t.Fatal("SCM query called after context cancellation")
	}
}

func TestNewWindowsSystemTimeDiagnosticsProviderReadOnlySmoke(t *testing.T) {
	diagnostics, err := NewSystemTimeDiagnosticsProvider().Observe(context.Background())
	if err != nil && !errors.Is(err, ErrSystemTimeDiagnosticsUnavailable) {
		t.Fatalf("Observe() returned unexpected error: %v", err)
	}
	if diagnostics.Mechanism != TimeMechanismWindowsW32Time {
		t.Fatalf("Mechanism = %q, want windows-w32time", diagnostics.Mechanism)
	}
	if diagnostics.ServiceState != ServiceStateUnknown &&
		diagnostics.ServiceState != ServiceStateActive &&
		diagnostics.ServiceState != ServiceStateInactive {
		t.Fatalf("invalid service state %q", diagnostics.ServiceState)
	}
	if diagnostics.SynchronizationState != SynchronizationUnknown {
		t.Fatalf("SynchronizationState = %q, want unknown", diagnostics.SynchronizationState)
	}
	if err := diagnostics.Validate(); err != nil {
		t.Fatalf("provider returned invalid diagnostics: %v", err)
	}
}
