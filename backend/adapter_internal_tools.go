package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
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

	if taskID == "" || result == "" {
		return map[string]any{"error": "task_id and result are required"}
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
			Filename: filepath.Base(path), RelativePath: path,
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
	tasks, err := a.pgClient.ListTasks(ctx, status, assigneeID, projectID)
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
	p := &Project{
		Name:        name,
		Description: description,
		Tags:        []string{},
		Owner:       &EmployeeBrief{ID: agent.ID},
	}
	if err := a.pgClient.CreateProject(ctx, p, a.config); err != nil {
		return map[string]any{"error": "failed to create project: " + err.Error()}
	}
	if a.esClient != nil {
		full, _ := a.pgClient.GetProject(ctx, p.ID)
		if full != nil {
			a.esClient.IndexProject(ctx, full)
		}
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
