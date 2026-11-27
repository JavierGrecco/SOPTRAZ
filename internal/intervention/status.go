package intervention

import "fmt"

// Status is the persisted lifecycle state of a technical intervention.
type Status string

const (
	StatusCreated          Status = "created"
	StatusTriaged          Status = "triaged"
	StatusDiagnosed        Status = "diagnosed"
	StatusAwaitingApproval Status = "awaiting_approval"
	StatusApproved         Status = "approved"
	StatusExecuting        Status = "executing"
	StatusVerifying        Status = "verifying"
	StatusResolved         Status = "resolved"
	StatusFailed           Status = "failed"
	StatusCancelled        Status = "cancelled"
	StatusClosed           Status = "closed"
)

var transitions = map[Status]map[Status]struct{}{
	StatusCreated: {
		StatusTriaged: {}, StatusCancelled: {},
	},
	StatusTriaged: {
		StatusDiagnosed: {}, StatusCancelled: {},
	},
	StatusDiagnosed: {
		StatusAwaitingApproval: {}, StatusResolved: {}, StatusFailed: {}, StatusCancelled: {},
	},
	StatusAwaitingApproval: {
		StatusApproved: {}, StatusCancelled: {},
	},
	StatusApproved: {
		StatusExecuting: {}, StatusCancelled: {},
	},
	StatusExecuting: {
		StatusVerifying: {}, StatusFailed: {},
	},
	StatusVerifying: {
		StatusResolved: {}, StatusFailed: {},
	},
	StatusResolved: {
		StatusClosed: {},
	},
	StatusFailed: {
		StatusTriaged: {}, StatusClosed: {},
	},
	StatusCancelled: {
		StatusClosed: {},
	},
	StatusClosed: {},
}

// CanTransition reports whether a lifecycle transition is allowed.
func CanTransition(from, to Status) bool {
	allowed, known := transitions[from]
	if !known {
		return false
	}
	_, ok := allowed[to]
	return ok
}

// ValidateTransition returns a descriptive error when a transition is invalid.
func ValidateTransition(from, to Status) error {
	if !CanTransition(from, to) {
		return fmt.Errorf("transición de intervención no permitida: %q -> %q", from, to)
	}
	return nil
}
