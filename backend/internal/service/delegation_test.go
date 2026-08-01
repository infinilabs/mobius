package service

import (
	"context"
	"mobius/internal/domain"
	"testing"
)

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

// delegationStore returns the shared hierarchy fixture used by the CanDelegate
// tests (defined in authz_test.go): ceo → mgr-a → worker-a1 → worker-a1x,
// ceo → mgr-b → worker-b1.
func delegationStore() *fakeAuthzStore { return newHierarchyStore() }

func TestCanDelegate_CEOToAnyone(t *testing.T) {
	s := delegationStore()
	ceo := makeEmployee("CEO", []string{"executive"}, nil, nil)
	worker := makeEmployee("Custom", []string{}, strPtr("other-mgr"), nil)
	worker.ID = "emp-002"
	if !CanDelegate(context.Background(), s, ceo, worker) {
		t.Error("CEO should be able to delegate to anyone")
	}
}

func TestCanDelegate_RefusesSelfDelegation(t *testing.T) {
	s := delegationStore()
	ceo := makeEmployee("CEO", []string{"executive"}, nil, nil)
	if CanDelegate(context.Background(), s, ceo, ceo) {
		t.Error("CEO should NOT delegate to themselves")
	}
	mgr := makeEmployee("PM", []string{"manager"}, nil, nil)
	if CanDelegate(context.Background(), s, mgr, mgr) {
		t.Error("manager should NOT delegate to themselves")
	}
}

func TestExceedsDelegationDepth(t *testing.T) {
	if ExceedsDelegationDepth(0) {
		t.Error("root task (depth 0) should be allowed to delegate")
	}
	if ExceedsDelegationDepth(MaxDelegationDepth - 1) {
		t.Error("delegating up to the max depth should be allowed")
	}
	if !ExceedsDelegationDepth(MaxDelegationDepth) {
		t.Errorf("delegating past depth %d should be refused", MaxDelegationDepth)
	}
}

func TestCanDelegate_ManagerToDirectReport(t *testing.T) {
	s := delegationStore()
	if !CanDelegate(context.Background(), s, s.employees["mgr-a"], s.employees["worker-a1"]) {
		t.Error("manager should delegate to direct report")
	}
}

func TestCanDelegate_ManagerToDescendant(t *testing.T) {
	s := delegationStore()
	if !CanDelegate(context.Background(), s, s.employees["mgr-a"], s.employees["worker-a1x"]) {
		t.Error("manager should delegate to a transitive descendant")
	}
}

func TestCanDelegate_ManagerToPeerManagerRefused(t *testing.T) {
	s := delegationStore()
	if CanDelegate(context.Background(), s, s.employees["mgr-a"], s.employees["mgr-b"]) {
		t.Error("manager should NOT delegate to a peer manager outside their subtree")
	}
}

func TestCanDelegate_ManagerToCEORefused(t *testing.T) {
	s := delegationStore()
	if CanDelegate(context.Background(), s, s.employees["mgr-a"], s.employees["ceo"]) {
		t.Error("manager should NOT delegate to the CEO")
	}
}

func TestCanDelegate_ManagerToOtherTeam(t *testing.T) {
	s := delegationStore()
	if CanDelegate(context.Background(), s, s.employees["mgr-a"], s.employees["worker-b1"]) {
		t.Error("manager should NOT delegate to another team's worker")
	}
}

func TestCanDelegate_WorkerToAnyone(t *testing.T) {
	s := delegationStore()
	if CanDelegate(context.Background(), s, s.employees["worker-a1x"], s.employees["worker-b1"]) {
		t.Error("non-manager should NOT delegate to anyone")
	}
}

func strPtr(s string) *string { return &s }
