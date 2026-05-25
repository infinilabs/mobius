package main

import "testing"

func TestValidTransitions_AllowedPaths(t *testing.T) {
	allowed := []struct{ from, to string }{
		{"todo", "ready"},
		{"todo", "blocked"},
		{"ready", "in_progress"},
		{"ready", "blocked"},
		{"in_progress", "needs_review"},
		{"in_progress", "ready"},
		{"in_progress", "blocked"},
		{"needs_review", "done"},
		{"needs_review", "ready"},
		{"needs_review", "blocked"},
		{"blocked", "ready"},
	}
	for _, tc := range allowed {
		if !validTransitions[tc.from][tc.to] {
			t.Errorf("expected %s -> %s to be allowed", tc.from, tc.to)
		}
	}
}

func TestValidTransitions_BlockedPaths(t *testing.T) {
	blocked := []struct{ from, to string }{
		{"todo", "done"},
		{"todo", "in_progress"},
		{"ready", "done"},
		{"ready", "needs_review"},
		{"in_progress", "done"},
		{"needs_review", "in_progress"},
		{"done", "ready"},
		{"blocked", "done"},
		{"blocked", "in_progress"},
		{"scheduled", "ready"},
		{"scheduled", "done"},
	}
	for _, tc := range blocked {
		if validTransitions[tc.from][tc.to] {
			t.Errorf("expected %s -> %s to be blocked", tc.from, tc.to)
		}
	}
}

func TestValidTransitions_ScheduledIsTerminal(t *testing.T) {
	if len(validTransitions["scheduled"]) != 0 {
		t.Error("scheduled should have no outgoing transitions")
	}
}
