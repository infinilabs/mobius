package main

import (
	"strings"
	"testing"
)

func TestValidateDelegateArgs(t *testing.T) {
	if err := validateDelegateArgs("fix bug", "make tests pass", "some context"); err != nil {
		t.Errorf("normal args should pass: %v", err)
	}
	long := strings.Repeat("x", maxTextArgLen+1)
	if err := validateDelegateArgs(strings.Repeat("t", maxTitleArgLen+1), "g", ""); err == nil {
		t.Error("oversized title must be rejected")
	}
	if err := validateDelegateArgs("t", long, ""); err == nil {
		t.Error("oversized goal must be rejected")
	}
	if err := validateDelegateArgs("t", "g", long); err == nil {
		t.Error("oversized context must be rejected")
	}
}

func TestValidateHireArgs(t *testing.T) {
	if err := validateHireArgs("Ada", "Engineer", "backstory"); err != nil {
		t.Errorf("normal args should pass: %v", err)
	}
	if err := validateHireArgs(strings.Repeat("n", maxTitleArgLen+1), "t", "b"); err == nil {
		t.Error("oversized name must be rejected")
	}
	if err := validateHireArgs("n", strings.Repeat("t", maxTitleArgLen+1), "b"); err == nil {
		t.Error("oversized title must be rejected")
	}
	if err := validateHireArgs("n", "t", strings.Repeat("b", maxTextArgLen+1)); err == nil {
		t.Error("oversized backstory must be rejected")
	}
}

func TestTruncateForContext(t *testing.T) {
	small := "hello"
	got, truncated := truncateForContext(small)
	if truncated || got != small {
		t.Errorf("small content must pass through unchanged, got truncated=%v", truncated)
	}

	big := strings.Repeat("a", maxToolResultBytes+100)
	got, truncated = truncateForContext(big)
	if !truncated {
		t.Fatal("oversized content must be truncated")
	}
	if len(got) > maxToolResultBytes+200 { // payload cap + short marker
		t.Errorf("truncated content still too large: %d bytes", len(got))
	}
	if !strings.Contains(got, "truncated") {
		t.Error("truncated content must carry a visible marker")
	}
}
