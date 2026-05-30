package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
)

func (h *APIHandler) ListGoals(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}
	projectID := r.URL.Query().Get("project_id")
	goals, err := h.pgClient.ListGoals(r.Context(), projectID)
	if err != nil {
		slog.Error("list goals failed", "error", err)
		writeError(w, "failed to list goals", http.StatusInternalServerError)
		return
	}
	writeJSON(w, goals)
}

func (h *APIHandler) CreateGoal(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Title       string  `json:"title"`
		Description string  `json:"description"`
		ParentID    *string `json:"parent_id"`
		ProjectID   *string `json:"project_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.Title == "" {
		writeError(w, "title is required", http.StatusBadRequest)
		return
	}

	g := &Goal{
		Title:       body.Title,
		Description: body.Description,
		ParentID:    body.ParentID,
		ProjectID:   body.ProjectID,
	}
	if err := h.pgClient.CreateGoal(r.Context(), g); err != nil {
		if errors.Is(err, errGoalBadReference) {
			writeError(w, "referenced parent goal or project does not exist", http.StatusBadRequest)
			return
		}
		slog.Error("create goal failed", "error", err)
		writeError(w, "failed to create goal", http.StatusInternalServerError)
		return
	}

	slog.Info("goal created", "id", g.ID, "title", g.Title)
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, g)
}

func (h *APIHandler) GetGoal(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	goal, err := h.pgClient.GetGoal(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, "goal not found", http.StatusNotFound)
			return
		}
		slog.Error("get goal failed", "id", id, "error", err)
		writeError(w, "failed to get goal", http.StatusInternalServerError)
		return
	}
	writeJSON(w, goal)
}

func (h *APIHandler) UpdateGoal(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	var body struct {
		Title       *string `json:"title"`
		Description *string `json:"description"`
		Status      *string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := h.pgClient.UpdateGoal(r.Context(), id, body.Title, body.Description, body.Status); err != nil {
		if errors.Is(err, errGoalNotFound) {
			writeError(w, "goal not found", http.StatusNotFound)
			return
		}
		slog.Error("update goal failed", "id", id, "error", err)
		writeError(w, "failed to update goal", http.StatusInternalServerError)
		return
	}
	goal, err := h.pgClient.GetGoal(r.Context(), id)
	if err != nil {
		slog.Error("get goal after update failed", "id", id, "error", err)
		writeError(w, "failed to load updated goal", http.StatusInternalServerError)
		return
	}
	writeJSON(w, goal)
}

func (h *APIHandler) DeleteGoal(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if err := h.pgClient.DeleteGoal(r.Context(), id); err != nil {
		slog.Error("delete goal failed", "id", id, "error", err)
		writeError(w, "failed to delete goal", http.StatusInternalServerError)
		return
	}
	slog.Info("goal deleted", "id", id)
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *APIHandler) ListGoalChildren(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	children, err := h.pgClient.GetGoalChildren(r.Context(), id)
	if err != nil {
		slog.Error("list goal children failed", "id", id, "error", err)
		writeError(w, "failed to list goal children", http.StatusInternalServerError)
		return
	}
	writeJSON(w, children)
}
