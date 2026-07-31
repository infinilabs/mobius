package main

import (
	"context"
	"testing"
)

func strPtr(s string) *string { return &s }

func makeEmployee(role string, tags []string, managerID *string, reports []EmployeeBrief) *Employee {
	return &Employee{
		ID:        "emp-001",
		Name:      "Test",
		Title:     "Test Title",
		Role:      role,
		Tags:      tags,
		ManagerID: managerID,
		Reports:   reports,
	}
}

// delegationStore returns the shared hierarchy fixture used by the canDelegate
// tests (defined in authz_test.go): ceo → mgr-a → worker-a1 → worker-a1x,
// ceo → mgr-b → worker-b1.
func delegationStore() *fakeAuthzStore { return newHierarchyStore() }

func TestCanDelegate_CEOToAnyone(t *testing.T) {
	s := delegationStore()
	ceo := makeEmployee("CEO", []string{"executive"}, nil, nil)
	worker := makeEmployee("Custom", []string{}, strPtr("other-mgr"), nil)
	worker.ID = "emp-002"
	if !canDelegate(context.Background(), s, ceo, worker) {
		t.Error("CEO should be able to delegate to anyone")
	}
}

// Self-delegation is a task loop that never converges: the same agent keeps
// re-receiving its own work (plan 1.1). Refused for every role, CEO included.
func TestCanDelegate_RefusesSelfDelegation(t *testing.T) {
	s := delegationStore()
	ceo := makeEmployee("CEO", []string{"executive"}, nil, nil)
	if canDelegate(context.Background(), s, ceo, ceo) {
		t.Error("CEO should NOT delegate to themselves")
	}
	mgr := makeEmployee("PM", []string{"manager"}, nil, nil)
	if canDelegate(context.Background(), s, mgr, mgr) {
		t.Error("manager should NOT delegate to themselves")
	}
}

// The depth bound is what stops an A→B→A delegation ping-pong: each hop
// increments the child's depth, so the chain dies at maxDelegationDepth even
// though each individual hop looks legitimate (plan 1.1).
func TestExceedsDelegationDepth(t *testing.T) {
	if exceedsDelegationDepth(0) {
		t.Error("root task (depth 0) should be allowed to delegate")
	}
	if exceedsDelegationDepth(maxDelegationDepth - 1) {
		t.Error("delegating up to the max depth should be allowed")
	}
	if !exceedsDelegationDepth(maxDelegationDepth) {
		t.Errorf("delegating past depth %d should be refused", maxDelegationDepth)
	}
}

func TestCanDelegate_ManagerToDirectReport(t *testing.T) {
	s := delegationStore()
	if !canDelegate(context.Background(), s, s.employees["mgr-a"], s.employees["worker-a1"]) {
		t.Error("manager should delegate to direct report")
	}
}

// A delegated task may land on a report who is themselves a manager; that
// manager must be able to re-delegate deeper into their own subtree.
func TestCanDelegate_ManagerToDescendant(t *testing.T) {
	s := delegationStore()
	if !canDelegate(context.Background(), s, s.employees["mgr-a"], s.employees["worker-a1x"]) {
		t.Error("manager should delegate to a transitive descendant")
	}
}

// Lateral manager→manager delegation across teams is refused (plan 2.4): a
// compromised or confused manager agent must not be able to inject work into
// unrelated teams just because the target also carries the manager tag.
func TestCanDelegate_ManagerToPeerManagerRefused(t *testing.T) {
	s := delegationStore()
	if canDelegate(context.Background(), s, s.employees["mgr-a"], s.employees["mgr-b"]) {
		t.Error("manager should NOT delegate to a peer manager outside their subtree")
	}
}

// Delegating upward to the CEO is refused: the hierarchy only flows down.
func TestCanDelegate_ManagerToCEORefused(t *testing.T) {
	s := delegationStore()
	if canDelegate(context.Background(), s, s.employees["mgr-a"], s.employees["ceo"]) {
		t.Error("manager should NOT delegate to the CEO")
	}
}

func TestCanDelegate_ManagerToOtherTeam(t *testing.T) {
	s := delegationStore()
	if canDelegate(context.Background(), s, s.employees["mgr-a"], s.employees["worker-b1"]) {
		t.Error("manager should NOT delegate to another team's worker")
	}
}

func TestCanDelegate_WorkerToAnyone(t *testing.T) {
	s := delegationStore()
	if canDelegate(context.Background(), s, s.employees["worker-a1x"], s.employees["worker-b1"]) {
		t.Error("non-manager should NOT delegate to anyone")
	}
}

func TestBuildAgentTools_CEO(t *testing.T) {
	ceo := makeEmployee("CEO", []string{"executive"}, nil, nil)
	tools := buildAgentTools(ceo, nil)

	names := toolNames(tools)
	for _, required := range []string{
		"delegate_task", "review_task", "submit_task_result", "store_memory",
		"create_project", "list_tasks", "get_task", "update_task", "update_task_status",
		"list_projects", "list_employees", "get_employee", "update_employee",
		"list_skills", "assign_skill", "unassign_skill",
		"list_prompts", "create_prompt", "update_prompt", "delete_prompt",
		"verify_deliverable", "add_task_comment", "update_project",
	} {
		assertContains(t, names, required, "CEO should have "+required)
	}
	assertNotContains(t, names, "hire_employee", "CEO should NOT have hire_employee")
}

func TestBuildAgentTools_Manager(t *testing.T) {
	mgr := makeEmployee("PM", []string{"manager"}, nil, nil)
	tools := buildAgentTools(mgr, nil)

	names := toolNames(tools)
	for _, required := range []string{
		"delegate_task", "hire_employee", "review_task", "store_memory",
		"list_tasks", "list_projects", "list_employees", "update_task_status",
		"list_skills", "assign_skill", "list_prompts", "create_prompt",
		"verify_deliverable", "get_task", "update_task",
	} {
		assertContains(t, names, required, "manager should have "+required)
	}
}

func TestBuildAgentTools_Founder(t *testing.T) {
	founder := makeEmployee("PM", []string{"founder"}, nil, nil)
	tools := buildAgentTools(founder, nil)

	names := toolNames(tools)
	assertContains(t, names, "create_project", "founder should have create_project")
	assertContains(t, names, "list_tasks", "founder should have list_tasks")
	assertContains(t, names, "list_prompts", "founder should have list_prompts")
	assertNotContains(t, names, "delegate_task", "founder without manager tag should NOT have delegate_task")
	assertNotContains(t, names, "hire_employee", "founder without manager tag should NOT have hire_employee")
}

func TestBuildAgentTools_Worker(t *testing.T) {
	worker := makeEmployee("Custom", []string{}, strPtr("mgr-001"), nil)
	tools := buildAgentTools(worker, nil)

	names := toolNames(tools)
	assertContains(t, names, "submit_task_result", "worker should have submit_task_result")
	assertContains(t, names, "list_team", "worker should have list_team")
	assertContains(t, names, "store_memory", "worker should have store_memory")
	assertContains(t, names, "forget_memory", "worker should have forget_memory")
	for _, forbidden := range []string{
		"delegate_task", "hire_employee", "review_task",
		"create_project", "list_tasks", "list_projects", "list_employees",
		"list_skills", "list_prompts", "update_task_status",
	} {
		assertNotContains(t, names, forbidden, "worker should NOT have "+forbidden)
	}
}

func TestBuildAgentTools_WithProjectContext(t *testing.T) {
	worker := makeEmployee("Custom", []string{}, strPtr("mgr-001"), nil)
	projectID := "proj-001"
	task := &Task{ID: "task-001", ProjectID: &projectID}
	tools := buildAgentTools(worker, task)

	names := toolNames(tools)
	for _, required := range []string{
		"write_project_file", "read_project_file",
		"search_project_assets", "list_project_assets", "run_project_command",
	} {
		assertContains(t, names, required, "project task should have "+required)
	}
}

func TestBuildAgentTools_WithoutProjectContext(t *testing.T) {
	worker := makeEmployee("Custom", []string{}, strPtr("mgr-001"), nil)
	tools := buildAgentTools(worker, nil)

	names := toolNames(tools)
	assertNotContains(t, names, "write_project_file", "no-project task should NOT have write_project_file")
	assertNotContains(t, names, "read_project_file", "no-project task should NOT have read_project_file")
	assertNotContains(t, names, "run_project_command", "no-project task should NOT have run_project_command")
}

func TestBuildAgentTools_CEONoDuplicates(t *testing.T) {
	ceo := makeEmployee("CEO", []string{"executive", "manager", "founder"}, nil, nil)
	projectID := "proj-001"
	task := &Task{ID: "task-001", ProjectID: &projectID}
	tools := buildAgentTools(ceo, task)

	counts := make(map[string]int)
	for _, td := range tools {
		counts[td.Name]++
	}
	for name, count := range counts {
		if count > 1 {
			t.Errorf("tool %q appears %d times, expected 1", name, count)
		}
	}
}

func TestCheckHireDuplicate_ExactMatch(t *testing.T) {
	mgr := makeEmployee("PM", []string{"manager"}, nil, []EmployeeBrief{
		{ID: "r1", Name: "Alice", Title: "Rust Developer", Role: "Custom"},
	})
	reason, ok := checkHireDuplicate(mgr, "Rust Developer")
	if ok {
		t.Error("should block exact duplicate title")
	}
	if reason == "" {
		t.Error("should provide reason for blocking")
	}
}

func TestCheckHireDuplicate_SimilarTitle(t *testing.T) {
	mgr := makeEmployee("PM", []string{"manager"}, nil, []EmployeeBrief{
		{ID: "r1", Name: "Alice", Title: "Rust Systems Developer", Role: "Custom"},
	})
	_, ok := checkHireDuplicate(mgr, "Rust Backend Developer")
	if ok {
		t.Error("should block similar overlapping title")
	}
}

func TestCheckHireDuplicate_DifferentTitle(t *testing.T) {
	mgr := makeEmployee("PM", []string{"manager"}, nil, []EmployeeBrief{
		{ID: "r1", Name: "Alice", Title: "Rust Developer", Role: "Custom"},
	})
	_, ok := checkHireDuplicate(mgr, "React Frontend Engineer")
	if !ok {
		t.Error("should allow distinct title")
	}
}

func TestCheckHireDuplicate_TeamFull(t *testing.T) {
	reports := make([]EmployeeBrief, maxDirectReports)
	for i := range reports {
		reports[i] = EmployeeBrief{ID: "r", Name: "R", Title: "T", Role: "Custom"}
	}
	mgr := makeEmployee("PM", []string{"manager"}, nil, reports)
	_, ok := checkHireDuplicate(mgr, "New Specialist")
	if ok {
		t.Errorf("should block when team has %d reports (max)", maxDirectReports)
	}
}

func TestTitleOverlap_HighOverlap(t *testing.T) {
	if !titleOverlap("rust systems developer", "rust backend developer") {
		t.Error("expected overlap for similar titles")
	}
}

func TestTitleOverlap_NoOverlap(t *testing.T) {
	if titleOverlap("rust systems developer", "react frontend engineer") {
		t.Error("expected no overlap for distinct titles")
	}
}

func TestTitleOverlap_NoiseWords(t *testing.T) {
	if titleOverlap("senior engineer", "lead designer") {
		t.Error("noise words should not cause overlap")
	}
}

func TestHasTag(t *testing.T) {
	tags := []string{"manager", "founder"}
	if !hasTag(tags, "manager") {
		t.Error("should find existing tag")
	}
	if hasTag(tags, "executive") {
		t.Error("should not find missing tag")
	}
	if hasTag(nil, "manager") {
		t.Error("nil tags should return false")
	}
}

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
	h.pgClient = &PGClient{}
	result := h.execGetEmployeeTool(context.Background(), map[string]any{})
	if _, ok := result["error"]; !ok {
		t.Error("should return error when employee_id missing")
	}
}

func TestExecUpdateTaskStatusTool_MissingArgs(t *testing.T) {
	h := makeHandler()
	h.pgClient = &PGClient{}
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
	h.pgClient = &PGClient{}
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
	h.esClient = &ESClient{}
	result := h.execCreatePromptTool(context.Background(), map[string]any{})
	if _, ok := result["error"]; !ok {
		t.Error("should return error when title and content missing")
	}
}

func TestExecDeletePromptTool_MissingID(t *testing.T) {
	h := makeHandler()
	h.esClient = &ESClient{}
	result := h.execDeletePromptTool(context.Background(), map[string]any{})
	if _, ok := result["error"]; !ok {
		t.Error("should return error when prompt_id missing")
	}
}

func TestExecAssignSkillTool_MissingArgs(t *testing.T) {
	h := makeHandler()
	h.pgClient = &PGClient{}
	result := h.execAssignSkillTool(context.Background(), map[string]any{"employee_id": "x"})
	if _, ok := result["error"]; !ok {
		t.Error("should return error when skill_id missing")
	}
}

func TestExecUpdateTaskTool_MissingTaskID(t *testing.T) {
	h := makeHandler()
	h.pgClient = &PGClient{}
	result := h.execUpdateTaskTool(context.Background(), map[string]any{})
	if _, ok := result["error"]; !ok {
		t.Error("should return error when task_id missing")
	}
}

func TestExecAddTaskCommentTool_MissingFields(t *testing.T) {
	h := makeHandler()
	h.pgClient = &PGClient{}
	result := h.execAddTaskCommentTool(context.Background(), map[string]any{"task_id": "x"}, makeEmployee("CEO", nil, nil, nil))
	if _, ok := result["error"]; !ok {
		t.Error("should return error when content missing")
	}
}

func TestExecUpdateProjectTool_MissingID(t *testing.T) {
	h := makeHandler()
	h.pgClient = &PGClient{}
	result := h.execUpdateProjectTool(context.Background(), map[string]any{})
	if _, ok := result["error"]; !ok {
		t.Error("should return error when project_id missing")
	}
}

func TestExecUpdateEmployeeTool_MissingID(t *testing.T) {
	h := makeHandler()
	h.pgClient = &PGClient{}
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
	h.pgClient = &PGClient{}
	result := h.execGetTaskTool(context.Background(), map[string]any{})
	if _, ok := result["error"]; !ok {
		t.Error("should return error when task_id missing")
	}
}

// helpers

func toolNames(tools []ToolDef) map[string]bool {
	m := make(map[string]bool, len(tools))
	for _, t := range tools {
		m[t.Name] = true
	}
	return m
}

func assertContains(t *testing.T, m map[string]bool, key, msg string) {
	t.Helper()
	if !m[key] {
		t.Error(msg)
	}
}

func assertNotContains(t *testing.T, m map[string]bool, key, msg string) {
	t.Helper()
	if m[key] {
		t.Error(msg)
	}
}
