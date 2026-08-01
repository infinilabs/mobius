package main

import (
	"mobius/internal/domain"
	"mobius/internal/seed"
	"testing"
)

func TestStrPtrOrNil(t *testing.T) {
	if strPtrOrNil("") != nil {
		t.Error("empty string should map to nil pointer")
	}
	p := strPtrOrNil("x")
	if p == nil || *p != "x" {
		t.Errorf("non-empty string should map to pointer, got %v", p)
	}
}

func TestNewEvent(t *testing.T) {
	actor := strPtrOrNil("agent-1")
	ev := newEvent("task.created", actor, nil, strPtrOrNil("task-9"), map[string]any{"k": "v"})
	if ev.ID == "" {
		t.Error("event must have a generated id")
	}
	if ev.EventType != "task.created" {
		t.Errorf("event type = %q", ev.EventType)
	}
	if ev.ActorID == nil || *ev.ActorID != "agent-1" {
		t.Error("actor id not wired")
	}
	if ev.ProjectID != nil {
		t.Error("nil project id should stay nil")
	}
	if ev.Timestamp.IsZero() {
		t.Error("timestamp not set")
	}
}

// skillIDFromName must be deterministic (the same skill name always maps to the
// same id) and collision-distinct for different names — ids key skill storage.
func TestSkillIDFromName(t *testing.T) {
	a1 := domain.SkillIDFromName("frontend-design")
	a2 := domain.SkillIDFromName("frontend-design")
	b := domain.SkillIDFromName("backend-design")
	if a1 != a2 {
		t.Errorf("non-deterministic: %q != %q", a1, a2)
	}
	if a1 == b {
		t.Error("distinct names produced the same id")
	}
	if len(a1) != 16 { // hex of first 8 bytes
		t.Errorf("id length = %d, want 16", len(a1))
	}
}

// The seed roster must include a CEO — bootstrap assigns the CEO as the root of
// the management chain; an empty/CEO-less roster would break first-run setup.
func TestDefaultEmployees_HasCEO(t *testing.T) {
	emps, err := seed.DefaultEmployees()
	if err != nil {
		t.Fatal(err)
	}
	if len(emps) == 0 {
		t.Fatal("default employee roster is empty")
	}
	hasCEO := false
	for _, e := range emps {
		if e.Role == "CEO" {
			hasCEO = true
		}
	}
	if !hasCEO {
		t.Error("seed roster has no CEO")
	}
}

// Every founder named in founderSkillDefaults must exist in the seed roster —
// otherwise a skill default references an employee that is never created.
func TestFounderSkillDefaults_ReferToSeededEmployees(t *testing.T) {
	emps, err := seed.DefaultEmployees()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, e := range emps {
		names[e.Name] = true
	}
	for founder := range seed.FounderSkillDefaults() {
		if !names[founder] {
			t.Errorf("founderSkillDefaults references %q which is not a seeded employee", founder)
		}
	}
}
