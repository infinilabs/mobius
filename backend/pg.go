package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PGClient struct {
	pool     *pgxpool.Pool
	dsn      string
	esClient *ESClient // optional ES mirror; kept in sync by mutating PG methods
}

// SetESClient wires the Elasticsearch mirror so PG mutations that are reached
// from many call sites (e.g. UpdateTaskStatus) keep ES in sync in one place,
// instead of relying on every caller to remember to reindex.
func (pg *PGClient) SetESClient(es *ESClient) {
	pg.esClient = es
}

// reindexTask re-mirrors a single task from PG into ES. No-op when ES is
// unavailable. Failures are logged, not fatal — PG remains the source of truth.
func (pg *PGClient) reindexTask(ctx context.Context, id string) {
	if pg.esClient == nil {
		return
	}
	t, err := pg.GetTask(ctx, id)
	if err != nil {
		slog.Warn("reindexTask: load from PG failed", "id", id, "error", err)
		return
	}
	if err := pg.esClient.IndexTask(ctx, t); err != nil {
		slog.Warn("reindexTask: ES index failed", "id", id, "error", err)
	}
}

func NewPGClient(ctx context.Context, cfg PostgresConfig) (*PGClient, error) {
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.DBName)

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to create PG pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("PG ping failed: %w", err)
	}

	slog.Info("PostgreSQL connected", "host", cfg.Host, "port", cfg.Port, "db", cfg.DBName)
	return &PGClient{pool: pool, dsn: dsn}, nil
}

func (pg *PGClient) DSN() string {
	return pg.dsn
}

func (pg *PGClient) Ping(ctx context.Context) error {
	return pg.pool.Ping(ctx)
}

// tokenUsageJSON serializes token usage for the heartbeat_runs.token_usage
// column. Returns nil (SQL NULL) when usage is nil so non-LLM runs (bash/CLI)
// store no token figures rather than a zeroed object.
func tokenUsageJSON(usage *TokenUsage) []byte {
	if usage == nil {
		return nil
	}
	b, _ := json.Marshal(map[string]int32{
		"prompt_tokens":     usage.PromptTokens,
		"completion_tokens": usage.CompletionTokens,
		"total_tokens":      usage.TotalTokens,
		"cached_tokens":     usage.CachedTokens,
		"thoughts_tokens":   usage.ThoughtsTokens,
		"tool_use_tokens":   usage.ToolUseTokens,
	})
	return b
}

// StartHeartbeatRun inserts an 'active' run row and returns its id. The
// dispatcher owns the row for the run's whole lifecycle: it flushes live
// output/token usage while the run executes (so the monthly-budget gate sees
// in-flight spend, including across concurrent runs of the same agent) and
// finalizes the row when the run reaches a terminal state. This is the durable,
// BigQuery-independent ledger the budget gate reads (see budgetExceeded).
func (pg *PGClient) StartHeartbeatRun(ctx context.Context, taskID, agentID, adapterType string, startedAt time.Time) (string, error) {
	var id string
	err := pg.pool.QueryRow(ctx, `
		INSERT INTO heartbeat_runs (task_id, agent_id, adapter_type, status, started_at)
		VALUES ($1, $2, $3, 'active', $4)
		RETURNING id
	`, taskID, agentID, adapterType, startedAt).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("start heartbeat run: %w", err)
	}
	return id, nil
}

// UpdateHeartbeatRun flushes a run's current output/usage/status. A non-nil
// completedAt marks the row terminal.
func (pg *PGClient) UpdateHeartbeatRun(ctx context.Context, rowID, status, output, errMsg string, usage *TokenUsage, completedAt *time.Time) error {
	var errArg any
	if errMsg != "" {
		errArg = errMsg
	}
	_, err := pg.pool.Exec(ctx, `
		UPDATE heartbeat_runs
		SET status = $2, output_text = $3, error_message = $4, token_usage = $5, completed_at = $6
		WHERE id = $1
	`, rowID, status, output, errArg, tokenUsageJSON(usage), completedAt)
	if err != nil {
		return fmt.Errorf("update heartbeat run: %w", err)
	}
	return nil
}

// ReconcileOrphanedRuns marks runs left 'active' by a previous process (a hard
// crash) as failed. Graceful shutdown finalizes its own runs as cancelled, so
// this only ever touches crash survivors. Called once at dispatcher startup.
func (pg *PGClient) ReconcileOrphanedRuns(ctx context.Context) (int64, error) {
	tag, err := pg.pool.Exec(ctx, `
		UPDATE heartbeat_runs
		SET status = 'failed', error_message = 'orphaned by restart', completed_at = NOW()
		WHERE status = 'active'
	`)
	if err != nil {
		return 0, fmt.Errorf("reconcile orphaned runs: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (pg *PGClient) Close() {
	pg.pool.Close()
}

func (pg *PGClient) RunMigrations(ctx context.Context, dir string) error {
	_, err := pg.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	applied := 0
	for _, name := range files {
		var exists bool
		err := pg.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)", name).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if exists {
			continue
		}

		sql, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		tx, err := pg.pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", name, err)
		}

		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("exec migration %s: %w", name, err)
		}

		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", name); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", name, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}

		applied++
		slog.Info("migration applied", "version", name)
	}

	if applied > 0 {
		slog.Info("migrations complete", "applied", applied)
	}
	return nil
}
