package postgres_test

// Repository round-trip tests (plan 6.2): pin that each entity's SQL reads
// back what it writes, against the real schema. The deeper state-machine and
// dispatcher behavior is pinned by the DB-backed tests in the main package.

import (
	"context"
	"os"
	"testing"

	"mobius/internal/domain"
	"mobius/internal/storage/postgres"
	"mobius/internal/storage/postgres/postgrestest"
)

func TestMain(m *testing.M) {
	code := m.Run()
	postgrestest.Cleanup()
	os.Exit(code)
}

func insertEmployee(t *testing.T, pg *postgres.Client, name, role string) string {
	t.Helper()
	var id string
	err := pg.Pool().QueryRow(context.Background(),
		"INSERT INTO employees (name, title, role) VALUES ($1, $2, $3) RETURNING id",
		name, name+" ("+role+")", role).Scan(&id)
	if err != nil {
		t.Fatalf("insert employee %s: %v", name, err)
	}
	return id
}

func TestEmployeeRoundTrip(t *testing.T) {
	pg := postgrestest.Client(t)
	ctx := context.Background()

	emp := &domain.Employee{
		Name:      "Repo Tester",
		Title:     "QA",
		Role:      "QA",
		Backstory: "round-trip fixture",
		Skills:    []domain.EmployeeSkill{{Skill: "testing", Description: "pin SQL"}},
		Tags:      []string{"qa"},
	}
	if err := pg.CreateEmployee(ctx, emp); err != nil {
		t.Fatalf("CreateEmployee: %v", err)
	}
	if emp.ID == "" {
		t.Fatal("CreateEmployee did not backfill ID")
	}

	got, err := pg.GetEmployee(ctx, emp.ID)
	if err != nil {
		t.Fatalf("GetEmployee: %v", err)
	}
	if got.Name != emp.Name || got.Role != emp.Role || got.Backstory != emp.Backstory {
		t.Errorf("GetEmployee = %+v, want name/role/backstory of %+v", got, emp)
	}
	if len(got.Skills) != 1 || got.Skills[0].Skill != "testing" {
		t.Errorf("skills not round-tripped: %+v", got.Skills)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "qa" {
		t.Errorf("tags not round-tripped: %+v", got.Tags)
	}
}

func TestTaskAndCommentRoundTrip(t *testing.T) {
	pg := postgrestest.Client(t)
	ctx := context.Background()

	creator := insertEmployee(t, pg, "Creator", "CEO")
	assignee := insertEmployee(t, pg, "Assignee", "Engineer")

	task := &domain.Task{
		Title:    "repo round-trip",
		Body:     "body",
		Priority: "high",
		Creator:  &domain.EmployeeBrief{ID: creator},
		Assignee: &domain.EmployeeBrief{ID: assignee},
	}
	if err := pg.CreateTask(ctx, task, nil); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	got, err := pg.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Title != task.Title || got.Assignee == nil || got.Assignee.ID != assignee {
		t.Errorf("GetTask = %+v, want title %q assignee %s", got, task.Title, assignee)
	}

	if _, err := pg.AddTaskComment(ctx, task.ID, creator, "first!"); err != nil {
		t.Fatalf("AddTaskComment: %v", err)
	}
	comments, err := pg.ListTaskComments(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListTaskComments: %v", err)
	}
	if len(comments) != 1 || comments[0].Content != "first!" || comments[0].Author == nil || comments[0].Author.ID != creator {
		t.Errorf("comments = %+v, want one comment by creator", comments)
	}
}

func TestProjectRoundTrip(t *testing.T) {
	pg := postgrestest.Client(t)
	ctx := context.Background()

	baseDir := t.TempDir()
	p, err := pg.CreateProject(ctx, domain.CreateProjectInput{
		Name:        "repo-project",
		Description: "round-trip",
	}, baseDir, nil)
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	got, err := pg.GetProjectByName(ctx, "repo-project")
	if err != nil {
		t.Fatalf("GetProjectByName: %v", err)
	}
	if got.ID != p.ID || got.Description != "round-trip" {
		t.Errorf("GetProjectByName = %+v, want id %s", got, p.ID)
	}
}

func TestConversationMetaRoundTrip(t *testing.T) {
	pg := postgrestest.Client(t)
	ctx := context.Background()

	conv := &domain.Conversation{ID: "conv-1", Title: "hello", UpdatedAt: 42}
	if err := pg.UpsertConversationMeta(ctx, conv); err != nil {
		t.Fatalf("UpsertConversationMeta: %v", err)
	}
	list, err := pg.ListConversationsMeta(ctx, "")
	if err != nil {
		t.Fatalf("ListConversationsMeta: %v", err)
	}
	if len(list) != 1 || list[0].ID != "conv-1" || list[0].Title != "hello" {
		t.Errorf("ListConversationsMeta = %+v, want the upserted conversation", list)
	}
}
