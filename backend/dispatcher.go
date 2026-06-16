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
	// Runs left 'active' by a previous process (a hard crash) can never be
	// finalized by their gone monitor; mark them failed so the budget ledger and
	// the active-runs view aren't polluted by ghosts.
	if n, err := d.pgClient.ReconcileOrphanedRuns(ctx); err != nil {
		slog.Warn("reconcile orphaned runs failed", "error", err)
	} else if n > 0 {
		slog.Info("reconciled orphaned heartbeat runs", "count", n)
	}

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
			d.sweepReviewTasks(ctx)
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

// sweepReviewTasks drives the autonomous quality gate: a task sitting in
// needs_review goes nowhere on its own, so its dependents stay blocked forever.
// Here the dispatcher hands each needs_review task to its creator (a manager/CEO,
// who holds verify_deliverable/review_task) to APPROVE (→ done, promoting
// dependents) or REJECT (→ ready with feedback).
func (d *TaskDispatcher) sweepReviewTasks(ctx context.Context) {
	reviews, err := d.claimReviewTasks(ctx)
	if err != nil {
		slog.Error("dispatcher: failed to claim review tasks", "error", err)
		return
	}
	for _, task := range reviews {
		select {
		case d.sem <- struct{}{}:
			d.wg.Add(1)
			go func(t Task) {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("PANIC recovered during review execution",
							"task_id", t.ID, "panic", r, "stack", string(debug.Stack()))
					}
					<-d.sem
					d.wg.Done()
				}()
				d.executeReviewTask(ctx, t)
			}(task)
		case <-ctx.Done():
			return
		}
	}
}

// claimReviewTasks selects needs_review tasks that have a creator and aren't
// already being reviewed. The task's status stays needs_review; we debounce
// re-dispatch by pushing retry_after past the run cap, so a single review run is
// in flight at a time and a crashed reviewer is retried after the window. The
// reviewer's review_task call is what finally moves the task off needs_review.
func (d *TaskDispatcher) claimReviewTasks(ctx context.Context) ([]Task, error) {
	tx, err := d.pgClient.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT id FROM tasks
		WHERE status = 'needs_review'
		  AND creator_id IS NOT NULL
		  AND (retry_after IS NULL OR retry_after <= NOW())
		FOR UPDATE SKIP LOCKED
		LIMIT $1
	`, d.maxConcurrency)
	if err != nil {
		return nil, fmt.Errorf("query review tasks: %w", err)
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

	debounce := time.Now().Add(2 * d.staleTimeout)
	if _, err := tx.Exec(ctx,
		"UPDATE tasks SET retry_after = $1 WHERE id = ANY($2)", debounce, taskIDs); err != nil {
		return nil, fmt.Errorf("debounce review tasks: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	var tasks []Task
	for _, id := range taskIDs {
		if t, err := d.pgClient.GetTask(ctx, id); err == nil {
			tasks = append(tasks, *t)
		}
	}
	return tasks, nil
}

// executeReviewTask runs the task's creator as a reviewer over a needs_review
// task. It reuses the normal agent machinery (heartbeat context + monitorRun) but
// never increments failure_count: the deliverable already succeeded, so a flaky
// reviewer must not block it — if the reviewer doesn't act, retry_after re-arms
// the next sweep. The terminal transition is owned by the review_task tool.
func (d *TaskDispatcher) executeReviewTask(parentCtx context.Context, t Task) {
	if t.Creator == nil {
		return
	}
	creator, err := d.pgClient.GetEmployee(parentCtx, t.Creator.ID)
	if err != nil {
		slog.Warn("review: failed to load creator", "task_id", t.ID, "error", err)
		return
	}
	// Only managers/CEO carry review_task/verify_deliverable. If a non-manager
	// created the task, leave it for a human; auto-approving would defeat the gate.
	if !(hasTag(creator.Tags, "manager") || creator.Role == "CEO") {
		slog.Warn("review: creator cannot review (not a manager); leaving needs_review",
			"task_id", t.ID, "creator", creator.Name)
		return
	}

	adapterType := AdapterType(creator.AdapterType)
	if adapterType == "" {
		adapterType = AdapterInternal
	}
	adapter, ok := d.adapters.Get(adapterType)
	if !ok {
		slog.Warn("review: unknown adapter type", "task_id", t.ID, "type", adapterType)
		return
	}

	hb := d.buildHeartbeatContext(parentCtx, creator, &t)
	hb.SystemPrompt += "\n\n## SYSTEM DIRECTIVE: Review this deliverable NOW\n" +
		"Task \"" + t.Title + "\" is awaiting YOUR review (status: needs_review). " +
		"Call verify_deliverable to inspect the produced work and run any tests, then call " +
		"review_task with action=\"APPROVE\" if it is correct, or action=\"REJECT\" with specific, " +
		"actionable feedback if it is not. You MUST call review_task before ending your turn — " +
		"describing your assessment without calling the tool leaves the task stuck and is not acceptable."

	slog.Info("agent review started", "task_id", t.ID, "reviewer", creator.Name,
		"adapter", adapterType, "model", hb.ModelID)

	startedAt := time.Now()
	rowID := d.openRun(t.ID, creator.ID, string(adapterType), startedAt)

	runCtx, cancel := context.WithTimeout(parentCtx, 2*d.staleTimeout)
	defer cancel()

	runID, err := adapter.Start(runCtx, hb)
	if err != nil {
		d.finishRun(rowID, RunFailed, "", "review start failed: "+err.Error(), nil)
		slog.Error("review run start failed", "task_id", t.ID, "error", err)
		return
	}

	d.monitorRun(runCtx, adapter, runID, &t, creator, rowID, startedAt, true)
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

	// Open the heartbeat_runs row up front so in-flight spend becomes visible to
	// the budget gate (including concurrent runs of the same agent) before the
	// first monitor tick. The dispatcher owns this row for the run's lifetime.
	startedAt := time.Now()
	rowID := d.openRun(t.ID, assignee.ID, string(adapterType), startedAt)

	// Cap the run so a wedged adapter that never returns a terminal Observe
	// cannot hold its concurrency slot forever. On expiry monitorRun stops the
	// run, finalizes the row, and the stale-task reclaimer re-queues the DB row.
	runCtx, cancel := context.WithTimeout(parentCtx, 2*d.staleTimeout)
	defer cancel()

	runID, err := adapter.Start(runCtx, hb)
	if err != nil {
		d.finishRun(rowID, RunFailed, "", "adapter start failed: "+err.Error(), nil)
		d.failTask(parentCtx, t.ID, "adapter start failed: "+err.Error())
		return
	}

	d.monitorRun(runCtx, adapter, runID, &t, assignee, rowID, startedAt, false)
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

// monitorRun polls a running agent every 30s. Beyond detecting terminal states
// it enforces two liveness guards that the bounded chat loop and the hard
// run-ctx cap don't cover, and keeps the heartbeat_runs row current:
//   - mid-run token ceiling: kills the run once the agent's month spend crosses
//     its budget (the pre-flight gate can't see spend accrued during a long run);
//   - stall guard: kills an LLM run that stops making progress (token-reporting
//     runs only — process adapters report no tokens and may legitimately run
//     quiet, so they rely on the run-ctx cap instead).
//
// touchTask still fires every tick so the crash-reclaimer (gated on updated_at)
// stays away while this monitor is alive; the monitor itself owns stall/budget
// kills so a still-running goroutine is never re-queued underneath it.
func (d *TaskDispatcher) monitorRun(ctx context.Context, adapter Adapter, runID string, t *Task, assignee *Employee, rowID string, startedAt time.Time, isReview bool) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	var budgetTokens int64
	if assignee.MonthlyBudget != nil && *assignee.MonthlyBudget > 0 {
		budgetTokens = int64(*assignee.MonthlyBudget) * 1000
	}

	lastOutLen, lastTokens := 0, 0
	lastProgress := startedAt

	for {
		select {
		case <-ticker.C:
			obs, err := adapter.Observe(ctx, runID)
			if err != nil {
				slog.Warn("run observation failed", "run_id", runID, "error", err)
				continue
			}

			// Flush live state first so the budget query below — and any
			// concurrent run of the same agent — sees this run's latest spend.
			d.flushRun(rowID, obs.Output, obs.ErrorMessage, obs.TokenUsage)
			d.touchTask(ctx, t.ID)

			// Mid-run token ceiling.
			if budgetTokens > 0 {
				if used, qerr := d.monthTokens(ctx, assignee.ID); qerr == nil && used >= budgetTokens {
					slog.Warn("run stopped: budget exceeded mid-run", "task_id", t.ID,
						"agent_id", assignee.ID, "used_tokens", used, "budget_tokens", budgetTokens)
					adapter.Stop(ctx, runID)
					d.finishRun(rowID, RunCancelled, obs.Output, "budget exceeded mid-run", obs.TokenUsage)
					if !isReview {
						d.failTask(ctx, t.ID, "agent paused: monthly budget exceeded mid-run")
					}
					return
				}
			}

			// Stall guard (token-reporting runs only).
			outLen := len(obs.Output)
			tokens := 0
			hasTokens := obs.TokenUsage != nil
			if hasTokens {
				tokens = int(obs.TokenUsage.TotalTokens)
			}
			if madeProgress(outLen, tokens, lastOutLen, lastTokens) {
				lastOutLen, lastTokens, lastProgress = outLen, tokens, time.Now()
			} else if shouldStallKill(hasTokens, time.Since(lastProgress), d.staleTimeout) {
				slog.Warn("run stopped: stalled (no progress)", "task_id", t.ID,
					"run_id", runID, "stalled_for", time.Since(lastProgress).String())
				adapter.Stop(ctx, runID)
				d.finishRun(rowID, RunFailed, obs.Output, "stalled: no progress for "+d.staleTimeout.String(), obs.TokenUsage)
				if !isReview {
					d.failTask(ctx, t.ID, "agent stalled: no progress for "+d.staleTimeout.String())
				}
				return
			}

			switch obs.Status {
			case RunCompleted:
				d.finishRun(rowID, RunCompleted, obs.Output, obs.ErrorMessage, obs.TokenUsage)
				updated, gerr := d.pgClient.GetTask(ctx, t.ID)
				if !isReview && gerr == nil && updated.Status == "in_progress" {
					d.failTask(ctx, t.ID, "agent finished without calling submit_task_result")
				}
				return
			case RunFailed:
				d.finishRun(rowID, RunFailed, obs.Output, obs.ErrorMessage, obs.TokenUsage)
				d.failTask(ctx, t.ID, "run failed: "+obs.ErrorMessage)
				return
			case RunCancelled:
				d.finishRun(rowID, RunCancelled, obs.Output, obs.ErrorMessage, obs.TokenUsage)
				return
			}
		case <-ctx.Done():
			// Observe before Stop: the internal adapter deletes its run entry on
			// Stop, so a post-Stop Observe would lose the final output/usage.
			obs, _ := adapter.Observe(context.Background(), runID)
			adapter.Stop(ctx, runID)
			d.finishRun(rowID, RunCancelled, obs.Output, "run cancelled (shutdown or time cap)", obs.TokenUsage)
			return
		}
	}
}

// madeProgress reports whether the run advanced since the last observation, by
// either emitting more output or consuming more tokens.
func madeProgress(outLen, tokens, lastOutLen, lastTokens int) bool {
	return outLen > lastOutLen || tokens > lastTokens
}

// shouldStallKill decides whether a non-advancing run should be killed. Only
// token-reporting (LLM-backed) runs are eligible: process adapters report no
// tokens and may legitimately produce no output for a while, so their runaway is
// bounded by the run-ctx cap rather than by progress.
func shouldStallKill(hasTokens bool, sinceProgress, timeout time.Duration) bool {
	return hasTokens && sinceProgress > timeout
}

// openRun records an 'active' heartbeat_runs row and returns its id (empty on
// failure, which the flush/finish helpers treat as a no-op). Uses a detached
// context so the write is independent of the run context's lifetime.
func (d *TaskDispatcher) openRun(taskID, agentID, adapterType string, startedAt time.Time) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	id, err := d.pgClient.StartHeartbeatRun(ctx, taskID, agentID, adapterType, startedAt)
	if err != nil {
		slog.Warn("failed to open heartbeat run", "task_id", taskID, "error", err)
		return ""
	}
	return id
}

// flushRun writes a run's current (non-terminal) output/usage to its row.
func (d *TaskDispatcher) flushRun(rowID, output, errMsg string, usage *TokenUsage) {
	if rowID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := d.pgClient.UpdateHeartbeatRun(ctx, rowID, string(RunActive), output, errMsg, usage, nil); err != nil {
		slog.Warn("failed to flush heartbeat run", "row_id", rowID, "error", err)
	}
}

// finishRun finalizes a run's row at a terminal status. Detached ctx so it
// survives a cancelled run context (shutdown / time cap).
func (d *TaskDispatcher) finishRun(rowID string, status RunStatus, output, errMsg string, usage *TokenUsage) {
	if rowID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.Now()
	if err := d.pgClient.UpdateHeartbeatRun(ctx, rowID, string(status), output, errMsg, usage, &now); err != nil {
		slog.Warn("failed to finalize heartbeat run", "row_id", rowID, "error", err)
	}
}

// monthTokens sums an agent's total_tokens recorded in heartbeat_runs since the
// start of the current month. Shared by the pre-flight gate and the mid-run
// ceiling so both read the same ledger.
func (d *TaskDispatcher) monthTokens(ctx context.Context, agentID string) (int64, error) {
	now := time.Now()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	var total int64
	err := d.pgClient.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(
			CASE WHEN token_usage->>'total_tokens' ~ '^[0-9]+$'
			     THEN (token_usage->>'total_tokens')::bigint
			     ELSE 0 END
		), 0)
		FROM heartbeat_runs
		WHERE agent_id = $1 AND started_at >= $2
	`, agentID, monthStart).Scan(&total)
	return total, err
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

	totalTokens, err := d.monthTokens(ctx, agent.ID)
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

