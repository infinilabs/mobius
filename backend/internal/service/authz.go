// Package service holds the business rules shared by every entrance —
// REST handlers, interactive chat, the autonomous adapter/dispatcher, and
// MCP (plan 6.3): object-level authorization, delegation policy, rate
// limits, input caps, and the budget gate.
package service

import (
	"context"
	"fmt"

	"mobius/internal/domain"
)

// Object-level authorization shared by every agent tool path (plan 2.1): the
// autonomous adapter (InternalLLMAdapter.routeToolCall), interactive chat
// (APIHandler.executeToolCall), and MCP (MCPServer.HandleMessage) all call
// AuthorizeToolCall before dispatching a tool. Authorizing a tool happens in
// exactly one place — the toolAuthzPolicy table below — so the three paths
// cannot drift apart.

// employeeGetter is the minimal lookup the hierarchy walks need; *PGClient and
// test fakes both satisfy it.
type employeeGetter interface {
	GetEmployee(ctx context.Context, id string) (*domain.Employee, error)
}

// authzStore is what the object-level checks need to resolve the entities a
// tool call touches.
type authzStore interface {
	employeeGetter
	GetTask(ctx context.Context, id string) (*domain.Task, error)
	GetProject(ctx context.Context, id string) (*domain.Project, error)
}

// managerChainDepthCap bounds upward walks of the ManagerID chain so a cyclic
// hierarchy cannot loop forever.
const managerChainDepthCap = 10

// isInManagementChain reports whether ancestorID appears in emp's upward
// ManagerID chain, i.e. ancestorID transitively manages emp.
func isInManagementChain(ctx context.Context, g employeeGetter, ancestorID string, emp *domain.Employee) bool {
	if emp == nil || ancestorID == "" {
		return false
	}
	cur := emp.ManagerID
	for i := 0; i < managerChainDepthCap && cur != nil && *cur != ""; i++ {
		if *cur == ancestorID {
			return true
		}
		parent, err := g.GetEmployee(ctx, *cur)
		if err != nil {
			return false
		}
		cur = parent.ManagerID
	}
	return false
}

// CallerManages reports whether callerID is targetID's (transitive) manager,
// or a CEO.
func CallerManages(ctx context.Context, g employeeGetter, callerID, targetID string) bool {
	caller, err := g.GetEmployee(ctx, callerID)
	if err != nil {
		return false
	}
	if caller.Role == "CEO" {
		return true
	}
	target, err := g.GetEmployee(ctx, targetID)
	if err != nil {
		return false
	}
	return isInManagementChain(ctx, g, callerID, target)
}

// RequireTaskAccess loads the task and verifies the actor may act on it: the
// actor must be the assignee, the creator, or a manager of the assignee. This
// is the object-level gate for mutating task tools — without it any agent
// could mutate any task by id (IDOR/BOLA).
func RequireTaskAccess(ctx context.Context, s authzStore, actorID, taskID string) (*domain.Task, error) {
	if actorID == "" {
		return nil, fmt.Errorf("unauthorized")
	}
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("task not found")
	}
	if task.Assignee != nil && task.Assignee.ID == actorID {
		return task, nil
	}
	if task.Creator != nil && task.Creator.ID == actorID {
		return task, nil
	}
	if task.Assignee != nil && CallerManages(ctx, s, actorID, task.Assignee.ID) {
		return task, nil
	}
	return nil, fmt.Errorf("forbidden: not authorized for this task")
}

// RequireEmployeeAccess allows an actor to modify only itself or someone it
// manages.
func RequireEmployeeAccess(ctx context.Context, g employeeGetter, actorID, empID string) error {
	if actorID == "" {
		return fmt.Errorf("unauthorized")
	}
	if actorID == empID || CallerManages(ctx, g, actorID, empID) {
		return nil
	}
	return fmt.Errorf("forbidden: not authorized to modify this employee")
}

// RequireProjectAccess allows only the project owner or a CEO to mutate a
// project.
func RequireProjectAccess(ctx context.Context, s authzStore, actorID, projectID string) error {
	if actorID == "" {
		return fmt.Errorf("unauthorized")
	}
	project, err := s.GetProject(ctx, projectID)
	if err != nil {
		return fmt.Errorf("project not found")
	}
	if project.Owner != nil && project.Owner.ID == actorID {
		return nil
	}
	if emp, err := s.GetEmployee(ctx, actorID); err == nil && emp.Role == "CEO" {
		return nil
	}
	return fmt.Errorf("forbidden: not authorized for this project")
}

type authzRule int

const (
	authzNone authzRule = iota
	authzManagerOnly
	authzTaskAccess
	authzEmployeeAccess
	authzProjectAccess
)

// toolAuthzPolicy is THE single place a tool's object-level authorization is
// declared. Tools absent from the table carry no object rule (read-only tools,
// or tools scoped by construction to the caller's own task/project context).
// delegate_task is intentionally not listed: its policy needs the assignee and
// the parent task's depth, and lives in the shared CanDelegate /
// ExceedsDelegationDepth helpers every path already calls.
var toolAuthzPolicy = map[string]authzRule{
	"hire_employee":      authzManagerOnly,
	"submit_task_result": authzTaskAccess,
	"review_task":        authzTaskAccess,
	"update_task":        authzTaskAccess,
	"update_task_status": authzTaskAccess,
	"add_task_comment":   authzTaskAccess,
	"update_employee":    authzEmployeeAccess,
	"assign_skill":       authzEmployeeAccess,
	"unassign_skill":     authzEmployeeAccess,
	"update_project":     authzProjectAccess,
}

// AuthorizeToolCall enforces toolAuthzPolicy for one tool invocation.
// currentTaskID is the task the caller is running as (autonomous adapter and
// MCP runs); it fills in an omitted task_id argument so "act on my own task"
// shorthands are authorized against the right object. Empty target-id
// arguments are left for the handler's argument validation to reject.
func AuthorizeToolCall(ctx context.Context, s authzStore, actorID, toolName string, args map[string]any, currentTaskID string) error {
	rule, ok := toolAuthzPolicy[toolName]
	if !ok || rule == authzNone {
		return nil
	}
	if actorID == "" {
		return fmt.Errorf("unauthorized")
	}

	switch rule {
	case authzManagerOnly:
		actor, err := s.GetEmployee(ctx, actorID)
		if err != nil {
			return fmt.Errorf("caller agent not found")
		}
		if !domain.HasTag(actor.Tags, "manager") && actor.Role != "CEO" {
			return fmt.Errorf("forbidden: only managers can use %s", toolName)
		}

	case authzTaskAccess:
		taskID, _ := args["task_id"].(string)
		if taskID == "" {
			taskID = currentTaskID
		}
		if taskID == "" {
			return nil
		}
		if _, err := RequireTaskAccess(ctx, s, actorID, taskID); err != nil {
			return err
		}
		// Reassignment is delegation by another name: changing a task's assignee
		// to anyone but yourself needs the same authority as delegate_task.
		if toolName == "update_task" {
			if newAssignee, _ := args["assignee_id"].(string); newAssignee != "" && newAssignee != actorID {
				actor, err := s.GetEmployee(ctx, actorID)
				if err != nil {
					return fmt.Errorf("caller agent not found")
				}
				assignee, err := s.GetEmployee(ctx, newAssignee)
				if err != nil {
					return fmt.Errorf("assignee not found")
				}
				if !CanDelegate(ctx, s, actor, assignee) {
					return fmt.Errorf("forbidden: cannot reassign to %s: outside your team hierarchy", assignee.Name)
				}
			}
		}

	case authzEmployeeAccess:
		empID, _ := args["employee_id"].(string)
		if empID == "" {
			return nil
		}
		return RequireEmployeeAccess(ctx, s, actorID, empID)

	case authzProjectAccess:
		projectID, _ := args["project_id"].(string)
		if projectID == "" {
			return nil
		}
		return RequireProjectAccess(ctx, s, actorID, projectID)
	}
	return nil
}

// privilegedTags are the tags that grant authority: tool access
// (buildAgentTools) or delegation/hiring rights (CanDelegate, hire gates).
// Only a CEO may grant or revoke them — otherwise any agent with the
// update_employee tool could escalate itself or its reports (plan 2.2).
var privilegedTags = map[string]bool{
	"manager":            true,
	"founder":            true,
	"media_tagger":       true,
	"media_watermarker":  true,
	"playable_planner":   true,
	"playable_designer":  true,
	"playable_developer": true,
	"playable_publisher": true,
}

// ValidateTagChange refuses any change to the privileged subset of an
// employee's tags unless the actor is a CEO. Non-privileged tags are free.
func ValidateTagChange(actor *domain.Employee, current, proposed []string) error {
	if actor != nil && actor.Role == "CEO" {
		return nil
	}
	cur := make(map[string]bool, len(current))
	for _, t := range current {
		cur[t] = true
	}
	prop := make(map[string]bool, len(proposed))
	for _, t := range proposed {
		prop[t] = true
	}
	for t := range prop {
		if privilegedTags[t] && !cur[t] {
			return fmt.Errorf("forbidden: only the CEO can grant the %q tag", t)
		}
	}
	for t := range cur {
		if privilegedTags[t] && !prop[t] {
			return fmt.Errorf("forbidden: only the CEO can revoke the %q tag", t)
		}
	}
	return nil
}
