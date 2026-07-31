package main

import (
	"fmt"
	"sync"
	"time"
)

// Per-caller rate limiting for paid GCP/LLM operations (plan 3.4): an agent
// stuck in a loop must not be able to trigger unbounded BigQuery/Vertex spend.
// Enforced next to authorizeToolCall at all three tool dispatch entrances
// (adapter routeToolCall, chat executeToolCall, MCP HandleMessage), so the
// paths cannot drift apart.

// toolRateLimits caps calls per caller per rateLimitWindow. Tools absent from
// the table are unlimited.
var toolRateLimits = map[string]int{
	"tag_media":      4,  // BigQuery AI.GENERATE_TABLE over up to 200 assets
	"generate_image": 20, // Vertex/LLM image generation
	"generate_audio": 20, // TTS generation
	"query_tags":     60, // BigQuery analytics scans
}

const rateLimitWindow = time.Hour

// rateLimiter is a fixed-window counter keyed by an opaque bucket key
// (caller|tool). Windows are per bucket, starting at the first call.
type rateLimiter struct {
	window  time.Duration
	now     func() time.Time // injectable for tests
	mu      sync.Mutex
	buckets map[string]*rateBucket
}

type rateBucket struct {
	windowStart time.Time
	count       int
}

func newRateLimiter(window time.Duration) *rateLimiter {
	return &rateLimiter{
		window:  window,
		now:     time.Now,
		buckets: make(map[string]*rateBucket),
	}
}

// allow counts one call against key and errors when the count would exceed
// limit within the current window.
func (rl *rateLimiter) allow(key string, limit int) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := rl.now()
	b, ok := rl.buckets[key]
	if !ok || now.Sub(b.windowStart) >= rl.window {
		rl.buckets[key] = &rateBucket{windowStart: now, count: 1}
		return nil
	}
	if b.count >= limit {
		retry := b.windowStart.Add(rl.window).Sub(now).Round(time.Second)
		return fmt.Errorf("rate limit exceeded: at most %d calls per %s (retry in %s)", limit, rl.window, retry)
	}
	b.count++
	return nil
}

// globalToolRateLimiter is process-wide: the three dispatch entrances live on
// different structs, and the guard must count across all of them.
var globalToolRateLimiter = newRateLimiter(rateLimitWindow)

// rateLimitToolCall applies toolRateLimits for one tool invocation by actorID.
// Tools without a limit and system calls without an actor pass through.
func rateLimitToolCall(actorID, toolName string) error {
	limit, ok := toolRateLimits[toolName]
	if !ok || actorID == "" {
		return nil
	}
	if err := globalToolRateLimiter.allow(actorID+"|"+toolName, limit); err != nil {
		return fmt.Errorf("%s: %w", toolName, err)
	}
	return nil
}
