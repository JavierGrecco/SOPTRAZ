package trustedtime

import (
	"errors"
	"fmt"
	"time"
)

// ErrInvalidDiagnostics identifies structurally invalid system time diagnostics.
var ErrInvalidDiagnostics = errors.New("diagnóstico horario del sistema inválido")

// ServiceState describes whether the observed operating-system time service
// is running. It is an operational diagnostic, not proof of synchronization.
type ServiceState string

const (
	ServiceStateUnknown  ServiceState = "unknown"
	ServiceStateActive   ServiceState = "active"
	ServiceStateInactive ServiceState = "inactive"
)

// SynchronizationState describes what a local operating-system interface
// reports about clock synchronization. It does not authenticate the source.
type SynchronizationState string

const (
	SynchronizationUnknown         SynchronizationState = "unknown"
	SynchronizationSynchronized    SynchronizationState = "synchronized"
	SynchronizationNotSynchronized SynchronizationState = "not_synchronized"
)

// TimeMechanism is a stable identifier for the local interface used to
// observe system time diagnostics. It must never contain raw command output,
// hostnames, addresses or other values copied from an untrusted source.
type TimeMechanism string

const (
	TimeMechanismUnknown          TimeMechanism = "unknown"
	TimeMechanismWindowsW32Time   TimeMechanism = "windows-w32time"
	TimeMechanismLinuxKernel      TimeMechanism = "linux-kernel"
	TimeMechanismSystemdTimedate1 TimeMechanism = "systemd-timedate1"
	TimeMechanismMacOSTimed       TimeMechanism = "macos-timed"
	TimeMechanismCustom           TimeMechanism = "custom"
)

// SystemTimeDiagnostics contains read-only observations about the local
// operating-system clock and its synchronization service.
//
// WallClockObservedAt is the system wall clock at observation time. It is
// intentionally not represented as an Interval and must never be converted
// into trustedtime.Evidence: the local clock does not provide a defensible
// uncertainty bound by itself.
//
// A zero LastSyncAt means that the interface did not expose a usable value.
// ServiceState and SynchronizationState are independent observations; callers
// must not infer one from the other. In particular, an active service does not
// imply that the clock is synchronized.
//
// This structure is diagnostic only. It cannot update LastAcceptedTrustedTime,
// validate TUF metadata expiration, authorize the offline catalog or produce a
// VerifiedTarget.
type SystemTimeDiagnostics struct {
	WallClockObservedAt  time.Time
	ServiceState         ServiceState
	SynchronizationState SynchronizationState
	Mechanism            TimeMechanism
	LastSyncAt           time.Time
}

// Validate checks the structural shape of a diagnostics observation. It does
// not verify the accuracy of the wall clock or authenticate a time source.
func (d SystemTimeDiagnostics) Validate() error {
	if d.WallClockObservedAt.IsZero() {
		return fmt.Errorf("%w: falta WallClockObservedAt", ErrInvalidDiagnostics)
	}

	switch d.ServiceState {
	case ServiceStateUnknown, ServiceStateActive, ServiceStateInactive:
	default:
		return fmt.Errorf("%w: ServiceState desconocido", ErrInvalidDiagnostics)
	}

	switch d.SynchronizationState {
	case SynchronizationUnknown, SynchronizationSynchronized, SynchronizationNotSynchronized:
	default:
		return fmt.Errorf("%w: SynchronizationState desconocido", ErrInvalidDiagnostics)
	}

	switch d.Mechanism {
	case TimeMechanismUnknown,
		TimeMechanismWindowsW32Time,
		TimeMechanismLinuxKernel,
		TimeMechanismSystemdTimedate1,
		TimeMechanismMacOSTimed,
		TimeMechanismCustom:
	default:
		return fmt.Errorf("%w: TimeMechanism desconocido", ErrInvalidDiagnostics)
	}

	return nil
}

// Canonical returns a validated copy with UTC wall-clock timestamps and no
// process-local monotonic clock readings. It does not change the receiver.
// It deliberately does not compare LastSyncAt with WallClockObservedAt:
// clock anomalies are exactly what this diagnostic may be reporting.
func (d SystemTimeDiagnostics) Canonical() (SystemTimeDiagnostics, error) {
	if err := d.Validate(); err != nil {
		return SystemTimeDiagnostics{}, err
	}

	d.WallClockObservedAt = d.WallClockObservedAt.UTC().Round(0)
	if !d.LastSyncAt.IsZero() {
		d.LastSyncAt = d.LastSyncAt.UTC().Round(0)
	}
	return d, nil
}
