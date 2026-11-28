// Package trustedtime defines the platform-independent evidence contract used by
// components that need a defensible UTC reference.
package trustedtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ErrInvalidEvidence identifies structurally invalid time evidence.
// It does not mean that a source is untrusted or that its claim is false.
var ErrInvalidEvidence = errors.New("evidencia temporal inválida")

// SourceKind classifies the mechanism that produced a time assertion.
type SourceKind string

const (
	SourceSystem    SourceKind = "system"
	SourceNTS       SourceKind = "nts"
	SourceRoughtime SourceKind = "roughtime"
	SourceCustom    SourceKind = "custom"
)

// Assurance describes how the provider obtained or authenticated its
// assertion. It is descriptive metadata, not a trust decision.
type Assurance string

const (
	// AssuranceSystem means the provider relies on the operating system's
	// time service or synchronization state. It is not cryptographic evidence.
	AssuranceSystem Assurance = "system"

	// AssuranceAuthenticated means the time exchange was authenticated by its
	// protocol, for example an NTS association with valid key establishment.
	AssuranceAuthenticated Assurance = "authenticated"

	// AssuranceAttested means a signed time assertion was verified against a
	// trust anchor provisioned independently of the response being checked.
	AssuranceAttested Assurance = "attested"
)

// Source identifies the configured origin and provenance of an assertion.
// ID must be stable within the SOPTRAZ configuration; it must not be copied
// from an untrusted server response. OperatorID is the administrative
// operator, not merely a DNS name or IP address. An empty OperatorID means
// operator independence is unknown. TrustAnchorID names a locally configured
// anchor; it is required for authenticated and attested assertions and must
// never be learned from the time response.
type Source struct {
	ID            string
	OperatorID    string
	Kind          SourceKind
	Assurance     Assurance
	TrustAnchorID string
}

// Interval bounds two instants on the UTC timeline as asserted by a provider.
// Earliest and Latest describe uncertainty; Canonical returns both in UTC.
type Interval struct {
	Earliest time.Time
	Latest   time.Time
}

// Evidence combines a provider's time interval with its provenance.
//
// A structurally valid Evidence value is not automatically trusted. The
// consumer must independently enforce source allowlists, trust-anchor
// configuration, freshness, uncertainty limits, source diversity and
// monotonic acceptance policy before using it for security decisions.
type Evidence struct {
	Interval Interval
	Source   Source
}

// TrustedTimeProvider reports time evidence. Implementations must honor the
// context, bound any I/O, and complete their protocol-specific authentication
// before returning authenticated or attested evidence. The consumer remains
// responsible for deciding whether the source is acceptable.
//
// The interface deliberately exposes no operation to change the operating
// system clock and makes no promise that an evidence value can be injected
// directly into a third-party updater.
type TrustedTimeProvider interface {
	Observe(ctx context.Context) (Evidence, error)
}

// Validate checks structural invariants only. It does not authenticate the
// source, validate a trust anchor, enforce freshness or accept the evidence.
func (e Evidence) Validate() error {
	if err := e.Interval.Validate(); err != nil {
		return err
	}
	if err := e.Source.Validate(); err != nil {
		return err
	}
	return nil
}

// Validate checks that the interval is present and ordered.
func (i Interval) Validate() error {
	if i.Earliest.IsZero() || i.Latest.IsZero() {
		return fmt.Errorf("%w: faltan límites del intervalo", ErrInvalidEvidence)
	}
	if i.Latest.Before(i.Earliest) {
		return fmt.Errorf("%w: el límite superior precede al inferior", ErrInvalidEvidence)
	}
	return nil
}

// Uncertainty returns the width of the asserted interval.
func (i Interval) Uncertainty() (time.Duration, error) {
	if err := i.Validate(); err != nil {
		return 0, err
	}
	return i.Latest.Sub(i.Earliest), nil
}

// Validate checks that source provenance is complete enough to identify and
// classify the assertion. Optional identifiers may be empty, but when present
// they must be canonical, valid UTF-8 and free of control characters.
func (s Source) Validate() error {
	if err := validateIdentifier("ID", s.ID, true); err != nil {
		return err
	}
	if err := validateIdentifier("OperatorID", s.OperatorID, false); err != nil {
		return err
	}
	if err := validateIdentifier("TrustAnchorID", s.TrustAnchorID, false); err != nil {
		return err
	}

	switch s.Kind {
	case SourceSystem, SourceNTS, SourceRoughtime, SourceCustom:
	default:
		return fmt.Errorf("%w: tipo de fuente desconocido", ErrInvalidEvidence)
	}

	switch s.Assurance {
	case AssuranceSystem:
	case AssuranceAuthenticated, AssuranceAttested:
		if s.TrustAnchorID == "" {
			return fmt.Errorf("%w: falta el ancla de una evidencia autenticada", ErrInvalidEvidence)
		}
	default:
		return fmt.Errorf("%w: clase de garantía desconocida", ErrInvalidEvidence)
	}
	return nil
}

// Canonical returns a validated copy whose bounds are UTC and have no
// process-local monotonic clock readings. It does not change the receiver.
func (e Evidence) Canonical() (Evidence, error) {
	if err := e.Validate(); err != nil {
		return Evidence{}, err
	}
	e.Interval.Earliest = e.Interval.Earliest.UTC().Round(0)
	e.Interval.Latest = e.Interval.Latest.UTC().Round(0)
	return e, nil
}

func validateIdentifier(field, value string, required bool) error {
	if value == "" {
		if required {
			return fmt.Errorf("%w: falta %s", ErrInvalidEvidence, field)
		}
		return nil
	}
	if value != strings.TrimSpace(value) || !utf8.ValidString(value) || len(value) > 128 {
		return fmt.Errorf("%w: %s no es un identificador válido", ErrInvalidEvidence, field)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("%w: %s contiene caracteres de control", ErrInvalidEvidence, field)
		}
	}
	return nil
}
