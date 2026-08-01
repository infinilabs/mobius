package main

// DB-backed tests in this package get their throwaway database from the
// shared harness in internal/storage/postgres/postgrestest (plan 5.1/6.2);
// this file keeps the package-local entry point and row-level fixtures.

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"mobius/internal/storage/postgres/postgrestest"
)

func TestMain(m *testing.M) {
	code := m.Run()
	postgrestest.Cleanup()
	os.Exit(code)
}

// testPG returns the shared throwaway-database client with all tables emptied.
func testPG(t *testing.T) *PGClient {
	t.Helper()
	return postgrestest.Client(t)
}

// --- fixtures ---

// newTestEmployee inserts a minimal employee row and returns its id.
// role must satisfy the employees.role CHECK ('CEO','PM','Engineer','QA','Designer','Custom').
func newTestEmployee(t *testing.T, pg *PGClient, name, role string) string {
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

// newTestTask inserts a task row directly (bypassing CreateTask so tests can
// pin arbitrary statuses without walking the state machine) and returns its id.
// assigneeID/creatorID may be empty for NULL.
func newTestTask(t *testing.T, pg *PGClient, title, status, assigneeID, creatorID string) string {
	t.Helper()
	var aID, cID *string
	if assigneeID != "" {
		aID = &assigneeID
	}
	if creatorID != "" {
		cID = &creatorID
	}
	var id string
	err := pg.Pool().QueryRow(context.Background(),
		"INSERT INTO tasks (title, status, assignee_id, creator_id) VALUES ($1, $2, $3, $4) RETURNING id",
		title, status, aID, cID).Scan(&id)
	if err != nil {
		t.Fatalf("insert task %q: %v", title, err)
	}
	return id
}

// setTaskColumns applies raw column updates to a task (retry_after, counters,
// timestamps…) without touching the state machine.
func setTaskColumns(t *testing.T, pg *PGClient, id, setClause string, args ...any) {
	t.Helper()
	query := fmt.Sprintf("UPDATE tasks SET %s WHERE id = $%d", setClause, len(args)+1)
	args = append(args, id)
	if _, err := pg.Pool().Exec(context.Background(), query, args...); err != nil {
		t.Fatalf("set task columns (%s): %v", setClause, err)
	}
}

// taskRow reads the dispatch-relevant columns of a task for assertions.
type taskRow struct {
	Status       string
	FailureCount int
	Result       string
	RetryAfter   *time.Time
	IsScheduled  bool
	NextRunAt    *time.Time
	Rejections   int
}

func getTaskRow(t *testing.T, pg *PGClient, id string) taskRow {
	t.Helper()
	var r taskRow
	err := pg.Pool().QueryRow(context.Background(), `
		SELECT status, failure_count, result, retry_after, is_scheduled, next_run_at, rejection_count
		FROM tasks WHERE id = $1
	`, id).Scan(&r.Status, &r.FailureCount, &r.Result, &r.RetryAfter, &r.IsScheduled, &r.NextRunAt, &r.Rejections)
	if err != nil {
		t.Fatalf("read task %s: %v", id, err)
	}
	return r
}

// taskComments returns the task's comment texts in creation order.
func taskComments(t *testing.T, pg *PGClient, id string) []string {
	t.Helper()
	rows, err := pg.Pool().Query(context.Background(),
		"SELECT content FROM task_comments WHERE task_id = $1 ORDER BY created_at", id)
	if err != nil {
		t.Fatalf("read comments: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatalf("scan comment: %v", err)
		}
		out = append(out, c)
	}
	return out
}

// newTestDispatcher builds a dispatcher wired to the test DB only: no ES, no
// token pipeline, an empty adapter registry (an executed task fails fast with
// "unknown adapter type", which is itself useful to observe failTask), nil
// config (RunTimeout is nil-safe), no event pipeline.
func newTestDispatcher(pg *PGClient) *TaskDispatcher {
	return NewTaskDispatcher(pg, nil, nil, NewAdapterRegistry(), 5, nil, nil)
}
