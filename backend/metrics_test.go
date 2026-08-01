package main

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mobius/internal/storage/postgres"
)

func TestLiveHandler_AlwaysOK(t *testing.T) {
	rec := httptest.NewRecorder()
	liveHandler(rec, httptest.NewRequest("GET", "/live", nil))
	if rec.Code != 200 {
		t.Fatalf("live probe returned %d, want 200", rec.Code)
	}
}

func TestMetricsHandler_ExposesCounters(t *testing.T) {
	recordDispatchDuration(1500 * time.Millisecond)

	// A nil pipeline (ES disabled) must still serve metrics.
	rec := httptest.NewRecorder()
	metricsHandler(nil)(rec, httptest.NewRequest("GET", "/metrics", nil))
	if rec.Code != 200 {
		t.Fatalf("metrics returned %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"mobius_events_dropped_total 0",
		"mobius_event_queue_capacity 0",
		"mobius_dispatch_duration_seconds_count",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics body missing %q:\n%s", want, body)
		}
	}

	// With a live pipeline the drop counter is wired through.
	ep := NewEventPipeline(nil, nil, EventConfig{BufferSize: 1})
	ep.Publish(&Event{EventType: "a"})
	ep.Publish(&Event{EventType: "b"})
	rec = httptest.NewRecorder()
	metricsHandler(ep)(rec, httptest.NewRequest("GET", "/metrics", nil))
	if body := rec.Body.String(); !strings.Contains(body, "mobius_events_dropped_total 1") {
		t.Errorf("metrics body missing pipeline drop count:\n%s", body)
	}
}

// Readiness must track Postgres: 200 while it answers, 503 once it doesn't.
func TestReadyHandler_TracksPostgres(t *testing.T) {
	pg := testPG(t)

	rec := httptest.NewRecorder()
	readyHandler(pg)(rec, httptest.NewRequest("GET", "/ready", nil))
	if rec.Code != 200 {
		t.Fatalf("ready with live Postgres returned %d, want 200", rec.Code)
	}

	// A separate client on the same DSN, closed, simulates Postgres down
	// without touching the shared harness pool.
	dead, err := postgres.New(context.Background(), pg.DSN())
	if err != nil {
		t.Fatalf("open second client: %v", err)
	}
	dead.Close()

	rec = httptest.NewRecorder()
	readyHandler(dead)(rec, httptest.NewRequest("GET", "/ready", nil))
	if rec.Code != 503 {
		t.Fatalf("ready with closed pool returned %d, want 503", rec.Code)
	}
}
