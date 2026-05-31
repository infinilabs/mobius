package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

type TaskDispatcher struct {
	pgClient       *PGClient
	esClient       *ESClient
	tokenPipeline  *TokenPipeline
	adapters       *AdapterRegistry
	config         *Config
	events         *EventPipeline
	maxConcurrency int
	staleTimeout   time.Duration
	sem            chan struct{}
	wg             sync.WaitGroup
}

func NewTaskDispatcher(pg *PGClient, es *ESClient, tp *TokenPipeline, adapters *AdapterRegistry, maxConcurrency int, cfg *Config, events *EventPipeline) *TaskDispatcher {
	if maxConcurrency <= 0 {
		maxConcurrency = 5
	}
	return &TaskDispatcher{
		pgClient:       pg,
		esClient:       es,
		tokenPipeline:  tp,
		adapters:       adapters,
		config:         cfg,
		events:         events,
		maxConcurrency: maxConcurrency,
		staleTimeout:   5 * time.Minute,
		sem:            make(chan struct{}, maxConcurrency),
	}
}

func (d *TaskDispatcher) Start(ctx context.Context) {
	go d.listenLoop(ctx)

	fallbackTicker := time.NewTicker(60 * time.Second)
	reclaimTicker := time.NewTicker(1 * time.Minute)
	scheduleTicker := time.NewTicker(60 * time.Second)
	retentionTicker := time.NewTicker(1 * time.Hour)
	defer fallbackTicker.Stop()
	defer reclaimTicker.Stop()
	defer scheduleTicker.Stop()
	defer retentionTicker.Stop()

	slog.Info("task dispatcher started", "mode", "listen+fallback", "max_concurrency", d.maxConcurrency)
	for {
		select {
		case <-fallbackTicker.C:
			d.sweepAndDispatch(ctx)
		case <-reclaimTicker.C:
			d.reclaimStaleTasks(ctx)
		case <-scheduleTicker.C:
			d.sweepScheduledTasks(ctx)
		case <-retentionTicker.C:
			d.pruneOldEvents(ctx)
		case <-ctx.Done():
			slog.Info("task dispatcher stopping, draining running tasks...")
			d.wg.Wait()
			slog.Info("task dispatcher stopped")
			return
		}
	}
}

func (d *TaskDispatcher) listenLoop(ctx context.Context) {
	backoff := time.Second
	maxBackoff := 30 * time.Second

	for {
		if ctx.Err() != nil {
			return
		}
		sessionStart := time.Now()
		err := d.listenSession(ctx)
		if ctx.Err() != nil {
			return
		}
		// A session that stayed up a while was healthy; reset the backoff so a
		// long-lived connection that finally drops reconnects promptly instead
		// of waiting the max interval.
		if time.Since(sessionStart) > 30*time.Second {
			backoff = time.Second
		}
		slog.Warn("LISTEN session ended, reconnecting", "error", err, "backoff", backoff)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

func (d *TaskDispatcher) listenSession(ctx context.Context) error {
	conn, err := pgx.Connect(ctx, d.pgClient.DSN())
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close(ctx)

	if _, err := conn.Exec(ctx, "LISTEN mobius_dispatch"); err != nil {
		return fmt.Errorf("LISTEN: %w", err)
	}

	slog.Info("LISTEN session established on mobius_dispatch")

	for {
		notification, err := conn.WaitForNotification(ctx)
		if err != nil {
			return fmt.Errorf("wait: %w", err)
		}

		var header struct {
			ID      int64  `json:"id"`
			Channel string `json:"channel"`
		}
		if err := json.Unmarshal([]byte(notification.Payload), &header); err != nil {
			slog.Warn("dropping malformed dispatch notification",
				"payload", notification.Payload, "error", err)
			continue
		}

		go d.processEventByID(ctx, header.ID)
	}
}

func (d *TaskDispatcher) processEventByID(ctx context.Context, eventID int64) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("PANIC recovered in dispatch event processing",
				"event_id", eventID, "panic", r, "stack", string(debug.Stack()))
		}
	}()

	// Don't start new work once shutdown has begun — otherwise a late event can
	// spawn a worker after Start()'s wg.Wait() returns, racing pool Close().
	if ctx.Err() != nil {
		return
	}

	var channel string
	var payload json.RawMessage
	err := d.pgClient.pool.QueryRow(ctx,
		"SELECT channel, payload FROM dispatch_events WHERE id = $1", eventID,
	).Scan(&channel, &payload)
	if err != nil {
		return
	}

	switch channel {
	case "task_ready":
		var evt struct {
			TaskID     string `json:"task_id"`
			AssigneeID string `json:"assignee_id"`
		}
		if json.Unmarshal(payload, &evt) == nil && evt.TaskID != "" {
			d.dispatchSingleTask(ctx, evt.TaskID)
		}
	case "interaction_resolved":
		var evt struct {
			TaskID            string `json:"task_id"`
			CreatorEmployeeID string `json:"creator_employee_id"`
		}
		if json.Unmarshal(payload, &evt) == nil && evt.TaskID != "" {
			slog.Info("interaction resolved, task may re-wake", "task_id", evt.TaskID)
		}
	}
}

func (d *TaskDispatcher) dispatchSingleTask(ctx context.Context, taskID string) {
	tx, err := d.pgClient.pool.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)

	var id string
	err = tx.QueryRow(ctx, `
		SELECT id FROM tasks
		WHERE id = $1 AND status = 'ready' AND assignee_id IS NOT NULL
		FOR UPDATE SKIP LOCKED
	`, taskID).Scan(&id)
	if err != nil {
		return
	}

	_, err = tx.Exec(ctx,
		"UPDATE tasks SET status = 'in_progress', updated_at = NOW() WHERE id = $1", id)
	if err != nil {
		return
	}
	if err := tx.Commit(ctx); err != nil {
		return
	}

	t, err := d.pgClient.GetTask(ctx, id)
	if err != nil {
		return
	}
	if d.esClient != nil {
		if err := d.esClient.IndexTask(ctx, t); err != nil {
			slog.Warn("ES index task (dispatch) failed", "id", t.ID, "error", err)
		}
	}

	select {
	case d.sem <- struct{}{}:
		d.wg.Add(1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("PANIC recovered during task execution",
						"task_id", t.ID, "panic", r, "stack", string(debug.Stack()))
					d.failTask(context.Background(), t.ID, fmt.Sprintf("internal panic: %v", r))
				}
				<-d.sem
				d.wg.Done()
			}()
			d.executeAgentTask(ctx, *t)
		}()
	case <-ctx.Done():
	}
}

func (d *TaskDispatcher) pruneOldEvents(ctx context.Context) {
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	result, err := d.pgClient.pool.Exec(ctx,
		"DELETE FROM dispatch_events WHERE created_at < $1", cutoff)
	if err != nil {
		slog.Error("dispatch event retention cleanup failed", "error", err)
		return
	}
	if result.RowsAffected() > 0 {
		slog.Info("dispatch events pruned", "deleted", result.RowsAffected())
	}
}

func (d *TaskDispatcher) sweepAndDispatch(ctx context.Context) {
	tasks, err := d.claimReadyTasks(ctx)
	if err != nil {
		slog.Error("dispatcher: failed to claim tasks", "error", err)
		return
	}

	for _, task := range tasks {
		select {
		case d.sem <- struct{}{}:
			d.wg.Add(1)
			go func(t Task) {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("PANIC recovered during task execution",
							"task_id", t.ID, "panic", r, "stack", string(debug.Stack()))
						d.failTask(context.Background(), t.ID, fmt.Sprintf("internal panic: %v", r))
					}
					<-d.sem
					d.wg.Done()
				}()
				d.executeAgentTask(ctx, t)
			}(task)
		case <-ctx.Done():
			return
		}
	}
}

func (d *TaskDispatcher) claimReadyTasks(ctx context.Context) ([]Task, error) {
	tx, err := d.pgClient.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT id FROM tasks
		WHERE status = 'ready'
		  AND assignee_id IS NOT NULL
		  AND (retry_after IS NULL OR retry_after <= NOW())
		FOR UPDATE SKIP LOCKED
		LIMIT $1
	`, d.maxConcurrency)
	if err != nil {
		return nil, fmt.Errorf("query ready tasks: %w", err)
	}
	defer rows.Close()

	var taskIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			taskIDs = append(taskIDs, id)
		}
	}
	rows.Close()

	if len(taskIDs) == 0 {
		return nil, nil
	}

	_, err = tx.Exec(ctx,
		"UPDATE tasks SET status = 'in_progress', updated_at = NOW() WHERE id = ANY($1)",
		taskIDs)
	if err != nil {
		return nil, fmt.Errorf("claim tasks: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	var tasks []Task
	for _, id := range taskIDs {
		t, err := d.pgClient.GetTask(ctx, id)
		if err == nil {
			tasks = append(tasks, *t)
			if d.esClient != nil {
				if err := d.esClient.IndexTask(ctx, t); err != nil {
					slog.Warn("ES index task (claim) failed", "id", t.ID, "error", err)
				}
			}
		}
	}
	return tasks, nil
}

func (d *TaskDispatcher) executeAgentTask(parentCtx context.Context, t Task) {
	if t.Assignee == nil {
		d.failTask(parentCtx, t.ID, "task has no assignee")
		return
	}

	assignee, err := d.pgClient.GetEmployee(parentCtx, t.Assignee.ID)
	if err != nil {
		d.failTask(parentCtx, t.ID, "failed to load assignee: "+err.Error())
		return
	}

	adapterType := AdapterType(assignee.AdapterType)
	if adapterType == "" {
		adapterType = AdapterInternal
	}

	adapter, ok := d.adapters.Get(adapterType)
	if !ok {
		d.failTask(parentCtx, t.ID, "unknown adapter type: "+string(adapterType))
		return
	}

	if assignee.MonthlyBudget != nil && *assignee.MonthlyBudget > 0 {
		if d.budgetExceeded(parentCtx, assignee) {
			d.failTask(parentCtx, t.ID, "agent paused: monthly budget exceeded")
			return
		}
	}

	hb := d.buildHeartbeatContext(parentCtx, assignee, &t)

	slog.Info("agent task started", "task_id", t.ID, "assignee", assignee.Name,
		"adapter", adapterType, "model", hb.ModelID)

	// Cap the run so a wedged adapter that never returns a terminal Observe
	// cannot hold its concurrency slot forever. On expiry monitorRun stops the
	// run and returns; the stale-task reclaimer then re-queues the DB row.
	runCtx, cancel := context.WithTimeout(parentCtx, 2*d.staleTimeout)
	defer cancel()

	runID, err := adapter.Start(runCtx, hb)
	if err != nil {
		d.failTask(parentCtx, t.ID, "adapter start failed: "+err.Error())
		return
	}

	d.monitorRun(runCtx, adapter, runID, &t)
}

func (d *TaskDispatcher) buildHeartbeatContext(ctx context.Context, assignee *Employee, t *Task) HeartbeatContext {
	systemPrompt := fmt.Sprintf("You are %s, %s. %s", assignee.Name, assignee.Title, assignee.Backstory)

	if d.esClient != nil {
		skillIDs, _ := d.pgClient.ListEmployeeSkillIDs(ctx, assignee.ID)
		for _, sid := range skillIDs {
			skill, serr := d.esClient.GetSkill(ctx, sid)
			if serr == nil {
				systemPrompt += "\n\n## Skill: " + skill.Name + "\n" + skill.Content
			}
		}
	}

	if hasTag(assignee.Tags, "manager") || assignee.Role == "CEO" {
		systemPrompt += managerDirectives()
	}

	systemPrompt += "\n\n## Project Management\n" +
		"You can create projects with the create_project tool. You will be the project owner.\n" +
		"- When the user explicitly asks to create a project, do it. Only 'name' is required — ask for it if not provided. Description and other details can be added later.\n" +
		"- When a task is complex (multi-step, multi-file, or will produce artifacts that need tracking), suggest creating a project for it. Always confirm with the user before creating."

	if t.ProjectID != nil && d.pgClient != nil {
		project, perr := d.pgClient.GetProject(ctx, *t.ProjectID)
		if perr == nil {
			mobiusMD := readProjectMemory(project, d.config)
			if mobiusMD != "" {
				injected := mobiusMD
				if len(injected) > d.config.Projects.MemoryInjectLimit {
					injected = injected[:d.config.Projects.MemoryInjectLimit]
				}
				systemPrompt += "\n\n## Project Context: " + project.Name + "\n" + injected
			}
		}
	}

	if d.esClient != nil {
		mList, _, _ := d.esClient.SearchEmployeeMemories(ctx, assignee.ID, t.Body, 3)
		if len(mList) > 0 {
			systemPrompt += "\n\n## Retrospective Learnings (your long-term memory):\n"
			for _, m := range mList {
				id := m.ID
				if len(id) > 8 {
					id = id[:8]
				}
				systemPrompt += fmt.Sprintf("- [%s] %s\n", id, m.MemoryText)
			}
			systemPrompt += "\nUse forget_memory with the ID in brackets to remove stale entries."
		}
	}

	systemPrompt += buildInteractionContext(ctx, d.pgClient, t.ID)

	comments, _ := d.pgClient.ListTaskComments(ctx, t.ID)
	if len(comments) > 0 {
		systemPrompt += "\n\n## Task History (previous review feedback — address these issues):\n"
		for _, c := range comments {
			author := "System"
			if c.Author != nil {
				author = c.Author.Name
			}
			systemPrompt += fmt.Sprintf("- [%s] %s\n", author, c.Content)
		}
	}

	modelID := resolveModelID(d.config, assignee)
	tools := buildAgentTools(assignee, t)

	var projectDir, projectName string
	if t.ProjectID != nil {
		project, perr := d.pgClient.GetProject(ctx, *t.ProjectID)
		if perr == nil {
			projectDir = project.RootDir(d.config)
			projectName = project.Name
		}
	}

	return HeartbeatContext{
		TaskID:       t.ID,
		TaskTitle:    t.Title,
		TaskBody:     t.Body,
		TaskResult:   t.Result,
		TaskStatus:   t.Status,
		ProjectID:    t.ProjectID,
		ProjectName:  projectName,
		ProjectDir:   projectDir,
		AgentID:      assignee.ID,
		AgentName:    assignee.Name,
		AgentTitle:   assignee.Title,
		AgentRole:    assignee.Role,
		SystemPrompt: systemPrompt,
		ModelID:      modelID,
		Env:          make(map[string]string),
		Comments:     comments,
		Tools:        tools,
	}
}

func (d *TaskDispatcher) monitorRun(ctx context.Context, adapter Adapter, runID string, t *Task) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			obs, err := adapter.Observe(ctx, runID)
			if err != nil {
				slog.Warn("run observation failed", "run_id", runID, "error", err)
				continue
			}
			d.touchTask(ctx, t.ID)

			switch obs.Status {
			case RunCompleted:
				updated, gerr := d.pgClient.GetTask(ctx, t.ID)
				if gerr == nil && updated.Status == "in_progress" {
					d.failTask(ctx, t.ID, "agent finished without calling submit_task_result")
				}
				return
			case RunFailed:
				d.failTask(ctx, t.ID, "run failed: "+obs.ErrorMessage)
				return
			case RunCancelled:
				return
			}
		case <-ctx.Done():
			adapter.Stop(ctx, runID)
			return
		}
	}
}

func (d *TaskDispatcher) reclaimStaleTasks(ctx context.Context) {
	tx, err := d.pgClient.pool.Begin(ctx)
	if err != nil {
		slog.Error("reclaimer: begin tx failed", "error", err)
		return
	}
	defer tx.Rollback(ctx)

	threshold := time.Now().Add(-d.staleTimeout)
	rows, err := tx.Query(ctx, `
		SELECT id FROM tasks
		WHERE status = 'in_progress' AND updated_at <= $1
		FOR UPDATE SKIP LOCKED
	`, threshold)
	if err != nil {
		slog.Error("reclaimer: query stale tasks failed", "error", err)
		return
	}
	defer rows.Close()

	var staleIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			staleIDs = append(staleIDs, id)
		}
	}
	rows.Close()

	if len(staleIDs) == 0 {
		return
	}

	slog.Warn("reclaimer: stale tasks detected", "count", len(staleIDs))

	for _, id := range staleIDs {
		var failures int
		if err := tx.QueryRow(ctx, "SELECT failure_count FROM tasks WHERE id = $1", id).Scan(&failures); err != nil {
			slog.Error("reclaimer: failed to read failure_count", "task_id", id, "error", err)
			continue
		}
		failures++

		status := "ready"
		if failures >= 3 {
			status = "blocked"
		}

		backoffSecs := 15 * (1 << min(uint(failures), 10))
		retryAfter := time.Now().Add(time.Duration(backoffSecs) * time.Second)

		if _, err := tx.Exec(ctx,
			"UPDATE tasks SET status = $1, failure_count = $2, retry_after = $3, updated_at = NOW() WHERE id = $4",
			status, failures, retryAfter, id); err != nil {
			slog.Error("reclaimer: failed to update task", "task_id", id, "error", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		slog.Error("reclaimer: commit failed", "error", err)
		return
	}

	// Audit comments go through a separate pooled connection, so add them only
	// after the reclaim is durably committed — otherwise a rollback leaves a
	// comment claiming a reclaim that never happened.
	for _, id := range staleIDs {
		d.pgClient.AddTaskComment(ctx, id, "", "System: Task reclaimed — execution stalled or server crashed.")
		d.pgClient.reindexTask(ctx, id)
	}
}

func (d *TaskDispatcher) sweepScheduledTasks(ctx context.Context) {
	type tpl struct {
		ID          string
		Title       string
		Body        string
		Priority    string
		AssigneeID  *string
		CreatorID   *string
		CronExpr    string
		RepeatTimes *int
		NextRunAt   time.Time
		ProjectID   *string
	}

	rows, err := d.pgClient.pool.Query(ctx, `
		SELECT id, title, body, priority, assignee_id, creator_id,
		       cron_expr, repeat_times, next_run_at, project_id
		FROM tasks
		WHERE is_scheduled = TRUE
		  AND next_run_at IS NOT NULL
		  AND next_run_at <= NOW()
	`)
	if err != nil {
		slog.Error("scheduler: query due templates failed", "error", err)
		return
	}
	defer rows.Close()

	var templates []tpl
	for rows.Next() {
		var t tpl
		if err := rows.Scan(&t.ID, &t.Title, &t.Body, &t.Priority,
			&t.AssigneeID, &t.CreatorID, &t.CronExpr, &t.RepeatTimes,
			&t.NextRunAt, &t.ProjectID); err == nil {
			templates = append(templates, t)
		}
	}
	rows.Close()

	if len(templates) == 0 {
		return
	}

	slog.Info("scheduler: due templates found", "count", len(templates))
	now := time.Now()

	for _, t := range templates {
		d.processTemplate(ctx, t.ID, t.Title, t.Body, t.Priority,
			t.AssigneeID, t.CreatorID, t.CronExpr, t.RepeatTimes, t.NextRunAt, t.ProjectID, now)
	}
}

func (d *TaskDispatcher) processTemplate(ctx context.Context,
	id, title, body, priority string,
	assigneeID, creatorID *string,
	cronExpr string, repeatTimes *int,
	nextRunAt time.Time, projectID *string, now time.Time) {

	tx, err := d.pgClient.pool.Begin(ctx)
	if err != nil {
		slog.Error("scheduler: begin tx failed", "template_id", id, "error", err)
		return
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, "SELECT id FROM tasks WHERE id = $1 FOR UPDATE SKIP LOCKED", id)
	if err != nil {
		return
	}

	nextRun, err := ComputeNextRun(cronExpr, now)
	if err != nil {
		slog.Error("scheduler: invalid cron expr, deactivating template",
			"id", id, "expr", cronExpr, "error", err)
		if _, err := tx.Exec(ctx, "UPDATE tasks SET is_scheduled = FALSE, next_run_at = NULL, updated_at = NOW() WHERE id = $1", id); err != nil {
			slog.Error("scheduler: failed to deactivate template", "id", id, "error", err)
		}
		tx.Commit(ctx)
		d.pgClient.AddTaskComment(ctx, id, "", "System: Invalid schedule expression. Template deactivated.")
		return
	}

	grace := GracePeriod(cronExpr)
	if now.Sub(nextRunAt) > grace && ScheduleKind(cronExpr) != "once" {
		slog.Info("scheduler: template past grace window, fast-forwarding",
			"id", id, "scheduled_at", nextRunAt, "grace_seconds", grace.Seconds())
		if _, err := tx.Exec(ctx, "UPDATE tasks SET next_run_at = $1, updated_at = NOW() WHERE id = $2", nextRun, id); err != nil {
			slog.Error("scheduler: fast-forward failed", "id", id, "error", err)
		}
		tx.Commit(ctx)
		return
	}

	isOneShot := ScheduleKind(cronExpr) == "once"

	if isOneShot {
		_, err = tx.Exec(ctx,
			"UPDATE tasks SET is_scheduled = FALSE, next_run_at = NULL, updated_at = NOW() WHERE id = $1", id)
	} else if repeatTimes != nil {
		rem := *repeatTimes - 1
		if rem <= 0 {
			_, err = tx.Exec(ctx,
				"UPDATE tasks SET is_scheduled = FALSE, repeat_times = 0, next_run_at = NULL, updated_at = NOW() WHERE id = $1", id)
		} else {
			_, err = tx.Exec(ctx,
				"UPDATE tasks SET next_run_at = $1, repeat_times = $2, updated_at = NOW() WHERE id = $3",
				nextRun, rem, id)
		}
	} else {
		_, err = tx.Exec(ctx,
			"UPDATE tasks SET next_run_at = $1, updated_at = NOW() WHERE id = $2", nextRun, id)
	}
	if err != nil {
		slog.Error("scheduler: failed to advance template", "id", id, "error", err)
		return
	}

	runTime := now.Format("2006-01-02 15:04")
	child := &Task{
		Title:    fmt.Sprintf("%s - %s", title, runTime),
		Body:     body,
		Priority: priority,
	}
	if assigneeID != nil {
		child.Assignee = &EmployeeBrief{ID: *assigneeID}
	}
	if creatorID != nil {
		child.Creator = &EmployeeBrief{ID: *creatorID}
	}
	child.ProjectID = projectID

	if err := d.pgClient.CreateChildTask(ctx, tx, child, id); err != nil {
		slog.Error("scheduler: failed to create child task", "template_id", id, "error", err)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		slog.Error("scheduler: commit failed", "template_id", id, "error", err)
		return
	}

	if d.esClient != nil {
		if err := d.esClient.IndexTask(ctx, child); err != nil {
			slog.Warn("ES index child task failed", "id", child.ID, "error", err)
		}
	}

	if repeatTimes != nil && *repeatTimes-1 <= 0 {
		d.pgClient.AddTaskComment(ctx, id, "", "System: Repeat count exhausted. Schedule deactivated.")
	}

	slog.Info("scheduler: child task materialized",
		"template", title, "child_id", child.ID, "next_run", nextRun.Format(time.RFC3339))
}

func (d *TaskDispatcher) touchTask(ctx context.Context, taskID string) {
	d.pgClient.pool.Exec(ctx,
		"UPDATE tasks SET updated_at = NOW() WHERE id = $1 AND status = 'in_progress'", taskID)
}

func (d *TaskDispatcher) failTask(ctx context.Context, taskID, reason string) {
	slog.Error("task failed, scheduling retry", "task_id", taskID, "reason", reason)

	tx, err := d.pgClient.pool.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)

	var failures int
	if err := tx.QueryRow(ctx, "SELECT failure_count FROM tasks WHERE id = $1", taskID).Scan(&failures); err != nil {
		slog.Error("failTask: task not found", "task_id", taskID, "error", err)
		return
	}
	failures++

	status := "ready"
	blocked := false
	if failures >= 3 {
		status = "blocked"
		blocked = true
	}

	backoffSecs := 15 * (1 << min(uint(failures), 10))
	retryAfter := time.Now().Add(time.Duration(backoffSecs) * time.Second)

	if _, err := tx.Exec(ctx,
		"UPDATE tasks SET status = $1, failure_count = $2, retry_after = $3, updated_at = NOW() WHERE id = $4",
		status, failures, retryAfter, taskID); err != nil {
		slog.Error("failTask: update failed", "task_id", taskID, "error", err)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		slog.Error("failTask: commit failed", "task_id", taskID, "error", err)
		return
	}

	// Audit comments use a separate pooled connection; add them only after the
	// failure/retry is durably committed so the trail never claims a rollback.
	d.pgClient.AddTaskComment(ctx, taskID, "", "System Error: "+reason)
	if blocked {
		d.pgClient.AddTaskComment(ctx, taskID, "", "System: Max retries exceeded. Task blocked.")
	}

	d.pgClient.reindexTask(ctx, taskID)
}

func (d *TaskDispatcher) budgetExceeded(ctx context.Context, agent *Employee) bool {
	if agent.MonthlyBudget == nil || *agent.MonthlyBudget <= 0 {
		return false
	}

	now := time.Now()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	var totalTokens int64
	err := d.pgClient.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(
			CASE WHEN token_usage->>'total_tokens' ~ '^[0-9]+$'
			     THEN (token_usage->>'total_tokens')::bigint
			     ELSE 0 END
		), 0)
		FROM heartbeat_runs
		WHERE agent_id = $1 AND started_at >= $2
	`, agent.ID, monthStart).Scan(&totalTokens)
	if err != nil {
		slog.Warn("budget check failed, allowing execution", "agent_id", agent.ID, "error", err)
		return false
	}

	budgetTokens := int64(*agent.MonthlyBudget) * 1000
	if totalTokens >= budgetTokens {
		slog.Warn("agent budget exceeded",
			"agent_id", agent.ID, "agent_name", agent.Name,
			"used_tokens", totalTokens, "budget_tokens", budgetTokens)
		return true
	}
	return false
}

