// Package supervisor defines the restricted contract for external read-only
// process actions. A validated profile is not permission to execute a process:
// authenticated TUF provenance and a cross-platform process runner are separate gates.
package supervisor

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	ProcessProfileSchemaVersion = "1.0.0"
	MaxProcessTimeoutSeconds    = 300
	MaxProcessArguments         = 32
	MaxProcessArgumentBytes     = 4096
	MaxProcessArgumentTotal     = 16 << 10
	MaxProcessStdoutBytes       = 8 << 20
	MaxProcessStderrBytes       = 256 << 10
)

var (
	ErrInvalidProcessProfile = fmt.Errorf("perfil de ejecución de proceso inválido")
	profileIDPattern         = regexp.MustCompile(`^soptraz\.process\.[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
	actionIDPattern          = regexp.MustCompile(`^soptraz\.[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
	artifactIDPattern        = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$`)
	semverPattern            = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)
)

// ProcessProfile is a fixed, reviewable argv profile for one read-only action.
// It intentionally has no executable path, shell, environment or working-directory
// field. The artifact is selected separately by artifactId/version and must be
// authenticated by the TUF catalog before any future process runner may use it.
type ProcessProfile struct {
	SchemaVersion     string   `json:"schemaVersion"`
	ProfileID         string   `json:"profileId"`
	ProfileVersion    string   `json:"profileVersion"`
	ActionID          string   `json:"actionId"`
	ActionVersion     string   `json:"actionVersion"`
	ArtifactID        string   `json:"artifactId"`
	ArtifactVersion   string   `json:"artifactVersion"`
	Purpose           string   `json:"purpose"`
	Risk              string   `json:"risk"`
	RequiresAdmin     bool     `json:"requiresAdmin"`
	RequiresApproval  bool     `json:"requiresApproval"`
	Idempotent        bool     `json:"idempotent"`
	ExecutionMode     string   `json:"executionMode"`
	NetworkAccess     string   `json:"networkAccess"`
	TimeoutSeconds    int      `json:"timeoutSeconds"`
	FixedArguments    []string `json:"fixedArguments"`
	OutputFormat      string   `json:"outputFormat"`
	MaxStdoutBytes    int64    `json:"maxStdoutBytes"`
	MaxStderrBytes    int64    `json:"maxStderrBytes"`
	AcceptedExitCodes []int    `json:"acceptedExitCodes"`
}

// Validate applies semantic limits in addition to the embedded JSON Schema.
// It is deliberately limited to local, read-only, direct-argv profiles.
func (profile ProcessProfile) Validate() error {
	invalid := func(reason string) error {
		return fmt.Errorf("%w: %s", ErrInvalidProcessProfile, reason)
	}
	if profile.SchemaVersion != ProcessProfileSchemaVersion {
		return invalid("schemaVersion no soportada")
	}
	if !profileIDPattern.MatchString(profile.ProfileID) || len(profile.ProfileID) > 160 {
		return invalid("profileId inválido")
	}
	for name, version := range map[string]string{
		"profileVersion":  profile.ProfileVersion,
		"actionVersion":   profile.ActionVersion,
		"artifactVersion": profile.ArtifactVersion,
	} {
		if !semverPattern.MatchString(version) || len(version) > 80 {
			return invalid(name + " debe usar SemVer")
		}
	}
	if !actionIDPattern.MatchString(profile.ActionID) || len(profile.ActionID) > 160 {
		return invalid("actionId inválido")
	}
	if !artifactIDPattern.MatchString(profile.ArtifactID) || len(profile.ArtifactID) > 128 {
		return invalid("artifactId inválido")
	}
	if len(strings.TrimSpace(profile.Purpose)) < 10 || len(profile.Purpose) > 1000 {
		return invalid("purpose ausente o fuera de límites")
	}
	if profile.Risk != "read_only" || profile.RequiresAdmin || profile.RequiresApproval || !profile.Idempotent {
		return invalid("solo se permiten acciones read_only, sin elevación ni aprobación, e idempotentes")
	}
	if profile.ExecutionMode != "direct_argv" {
		return invalid("executionMode debe ser direct_argv; no se admite shell")
	}
	if profile.NetworkAccess != "none" {
		return invalid("networkAccess debe ser none")
	}
	if profile.TimeoutSeconds < 1 || profile.TimeoutSeconds > MaxProcessTimeoutSeconds {
		return invalid("timeoutSeconds fuera de los límites permitidos")
	}
	if len(profile.FixedArguments) > MaxProcessArguments {
		return invalid("demasiados argumentos fijos")
	}
	totalArgumentBytes := 0
	for index, argument := range profile.FixedArguments {
		if argument == "" || !utf8.ValidString(argument) || len(argument) > MaxProcessArgumentBytes {
			return invalid(fmt.Sprintf("fixedArguments[%d] vacío, inválido o demasiado largo", index))
		}
		if strings.IndexFunc(argument, unicode.IsControl) >= 0 {
			return invalid(fmt.Sprintf("fixedArguments[%d] contiene caracteres de control", index))
		}
		totalArgumentBytes += len(argument)
	}
	if totalArgumentBytes > MaxProcessArgumentTotal {
		return invalid("la suma de argumentos supera el límite")
	}
	if profile.OutputFormat != "none" && profile.OutputFormat != "text" &&
		profile.OutputFormat != "json" && profile.OutputFormat != "jsonl" {
		return invalid("outputFormat no soportado")
	}
	if profile.MaxStdoutBytes < 1 || profile.MaxStdoutBytes > MaxProcessStdoutBytes {
		return invalid("maxStdoutBytes fuera de los límites permitidos")
	}
	if profile.MaxStderrBytes < 1 || profile.MaxStderrBytes > MaxProcessStderrBytes {
		return invalid("maxStderrBytes fuera de los límites permitidos")
	}
	if len(profile.AcceptedExitCodes) == 0 || len(profile.AcceptedExitCodes) > 8 {
		return invalid("acceptedExitCodes debe contener entre uno y ocho códigos")
	}
	seen := make(map[int]struct{}, len(profile.AcceptedExitCodes))
	hasSuccess := false
	for _, code := range profile.AcceptedExitCodes {
		if code < 0 || code > 255 {
			return invalid("acceptedExitCodes contiene un código fuera de rango")
		}
		if _, exists := seen[code]; exists {
			return invalid("acceptedExitCodes contiene duplicados")
		}
		seen[code] = struct{}{}
		if code == 0 {
			hasSuccess = true
		}
	}
	if !hasSuccess {
		return invalid("acceptedExitCodes debe incluir el código 0")
	}
	return nil
}
