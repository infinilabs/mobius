package main

import (
	"context"
	"fmt"
	"testing"
)

// fakeAuthzStore is an in-memory authzStore so authorization rules can be
// tested without Postgres.
type fakeAuthzStore struct {
	employees map[string]*Employee
	tasks     map[string]*Task
	projects  map[string]*Project
}

func (f *fakeAuthzStore) GetEmployee(_ context.Context, id string) (*Employee, error) {
	if e, ok := f.employees[id]; ok {
		return e, nil
	}
	return nil, fmt.Errorf("employee not found: %s", id)
}

func (f *fakeAuthzStore) GetTask(_ context.Context, id string) (*Task, error) {
	if t, ok := f.tasks[id]; ok {
		return t, nil
	}
	return nil, fmt.Errorf("task not found: %s", id)
}

func (f *fakeAuthzStore) GetProject(_ context.Context, id string) (*Project, error) {
	if p, ok := f.projects[id]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("project not found: %s", id)
}

// newHierarchyStore builds: ceo → mgr-a → worker-a1 → worker-a1x
//                               → mgr-b → worker-b1
func newHierarchyStore() *fakeAuthzStore {
	emp := func(id, role string, tags []string, managerID *string) *Employee {
		return &Employee{ID: id, Name: id, Role: role, Tags: tags, ManagerID: managerID}
	}
	return &fakeAuthzStore{
		employees: map[string]*Employee{
			"ceo":        emp("ceo", "CEO", []string{"executive"}, nil),
			"mgr-a":      emp("mgr-a", "PM", []string{"manager"}, strPtr("ceo")),
			"mgr-b":      emp("mgr-b", "PM", []string{"manager"}, strPtr("ceo")),
			"worker-a1":  emp("worker-a1", "Custom", []string{"manager"}, strPtr("mgr-a")),
			"worker-a1x": emp("worker-a1x", "Custom", []string{}, strPtr("worker-a1")),
			"worker-b1":  emp("worker-b1", "Custom", []string{}, strPtr("mgr-b")),
		},
		tasks:    map[string]*Task{},
		projects: map[string]*Project{},
	}
}

func TestCallerManages(t *testing.T) {
	s := newHierarchyStore()
	ctx := context.Background()

	tests := []struct {
		name     string
		caller   string
		target   string
		expected bool
	}{
		{"CEO manages anyone", "ceo", "worker-b1", true},
		{"direct manager", "mgr-a", "worker-a1", true},
		{"transitive manager", "mgr-a", "worker-a1x", true},
		{"lateral manager refused", "mgr-b", "worker-a1", false},
		{"report does not manage boss", "worker-a1", "mgr-a", false},
		{"unknown caller refused", "ghost", "worker-a1", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := callerManages(ctx, s, tc.caller, tc.target); got != tc.expected {
				t.Errorf("callerManages(%s, %s) = %v, want %v", tc.caller, tc.target, got, tc.expected)
			}
		})
	}
}

// A cyclic ManagerID chain must terminate, not loop forever.
func TestCallerManages_CycleSafe(t *testing.T) {
	s := newHierarchyStore()
	s.employees["cyc-1"] = &Employee{ID: "cyc-1", Role: "Custom", ManagerID: strPtr("cyc-2")}
	s.employees["cyc-2"] = &Employee{ID: "cyc-2", Role: "Custom", ManagerID: strPtr("cyc-1")}
	if callerManages(context.Background(), s, "mgr-a", "cyc-1") {
		t.Error("cyclic chain should not grant management")
	}
}

func TestRequireTaskAccess(t *testing.T) {
	s := newHierarchyStore()
	s.tasks["t1"] = &Task{
		ID:       "t1",
		Creator:  &EmployeeBrief{ID: "mgr-a"},
		Assignee: &EmployeeBrief{ID: "worker-a1"},
	}
	ctx := context.Background()

	tests := []struct {
		name    string
		actor   string
		allowed bool
	}{
		{"assignee allowed", "worker-a1", true},
		{"creator allowed", "mgr-a", true},
		{"manager of assignee allowed", "ceo", true},
		{"unrelated agent refused", "worker-b1", false},
		{"lateral manager refused", "mgr-b", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := requireTaskAccess(ctx, s, tc.actor, "t1")
			if tc.allowed && err != nil {
				t.Errorf("expected access for %s, got %v", tc.actor, err)
			}
			if !tc.allowed && err == nil {
				t.Errorf("expected refusal for %s", tc.actor)
			}
		})
	}

	if _, err := requireTaskAccess(ctx, s, "", "t1"); err == nil {
		t.Error("empty actor must be unauthorized")
	}
	if _, err := requireTaskAccess(ctx, s, "mgr-a", "missing"); err == nil {
		t.Error("missing task must be refused")
	}
}

func TestRequireEmployeeAccess(t *testing.T) {
	s := newHierarchyStore()
	ctx := context.Background()

	if err := requireEmployeeAccess(ctx, s, "worker-a1", "worker-a1"); err != nil {
		t.Errorf("self access should be allowed: %v", err)
	}
	if err := requireEmployeeAccess(ctx, s, "mgr-a", "worker-a1x"); err != nil {
		t.Errorf("transitive manager should be allowed: %v", err)
	}
	if err := requireEmployeeAccess(ctx, s, "worker-b1", "worker-a1"); err == nil {
		t.Error("unrelated agent should be refused")
	}
}

func TestRequireProjectAccess(t *testing.T) {
	s := newHierarchyStore()
	s.projects["p1"] = &Project{ID: "p1", Owner: &EmployeeBrief{ID: "mgr-a"}}
	ctx := context.Background()

	if err := requireProjectAccess(ctx, s, "mgr-a", "p1"); err != nil {
		t.Errorf("owner should be allowed: %v", err)
	}
	if err := requireProjectAccess(ctx, s, "ceo", "p1"); err != nil {
		t.Errorf("CEO should be allowed: %v", err)
	}
	if err := requireProjectAccess(ctx, s, "mgr-b", "p1"); err == nil {
		t.Error("non-owner should be refused")
	}
}

// The central gate is the single authorization entry point for all three tool
// paths (plan 2.1); these tests pin the per-rule behavior.
func TestAuthorizeToolCall(t *testing.T) {
	s := newHierarchyStore()
	s.tasks["t1"] = &Task{
		ID:       "t1",
		Creator:  &EmployeeBrief{ID: "mgr-a"},
		Assignee: &EmployeeBrief{ID: "worker-a1"},
	}
	ctx := context.Background()

	t.Run("read-only tool has no object rule", func(t *testing.T) {
		if err := authorizeToolCall(ctx, s, "worker-b1", "list_tasks", map[string]any{}, ""); err != nil {
			t.Errorf("list_tasks should not be gated: %v", err)
		}
	})

	t.Run("submit on foreign task refused", func(t *testing.T) {
		err := authorizeToolCall(ctx, s, "worker-b1", "submit_task_result",
			map[string]any{"task_id": "t1", "result": "x"}, "")
		if err == nil {
			t.Error("stranger submitting another team's task must be refused")
		}
	})

	t.Run("reject by unrelated agent refused", func(t *testing.T) {
		err := authorizeToolCall(ctx, s, "worker-b1", "review_task",
			map[string]any{"task_id": "t1", "action": "REJECT", "feedback": "no"}, "")
		if err == nil {
			t.Error("stranger rejecting another team's task must be refused")
		}
	})

	t.Run("review by creator allowed", func(t *testing.T) {
		err := authorizeToolCall(ctx, s, "mgr-a", "review_task",
			map[string]any{"task_id": "t1", "action": "REJECT", "feedback": "redo"}, "")
		if err != nil {
			t.Errorf("creator review should be allowed: %v", err)
		}
	})

	t.Run("empty task_id falls back to current task", func(t *testing.T) {
		err := authorizeToolCall(ctx, s, "worker-a1", "submit_task_result",
			map[string]any{"result": "x"}, "t1")
		if err != nil {
			t.Errorf("assignee submitting own current task should pass: %v", err)
		}
	})

	t.Run("hire requires manager", func(t *testing.T) {
		if err := authorizeToolCall(ctx, s, "worker-b1", "hire_employee", map[string]any{}, ""); err == nil {
			t.Error("non-manager hire must be refused")
		}
		if err := authorizeToolCall(ctx, s, "mgr-a", "hire_employee", map[string]any{}, ""); err != nil {
			t.Errorf("manager hire should pass the gate: %v", err)
		}
	})

	t.Run("update_employee scoped to self or managed", func(t *testing.T) {
		err := authorizeToolCall(ctx, s, "mgr-b", "update_employee",
			map[string]any{"employee_id": "worker-a1"}, "")
		if err == nil {
			t.Error("editing an employee outside your subtree must be refused")
		}
		err = authorizeToolCall(ctx, s, "mgr-a", "update_employee",
			map[string]any{"employee_id": "worker-a1"}, "")
		if err != nil {
			t.Errorf("editing your own report should be allowed: %v", err)
		}
	})

	t.Run("update_task_status on foreign task refused", func(t *testing.T) {
		err := authorizeToolCall(ctx, s, "worker-b1", "update_task_status",
			map[string]any{"task_id": "t1", "status": "blocked"}, "")
		if err == nil {
			t.Error("stranger flipping another team's task status must be refused")
		}
	})

	t.Run("add_task_comment scoped to task participants", func(t *testing.T) {
		err := authorizeToolCall(ctx, s, "worker-b1", "add_task_comment",
			map[string]any{"task_id": "t1", "content": "hi"}, "")
		if err == nil {
			t.Error("stranger commenting on another team's task must be refused")
		}
		err = authorizeToolCall(ctx, s, "worker-a1", "add_task_comment",
			map[string]any{"task_id": "t1", "content": "hi"}, "")
		if err != nil {
			t.Errorf("assignee commenting on own task should be allowed: %v", err)
		}
	})

	t.Run("assign_skill scoped to self or managed", func(t *testing.T) {
		err := authorizeToolCall(ctx, s, "worker-b1", "assign_skill",
			map[string]any{"employee_id": "worker-a1", "skill_id": "s1"}, "")
		if err == nil {
			t.Error("assigning a skill to an employee outside your subtree must be refused")
		}
		err = authorizeToolCall(ctx, s, "worker-b1", "assign_skill",
			map[string]any{"employee_id": "worker-b1", "skill_id": "s1"}, "")
		if err != nil {
			t.Errorf("self skill assignment should be allowed: %v", err)
		}
	})

	t.Run("reassignment requires delegation authority", func(t *testing.T) {
		// worker-a1 owns t1 but is not worker-b1's manager: handing the task to
		// worker-b1 would be delegation without canDelegate.
		err := authorizeToolCall(ctx, s, "worker-a1", "update_task",
			map[string]any{"task_id": "t1", "assignee_id": "worker-b1"}, "")
		if err == nil {
			t.Error("reassigning outside your subtree must be refused")
		}
		// Taking over your own task is not an escalation.
		err = authorizeToolCall(ctx, s, "mgr-a", "update_task",
			map[string]any{"task_id": "t1", "assignee_id": "mgr-a"}, "")
		if err != nil {
			t.Errorf("creator taking over own task should be allowed: %v", err)
		}
		// Manager reassigning within their subtree is fine.
		err = authorizeToolCall(ctx, s, "mgr-a", "update_task",
			map[string]any{"task_id": "t1", "assignee_id": "worker-a1x"}, "")
		if err != nil {
			t.Errorf("manager reassigning within subtree should be allowed: %v", err)
		}
	})
}

// A manager-toolset agent must not be able to self-grant privileged tags
// (plan 2.2): tags gate tool access (buildAgentTools) and delegation authority
// (canDelegate), so granting them is privilege escalation.
func TestValidateTagChange(t *testing.T) {
	mgr := makeEmployee("PM", []string{"manager"}, nil, nil)
	ceo := makeEmployee("CEO", []string{"executive"}, nil, nil)

	t.Run("non-CEO cannot grant manager", func(t *testing.T) {
		if err := validateTagChange(mgr, []string{}, []string{"manager"}); err == nil {
			t.Error("granting manager tag must be refused for non-CEO")
		}
	})
	t.Run("non-CEO cannot grant media_tagger", func(t *testing.T) {
		if err := validateTagChange(mgr, []string{}, []string{"media_tagger"}); err == nil {
			t.Error("granting media_tagger tag must be refused for non-CEO")
		}
	})
	t.Run("non-CEO cannot revoke manager", func(t *testing.T) {
		if err := validateTagChange(mgr, []string{"manager"}, []string{}); err == nil {
			t.Error("revoking manager tag must be refused for non-CEO")
		}
	})
	t.Run("non-privileged tags are free", func(t *testing.T) {
		if err := validateTagChange(mgr, []string{"golang"}, []string{"golang", "react"}); err != nil {
			t.Errorf("plain tags should be editable: %v", err)
		}
	})
	t.Run("keeping an existing privileged tag is fine", func(t *testing.T) {
		if err := validateTagChange(mgr, []string{"manager"}, []string{"manager", "golang"}); err != nil {
			t.Errorf("unchanged privileged tag should pass: %v", err)
		}
	})
	t.Run("CEO can grant anything", func(t *testing.T) {
		if err := validateTagChange(ceo, []string{}, []string{"manager", "media_tagger"}); err != nil {
			t.Errorf("CEO grant should be allowed: %v", err)
		}
	})
}
