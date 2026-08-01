package schedule

import (
	"testing"
	"time"
)

func TestComputeNextRun_CronExpression(t *testing.T) {
	// Monday 8am → next occurrence of "0 9 * * 1-5" should be Monday 9am
	mon8am := time.Date(2026, 5, 25, 8, 0, 0, 0, time.UTC) // Monday
	next, err := ComputeNextRun("0 9 * * 1-5", mon8am)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if next.Hour() != 9 || next.Minute() != 0 {
		t.Errorf("expected 09:00, got %s", next.Format("15:04"))
	}
	if next.Before(mon8am) || next.Equal(mon8am) {
		t.Errorf("next run should be after from time")
	}
}

func TestComputeNextRun_Interval(t *testing.T) {
	now := time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		expr     string
		expected time.Duration
	}{
		{"every 30m", 30 * time.Minute},
		{"every 30 minutes", 30 * time.Minute},
		{"every 2h", 2 * time.Hour},
		{"every 2 hours", 2 * time.Hour},
		{"every 1d", 24 * time.Hour},
		{"every 1 day", 24 * time.Hour},
	}
	for _, tc := range tests {
		next, err := ComputeNextRun(tc.expr, now)
		if err != nil {
			t.Errorf("ComputeNextRun(%q): unexpected error: %v", tc.expr, err)
			continue
		}
		got := next.Sub(now)
		if got != tc.expected {
			t.Errorf("ComputeNextRun(%q): expected %v, got %v", tc.expr, tc.expected, got)
		}
	}
}

func TestComputeNextRun_RFC3339_OneShot(t *testing.T) {
	ts := "2026-06-15T14:30:00Z"
	expected, _ := time.Parse(time.RFC3339, ts)

	// Regardless of 'from', a one-shot always returns the fixed time
	from := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	next, err := ComputeNextRun(ts, from)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !next.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, next)
	}
}

func TestComputeNextRun_EmptyExpression(t *testing.T) {
	_, err := ComputeNextRun("", time.Now())
	if err == nil {
		t.Error("expected error for empty expression")
	}
}

func TestComputeNextRun_InvalidExpression(t *testing.T) {
	_, err := ComputeNextRun("not a valid schedule", time.Now())
	if err == nil {
		t.Error("expected error for invalid expression")
	}
}

func TestScheduleKind_AllTypes(t *testing.T) {
	tests := []struct {
		expr     string
		expected string
	}{
		{"2026-06-15T14:30:00Z", "once"},
		{"every 30m", "interval"},
		{"every 2 hours", "interval"},
		{"0 9 * * 1-5", "cron"},
		{"*/5 * * * *", "cron"},
		{"garbage", "unknown"},
	}
	for _, tc := range tests {
		got := ScheduleKind(tc.expr)
		if got != tc.expected {
			t.Errorf("ScheduleKind(%q): expected %q, got %q", tc.expr, tc.expected, got)
		}
	}
}

func TestGracePeriod_Clamping(t *testing.T) {
	// Short interval → clamped to minimum 120s
	grace := GracePeriod("every 1m")
	if grace < 120*time.Second {
		t.Errorf("expected minimum 120s grace, got %v", grace)
	}

	// Long interval → clamped to maximum 7200s
	grace = GracePeriod("every 7d")
	if grace > 7200*time.Second {
		t.Errorf("expected maximum 7200s grace, got %v", grace)
	}
}

func TestGracePeriod_CronHalfPeriod(t *testing.T) {
	// Hourly cron: period = 60min, grace = 30min
	grace := GracePeriod("0 * * * *")
	expected := 30 * time.Minute
	if grace != expected {
		t.Errorf("expected %v grace for hourly cron, got %v", expected, grace)
	}
}

func TestGracePeriod_OnceReturnsMinimum(t *testing.T) {
	grace := GracePeriod("2026-06-15T14:30:00Z")
	if grace != 120*time.Second {
		t.Errorf("expected 120s grace for one-shot, got %v", grace)
	}
}
