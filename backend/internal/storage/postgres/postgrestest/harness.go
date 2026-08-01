// Package postgrestest is the shared throwaway-database harness for DB-backed
// tests (plan 5.1/6.2). Each test binary that uses it gets its own
// mobius_test_<nano> database with the real schemas/postgres migrations
// applied; call Cleanup from TestMain to drop it after the run.
//
//   - connects to a local Postgres server (the mobius-postgres dev container by
//     default; override with MOBIUS_TEST_PG_DSN, URL form),
//   - creates a unique mobius_test_<nano> database once per test binary,
//   - truncates all tables on every Client call,
//   - never writes to the dev `mobius` database (admin connection target only).
//
// When the server is unreachable the calling test skips (visible in -v
// output); any other setup failure fails loudly.
package postgrestest

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mobius/internal/storage/postgres"
)

const adminDSNDefault = "postgres://mobius:mobius@localhost:5432/mobius?sslmode=disable"

var (
	once       sync.Once
	client     *postgres.Client
	dbName     string
	skipReason string // server unreachable → skip DB tests
	setupErr   error  // server reachable but setup failed → fail loudly
)

func adminDSN() string {
	if dsn := os.Getenv("MOBIUS_TEST_PG_DSN"); dsn != "" {
		return dsn
	}
	return adminDSNDefault
}

// Client returns the shared throwaway-database client with all tables emptied.
func Client(t *testing.T) *postgres.Client {
	t.Helper()
	once.Do(setup)
	if skipReason != "" {
		t.Skip(skipReason)
	}
	if setupErr != nil {
		t.Fatalf("postgres test harness setup failed: %v", setupErr)
	}
	reset(t)
	return client
}

func setup() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dsn := adminDSN()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		setupErr = fmt.Errorf("parse admin DSN %q: %w", dsn, err)
		return
	}
	defer admin.Close()

	if err := admin.Ping(ctx); err != nil {
		skipReason = fmt.Sprintf(
			"Postgres unreachable at %s — DB-backed tests skipped (run `make docker-up-postgres` or set MOBIUS_TEST_PG_DSN): %v",
			dsn, err)
		return
	}

	name := fmt.Sprintf("mobius_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		setupErr = fmt.Errorf("create database %s: %w", name, err)
		return
	}
	dbName = name

	testDSN, err := replaceDSNDatabase(dsn, name)
	if err != nil {
		setupErr = err
		return
	}

	c, err := postgres.New(ctx, testDSN)
	if err != nil {
		setupErr = fmt.Errorf("connect to test database: %w", err)
		return
	}

	migrations, err := findMigrationsDir()
	if err != nil {
		c.Close()
		setupErr = err
		return
	}
	if err := c.RunMigrations(ctx, migrations); err != nil {
		c.Close()
		setupErr = fmt.Errorf("run migrations: %w", err)
		return
	}
	client = c
}

// findMigrationsDir walks up from the test working directory to the repo root
// (tests run with wd = their package dir, which varies by package depth).
func findMigrationsDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, "schemas", "postgres")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("schemas/postgres not found above test working directory")
		}
		dir = parent
	}
}

// replaceDSNDatabase swaps the database name in a postgres:// URL DSN.
func replaceDSNDatabase(dsn, db string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("MOBIUS_TEST_PG_DSN must be a postgres:// URL: %w", err)
	}
	u.Path = "/" + db
	return u.String(), nil
}

// reset empties every table so each test starts from a clean slate.
// Keep the list in sync with schemas/postgres/ when tables are added.
func reset(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := client.Pool().Exec(ctx, `
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

// Cleanup drops the throwaway database. Call it from TestMain after m.Run().
func Cleanup() {
	if client != nil {
		client.Close()
	}
	if dbName == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, adminDSN())
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: could not connect to drop test database %s: %v\n", dbName, err)
		return
	}
	defer admin.Close()
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+dbName+" WITH (FORCE)"); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: failed to drop test database %s: %v\n", dbName, err)
	}
}
