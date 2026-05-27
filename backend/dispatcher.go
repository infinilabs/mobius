package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

type TaskDispatcher struct {
	pgClient       *PGClient
	esClient       *ESClient
	tokenPipeline  *TokenPipeline
	providers      *ProviderRegistry
	config         *Config
	events         *EventPipeline
	maxConcurrency int
	staleTimeout   time.Duration
	sem            chan struct{}
	wg             sync.WaitGroup
}

func NewTaskDispatcher(pg *PGClient, es *ESClient, tp *TokenPipeline, pr *ProviderRegistry, maxConcurrency int, cfg *Config, events *EventPipeline) *TaskDispatcher {
	if maxConcurrency <= 0 {
		maxConcurrency = 5
	}
	return &TaskDispatcher{
		pgClient:       pg,
		esClient:       es,
		tokenPipeline:  tp,
		providers:      pr,
		config:         cfg,
		events:         events,
		maxConcurrency: maxConcurrency,
		staleTimeout:   5 * time.Minute,
		sem:            make(chan struct{}, maxConcurrency),
	}
}

func (d *TaskDispatcher) Start(ctx context.Context) {
	dispatchTicker := time.NewTicker(15 * time.Second)
	reclaimTicker := time.NewTicker(1 * time.Minute)
	scheduleTicker := time.NewTicker(60 * time.Second)
	defer dispatchTicker.Stop()
	defer reclaimTicker.Stop()
	defer scheduleTicker.Stop()

	slog.Info("task dispatcher started", "max_concurrency", d.maxConcurrency)
	for {
		select {
		case <-dispatchTicker.C:
			d.sweepAndDispatch(ctx)
		case <-reclaimTicker.C:
			d.reclaimStaleTasks(ctx)
		case <-scheduleTicker.C:
			d.sweepScheduledTasks(ctx)
		case <-ctx.Done():
			slog.Info("task dispatcher stopping, draining running tasks...")
			d.wg.Wait()
			slog.Info("task dispatcher stopped")
			return
		}
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
		}
	}
	return tasks, nil
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
		d.pgClient.AddTaskComment(ctx, id, "", "System: Task reclaimed — execution stalled or server crashed.")

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

func (d *TaskDispatcher) executeAgentTask(parentCtx context.Context, t Task) {
	if t.Assignee == nil {
		d.failTask(parentCtx, t.ID, "task has no assignee")
		return
	}

	ctx, cancel := context.WithTimeout(parentCtx, 10*time.Minute)
	defer cancel()

	heartbeat := time.NewTicker(2 * time.Minute)
	defer heartbeat.Stop()
	go func() {
		for {
			select {
			case <-heartbeat.C:
				d.touchTask(parentCtx, t.ID)
			case <-ctx.Done():
				return
			}
		}
	}()

	slog.Info("agent task started", "task_id", t.ID, "assignee", t.Assignee.Name)

	assignee, err := d.pgClient.GetEmployee(ctx, t.Assignee.ID)
	if err != nil {
		d.failTask(ctx, t.ID, "failed to load assignee: "+err.Error())
		return
	}

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
		}
	}

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

	var messages []LLMMessage
	messages = append(messages, LLMMessage{Role: "user", Text: systemPrompt})
	messages = append(messages, LLMMessage{Role: "model", Text: fmt.Sprintf("I'm %s, %s. Ready.", assignee.Name, assignee.Title)})
	messages = append(messages, LLMMessage{Role: "user", Text: fmt.Sprintf("Please complete this task:\n\n%s", t.Body)})

	if t.Status == "needs_review" && t.Result != "" {
		messages = append(messages, LLMMessage{
			Role: "user",
			Text: fmt.Sprintf("A team member has submitted this work for your review:\n\n%s\n\nReview it carefully. Use verify_deliverable to inspect files produced, then read_project_file to check quality. Only then approve or reject.", t.Result),
		})
	}

	modelID := resolveModelID(d.config, assignee)

	provider := d.providers.ResolveProvider(modelID)
	if provider == nil {
		d.failTask(ctx, t.ID, "no provider for model: "+modelID)
		return
	}

	providerName := "gemini"
	if strings.HasPrefix(modelID, "claude-") {
		providerName = "claude"
	}
	var projectID string
	if t.ProjectID != nil {
		projectID = *t.ProjectID
	}

	tools := buildAgentTools(assignee, &t)
	llmReq := &LLMRequest{
		Model:    modelID,
		Messages: messages,
		Tools:    tools,
		OnText:   func(string) {},
		OnToolCall: func(call ToolCall) map[string]any {
			return d.dispatcherToolCall(ctx, call, assignee, &t)
		},
		OnUsage: func(usage TokenUsage) {
			if d.tokenPipeline == nil {
				return
			}
			d.tokenPipeline.Record(&bqTokenRow{
				ID:               generateID(),
				Timestamp:        time.Now().Format("2006-01-02 15:04:05.999999 UTC"),
				ModelID:          modelID,
				Provider:         providerName,
				EmployeeID:       assignee.ID,
				EmployeeName:     assignee.Name,
				ProjectID:        projectID,
				TaskID:           t.ID,
				PromptTokens:     int64(usage.PromptTokens),
				CompletionTokens: int64(usage.CompletionTokens),
				TotalTokens:      int64(usage.TotalTokens),
				CachedTokens:     int64(usage.CachedTokens),
				ThoughtsTokens:   int64(usage.ThoughtsTokens),
				ToolUseTokens:    int64(usage.ToolUseTokens),
				LatencyMs:        usage.LatencyMs,
				Status:           "success",
				Source:           "task",
			})
		},
	}

	fullResponse, err := provider.ChatStream(ctx, llmReq)
	if err != nil {
		d.failTask(ctx, t.ID, "LLM execution failed: "+err.Error())
		return
	}

	if d.esClient != nil && fullResponse != "" && len(t.Body)+len(fullResponse) > 100 {
		go absorbMemoryFromExchange(context.Background(), d.config, d.providers,
			d.esClient, t.Assignee.ID, t.Body, fullResponse, t.ID)
	}

	updated, err := d.pgClient.GetTask(ctx, t.ID)
	if err == nil && updated.Status == "in_progress" {
		d.failTask(ctx, t.ID, "agent finished without calling submit_task_result")
	}
}

func (d *TaskDispatcher) dispatcherToolCall(ctx context.Context, call ToolCall, agent *Employee, currentTask *Task) map[string]any {
	taskID := currentTask.ID
	switch call.Name {
	case "delegate_task":
		return d.execDelegateFromDispatcher(ctx, call.Args, agent, currentTask)
	case "hire_employee":
		return d.execHireFromDispatcher(ctx, call.Args, agent)
	case "submit_task_result":
		return d.execSubmitFromDispatcher(ctx, call.Args, currentTask)
	case "review_task":
		return d.execReviewFromDispatcher(ctx, call.Args, agent)
	case "verify_deliverable":
		return d.execVerifyDeliverable(ctx, call.Args)
	case "list_team":
		emp, err := d.pgClient.GetEmployee(ctx, agent.ID)
		if err != nil {
			return map[string]any{"error": err.Error()}
		}
		var members []map[string]any
		for _, r := range emp.Reports {
			members = append(members, map[string]any{"id": r.ID, "name": r.Name, "title": r.Title, "role": r.Role})
		}
		if members == nil {
			members = []map[string]any{}
		}
		return map[string]any{"team": members, "count": len(members)}
	case "store_memory":
		text, _ := call.Args["memory_text"].(string)
		if text == "" || d.esClient == nil {
			return map[string]any{"error": "memory_text is required"}
		}
		if err := d.esClient.IndexEmployeeMemoryDedup(ctx, agent.ID, taskID, text); err != nil {
			return map[string]any{"error": "failed to store memory: " + err.Error()}
		}
		if d.events != nil {
			d.events.Publish(newEvent("memory_stored",
				&agent.ID, nil, &taskID,
				map[string]any{"memory_text": truncateStr(text, 200)}))
		}
		return map[string]any{"status": "remembered", "memory_text": text}
	case "forget_memory":
		memoryID, _ := call.Args["memory_id"].(string)
		if memoryID == "" || d.esClient == nil {
			return map[string]any{"error": "memory_id is required"}
		}
		if err := d.esClient.DeleteEmployeeMemory(ctx, memoryID); err != nil {
			return map[string]any{"error": "failed to forget: " + err.Error()}
		}
		return map[string]any{"status": "forgotten", "memory_id": memoryID}
	case "write_project_file":
		return d.execWriteProjectFile(ctx, call.Args, currentTask)
	case "read_project_file":
		return d.execReadProjectFile(ctx, call.Args, currentTask)
	case "search_project_assets":
		return d.execSearchProjectAssets(ctx, call.Args, currentTask)
	case "list_project_assets":
		return d.execListProjectAssets(ctx, currentTask)
	case "run_project_command":
		return d.execRunProjectCommand(ctx, call.Args, currentTask)
	default:
		return map[string]any{"error": "unknown tool: " + call.Name}
	}
}

func (d *TaskDispatcher) execWriteProjectFile(ctx context.Context, args map[string]any, task *Task) map[string]any {
	if task.ProjectID == nil {
		return map[string]any{"error": "no project context"}
	}
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	if path == "" || content == "" {
		return map[string]any{"error": "path and content are required"}
	}
	if err := validateProjectPath(path); err != nil {
		return map[string]any{"error": err.Error()}
	}

	project, err := d.pgClient.GetProject(ctx, *task.ProjectID)
	if err != nil {
		return map[string]any{"error": "project not found"}
	}

	fullPath := filepath.Join(project.RootDir(d.config), path)
	os.MkdirAll(filepath.Dir(fullPath), 0755)
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		return map[string]any{"error": "write failed: " + err.Error()}
	}

	if d.esClient != nil {
		mimeType := "text/plain"
		ct := classifyContentType(mimeType)
		now := time.Now().Format(time.RFC3339)
		maxIdx := d.config.Projects.ContentMaxIndex
		indexContent := content
		truncated := false
		if len(content) > maxIdx {
			indexContent = content[:maxIdx]
			truncated = true
		}
		asset := &ProjectAsset{
			ID: generateID(), ProjectID: *task.ProjectID,
			Filename: filepath.Base(path), RelativePath: path,
			MIMEType: mimeType, SizeBytes: int64(len(content)),
			Content: indexContent, ContentTruncated: truncated,
			ContentType: ct, GCSStatus: "pending",
			Tags: []string{}, CreatedByID: task.Assignee.ID, TaskID: task.ID,
			CreatedAt: now, UpdatedAt: now,
		}
		d.esClient.IndexProjectAsset(ctx, asset)
	}

	if d.events != nil {
		d.events.Publish(newEvent("file_written",
			&task.Assignee.ID, task.ProjectID, &task.ID,
			map[string]any{
				"path":       path,
				"size_bytes": len(content),
			}))
	}

	return map[string]any{"status": "written", "path": path, "bytes": len(content)}
}

func (d *TaskDispatcher) execReadProjectFile(ctx context.Context, args map[string]any, task *Task) map[string]any {
	if task.ProjectID == nil {
		return map[string]any{"error": "no project context"}
	}
	path, _ := args["path"].(string)
	if path == "" {
		return map[string]any{"error": "path is required"}
	}
	if err := validateProjectPath(path); err != nil {
		return map[string]any{"error": err.Error()}
	}

	project, err := d.pgClient.GetProject(ctx, *task.ProjectID)
	if err != nil {
		return map[string]any{"error": "project not found"}
	}

	fullPath := filepath.Join(project.RootDir(d.config), path)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return map[string]any{"error": "read failed: " + err.Error()}
	}
	return map[string]any{"content": string(data), "path": path, "bytes": len(data)}
}

func (d *TaskDispatcher) execSearchProjectAssets(ctx context.Context, args map[string]any, task *Task) map[string]any {
	if task.ProjectID == nil || d.esClient == nil {
		return map[string]any{"error": "no project context or ES unavailable"}
	}
	query, _ := args["query"].(string)
	contentType, _ := args["type"].(string)
	assets, err := d.esClient.SearchProjectAssets(ctx, *task.ProjectID, query, contentType, 10)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	var results []map[string]any
	for _, a := range assets {
		results = append(results, map[string]any{
			"filename": a.Filename, "path": a.RelativePath, "type": a.ContentType, "size": a.SizeBytes,
		})
	}
	if results == nil {
		results = []map[string]any{}
	}
	return map[string]any{"results": results, "count": len(results)}
}

func (d *TaskDispatcher) execListProjectAssets(ctx context.Context, task *Task) map[string]any {
	if task.ProjectID == nil || d.esClient == nil {
		return map[string]any{"error": "no project context or ES unavailable"}
	}
	assets, err := d.esClient.SearchProjectAssets(ctx, *task.ProjectID, "", "", 100)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	var results []map[string]any
	for _, a := range assets {
		results = append(results, map[string]any{
			"filename": a.Filename, "path": a.RelativePath, "type": a.ContentType, "size": a.SizeBytes,
		})
	}
	if results == nil {
		results = []map[string]any{}
	}
	return map[string]any{"results": results, "count": len(results)}
}

const maxCommandOutput = 4000

func (d *TaskDispatcher) execRunProjectCommand(ctx context.Context, args map[string]any, task *Task) map[string]any {
	if task.ProjectID == nil {
		return map[string]any{"error": "no project context"}
	}
	command, _ := args["command"].(string)
	if command == "" {
		return map[string]any{"error": "command is required"}
	}

	project, err := d.pgClient.GetProject(ctx, *task.ProjectID)
	if err != nil {
		return map[string]any{"error": "project not found"}
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "sh", "-c", command)
	cmd.Dir = project.RootDir(d.config)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()

	out := stdout.String()
	if len(out) > maxCommandOutput {
		out = out[:maxCommandOutput] + "\n... (truncated)"
	}
	errOut := stderr.String()
	if len(errOut) > maxCommandOutput {
		errOut = errOut[:maxCommandOutput] + "\n... (truncated)"
	}

	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return map[string]any{"error": "command execution failed: " + runErr.Error()}
		}
	}

	slog.Info("project command executed", "task_id", task.ID, "command", command, "exit_code", exitCode)

	if d.events != nil {
		d.events.Publish(newEvent("command_execution",
			&task.Assignee.ID, task.ProjectID, &task.ID,
			map[string]any{
				"command":        command,
				"exit_code":      exitCode,
				"stdout_preview": truncateStr(out, 200),
				"success":        exitCode == 0,
			}))
	}

	return map[string]any{
		"stdout":    out,
		"stderr":    errOut,
		"exit_code": exitCode,
		"success":   exitCode == 0,
	}
}

func (d *TaskDispatcher) execDelegateFromDispatcher(ctx context.Context, args map[string]any, creator *Employee, currentTask *Task) map[string]any {
	assigneeID, _ := args["assignee_id"].(string)
	title, _ := args["title"].(string)
	goal, _ := args["goal"].(string)
	taskContext, _ := args["context"].(string)
	priority, _ := args["priority"].(string)

	if assigneeID == "" || title == "" || goal == "" {
		return map[string]any{"error": "assignee_id, title, and goal are required"}
	}

	assignee, err := d.pgClient.GetEmployee(ctx, assigneeID)
	if err != nil {
		return map[string]any{"error": "assignee not found: " + err.Error()}
	}
	if !canDelegate(creator, assignee) {
		return map[string]any{"error": fmt.Sprintf("cannot delegate to %s: outside team hierarchy", assignee.Name)}
	}

	body := "## Goal\n" + goal
	if taskContext != "" {
		body += "\n\n## Context\n" + taskContext
	}
	if priority == "" {
		priority = "medium"
	}

	t := &Task{
		Title:    title,
		Body:     body,
		Priority: priority,
		Creator:  &EmployeeBrief{ID: creator.ID, Name: creator.Name, Title: creator.Title, Role: creator.Role},
		Assignee: &EmployeeBrief{ID: assignee.ID, Name: assignee.Name, Title: assignee.Title, Role: assignee.Role},
	}
	projectID, _ := args["project_id"].(string)
	if projectID == "" && currentTask.ProjectID != nil {
		projectID = *currentTask.ProjectID
	}
	if projectID != "" {
		t.ProjectID = &projectID
	}
	if err := d.pgClient.CreateTask(ctx, t, nil); err != nil {
		return map[string]any{"error": "failed to create task: " + err.Error()}
	}
	if d.esClient != nil {
		if full, ferr := d.pgClient.GetTask(ctx, t.ID); ferr == nil {
			if ierr := d.esClient.IndexTask(ctx, full); ierr != nil {
				slog.Warn("ES index delegated task failed", "id", t.ID, "error", ierr)
			}
		}
	}

	if currentTask.Body != "" {
		if summary := d.summarizeForHandoff(ctx, currentTask.Body, currentTask.Result, "delegated"); summary != "" {
			d.pgClient.AddTaskComment(ctx, t.ID, "", "CONTEXT (from parent): "+summary)
		}
	}

	if d.events != nil {
		d.events.Publish(newEvent("task_delegated",
			&creator.ID, currentTask.ProjectID, &currentTask.ID,
			map[string]any{
				"child_task_id":     t.ID,
				"delegated_to_id":   assignee.ID,
				"delegated_to_name": assignee.Name,
				"title":             title,
				"priority":          priority,
			}))
	}

	slog.Info("dispatcher: task delegated", "task_id", t.ID, "from", creator.Name, "to", assignee.Name)
	return map[string]any{"status": "created", "task_id": t.ID, "assignee": assignee.Name}
}

func (d *TaskDispatcher) execHireFromDispatcher(ctx context.Context, args map[string]any, manager *Employee) map[string]any {
	if !hasTag(manager.Tags, "manager") && manager.Role != "CEO" {
		return map[string]any{"error": "only managers can hire"}
	}

	name, _ := args["name"].(string)
	title, _ := args["title"].(string)
	backstory, _ := args["backstory"].(string)
	primaryLLM, _ := args["primary_llm"].(string)

	if name == "" || title == "" || backstory == "" {
		return map[string]any{"error": "name, title, and backstory are required"}
	}

	fresh, err := d.pgClient.GetEmployee(ctx, manager.ID)
	if err != nil {
		return map[string]any{"error": "failed to load manager: " + err.Error()}
	}
	if reason, ok := checkHireDuplicate(fresh, title); !ok {
		return map[string]any{"error": reason}
	}

	emp := &Employee{
		Name: name, Title: title, Role: "Custom", Backstory: backstory,
		Models: []EmployeeModel{}, Skills: []EmployeeSkill{}, Tags: []string{},
		ManagerID: &manager.ID,
	}
	if primaryLLM != "" {
		emp.Models = append(emp.Models, EmployeeModel{ModelID: primaryLLM, Purpose: "primary_llm"})
	}
	if err := d.pgClient.CreateEmployee(ctx, emp); err != nil {
		return map[string]any{"error": "failed to hire: " + err.Error()}
	}
	if d.esClient != nil {
		if ierr := d.esClient.IndexEmployee(ctx, emp); ierr != nil {
			slog.Warn("ES index hired employee failed", "id", emp.ID, "error", ierr)
		}
	}

	if d.events != nil {
		d.events.Publish(newEvent("employee_hired",
			&manager.ID, nil, nil,
			map[string]any{
				"hired_employee_id": emp.ID,
				"name":              name,
				"title":             title,
				"manager_name":      manager.Name,
			}))
	}

	slog.Info("dispatcher: employee hired", "id", emp.ID, "name", name, "manager", manager.Name)
	return map[string]any{"status": "hired", "employee_id": emp.ID, "name": name, "reports_to": manager.Name}
}

func (d *TaskDispatcher) execSubmitFromDispatcher(ctx context.Context, args map[string]any, currentTask *Task) map[string]any {
	taskID, _ := args["task_id"].(string)
	result, _ := args["result"].(string)

	if taskID == "" || result == "" {
		return map[string]any{"error": "task_id and result are required"}
	}
	if taskID != currentTask.ID {
		return map[string]any{"error": "can only submit your own task"}
	}
	if err := d.pgClient.UpdateTask(ctx, taskID, nil, nil, nil, nil, &result); err != nil {
		return map[string]any{"error": "failed to update result: " + err.Error()}
	}
	if err := d.pgClient.UpdateTaskStatus(ctx, taskID, "needs_review", ""); err != nil {
		return map[string]any{"error": "failed to submit: " + err.Error()}
	}

	if summary := d.summarizeForHandoff(ctx, currentTask.Body, result, "submitted"); summary != "" {
		d.pgClient.AddTaskComment(ctx, taskID, "", "CONTEXT: "+summary)
	}

	if d.events != nil {
		d.events.Publish(newEvent("task_submitted",
			&currentTask.Assignee.ID, currentTask.ProjectID, &currentTask.ID,
			map[string]any{
				"result_preview": truncateStr(result, 200),
				"result_length":  len(result),
			}))
	}

	slog.Info("dispatcher: task submitted", "task_id", taskID)
	return map[string]any{"status": "submitted_for_review", "task_id": taskID}
}

func (d *TaskDispatcher) execReviewFromDispatcher(ctx context.Context, args map[string]any, reviewer *Employee) map[string]any {
	taskID, _ := args["task_id"].(string)
	action, _ := args["action"].(string)
	feedback, _ := args["feedback"].(string)

	if taskID == "" || action == "" {
		return map[string]any{"error": "task_id and action are required"}
	}

	switch action {
	case "APPROVE":
		if err := d.pgClient.UpdateTaskStatus(ctx, taskID, "done", reviewer.ID); err != nil {
			return map[string]any{"error": "failed to approve: " + err.Error()}
		}
		if d.events != nil {
			d.events.Publish(newEvent("task_approved",
				&reviewer.ID, nil, &taskID,
				map[string]any{"reviewer_name": reviewer.Name}))
		}
		return map[string]any{"status": "approved", "task_id": taskID}
	case "REJECT":
		if feedback == "" {
			return map[string]any{"error": "feedback required when rejecting"}
		}
		if err := d.pgClient.UpdateTaskStatus(ctx, taskID, "ready", reviewer.ID); err != nil {
			return map[string]any{"error": "failed to reject: " + err.Error()}
		}
		if _, err := d.pgClient.AddTaskComment(ctx, taskID, reviewer.ID, "REJECTED: "+feedback); err != nil {
			slog.Error("dispatcher: failed to add rejection comment", "task_id", taskID, "error", err)
		}
		if d.events != nil {
			d.events.Publish(newEvent("task_rejected",
				&reviewer.ID, nil, &taskID,
				map[string]any{"reviewer_name": reviewer.Name, "feedback": feedback}))
		}
		return map[string]any{"status": "rejected", "task_id": taskID, "feedback": feedback}
	default:
		return map[string]any{"error": "action must be APPROVE or REJECT"}
	}
}

func (d *TaskDispatcher) execVerifyDeliverable(ctx context.Context, args map[string]any) map[string]any {
	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return map[string]any{"error": "task_id is required"}
	}

	task, err := d.pgClient.GetTask(ctx, taskID)
	if err != nil {
		return map[string]any{"error": "task not found: " + err.Error()}
	}

	result := map[string]any{
		"task_id": taskID,
		"status":  task.Status,
		"result":  task.Result,
	}

	if d.esClient == nil {
		result["files"] = []map[string]any{}
		return result
	}

	assets, err := d.esClient.SearchAssetsByTask(ctx, taskID)
	if err != nil {
		result["files"] = []map[string]any{}
		result["files_error"] = err.Error()
		return result
	}

	var files []map[string]any
	for _, a := range assets {
		preview := a.Content
		if len(preview) > 500 {
			preview = preview[:500] + "..."
		}
		files = append(files, map[string]any{
			"filename": a.Filename,
			"path":     a.RelativePath,
			"type":     a.ContentType,
			"size":     a.SizeBytes,
			"preview":  preview,
		})
	}
	if files == nil {
		files = []map[string]any{}
	}
	result["files"] = files
	result["file_count"] = len(files)
	return result
}

func (d *TaskDispatcher) failTask(ctx context.Context, taskID, reason string) {
	slog.Error("task failed, scheduling retry", "task_id", taskID, "reason", reason)
	d.pgClient.AddTaskComment(ctx, taskID, "", "System Error: "+reason)

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
	if failures >= 3 {
		status = "blocked"
		d.pgClient.AddTaskComment(ctx, taskID, "", "System: Max retries exceeded. Task blocked.")
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
	}
}

func (d *TaskDispatcher) summarizeForHandoff(ctx context.Context, taskBody, result, event string) string {
	if d.config == nil {
		return ""
	}
	settings := d.config.GetSettings()
	modelID, _ := settings.GoogleCloud.VertexAI.DefaultLLM()
	if modelID == "" {
		return ""
	}
	provider := d.providers.ResolveProvider(modelID)
	if provider == nil {
		return ""
	}

	input := truncateForExtraction(taskBody, maxExtractionInputLen)
	output := truncateForExtraction(result, maxExtractionInputLen)

	prompt := fmt.Sprintf(`Summarize this task handoff in one concise paragraph (max 300 characters).

Event: %s
Task goal:
%s

Deliverable:
%s

Include: what was accomplished, key decisions made, and any open issues. Be specific and factual.`, event, input, output)

	req := &LLMRequest{
		Model:    modelID,
		Messages: []LLMMessage{{Role: "user", Text: prompt}},
		OnText:   func(string) {},
	}

	summary, err := provider.ChatStream(ctx, req)
	if err != nil {
		slog.Warn("handoff summarization failed", "event", event, "error", err)
		return ""
	}
	return strings.TrimSpace(summary)
}
