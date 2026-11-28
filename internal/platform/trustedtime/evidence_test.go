package trustedtime

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func validEvidence() Evidence {
	start := time.Date(2026, time.November, 28, 12, 0, 0, 0, time.UTC)
	return Evidence{
		Interval: Interval{
			Earliest: start,
			Latest:   start.Add(250 * time.Millisecond),
		},
		Source: Source{
			ID:            "lab-roughtime-a",
			OperatorID:    "independent-operator-a",
			Kind:          SourceRoughtime,
			Assurance:     AssuranceAttested,
			TrustAnchorID: "lab-roughtime-root-a",
		},
	}
}

func TestEvidenceValidate(t *testing.T) {
	if err := validEvidence().Validate(); err != nil {
		t.Fatalf("valid evidence rejected: %v", err)
	}
}

func TestEvidenceValidateRejectsMalformedValues(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Evidence)
	}{
		{
			name: "missing earliest",
			edit: func(e *Evidence) { e.Interval.Earliest = time.Time{} },
		},
		{
			name: "missing latest",
			edit: func(e *Evidence) { e.Interval.Latest = time.Time{} },
		},
		{
			name: "reversed interval",
			edit: func(e *Evidence) {
				e.Interval.Earliest = e.Interval.Latest.Add(time.Second)
			},
		},
		{
			name: "missing source id",
			edit: func(e *Evidence) { e.Source.ID = "" },
		},
		{
			name: "non canonical source id",
			edit: func(e *Evidence) { e.Source.ID = " source " },
		},
		{
			name: "control character in source id",
			edit: func(e *Evidence) { e.Source.ID = "source\ninjected" },
		},
		{
			name: "oversized operator id",
			edit: func(e *Evidence) { e.Source.OperatorID = strings.Repeat("a", 129) },
		},
		{
			name: "unknown source kind",
			edit: func(e *Evidence) { e.Source.Kind = "unknown" },
		},
		{
			name: "unknown assurance",
			edit: func(e *Evidence) { e.Source.Assurance = "best-effort" },
		},
		{
			name: "authenticated evidence without trust anchor",
			edit: func(e *Evidence) {
				e.Source.Assurance = AssuranceAuthenticated
				e.Source.TrustAnchorID = ""
			},
		},
		{
			name: "attested evidence without trust anchor",
			edit: func(e *Evidence) { e.Source.TrustAnchorID = "" },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evidence := validEvidence()
			tt.edit(&evidence)
			if err := evidence.Validate(); !errors.Is(err, ErrInvalidEvidence) {
				t.Fatalf("Validate() error = %v, want ErrInvalidEvidence", err)
			}
		})
	}
}

func TestEvidenceCanonicalNormalizesBoundsWithoutMutatingInput(t *testing.T) {
	zone := time.FixedZone("UTC-03", -3*60*60)
	start := time.Date(2026, time.November, 28, 12, 0, 0, 0, zone)
	evidence := Evidence{
		Interval: Interval{
			Earliest: start,
			Latest:   start.Add(1500 * time.Millisecond),
		},
		Source: Source{
			ID:        "system-clock",
			Kind:      SourceSystem,
			Assurance: AssuranceSystem,
		},
	}

	canonical, err := evidence.Canonical()
	if err != nil {
		t.Fatalf("Canonical() error = %v", err)
	}
	if canonical.Interval.Earliest.Location() != time.UTC ||
		canonical.Interval.Latest.Location() != time.UTC {
		t.Fatal("canonical bounds must use UTC")
	}
	if !canonical.Interval.Earliest.Equal(start) || !canonical.Interval.Latest.Equal(start.Add(1500*time.Millisecond)) {
		t.Fatal("canonicalization changed the represented instants")
	}
	if evidence.Interval.Earliest.Location() != zone {
		t.Fatal("Canonical() mutated its receiver")
	}
}

func TestEvidenceCanonicalStripsMonotonicClockReading(t *testing.T) {
	start := time.Now()
	evidence := Evidence{
		Interval: Interval{
			Earliest: start,
			Latest:   start.Add(time.Second),
		},
		Source: Source{
			ID:        "system-clock",
			Kind:      SourceSystem,
			Assurance: AssuranceSystem,
		},
	}

	canonical, err := evidence.Canonical()
	if err != nil {
		t.Fatalf("Canonical() error = %v", err)
	}
	if strings.Contains(canonical.Interval.Earliest.String(), "m=") ||
		strings.Contains(canonical.Interval.Latest.String(), "m=") {
		t.Fatal("canonical bounds must not retain process-local monotonic readings")
	}
}

func TestIntervalUncertainty(t *testing.T) {
	start := time.Now()
	interval := Interval{Earliest: start, Latest: start.Add(750 * time.Millisecond)}
	got, err := interval.Uncertainty()
	if err != nil {
		t.Fatalf("Uncertainty() error = %v", err)
	}
	if got != 750*time.Millisecond {
		t.Fatalf("Uncertainty() = %s, want 750ms", got)
	}
}

func TestIntervalUncertaintyRejectsInvalidInterval(t *testing.T) {
	if _, err := (Interval{}).Uncertainty(); !errors.Is(err, ErrInvalidEvidence) {
		t.Fatalf("Uncertainty() error = %v, want ErrInvalidEvidence", err)
	}
}
