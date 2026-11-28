package supervisor

import (
	"errors"
	"strings"
	"testing"
)

func validProcessProfile() ProcessProfile {
	return ProcessProfile{
		SchemaVersion:     ProcessProfileSchemaVersion,
		ProfileID:         "soptraz.process.osquery.os-version",
		ProfileVersion:    "1.0.0",
		ActionID:          "soptraz.inventory.os-version",
		ActionVersion:     "1.0.0",
		ArtifactID:        "soptraz.osquery",
		ArtifactVersion:   "5.24.1",
		Purpose:           "Consulta fija local de solo lectura.",
		Risk:              "read_only",
		RequiresAdmin:     false,
		RequiresApproval:  false,
		Idempotent:        true,
		ExecutionMode:     "direct_argv",
		NetworkAccess:     "none",
		TimeoutSeconds:    10,
		FixedArguments:    []string{"--json", "SELECT name, version FROM os_version;"},
		OutputFormat:      "json",
		MaxStdoutBytes:    2 << 20,
		MaxStderrBytes:    64 << 10,
		AcceptedExitCodes: []int{0},
	}
}

func TestProcessProfileAcceptsBoundedReadOnlyDirectArguments(t *testing.T) {
	profile := validProcessProfile()
	if err := profile.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestProcessProfileRejectsUnsafeExecutionModes(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ProcessProfile)
	}{
		{"write risk", func(p *ProcessProfile) { p.Risk = "low" }},
		{"admin", func(p *ProcessProfile) { p.RequiresAdmin = true }},
		{"approval", func(p *ProcessProfile) { p.RequiresApproval = true }},
		{"not idempotent", func(p *ProcessProfile) { p.Idempotent = false }},
		{"shell mode", func(p *ProcessProfile) { p.ExecutionMode = "shell" }},
		{"network access", func(p *ProcessProfile) { p.NetworkAccess = "read" }},
		{"invalid profile id", func(p *ProcessProfile) { p.ProfileID = "osquery" }},
		{"invalid artifact id", func(p *ProcessProfile) { p.ArtifactID = "../osquery" }},
		{"invalid version", func(p *ProcessProfile) { p.ArtifactVersion = "5.24" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile := validProcessProfile()
			test.mutate(&profile)
			if err := profile.Validate(); !errors.Is(err, ErrInvalidProcessProfile) {
				t.Fatalf("Validate() error = %v, want ErrInvalidProcessProfile", err)
			}
		})
	}
}

func TestProcessProfileRejectsOutOfRangeResourceLimits(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ProcessProfile)
	}{
		{"zero timeout", func(p *ProcessProfile) { p.TimeoutSeconds = 0 }},
		{"timeout too long", func(p *ProcessProfile) { p.TimeoutSeconds = MaxProcessTimeoutSeconds + 1 }},
		{"stdout too large", func(p *ProcessProfile) { p.MaxStdoutBytes = MaxProcessStdoutBytes + 1 }},
		{"stderr too large", func(p *ProcessProfile) { p.MaxStderrBytes = MaxProcessStderrBytes + 1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile := validProcessProfile()
			test.mutate(&profile)
			if err := profile.Validate(); !errors.Is(err, ErrInvalidProcessProfile) {
				t.Fatalf("Validate() error = %v, want ErrInvalidProcessProfile", err)
			}
		})
	}
}

func TestProcessProfileRejectsUnboundedOrControlArguments(t *testing.T) {
	profile := validProcessProfile()
	profile.FixedArguments = make([]string, MaxProcessArguments+1)
	for index := range profile.FixedArguments {
		profile.FixedArguments[index] = "x"
	}
	if err := profile.Validate(); !errors.Is(err, ErrInvalidProcessProfile) {
		t.Fatalf("too many arguments error = %v", err)
	}

	profile = validProcessProfile()
	profile.FixedArguments = []string{"SELECT\nversion FROM os_version;"}
	if err := profile.Validate(); !errors.Is(err, ErrInvalidProcessProfile) {
		t.Fatalf("control character error = %v", err)
	}

	profile = validProcessProfile()
	profile.FixedArguments = []string{strings.Repeat("x", MaxProcessArgumentBytes+1)}
	if err := profile.Validate(); !errors.Is(err, ErrInvalidProcessProfile) {
		t.Fatalf("oversized argument error = %v", err)
	}
}

func TestProcessProfileRequiresSuccessExitCodeAndUniqueCodes(t *testing.T) {
	profile := validProcessProfile()
	profile.AcceptedExitCodes = []int{1}
	if err := profile.Validate(); !errors.Is(err, ErrInvalidProcessProfile) {
		t.Fatalf("missing zero exit code error = %v", err)
	}

	profile = validProcessProfile()
	profile.AcceptedExitCodes = []int{0, 0}
	if err := profile.Validate(); !errors.Is(err, ErrInvalidProcessProfile) {
		t.Fatalf("duplicate exit code error = %v", err)
	}
}

func TestProcessProfileDoesNotExpandShellMetacharacters(t *testing.T) {
	profile := validProcessProfile()
	profile.FixedArguments = []string{"--literal", "& $(not-expanded) |"}
	if err := profile.Validate(); err != nil {
		t.Fatalf("static argv should preserve shell metacharacters literally: %v", err)
	}
	if profile.FixedArguments[1] != "& $(not-expanded) |" {
		t.Fatal("fixed argument was modified")
	}
}


func TestProcessProfileRejectsAggregateArgumentLimit(t *testing.T) {
	profile := validProcessProfile()
	profile.FixedArguments = []string{
		strings.Repeat("x", MaxProcessArgumentBytes),
		strings.Repeat("x", MaxProcessArgumentBytes),
		strings.Repeat("x", MaxProcessArgumentBytes),
		strings.Repeat("x", MaxProcessArgumentBytes),
		"x",
	}
	if err := profile.Validate(); !errors.Is(err, ErrInvalidProcessProfile) {
		t.Fatalf("aggregate argument bytes error = %v", err)
	}
}
