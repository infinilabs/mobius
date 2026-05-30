package main

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
)

func (h *APIHandler) ListInteractions(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}

	taskID := r.PathValue("id")
	interactions, err := h.pgClient.ListInteractions(r.Context(), taskID)
	if err != nil {
		slog.Error("list interactions failed", "task_id", taskID, "error", err)
		writeError(w, "failed to list interactions", http.StatusInternalServerError)
		return
	}
	writeJSON(w, interactions)
}

func (h *APIHandler) ResolveInteraction(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}

	interactionID := r.PathValue("interactionId")

	var body struct {
		ResolvedBy string          `json:"resolved_by"`
		Response   json.RawMessage `json:"response"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.ResolvedBy != "" {
		if _, err := uuid.Parse(body.ResolvedBy); err != nil {
			writeError(w, "resolved_by must be a valid UUID", http.StatusBadRequest)
			return
		}
	}

	if err := h.pgClient.ResolveInteraction(r.Context(), interactionID, body.ResolvedBy, body.Response); err != nil {
		slog.Warn("resolve interaction failed", "id", interactionID, "error", err)
		writeError(w, "failed to resolve interaction", http.StatusBadRequest)
		return
	}

	slog.Info("interaction resolved", "id", interactionID, "resolved_by", body.ResolvedBy)
	writeJSON(w, map[string]string{"status": "resolved", "id": interactionID})
}
