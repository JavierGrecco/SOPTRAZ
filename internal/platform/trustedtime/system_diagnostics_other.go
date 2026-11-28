//go:build !linux && !windows

package trustedtime

import (
	"context"
	"time"
)

// unknownSystemTimeDiagnosticsProvider is the safe fallback until a
// non-Linux platform has an adapter with a public, unprivileged and
// deterministic synchronization signal.
type unknownSystemTimeDiagnosticsProvider struct{}

// NewSystemTimeDiagnosticsProvider returns an explicit unknown result on
// platforms whose native adapter is not implemented yet. It never invokes
// privileged commands or reads private operating-system state.
func NewSystemTimeDiagnosticsProvider() SystemTimeDiagnosticsProvider {
	return unknownSystemTimeDiagnosticsProvider{}
}

func (unknownSystemTimeDiagnosticsProvider) Observe(ctx context.Context) (SystemTimeDiagnostics, error) {
	if ctx == nil {
		return SystemTimeDiagnostics{}, ErrSystemTimeDiagnosticsUnavailable
	}
	if err := ctx.Err(); err != nil {
		return SystemTimeDiagnostics{}, err
	}
	return SystemTimeDiagnostics{
		WallClockObservedAt:  time.Now().UTC().Round(0),
		ServiceState:         ServiceStateUnknown,
		SynchronizationState: SynchronizationUnknown,
		Mechanism:            TimeMechanismUnknown,
	}, ErrSystemTimeDiagnosticsUnavailable
}
