//go:build windows

package trustedtime

import (
	"context"
	"errors"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsSystemTimeDiagnosticsProvider struct {
	queryServiceState func() (ServiceState, error)
}

// NewSystemTimeDiagnosticsProvider returns a Windows provider that checks
// only the W32Time service state through the Service Control Manager.
func NewSystemTimeDiagnosticsProvider() SystemTimeDiagnosticsProvider {
	return windowsSystemTimeDiagnosticsProvider{}
}

func (p windowsSystemTimeDiagnosticsProvider) Observe(ctx context.Context) (SystemTimeDiagnostics, error) {
	if ctx == nil {
		return SystemTimeDiagnostics{}, ErrSystemTimeDiagnosticsUnavailable
	}
	if err := ctx.Err(); err != nil {
		return SystemTimeDiagnostics{}, err
	}

	diagnostics := SystemTimeDiagnostics{
		WallClockObservedAt:  time.Now().UTC().Round(0),
		ServiceState:         ServiceStateUnknown,
		SynchronizationState: SynchronizationUnknown,
		Mechanism:            TimeMechanismWindowsW32Time,
	}

	query := p.queryServiceState
	if query == nil {
		query = queryWindowsW32TimeServiceState
	}
	state, err := query()
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return diagnostics, err
		}
		return diagnostics, ErrSystemTimeDiagnosticsUnavailable
	}

	diagnostics.ServiceState = state
	return diagnostics, nil
}

// queryWindowsW32TimeServiceState uses only SC_MANAGER_CONNECT and
// SERVICE_QUERY_STATUS. It never sends service controls or changes
// configuration, clock settings, registry values or network peers.
func queryWindowsW32TimeServiceState() (ServiceState, error) {
	manager, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return ServiceStateUnknown, ErrSystemTimeDiagnosticsUnavailable
	}
	defer func() { _ = windows.CloseServiceHandle(manager) }()

	serviceName, err := windows.UTF16PtrFromString("W32Time")
	if err != nil {
		return ServiceStateUnknown, ErrSystemTimeDiagnosticsUnavailable
	}
	service, err := windows.OpenService(manager, serviceName, windows.SERVICE_QUERY_STATUS)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return ServiceStateInactive, nil
	}
	if err != nil {
		return ServiceStateUnknown, ErrSystemTimeDiagnosticsUnavailable
	}
	defer func() { _ = windows.CloseServiceHandle(service) }()

	var status windows.SERVICE_STATUS_PROCESS
	var bytesNeeded uint32
	err = windows.QueryServiceStatusEx(
		service,
		windows.SC_STATUS_PROCESS_INFO,
		(*byte)(unsafe.Pointer(&status)),
		uint32(unsafe.Sizeof(status)),
		&bytesNeeded,
	)
	if err != nil {
		return ServiceStateUnknown, ErrSystemTimeDiagnosticsUnavailable
	}
	return mapWindowsW32TimeServiceState(status.CurrentState), nil
}

// mapWindowsW32TimeServiceState reports only stable running/stopped states.
// Transitional states remain unknown instead of being guessed.
func mapWindowsW32TimeServiceState(state uint32) ServiceState {
	switch state {
	case windows.SERVICE_RUNNING:
		return ServiceStateActive
	case windows.SERVICE_STOPPED, windows.SERVICE_PAUSED:
		return ServiceStateInactive
	default:
		return ServiceStateUnknown
	}
}
