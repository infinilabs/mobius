package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// Object-level authorization for MCP tool calls is enforced centrally by
// authorizeToolCall (see authz.go), invoked from HandleMessage before a
// handler runs — handlers here must NOT re-implement per-handler checks.

func (s *MCPServer) handleDelegateTask(ctx context.Context, raw json.RawMessage, caller MCPCaller) (any, error) {
	args := parseArgs(raw)
	assigneeID := argStr(args, "assignee_id")
	title := argStr(args, "title")
	goal := argStr(args, "goal")
	taskContext := argStr(args, "context")
	priority := argStr(args, "priority")

	if assigneeID == "" || title == "" || goal == "" {
		return nil, fmt.Errorf("assignee_id, title, and goal are required")
	}

	creator, err := s.pgClient.GetEmployee(ctx, caller.AgentID)
	if err != nil {
		return nil, fmt.Errorf("caller agent not found")
	}
	assignee, err := s.pgClient.GetEmployee(ctx, assigneeID)
	if err != nil {
		return nil, fmt.Errorf("assignee not found")
	}
	if !canDelegate(ctx, s.pgClient, creator, assignee) {
		return nil, fmt.Errorf("cannot delegate to %s: outside team hierarchy", assignee.Name)
	}

	// Delegations from within a task run inherit the parent's chain depth so
	// the bound in exceedsDelegationDepth holds across the MCP path too.
	depth := 0
	if caller.TaskID != "" {
		parent, err := s.pgClient.GetTask(ctx, caller.TaskID)
		if err != nil {
			return nil, fmt.Errorf("failed to load parent task: %w", err)
		}
		if exceedsDelegationDepth(parent.DelegationDepth) {
			return nil, fmt.Errorf("delegation chain too deep (depth %d, max %d): do the work yourself or report back to your manager",
				parent.DelegationDepth, maxDelegationDepth)
		}
		depth = parent.DelegationDepth + 1
	}

	body := "## Goal\n" + goal
	if taskContext != "" {
		body += "\n\n## Context\n" + taskContext
	}
	if priority == "" {
		priority = "medium"
	}

	t := &Task{
		Title:           title,
		Body:            body,
		Priority:        priority,
		Creator:         &EmployeeBrief{ID: creator.ID, Name: creator.Name, Title: creator.Title, Role: creator.Role},
		Assignee:        &EmployeeBrief{ID: assignee.ID, Name: assignee.Name, Title: assignee.Title, Role: assignee.Role},
		DelegationDepth: depth,
	}
	if err := s.pgClient.CreateTask(ctx, t, nil); err != nil {
		return nil, fmt.Errorf("failed to create task: %w", err)
	}
	if s.esClient != nil {
		if full, ferr := s.pgClient.GetTask(ctx, t.ID); ferr == nil {
			if ierr := s.esClient.IndexTask(ctx, full); ierr != nil {
				slog.Warn("ES index delegated task failed", "task_id", t.ID, "error", ierr)
			}
		}
	}

	return map[string]any{"status": "created", "task_id": t.ID, "assignee": assignee.Name}, nil
}

func (s *MCPServer) handleHireEmployee(ctx context.Context, raw json.RawMessage, caller MCPCaller) (any, error) {
	args := parseArgs(raw)
	manager, err := s.pgClient.GetEmployee(ctx, caller.AgentID)
	if err != nil {
		return nil, fmt.Errorf("caller agent not found")
	}

	name := argStr(args, "name")
	title := argStr(args, "title")
	backstory := argStr(args, "backstory")
	if name == "" || title == "" || backstory == "" {
		return nil, fmt.Errorf("name, title, and backstory are required")
	}

	if reason, ok := checkHireDuplicate(manager, title); !ok {
		return nil, fmt.Errorf("%s", reason)
	}

	emp := &Employee{
		Name: name, Title: title, Role: "Custom", Backstory: backstory,
		AdapterConfig: map[string]any{},
		Models:        []EmployeeModel{}, Skills: []EmployeeSkill{}, Tags: []string{},
		ManagerID:     &manager.ID,
	}
	if llm := argStr(args, "primary_llm"); llm != "" {
		emp.Models = append(emp.Models, EmployeeModel{ModelID: llm, Purpose: "primary_llm"})
	}
	if err := s.pgClient.CreateEmployee(ctx, emp); err != nil {
		return nil, fmt.Errorf("failed to hire: %w", err)
	}
	if s.esClient != nil {
		if ierr := s.esClient.IndexEmployee(ctx, emp); ierr != nil {
			slog.Warn("ES index hired employee failed", "employee_id", emp.ID, "error", ierr)
		}
	}

	return map[string]any{"status": "hired", "employee_id": emp.ID, "name": name}, nil
}

func (s *MCPServer) handleSubmitResult(ctx context.Context, raw json.RawMessage, caller MCPCaller) (any, error) {
	args := parseArgs(raw)
	taskID := argStr(args, "task_id")
	result := argStr(args, "result")
	if taskID == "" || result == "" {
		return nil, fmt.Errorf("task_id and result are required")
	}
	if err := s.pgClient.SubmitTaskResult(ctx, taskID, result); err != nil {
		return nil, fmt.Errorf("failed to submit: %w", err)
	}
	return map[string]any{"status": "submitted_for_review", "task_id": taskID}, nil
}

func (s *MCPServer) handleReviewTask(ctx context.Context, raw json.RawMessage, caller MCPCaller) (any, error) {
	args := parseArgs(raw)
	taskID := argStr(args, "task_id")
	action := argStr(args, "action")
	feedback := argStr(args, "feedback")
	if taskID == "" || action == "" {
		return nil, fmt.Errorf("task_id and action are required")
	}
	switch action {
	case "APPROVE":
		if err := s.pgClient.UpdateTaskStatus(ctx, taskID, "done", caller.AgentID); err != nil {
			return nil, err
		}
		return map[string]any{"status": "approved", "task_id": taskID}, nil
	case "REJECT":
		if feedback == "" {
			return nil, fmt.Errorf("feedback required when rejecting")
		}
		if err := s.pgClient.UpdateTaskStatus(ctx, taskID, "ready", caller.AgentID); err != nil {
			return nil, err
		}
		// The rejection feedback is the actionable output of a REJECT; if it can't
		// be recorded the task would re-run with no guidance, so surface it loudly.
		if _, err := s.pgClient.AddTaskComment(ctx, taskID, caller.AgentID, "REJECTED: "+feedback); err != nil {
			slog.Error("failed to record rejection feedback", "task_id", taskID, "error", err)
		}
		return map[string]any{"status": "rejected", "task_id": taskID}, nil
	default:
		return nil, fmt.Errorf("action must be APPROVE or REJECT")
	}
}

func (s *MCPServer) handleVerifyDeliverable(ctx context.Context, raw json.RawMessage, _ MCPCaller) (any, error) {
	args := parseArgs(raw)
	taskID := argStr(args, "task_id")
	if taskID == "" {
		return nil, fmt.Errorf("task_id is required")
	}
	task, err := s.pgClient.GetTask(ctx, taskID)
	if err != nil || task == nil {
		return nil, fmt.Errorf("task not found")
	}
	result := map[string]any{"task_id": taskID, "status": task.Status, "result": task.Result}
	if s.esClient != nil {
		assets, aerr := s.esClient.SearchAssetsByTask(ctx, taskID)
		if aerr == nil {
			var files []map[string]any
			for _, a := range assets {
				files = append(files, map[string]any{"filename": a.Filename, "path": a.RelativePath, "type": a.ContentType, "size": a.SizeBytes})
			}
			if files == nil {
				files = []map[string]any{}
			}
			result["files"] = files
		}
	}
	return result, nil
}

func (s *MCPServer) handleListTeam(ctx context.Context, _ json.RawMessage, caller MCPCaller) (any, error) {
	emp, err := s.pgClient.GetEmployee(ctx, caller.AgentID)
	if err != nil {
		return nil, err
	}
	var members []map[string]any
	for _, r := range emp.Reports {
		members = append(members, map[string]any{"id": r.ID, "name": r.Name, "title": r.Title, "role": r.Role})
	}
	if members == nil {
		members = []map[string]any{}
	}
	return map[string]any{"team": members, "count": len(members)}, nil
}

func (s *MCPServer) handleStoreMemory(ctx context.Context, raw json.RawMessage, caller MCPCaller) (any, error) {
	args := parseArgs(raw)
	text := argStr(args, "memory_text")
	if text == "" || s.esClient == nil {
		return nil, fmt.Errorf("memory_text is required")
	}
	if err := s.esClient.IndexEmployeeMemoryDedup(ctx, caller.AgentID, caller.TaskID, text); err != nil {
		return nil, err
	}
	return map[string]any{"status": "remembered", "memory_text": text}, nil
}

func (s *MCPServer) handleForgetMemory(ctx context.Context, raw json.RawMessage, _ MCPCaller) (any, error) {
	args := parseArgs(raw)
	memoryID := argStr(args, "memory_id")
	if memoryID == "" || s.esClient == nil {
		return nil, fmt.Errorf("memory_id is required")
	}
	if err := s.esClient.DeleteEmployeeMemory(ctx, memoryID); err != nil {
		return nil, err
	}
	return map[string]any{"status": "forgotten", "memory_id": memoryID}, nil
}

func (s *MCPServer) handleWriteFile(ctx context.Context, raw json.RawMessage, caller MCPCaller) (any, error) {
	args := parseArgs(raw)
	path := argStr(args, "path")
	content := argStr(args, "content")
	if path == "" || content == "" {
		return nil, fmt.Errorf("path and content are required")
	}

	task, err := s.pgClient.GetTask(ctx, caller.TaskID)
	if err != nil || task == nil || task.ProjectID == nil {
		return nil, fmt.Errorf("no project context")
	}
	project, err := s.pgClient.GetProject(ctx, *task.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("project not found")
	}

	fullPath, err := resolveWithinRoot(project.RootDir(s.config), path)
	if err != nil {
		return nil, err
	}
	os.MkdirAll(filepath.Dir(fullPath), 0755)
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("write failed: %w", err)
	}

	if s.esClient != nil {
		ct := classifyContentType("text/plain")
		now := time.Now().Format(time.RFC3339)
		maxIdx := s.config.Projects.ContentMaxIndex
		indexContent := content
		truncated := false
		if len(content) > maxIdx {
			indexContent = content[:maxIdx]
			truncated = true
		}
		asset := &ProjectAsset{
			ID: generateID(), ProjectID: *task.ProjectID,
			Filename: filepath.Base(path), RelativePath: path,
			MIMEType: "text/plain", SizeBytes: int64(len(content)),
			Content: indexContent, ContentTruncated: truncated,
			ContentType: ct, GCSStatus: "pending",
			Tags: []string{}, CreatedByID: caller.AgentID, TaskID: caller.TaskID,
			CreatedAt: now, UpdatedAt: now,
		}
		s.esClient.IndexProjectAsset(ctx, asset)
	}

	return map[string]any{"status": "written", "path": path, "bytes": len(content)}, nil
}

func (s *MCPServer) handleReadFile(ctx context.Context, raw json.RawMessage, caller MCPCaller) (any, error) {
	args := parseArgs(raw)
	path := argStr(args, "path")
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
	task, err := s.pgClient.GetTask(ctx, caller.TaskID)
	if err != nil || task == nil || task.ProjectID == nil {
		return nil, fmt.Errorf("no project context")
	}
	project, err := s.pgClient.GetProject(ctx, *task.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("project not found")
	}
	fullPath, err := resolveWithinRoot(project.RootDir(s.config), path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("read failed: %w", err)
	}
	return map[string]any{"content": string(data), "path": path, "bytes": len(data)}, nil
}

func (s *MCPServer) handleSearchAssets(ctx context.Context, raw json.RawMessage, caller MCPCaller) (any, error) {
	args := parseArgs(raw)
	task, _ := s.pgClient.GetTask(ctx, caller.TaskID)
	if task == nil || task.ProjectID == nil || s.esClient == nil {
		return nil, fmt.Errorf("no project context or ES unavailable")
	}
	query := argStr(args, "query")
	contentType := argStr(args, "type")
	assets, err := s.esClient.SearchProjectAssets(ctx, *task.ProjectID, query, contentType, 10)
	if err != nil {
		return nil, err
	}
	var results []map[string]any
	for _, a := range assets {
		results = append(results, map[string]any{"filename": a.Filename, "path": a.RelativePath, "type": a.ContentType, "size": a.SizeBytes})
	}
	if results == nil {
		results = []map[string]any{}
	}
	return map[string]any{"results": results, "count": len(results)}, nil
}

func (s *MCPServer) handleListAssets(ctx context.Context, _ json.RawMessage, caller MCPCaller) (any, error) {
	task, _ := s.pgClient.GetTask(ctx, caller.TaskID)
	if task == nil || task.ProjectID == nil || s.esClient == nil {
		return nil, fmt.Errorf("no project context or ES unavailable")
	}
	assets, err := s.esClient.SearchProjectAssets(ctx, *task.ProjectID, "", "", 100)
	if err != nil {
		return nil, err
	}
	var results []map[string]any
	for _, a := range assets {
		results = append(results, map[string]any{"filename": a.Filename, "path": a.RelativePath, "type": a.ContentType, "size": a.SizeBytes})
	}
	if results == nil {
		results = []map[string]any{}
	}
	return map[string]any{"results": results, "count": len(results)}, nil
}

func (s *MCPServer) handleRunCommand(ctx context.Context, raw json.RawMessage, caller MCPCaller) (any, error) {
	args := parseArgs(raw)
	command := argStr(args, "command")
	if command == "" {
		return nil, fmt.Errorf("command is required")
	}
	if err := validateCommand(command); err != nil {
		return nil, err
	}
	// Code execution is sandbox-only: never fall back to running on the host.
	if s.config == nil || !s.config.Sandbox.Enabled {
		return nil, fmt.Errorf("command execution requires the sandbox: enable sandbox in config (host execution is not permitted)")
	}

	task, _ := s.pgClient.GetTask(ctx, caller.TaskID)
	if task == nil || task.ProjectID == nil {
		return nil, fmt.Errorf("no project context")
	}
	project, err := s.pgClient.GetProject(ctx, *task.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("project not found")
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	workdir := project.RootDir(s.config)
	out, errOut, exitCode, execErr := runSandboxedCommand(cmdCtx, s.config.Sandbox, workdir, command, nil)
	if execErr != nil {
		return nil, execErr
	}
	return map[string]any{
		"stdout":    truncateOutput(out),
		"stderr":    truncateOutput(errOut),
		"exit_code": exitCode,
		"success":   exitCode == 0,
	}, nil
}

func (s *MCPServer) handleListTasks(ctx context.Context, raw json.RawMessage, _ MCPCaller) (any, error) {
	args := parseArgs(raw)
	tasks, err := s.pgClient.ListTasks(ctx, argStr(args, "status"), argStr(args, "assignee_id"), argStr(args, "project_id"), "")
	if err != nil {
		return nil, err
	}
	var results []map[string]any
	for _, t := range tasks {
		r := map[string]any{"id": t.ID, "title": t.Title, "status": t.Status, "priority": t.Priority}
		if t.Assignee != nil {
			r["assignee"] = t.Assignee.Name
		}
		results = append(results, r)
	}
	if results == nil {
		results = []map[string]any{}
	}
	return map[string]any{"tasks": results, "count": len(results)}, nil
}

func (s *MCPServer) handleGetTask(ctx context.Context, raw json.RawMessage, _ MCPCaller) (any, error) {
	args := parseArgs(raw)
	taskID := argStr(args, "task_id")
	if taskID == "" {
		return nil, fmt.Errorf("task_id is required")
	}
	task, err := s.pgClient.GetTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("task not found")
	}
	return map[string]any{"id": task.ID, "title": task.Title, "body": task.Body, "status": task.Status, "priority": task.Priority, "result": task.Result}, nil
}

func (s *MCPServer) handleUpdateTask(ctx context.Context, raw json.RawMessage, caller MCPCaller) (any, error) {
	args := parseArgs(raw)
	taskID := argStr(args, "task_id")
	if taskID == "" {
		return nil, fmt.Errorf("task_id is required")
	}
	var title, body, priority, assigneeID *string
	if v := argStr(args, "title"); v != "" {
		title = &v
	}
	if v := argStr(args, "body"); v != "" {
		body = &v
	}
	if v := argStr(args, "priority"); v != "" {
		priority = &v
	}
	if v := argStr(args, "assignee_id"); v != "" {
		assigneeID = &v
	}
	if err := s.pgClient.UpdateTask(ctx, taskID, title, body, priority, assigneeID, nil); err != nil {
		return nil, err
	}
	return map[string]any{"status": "updated", "task_id": taskID}, nil
}

func (s *MCPServer) handleUpdateTaskStatus(ctx context.Context, raw json.RawMessage, caller MCPCaller) (any, error) {
	args := parseArgs(raw)
	taskID := argStr(args, "task_id")
	status := argStr(args, "status")
	feedback := argStr(args, "feedback")
	if taskID == "" || status == "" {
		return nil, fmt.Errorf("task_id and status are required")
	}
	if err := s.pgClient.UpdateTaskStatus(ctx, taskID, status, caller.AgentID, feedback); err != nil {
		return nil, err
	}
	return map[string]any{"status": "updated", "task_id": taskID, "new_status": status}, nil
}

func (s *MCPServer) handleAddTaskComment(ctx context.Context, raw json.RawMessage, caller MCPCaller) (any, error) {
	args := parseArgs(raw)
	taskID := argStr(args, "task_id")
	content := argStr(args, "content")
	if taskID == "" || content == "" {
		return nil, fmt.Errorf("task_id and content are required")
	}
	if _, err := s.pgClient.AddTaskComment(ctx, taskID, caller.AgentID, content); err != nil {
		return nil, err
	}
	return map[string]any{"status": "commented", "task_id": taskID}, nil
}

func (s *MCPServer) handleListEmployees(ctx context.Context, _ json.RawMessage, _ MCPCaller) (any, error) {
	employees, err := s.pgClient.ListEmployees(ctx)
	if err != nil {
		return nil, err
	}
	var results []map[string]any
	for _, e := range employees {
		results = append(results, map[string]any{"id": e.ID, "name": e.Name, "title": e.Title, "role": e.Role})
	}
	if results == nil {
		results = []map[string]any{}
	}
	return map[string]any{"employees": results, "count": len(results)}, nil
}

func (s *MCPServer) handleGetEmployee(ctx context.Context, raw json.RawMessage, _ MCPCaller) (any, error) {
	args := parseArgs(raw)
	empID := argStr(args, "employee_id")
	if empID == "" {
		return nil, fmt.Errorf("employee_id is required")
	}
	emp, err := s.pgClient.GetEmployee(ctx, empID)
	if err != nil {
		return nil, fmt.Errorf("employee not found")
	}
	return map[string]any{"id": emp.ID, "name": emp.Name, "title": emp.Title, "role": emp.Role, "backstory": emp.Backstory, "reports": emp.Reports}, nil
}

func (s *MCPServer) handleUpdateEmployee(ctx context.Context, raw json.RawMessage, caller MCPCaller) (any, error) {
	args := parseArgs(raw)
	empID := argStr(args, "employee_id")
	if empID == "" {
		return nil, fmt.Errorf("employee_id is required")
	}
	emp, err := s.pgClient.GetEmployee(ctx, empID)
	if err != nil {
		return nil, fmt.Errorf("employee not found")
	}
	if v := argStr(args, "title"); v != "" {
		emp.Title = v
	}
	if v := argStr(args, "backstory"); v != "" {
		emp.Backstory = v
	}
	if err := s.pgClient.UpdateEmployee(ctx, empID, emp); err != nil {
		return nil, err
	}
	return map[string]any{"status": "updated", "employee_id": empID}, nil
}

func (s *MCPServer) handleListProjects(ctx context.Context, _ json.RawMessage, _ MCPCaller) (any, error) {
	projects, err := s.pgClient.ListProjects(ctx, "")
	if err != nil {
		return nil, err
	}
	var results []map[string]any
	for _, p := range projects {
		results = append(results, map[string]any{"id": p.ID, "name": p.Name, "status": p.Status})
	}
	if results == nil {
		results = []map[string]any{}
	}
	return map[string]any{"projects": results, "count": len(results)}, nil
}

func (s *MCPServer) handleCreateProject(ctx context.Context, raw json.RawMessage, caller MCPCaller) (any, error) {
	args := parseArgs(raw)
	name := argStr(args, "name")
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	p, err := s.pgClient.CreateProject(ctx, CreateProjectInput{
		Name:        name,
		Description: argStr(args, "description"),
		OwnerID:     caller.AgentID,
	}, s.config)
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": "created", "project_id": p.ID, "name": name}, nil
}

func (s *MCPServer) handleUpdateProject(ctx context.Context, raw json.RawMessage, caller MCPCaller) (any, error) {
	args := parseArgs(raw)
	projectID := argStr(args, "project_id")
	if projectID == "" {
		return nil, fmt.Errorf("project_id is required")
	}
	var description, status *string
	if v := argStr(args, "description"); v != "" {
		description = &v
	}
	if v := argStr(args, "status"); v != "" {
		status = &v
	}
	if err := s.pgClient.UpdateProject(ctx, projectID, nil, description, status); err != nil {
		return nil, err
	}
	return map[string]any{"status": "updated", "project_id": projectID}, nil
}

func (s *MCPServer) handleListPrompts(ctx context.Context, raw json.RawMessage, _ MCPCaller) (any, error) {
	if s.esClient == nil {
		return nil, fmt.Errorf("ES unavailable")
	}
	args := parseArgs(raw)
	prompts, err := s.esClient.SearchPrompts(ctx, argStr(args, "query"))
	if err != nil {
		return nil, err
	}
	var results []map[string]any
	for _, p := range prompts {
		results = append(results, map[string]any{"id": p.ID, "title": p.Title, "tags": p.Tags})
	}
	if results == nil {
		results = []map[string]any{}
	}
	return map[string]any{"prompts": results, "count": len(results)}, nil
}

func (s *MCPServer) handleCreatePrompt(ctx context.Context, raw json.RawMessage, _ MCPCaller) (any, error) {
	if s.esClient == nil {
		return nil, fmt.Errorf("ES unavailable")
	}
	args := parseArgs(raw)
	title := argStr(args, "title")
	content := argStr(args, "content")
	if title == "" || content == "" {
		return nil, fmt.Errorf("title and content are required")
	}
	p := &Prompt{Title: title, Content: content, Tags: []string{}}
	if err := s.esClient.IndexPrompt(ctx, p); err != nil {
		return nil, err
	}
	return map[string]any{"status": "created", "prompt_id": p.ID}, nil
}

func (s *MCPServer) handleUpdatePrompt(ctx context.Context, raw json.RawMessage, _ MCPCaller) (any, error) {
	if s.esClient == nil {
		return nil, fmt.Errorf("ES unavailable")
	}
	args := parseArgs(raw)
	promptID := argStr(args, "prompt_id")
	if promptID == "" {
		return nil, fmt.Errorf("prompt_id is required")
	}
	p, err := s.esClient.GetPrompt(ctx, promptID)
	if err != nil {
		return nil, fmt.Errorf("prompt not found")
	}
	if v := argStr(args, "title"); v != "" {
		p.Title = v
	}
	if v := argStr(args, "content"); v != "" {
		p.Content = v
	}
	if err := s.esClient.IndexPrompt(ctx, p); err != nil {
		return nil, err
	}
	return map[string]any{"status": "updated", "prompt_id": promptID}, nil
}

func (s *MCPServer) handleDeletePrompt(ctx context.Context, raw json.RawMessage, _ MCPCaller) (any, error) {
	if s.esClient == nil {
		return nil, fmt.Errorf("ES unavailable")
	}
	args := parseArgs(raw)
	promptID := argStr(args, "prompt_id")
	if promptID == "" {
		return nil, fmt.Errorf("prompt_id is required")
	}
	if err := s.esClient.DeletePrompt(ctx, promptID); err != nil {
		return nil, err
	}
	return map[string]any{"status": "deleted", "prompt_id": promptID}, nil
}

func (s *MCPServer) handleListSkills(ctx context.Context, raw json.RawMessage, _ MCPCaller) (any, error) {
	if s.esClient == nil {
		return nil, fmt.Errorf("ES unavailable")
	}
	args := parseArgs(raw)
	skills, err := s.esClient.SearchSkills(ctx, argStr(args, "query"))
	if err != nil {
		return nil, err
	}
	var results []map[string]any
	for _, sk := range skills {
		results = append(results, map[string]any{"id": sk.ID, "name": sk.Name, "category": sk.Category})
	}
	if results == nil {
		results = []map[string]any{}
	}
	return map[string]any{"skills": results, "count": len(results)}, nil
}

func (s *MCPServer) handleAssignSkill(ctx context.Context, raw json.RawMessage, _ MCPCaller) (any, error) {
	args := parseArgs(raw)
	empID := argStr(args, "employee_id")
	skillID := argStr(args, "skill_id")
	if empID == "" || skillID == "" {
		return nil, fmt.Errorf("employee_id and skill_id are required")
	}
	if err := s.pgClient.AssignSkill(ctx, empID, skillID); err != nil {
		return nil, err
	}
	return map[string]any{"status": "assigned"}, nil
}

func (s *MCPServer) handleUnassignSkill(ctx context.Context, raw json.RawMessage, _ MCPCaller) (any, error) {
	args := parseArgs(raw)
	empID := argStr(args, "employee_id")
	skillID := argStr(args, "skill_id")
	if empID == "" || skillID == "" {
		return nil, fmt.Errorf("employee_id and skill_id are required")
	}
	if err := s.pgClient.UnassignSkill(ctx, empID, skillID); err != nil {
		return nil, err
	}
	return map[string]any{"status": "unassigned"}, nil
}

func (s *MCPServer) handleAskUser(ctx context.Context, raw json.RawMessage, caller MCPCaller) (any, error) {
	args := parseArgs(raw)
	question := argStr(args, "question")
	if question == "" {
		return nil, fmt.Errorf("question is required")
	}
	if caller.TaskID == "" {
		return nil, fmt.Errorf("task context required for ask_user")
	}

	payload, _ := json.Marshal(map[string]any{
		"question": question,
		"options":  args["options"],
	})

	ix := &TaskInteraction{
		TaskID:            caller.TaskID,
		CreatorEmployeeID: caller.AgentID,
		Kind:              "ask_user",
		Payload:           payload,
	}
	if err := s.pgClient.CreateInteraction(ctx, ix); err != nil {
		return nil, fmt.Errorf("failed to create interaction: %w", err)
	}
	// Blocking the task is the whole point of ask_user; if it fails the caller
	// must not be told the task is paused when it is still runnable.
	if err := s.pgClient.UpdateTaskStatus(ctx, caller.TaskID, "blocked", ""); err != nil {
		return nil, fmt.Errorf("failed to block task: %w", err)
	}

	return map[string]any{
		"status":         "waiting_for_answer",
		"interaction_id": ix.ID,
		"message":        "Your question has been posted. The task is paused until answered.",
	}, nil
}

func (s *MCPServer) handleSuggestTasks(ctx context.Context, raw json.RawMessage, caller MCPCaller) (any, error) {
	args := parseArgs(raw)
	tasks, _ := args["tasks"].([]any)
	rationale := argStr(args, "rationale")
	if len(tasks) == 0 {
		return nil, fmt.Errorf("tasks array is required")
	}
	if caller.TaskID == "" {
		return nil, fmt.Errorf("task context required for suggest_tasks")
	}

	payload, _ := json.Marshal(map[string]any{
		"tasks":     tasks,
		"rationale": rationale,
	})

	ix := &TaskInteraction{
		TaskID:            caller.TaskID,
		CreatorEmployeeID: caller.AgentID,
		Kind:              "suggest_tasks",
		Payload:           payload,
	}
	if err := s.pgClient.CreateInteraction(ctx, ix); err != nil {
		return nil, fmt.Errorf("failed to create interaction: %w", err)
	}

	return map[string]any{
		"status":         "pending_approval",
		"interaction_id": ix.ID,
		"task_count":     len(tasks),
	}, nil
}

