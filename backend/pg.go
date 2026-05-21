package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PGClient struct {
	pool *pgxpool.Pool
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
	return &PGClient{pool: pool}, nil
}

func (pg *PGClient) Ping(ctx context.Context) error {
	return pg.pool.Ping(ctx)
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
