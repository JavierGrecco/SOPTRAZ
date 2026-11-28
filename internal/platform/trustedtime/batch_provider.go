package trustedtime

import "context"

// TrustedTimeBatchProvider returns one authenticated evidence sample per
// configured source. Roughtime needs multiple independent witnesses and
// causal multi-pass exchanges; the caller still applies AcceptancePolicy.
type TrustedTimeBatchProvider interface {
	ObserveBatch(ctx context.Context) ([]Evidence, error)
}
