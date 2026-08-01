package httpapi

import (
	"encoding/json"
	"log/slog"
	"mobius/internal/domain"
	"mobius/internal/schedule"
	"net/http"
	"time"
)

// domain.Task, domain.TaskComment live in internal/domain (plan 6.1); see domain_aliases.go.

// HTTP handlers

func (h *APIHandler) ListTasks(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}

	status := r.URL.Query().Get("status")
	assigneeID := r.URL.Query().Get("assignee_id")
	projectID := r.URL.Query().Get("project_id")
	conversationID := r.URL.Query().Get("conversation_id")

	tasks, err := h.pgClient.ListTasks(r.Context(), status, assigneeID, projectID, conversationID)
	if err != nil {
		writeError(w, "failed to list tasks: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, tasks)
}

func (h *APIHandler) GetTask(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}

	id := r.PathValue("id")
	task, err := h.pgClient.GetTask(r.Context(), id)
	if err != nil {
		writeError(w, "task not found", http.StatusNotFound)
		return
	}
	writeJSON(w, task)
}

func (h *APIHandler) CreateTask(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}

	var body struct {
		Title        string   `json:"title"`
		Body         string   `json:"body"`
		Priority     string   `json:"priority"`
		AssigneeID   string   `json:"assignee_id"`
		CreatorID    string   `json:"creator_id"`
		Dependencies []string `json:"dependencies"`
		IsScheduled  bool     `json:"is_scheduled"`
		CronExpr     string   `json:"cron_expr"`
		RepeatTimes  *int     `json:"repeat_times"`
		ProjectID    string   `json:"project_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.Title == "" {
		writeError(w, "title is required", http.StatusBadRequest)
		return
	}

	t := &domain.Task{
		Title:    body.Title,
		Body:     body.Body,
		Priority: body.Priority,
		Result:   "",
	}
	if body.AssigneeID != "" {
		t.Assignee = &domain.EmployeeBrief{ID: body.AssigneeID}
	}
	if body.CreatorID != "" {
		t.Creator = &domain.EmployeeBrief{ID: body.CreatorID}
	}
	if body.ProjectID != "" {
		if _, err := h.pgClient.GetProject(r.Context(), body.ProjectID); err != nil {
			writeError(w, "project not found: "+body.ProjectID, http.StatusBadRequest)
			return
		}
		t.ProjectID = &body.ProjectID
	}

	if body.IsScheduled {
		if body.CronExpr == "" {
			writeError(w, "cron_expr is required for scheduled tasks", http.StatusBadRequest)
			return
		}
		nextRun, err := schedule.ComputeNextRun(body.CronExpr, time.Now())
		if err != nil {
			writeError(w, "invalid schedule: "+err.Error(), http.StatusBadRequest)
			return
		}
		t.CronExpr = body.CronExpr
		t.NextRunAt = &nextRun
		t.RepeatTimes = body.RepeatTimes

		if err := h.pgClient.CreateScheduledTask(r.Context(), t); err != nil {
			writeError(w, "failed to create scheduled task: "+err.Error(), http.StatusInternalServerError)
			return
		}

		full, err := h.pgClient.GetTask(r.Context(), t.ID)
		if err != nil {
			writeJSON(w, t)
			return
		}
		if h.esClient != nil {
			if err := h.esClient.IndexTask(r.Context(), full); err != nil {
				slog.Warn("ES index scheduled task failed", "id", full.ID, "error", err)
			}
		}
		slog.Info("scheduled task created", "id", full.ID, "title", full.Title, "cron_expr", body.CronExpr)
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, full)
		return
	}

	deps := body.Dependencies
	if deps == nil {
		deps = []string{}
	}

	if err := h.pgClient.CreateTask(r.Context(), t, deps); err != nil {
		writeError(w, "failed to create task: "+err.Error(), http.StatusInternalServerError)
		return
	}

	full, err := h.pgClient.GetTask(r.Context(), t.ID)
	if err != nil {
		writeJSON(w, t)
		return
	}

	if h.esClient != nil {
		if err := h.esClient.IndexTask(r.Context(), full); err != nil {
			slog.Warn("ES index task failed", "id", full.ID, "error", err)
		}
	}

	slog.Info("task created", "id", full.ID, "title", full.Title, "status", full.Status)
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, full)
}

func (h *APIHandler) UpdateTaskFields(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}

	id := r.PathValue("id")
	var body struct {
		Title      *string `json:"title"`
		Body       *string `json:"body"`
		Priority   *string `json:"priority"`
		AssigneeID *string `json:"assignee_id"`
		Result     *string `json:"result"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.pgClient.UpdateTask(r.Context(), id, body.Title, body.Body, body.Priority, body.AssigneeID, body.Result); err != nil {
		writeError(w, "failed to update task: "+err.Error(), http.StatusInternalServerError)
		return
	}

	task, err := h.pgClient.GetTask(r.Context(), id)
	if err != nil {
		writeError(w, "task not found after update", http.StatusNotFound)
		return
	}

	if h.esClient != nil {
		if err := h.esClient.IndexTask(r.Context(), task); err != nil {
			slog.Warn("ES index task failed", "id", id, "error", err)
		}
	}

	slog.Info("task updated", "id", id)
	writeJSON(w, task)
}

func (h *APIHandler) DeleteTask(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}

	id := r.PathValue("id")
	if err := h.pgClient.DeleteTask(r.Context(), id); err != nil {
		writeError(w, "failed to delete task: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if h.esClient != nil {
		if err := h.esClient.DeleteESTask(r.Context(), id); err != nil {
			slog.Warn("ES delete task failed", "id", id, "error", err)
		}
	}

	slog.Info("task deleted", "id", id)
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *APIHandler) UpdateTaskStatus(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}

	id := r.PathValue("id")
	var body struct {
		Status   string `json:"status"`
		ActorID  string `json:"actor_id"`
		Feedback string `json:"feedback,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.pgClient.UpdateTaskStatus(r.Context(), id, body.Status, body.ActorID, body.Feedback); err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}

	task, err := h.pgClient.GetTask(r.Context(), id)
	if err != nil {
		writeError(w, "task not found after status update", http.StatusNotFound)
		return
	}
	// ES mirror is kept in sync inside UpdateTaskStatus (covers promoted dependents too).

	slog.Info("task status updated", "id", id, "status", body.Status)
	writeJSON(w, task)
}

func (h *APIHandler) ListTaskComments(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}

	id := r.PathValue("id")
	comments, err := h.pgClient.ListTaskComments(r.Context(), id)
	if err != nil {
		writeError(w, "failed to list comments: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, comments)
}

func (h *APIHandler) AddTaskComment(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}

	id := r.PathValue("id")
	var body struct {
		AuthorID string `json:"author_id"`
		Content  string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.Content == "" {
		writeError(w, "content is required", http.StatusBadRequest)
		return
	}

	comment, err := h.pgClient.AddTaskComment(r.Context(), id, body.AuthorID, body.Content)
	if err != nil {
		writeError(w, "failed to add comment: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("task comment added", "task_id", id, "comment_id", comment.ID)
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, comment)
}

func (h *APIHandler) ListTaskRuns(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}

	id := r.PathValue("id")
	runs, err := h.pgClient.ListChildTasks(r.Context(), id)
	if err != nil {
		writeError(w, "failed to list runs: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, runs)
}

func (h *APIHandler) UpdateTaskSchedule(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}

	id := r.PathValue("id")
	var body struct {
		CronExpr    *string `json:"cron_expr"`
		RepeatTimes *int    `json:"repeat_times"`
		IsScheduled *bool   `json:"is_scheduled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	var nextRun *time.Time
	if (body.IsScheduled != nil && *body.IsScheduled) || (body.CronExpr != nil && *body.CronExpr != "") {
		expr := ""
		if body.CronExpr != nil && *body.CronExpr != "" {
			expr = *body.CronExpr
		} else {
			err := h.pgClient.Pool().QueryRow(r.Context(),
				"SELECT cron_expr FROM tasks WHERE id = $1", id).Scan(&expr)
			if err != nil || expr == "" {
				writeError(w, "failed to read schedule expression", http.StatusInternalServerError)
				return
			}
		}

		t, err := schedule.ComputeNextRun(expr, time.Now())
		if err != nil {
			writeError(w, "invalid schedule: "+err.Error(), http.StatusBadRequest)
			return
		}
		nextRun = &t
	}

	if err := h.pgClient.UpdateSchedule(r.Context(), id, body.CronExpr, nextRun,
		body.RepeatTimes, body.IsScheduled); err != nil {
		writeError(w, "failed to update schedule: "+err.Error(), http.StatusInternalServerError)
		return
	}

	task, _ := h.pgClient.GetTask(r.Context(), id)
	writeJSON(w, task)
}
