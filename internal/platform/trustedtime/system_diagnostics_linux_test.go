//go:build linux

package trustedtime

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

func TestReadLinuxKernelSynchronizationStateUsesReadOnlyModes(t *testing.T) {
	tests := []struct {
		name  string
		state int
		want  SynchronizationState
	}{
		{name: "clock synchronized", state: linuxTimeOK, want: SynchronizationSynchronized},
		{name: "leap insertion pending", state: linuxTimeInsertLeap, want: SynchronizationSynchronized},
		{name: "leap deletion pending", state: linuxTimeDeleteLeap, want: SynchronizationSynchronized},
		{name: "leap second in progress", state: linuxTimeLeapInProgress, want: SynchronizationSynchronized},
		{name: "leap operation complete", state: linuxTimeLeapComplete, want: SynchronizationSynchronized},
		{name: "kernel reports unsynchronized", state: linuxTimeError, want: SynchronizationNotSynchronized},
		{name: "unknown kernel state", state: 99, want: SynchronizationUnknown},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			called := false
			got, err := readLinuxKernelSynchronizationState(func(request *unix.Timex) (int, error) {
				called = true
				if request.Modes != 0 {
					t.Fatalf("adjtimex Modes = %d, want 0 (read-only)", request.Modes)
				}
				return test.state, nil
			})
			if !called {
				t.Fatal("adjtimex was not called")
			}
			if got != test.want {
				t.Fatalf("synchronization state = %q, want %q", got, test.want)
			}
			if test.state == 99 && !errors.Is(err, ErrSystemTimeDiagnosticsUnavailable) {
				t.Fatalf("unknown state error = %v, want ErrSystemTimeDiagnosticsUnavailable", err)
			}
			if test.state != 99 && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestReadLinuxKernelSynchronizationStateSanitizesSyscallErrors(t *testing.T) {
	rawErr := errors.New("permission denied: sensitive kernel detail")
	got, err := readLinuxKernelSynchronizationState(func(request *unix.Timex) (int, error) {
		if request.Modes != 0 {
			t.Fatalf("adjtimex Modes = %d, want 0", request.Modes)
		}
		return -1, rawErr
	})

	if got != SynchronizationUnknown {
		t.Fatalf("synchronization state = %q, want unknown", got)
	}
	if !errors.Is(err, ErrSystemTimeDiagnosticsUnavailable) {
		t.Fatalf("error = %v, want ErrSystemTimeDiagnosticsUnavailable", err)
	}
	if err.Error() == rawErr.Error() {
		t.Fatal("raw syscall error escaped diagnostics")
	}
}

func TestLinuxSystemTimeDiagnosticsProviderObserve(t *testing.T) {
	provider := linuxSystemTimeDiagnosticsProvider{
		adjtimex: func(request *unix.Timex) (int, error) {
			if request.Modes != 0 {
				t.Fatalf("adjtimex Modes = %d, want 0", request.Modes)
			}
			return linuxTimeError, nil
		},
	}

	diagnostics, err := provider.Observe(context.Background())
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if diagnostics.WallClockObservedAt.IsZero() {
		t.Fatal("WallClockObservedAt must be populated")
	}
	if diagnostics.Mechanism != TimeMechanismLinuxKernel {
		t.Fatalf("Mechanism = %q, want linux-kernel", diagnostics.Mechanism)
	}
	if diagnostics.ServiceState != ServiceStateUnknown {
		t.Fatalf("ServiceState = %q, want unknown; adjtimex does not identify a service", diagnostics.ServiceState)
	}
	if diagnostics.SynchronizationState != SynchronizationNotSynchronized {
		t.Fatalf("SynchronizationState = %q, want not_synchronized", diagnostics.SynchronizationState)
	}
	if !diagnostics.LastSyncAt.IsZero() {
		t.Fatal("adjtimex does not expose last synchronization time")
	}
	if err := diagnostics.Validate(); err != nil {
		t.Fatalf("returned diagnostics invalid: %v", err)
	}
}

func TestLinuxSystemTimeDiagnosticsProviderContextCancellation(t *testing.T) {
	called := false
	provider := linuxSystemTimeDiagnosticsProvider{
		adjtimex: func(*unix.Timex) (int, error) {
			called = true
			return linuxTimeOK, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := provider.Observe(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Observe() error = %v, want context.Canceled", err)
	}
	if called {
		t.Fatal("adjtimex called after context was canceled")
	}
}

func TestNewLinuxSystemTimeDiagnosticsProviderReturnsKernelObservation(t *testing.T) {
	provider := NewSystemTimeDiagnosticsProvider()
	diagnostics, err := provider.Observe(context.Background())
	if err != nil && !errors.Is(err, ErrSystemTimeDiagnosticsUnavailable) {
		t.Fatalf("Observe() returned unexpected error: %v", err)
	}
	if diagnostics.WallClockObservedAt.IsZero() {
		t.Fatal("provider must return an observation timestamp, even when the kernel signal is unavailable")
	}
	if diagnostics.Mechanism != TimeMechanismLinuxKernel {
		t.Fatalf("Mechanism = %q, want linux-kernel", diagnostics.Mechanism)
	}
	if diagnostics.SynchronizationState != SynchronizationUnknown &&
		diagnostics.SynchronizationState != SynchronizationSynchronized &&
		diagnostics.SynchronizationState != SynchronizationNotSynchronized {
		t.Fatalf("invalid synchronization state %q", diagnostics.SynchronizationState)
	}
	if !diagnostics.LastSyncAt.IsZero() {
		t.Fatal("provider must not invent a last synchronization timestamp")
	}
	if err := diagnostics.Validate(); err != nil {
		t.Fatalf("provider returned invalid diagnostics: %v", err)
	}
}
