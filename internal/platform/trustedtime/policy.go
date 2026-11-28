package trustedtime

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

const MaxPolicySources = 64

var (
	ErrInvalidAcceptancePolicy = errors.New("política de tiempo confiable inválida")
	ErrUntrustedTimeSource     = errors.New("fuente de tiempo no autorizada")
	ErrDuplicateTimeSource     = errors.New("fuente u operador temporal duplicado")
	ErrInsufficientTimeEvidence = errors.New("evidencia temporal insuficiente")
	ErrConflictingTimeEvidence = errors.New("evidencias temporales contradictorias")
	ErrTimeUncertaintyExceeded = errors.New("incertidumbre temporal superior al límite")
	ErrTrustedTimeRollback     = errors.New("la evidencia temporal retrocede respecto del último tiempo aceptado")
)

// AcceptancePolicy is an in-memory allowlist and interval-consensus policy.
// TrustedSources must come from local configuration or a separately
// authenticated provisioning channel. Source identities returned by providers
// are never allowed to define their own trust.
//
// The policy accepts only authenticated or attested sources. System clock
// observations are useful for diagnostics but cannot establish this trust
// boundary. Freshness and protocol authentication (including nonce/replay
// checks) remain obligations of each provider before Observe returns.
type AcceptancePolicy struct {
	TrustedSources         []Source
	MinDistinctOperators   int
	MaxSourceUncertainty   time.Duration
	MaxAggregateUncertainty time.Duration
}

// AcceptedTime is the conservative intersection supported by one unique
// maximum-overlap group of independently configured operators. Sources are
// sorted by stable source ID. RollbackFloorApplied indicates that the
// interval's lower bound was raised to the previous accepted lower bound.
type AcceptedTime struct {
	Interval             Interval
	Sources              []Source
	RollbackFloorApplied bool
}

// Validate checks policy configuration before it is used to evaluate evidence.
func (policy AcceptancePolicy) Validate() error {
	if len(policy.TrustedSources) == 0 || len(policy.TrustedSources) > MaxPolicySources {
		return fmt.Errorf("%w: la lista debe contener entre 1 y %d fuentes", ErrInvalidAcceptancePolicy, MaxPolicySources)
	}
	if policy.MinDistinctOperators < 2 {
		return fmt.Errorf("%w: se requieren al menos dos operadores independientes", ErrInvalidAcceptancePolicy)
	}
	if policy.MaxSourceUncertainty <= 0 || policy.MaxAggregateUncertainty <= 0 {
		return fmt.Errorf("%w: los límites de incertidumbre deben ser positivos", ErrInvalidAcceptancePolicy)
	}

	sourceIDs := make(map[string]struct{}, len(policy.TrustedSources))
	operators := make(map[string]struct{}, len(policy.TrustedSources))
	for _, source := range policy.TrustedSources {
		if err := source.Validate(); err != nil {
			return fmt.Errorf("%w: fuente configurada inválida: %v", ErrInvalidAcceptancePolicy, err)
		}
		if source.Assurance == AssuranceSystem || source.OperatorID == "" || source.TrustAnchorID == "" {
			return fmt.Errorf("%w: las fuentes deben tener garantía criptográfica, operador y ancla explícitos", ErrInvalidAcceptancePolicy)
		}
		if _, exists := sourceIDs[source.ID]; exists {
			return fmt.Errorf("%w: ID de fuente repetido %q", ErrInvalidAcceptancePolicy, source.ID)
		}
		sourceIDs[source.ID] = struct{}{}
		operators[source.OperatorID] = struct{}{}
	}
	if policy.MinDistinctOperators > len(operators) {
		return fmt.Errorf("%w: el mínimo de operadores supera los operadores configurados", ErrInvalidAcceptancePolicy)
	}
	return nil
}

// Accept validates source allowlists and combines overlapping intervals.
// It does not read the system clock, perform network I/O, verify signatures,
// or persist the resulting high-water mark.
//
// At most one observation per administrative operator is accepted in a batch.
// The strongest supported interval is the unique group with maximum common
// overlap; a tie between different groups is rejected instead of choosing a
// convenient time. The previous accepted lower bound is a monotonic floor:
// a new interval may be narrowed to that floor, but may not end before it.
func (policy AcceptancePolicy) Accept(evidence []Evidence, lastAccepted *Interval) (AcceptedTime, error) {
	if err := policy.Validate(); err != nil {
		return AcceptedTime{}, err
	}
	if len(evidence) > MaxPolicySources {
		return AcceptedTime{}, fmt.Errorf("%w: se recibieron más de %d observaciones", ErrInsufficientTimeEvidence, MaxPolicySources)
	}
	if lastAccepted != nil {
		if err := lastAccepted.Validate(); err != nil {
			return AcceptedTime{}, fmt.Errorf("%w: intervalo anterior inválido: %v", ErrInvalidAcceptancePolicy, err)
		}
	}

	allowed := make(map[string]Source, len(policy.TrustedSources))
	for _, source := range policy.TrustedSources {
		allowed[source.ID] = source
	}

	observations := make([]Evidence, 0, len(evidence))
	seenSources := make(map[string]struct{}, len(evidence))
	seenOperators := make(map[string]struct{}, len(evidence))
	for _, candidate := range evidence {
		canonical, err := candidate.Canonical()
		if err != nil {
			return AcceptedTime{}, fmt.Errorf("%w: %v", ErrInvalidEvidence, err)
		}
		expected, ok := allowed[canonical.Source.ID]
		if !ok || expected != canonical.Source {
			return AcceptedTime{}, fmt.Errorf("%w: %s", ErrUntrustedTimeSource, canonical.Source.ID)
		}
		if canonical.Source.Assurance == AssuranceSystem || canonical.Source.OperatorID == "" {
			return AcceptedTime{}, fmt.Errorf("%w: la fuente no acredita operador y garantía criptográfica", ErrUntrustedTimeSource)
		}
		if _, exists := seenSources[canonical.Source.ID]; exists {
			return AcceptedTime{}, fmt.Errorf("%w: ID %s", ErrDuplicateTimeSource, canonical.Source.ID)
		}
		if _, exists := seenOperators[canonical.Source.OperatorID]; exists {
			return AcceptedTime{}, fmt.Errorf("%w: operador %s", ErrDuplicateTimeSource, canonical.Source.OperatorID)
		}
		seenSources[canonical.Source.ID] = struct{}{}
		seenOperators[canonical.Source.OperatorID] = struct{}{}

		uncertainty, err := canonical.Interval.Uncertainty()
		if err != nil {
			return AcceptedTime{}, err
		}
		if uncertainty > policy.MaxSourceUncertainty {
			return AcceptedTime{}, fmt.Errorf("%w: fuente %s informa %s", ErrTimeUncertaintyExceeded, canonical.Source.ID, uncertainty)
		}
		observations = append(observations, canonical)
	}
	if len(seenOperators) < policy.MinDistinctOperators {
		return AcceptedTime{}, fmt.Errorf("%w: %d operadores válidos; se requieren %d", ErrInsufficientTimeEvidence, len(seenOperators), policy.MinDistinctOperators)
	}

	sort.Slice(observations, func(i, j int) bool {
		return observations[i].Source.ID < observations[j].Source.ID
	})

	points := make([]time.Time, 0, len(observations)*2)
	for _, observation := range observations {
		points = append(points, observation.Interval.Earliest, observation.Interval.Latest)
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Before(points[j]) })

	bestCount := 0
	var bestIndexes []int
	var bestInterval Interval
	ambiguous := false
	for pointIndex, point := range points {
		if pointIndex > 0 && point.Equal(points[pointIndex-1]) {
			continue
		}
		selected := make([]int, 0, len(observations))
		earliest := time.Time{}
		latest := time.Time{}
		for index, observation := range observations {
			if point.Before(observation.Interval.Earliest) || point.After(observation.Interval.Latest) {
				continue
			}
			selected = append(selected, index)
			if earliest.IsZero() || observation.Interval.Earliest.After(earliest) {
				earliest = observation.Interval.Earliest
			}
			if latest.IsZero() || observation.Interval.Latest.Before(latest) {
				latest = observation.Interval.Latest
			}
		}
		count := len(selected)
		if count > bestCount {
			bestCount = count
			bestIndexes = selected
			bestInterval = Interval{Earliest: earliest, Latest: latest}
			ambiguous = false
			continue
		}
		if count == bestCount && count > 0 && !sameObservationSet(bestIndexes, selected) {
			ambiguous = true
		}
	}
	if bestCount < policy.MinDistinctOperators {
		return AcceptedTime{}, fmt.Errorf("%w: máximo de %d operadores con intervalos compatibles; se requieren %d", ErrInsufficientTimeEvidence, bestCount, policy.MinDistinctOperators)
	}
	if ambiguous {
		return AcceptedTime{}, ErrConflictingTimeEvidence
	}

	aggregateUncertainty, err := bestInterval.Uncertainty()
	if err != nil {
		return AcceptedTime{}, err
	}
	if aggregateUncertainty > policy.MaxAggregateUncertainty {
		return AcceptedTime{}, fmt.Errorf("%w: intervalo combinado %s", ErrTimeUncertaintyExceeded, aggregateUncertainty)
	}

	result := AcceptedTime{
		Interval: bestInterval,
		Sources:  make([]Source, 0, len(bestIndexes)),
	}
	for _, index := range bestIndexes {
		result.Sources = append(result.Sources, observations[index].Source)
	}
	if lastAccepted != nil {
		floor := lastAccepted.Earliest.UTC().Round(0)
		if result.Interval.Latest.Before(floor) {
			return AcceptedTime{}, ErrTrustedTimeRollback
		}
		if result.Interval.Earliest.Before(floor) {
			result.Interval.Earliest = floor
			result.RollbackFloorApplied = true
		}
	}
	return result, nil
}

func sameObservationSet(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
