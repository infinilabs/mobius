package main

import "testing"

// failTransition is the guard that routes failTask through the task state
// machine (plan 1.4): a late failure may only act on a task the run still owns
// (in_progress). The high-stakes case is needs_review — a provider error
// arriving after a successful submit must not discard the submission.
func TestFailTransition(t *testing.T) {
	tests := []struct {
		name     string
		status   string
		failures int
		wantNext string
		wantOK   bool
	}{
		{"in_progress below cap retries", "in_progress", 1, "ready", true},
		{"in_progress at cap blocks", "in_progress", 3, "blocked", true},
		{"needs_review keeps submitted result", "needs_review", 1, "", false},
		{"done is terminal", "done", 1, "", false},
		{"ready already reclaimed", "ready", 1, "", false},
		{"blocked already parked", "blocked", 4, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			next, ok := failTransition(tc.status, tc.failures)
			if ok != tc.wantOK || next != tc.wantNext {
				t.Errorf("failTransition(%q, %d) = (%q, %v), want (%q, %v)",
					tc.status, tc.failures, next, ok, tc.wantNext, tc.wantOK)
			}
		})
	}
}
