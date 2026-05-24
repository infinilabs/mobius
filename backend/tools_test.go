package main

import (
	"testing"
)

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

func strPtr(s string) *string { return &s }

func TestCanDelegate_CEOToAnyone(t *testing.T) {
	ceo := makeEmployee("CEO", []string{"executive"}, nil, nil)
	worker := makeEmployee("Custom", []string{}, strPtr("other-mgr"), nil)
	if !canDelegate(ceo, worker) {
		t.Error("CEO should be able to delegate to anyone")
	}
}

func TestCanDelegate_ManagerToDirectReport(t *testing.T) {
	mgr := makeEmployee("PM", []string{"manager"}, nil, nil)
	mgr.ID = "mgr-001"
	report := makeEmployee("Custom", []string{}, strPtr("mgr-001"), nil)
	if !canDelegate(mgr, report) {
		t.Error("manager should delegate to direct report")
	}
}

func TestCanDelegate_ManagerToPeerManager(t *testing.T) {
	mgr1 := makeEmployee("PM", []string{"manager"}, nil, nil)
	mgr2 := makeEmployee("Engineer", []string{"manager"}, strPtr("ceo-001"), nil)
	if !canDelegate(mgr1, mgr2) {
		t.Error("manager should delegate to peer manager")
	}
}

func TestCanDelegate_ManagerToOtherTeam(t *testing.T) {
	mgr := makeEmployee("PM", []string{"manager"}, nil, nil)
	mgr.ID = "mgr-001"
	other := makeEmployee("Custom", []string{}, strPtr("mgr-other"), nil)
	if canDelegate(mgr, other) {
		t.Error("manager should NOT delegate to non-report non-manager")
	}
}

func TestCanDelegate_WorkerToAnyone(t *testing.T) {
	worker := makeEmployee("Custom", []string{}, strPtr("mgr-001"), nil)
	other := makeEmployee("Custom", []string{}, strPtr("mgr-001"), nil)
	if canDelegate(worker, other) {
		t.Error("non-manager should NOT delegate to anyone")
	}
}

func TestBuildAgentTools_CEO(t *testing.T) {
	ceo := makeEmployee("CEO", []string{"executive"}, nil, nil)
	tools := buildAgentTools(ceo)

	names := toolNames(tools)
	assertContains(t, names, "delegate_task", "CEO should have delegate_task")
	assertContains(t, names, "review_task", "CEO should have review_task")
	assertContains(t, names, "submit_task_result", "CEO should have submit_task_result")
	assertContains(t, names, "store_memory", "CEO should have store_memory")
	assertNotContains(t, names, "hire_employee", "CEO should NOT have hire_employee")
}

func TestBuildAgentTools_Manager(t *testing.T) {
	mgr := makeEmployee("PM", []string{"manager"}, nil, nil)
	tools := buildAgentTools(mgr)

	names := toolNames(tools)
	assertContains(t, names, "delegate_task", "manager should have delegate_task")
	assertContains(t, names, "hire_employee", "manager should have hire_employee")
	assertContains(t, names, "review_task", "manager should have review_task")
	assertContains(t, names, "store_memory", "manager should have store_memory")
}

func TestBuildAgentTools_Worker(t *testing.T) {
	worker := makeEmployee("Custom", []string{}, strPtr("mgr-001"), nil)
	tools := buildAgentTools(worker)

	names := toolNames(tools)
	assertContains(t, names, "submit_task_result", "worker should have submit_task_result")
	assertContains(t, names, "list_team", "worker should have list_team")
	assertContains(t, names, "store_memory", "worker should have store_memory")
	assertContains(t, names, "forget_memory", "worker should have forget_memory")
	assertNotContains(t, names, "delegate_task", "worker should NOT have delegate_task")
	assertNotContains(t, names, "hire_employee", "worker should NOT have hire_employee")
	assertNotContains(t, names, "review_task", "worker should NOT have review_task")
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
	// "senior" and "lead" are noise words — should not count toward overlap
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
