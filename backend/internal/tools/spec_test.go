package tools

import (
	"testing"

	"mobius/internal/domain"
	"mobius/internal/llm"
)

func specStrPtr(s string) *string { return &s }

func specEmployee(role string, tags []string, managerID *string, reports []domain.EmployeeBrief) *domain.Employee {
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

func TestBuildAgentTools_CEO(t *testing.T) {
	ceo := specEmployee("CEO", []string{"executive"}, nil, nil)
	tools := BuildAgentTools(ceo, nil)

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
	mgr := specEmployee("PM", []string{"manager"}, nil, nil)
	tools := BuildAgentTools(mgr, nil)

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
	founder := specEmployee("PM", []string{"founder"}, nil, nil)
	tools := BuildAgentTools(founder, nil)

	names := toolNames(tools)
	assertContains(t, names, "create_project", "founder should have create_project")
	assertContains(t, names, "list_tasks", "founder should have list_tasks")
	assertContains(t, names, "list_prompts", "founder should have list_prompts")
	assertNotContains(t, names, "delegate_task", "founder without manager tag should NOT have delegate_task")
	assertNotContains(t, names, "hire_employee", "founder without manager tag should NOT have hire_employee")
}

func TestBuildAgentTools_Worker(t *testing.T) {
	worker := specEmployee("Custom", []string{}, specStrPtr("mgr-001"), nil)
	tools := BuildAgentTools(worker, nil)

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
	worker := specEmployee("Custom", []string{}, specStrPtr("mgr-001"), nil)
	projectID := "proj-001"
	task := &domain.Task{ID: "task-001", ProjectID: &projectID}
	tools := BuildAgentTools(worker, task)

	names := toolNames(tools)
	for _, required := range []string{
		"write_project_file", "read_project_file",
		"search_project_assets", "list_project_assets", "run_project_command",
	} {
		assertContains(t, names, required, "project task should have "+required)
	}
}

func TestBuildAgentTools_WithoutProjectContext(t *testing.T) {
	worker := specEmployee("Custom", []string{}, specStrPtr("mgr-001"), nil)
	tools := BuildAgentTools(worker, nil)

	names := toolNames(tools)
	assertNotContains(t, names, "write_project_file", "no-project task should NOT have write_project_file")
	assertNotContains(t, names, "read_project_file", "no-project task should NOT have read_project_file")
	assertNotContains(t, names, "run_project_command", "no-project task should NOT have run_project_command")
}

func TestBuildAgentTools_CEONoDuplicates(t *testing.T) {
	ceo := specEmployee("CEO", []string{"executive", "manager", "founder"}, nil, nil)
	projectID := "proj-001"
	task := &domain.Task{ID: "task-001", ProjectID: &projectID}
	tools := BuildAgentTools(ceo, task)

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
	mgr := specEmployee("PM", []string{"manager"}, nil, []domain.EmployeeBrief{
		{ID: "r1", Name: "Alice", Title: "Rust Developer", Role: "Custom"},
	})
	reason, ok := CheckHireDuplicate(mgr, "Rust Developer")
	if ok {
		t.Error("should block exact duplicate title")
	}
	if reason == "" {
		t.Error("should provide reason for blocking")
	}
}

func TestCheckHireDuplicate_SimilarTitle(t *testing.T) {
	mgr := specEmployee("PM", []string{"manager"}, nil, []domain.EmployeeBrief{
		{ID: "r1", Name: "Alice", Title: "Rust Systems Developer", Role: "Custom"},
	})
	_, ok := CheckHireDuplicate(mgr, "Rust Backend Developer")
	if ok {
		t.Error("should block similar overlapping title")
	}
}

func TestCheckHireDuplicate_DifferentTitle(t *testing.T) {
	mgr := specEmployee("PM", []string{"manager"}, nil, []domain.EmployeeBrief{
		{ID: "r1", Name: "Alice", Title: "Rust Developer", Role: "Custom"},
	})
	_, ok := CheckHireDuplicate(mgr, "React Frontend Engineer")
	if !ok {
		t.Error("should allow distinct title")
	}
}

func TestCheckHireDuplicate_TeamFull(t *testing.T) {
	reports := make([]domain.EmployeeBrief, maxDirectReports)
	for i := range reports {
		reports[i] = domain.EmployeeBrief{ID: "r", Name: "R", Title: "T", Role: "Custom"}
	}
	mgr := specEmployee("PM", []string{"manager"}, nil, reports)
	_, ok := CheckHireDuplicate(mgr, "New Specialist")
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
	if !domain.HasTag(tags, "manager") {
		t.Error("should find existing tag")
	}
	if domain.HasTag(tags, "executive") {
		t.Error("should not find missing tag")
	}
	if domain.HasTag(nil, "manager") {
		t.Error("nil tags should return false")
	}
}

func toolNames(tools []llm.ToolDef) map[string]bool {
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

func TestBuildAgentTools_MediaTagger(t *testing.T) {
	tagger := specEmployee("Custom", []string{"media_tagger"}, specStrPtr("mgr-001"), nil)
	names := toolNames(BuildAgentTools(tagger, nil))
	for _, required := range []string{"tag_media", "get_tag_results", "query_tags", "add_to_creative_repo"} {
		assertContains(t, names, required, "media_tagger should have "+required)
	}
}

func TestBuildAgentTools_NonTaggerHasNoTaggingTools(t *testing.T) {
	worker := specEmployee("Custom", []string{}, specStrPtr("mgr-001"), nil)
	names := toolNames(BuildAgentTools(worker, nil))
	for _, forbidden := range []string{"tag_media", "get_tag_results", "query_tags", "add_to_creative_repo"} {
		assertNotContains(t, names, forbidden, "worker without media_tagger should NOT have "+forbidden)
	}
	// A CEO without the tag also must not get tagging tools (capability, not seniority).
	ceo := specEmployee("CEO", []string{"executive", "manager"}, nil, nil)
	ceoNames := toolNames(BuildAgentTools(ceo, nil))
	assertNotContains(t, ceoNames, "tag_media", "CEO without media_tagger should NOT have tag_media")
}

func TestBuildAgentTools_Watermarker(t *testing.T) {
	watermarker := specEmployee("Custom", []string{"media_watermarker"}, specStrPtr("mgr-001"), nil)
	names := toolNames(BuildAgentTools(watermarker, nil))
	for _, required := range []string{"watermark_assets", "verify_watermark"} {
		assertContains(t, names, required, "media_watermarker should have "+required)
	}
}

func TestBuildAgentTools_NonWatermarkerHasNoTools(t *testing.T) {
	worker := specEmployee("Custom", []string{}, specStrPtr("mgr-001"), nil)
	names := toolNames(BuildAgentTools(worker, nil))
	for _, forbidden := range []string{"watermark_assets", "verify_watermark"} {
		assertNotContains(t, names, forbidden, "worker without media_watermarker should NOT have "+forbidden)
	}
}
