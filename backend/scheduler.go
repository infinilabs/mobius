package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

var intervalRegex = regexp.MustCompile(
	`^every\s+(\d+)\s*(m|min|mins|minute|minutes|h|hr|hrs|hour|hours|d|day|days)$`,
)

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

func ComputeNextRun(expr string, from time.Time) (time.Time, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return time.Time{}, fmt.Errorf("empty schedule expression")
	}

	if t, err := time.Parse(time.RFC3339, expr); err == nil {
		return t, nil
	}

	lower := strings.ToLower(expr)
	if match := intervalRegex.FindStringSubmatch(lower); len(match) == 3 {
		val, _ := strconv.Atoi(match[1])
		switch match[2][0] {
		case 'm':
			return from.Add(time.Duration(val) * time.Minute), nil
		case 'h':
			return from.Add(time.Duration(val) * time.Hour), nil
		case 'd':
			return from.Add(time.Duration(val) * 24 * time.Hour), nil
		}
	}

	sched, err := cronParser.Parse(expr)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid schedule expression %q: %w", expr, err)
	}
	return sched.Next(from), nil
}

func ScheduleKind(expr string) string {
	expr = strings.TrimSpace(expr)
	if _, err := time.Parse(time.RFC3339, expr); err == nil {
		return "once"
	}
	lower := strings.ToLower(expr)
	if intervalRegex.MatchString(lower) {
		return "interval"
	}
	if _, err := cronParser.Parse(expr); err == nil {
		return "cron"
	}
	return "unknown"
}

func GracePeriod(expr string) time.Duration {
	const minGrace = 120 * time.Second
	const maxGrace = 7200 * time.Second

	kind := ScheduleKind(expr)
	now := time.Now()

	var period time.Duration
	switch kind {
	case "interval":
		next, err := ComputeNextRun(expr, now)
		if err != nil {
			return minGrace
		}
		period = next.Sub(now)
	case "cron":
		sched, err := cronParser.Parse(expr)
		if err != nil {
			return minGrace
		}
		first := sched.Next(now)
		second := sched.Next(first)
		period = second.Sub(first)
	default:
		return minGrace
	}

	half := period / 2
	if half < minGrace {
		return minGrace
	}
	if half > maxGrace {
		return maxGrace
	}
	return half
}

func FormatScheduleDisplay(expr string) string {
	switch ScheduleKind(expr) {
	case "once":
		t, _ := time.Parse(time.RFC3339, expr)
		return "once at " + t.Format("2006-01-02 15:04")
	case "interval":
		return expr
	case "cron":
		return "cron: " + expr
	}
	return expr
}
