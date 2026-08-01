package main

// Transitional alias (plan 6.4f): cron schedule computation lives in
// internal/schedule.

import "mobius/internal/schedule"

var (
	ComputeNextRun = schedule.ComputeNextRun
	ScheduleKind   = schedule.ScheduleKind
	GracePeriod    = schedule.GracePeriod
)
