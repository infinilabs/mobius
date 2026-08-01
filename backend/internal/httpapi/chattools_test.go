package httpapi

import (
	"context"
	"mobius/internal/domain"
	"mobius/internal/search"
	"mobius/internal/storage/postgres"
	"testing"
)

func strPtr(s string) *string { return &s }

func makeEmployee(role string, tags []string, managerID *string, reports []domain.EmployeeBrief) *domain.Employee {
	return &domain.Employee{
		ID:        "emp-001",
		Name:      "Test",
		Title:     "Test Title",
		Role:      role,
		Tags:      tags,
		ManagerID: managerID,
		Reports:   reports,
	}
}

// Self-delegation is a task loop that never converges: the same agent keeps
// re-receiving its own work (plan 1.1). Refused for every role, CEO included.

// The depth bound is what stops an A→B→A delegation ping-pong: each hop
// increments the child's depth, so the chain dies at maxDelegationDepth even
// though each individual hop looks legitimate (plan 1.1).

// A delegated task may land on a report who is themselves a manager; that
// manager must be able to re-delegate deeper into their own subtree.

// Lateral manager→manager delegation across teams is refused (plan 2.4): a
// compromised or confused manager agent must not be able to inject work into
// unrelated teams just because the target also carries the manager tag.

// Delegating upward to the CEO is refused: the hierarchy only flows down.

// --- Handler arg validation tests (nil client) ---

func makeHandler() *APIHandler {
	return &APIHandler{}
}

func TestExecListTasksTool_NilDB(t *testing.T) {
	h := makeHandler()
	result := h.execListTasksTool(context.Background(), map[string]any{})
	if _, ok := result["error"]; !ok {
		t.Error("should return error with nil pgClient")
	}
}

func TestExecListProjectsTool_NilDB(t *testing.T) {
	h := makeHandler()
	result := h.execListProjectsTool(context.Background())
	if _, ok := result["error"]; !ok {
		t.Error("should return error with nil pgClient")
	}
}

func TestExecListEmployeesTool_NilDB(t *testing.T) {
	h := makeHandler()
	result := h.execListEmployeesTool(context.Background())
	if _, ok := result["error"]; !ok {
		t.Error("should return error with nil pgClient")
	}
}

func TestExecGetEmployeeTool_MissingID(t *testing.T) {
	h := makeHandler()
	h.pgClient = &postgres.Client{}
	result := h.execGetEmployeeTool(context.Background(), map[string]any{})
	if _, ok := result["error"]; !ok {
		t.Error("should return error when employee_id missing")
	}
}

func TestExecUpdateTaskStatusTool_MissingArgs(t *testing.T) {
	h := makeHandler()
	h.pgClient = &postgres.Client{}
	result := h.execUpdateTaskStatusTool(context.Background(), map[string]any{}, makeEmployee("CEO", nil, nil, nil))
	if _, ok := result["error"]; !ok {
		t.Error("should return error when task_id and status missing")
	}
}

func TestExecCreateProjectTool_NilDB(t *testing.T) {
	h := makeHandler()
	result := h.execCreateProject(context.Background(), map[string]any{"name": "test"}, makeEmployee("CEO", nil, nil, nil), "")
	if _, ok := result["error"]; !ok {
		t.Error("should return error with nil pgClient")
	}
}

func TestExecCreateProjectTool_MissingName(t *testing.T) {
	h := makeHandler()
	h.pgClient = &postgres.Client{}
	result := h.execCreateProject(context.Background(), map[string]any{}, makeEmployee("CEO", nil, nil, nil), "")
	if _, ok := result["error"]; !ok {
		t.Error("should return error when name missing")
	}
}

func TestExecCreatePromptTool_NilES(t *testing.T) {
	h := makeHandler()
	result := h.execCreatePromptTool(context.Background(), map[string]any{"title": "t", "content": "c"})
	if _, ok := result["error"]; !ok {
		t.Error("should return error with nil esClient")
	}
}

func TestExecCreatePromptTool_MissingFields(t *testing.T) {
	h := makeHandler()
	h.esClient = &search.Client{}
	result := h.execCreatePromptTool(context.Background(), map[string]any{})
	if _, ok := result["error"]; !ok {
		t.Error("should return error when title and content missing")
	}
}

func TestExecDeletePromptTool_MissingID(t *testing.T) {
	h := makeHandler()
	h.esClient = &search.Client{}
	result := h.execDeletePromptTool(context.Background(), map[string]any{})
	if _, ok := result["error"]; !ok {
		t.Error("should return error when prompt_id missing")
	}
}

func TestExecAssignSkillTool_MissingArgs(t *testing.T) {
	h := makeHandler()
	h.pgClient = &postgres.Client{}
	result := h.execAssignSkillTool(context.Background(), map[string]any{"employee_id": "x"})
	if _, ok := result["error"]; !ok {
		t.Error("should return error when skill_id missing")
	}
}

func TestExecUpdateTaskTool_MissingTaskID(t *testing.T) {
	h := makeHandler()
	h.pgClient = &postgres.Client{}
	result := h.execUpdateTaskTool(context.Background(), map[string]any{})
	if _, ok := result["error"]; !ok {
		t.Error("should return error when task_id missing")
	}
}

func TestExecAddTaskCommentTool_MissingFields(t *testing.T) {
	h := makeHandler()
	h.pgClient = &postgres.Client{}
	result := h.execAddTaskCommentTool(context.Background(), map[string]any{"task_id": "x"}, makeEmployee("CEO", nil, nil, nil))
	if _, ok := result["error"]; !ok {
		t.Error("should return error when content missing")
	}
}

func TestExecUpdateProjectTool_MissingID(t *testing.T) {
	h := makeHandler()
	h.pgClient = &postgres.Client{}
	result := h.execUpdateProjectTool(context.Background(), map[string]any{})
	if _, ok := result["error"]; !ok {
		t.Error("should return error when project_id missing")
	}
}

func TestExecUpdateEmployeeTool_MissingID(t *testing.T) {
	h := makeHandler()
	h.pgClient = &postgres.Client{}
	result := h.execUpdateEmployeeTool(context.Background(), map[string]any{}, makeEmployee("CEO", nil, nil, nil))
	if _, ok := result["error"]; !ok {
		t.Error("should return error when employee_id missing")
	}
}

func TestExecListSkillsTool_NilES(t *testing.T) {
	h := makeHandler()
	result := h.execListSkillsTool(context.Background(), map[string]any{})
	if _, ok := result["error"]; !ok {
		t.Error("should return error with nil esClient")
	}
}

func TestExecGetTaskTool_MissingID(t *testing.T) {
	h := makeHandler()
	h.pgClient = &postgres.Client{}
	result := h.execGetTaskTool(context.Background(), map[string]any{})
	if _, ok := result["error"]; !ok {
		t.Error("should return error when task_id missing")
	}
}

// helpers
