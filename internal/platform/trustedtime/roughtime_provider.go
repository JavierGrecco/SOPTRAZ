package trustedtime

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/tannerryan/roughtime/protocol"
)

const (
	MinRoughtimeWitnesses              = 3
	MaxRoughtimeWitnesses              = 8
	MaxRoughtimeEndpoints              = 4
	defaultRoughtimeExchangeTimeout    = 2 * time.Second
	defaultRoughtimeMeasurementTimeout = 45 * time.Second
	maxRoughtimeExchangeTimeout        = 10 * time.Second
	maxRoughtimeMeasurementTimeout     = 3 * time.Minute
	maxRoughtimeRadius                 = time.Minute
)

var (
	ErrInvalidRoughtimeConfig = errors.New("configuración Roughtime inválida")
	ErrRoughtimeExchange      = errors.New("falló el intercambio Roughtime")
	ErrRoughtimeVerification  = errors.New("respuesta Roughtime no válida")
	ErrRoughtimeRadius        = errors.New("incertidumbre Roughtime superior al límite")
	ErrRoughtimeMeasurement   = errors.New("medición Roughtime incompleta")
)

type RoughtimeEndpoint struct {
	Transport string
	Address   string
}

type RoughtimeWitness struct {
	Source    Source
	PublicKey []byte
	Endpoints []RoughtimeEndpoint
}

type RoughtimeProviderConfig struct {
	Witnesses          []RoughtimeWitness
	ExchangeTimeout    time.Duration
	MeasurementTimeout time.Duration
	MaxRadius          time.Duration
}

type RoughtimeProvider struct {
	witnesses          []RoughtimeWitness
	exchangeTimeout    time.Duration
	measurementTimeout time.Duration
	maxRadius          time.Duration
}

type roughtimeObservation struct {
	receivedAt time.Time
	rtt        time.Duration
	digest     string
}

// NewRoughtimeProvider accepts only locally pinned Ed25519 roots and strict
// RFC 10049 v1. It does not infer operator identity from DNS or IP addresses.
func NewRoughtimeProvider(config RoughtimeProviderConfig) (*RoughtimeProvider, error) {
	if len(config.Witnesses) < MinRoughtimeWitnesses || len(config.Witnesses) > MaxRoughtimeWitnesses {
		return nil, fmt.Errorf("%w: se requieren entre %d y %d testigos", ErrInvalidRoughtimeConfig, MinRoughtimeWitnesses, MaxRoughtimeWitnesses)
	}
	if config.ExchangeTimeout <= 0 {
		config.ExchangeTimeout = defaultRoughtimeExchangeTimeout
	}
	if config.ExchangeTimeout > maxRoughtimeExchangeTimeout {
		return nil, fmt.Errorf("%w: ExchangeTimeout supera %s", ErrInvalidRoughtimeConfig, maxRoughtimeExchangeTimeout)
	}
	if config.MeasurementTimeout <= 0 {
		config.MeasurementTimeout = defaultRoughtimeMeasurementTimeout
	}
	if config.MeasurementTimeout <= config.ExchangeTimeout || config.MeasurementTimeout > maxRoughtimeMeasurementTimeout {
		return nil, fmt.Errorf("%w: MeasurementTimeout inválido", ErrInvalidRoughtimeConfig)
	}
	if config.MaxRadius <= 0 || config.MaxRadius > maxRoughtimeRadius {
		return nil, fmt.Errorf("%w: MaxRadius inválido", ErrInvalidRoughtimeConfig)
	}

	witnesses := make([]RoughtimeWitness, len(config.Witnesses))
	sourceIDs, operators, keys, anchors := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for i, input := range config.Witnesses {
		if err := input.Source.Validate(); err != nil {
			return nil, fmt.Errorf("%w: testigo %d: %v", ErrInvalidRoughtimeConfig, i, err)
		}
		if input.Source.Kind != SourceRoughtime || input.Source.Assurance != AssuranceAttested ||
			input.Source.OperatorID == "" || input.Source.TrustAnchorID == "" {
			return nil, fmt.Errorf("%w: identidad Roughtime incompleta para %q", ErrInvalidRoughtimeConfig, input.Source.ID)
		}
		if len(input.PublicKey) != 32 {
			return nil, fmt.Errorf("%w: raíz Ed25519 inválida", ErrInvalidRoughtimeConfig)
		}
		fingerprint := sha256.Sum256(input.PublicKey)
		if input.Source.KeyID != hex.EncodeToString(fingerprint[:]) {
			return nil, fmt.Errorf("%w: KeyID no coincide con la raíz fijada para %q", ErrInvalidRoughtimeConfig, input.Source.ID)
		}
		if sourceIDs[input.Source.ID] || operators[input.Source.OperatorID] || keys[string(input.PublicKey)] || anchors[input.Source.TrustAnchorID] {
			return nil, fmt.Errorf("%w: Source.ID, OperatorID, raíz o TrustAnchorID duplicado", ErrInvalidRoughtimeConfig)
		}
		if len(input.Endpoints) == 0 || len(input.Endpoints) > MaxRoughtimeEndpoints {
			return nil, fmt.Errorf("%w: endpoints inválidos para %q", ErrInvalidRoughtimeConfig, input.Source.ID)
		}
		endpoints := make([]RoughtimeEndpoint, len(input.Endpoints))
		seen := map[string]bool{}
		for j, endpoint := range input.Endpoints {
			normalized, err := normalizeRoughtimeEndpoint(endpoint)
			if err != nil {
				return nil, fmt.Errorf("%w: endpoint %d: %v", ErrInvalidRoughtimeConfig, j, err)
			}
			key := normalized.Transport + "://" + normalized.Address
			if seen[key] {
				return nil, fmt.Errorf("%w: endpoint duplicado", ErrInvalidRoughtimeConfig)
			}
			seen[key] = true
			endpoints[j] = normalized
		}
		witnesses[i] = RoughtimeWitness{Source: input.Source, PublicKey: append([]byte(nil), input.PublicKey...), Endpoints: endpoints}
		sourceIDs[input.Source.ID], operators[input.Source.OperatorID], keys[string(input.PublicKey)], anchors[input.Source.TrustAnchorID] = true, true, true, true
	}
	return &RoughtimeProvider{
		witnesses: witnesses, exchangeTimeout: config.ExchangeTimeout,
		measurementTimeout: config.MeasurementTimeout, maxRadius: config.MaxRadius,
	}, nil
}

func normalizeRoughtimeEndpoint(endpoint RoughtimeEndpoint) (RoughtimeEndpoint, error) {
	transport := strings.ToLower(strings.TrimSpace(endpoint.Transport))
	if transport != "udp" && transport != "tcp" {
		return RoughtimeEndpoint{}, errors.New("Transport debe ser udp o tcp")
	}
	address := strings.TrimSpace(endpoint.Address)
	if address == "" || address != endpoint.Address || len(address) > 512 {
		return RoughtimeEndpoint{}, errors.New("Address inválido")
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil || strings.TrimSpace(host) == "" {
		return RoughtimeEndpoint{}, errors.New("Address debe ser host:port")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return RoughtimeEndpoint{}, errors.New("puerto inválido")
	}
	return RoughtimeEndpoint{Transport: transport, Address: address}, nil
}

// ObserveBatch runs two sequential passes in one causal chain and the same
// random witness order. It returns no partial batch on any protocol failure.
// Only monotonic elapsed time after receipt and the measured RTT project the
// signed server interval to measurement completion; local wall time is not a
// trusted reference.
func (p *RoughtimeProvider) ObserveBatch(ctx context.Context) ([]Evidence, error) {
	if p == nil {
		return nil, ErrInvalidRoughtimeConfig
	}
	if ctx == nil {
		return nil, ErrRoughtimeMeasurement
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	measureCtx, cancel := context.WithTimeout(ctx, p.measurementTimeout)
	defer cancel()

	order, err := randomizedWitnessOrder(len(p.witnesses))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRoughtimeMeasurement, err)
	}
	chain := protocol.Chain{Mode: protocol.Strict}
	samples := make([]roughtimeObservation, 0, 2*len(order))
	seenNonces := map[string]bool{}

	for pass := 0; pass < 2; pass++ {
		for _, witnessIndex := range order {
			if err := measureCtx.Err(); err != nil {
				return nil, err
			}
			witness := p.witnesses[witnessIndex]
			var lastErr error
			var completed protocol.ChainLink
			var sample roughtimeObservation
			ok := false
			for _, endpoint := range witness.Endpoints {
				link, err := chain.NextRequest([]protocol.Version{protocol.Version1}, witness.PublicKey, rand.Reader)
				if err != nil {
					return nil, fmt.Errorf("%w: %w", ErrRoughtimeMeasurement, err)
				}
				if seenNonces[string(link.Nonce)] {
					return nil, ErrRoughtimeVerification
				}
				seenNonces[string(link.Nonce)] = true
				reply, rtt, received, err := roughtimeRoundTrip(measureCtx, endpoint, link.Request, p.exchangeTimeout)
				if err != nil {
					if measureCtx.Err() != nil {
						return nil, measureCtx.Err()
					}
					lastErr = err
					continue
				}
				_, radius, err := protocol.VerifyReply([]protocol.Version{protocol.Version1}, reply, witness.PublicKey, link.Nonce, link.Request)
				if err != nil {
					return nil, fmt.Errorf("%w: testigo %q: %w", ErrRoughtimeVerification, witness.Source.ID, err)
				}
				if radius <= 0 || radius > p.maxRadius {
					return nil, fmt.Errorf("%w: %q informa %s; máximo %s", ErrRoughtimeRadius, witness.Source.ID, radius, p.maxRadius)
				}
				if rtt < 0 || received.IsZero() {
					return nil, ErrRoughtimeMeasurement
				}
				link.Response = append([]byte(nil), reply...)
				completed = link
				sample = roughtimeObservation{receivedAt: received, rtt: rtt, digest: digestSHA256(reply)}
				ok = true
				break
			}
			if !ok {
				return nil, fmt.Errorf("%w: testigo %q: %w", ErrRoughtimeExchange, witness.Source.ID, lastErr)
			}
			chain.Append(completed)
			samples = append(samples, sample)
		}
	}

	bounds, err := chain.VerifyBounds()
	if err != nil {
		return nil, fmt.Errorf("%w: cadena causal: %w", ErrRoughtimeVerification, err)
	}
	if len(bounds) != 2*len(order) || len(samples) != len(bounds) {
		return nil, ErrRoughtimeMeasurement
	}
	if err := measureCtx.Err(); err != nil {
		return nil, err
	}

	finished := time.Now()
	evidence := make([]Evidence, 0, len(order))
	for i, witnessIndex := range order {
		index := len(order) + i
		elapsed := finished.Sub(samples[index].receivedAt)
		if elapsed < 0 {
			return nil, ErrRoughtimeMeasurement
		}
		bound, sample := bounds[index], samples[index]
		item, err := (Evidence{
			Interval: Interval{
				Earliest: bound.Midpoint.Add(-bound.Radius).Add(elapsed),
				Latest:   bound.Midpoint.Add(bound.Radius).Add(sample.rtt).Add(elapsed),
			},
			Source:         p.witnesses[witnessIndex].Source,
			EvidenceSHA256: sample.digest,
		}).Canonical()
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrRoughtimeMeasurement, err)
		}
		evidence = append(evidence, item)
	}
	return evidence, nil
}

func roughtimeRoundTrip(ctx context.Context, endpoint RoughtimeEndpoint, request []byte, timeout time.Duration) ([]byte, time.Duration, time.Time, error) {
	exchangeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if endpoint.Transport == "udp" {
		return protocol.RoundTripUDP(exchangeCtx, endpoint.Address, request, timeout)
	}
	return protocol.RoundTripTCP(exchangeCtx, endpoint.Address, request, timeout)
}

func randomizedWitnessOrder(count int) ([]int, error) {
	if count < MinRoughtimeWitnesses {
		return nil, ErrInsufficientTimeEvidence
	}
	order := make([]int, count)
	for i := range order {
		order[i] = i
	}
	for i := count - 1; i > 0; i-- {
		selected, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return nil, err
		}
		j := int(selected.Int64())
		order[i], order[j] = order[j], order[i]
	}
	return order, nil
}

func digestSHA256(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
