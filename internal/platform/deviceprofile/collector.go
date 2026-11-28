// Package deviceprofile contains native, read-only device inventory adapters.
package deviceprofile

import moduleprofile "github.com/JavierGrecco/SOPTRAZ/internal/modules/deviceprofile"

// SourceError identifies a hardware information source that could not be read.
// A failed source does not discard observations successfully collected elsewhere.
type SourceError struct {
	Source  string `json:"source"`
	Message string `json:"message"`
}

// Result is a partial-safe hardware snapshot. Fields that a platform could not
// observe remain Unknown in Observations.Capabilities.
type Result struct {
	Observations moduleprofile.Observations `json:"observations"`
	SourceErrors []SourceError              `json:"source_errors,omitempty"`
}

func (result *Result) addSourceError(source string, err error) {
	if result == nil || err == nil {
		return
	}
	result.SourceErrors = append(result.SourceErrors, SourceError{
		Source:  source,
		Message: err.Error(),
	})
}
