package main

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

type TaskDispatcher struct {
	pgClient       *PGClient
	esClient       *ESClient
	providers      *ProviderRegistry
	config         *Config
	maxConcurrency int
	staleTimeout   time.Duration
	sem            chan struct{}
	wg             sync.WaitGroup
}

func NewTaskDispatcher(pg *PGClient, es *ESClient, pr *ProviderRegistry, maxConcurrency int, cfg *Config) *TaskDispatcher {
	if maxConcurrency <= 0 {
		maxConcurrency = 5
	}
	return &TaskDispatcher{
		pgClient:       pg,
		esClient:       es,
		providers:      pr,
		config:         cfg,
		maxConcurrency: maxConcurrency,
		staleTimeout:   5 * time.Minute,
		sem:            make(chan struct{}, maxConcurrency),
	}
}

func (d *TaskDispatcher) Start(ctx context.Context) {
	dispatchTicker := time.NewTicker(15 * time.Second)
	reclaimTicker := time.NewTicker(1 * time.Minute)
	defer dispatchTicker.Stop()
	defer reclaimTicker.Stop()

	slog.Info("task dispatcher started", "max_concurrency", d.maxConcurrency)
	for {
		select {
		case <-dispatchTicker.C:
			d.sweepAndDispatch(ctx)
		case <-reclaimTicker.C:
			d.reclaimStaleTasks(ctx)
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
		tx.QueryRow(ctx, "SELECT failure_count FROM tasks WHERE id = $1", id).Scan(&failures)
		failures++

		status := "ready"
		if failures >= 3 {
			status = "blocked"
		}

		backoffSecs := 15 * (1 << uint(failures))
		retryAfter := time.Now().Add(time.Duration(backoffSecs) * time.Second)

		tx.Exec(ctx,
			"UPDATE tasks SET status = $1, failure_count = $2, retry_after = $3, updated_at = NOW() WHERE id = $4",
			status, failures, retryAfter, id)
	}

	tx.Commit(ctx)
}

func (d *TaskDispatcher) executeAgentTask(ctx context.Context, t Task) {
	if t.Assignee == nil {
		d.failTask(ctx, t.ID, "task has no assignee")
		return
	}

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

	var messages []LLMMessage
	messages = append(messages, LLMMessage{Role: "user", Text: systemPrompt})
	messages = append(messages, LLMMessage{Role: "model", Text: fmt.Sprintf("I'm %s, %s. Ready.", assignee.Name, assignee.Title)})
	messages = append(messages, LLMMessage{Role: "user", Text: fmt.Sprintf("Please complete this task:\n\n%s", t.Body)})

	modelID := "gemini-3.1-pro-preview"
	for _, m := range assignee.Models {
		if m.Purpose == "primary_llm" && m.ModelID != "" {
			modelID = m.ModelID
			break
		}
	}

	provider := d.providers.ResolveProvider(modelID)
	if provider == nil {
		d.failTask(ctx, t.ID, "no provider for model: "+modelID)
		return
	}

	tools := buildAgentTools(assignee)
	llmReq := &LLMRequest{
		Model:    modelID,
		Messages: messages,
		Tools:    tools,
		OnText:   func(string) {},
		OnToolCall: func(call ToolCall) map[string]any {
			return d.dispatcherToolCall(ctx, call, assignee, t.ID)
		},
	}

	fullResponse, err := provider.ChatStream(ctx, llmReq)
	if err != nil {
		d.failTask(ctx, t.ID, "LLM execution failed: "+err.Error())
		return
	}

	if d.esClient != nil && fullResponse != "" && len(t.Body)+len(fullResponse) > 100 {
		go d.absorbMemory(context.Background(), t.Assignee.ID, t.Body, fullResponse, t.ID)
	}

	updated, err := d.pgClient.GetTask(ctx, t.ID)
	if err == nil && updated.Status == "in_progress" {
		d.failTask(ctx, t.ID, "agent finished without calling submit_task_result")
	}
}

func (d *TaskDispatcher) dispatcherToolCall(ctx context.Context, call ToolCall, agent *Employee, taskID string) map[string]any {
	switch call.Name {
	case "delegate_task":
		return d.execDelegateFromDispatcher(ctx, call.Args, agent)
	case "hire_employee":
		return d.execHireFromDispatcher(ctx, call.Args, agent)
	case "submit_task_result":
		return d.execSubmitFromDispatcher(ctx, call.Args)
	case "review_task":
		return d.execReviewFromDispatcher(ctx, call.Args, agent)
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
	default:
		return map[string]any{"error": "unknown tool: " + call.Name}
	}
}

func (d *TaskDispatcher) execDelegateFromDispatcher(ctx context.Context, args map[string]any, creator *Employee) map[string]any {
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
	if err := d.pgClient.CreateTask(ctx, t, nil); err != nil {
		return map[string]any{"error": "failed to create task: " + err.Error()}
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

	slog.Info("dispatcher: employee hired", "id", emp.ID, "name", name, "manager", manager.Name)
	return map[string]any{"status": "hired", "employee_id": emp.ID, "name": name, "reports_to": manager.Name}
}

func (d *TaskDispatcher) execSubmitFromDispatcher(ctx context.Context, args map[string]any) map[string]any {
	taskID, _ := args["task_id"].(string)
	result, _ := args["result"].(string)

	if taskID == "" || result == "" {
		return map[string]any{"error": "task_id and result are required"}
	}
	if err := d.pgClient.UpdateTask(ctx, taskID, nil, nil, nil, nil, &result); err != nil {
		return map[string]any{"error": "failed to update result: " + err.Error()}
	}
	if err := d.pgClient.UpdateTaskStatus(ctx, taskID, "needs_review", ""); err != nil {
		return map[string]any{"error": "failed to submit: " + err.Error()}
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
		return map[string]any{"status": "rejected", "task_id": taskID, "feedback": feedback}
	default:
		return map[string]any{"error": "action must be APPROVE or REJECT"}
	}
}

func (d *TaskDispatcher) absorbMemory(ctx context.Context, employeeID, input, response, sourceID string) {
	if d.config == nil {
		return
	}
	modelID, _ := d.config.GoogleCloud.VertexAI.DefaultLLM()
	if modelID == "" {
		return
	}
	provider := d.providers.ResolveProvider(modelID)
	if provider == nil {
		return
	}

	prompt := fmt.Sprintf(`You extract concise facts from conversations.

Review this exchange:
User: %s
Assistant: %s

If a new technical decision, convention, constraint, or user preference was established, output it as a single concise sentence.
Examples:
- "We use pgx/v5 for PostgreSQL transactions in this project."
- "The user prefers CamelCase for Go struct field names."

If nothing new was decided, output exactly: NONE`, input, response)

	req := &LLMRequest{
		Model:    modelID,
		Messages: []LLMMessage{{Role: "user", Text: prompt}},
		OnText:   func(string) {},
	}

	result, err := provider.ChatStream(ctx, req)
	if err != nil || result == "" || strings.Contains(strings.ToUpper(result), "NONE") {
		return
	}

	memoryText := strings.TrimSpace(result)
	if memoryText != "" {
		d.esClient.IndexEmployeeMemoryDedup(ctx, employeeID, sourceID, memoryText)
		slog.Info("memory absorbed", "employee_id", employeeID, "memory", memoryText)
	}
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
	tx.QueryRow(ctx, "SELECT failure_count FROM tasks WHERE id = $1", taskID).Scan(&failures)
	failures++

	status := "ready"
	if failures >= 3 {
		status = "blocked"
		d.pgClient.AddTaskComment(ctx, taskID, "", "System: Max retries exceeded. Task blocked.")
	}

	backoffSecs := 15 * (1 << uint(failures))
	retryAfter := time.Now().Add(time.Duration(backoffSecs) * time.Second)

	tx.Exec(ctx,
		"UPDATE tasks SET status = $1, failure_count = $2, retry_after = $3, updated_at = NOW() WHERE id = $4",
		status, failures, retryAfter, taskID)

	tx.Commit(ctx)
}
