package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"google.golang.org/genai"
)

const maxCommandOutput = 4000

var blockedCommandPatterns = []string{
	"rm -rf /", "rm -rf ~", "mkfs", "dd if=",
	":(){", "fork bomb",
	"chmod -R 777 /", "chown -R",
	"> /dev/sd", "> /dev/null",
	"curl | sh", "wget | sh", "curl|sh", "wget|sh",
}

func validateCommand(command string) error {
	lower := strings.ToLower(command)
	for _, p := range blockedCommandPatterns {
		if strings.Contains(lower, p) {
			return fmt.Errorf("blocked command pattern: %s", p)
		}
	}
	return nil
}

func (a *InternalLLMAdapter) execDelegate(ctx context.Context, args map[string]any, creator *Employee, currentTask *Task) map[string]any {
	assigneeID, _ := args["assignee_id"].(string)
	title, _ := args["title"].(string)
	goal, _ := args["goal"].(string)
	taskContext, _ := args["context"].(string)
	priority, _ := args["priority"].(string)

	if assigneeID == "" || title == "" || goal == "" {
		return map[string]any{"error": "assignee_id, title, and goal are required"}
	}

	assignee, err := a.pgClient.GetEmployee(ctx, assigneeID)
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
	if err := a.pgClient.CreateTask(ctx, t, nil); err != nil {
		return map[string]any{"error": "failed to create task: " + err.Error()}
	}
	if a.esClient != nil {
		if full, ferr := a.pgClient.GetTask(ctx, t.ID); ferr == nil {
			if ierr := a.esClient.IndexTask(ctx, full); ierr != nil {
				slog.Warn("ES index delegated task failed", "id", t.ID, "error", ierr)
			}
		}
	}

	if currentTask.Body != "" {
		if summary := a.summarizeForHandoff(ctx, currentTask.Body, currentTask.Result, "delegated"); summary != "" {
			a.pgClient.AddTaskComment(ctx, t.ID, "", "CONTEXT (from parent): "+summary)
		}
	}

	if a.events != nil {
		a.events.Publish(newEvent("task_delegated",
			&creator.ID, currentTask.ProjectID, &currentTask.ID,
			map[string]any{
				"child_task_id":     t.ID,
				"delegated_to_id":   assignee.ID,
				"delegated_to_name": assignee.Name,
				"title":             title,
				"priority":          priority,
			}))
	}

	slog.Info("adapter: task delegated", "task_id", t.ID, "from", creator.Name, "to", assignee.Name)
	return map[string]any{"status": "created", "task_id": t.ID, "assignee": assignee.Name}
}

func (a *InternalLLMAdapter) execHire(ctx context.Context, args map[string]any, manager *Employee) map[string]any {
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

	fresh, err := a.pgClient.GetEmployee(ctx, manager.ID)
	if err != nil {
		return map[string]any{"error": "failed to load manager: " + err.Error()}
	}
	if reason, ok := checkHireDuplicate(fresh, title); !ok {
		return map[string]any{"error": reason}
	}

	emp := &Employee{
		Name: name, Title: title, Role: "Custom", Backstory: backstory,
		AdapterConfig: map[string]any{},
		Models:        []EmployeeModel{}, Skills: []EmployeeSkill{}, Tags: []string{},
		ManagerID:     &manager.ID,
	}
	if primaryLLM != "" {
		emp.Models = append(emp.Models, EmployeeModel{ModelID: primaryLLM, Purpose: "primary_llm"})
	}
	if err := a.pgClient.CreateEmployee(ctx, emp); err != nil {
		return map[string]any{"error": "failed to hire: " + err.Error()}
	}
	if a.esClient != nil {
		if ierr := a.esClient.IndexEmployee(ctx, emp); ierr != nil {
			slog.Warn("ES index hired employee failed", "id", emp.ID, "error", ierr)
		}
	}

	if a.events != nil {
		a.events.Publish(newEvent("employee_hired",
			&manager.ID, nil, nil,
			map[string]any{
				"hired_employee_id": emp.ID,
				"name":              name,
				"title":             title,
				"manager_name":      manager.Name,
			}))
	}

	slog.Info("adapter: employee hired", "id", emp.ID, "name", name, "manager", manager.Name)
	return map[string]any{"status": "hired", "employee_id": emp.ID, "name": name, "reports_to": manager.Name}
}

func (a *InternalLLMAdapter) execSubmit(ctx context.Context, args map[string]any, currentTask *Task) map[string]any {
	taskID, _ := args["task_id"].(string)
	result, _ := args["result"].(string)

	// The run is bound to exactly one task. Default to it so the agent never has
	// to discover its own opaque task ID (which it cannot see) just to submit.
	if taskID == "" {
		taskID = currentTask.ID
	}
	if result == "" {
		return map[string]any{"error": "result is required"}
	}
	if taskID != currentTask.ID {
		return map[string]any{"error": "can only submit your own task"}
	}
	if err := a.pgClient.UpdateTask(ctx, taskID, nil, nil, nil, nil, &result); err != nil {
		return map[string]any{"error": "failed to update result: " + err.Error()}
	}
	if err := a.pgClient.UpdateTaskStatus(ctx, taskID, "needs_review", ""); err != nil {
		return map[string]any{"error": "failed to submit: " + err.Error()}
	}

	if summary := a.summarizeForHandoff(ctx, currentTask.Body, result, "submitted"); summary != "" {
		a.pgClient.AddTaskComment(ctx, taskID, "", "CONTEXT: "+summary)
	}

	if a.events != nil {
		a.events.Publish(newEvent("task_submitted",
			&currentTask.Assignee.ID, currentTask.ProjectID, &currentTask.ID,
			map[string]any{
				"result_preview": truncateStr(result, 200),
				"result_length":  len(result),
			}))
	}

	slog.Info("adapter: task submitted", "task_id", taskID)
	return map[string]any{"status": "submitted_for_review", "task_id": taskID}
}

func (a *InternalLLMAdapter) execReview(ctx context.Context, args map[string]any, reviewer *Employee) map[string]any {
	taskID, _ := args["task_id"].(string)
	action, _ := args["action"].(string)
	feedback, _ := args["feedback"].(string)

	if taskID == "" || action == "" {
		return map[string]any{"error": "task_id and action are required"}
	}

	switch action {
	case "APPROVE":
		if err := a.pgClient.UpdateTaskStatus(ctx, taskID, "done", reviewer.ID); err != nil {
			return map[string]any{"error": "failed to approve: " + err.Error()}
		}
		if a.events != nil {
			a.events.Publish(newEvent("task_approved",
				&reviewer.ID, nil, &taskID,
				map[string]any{"reviewer_name": reviewer.Name}))
		}
		return map[string]any{"status": "approved", "task_id": taskID}
	case "REJECT":
		if feedback == "" {
			return map[string]any{"error": "feedback required when rejecting"}
		}
		if err := a.pgClient.UpdateTaskStatus(ctx, taskID, "ready", reviewer.ID); err != nil {
			return map[string]any{"error": "failed to reject: " + err.Error()}
		}
		if _, err := a.pgClient.AddTaskComment(ctx, taskID, reviewer.ID, "REJECTED: "+feedback); err != nil {
			slog.Error("adapter: failed to add rejection comment", "task_id", taskID, "error", err)
		}
		if a.events != nil {
			a.events.Publish(newEvent("task_rejected",
				&reviewer.ID, nil, &taskID,
				map[string]any{"reviewer_name": reviewer.Name, "feedback": feedback}))
		}
		return map[string]any{"status": "rejected", "task_id": taskID, "feedback": feedback}
	default:
		return map[string]any{"error": "action must be APPROVE or REJECT"}
	}
}

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

func (a *InternalLLMAdapter) execWriteProjectFile(ctx context.Context, args map[string]any, task *Task) map[string]any {
	if task.ProjectID == nil {
		return map[string]any{"error": "no project context"}
	}
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	if path == "" || content == "" {
		return map[string]any{"error": "path and content are required"}
	}

	project, err := a.pgClient.GetProject(ctx, *task.ProjectID)
	if err != nil {
		return map[string]any{"error": "project not found"}
	}

	fullPath, err := resolveWithinRoot(project.RootDir(a.config), path)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return map[string]any{"error": "mkdir failed: " + err.Error()}
	}
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		return map[string]any{"error": "write failed: " + err.Error()}
	}

	if a.esClient != nil {
		mimeType := "text/plain"
		ct := classifyContentType(mimeType)
		now := time.Now().Format(time.RFC3339)
		maxIdx := a.config.Projects.ContentMaxIndex
		indexContent := content
		truncated := false
		if len(content) > maxIdx {
			indexContent = content[:maxIdx]
			truncated = true
		}
		asset := &ProjectAsset{
			ID: generateID(), ProjectID: *task.ProjectID,
			Filename: filepath.Base(path), RelativePath: path, AbsolutePath: fullPath,
			MIMEType: mimeType, SizeBytes: int64(len(content)),
			Content: indexContent, ContentTruncated: truncated,
			ContentType: ct, GCSStatus: "pending",
			Tags: []string{}, CreatedByID: task.Assignee.ID, TaskID: task.ID,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := a.esClient.IndexProjectAsset(ctx, asset); err != nil {
			slog.Warn("ES index project asset failed", "path", path, "error", err)
		}
	}

	if a.events != nil {
		a.events.Publish(newEvent("file_written",
			&task.Assignee.ID, task.ProjectID, &task.ID,
			map[string]any{"path": path, "size_bytes": len(content)}))
	}

	return map[string]any{"status": "written", "path": path, "bytes": len(content)}
}

func (a *InternalLLMAdapter) execReadProjectFile(ctx context.Context, args map[string]any, task *Task) map[string]any {
	if task.ProjectID == nil {
		return map[string]any{"error": "no project context"}
	}
	path, _ := args["path"].(string)
	if path == "" {
		return map[string]any{"error": "path is required"}
	}

	project, err := a.pgClient.GetProject(ctx, *task.ProjectID)
	if err != nil {
		return map[string]any{"error": "project not found"}
	}

	fullPath, err := resolveWithinRoot(project.RootDir(a.config), path)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return map[string]any{"error": "read failed: " + err.Error()}
	}
	return map[string]any{"content": string(data), "path": path, "bytes": len(data)}
}

func (a *InternalLLMAdapter) execSearchProjectAssets(ctx context.Context, args map[string]any, task *Task) map[string]any {
	if task.ProjectID == nil || a.esClient == nil {
		return map[string]any{"error": "no project context or ES unavailable"}
	}
	query, _ := args["query"].(string)
	contentType, _ := args["type"].(string)
	assets, err := a.esClient.SearchProjectAssets(ctx, *task.ProjectID, query, contentType, 10)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	var results []map[string]any
	for _, asset := range assets {
		results = append(results, map[string]any{
			"filename": asset.Filename, "path": asset.RelativePath, "type": asset.ContentType, "size": asset.SizeBytes,
		})
	}
	if results == nil {
		results = []map[string]any{}
	}
	return map[string]any{"results": results, "count": len(results)}
}

func (a *InternalLLMAdapter) execListProjectAssets(ctx context.Context, task *Task) map[string]any {
	if task.ProjectID == nil || a.esClient == nil {
		return map[string]any{"error": "no project context or ES unavailable"}
	}
	assets, err := a.esClient.SearchProjectAssets(ctx, *task.ProjectID, "", "", 100)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	var results []map[string]any
	for _, asset := range assets {
		results = append(results, map[string]any{
			"filename": asset.Filename, "path": asset.RelativePath, "type": asset.ContentType, "size": asset.SizeBytes,
		})
	}
	if results == nil {
		results = []map[string]any{}
	}
	return map[string]any{"results": results, "count": len(results)}
}

func (a *InternalLLMAdapter) execRunProjectCommand(ctx context.Context, args map[string]any, task *Task) map[string]any {
	if task.ProjectID == nil {
		return map[string]any{"error": "no project context"}
	}
	command, _ := args["command"].(string)
	if command == "" {
		return map[string]any{"error": "command is required"}
	}
	if err := validateCommand(command); err != nil {
		slog.Warn("blocked dangerous command", "task_id", task.ID, "command", command, "reason", err)
		return map[string]any{"error": "command rejected: " + err.Error()}
	}

	project, err := a.pgClient.GetProject(ctx, *task.ProjectID)
	if err != nil {
		return map[string]any{"error": "project not found"}
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	workdir := project.RootDir(a.config)
	var out, errOut string
	var exitCode int
	var execErr error
	if a.config != nil && a.config.Sandbox.Enabled {
		out, errOut, exitCode, execErr = runSandboxedCommand(cmdCtx, a.config.Sandbox, workdir, command, nil)
	} else {
		out, errOut, exitCode, execErr = runHostCommand(cmdCtx, workdir, command, nil)
	}
	if execErr != nil {
		return map[string]any{"error": "command execution failed: " + execErr.Error()}
	}
	out = truncateOutput(out)
	errOut = truncateOutput(errOut)

	slog.Info("project command executed", "task_id", task.ID, "command", command, "exit_code", exitCode)

	if a.events != nil {
		a.events.Publish(newEvent("command_execution",
			&task.Assignee.ID, task.ProjectID, &task.ID,
			map[string]any{
				"command":        command,
				"exit_code":      exitCode,
				"stdout_preview": truncateStr(out, 200),
				"success":        exitCode == 0,
			}))
	}

	return map[string]any{
		"stdout": out, "stderr": errOut,
		"exit_code": exitCode, "success": exitCode == 0,
	}
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

func (a *InternalLLMAdapter) execListEmployees(ctx context.Context) map[string]any {
	employees, err := a.pgClient.ListEmployees(ctx)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	var results []map[string]any
	for _, e := range employees {
		r := map[string]any{"id": e.ID, "name": e.Name, "title": e.Title, "role": e.Role, "tags": e.Tags}
		if e.ManagerID != nil {
			r["manager_id"] = *e.ManagerID
		}
		results = append(results, r)
	}
	if results == nil {
		results = []map[string]any{}
	}
	return map[string]any{"employees": results, "count": len(results)}
}

func (a *InternalLLMAdapter) execGetEmployee(ctx context.Context, args map[string]any) map[string]any {
	empID, _ := args["employee_id"].(string)
	if empID == "" {
		return map[string]any{"error": "employee_id is required"}
	}
	emp, err := a.pgClient.GetEmployee(ctx, empID)
	if err != nil {
		return map[string]any{"error": "employee not found: " + err.Error()}
	}
	return map[string]any{
		"id": emp.ID, "name": emp.Name, "title": emp.Title, "role": emp.Role,
		"backstory": emp.Backstory, "skills": emp.Skills, "models": emp.Models,
		"tags": emp.Tags, "reports": emp.Reports,
	}
}

func (a *InternalLLMAdapter) execUpdateEmployee(ctx context.Context, args map[string]any) map[string]any {
	empID, _ := args["employee_id"].(string)
	if empID == "" {
		return map[string]any{"error": "employee_id is required"}
	}
	emp, err := a.pgClient.GetEmployee(ctx, empID)
	if err != nil {
		return map[string]any{"error": "employee not found: " + err.Error()}
	}
	if v, ok := args["title"].(string); ok {
		emp.Title = v
	}
	if v, ok := args["backstory"].(string); ok {
		emp.Backstory = v
	}
	if v, ok := args["tags"].([]any); ok {
		tags := make([]string, 0, len(v))
		for _, t := range v {
			if s, ok := t.(string); ok {
				tags = append(tags, s)
			}
		}
		emp.Tags = tags
	}
	if err := a.pgClient.UpdateEmployee(ctx, empID, emp); err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"status": "updated", "employee_id": empID}
}

func (a *InternalLLMAdapter) execListProjects(ctx context.Context) map[string]any {
	projects, err := a.pgClient.ListProjects(ctx, "")
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	var results []map[string]any
	for _, p := range projects {
		r := map[string]any{"id": p.ID, "name": p.Name, "status": p.Status, "task_count": p.TaskCount, "asset_count": p.AssetCount}
		if p.Owner != nil {
			r["owner"] = p.Owner.Name
		}
		results = append(results, r)
	}
	if results == nil {
		results = []map[string]any{}
	}
	return map[string]any{"projects": results, "count": len(results)}
}

func (a *InternalLLMAdapter) execCreateProject(ctx context.Context, args map[string]any, agent *Employee) map[string]any {
	name, _ := args["name"].(string)
	description, _ := args["description"].(string)
	if name == "" {
		return map[string]any{"error": "name is required"}
	}
	p, err := a.pgClient.CreateProject(ctx, CreateProjectInput{
		Name:        name,
		Description: description,
		OwnerID:     agent.ID,
	}, a.config)
	if err != nil {
		return map[string]any{"error": "failed to create project: " + err.Error()}
	}
	return map[string]any{"status": "created", "project_id": p.ID, "name": name}
}

func (a *InternalLLMAdapter) execUpdateProject(ctx context.Context, args map[string]any) map[string]any {
	projectID, _ := args["project_id"].(string)
	if projectID == "" {
		return map[string]any{"error": "project_id is required"}
	}
	var description, status *string
	if v, ok := args["description"].(string); ok {
		description = &v
	}
	if v, ok := args["status"].(string); ok {
		status = &v
	}
	if err := a.pgClient.UpdateProject(ctx, projectID, nil, description, status); err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"status": "updated", "project_id": projectID}
}

func (a *InternalLLMAdapter) execListPrompts(ctx context.Context, args map[string]any) map[string]any {
	if a.esClient == nil {
		return map[string]any{"error": "ES unavailable"}
	}
	query, _ := args["query"].(string)
	prompts, err := a.esClient.SearchPrompts(ctx, query)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	var results []map[string]any
	for _, p := range prompts {
		results = append(results, map[string]any{"id": p.ID, "title": p.Title, "tags": p.Tags})
	}
	if results == nil {
		results = []map[string]any{}
	}
	return map[string]any{"prompts": results, "count": len(results)}
}

func (a *InternalLLMAdapter) execCreatePrompt(ctx context.Context, args map[string]any) map[string]any {
	if a.esClient == nil {
		return map[string]any{"error": "ES unavailable"}
	}
	title, _ := args["title"].(string)
	content, _ := args["content"].(string)
	if title == "" || content == "" {
		return map[string]any{"error": "title and content are required"}
	}
	var tags []string
	if t, ok := args["tags"].([]any); ok {
		for _, v := range t {
			if s, ok := v.(string); ok {
				tags = append(tags, s)
			}
		}
	}
	if tags == nil {
		tags = []string{}
	}
	p := &Prompt{Title: title, Content: content, Tags: tags}
	if err := a.esClient.IndexPrompt(ctx, p); err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"status": "created", "prompt_id": p.ID}
}

func (a *InternalLLMAdapter) execUpdatePrompt(ctx context.Context, args map[string]any) map[string]any {
	if a.esClient == nil {
		return map[string]any{"error": "ES unavailable"}
	}
	promptID, _ := args["prompt_id"].(string)
	if promptID == "" {
		return map[string]any{"error": "prompt_id is required"}
	}
	p, err := a.esClient.GetPrompt(ctx, promptID)
	if err != nil {
		return map[string]any{"error": "prompt not found"}
	}
	if v, ok := args["title"].(string); ok {
		p.Title = v
	}
	if v, ok := args["content"].(string); ok {
		p.Content = v
	}
	if t, ok := args["tags"].([]any); ok {
		tags := make([]string, 0, len(t))
		for _, v := range t {
			if s, ok := v.(string); ok {
				tags = append(tags, s)
			}
		}
		p.Tags = tags
	}
	if err := a.esClient.IndexPrompt(ctx, p); err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"status": "updated", "prompt_id": promptID}
}

func (a *InternalLLMAdapter) execDeletePrompt(ctx context.Context, args map[string]any) map[string]any {
	if a.esClient == nil {
		return map[string]any{"error": "ES unavailable"}
	}
	promptID, _ := args["prompt_id"].(string)
	if promptID == "" {
		return map[string]any{"error": "prompt_id is required"}
	}
	if err := a.esClient.DeletePrompt(ctx, promptID); err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"status": "deleted", "prompt_id": promptID}
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

func (a *InternalLLMAdapter) execListSkills(ctx context.Context, args map[string]any) map[string]any {
	if a.esClient == nil {
		return map[string]any{"error": "ES unavailable"}
	}
	query, _ := args["query"].(string)
	skills, err := a.esClient.SearchSkills(ctx, query)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	var results []map[string]any
	for _, s := range skills {
		results = append(results, map[string]any{"id": s.ID, "name": s.Name, "category": s.Category, "tags": s.Tags})
	}
	if results == nil {
		results = []map[string]any{}
	}
	return map[string]any{"skills": results, "count": len(results)}
}

func (a *InternalLLMAdapter) execAssignSkill(ctx context.Context, args map[string]any) map[string]any {
	empID, _ := args["employee_id"].(string)
	skillID, _ := args["skill_id"].(string)
	if empID == "" || skillID == "" {
		return map[string]any{"error": "employee_id and skill_id are required"}
	}
	if a.pgClient == nil {
		return map[string]any{"error": "PG unavailable"}
	}
	if err := a.pgClient.AssignSkill(ctx, empID, skillID); err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"status": "assigned", "employee_id": empID, "skill_id": skillID}
}
func (a *InternalLLMAdapter) execUnassignSkill(ctx context.Context, args map[string]any) map[string]any {
	empID, _ := args["employee_id"].(string)
	skillID, _ := args["skill_id"].(string)
	if empID == "" || skillID == "" {
		return map[string]any{"error": "employee_id and skill_id are required"}
	}
	if a.pgClient == nil {
		return map[string]any{"error": "PG unavailable"}
	}
	if err := a.pgClient.UnassignSkill(ctx, empID, skillID); err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"status": "unassigned", "employee_id": empID, "skill_id": skillID}
}

// ─── Playable Ads Tool Executors & Implementations ──────────────────────────

// resolvePlayableProjectID resolves the active project for a playable tool call.
// The autonomous run path supplies a *Task; the interactive chat path injects
// _project_id into the tool args (see chat.go). Both routers call this helper so
// they share one resolution rule.
func resolvePlayableProjectID(task *Task, args map[string]any) string {
	if task != nil && task.ProjectID != nil && *task.ProjectID != "" {
		return *task.ProjectID
	}
	if pid, ok := args["_project_id"].(string); ok && pid != "" {
		return pid
	}
	if pid, ok := args["project_id"].(string); ok && pid != "" {
		return pid
	}
	return ""
}

func execPlayableLoadReferenceGameTool(config *Config, args map[string]any) map[string]any {
	gameType, _ := args["game_type"].(string)
	if gameType == "" {
		return map[string]any{"error": "game_type is required"}
	}

	baseDir := "templates"
	if len(config.Projects.TemplateDirs) > 0 {
		baseDir = config.Projects.TemplateDirs[0]
	}

	content, err := loadReferenceGameImpl(baseDir, gameType)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"html_template": content}
}

func execPlayableGetTrackingSDKTool() map[string]any {
	sdkPath := filepath.Join("static", "tracking", "sdk.js")
	content, err := getTrackingSDKImpl(sdkPath)
	if err != nil {
		content, err = getTrackingSDKImpl(filepath.Join("backend", sdkPath))
		if err != nil {
			return map[string]any{"error": "failed to load tracking SDK: " + err.Error()}
		}
	}
	return map[string]any{"tracking_sdk": content}
}

func execPlayableGetWebAudioSFXTool() map[string]any {
	return map[string]any{"web_audio_sfx_helpers": getWebAudioSFXImpl()}
}

func execPlayableWriteHTMLTool(ctx context.Context, config *Config, pg *PGClient, projectID string, args map[string]any) map[string]any {
	htmlContent, _ := args["html_content"].(string)
	pipelineID, _ := args["pipeline_id"].(string)
	if htmlContent == "" || pipelineID == "" {
		return map[string]any{"error": "html_content and pipeline_id are required"}
	}
	if projectID == "" {
		return map[string]any{"error": "project context required"}
	}

	projectDir := playableProjectDir(ctx, pg, config, projectID)

	valScript := filepath.Join("static", "validation", "playwright_validation.js")
	if _, err := os.Stat(valScript); os.IsNotExist(err) {
		valScript = filepath.Join("backend", valScript)
	}

	report, err := writeHTMLImpl(projectDir, pipelineID, htmlContent, valScript)
	if err != nil {
		return map[string]any{"error": "compile failed: " + err.Error()}
	}

	return map[string]any{
		"status":              "compiled",
		"passed":              report.Passed,
		"errors":              report.Errors,
		"preview_inline_path": filepath.Join("output", pipelineID, "preview_inline.html"),
	}
}

// execSaveUploadToAssetsTool copies a file the user uploaded in chat into the project's
// asset library and indexes it, so an uploaded image can become part of a playable ad.
// srcFile is the most recent chat upload, resolved by the caller from the conversation;
// it is nil in autonomous (non-chat) runs, where there is no upload to save.
func execSaveUploadToAssetsTool(ctx context.Context, gcs *GCSClient, es *ESClient, pg *PGClient, config *Config, projectID string, srcFile *FileRef, args map[string]any) map[string]any {
	if es == nil || pg == nil {
		return map[string]any{"error": "required services not available"}
	}
	if projectID == "" {
		return map[string]any{"error": "project context required"}
	}
	if srcFile == nil {
		return map[string]any{"error": "no uploaded file found in this conversation to add to assets"}
	}

	project, err := pg.GetProject(ctx, projectID)
	if err != nil {
		return map[string]any{"error": "project not found: " + err.Error()}
	}

	relativePath, _ := args["relative_path"].(string)
	if relativePath == "" {
		name := filepath.Base(srcFile.Name)
		if name == "" || name == "." || name == "/" {
			name = srcFile.ID
		}
		relativePath = filepath.Join("assets", name)
	}

	root := project.RootDir(config)
	fullPath, err := resolveWithinRoot(root, relativePath)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return map[string]any{"error": "failed to create asset dir: " + err.Error()}
	}

	// Fetch the uploaded bytes into the project from GCS or the local uploads dir.
	if srcFile.GCSURI != "" && gcs != nil {
		if err := gcs.DownloadURI(ctx, srcFile.GCSURI, fullPath); err != nil {
			return map[string]any{"error": "failed to fetch uploaded file from storage: " + err.Error()}
		}
	} else {
		localSrc := filepath.Join(uploadsDir, srcFile.ID+filepath.Ext(srcFile.Name))
		data, rerr := os.ReadFile(localSrc)
		if rerr != nil {
			return map[string]any{"error": "uploaded file is no longer available: " + rerr.Error()}
		}
		if werr := os.WriteFile(fullPath, data, 0644); werr != nil {
			return map[string]any{"error": "failed to write asset: " + werr.Error()}
		}
	}

	buf, err := os.ReadFile(fullPath)
	if err != nil {
		buf = []byte{}
	}

	mimeType := srcFile.MIMEType
	if mimeType == "" {
		mimeType = resolveMimeType(srcFile.Name, "")
	}
	ct := classifyContentType(mimeType)
	var content string
	var truncated bool
	if isTextIndexable(ct) && len(buf) > 0 {
		maxIdx := config.Projects.ContentMaxIndex
		if len(buf) > maxIdx {
			content = string(buf[:maxIdx])
			truncated = true
		} else {
			content = string(buf)
		}
	}

	var tags []string
	if v, ok := args["tags"].([]any); ok {
		for _, t := range v {
			if s, ok := t.(string); ok {
				tags = append(tags, s)
			}
		}
	}
	if len(tags) == 0 {
		tags = []string{"uploaded"}
	}

	now := time.Now().Format(time.RFC3339)
	asset := &ProjectAsset{
		ID:               generateID(),
		ProjectID:        projectID,
		Filename:         filepath.Base(relativePath),
		RelativePath:     relativePath,
		MIMEType:         mimeType,
		SizeBytes:        int64(len(buf)),
		Content:          content,
		ContentTruncated: truncated,
		ContentType:      ct,
		GCSStatus:        "pending",
		Checksum:         calculateSHA256(buf),
		Tags:             tags,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := es.IndexProjectAsset(ctx, asset); err != nil {
		return map[string]any{"error": "failed to index asset: " + err.Error()}
	}
	if gcs != nil {
		go uploadAssetToGCS(config, gcs, es, project, asset.ID, fullPath, relativePath)
	}

	slog.Info("chat upload saved to project assets", "project", project.Name, "path", relativePath, "asset_id", asset.ID)
	return map[string]any{
		"status":        "success",
		"asset_id":      asset.ID,
		"relative_path": relativePath,
		"content_type":  ct,
		"tags":          tags,
	}
}

// playableProjectDir resolves the on-disk working directory for a playable project.
// It uses the project's canonical RootDir (keyed by Name) so generated and published
// files land where ReindexProjectAssets and GCS sync expect them. Falls back to
// ProjectsDir/<projectID> only when the project cannot be resolved (e.g. no PG client).
func playableProjectDir(ctx context.Context, pg *PGClient, config *Config, projectID string) string {
	if pg != nil {
		if project, err := pg.GetProject(ctx, projectID); err == nil {
			return project.RootDir(config)
		}
	}
	return filepath.Join(config.Projects.ProjectsDir, projectID)
}

func execGenerateImageTool(ctx context.Context, config *Config, providers *ProviderRegistry, gcs *GCSClient, es *ESClient, pg *PGClient, projectID string, args map[string]any) map[string]any {
	prompt, _ := args["prompt"].(string)
	size, _ := args["size"].(string)
	outPath, _ := args["output_path"].(string)

	if prompt == "" || outPath == "" {
		return map[string]any{"error": "prompt and output_path are required"}
	}
	if projectID == "" {
		return map[string]any{"error": "project context required"}
	}
	if size == "" {
		size = "512x512"
	}

	// Resolve the canonical project so generated files land under RootDir (reindexable +
	// GCS-synced). Fall back to ProjectsDir/<id> only when the project can't be resolved.
	var project *Project
	projectDir := filepath.Join(config.Projects.ProjectsDir, projectID)
	if pg != nil {
		if p, perr := pg.GetProject(ctx, projectID); perr == nil {
			project = p
			projectDir = p.RootDir(config)
		}
	}
	absOutPath := filepath.Join(projectDir, outPath)

	os.MkdirAll(filepath.Dir(absOutPath), 0755)

	err := generateImageAsset(ctx, config, providers, prompt, size, absOutPath)
	if err != nil {
		return map[string]any{"error": "image generation failed: " + err.Error()}
	}

	transparent, _ := args["transparent"].(bool)
	if transparent {
		if perr := postProcessSprite(absOutPath); perr != nil {
			slog.Warn("sprite post-processing failed; keeping raw image", "path", absOutPath, "error", perr)
		}
	}

	// Register the generated image in the asset library (best-effort) so it appears in the
	// project AssetsTab and Creatives immediately. The file is also under RootDir, so a
	// reindex would recover it even if indexing here fails.
	if es != nil {
		registerGeneratedImageAsset(ctx, gcs, es, config, project, projectID, outPath, absOutPath, transparent)
	}

	return map[string]any{"status": "success", "output_path": outPath}
}

// registerGeneratedImageAsset indexes a freshly generated image as a ProjectAsset and
// kicks off async GCS sync, mirroring UploadProjectAsset.
func registerGeneratedImageAsset(ctx context.Context, gcs *GCSClient, es *ESClient, config *Config, project *Project, projectID, relPath, absPath string, transparent bool) {
	relativePath := filepath.Clean(relPath)
	buf, rerr := os.ReadFile(absPath)
	if rerr != nil {
		slog.Warn("generated image unreadable for asset registration", "path", absPath, "error", rerr)
		return
	}

	mimeType := resolveMimeType(relativePath, "")
	tags := []string{"ai_generated"}
	if transparent {
		tags = append(tags, "playable_asset")
	}

	now := time.Now().Format(time.RFC3339)
	asset := &ProjectAsset{
		ID:           generateID(),
		ProjectID:    projectID,
		Filename:     filepath.Base(relativePath),
		RelativePath: relativePath,
		MIMEType:     mimeType,
		SizeBytes:    int64(len(buf)),
		ContentType:  classifyContentType(mimeType),
		GCSStatus:    "pending",
		Checksum:     calculateSHA256(buf),
		Tags:         tags,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := es.IndexProjectAsset(ctx, asset); err != nil {
		slog.Warn("failed to index generated image asset", "path", relativePath, "error", err)
		return
	}
	if gcs != nil && project != nil {
		go uploadAssetToGCS(config, gcs, es, project, asset.ID, absPath, relativePath)
	}
	slog.Info("generated image registered as asset", "project_id", projectID, "path", relativePath, "asset_id", asset.ID)
}

func execGenerateAudioTool(ctx context.Context, config *Config, pg *PGClient, projectID string, args map[string]any) map[string]any {
	prompt, _ := args["prompt"].(string)
	duration, _ := args["duration_sec"].(float64)
	outPath, _ := args["output_path"].(string)

	if prompt == "" || outPath == "" {
		return map[string]any{"error": "prompt and output_path are required"}
	}
	if projectID == "" {
		return map[string]any{"error": "project context required"}
	}
	if duration <= 0 {
		duration = 5
	}

	projectDir := playableProjectDir(ctx, pg, config, projectID)
	absOutPath := filepath.Join(projectDir, outPath)

	os.MkdirAll(filepath.Dir(absOutPath), 0755)

	err := generateAudioAsset(ctx, prompt, int(duration), absOutPath)
	if err != nil {
		return map[string]any{"error": "audio generation failed: " + err.Error()}
	}

	return map[string]any{"status": "success", "output_path": outPath}
}

func execPublishPlayableAdTool(ctx context.Context, gcs *GCSClient, es *ESClient, config *Config, pg *PGClient, projectID string, args map[string]any) map[string]any {
	pipelineID, _ := args["pipeline_id"].(string)
	if pipelineID == "" {
		return map[string]any{"error": "pipeline_id is required"}
	}
	if projectID == "" {
		return map[string]any{"error": "project context required"}
	}

	publishToGCS, _ := args["publish_to_gcs"].(bool)

	projectDir := playableProjectDir(ctx, pg, config, projectID)

	url, gcsURI, err := publishPlayableAdImpl(ctx, gcs, config, projectDir, pipelineID, publishToGCS)
	if err != nil {
		return map[string]any{"error": "publish failed: " + err.Error()}
	}

	// Register the asset in Elasticsearch (creatives library)
	if es != nil {
		outDir := filepath.Join(projectDir, "output", pipelineID)
		inlinePath := filepath.Join(outDir, "preview_inline.html")

		var sizeBytes int64
		var content string
		var checksum string

		if data, err := os.ReadFile(inlinePath); err == nil {
			sizeBytes = int64(len(data))
			checksum = calculateSHA256(data)

			maxIdx := config.Projects.ContentMaxIndex
			if maxIdx <= 0 {
				maxIdx = 100000
			}
			if len(data) > maxIdx {
				content = string(data[:maxIdx])
			} else {
				content = string(data)
			}
		} else {
			slog.Warn("Failed to read compiled playable for asset registration", "path", inlinePath, "error", err)
		}

		now := time.Now().Format(time.RFC3339)
		relativePath := filepath.Join("output", pipelineID, "preview_inline.html")

		gcsStatus := "none"
		if gcsURI != "" {
			gcsStatus = "uploaded"
		}

		asset := &ProjectAsset{
			ID:           generateID(),
			ProjectID:    projectID,
			Filename:     "preview_inline.html",
			RelativePath: relativePath,
			MIMEType:     "text/html",
			SizeBytes:    sizeBytes,
			Content:      content,
			ContentType:  "document",
			GCSStatus:    gcsStatus,
			GCSURI:       gcsURI,
			Checksum:     checksum,
			Tags:         []string{"playable", "html5_ad"},
			CreatedAt:    now,
			UpdatedAt:    now,
		}

		if err := es.IndexProjectAsset(ctx, asset); err != nil {
			slog.Warn("Failed to index playable asset in ES", "error", err)
		} else {
			slog.Info("Playable registered in project creatives library", "project", projectID, "path", relativePath)
		}
	}

	status := "registered"
	if publishToGCS && gcsURI != "" {
		status = "published"
	}

	return map[string]any{
		"status":         status,
		"production_url": url,
		"preview_url":    url,
	}
}

func generateImageAsset(ctx context.Context, config *Config, providers *ProviderRegistry, prompt, size, absOutPath string) error {
	settings := config.GetSettings()
	if settings.GoogleCloud.APIKey == "" && os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") == "" {
		dummyPng := []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13, 73, 72, 68, 82, 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0, 31, 21, 196, 137, 0, 0, 0, 10, 73, 68, 65, 84, 120, 156, 99, 0, 1, 0, 0, 5, 0, 1, 13, 10, 45, 180, 0, 0, 0, 0, 73, 69, 78, 68, 174, 66, 96, 130}
		slog.Warn("No Google Cloud credentials, writing dummy PNG for local test")
		return os.WriteFile(absOutPath, dummyPng, 0644)
	}

	if providers == nil {
		dummyPng := []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13, 73, 72, 68, 82, 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0, 31, 21, 196, 137, 0, 0, 0, 10, 73, 68, 65, 84, 120, 156, 99, 0, 1, 0, 0, 5, 0, 1, 13, 10, 45, 180, 0, 0, 0, 0, 73, 69, 78, 68, 174, 66, 96, 130}
		slog.Warn("No provider registry available, writing dummy PNG for local test")
		return os.WriteFile(absOutPath, dummyPng, 0644)
	}

	gp, ok := providers.Get("gemini").(*GeminiProvider)
	if !ok || gp == nil {
		return fmt.Errorf("gemini provider not found or misconfigured")
	}

	modelID, _ := config.GoogleCloud.VertexAI.DefaultModel("image")
	if modelID == "" {
		modelID = "imagen-3.0-generate-002"
	}

	client := gp.clientForModel(modelID)
	if client == nil {
		return fmt.Errorf("gemini client for model %s not initialized", modelID)
	}

	aspectRatio := mapSizeToAspectRatio(size)

	imgConfig := &genai.GenerateImagesConfig{
		NumberOfImages: 1,
		OutputMIMEType: "image/png",
		AspectRatio:    aspectRatio,
	}

	slog.Info("Generating image using Imagen 3", "prompt", prompt, "model", modelID, "aspect_ratio", aspectRatio)
	response, err := client.Models.GenerateImages(ctx, modelID, prompt, imgConfig)
	if err != nil {
		return fmt.Errorf("imagen generation failed: %w", err)
	}

	if len(response.GeneratedImages) == 0 || response.GeneratedImages[0].Image == nil {
		return fmt.Errorf("imagen returned no images")
	}

	imgBytes := response.GeneratedImages[0].Image.ImageBytes
	if len(imgBytes) == 0 {
		return fmt.Errorf("imagen returned empty image bytes")
	}

	if err := os.MkdirAll(filepath.Dir(absOutPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory for image: %w", err)
	}

	return os.WriteFile(absOutPath, imgBytes, 0644)
}

func generateAudioAsset(ctx context.Context, prompt string, duration int, absOutPath string) error {
	slog.Info("Generating procedural ambient WAV", "prompt", prompt, "duration", duration, "path", absOutPath)
	if err := os.MkdirAll(filepath.Dir(absOutPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory for audio: %w", err)
	}
	return generateAmbientWav(float64(duration), absOutPath)
}

func mapSizeToAspectRatio(size string) string {
	parts := strings.Split(size, "x")
	if len(parts) != 2 {
		return "1:1"
	}
	var w, h float64
	fmt.Sscanf(parts[0], "%f", &w)
	fmt.Sscanf(parts[1], "%f", &h)
	if w <= 0 || h <= 0 {
		return "1:1"
	}
	ratio := w / h
	if ratio == 1.0 {
		return "1:1"
	}
	if ratio > 1.0 {
		if ratio > 1.5 {
			return "16:9"
		}
		return "4:3"
	} else {
		if ratio < 0.65 {
			return "9:16"
		}
		return "3:4"
	}
}

func generateAmbientWav(durationSec float64, absOutPath string) error {
	const sampleRate = 44100
	const bitsPerSample = 16
	const numChannels = 1

	numSamples := int(sampleRate * durationSec)
	blockAlign := numChannels * (bitsPerSample / 8)
	dataSize := numSamples * blockAlign

	f, err := os.Create(absOutPath)
	if err != nil {
		return err
	}
	defer f.Close()

	f.Write([]byte("RIFF"))
	binary.Write(f, binary.LittleEndian, uint32(36+dataSize))
	f.Write([]byte("WAVE"))

	f.Write([]byte("fmt "))
	binary.Write(f, binary.LittleEndian, uint32(16))
	binary.Write(f, binary.LittleEndian, uint16(1))
	binary.Write(f, binary.LittleEndian, uint16(numChannels))
	binary.Write(f, binary.LittleEndian, uint32(sampleRate))
	binary.Write(f, binary.LittleEndian, uint32(sampleRate*numChannels*(bitsPerSample/8)))
	binary.Write(f, binary.LittleEndian, uint16(blockAlign))
	binary.Write(f, binary.LittleEndian, uint16(bitsPerSample))

	f.Write([]byte("data"))
	binary.Write(f, binary.LittleEndian, uint32(dataSize))

	amp := 0.15
	freqs := []float64{130.81, 196.0, 261.63, 329.63}
	for i := 0; i < numSamples; i++ {
		t := float64(i) / float64(sampleRate)
		lfo := 0.5 + 0.5*math.Sin(2*math.Pi*0.1*t)
		var sample float64
		for j, freq := range freqs {
			vol := amp * 0.4
			if j >= 2 {
				vol = amp * 0.2
			}
			vol = vol * (0.6 + 0.4*lfo)
			sample += vol * math.Sin(2*math.Pi*freq*t+float64(j)*0.3)
		}
		val := int16(math.Max(-1.0, math.Min(1.0, sample)) * 32767)
		binary.Write(f, binary.LittleEndian, val)
	}

	return nil
}

// Helper core functions

var base64Regex = regexp.MustCompile(`data:[^;]+;base64,[A-Za-z0-9+/=]+`)

func loadReferenceGameImpl(templatesBaseDir, gameType string) (string, error) {
	normalized := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(gameType)), "-", "_")
	baseDir := filepath.Join(templatesBaseDir, "playable_ads", normalized)

	if normalized == "match3" || normalized == "match_3" {
		indexPath := filepath.Join(baseDir, "index.html")
		data, err := os.ReadFile(indexPath)
		if err != nil {
			return "", fmt.Errorf("failed to read match3 template: %w", err)
		}
		stripped := base64Regex.ReplaceAllString(string(data), `data:image/png;base64,__BASE64_DATA_OMITTED__`)
		return stripped, nil
	}

	if normalized == "tile_match" || normalized == "mahjong" {
		files := []string{"index.html", "style.css", "playable.js"}
		var parts []string
		parts = append(parts, "=== Animal Mahjong (Tile Match, Vanilla HTML/CSS/JS) ===")
		parts = append(parts, fmt.Sprintf("Sample path: playable_ads/%s", normalized))

		for _, file := range files {
			filePath := filepath.Join(baseDir, file)
			data, err := os.ReadFile(filePath)
			if err != nil {
				slog.Warn("Failed to read tile_match template file", "file", file, "error", err)
				continue
			}
			content := string(data)
			content = base64Regex.ReplaceAllString(content, `data:image/png;base64,__BASE64_DATA_OMITTED__`)
			parts = append(parts, fmt.Sprintf("\n// --- %s ---\n%s", file, content))
		}
		return strings.Join(parts, "\n"), nil
	}

	if normalized == "vertical_shooter" || normalized == "shooter" {
		indexPath := filepath.Join(baseDir, "index.html")
		data, err := os.ReadFile(indexPath)
		if err != nil {
			indexPath = filepath.Join(baseDir, "template", "index.html")
			data, err = os.ReadFile(indexPath)
			if err != nil {
				return "", fmt.Errorf("failed to read vertical_shooter template: %w", err)
			}
		}
		stripped := base64Regex.ReplaceAllString(string(data), `data:image/png;base64,__BASE64_DATA_OMITTED__`)
		return stripped, nil
	}

	return "", fmt.Errorf("unknown game type: %s", gameType)
}

func getTrackingSDKImpl(sdkPath string) (string, error) {
	data, err := os.ReadFile(sdkPath)
	if err != nil {
		return "", fmt.Errorf("read sdk file: %w", err)
	}
	return string(data), nil
}

func getWebAudioSFXImpl() string {
	return `
// Procedural SFX engine using Web Audio API
class SoundEffects {
    constructor() {
        this.ctx = null;
    }
    init() {
        if (!this.ctx) {
            this.ctx = new (window.AudioContext || window.webkitAudioContext)();
        }
    }
    playLaser() {
        this.init();
        if (this.ctx.state === 'suspended') return;
        let osc = this.ctx.createOscillator();
        let gain = this.ctx.createGain();
        osc.connect(gain);
        gain.connect(this.ctx.destination);
        osc.frequency.setValueAtTime(800, this.ctx.currentTime);
        osc.frequency.exponentialRampToValueAtTime(100, this.ctx.currentTime + 0.3);
        gain.gain.setValueAtTime(0.3, this.ctx.currentTime);
        gain.gain.exponentialRampToValueAtTime(0.01, this.ctx.currentTime + 0.3);
        osc.start();
        osc.stop(this.ctx.currentTime + 0.3);
    }
}`
}

type WriteHTMLReport struct {
	Passed bool     `json:"passed"`
	Errors []string `json:"errors"`
}

func writeHTMLImpl(projectDir, pipelineID, htmlContent, validationScriptPath string) (*WriteHTMLReport, error) {
	outDir := filepath.Join(projectDir, "output", pipelineID)
	os.MkdirAll(outDir, 0755)

	indexPath := filepath.Join(outDir, "index.html")
	if err := os.WriteFile(indexPath, []byte(htmlContent), 0644); err != nil {
		return nil, err
	}

	// 1. Normalization pass
	normalizedHTML := htmlContent

	// Normalize url(assets/...) and url('assets/...') to url('assets/...')
	urlRegex := regexp.MustCompile(`(?i)url\(\s*['"]?(?:\./)?(assets/[^)'"]+)['"]?\s*\)`)
	normalizedHTML = urlRegex.ReplaceAllString(normalizedHTML, "url('$1')")

	// Normalize src="./assets/..." to src="assets/..."
	attrRegex := regexp.MustCompile(`(?i)\b(src|href|poster|data-src)\s*=\s*(["'])\s*(?:\./)?(assets/[^"'\s<>]+)(["'])`)
	normalizedHTML = attrRegex.ReplaceAllStringFunc(normalizedHTML, func(match string) string {
		submatches := attrRegex.FindStringSubmatch(match)
		if len(submatches) != 5 {
			return match
		}
		attr := submatches[1]
		openQuote := submatches[2]
		assetPath := submatches[3]
		closeQuote := submatches[4]
		if openQuote != closeQuote {
			return match
		}
		return fmt.Sprintf("%s=%s%s%s", attr, openQuote, assetPath, closeQuote)
	})

	// 2. Inlining pass: replace any quoted "assets/..." string with base64 data URI
	inlineRegex := regexp.MustCompile(`(["'])(assets/[^"'\s<>]+)(["'])`)
	inlineContent := inlineRegex.ReplaceAllStringFunc(normalizedHTML, func(match string) string {
		submatches := inlineRegex.FindStringSubmatch(match)
		if len(submatches) != 4 {
			return match
		}
		quote := submatches[1]
		relPath := submatches[2]
		closeQuote := submatches[3]
		if quote != closeQuote {
			return match
		}

		cleanPath := relPath
		if idx := strings.Index(cleanPath, "?"); idx != -1 {
			cleanPath = cleanPath[:idx]
		}
		if idx := strings.Index(cleanPath, "#"); idx != -1 {
			cleanPath = cleanPath[:idx]
		}

		absAssetPath := filepath.Join(outDir, cleanPath)
		assetData, err := os.ReadFile(absAssetPath)
		if err != nil {
			basename := filepath.Base(cleanPath)
			fallbackPath := filepath.Join(outDir, "assets", basename)
			assetData, err = os.ReadFile(fallbackPath)
			if err != nil {
				slog.Warn("Asset not found for inlining", "ref", relPath, "abs_path", absAssetPath, "fallback_path", fallbackPath)
				return match
			}
		}

		mimeType := mime.TypeByExtension(filepath.Ext(cleanPath))
		if mimeType == "" {
			mimeType = http.DetectContentType(assetData)
		}
		b64 := base64.StdEncoding.EncodeToString(assetData)
		dataURI := fmt.Sprintf("data:%s;base64,%s", mimeType, b64)

		return quote + dataURI + quote
	})

	inlinePath := filepath.Join(outDir, "preview_inline.html")
	if err := os.WriteFile(inlinePath, []byte(inlineContent), 0644); err != nil {
		return nil, err
	}

	valDest := filepath.Join(outDir, "playwright_validation.js")
	if scriptData, err := os.ReadFile(validationScriptPath); err == nil {
		os.WriteFile(valDest, scriptData, 0644)
	}

	report := &WriteHTMLReport{Passed: true, Errors: []string{}}
	if len(inlineContent) > 5*1024*1024 {
		report.Passed = false
		report.Errors = append(report.Errors, "Inline HTML size exceeds 5MB")
	}
	if strings.Contains(htmlContent, "eval(") {
		report.Passed = false
		report.Errors = append(report.Errors, "Forbidden 'eval()' function call detected")
	}
	if regexp.MustCompile(`(?i)(src|href)\s*=\s*["']http(s)?://`).MatchString(htmlContent) {
		report.Errors = append(report.Errors, "Warning: absolute network URL found in src/href attributes")
	}

	return report, nil
}

func publishPlayableAdImpl(ctx context.Context, gcs *GCSClient, config *Config, projectDir, pipelineID string, publishToGCS bool) (string, string, error) {
	port := config.Server.Port
	if port == 0 {
		port = 1983
	}
	localURL := fmt.Sprintf("http://localhost:%d/playable-preview/%s/preview_inline.html", port, pipelineID)

	if !publishToGCS || gcs == nil {
		if publishToGCS && gcs == nil {
			slog.Warn("GCS publish requested but GCS client not configured; falling back to local URL")
		}
		return localURL, "", nil
	}

	outDir := filepath.Join(projectDir, "output", pipelineID)
	url, err := gcs.PublishPlayable(ctx, pipelineID, outDir)
	if err != nil {
		slog.Warn("GCS upload failed, falling back to local preview", "error", err, "url", localURL)
		return localURL, "", nil
	}

	gcsURI := fmt.Sprintf("gs://%s/playable-ads/%s/index.html", gcs.bucket, pipelineID)
	return url, gcsURI, nil
}
