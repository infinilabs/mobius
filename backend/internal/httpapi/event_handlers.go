package httpapi

import (
	"net/http"
	"strconv"
	"time"
)

func (h *APIHandler) ListEvents(w http.ResponseWriter, r *http.Request) {
	if h.esClient == nil {
		writeError(w, "Elasticsearch not available", http.StatusServiceUnavailable)
		return
	}

	q := r.URL.Query()
	eventType := q.Get("event_type")
	actorID := q.Get("actor_id")
	projectID := q.Get("project_id")
	taskID := q.Get("task_id")

	var since, until time.Time
	if s := q.Get("since"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			since = t
		}
	}
	if u := q.Get("until"); u != "" {
		if t, err := time.Parse(time.RFC3339, u); err == nil {
			until = t
		}
	}

	limit := 50
	if l := q.Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}

	events, err := h.esClient.SearchEvents(r.Context(), eventType, actorID, projectID, taskID, since, until, limit)
	if err != nil {
		writeError(w, "failed to search events: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"events": events, "count": len(events)})
}

func (h *APIHandler) EventStats(w http.ResponseWriter, r *http.Request) {
	if h.esClient == nil {
		writeError(w, "Elasticsearch not available", http.StatusServiceUnavailable)
		return
	}

	q := r.URL.Query()
	projectID := q.Get("project_id")

	var since, until time.Time
	if s := q.Get("since"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			since = t
		}
	}
	if u := q.Get("until"); u != "" {
		if t, err := time.Parse(time.RFC3339, u); err == nil {
			until = t
		}
	}

	counts, total, err := h.esClient.EventStats(r.Context(), projectID, since, until)
	if err != nil {
		writeError(w, "failed to get event stats: "+err.Error(), http.StatusInternalServerError)
		return
	}

	period := map[string]string{}
	if !since.IsZero() {
		period["since"] = since.Format(time.RFC3339)
	}
	if !until.IsZero() {
		period["until"] = until.Format(time.RFC3339)
	}

	writeJSON(w, map[string]any{
		"period": period,
		"counts": counts,
		"total":  total,
	})
}
