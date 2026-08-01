package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// eventsWSHandler streams live pipeline events to the frontend (plan 7.3) so
// pages refresh on activity instead of polling. The route is registered behind
// the API auth middleware (cookie auth works for browser WebSockets), and
// wsUpgrader's checkOrigin rejects cross-site browser connections.
func eventsWSHandler(ep *EventPipeline) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ep == nil {
			http.Error(w, "event stream unavailable", http.StatusServiceUnavailable)
			return
		}

		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("events WebSocket upgrade failed", "error", err)
			return
		}
		defer conn.Close()
		wc := &wsConn{conn: conn}

		sub, cancel := ep.Subscribe()
		defer cancel()

		conn.SetReadLimit(wsReadLimit)
		conn.SetReadDeadline(time.Now().Add(wsPongWait))
		conn.SetPongHandler(func(string) error {
			conn.SetReadDeadline(time.Now().Add(wsPongWait))
			return nil
		})

		// Read pump: the client never sends data, but reading surfaces pongs
		// (resetting the deadline) and detects the close frame.
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()

		ticker := time.NewTicker(wsPingPeriod)
		defer ticker.Stop()

		for {
			select {
			case evt := <-sub:
				msg, err := json.Marshal(evt)
				if err != nil {
					slog.Warn("events WebSocket marshal failed", "error", err)
					continue
				}
				if err := wc.write(msg); err != nil {
					return
				}
			case <-ticker.C:
				if err := wc.ping(); err != nil {
					return
				}
			case <-done:
				return
			}
		}
	}
}
