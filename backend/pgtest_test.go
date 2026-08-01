package main

// Postgres test harness (plan 5.1–5.3). DB-backed tests call testPG(t) to get
// a *PGClient bound to a throwaway database:
//   - connects to a local Postgres server (the mobius-postgres dev container by
//     default; override with MOBIUS_TEST_PG_DSN, URL form),
//   - creates a unique mobius_test_<nano> database once per `go test` run,
//   - applies the real migrations from schemas/postgres/,
//   - truncates all tables before each test,
//   - drops the database in TestMain after the run.
// The dev `mobius` database is only used as the admin connection target and is
// never written to. When the server is unreachable the DB-backed tests skip
// (visible in -v output); any other setup failure fails the tests loudly.

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const pgTestAdminDSNDefault = "postgres://mobius:mobius@localhost:5432/mobius?sslmode=disable"

var (
	pgTestOnce       sync.Once
	pgTestClient     *PGClient
	pgTestDBName     string
	pgTestSkipReason string // server unreachable → skip DB tests
	pgTestSetupErr   error  // server reachable but setup failed → fail loudly
)

func TestMain(m *testing.M) {
	code := m.Run()
	dropPGTestDatabase()
	os.Exit(code)
}

func pgTestAdminDSN() string {
	if dsn := os.Getenv("MOBIUS_TEST_PG_DSN"); dsn != "" {
		return dsn
	}
	return pgTestAdminDSNDefault
}

// testPG returns the shared throwaway-database client with all tables emptied.
func testPG(t *testing.T) *PGClient {
	t.Helper()
	pgTestOnce.Do(setupPGTestDatabase)
	if pgTestSkipReason != "" {
		t.Skip(pgTestSkipReason)
	}
	if pgTestSetupErr != nil {
		t.Fatalf("postgres test harness setup failed: %v", pgTestSetupErr)
	}
	resetPGTestData(t)
	return pgTestClient
}

func setupPGTestDatabase() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	adminDSN := pgTestAdminDSN()
	admin, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		pgTestSetupErr = fmt.Errorf("parse admin DSN %q: %w", adminDSN, err)
		return
	}
	defer admin.Close()

	if err := admin.Ping(ctx); err != nil {
		pgTestSkipReason = fmt.Sprintf(
			"Postgres unreachable at %s — DB-backed tests skipped (run `make docker-up-postgres` or set MOBIUS_TEST_PG_DSN): %v",
			adminDSN, err)
		return
	}

	name := fmt.Sprintf("mobius_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		pgTestSetupErr = fmt.Errorf("create database %s: %w", name, err)
		return
	}
	pgTestDBName = name

	testDSN, err := replaceDSNDatabase(adminDSN, name)
	if err != nil {
		pgTestSetupErr = err
		return
	}

	pool, err := pgxpool.New(ctx, testDSN)
	if err != nil {
		pgTestSetupErr = fmt.Errorf("connect to test database: %w", err)
		return
	}

	client := &PGClient{pool: pool, dsn: testDSN}
	if err := client.RunMigrations(ctx, "../schemas/postgres"); err != nil {
		pool.Close()
		pgTestSetupErr = fmt.Errorf("run migrations: %w", err)
		return
	}
	pgTestClient = client
}

// replaceDSNDatabase swaps the database name in a postgres:// URL DSN.
func replaceDSNDatabase(dsn, dbName string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("MOBIUS_TEST_PG_DSN must be a postgres:// URL: %w", err)
	}
	u.Path = "/" + dbName
	return u.String(), nil
}

// resetPGTestData empties every table so each test starts from a clean slate.
// Keep the list in sync with schemas/postgres/ when tables are added.
func resetPGTestData(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := pgTestClient.pool.Exec(ctx, `
		TRUNCATE TABLE
			heartbeat_runs, dispatch_events, task_interactions, task_comments,
			task_dependencies, tasks, skill_assignments, employee_reporting,
			employee_tags, employee_skills, employee_models, employees,
			projects, conversations
		CASCADE
	`)
	if err != nil {
		t.Fatalf("reset test data: %v", err)
	}
}

func dropPGTestDatabase() {
	if pgTestClient != nil {
		pgTestClient.pool.Close()
	}
	if pgTestDBName == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, pgTestAdminDSN())
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: could not connect to drop test database %s: %v\n", pgTestDBName, err)
		return
	}
	defer admin.Close()
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+pgTestDBName+" WITH (FORCE)"); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: failed to drop test database %s: %v\n", pgTestDBName, err)
	}
}

// --- fixtures ---

// newTestEmployee inserts a minimal employee row and returns its id.
// role must satisfy the employees.role CHECK ('CEO','PM','Engineer','QA','Designer','Custom').
func newTestEmployee(t *testing.T, pg *PGClient, name, role string) string {
	t.Helper()
	var id string
	err := pg.pool.QueryRow(context.Background(),
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
	err := pg.pool.QueryRow(context.Background(),
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
	if _, err := pg.pool.Exec(context.Background(), query, args...); err != nil {
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
	err := pg.pool.QueryRow(context.Background(), `
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
	rows, err := pg.pool.Query(context.Background(),
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
