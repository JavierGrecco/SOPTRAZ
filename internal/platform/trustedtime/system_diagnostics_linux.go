//go:build linux

package trustedtime

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sys/unix"
)

const (
	linuxTimeOK = iota
	linuxTimeInsertLeap
	linuxTimeDeleteLeap
	linuxTimeLeapInProgress
	linuxTimeLeapComplete
	linuxTimeError
)

type linuxSystemTimeDiagnosticsProvider struct {
	adjtimex func(*unix.Timex) (int, error)
}

// NewSystemTimeDiagnosticsProvider returns a Linux provider that reads the
// kernel clock-discipline state with adjtimex(2). It does not query a daemon
// or identify the time server/operator.
func NewSystemTimeDiagnosticsProvider() SystemTimeDiagnosticsProvider {
	return linuxSystemTimeDiagnosticsProvider{adjtimex: unix.Adjtimex}
}

func (p linuxSystemTimeDiagnosticsProvider) Observe(ctx context.Context) (SystemTimeDiagnostics, error) {
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
		Mechanism:            TimeMechanismLinuxKernel,
	}

	adjtimex := p.adjtimex
	if adjtimex == nil {
		adjtimex = unix.Adjtimex
	}
	state, err := readLinuxKernelSynchronizationState(adjtimex)
	if err != nil {
		return diagnostics, err
	}

	diagnostics.SynchronizationState = state
	return diagnostics, nil
}

// readLinuxKernelSynchronizationState calls adjtimex with Modes=0. Linux
// documents this as the unprivileged read-only form; non-zero adjustment
// modes require CAP_SYS_TIME. The returned kernel state is operational
// diagnostic data only and does not authenticate a server or operator.
func readLinuxKernelSynchronizationState(
	adjtimex func(*unix.Timex) (int, error),
) (SynchronizationState, error) {
	if adjtimex == nil {
		return SynchronizationUnknown, ErrSystemTimeDiagnosticsUnavailable
	}

	var request unix.Timex
	request.Modes = 0
	state, err := adjtimex(&request)
	if err != nil {
		// Do not surface raw errno or kernel details in normal diagnostics.
		return SynchronizationUnknown, ErrSystemTimeDiagnosticsUnavailable
	}

	switch state {
	case linuxTimeOK,
		linuxTimeInsertLeap,
		linuxTimeDeleteLeap,
		linuxTimeLeapInProgress,
		linuxTimeLeapComplete:
		return SynchronizationSynchronized, nil
	case linuxTimeError:
		return SynchronizationNotSynchronized, nil
	default:
		return SynchronizationUnknown, fmt.Errorf("%w: estado del kernel no reconocido", ErrSystemTimeDiagnosticsUnavailable)
	}
}
