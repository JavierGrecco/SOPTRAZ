package intervention

import "testing"

func TestCanTransitionAllowsExpectedLifecycle(t *testing.T) {
	cases := []struct {
		from Status
		to   Status
	}{
		{StatusCreated, StatusTriaged},
		{StatusTriaged, StatusDiagnosed},
		{StatusDiagnosed, StatusAwaitingApproval},
		{StatusAwaitingApproval, StatusApproved},
		{StatusApproved, StatusExecuting},
		{StatusExecuting, StatusVerifying},
		{StatusVerifying, StatusResolved},
		{StatusResolved, StatusClosed},
		{StatusFailed, StatusTriaged},
		{StatusCancelled, StatusClosed},
	}
	for _, tc := range cases {
		if !CanTransition(tc.from, tc.to) {
			t.Errorf("CanTransition(%q, %q) = false, want true", tc.from, tc.to)
		}
	}
}

func TestCanTransitionRejectsUnsafeOrAmbiguousTransitions(t *testing.T) {
	cases := []struct {
		from Status
		to   Status
	}{
		{StatusCreated, StatusExecuting},
		{StatusDiagnosed, StatusExecuting},
		{StatusAwaitingApproval, StatusExecuting},
		{StatusExecuting, StatusResolved},
		{StatusClosed, StatusTriaged},
		{StatusClosed, StatusClosed},
		{"unknown", StatusTriaged},
	}
	for _, tc := range cases {
		if CanTransition(tc.from, tc.to) {
			t.Errorf("CanTransition(%q, %q) = true, want false", tc.from, tc.to)
		}
	}
}

func TestValidateTransitionExplainsRejection(t *testing.T) {
	if err := ValidateTransition(StatusDiagnosed, StatusExecuting); err == nil {
		t.Fatal("ValidateTransition should reject bypassing approval and execution states")
	}
}
