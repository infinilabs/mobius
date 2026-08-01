package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
)

// Task tools: task CRUD, comments, interactions, suggestions.
// Split from adapter_internal_tools.go (plan 6.5).

func (a *InternalLLMAdapter) execVerifyDeliverable(ctx context.Context, args map[string]any) map[string]any {
	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return map[string]any{"error": "task_id is required"}
	}

	task, err := a.pgClient.GetTask(ctx, taskID)
	if err != nil {
		return map[string]any{"error": "task not found: " + err.Error()}
	}

	result := map[string]any{"task_id": taskID, "status": task.Status, "result": task.Result}

	if a.esClient == nil {
		result["files"] = []map[string]any{}
		return result
	}

	assets, err := a.esClient.SearchAssetsByTask(ctx, taskID)
	if err != nil {
		result["files"] = []map[string]any{}
		result["files_error"] = err.Error()
		return result
	}

	var files []map[string]any
	for _, asset := range assets {
		preview := asset.Content
		if len(preview) > 500 {
			preview = truncateStr(preview, 500) + "..."
		}
		files = append(files, map[string]any{
			"filename": asset.Filename, "path": asset.RelativePath,
			"type": asset.ContentType, "size": asset.SizeBytes, "preview": preview,
		})
	}
	if files == nil {
		files = []map[string]any{}
	}
	result["files"] = files
	result["file_count"] = len(files)
	return result
}

func (a *InternalLLMAdapter) execListTasks(ctx context.Context, args map[string]any) map[string]any {
	status, _ := args["status"].(string)
	assigneeID, _ := args["assignee_id"].(string)
	projectID, _ := args["project_id"].(string)
	tasks, err := a.pgClient.ListTasks(ctx, status, assigneeID, projectID, "")
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	var results []map[string]any
	for _, t := range tasks {
		r := map[string]any{"id": t.ID, "title": t.Title, "status": t.Status, "priority": t.Priority}
		if t.Assignee != nil {
			r["assignee"] = t.Assignee.Name
		}
		if t.ProjectID != nil {
			r["project_id"] = *t.ProjectID
		}
		results = append(results, r)
	}
	if results == nil {
		results = []map[string]any{}
	}
	return map[string]any{"tasks": results, "count": len(results)}
}

func (a *InternalLLMAdapter) execGetTask(ctx context.Context, args map[string]any) map[string]any {
	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return map[string]any{"error": "task_id is required"}
	}
	task, err := a.pgClient.GetTask(ctx, taskID)
	if err != nil {
		return map[string]any{"error": "task not found: " + err.Error()}
	}
	return map[string]any{
		"id": task.ID, "title": task.Title, "body": task.Body,
		"status": task.Status, "priority": task.Priority, "result": task.Result,
	}
}

func (a *InternalLLMAdapter) execUpdateTask(ctx context.Context, args map[string]any) map[string]any {
	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return map[string]any{"error": "task_id is required"}
	}
	var title, body, priority, assigneeID *string
	if v, ok := args["title"].(string); ok {
		title = &v
	}
	if v, ok := args["body"].(string); ok {
		body = &v
	}
	if v, ok := args["priority"].(string); ok {
		priority = &v
	}
	if v, ok := args["assignee_id"].(string); ok {
		assigneeID = &v
	}
	if err := a.pgClient.UpdateTask(ctx, taskID, title, body, priority, assigneeID, nil); err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"status": "updated", "task_id": taskID}
}

func (a *InternalLLMAdapter) execUpdateTaskStatus(ctx context.Context, args map[string]any, agent *Employee) map[string]any {
	taskID, _ := args["task_id"].(string)
	status, _ := args["status"].(string)
	feedback, _ := args["feedback"].(string)
	if taskID == "" || status == "" {
		return map[string]any{"error": "task_id and status are required"}
	}
	if err := a.pgClient.UpdateTaskStatus(ctx, taskID, status, agent.ID, feedback); err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"status": "updated", "task_id": taskID, "new_status": status}
}

func (a *InternalLLMAdapter) execAddTaskComment(ctx context.Context, args map[string]any, agent *Employee) map[string]any {
	taskID, _ := args["task_id"].(string)
	content, _ := args["content"].(string)
	if taskID == "" || content == "" {
		return map[string]any{"error": "task_id and content are required"}
	}
	if _, err := a.pgClient.AddTaskComment(ctx, taskID, agent.ID, content); err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"status": "commented", "task_id": taskID}
}

func (a *InternalLLMAdapter) execAskUser(ctx context.Context, args map[string]any, agent *Employee, task *Task) map[string]any {
	question, _ := args["question"].(string)
	if question == "" {
		return map[string]any{"error": "question is required"}
	}
	if a.pgClient == nil {
		return map[string]any{"error": "PG unavailable"}
	}

	payload, _ := json.Marshal(map[string]any{
		"question": question,
		"options":  args["options"],
	})

	ix := &TaskInteraction{
		TaskID:            task.ID,
		CreatorEmployeeID: agent.ID,
		Kind:              "ask_user",
		Payload:           payload,
	}
	if err := a.pgClient.CreateInteraction(ctx, ix); err != nil {
		return map[string]any{"error": "failed to create interaction: " + err.Error()}
	}

	// Propagate the blocked-status failure: the caller must not be told the task
	// is paused while it is still runnable (mirrors MCP handleAskUser, M15).
	if err := a.pgClient.UpdateTaskStatus(ctx, task.ID, "blocked", ""); err != nil {
		return map[string]any{"error": "failed to block task: " + err.Error()}
	}
	if _, err := a.pgClient.AddTaskComment(ctx, task.ID, agent.ID, "Blocking question: "+question); err != nil {
		slog.Warn("failed to add blocking-question comment", "task_id", task.ID, "error", err)
	}

	slog.Info("agent asked blocking question", "task_id", task.ID, "agent", agent.Name, "question", question)
	return map[string]any{
		"status":         "waiting_for_answer",
		"interaction_id": ix.ID,
		"message":        "Your question has been posted. The task is paused until it's answered.",
	}
}

func (a *InternalLLMAdapter) execSuggestTasks(ctx context.Context, args map[string]any, agent *Employee, task *Task) map[string]any {
	tasks, _ := args["tasks"].([]any)
	rationale, _ := args["rationale"].(string)
	if len(tasks) == 0 {
		return map[string]any{"error": "tasks array is required"}
	}
	if a.pgClient == nil {
		return map[string]any{"error": "PG unavailable"}
	}

	payload, _ := json.Marshal(map[string]any{
		"tasks":     tasks,
		"rationale": rationale,
	})

	ix := &TaskInteraction{
		TaskID:            task.ID,
		CreatorEmployeeID: agent.ID,
		Kind:              "suggest_tasks",
		Payload:           payload,
	}
	if err := a.pgClient.CreateInteraction(ctx, ix); err != nil {
		return map[string]any{"error": "failed to create interaction: " + err.Error()}
	}

	a.pgClient.AddTaskComment(ctx, task.ID, agent.ID, fmt.Sprintf("Suggested %d sub-tasks for approval", len(tasks)))

	return map[string]any{
		"status":         "pending_approval",
		"interaction_id": ix.ID,
		"task_count":     len(tasks),
	}
}
