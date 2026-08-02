package main

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

// Observability endpoints (plan 7.5). /metrics emits Prometheus text format
// by hand — the handful of counters here doesn't justify a client-library
// dependency. /live and /ready are k8s-style probes; all three are top-level
// (not /api/*) and unauthenticated: orchestrators can't easily send tokens,
// and the metrics expose only aggregate counters.

var (
	dispatchRuns   atomic.Int64
	dispatchMillis atomic.Int64
)

// recordDispatchDuration is called by the dispatcher when a run's monitor
// loop ends (terminal state or shutdown).
func recordDispatchDuration(d time.Duration) {
	dispatchRuns.Add(1)
	dispatchMillis.Add(d.Milliseconds())
}

func metricsHandler(ep *EventPipeline) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

		var dropped int64
		var qLen, qCap int
		if ep != nil {
			dropped = ep.Dropped()
			qLen, qCap = ep.QueueStats()
		}
		fmt.Fprintf(w, "# HELP mobius_events_dropped_total Events discarded because the pipeline queue was full.\n")
		fmt.Fprintf(w, "# TYPE mobius_events_dropped_total counter\n")
		fmt.Fprintf(w, "mobius_events_dropped_total %d\n", dropped)
		fmt.Fprintf(w, "# HELP mobius_event_queue_length Current event pipeline queue length.\n")
		fmt.Fprintf(w, "# TYPE mobius_event_queue_length gauge\n")
		fmt.Fprintf(w, "mobius_event_queue_length %d\n", qLen)
		fmt.Fprintf(w, "# HELP mobius_event_queue_capacity Event pipeline queue capacity.\n")
		fmt.Fprintf(w, "# TYPE mobius_event_queue_capacity gauge\n")
		fmt.Fprintf(w, "mobius_event_queue_capacity %d\n", qCap)

		runs := dispatchRuns.Load()
		fmt.Fprintf(w, "# HELP mobius_dispatch_duration_seconds Wall-clock duration of dispatched task runs.\n")
		fmt.Fprintf(w, "# TYPE mobius_dispatch_duration_seconds summary\n")
		fmt.Fprintf(w, "mobius_dispatch_duration_seconds_sum %.3f\n", float64(dispatchMillis.Load())/1000)
		fmt.Fprintf(w, "mobius_dispatch_duration_seconds_count %d\n", runs)
	}
}

func liveHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "ok")
}

// readyHandler reports 503 until Postgres answers a ping — the service cannot
// do useful work without its primary store.
func readyHandler(pg *PGClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// main keeps serving with pg == nil when Postgres init fails at
		// startup — exactly the condition this probe must report.
		if pg == nil {
			http.Error(w, "postgres unavailable", http.StatusServiceUnavailable)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := pg.Ping(ctx); err != nil {
			http.Error(w, "postgres unavailable", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprintln(w, "ready")
	}
}
