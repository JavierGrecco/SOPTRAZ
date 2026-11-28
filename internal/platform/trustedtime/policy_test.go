package trustedtime

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func policyTestStart() time.Time {
	return time.Date(2026, time.November, 28, 12, 0, 0, 0, time.UTC)
}

func policyTestSource(id, operator string) Source {
	return Source{
		ID:            id,
		OperatorID:    operator,
		Kind:          SourceRoughtime,
		Assurance:     AssuranceAttested,
		TrustAnchorID: "anchor-" + id,
		KeyID:         "key-" + id,
	}
}

func policyTestEvidence(source Source, start, end time.Time) Evidence {
	return Evidence{
		Interval: Interval{Earliest: start, Latest: end},
		Source:   source,
		EvidenceSHA256: strings.Repeat("a", 64),
	}
}

func policyTestConfig(sources ...Source) AcceptancePolicy {
	return AcceptancePolicy{
		TrustedSources:          sources,
		MinDistinctOperators:    3,
		MaxSourceUncertainty:    5 * time.Second,
		MaxAggregateUncertainty: 2 * time.Second,
	}
}

func TestAcceptancePolicyCombinesOnlyAllowlistedIndependentOperators(t *testing.T) {
	start := policyTestStart()
	a := policyTestSource("source-a", "operator-a")
	b := policyTestSource("source-b", "operator-b")
	c := policyTestSource("source-c", "operator-c")
	policy := policyTestConfig(a, b, c)

	got, err := policy.Accept([]Evidence{
		policyTestEvidence(a, start, start.Add(1500*time.Millisecond)),
		policyTestEvidence(b, start.Add(250*time.Millisecond), start.Add(1750*time.Millisecond)),
		policyTestEvidence(c, start.Add(500*time.Millisecond), start.Add(2*time.Second)),
	}, nil)
	if err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	want := Interval{Earliest: start.Add(500 * time.Millisecond), Latest: start.Add(1500 * time.Millisecond)}
	if !got.Interval.Earliest.Equal(want.Earliest) || !got.Interval.Latest.Equal(want.Latest) {
		t.Fatalf("accepted interval = %#v, want %#v", got.Interval, want)
	}
	if len(got.Sources) != 3 || got.RollbackFloorApplied {
		t.Fatalf("unexpected accepted provenance: %#v", got)
	}
	if got.Sources[0].ID != "source-a" || got.Sources[1].ID != "source-b" || got.Sources[2].ID != "source-c" {
		t.Fatalf("sources are not returned in stable ID order: %#v", got.Sources)
	}
}

func TestAcceptancePolicyRejectsSourceNotInIndependentAllowlist(t *testing.T) {
	start := policyTestStart()
	a := policyTestSource("source-a", "operator-a")
	b := policyTestSource("source-b", "operator-b")
	c := policyTestSource("source-c", "operator-c")
	unknown := policyTestSource("unprovisioned", "operator-d")

	_, err := policyTestConfig(a, b, c).Accept([]Evidence{
		policyTestEvidence(a, start, start.Add(time.Second)),
		policyTestEvidence(b, start, start.Add(time.Second)),
		policyTestEvidence(unknown, start, start.Add(time.Second)),
	}, nil)
	if !errors.Is(err, ErrUntrustedTimeSource) {
		t.Fatalf("Accept() error = %v, want ErrUntrustedTimeSource", err)
	}
}

func TestAcceptancePolicyRejectsEvidenceThatChangesPinnedSourceIdentity(t *testing.T) {
	start := policyTestStart()
	a := policyTestSource("source-a", "operator-a")
	b := policyTestSource("source-b", "operator-b")
	c := policyTestSource("source-c", "operator-c")
	forged := a
	forged.OperatorID = "operator-attacker"

	_, err := policyTestConfig(a, b, c).Accept([]Evidence{
		policyTestEvidence(forged, start, start.Add(time.Second)),
		policyTestEvidence(b, start, start.Add(time.Second)),
		policyTestEvidence(c, start, start.Add(time.Second)),
	}, nil)
	if !errors.Is(err, ErrUntrustedTimeSource) {
		t.Fatalf("Accept() error = %v, want ErrUntrustedTimeSource", err)
	}
}

func TestAcceptancePolicyRejectsDuplicateOperatorEvenWithDifferentSourceIDs(t *testing.T) {
	start := policyTestStart()
	a := policyTestSource("source-a", "operator-shared")
	b := policyTestSource("source-b", "operator-shared")
	c := policyTestSource("source-c", "operator-c")
	policy := policyTestConfig(a, b, c)
	policy.MinDistinctOperators = 2

	_, err := policy.Accept([]Evidence{
		policyTestEvidence(a, start, start.Add(time.Second)),
		policyTestEvidence(b, start, start.Add(time.Second)),
		policyTestEvidence(c, start, start.Add(time.Second)),
	}, nil)
	if !errors.Is(err, ErrDuplicateTimeSource) {
		t.Fatalf("Accept() error = %v, want ErrDuplicateTimeSource", err)
	}
}

func TestAcceptancePolicyRejectsDuplicateSourceID(t *testing.T) {
	start := policyTestStart()
	a := policyTestSource("source-a", "operator-a")
	b := policyTestSource("source-b", "operator-b")
	c := policyTestSource("source-c", "operator-c")
	evidence := []Evidence{
		policyTestEvidence(a, start, start.Add(time.Second)),
		policyTestEvidence(b, start, start.Add(time.Second)),
		policyTestEvidence(c, start, start.Add(time.Second)),
	}
	evidence[2].Source = b

	_, err := policyTestConfig(a, b, c).Accept(evidence, nil)
	if !errors.Is(err, ErrDuplicateTimeSource) && !errors.Is(err, ErrUntrustedTimeSource) {
		t.Fatalf("Accept() error = %v, want duplicate/untrusted source error", err)
	}
}

func TestAcceptancePolicyRejectsInsufficientAndDisjointEvidence(t *testing.T) {
	start := policyTestStart()
	a := policyTestSource("source-a", "operator-a")
	b := policyTestSource("source-b", "operator-b")
	c := policyTestSource("source-c", "operator-c")
	policy := policyTestConfig(a, b, c)

	_, err := policy.Accept([]Evidence{
		policyTestEvidence(a, start, start.Add(time.Second)),
		policyTestEvidence(b, start, start.Add(time.Second)),
	}, nil)
	if !errors.Is(err, ErrInsufficientTimeEvidence) {
		t.Fatalf("insufficient evidence error = %v", err)
	}

	_, err = policy.Accept([]Evidence{
		policyTestEvidence(a, start, start.Add(time.Second)),
		policyTestEvidence(b, start.Add(2*time.Second), start.Add(3*time.Second)),
		policyTestEvidence(c, start.Add(4*time.Second), start.Add(5*time.Second)),
	}, nil)
	if !errors.Is(err, ErrInsufficientTimeEvidence) {
		t.Fatalf("disjoint evidence error = %v, want ErrInsufficientTimeEvidence", err)
	}
}

func TestAcceptancePolicyRejectsAmbiguousMaximumOverlap(t *testing.T) {
	start := policyTestStart()
	a := policyTestSource("source-a", "operator-a")
	b := policyTestSource("source-b", "operator-b")
	c := policyTestSource("source-c", "operator-c")
	d := policyTestSource("source-d", "operator-d")
	policy := policyTestConfig(a, b, c, d)
	policy.MinDistinctOperators = 2

	_, err := policy.Accept([]Evidence{
		policyTestEvidence(a, start, start.Add(time.Second)),
		policyTestEvidence(b, start.Add(500*time.Millisecond), start.Add(1500*time.Millisecond)),
		policyTestEvidence(c, start.Add(10*time.Second), start.Add(11*time.Second)),
		policyTestEvidence(d, start.Add(10500*time.Millisecond), start.Add(11500*time.Millisecond)),
	}, nil)
	if !errors.Is(err, ErrConflictingTimeEvidence) {
		t.Fatalf("Accept() error = %v, want ErrConflictingTimeEvidence", err)
	}
}

func TestAcceptancePolicyRejectsExcessivePerSourceUncertainty(t *testing.T) {
	start := policyTestStart()
	a := policyTestSource("source-a", "operator-a")
	b := policyTestSource("source-b", "operator-b")
	c := policyTestSource("source-c", "operator-c")
	_, err := policyTestConfig(a, b, c).Accept([]Evidence{
		policyTestEvidence(a, start, start.Add(6*time.Second)),
		policyTestEvidence(b, start, start.Add(time.Second)),
		policyTestEvidence(c, start, start.Add(time.Second)),
	}, nil)
	if !errors.Is(err, ErrTimeUncertaintyExceeded) {
		t.Fatalf("Accept() error = %v, want ErrTimeUncertaintyExceeded", err)
	}
}

func TestAcceptancePolicyRejectsExcessiveAggregateUncertainty(t *testing.T) {
	start := policyTestStart()
	a := policyTestSource("source-a", "operator-a")
	b := policyTestSource("source-b", "operator-b")
	c := policyTestSource("source-c", "operator-c")
	policy := policyTestConfig(a, b, c)
	policy.MaxAggregateUncertainty = 500 * time.Millisecond

	_, err := policy.Accept([]Evidence{
		policyTestEvidence(a, start, start.Add(2*time.Second)),
		policyTestEvidence(b, start, start.Add(2*time.Second)),
		policyTestEvidence(c, start, start.Add(2*time.Second)),
	}, nil)
	if !errors.Is(err, ErrTimeUncertaintyExceeded) {
		t.Fatalf("Accept() error = %v, want ErrTimeUncertaintyExceeded", err)
	}
}

func TestAcceptancePolicyClampsToPreviousAcceptedLowerBound(t *testing.T) {
	start := policyTestStart()
	a := policyTestSource("source-a", "operator-a")
	b := policyTestSource("source-b", "operator-b")
	c := policyTestSource("source-c", "operator-c")
	previous := Interval{Earliest: start.Add(time.Second), Latest: start.Add(2 * time.Second)}

	got, err := policyTestConfig(a, b, c).Accept([]Evidence{
		policyTestEvidence(a, start, start.Add(1500*time.Millisecond)),
		policyTestEvidence(b, start.Add(250*time.Millisecond), start.Add(1750*time.Millisecond)),
		policyTestEvidence(c, start.Add(500*time.Millisecond), start.Add(1800*time.Millisecond)),
	}, &previous)
	if err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	if !got.Interval.Earliest.Equal(previous.Earliest) || !got.Interval.Latest.Equal(start.Add(1500*time.Millisecond)) {
		t.Fatalf("accepted interval = %#v, want lower-bound-clamped interval", got.Interval)
	}
	if !got.RollbackFloorApplied {
		t.Fatal("expected the previous accepted lower bound to be applied")
	}
}

func TestAcceptancePolicyRejectsEvidenceBelowPreviousAcceptedFloor(t *testing.T) {
	start := policyTestStart()
	a := policyTestSource("source-a", "operator-a")
	b := policyTestSource("source-b", "operator-b")
	c := policyTestSource("source-c", "operator-c")
	previous := Interval{Earliest: start.Add(2 * time.Second), Latest: start.Add(3 * time.Second)}

	_, err := policyTestConfig(a, b, c).Accept([]Evidence{
		policyTestEvidence(a, start, start.Add(time.Second)),
		policyTestEvidence(b, start, start.Add(time.Second)),
		policyTestEvidence(c, start, start.Add(time.Second)),
	}, &previous)
	if !errors.Is(err, ErrTrustedTimeRollback) {
		t.Fatalf("Accept() error = %v, want ErrTrustedTimeRollback", err)
	}
}

func TestAcceptancePolicyRejectsSystemOnlySources(t *testing.T) {
	system := Source{
		ID:        "system-clock",
		Kind:      SourceSystem,
		Assurance: AssuranceSystem,
	}
	policy := policyTestConfig(system, policyTestSource("source-b", "operator-b"), policyTestSource("source-c", "operator-c"))
	if err := policy.Validate(); !errors.Is(err, ErrInvalidAcceptancePolicy) {
		t.Fatalf("Validate() error = %v, want ErrInvalidAcceptancePolicy", err)
	}
}

func TestAcceptancePolicyRejectsInvalidConfiguration(t *testing.T) {
	valid := policyTestConfig(
		policyTestSource("source-a", "operator-a"),
		policyTestSource("source-b", "operator-b"),
		policyTestSource("source-c", "operator-c"),
	)
	tests := []struct {
		name string
		edit func(*AcceptancePolicy)
	}{
		{"one operator minimum", func(p *AcceptancePolicy) { p.MinDistinctOperators = 1 }},
		{"no source uncertainty limit", func(p *AcceptancePolicy) { p.MaxSourceUncertainty = 0 }},
		{"no aggregate uncertainty limit", func(p *AcceptancePolicy) { p.MaxAggregateUncertainty = 0 }},
		{"duplicate source id", func(p *AcceptancePolicy) { p.TrustedSources[1].ID = p.TrustedSources[0].ID }},
		{"too many operators", func(p *AcceptancePolicy) { p.MinDistinctOperators = 4 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := valid
			policy.TrustedSources = append([]Source(nil), valid.TrustedSources...)
			test.edit(&policy)
			if err := policy.Validate(); !errors.Is(err, ErrInvalidAcceptancePolicy) {
				t.Fatalf("Validate() error = %v, want ErrInvalidAcceptancePolicy", err)
			}
		})
	}
}


func TestAcceptancePolicySelectsUniqueMaximumOverlapAgainstOneOutlier(t *testing.T) {
	start := policyTestStart()
	a := policyTestSource("source-a", "operator-a")
	b := policyTestSource("source-b", "operator-b")
	c := policyTestSource("source-c", "operator-c")
	d := policyTestSource("source-d", "operator-d")
	policy := policyTestConfig(a, b, c, d)

	got, err := policy.Accept([]Evidence{
		policyTestEvidence(a, start, start.Add(1500*time.Millisecond)),
		policyTestEvidence(b, start.Add(250*time.Millisecond), start.Add(1750*time.Millisecond)),
		policyTestEvidence(c, start.Add(500*time.Millisecond), start.Add(2*time.Second)),
		policyTestEvidence(d, start.Add(time.Minute), start.Add(time.Minute+time.Second)),
	}, nil)
	if err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	if len(got.Evidence) != 3 {
		t.Fatalf("selected %d sources, want the three-source overlapping group", len(got.Evidence))
	}
	for _, item := range got.Evidence {
		if item.Source.ID == d.ID {
			t.Fatal("the disjoint outlier was included in the accepted group")
		}
	}
}

func TestAcceptancePolicySupportsAuthenticatedNTSFromIndependentOperators(t *testing.T) {
	start := policyTestStart()
	a := policyTestSource("nts-a", "operator-a")
	b := policyTestSource("nts-b", "operator-b")
	c := policyTestSource("nts-c", "operator-c")
	for _, source := range []*Source{&a, &b, &c} {
		source.Kind = SourceNTS
		source.Assurance = AssuranceAuthenticated
		source.KeyID = ""
	}
	policy := policyTestConfig(a, b, c)
	evidence := []Evidence{
		policyTestEvidence(a, start, start.Add(time.Second)),
		policyTestEvidence(b, start.Add(100*time.Millisecond), start.Add(1100*time.Millisecond)),
		policyTestEvidence(c, start.Add(200*time.Millisecond), start.Add(1200*time.Millisecond)),
	}
	for index := range evidence {
		evidence[index].EvidenceSHA256 = ""
	}

	got, err := policy.Accept(evidence, nil)
	if err != nil {
		t.Fatalf("Accept() rejected independent authenticated NTS sources: %v", err)
	}
	if len(got.Evidence) != 3 || !got.Interval.Earliest.Equal(start.Add(200*time.Millisecond)) ||
		!got.Interval.Latest.Equal(start.Add(time.Second)) {
		t.Fatalf("unexpected accepted NTS result: %#v", got)
	}
}

func TestAcceptancePolicyRejectsInvalidPreviousInterval(t *testing.T) {
	start := policyTestStart()
	a := policyTestSource("source-a", "operator-a")
	b := policyTestSource("source-b", "operator-b")
	c := policyTestSource("source-c", "operator-c")
	invalidPrevious := Interval{Earliest: start.Add(time.Second), Latest: start}

	_, err := policyTestConfig(a, b, c).Accept([]Evidence{
		policyTestEvidence(a, start, start.Add(time.Second)),
		policyTestEvidence(b, start, start.Add(time.Second)),
		policyTestEvidence(c, start, start.Add(time.Second)),
	}, &invalidPrevious)
	if !errors.Is(err, ErrInvalidAcceptancePolicy) {
		t.Fatalf("Accept() error = %v, want ErrInvalidAcceptancePolicy", err)
	}
}

func TestAcceptancePolicyRejectsOversizedEvidenceBatch(t *testing.T) {
	sources := make([]Source, 65)
	for index := range sources {
		sources[index] = policyTestSource(
			fmt.Sprintf("source-%02d", index),
			fmt.Sprintf("operator-%02d", index),
		)
	}
	policy := policyTestConfig(sources[:3]...)
	evidence := make([]Evidence, 65)
	start := policyTestStart()
	for index := range evidence {
		evidence[index] = policyTestEvidence(sources[index], start, start.Add(time.Second))
	}

	_, err := policy.Accept(evidence, nil)
	if !errors.Is(err, ErrInsufficientTimeEvidence) {
		t.Fatalf("Accept() error = %v, want ErrInsufficientTimeEvidence", err)
	}
}

func TestAcceptancePolicyDoesNotReadWallClockOrMutateEvidence(t *testing.T) {
	start := policyTestStart()
	a := policyTestSource("source-a", "operator-a")
	b := policyTestSource("source-b", "operator-b")
	c := policyTestSource("source-c", "operator-c")
	input := []Evidence{
		policyTestEvidence(a, start, start.Add(time.Second)),
		policyTestEvidence(b, start, start.Add(time.Second)),
		policyTestEvidence(c, start, start.Add(time.Second)),
	}
	original := append([]Evidence(nil), input...)
	accepted, err := policyTestConfig(a, b, c).Accept(input, nil)
	if err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	if len(accepted.Sources) == 0 {
		t.Fatal("expected accepted source provenance")
	}
	for index := range input {
		if input[index] != original[index] {
			t.Fatalf("Accept() mutated input evidence at index %d", index)
		}
	}
}
