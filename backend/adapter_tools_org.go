package main

import (
	"context"
	"fmt"
	"log/slog"
	"mobius/internal/service"
)

// Org tools: delegation/hire/review lifecycle, employees, skills.
// Split from adapter_internal_tools.go (plan 6.5).

func (a *InternalLLMAdapter) execDelegate(ctx context.Context, args map[string]any, creator *Employee, currentTask *Task) map[string]any {
	assigneeID, _ := args["assignee_id"].(string)
	title, _ := args["title"].(string)
	goal, _ := args["goal"].(string)
	taskContext, _ := args["context"].(string)
	priority, _ := args["priority"].(string)

	if assigneeID == "" || title == "" || goal == "" {
		return map[string]any{"error": "assignee_id, title, and goal are required"}
	}
	if err := service.ValidateDelegateArgs(title, goal, taskContext); err != nil {
		return map[string]any{"error": err.Error()}
	}

	assignee, err := a.pgClient.GetEmployee(ctx, assigneeID)
	if err != nil {
		return map[string]any{"error": "assignee not found: " + err.Error()}
	}
	if !service.CanDelegate(ctx, a.pgClient, creator, assignee) {
		return map[string]any{"error": fmt.Sprintf("cannot delegate to %s: outside team hierarchy", assignee.Name)}
	}
	if service.ExceedsDelegationDepth(currentTask.DelegationDepth) {
		return map[string]any{"error": fmt.Sprintf(
			"delegation chain too deep (depth %d, max %d): do the work yourself or report back to your manager",
			currentTask.DelegationDepth, service.MaxDelegationDepth)}
	}

	// A retried parent run re-issues the same delegate call; reuse the child a
	// previous attempt already created instead of duplicating it.
	existingID, err := a.pgClient.FindActiveChildTask(ctx, currentTask.ID, assignee.ID, title)
	if err != nil {
		return map[string]any{"error": "failed to check existing delegation: " + err.Error()}
	}
	if existingID != "" {
		slog.Info("adapter: delegation deduped", "existing_task_id", existingID, "from", creator.Name, "to", assignee.Name)
		return map[string]any{"status": "already_delegated", "task_id": existingID, "assignee": assignee.Name,
			"note": "an identical delegation from this task already exists; not creating a duplicate"}
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
		DelegationDepth: currentTask.DelegationDepth + 1,
		ParentTaskID:    &currentTask.ID,
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

// Manager-only access is enforced by service.AuthorizeToolCall (authz.go) before this
// executor runs.
func (a *InternalLLMAdapter) execHire(ctx context.Context, args map[string]any, manager *Employee) map[string]any {
	name, _ := args["name"].(string)
	title, _ := args["title"].(string)
	backstory, _ := args["backstory"].(string)
	primaryLLM, _ := args["primary_llm"].(string)

	if name == "" || title == "" || backstory == "" {
		return map[string]any{"error": "name, title, and backstory are required"}
	}
	if err := service.ValidateHireArgs(name, title, backstory); err != nil {
		return map[string]any{"error": err.Error()}
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
		ManagerID: &manager.ID,
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
	if err := a.pgClient.SubmitTaskResult(ctx, taskID, result); err != nil {
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

func (a *InternalLLMAdapter) execUpdateEmployee(ctx context.Context, args map[string]any, actor *Employee) map[string]any {
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
		// Tags gate tool access and delegation authority; only a CEO may change
		// the privileged subset (plan 2.2).
		if err := service.ValidateTagChange(actor, emp.Tags, tags); err != nil {
			return map[string]any{"error": err.Error()}
		}
		emp.Tags = tags
	}
	if err := a.pgClient.UpdateEmployee(ctx, empID, emp); err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"status": "updated", "employee_id": empID}
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
