package main

import (
	"testing"
	"time"
)

// madeProgress is the per-tick advance check monitorRun uses to decide whether a
// run is still doing work. It must treat EITHER more output OR more tokens as
// progress, so a run that streams output without new token accounting (or vice
// versa) is never falsely judged stalled.
func TestMadeProgress(t *testing.T) {
	tests := []struct {
		name                                   string
		outLen, tokens, lastOutLen, lastTokens int
		want                                   bool
	}{
		{"no change", 100, 50, 100, 50, false},
		{"more output only", 120, 50, 100, 50, true},
		{"more tokens only", 100, 60, 100, 50, true},
		{"both grew", 120, 60, 100, 50, true},
		{"first observation from zero with tokens", 0, 10, 0, 0, true},
		{"first observation from zero, nothing yet", 0, 0, 0, 0, false},
		// Output/tokens never shrink in practice, but a regression must not be
		// read as progress.
		{"counts went backwards", 80, 40, 100, 50, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := madeProgress(tt.outLen, tt.tokens, tt.lastOutLen, tt.lastTokens); got != tt.want {
				t.Errorf("madeProgress(%d,%d,%d,%d) = %v, want %v",
					tt.outLen, tt.tokens, tt.lastOutLen, tt.lastTokens, got, tt.want)
			}
		})
	}
}

// shouldStallKill encodes WHY the stall guard is scoped the way it is: only
// token-reporting (LLM-backed) runs may be killed for inactivity. Process
// adapters (bash/CLI) report no tokens and can legitimately run quiet, so they
// must never be stall-killed regardless of how long they go without output —
// their runaway is bounded by the run-ctx cap instead.
func TestShouldStallKill(t *testing.T) {
	const timeout = 5 * time.Minute
	tests := []struct {
		name          string
		hasTokens     bool
		sinceProgress time.Duration
		want          bool
	}{
		{"llm run idle past timeout is killed", true, 6 * time.Minute, true},
		{"llm run idle within timeout survives", true, 4 * time.Minute, false},
		{"llm run idle exactly at timeout survives", true, timeout, false},
		{"process run idle past timeout is NOT killed", false, 30 * time.Minute, false},
		{"process run never killed for inactivity", false, time.Hour, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldStallKill(tt.hasTokens, tt.sinceProgress, timeout); got != tt.want {
				t.Errorf("shouldStallKill(%v, %v, %v) = %v, want %v",
					tt.hasTokens, tt.sinceProgress, timeout, got, tt.want)
			}
		})
	}
}
