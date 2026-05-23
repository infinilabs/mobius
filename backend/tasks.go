package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type Task struct {
	ID           string         `json:"id"`
	Title        string         `json:"title"`
	Body         string         `json:"body"`
	Status       string         `json:"status"`
	Priority     string         `json:"priority"`
	Assignee     *EmployeeBrief `json:"assignee"`
	Creator      *EmployeeBrief `json:"creator"`
	Result       string         `json:"result"`
	FailureCount int            `json:"failure_count"`
	Dependencies []string       `json:"dependencies"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	CompletedAt  *time.Time     `json:"completed_at,omitempty"`
}

type TaskComment struct {
	ID        string         `json:"id"`
	TaskID    string         `json:"task_id"`
	Author    *EmployeeBrief `json:"author"`
	Content   string         `json:"content"`
	CreatedAt time.Time      `json:"created_at"`
}

// PG operations

func (pg *PGClient) ListTasks(ctx context.Context, status, assigneeID string) ([]Task, error) {
	query := `
		SELECT t.id, t.title, t.body, t.status, t.priority, t.result,
		       t.failure_count, t.created_at, t.updated_at, t.completed_at,
		       t.assignee_id, a.name, a.title, a.role,
		       t.creator_id, c.name, c.title, c.role
		FROM tasks t
		LEFT JOIN employees a ON a.id = t.assignee_id
		LEFT JOIN employees c ON c.id = t.creator_id
	`

	var conditions []string
	var args []any
	argN := 1

	if status != "" {
		conditions = append(conditions, fmt.Sprintf("t.status = $%d", argN))
		args = append(args, status)
		argN++
	}
	if assigneeID != "" {
		conditions = append(conditions, fmt.Sprintf("t.assignee_id = $%d", argN))
		args = append(args, assigneeID)
		argN++
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	query += " ORDER BY t.created_at DESC"

	rows, err := pg.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	var taskIDs []string
	var tasks []Task

	for rows.Next() {
		var t Task
		var assigneeID, assigneeName, assigneeTitle, assigneeRole *string
		var creatorID, creatorName, creatorTitle, creatorRole *string

		if err := rows.Scan(
			&t.ID, &t.Title, &t.Body, &t.Status, &t.Priority, &t.Result,
			&t.FailureCount, &t.CreatedAt, &t.UpdatedAt, &t.CompletedAt,
			&assigneeID, &assigneeName, &assigneeTitle, &assigneeRole,
			&creatorID, &creatorName, &creatorTitle, &creatorRole,
		); err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}

		if assigneeID != nil {
			t.Assignee = &EmployeeBrief{ID: *assigneeID, Name: *assigneeName, Title: *assigneeTitle, Role: *assigneeRole}
		}
		if creatorID != nil {
			t.Creator = &EmployeeBrief{ID: *creatorID, Name: *creatorName, Title: *creatorTitle, Role: *creatorRole}
		}
		t.Dependencies = []string{}
		tasks = append(tasks, t)
		taskIDs = append(taskIDs, t.ID)
	}

	if len(tasks) == 0 {
		return []Task{}, nil
	}

	depRows, err := pg.pool.Query(ctx,
		"SELECT task_id, depends_on FROM task_dependencies WHERE task_id = ANY($1)", taskIDs)
	if err != nil {
		return nil, fmt.Errorf("load dependencies: %w", err)
	}
	defer depRows.Close()

	depMap := make(map[string][]string)
	for depRows.Next() {
		var taskID, depID string
		if err := depRows.Scan(&taskID, &depID); err != nil {
			return nil, fmt.Errorf("scan dependency: %w", err)
		}
		depMap[taskID] = append(depMap[taskID], depID)
	}

	for i := range tasks {
		if deps, ok := depMap[tasks[i].ID]; ok {
			tasks[i].Dependencies = deps
		}
	}

	return tasks, nil
}

func (pg *PGClient) GetTask(ctx context.Context, id string) (*Task, error) {
	var t Task
	var assigneeID, assigneeName, assigneeTitle, assigneeRole *string
	var creatorID, creatorName, creatorTitle, creatorRole *string

	err := pg.pool.QueryRow(ctx, `
		SELECT t.id, t.title, t.body, t.status, t.priority, t.result,
		       t.failure_count, t.created_at, t.updated_at, t.completed_at,
		       t.assignee_id, a.name, a.title, a.role,
		       t.creator_id, c.name, c.title, c.role
		FROM tasks t
		LEFT JOIN employees a ON a.id = t.assignee_id
		LEFT JOIN employees c ON c.id = t.creator_id
		WHERE t.id = $1
	`, id).Scan(
		&t.ID, &t.Title, &t.Body, &t.Status, &t.Priority, &t.Result,
		&t.FailureCount, &t.CreatedAt, &t.UpdatedAt, &t.CompletedAt,
		&assigneeID, &assigneeName, &assigneeTitle, &assigneeRole,
		&creatorID, &creatorName, &creatorTitle, &creatorRole,
	)
	if err != nil {
		return nil, fmt.Errorf("get task: %w", err)
	}

	if assigneeID != nil {
		t.Assignee = &EmployeeBrief{ID: *assigneeID, Name: *assigneeName, Title: *assigneeTitle, Role: *assigneeRole}
	}
	if creatorID != nil {
		t.Creator = &EmployeeBrief{ID: *creatorID, Name: *creatorName, Title: *creatorTitle, Role: *creatorRole}
	}

	depRows, err := pg.pool.Query(ctx,
		"SELECT depends_on FROM task_dependencies WHERE task_id = $1", id)
	if err != nil {
		return nil, fmt.Errorf("load task deps: %w", err)
	}
	defer depRows.Close()

	t.Dependencies = []string{}
	for depRows.Next() {
		var depID string
		if err := depRows.Scan(&depID); err != nil {
			return nil, fmt.Errorf("scan dep: %w", err)
		}
		t.Dependencies = append(t.Dependencies, depID)
	}

	return &t, nil
}

func (pg *PGClient) CreateTask(ctx context.Context, t *Task, depIDs []string) error {
	tx, err := pg.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	status := "ready"
	if len(depIDs) > 0 {
		var undone int
		err := tx.QueryRow(ctx,
			"SELECT COUNT(*) FROM tasks WHERE id = ANY($1) AND status != 'done'",
			depIDs).Scan(&undone)
		if err != nil {
			return fmt.Errorf("check deps: %w", err)
		}
		if undone > 0 {
			status = "todo"
		}
	}

	var assigneeID, creatorID *string
	if t.Assignee != nil && t.Assignee.ID != "" {
		assigneeID = &t.Assignee.ID
	}
	if t.Creator != nil && t.Creator.ID != "" {
		creatorID = &t.Creator.ID
	}

	priority := t.Priority
	if priority == "" {
		priority = "medium"
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO tasks (title, body, status, priority, assignee_id, creator_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, updated_at
	`, t.Title, t.Body, status, priority, assigneeID, creatorID).Scan(
		&t.ID, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert task: %w", err)
	}
	t.Status = status
	t.Priority = priority

	for _, depID := range depIDs {
		var hasCycle bool
		err := tx.QueryRow(ctx, `
			WITH RECURSIVE dep_tree AS (
				SELECT depends_on FROM task_dependencies WHERE task_id = $1
				UNION
				SELECT d.depends_on FROM task_dependencies d
				JOIN dep_tree dt ON d.task_id = dt.depends_on
			)
			SELECT EXISTS(SELECT 1 FROM dep_tree WHERE depends_on = $2)
		`, depID, t.ID).Scan(&hasCycle)
		if err == nil && hasCycle {
			return fmt.Errorf("cyclic dependency detected: task %s already depends on this task", depID[:8])
		}

		if _, err := tx.Exec(ctx,
			"INSERT INTO task_dependencies (task_id, depends_on) VALUES ($1, $2)",
			t.ID, depID); err != nil {
			return fmt.Errorf("insert dep: %w", err)
		}
	}
	t.Dependencies = depIDs
	if t.Dependencies == nil {
		t.Dependencies = []string{}
	}

	return tx.Commit(ctx)
}

func (pg *PGClient) UpdateTask(ctx context.Context, id string, title, body, priority *string, assigneeID *string, result *string) error {
	sets := []string{"updated_at = NOW()"}
	args := []any{}
	argN := 1

	if title != nil {
		sets = append(sets, fmt.Sprintf("title = $%d", argN))
		args = append(args, *title)
		argN++
	}
	if body != nil {
		sets = append(sets, fmt.Sprintf("body = $%d", argN))
		args = append(args, *body)
		argN++
	}
	if priority != nil {
		sets = append(sets, fmt.Sprintf("priority = $%d", argN))
		args = append(args, *priority)
		argN++
	}
	if assigneeID != nil {
		if *assigneeID == "" {
			sets = append(sets, "assignee_id = NULL")
		} else {
			sets = append(sets, fmt.Sprintf("assignee_id = $%d", argN))
			args = append(args, *assigneeID)
			argN++
		}
	}
	if result != nil {
		sets = append(sets, fmt.Sprintf("result = $%d", argN))
		args = append(args, *result)
		argN++
	}

	if len(sets) == 1 {
		return nil
	}

	query := fmt.Sprintf("UPDATE tasks SET %s WHERE id = $%d", strings.Join(sets, ", "), argN)
	args = append(args, id)

	_, err := pg.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update task: %w", err)
	}
	return nil
}

func (pg *PGClient) DeleteTask(ctx context.Context, id string) error {
	_, err := pg.pool.Exec(ctx, "DELETE FROM tasks WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete task: %w", err)
	}
	return nil
}

var validTransitions = map[string]map[string]bool{
	"todo":         {"blocked": true},
	"ready":        {"in_progress": true, "blocked": true},
	"in_progress":  {"needs_review": true, "ready": true, "blocked": true},
	"needs_review": {"done": true, "ready": true, "blocked": true},
	"blocked":      {"ready": true},
}

func (pg *PGClient) UpdateTaskStatus(ctx context.Context, id, newStatus, actorID string) error {
	tx, err := pg.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var currentStatus, result string
	var creatorID *string
	err = tx.QueryRow(ctx, "SELECT status, result, creator_id FROM tasks WHERE id = $1 FOR UPDATE", id).Scan(&currentStatus, &result, &creatorID)
	if err != nil {
		return fmt.Errorf("get task status: %w", err)
	}

	allowed, ok := validTransitions[currentStatus]
	if !ok || !allowed[newStatus] {
		return fmt.Errorf("invalid transition from %s to %s", currentStatus, newStatus)
	}

	if newStatus == "needs_review" && result == "" {
		return fmt.Errorf("cannot submit for review without a result")
	}

	if newStatus == "done" && actorID != "" {
		isCreator := creatorID != nil && *creatorID == actorID
		if !isCreator {
			var role string
			err := tx.QueryRow(ctx, "SELECT role FROM employees WHERE id = $1", actorID).Scan(&role)
			if err != nil || role != "CEO" {
				return fmt.Errorf("unauthorized: only the task creator or the CEO can approve")
			}
		}
	}

	if newStatus == "ready" && currentStatus == "blocked" {
		var undone int
		err := tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM task_dependencies d
			JOIN tasks t ON t.id = d.depends_on
			WHERE d.task_id = $1 AND t.status != 'done'
		`, id).Scan(&undone)
		if err == nil && undone > 0 {
			newStatus = "todo"
		}
	}

	sets := "status = $1, updated_at = NOW()"
	args := []any{newStatus, id}

	if newStatus == "done" {
		sets = "status = $1, updated_at = NOW(), completed_at = NOW()"
	} else if newStatus == "ready" && currentStatus == "needs_review" {
		sets = "status = $1, updated_at = NOW(), result = ''"
	}

	_, err = tx.Exec(ctx, fmt.Sprintf("UPDATE tasks SET %s WHERE id = $2", sets), args...)
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	if newStatus == "done" {
		if err := pg.promoteDependents(ctx, tx, id); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (pg *PGClient) promoteDependents(ctx context.Context, tx pgx.Tx, doneTaskID string) error {
	rows, err := tx.Query(ctx,
		"SELECT task_id FROM task_dependencies WHERE depends_on = $1", doneTaskID)
	if err != nil {
		return fmt.Errorf("find dependents: %w", err)
	}
	defer rows.Close()

	var candidates []string
	for rows.Next() {
		var tid string
		if err := rows.Scan(&tid); err != nil {
			return fmt.Errorf("scan dependent: %w", err)
		}
		candidates = append(candidates, tid)
	}
	rows.Close()

	for _, cid := range candidates {
		var status string
		err := tx.QueryRow(ctx, "SELECT status FROM tasks WHERE id = $1", cid).Scan(&status)
		if err != nil || status != "todo" {
			continue
		}

		var undone int
		err = tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM task_dependencies d
			JOIN tasks t ON t.id = d.depends_on
			WHERE d.task_id = $1 AND t.status != 'done'
		`, cid).Scan(&undone)
		if err != nil {
			continue
		}

		if undone == 0 {
			if _, err := tx.Exec(ctx, "UPDATE tasks SET status = 'ready', updated_at = NOW() WHERE id = $1", cid); err != nil {
				return fmt.Errorf("failed to promote dependent task %s: %w", cid, err)
			}
			slog.Info("task promoted to ready", "task_id", cid)
		}
	}

	return nil
}

func (pg *PGClient) ListTaskComments(ctx context.Context, taskID string) ([]TaskComment, error) {
	rows, err := pg.pool.Query(ctx, `
		SELECT c.id, c.task_id, c.content, c.created_at,
		       c.author_id, e.name, e.title, e.role
		FROM task_comments c
		LEFT JOIN employees e ON e.id = c.author_id
		WHERE c.task_id = $1
		ORDER BY c.created_at ASC
	`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list comments: %w", err)
	}
	defer rows.Close()

	var comments []TaskComment
	for rows.Next() {
		var tc TaskComment
		var authorID, authorName, authorTitle, authorRole *string
		if err := rows.Scan(&tc.ID, &tc.TaskID, &tc.Content, &tc.CreatedAt,
			&authorID, &authorName, &authorTitle, &authorRole); err != nil {
			return nil, fmt.Errorf("scan comment: %w", err)
		}
		if authorID != nil {
			tc.Author = &EmployeeBrief{ID: *authorID, Name: *authorName, Title: *authorTitle, Role: *authorRole}
		}
		comments = append(comments, tc)
	}

	if comments == nil {
		comments = []TaskComment{}
	}
	return comments, nil
}

func (pg *PGClient) AddTaskComment(ctx context.Context, taskID, authorID, content string) (*TaskComment, error) {
	var tc TaskComment
	var aID *string
	if authorID != "" {
		aID = &authorID
	}

	err := pg.pool.QueryRow(ctx, `
		INSERT INTO task_comments (task_id, author_id, content)
		VALUES ($1, $2, $3)
		RETURNING id, task_id, created_at
	`, taskID, aID, content).Scan(&tc.ID, &tc.TaskID, &tc.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert comment: %w", err)
	}
	tc.Content = content

	if aID != nil {
		var name, title, role string
		err := pg.pool.QueryRow(ctx,
			"SELECT name, title, role FROM employees WHERE id = $1", *aID).Scan(&name, &title, &role)
		if err == nil {
			tc.Author = &EmployeeBrief{ID: *aID, Name: name, Title: title, Role: role}
		}
	}

	return &tc, nil
}

// HTTP handlers

func (h *APIHandler) ListTasks(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}

	status := r.URL.Query().Get("status")
	assigneeID := r.URL.Query().Get("assignee_id")

	tasks, err := h.pgClient.ListTasks(r.Context(), status, assigneeID)
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
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.Title == "" {
		writeError(w, "title is required", http.StatusBadRequest)
		return
	}

	t := &Task{
		Title:    body.Title,
		Body:     body.Body,
		Priority: body.Priority,
		Result:   "",
	}
	if body.AssigneeID != "" {
		t.Assignee = &EmployeeBrief{ID: body.AssigneeID}
	}
	if body.CreatorID != "" {
		t.Creator = &EmployeeBrief{ID: body.CreatorID}
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
		Status  string `json:"status"`
		ActorID string `json:"actor_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.pgClient.UpdateTaskStatus(r.Context(), id, body.Status, body.ActorID); err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}

	task, err := h.pgClient.GetTask(r.Context(), id)
	if err != nil {
		writeError(w, "task not found after status update", http.StatusNotFound)
		return
	}

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
