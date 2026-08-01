package service

import (
	"strings"
	"testing"
	"time"
)

func TestRateLimiter_Allow(t *testing.T) {
	now := time.Unix(1000, 0)
	rl := newRateLimiter(time.Hour)
	rl.now = func() time.Time { return now }

	// Within the limit: allowed.
	for i := 0; i < 3; i++ {
		if err := rl.allow("emp-1|tag_media", 3); err != nil {
			t.Fatalf("call %d should be allowed: %v", i+1, err)
		}
	}
	// Over the limit: refused with a retry hint.
	err := rl.allow("emp-1|tag_media", 3)
	if err == nil {
		t.Fatal("4th call within the window must be refused")
	}
	if !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("error should mention rate limit: %v", err)
	}

	// Other callers and other tools are independent.
	if err := rl.allow("emp-2|tag_media", 3); err != nil {
		t.Errorf("different caller must not share the bucket: %v", err)
	}
	if err := rl.allow("emp-1|generate_image", 3); err != nil {
		t.Errorf("different tool must not share the bucket: %v", err)
	}

	// Window rollover resets the count.
	now = now.Add(time.Hour + time.Second)
	if err := rl.allow("emp-1|tag_media", 3); err != nil {
		t.Errorf("call after window rollover should be allowed: %v", err)
	}
}

func TestToolRateLimits_CoverPaidOperations(t *testing.T) {
	// The paid GCP/LLM operations must all be in the policy table (plan 3.4).
	for _, tool := range []string{"tag_media", "generate_image", "generate_audio", "query_tags"} {
		if toolRateLimits[tool] <= 0 {
			t.Errorf("expensive tool %q must have a positive rate limit", tool)
		}
	}
}

func TestRateLimitToolCall(t *testing.T) {
	// Unlimited tools always pass and must not allocate buckets.
	if err := RateLimitToolCall("emp-1", "list_tasks"); err != nil {
		t.Errorf("tool without a limit must pass: %v", err)
	}
	// An empty actor (system paths) is never limited.
	if err := RateLimitToolCall("", "tag_media"); err != nil {
		t.Errorf("empty actor must pass: %v", err)
	}
}
