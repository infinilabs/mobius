package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
)

func (h *APIHandler) executeToolCall(
	ctx context.Context,
	call ToolCall,
	agent *Employee,
	conversationID string,
) map[string]any {
	switch call.Name {
	case "delegate_task":
		return h.execDelegateTask(ctx, call.Args, agent, conversationID)
	case "hire_employee":
		return h.execHireEmployee(ctx, call.Args, agent)
	case "submit_task_result":
		return h.execSubmitResult(ctx, call.Args)
	case "review_task":
		return h.execReviewTask(ctx, call.Args, agent)
	case "list_team":
		return h.execListTeam(ctx, agent)
	case "store_memory":
		return h.execStoreMemory(ctx, call.Args, agent, conversationID)
	case "forget_memory":
		return h.execForgetMemory(ctx, call.Args)
	default:
		return map[string]any{"error": "unknown tool: " + call.Name}
	}
}

func (h *APIHandler) execStoreMemory(ctx context.Context, args map[string]any, agent *Employee, convID string) map[string]any {
	if h.esClient == nil {
		return map[string]any{"error": "memory storage not available"}
	}
	text, _ := args["memory_text"].(string)
	if text == "" {
		return map[string]any{"error": "memory_text is required"}
	}
	if err := h.esClient.IndexEmployeeMemoryDedup(ctx, agent.ID, convID, text); err != nil {
		return map[string]any{"error": "failed to store memory: " + err.Error()}
	}
	return map[string]any{"status": "remembered", "memory_text": text}
}

func (h *APIHandler) execForgetMemory(ctx context.Context, args map[string]any) map[string]any {
	if h.esClient == nil {
		return map[string]any{"error": "memory storage not available"}
	}
	memoryID, _ := args["memory_id"].(string)
	if memoryID == "" {
		return map[string]any{"error": "memory_id is required"}
	}
	if err := h.esClient.DeleteEmployeeMemory(ctx, memoryID); err != nil {
		return map[string]any{"error": "failed to forget: " + err.Error()}
	}
	return map[string]any{"status": "forgotten", "memory_id": memoryID}
}

func (h *APIHandler) execDelegateTask(ctx context.Context, args map[string]any, creator *Employee, conversationID string) map[string]any {
	if h.pgClient == nil {
		return map[string]any{"error": "database not available"}
	}

	assigneeID, _ := args["assignee_id"].(string)
	title, _ := args["title"].(string)
	goal, _ := args["goal"].(string)
	taskContext, _ := args["context"].(string)
	priority, _ := args["priority"].(string)

	if assigneeID == "" || title == "" || goal == "" {
		return map[string]any{"error": "assignee_id, title, and goal are required"}
	}

	assignee, err := h.pgClient.GetEmployee(ctx, assigneeID)
	if err != nil {
		return map[string]any{"error": "assignee not found: " + err.Error()}
	}

	if !canDelegate(creator, assignee) {
		return map[string]any{"error": fmt.Sprintf("cannot delegate to %s: outside your team hierarchy", assignee.Name)}
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

	if err := h.pgClient.CreateTask(ctx, t, nil); err != nil {
		return map[string]any{"error": "failed to create task: " + err.Error()}
	}

	if conversationID != "" {
		if err := h.pgClient.SetTaskConversationID(ctx, t.ID, conversationID); err != nil {
			slog.Error("failed to link task to conversation", "task_id", t.ID, "error", err)
		}
	}

	slog.Info("task delegated", "task_id", t.ID, "from", creator.Name, "to", assignee.Name, "title", title)
	return map[string]any{
		"status":  "created",
		"task_id": t.ID,
		"title":   t.Title,
		"assignee": map[string]any{
			"id":   assignee.ID,
			"name": assignee.Name,
		},
	}
}

func (h *APIHandler) execHireEmployee(ctx context.Context, args map[string]any, manager *Employee) map[string]any {
	if h.pgClient == nil {
		return map[string]any{"error": "database not available"}
	}

	if !hasTag(manager.Tags, "manager") && manager.Role != "CEO" {
		return map[string]any{"error": "only managers can hire employees"}
	}

	name, _ := args["name"].(string)
	title, _ := args["title"].(string)
	backstory, _ := args["backstory"].(string)
	primaryLLM, _ := args["primary_llm"].(string)

	if name == "" || title == "" || backstory == "" {
		return map[string]any{"error": "name, title, and backstory are required"}
	}

	fresh, err := h.pgClient.GetEmployee(ctx, manager.ID)
	if err != nil {
		return map[string]any{"error": "failed to load manager: " + err.Error()}
	}
	if reason, ok := checkHireDuplicate(fresh, title); !ok {
		return map[string]any{"error": reason}
	}

	emp := &Employee{
		Name:      name,
		Title:     title,
		Role:      "Custom",
		Backstory: backstory,
		Models:    []EmployeeModel{},
		Skills:    []EmployeeSkill{},
		Tags:      []string{},
		ManagerID: &manager.ID,
	}

	if primaryLLM != "" {
		emp.Models = append(emp.Models, EmployeeModel{ModelID: primaryLLM, Purpose: "primary_llm"})
	}

	if err := h.pgClient.CreateEmployee(ctx, emp); err != nil {
		return map[string]any{"error": "failed to create employee: " + err.Error()}
	}

	slog.Info("employee hired", "id", emp.ID, "name", name, "manager", manager.Name)
	return map[string]any{
		"status":      "hired",
		"employee_id": emp.ID,
		"name":        emp.Name,
		"title":       emp.Title,
		"reports_to":  manager.Name,
	}
}

func (h *APIHandler) execSubmitResult(ctx context.Context, args map[string]any) map[string]any {
	if h.pgClient == nil {
		return map[string]any{"error": "database not available"}
	}

	taskID, _ := args["task_id"].(string)
	result, _ := args["result"].(string)

	if taskID == "" || result == "" {
		return map[string]any{"error": "task_id and result are required"}
	}

	if err := h.pgClient.UpdateTask(ctx, taskID, nil, nil, nil, nil, &result); err != nil {
		return map[string]any{"error": "failed to update result: " + err.Error()}
	}

	if err := h.pgClient.UpdateTaskStatus(ctx, taskID, "needs_review", ""); err != nil {
		return map[string]any{"error": "failed to submit for review: " + err.Error()}
	}

	slog.Info("task result submitted", "task_id", taskID)
	return map[string]any{"status": "submitted_for_review", "task_id": taskID}
}

func (h *APIHandler) execReviewTask(ctx context.Context, args map[string]any, reviewer *Employee) map[string]any {
	if h.pgClient == nil {
		return map[string]any{"error": "database not available"}
	}

	taskID, _ := args["task_id"].(string)
	action, _ := args["action"].(string)
	feedback, _ := args["feedback"].(string)

	if taskID == "" || action == "" {
		return map[string]any{"error": "task_id and action are required"}
	}

	switch action {
	case "APPROVE":
		if err := h.pgClient.UpdateTaskStatus(ctx, taskID, "done", reviewer.ID); err != nil {
			return map[string]any{"error": "failed to approve: " + err.Error()}
		}
		slog.Info("task approved", "task_id", taskID, "reviewer", reviewer.Name)
		return map[string]any{"status": "approved", "task_id": taskID}

	case "REJECT":
		if feedback == "" {
			return map[string]any{"error": "feedback is required when rejecting"}
		}
		if err := h.pgClient.UpdateTaskStatus(ctx, taskID, "ready", reviewer.ID); err != nil {
			return map[string]any{"error": "failed to reject: " + err.Error()}
		}
		if _, err := h.pgClient.AddTaskComment(ctx, taskID, reviewer.ID, "REJECTED: "+feedback); err != nil {
			slog.Error("failed to add rejection comment", "task_id", taskID, "error", err)
		}
		slog.Info("task rejected", "task_id", taskID, "reviewer", reviewer.Name)
		return map[string]any{"status": "rejected", "task_id": taskID, "feedback": feedback}

	default:
		return map[string]any{"error": "action must be APPROVE or REJECT"}
	}
}

func (h *APIHandler) execListTeam(ctx context.Context, agent *Employee) map[string]any {
	if h.pgClient == nil {
		return map[string]any{"error": "database not available"}
	}

	emp, err := h.pgClient.GetEmployee(ctx, agent.ID)
	if err != nil {
		return map[string]any{"error": "failed to load employee: " + err.Error()}
	}

	var members []map[string]any
	for _, r := range emp.Reports {
		members = append(members, map[string]any{
			"id": r.ID, "name": r.Name, "title": r.Title, "role": r.Role,
		})
	}
	if members == nil {
		members = []map[string]any{}
	}

	return map[string]any{"team": members, "count": len(members)}
}

// HTTP handlers for REST API access (independent of chat)

func (h *APIHandler) HireEmployee(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}

	var body struct {
		HiringManagerID string          `json:"hiring_manager_id"`
		Name            string          `json:"name"`
		Title           string          `json:"title"`
		Role            string          `json:"role"`
		Backstory       string          `json:"backstory"`
		PrimaryLLM      string          `json:"primary_llm"`
		Skills          []EmployeeSkill `json:"skills"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if body.HiringManagerID == "" || body.Name == "" {
		writeError(w, "hiring_manager_id and name are required", http.StatusBadRequest)
		return
	}

	manager, err := h.pgClient.GetEmployee(r.Context(), body.HiringManagerID)
	if err != nil {
		writeError(w, "hiring manager not found", http.StatusNotFound)
		return
	}

	if !hasTag(manager.Tags, "manager") && manager.Role != "CEO" {
		writeError(w, "only managers can hire employees", http.StatusForbidden)
		return
	}

	if reason, ok := checkHireDuplicate(manager, body.Title); !ok {
		writeError(w, reason, http.StatusConflict)
		return
	}

	role := body.Role
	if role == "" {
		role = "Custom"
	}

	emp := &Employee{
		Name:      body.Name,
		Title:     body.Title,
		Role:      role,
		Backstory: body.Backstory,
		Models:    []EmployeeModel{},
		Skills:    body.Skills,
		Tags:      []string{},
		ManagerID: &body.HiringManagerID,
	}
	if emp.Skills == nil {
		emp.Skills = []EmployeeSkill{}
	}
	if body.PrimaryLLM != "" {
		emp.Models = append(emp.Models, EmployeeModel{ModelID: body.PrimaryLLM, Purpose: "primary_llm"})
	}

	if err := h.pgClient.CreateEmployee(r.Context(), emp); err != nil {
		writeError(w, "failed to create employee: "+err.Error(), http.StatusInternalServerError)
		return
	}

	full, err := h.pgClient.GetEmployee(r.Context(), emp.ID)
	if err != nil {
		full = emp
	}

	slog.Info("employee hired via API", "id", full.ID, "name", full.Name, "manager", manager.Name)
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, full)
}

func (h *APIHandler) DelegateTask(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}

	var body struct {
		CreatorID      string   `json:"creator_id"`
		AssigneeID     string   `json:"assignee_id"`
		Title          string   `json:"title"`
		Goal           string   `json:"goal"`
		Context        string   `json:"context"`
		Priority       string   `json:"priority"`
		ConversationID string   `json:"conversation_id"`
		Dependencies   []string `json:"dependencies"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if body.CreatorID == "" || body.AssigneeID == "" || body.Title == "" || body.Goal == "" {
		writeError(w, "creator_id, assignee_id, title, and goal are required", http.StatusBadRequest)
		return
	}

	creator, err := h.pgClient.GetEmployee(r.Context(), body.CreatorID)
	if err != nil {
		writeError(w, "creator not found", http.StatusNotFound)
		return
	}
	assignee, err := h.pgClient.GetEmployee(r.Context(), body.AssigneeID)
	if err != nil {
		writeError(w, "assignee not found", http.StatusNotFound)
		return
	}

	if !canDelegate(creator, assignee) {
		writeError(w, fmt.Sprintf("cannot delegate to %s: outside team hierarchy", assignee.Name), http.StatusForbidden)
		return
	}

	taskBody := "## Goal\n" + body.Goal
	if body.Context != "" {
		taskBody += "\n\n## Context\n" + body.Context
	}

	priority := body.Priority
	if priority == "" {
		priority = "medium"
	}

	t := &Task{
		Title:    body.Title,
		Body:     taskBody,
		Priority: priority,
		Creator:  &EmployeeBrief{ID: creator.ID, Name: creator.Name, Title: creator.Title, Role: creator.Role},
		Assignee: &EmployeeBrief{ID: assignee.ID, Name: assignee.Name, Title: assignee.Title, Role: assignee.Role},
	}

	deps := body.Dependencies
	if deps == nil {
		deps = []string{}
	}

	if err := h.pgClient.CreateTask(r.Context(), t, deps); err != nil {
		writeError(w, "failed to create task: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if body.ConversationID != "" {
		if err := h.pgClient.SetTaskConversationID(r.Context(), t.ID, body.ConversationID); err != nil {
			slog.Error("failed to link task to conversation", "task_id", t.ID, "error", err)
		}
	}

	full, err := h.pgClient.GetTask(r.Context(), t.ID)
	if err != nil {
		full = t
	}

	slog.Info("task delegated via API", "task_id", full.ID, "from", creator.Name, "to", assignee.Name)
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, full)
}
