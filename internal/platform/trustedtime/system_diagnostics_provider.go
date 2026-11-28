package trustedtime

import (
	"context"
	"errors"
)

// ErrSystemTimeDiagnosticsUnavailable indicates that the platform adapter
// could not obtain a usable synchronization signal. Callers must treat this
// as a non-fatal diagnostic condition and keep the rest of the application
// operational.
var ErrSystemTimeDiagnosticsUnavailable = errors.New("diagnóstico de sincronización horaria no disponible")

// SystemTimeDiagnosticsProvider performs local, read-only observation of
// operating-system clock synchronization state. Implementations must not
// change the system clock, time service configuration or network settings.
type SystemTimeDiagnosticsProvider interface {
	Observe(ctx context.Context) (SystemTimeDiagnostics, error)
}
