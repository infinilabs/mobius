package main

// Transitional shims for the Phase-6 decomposition (plan 6.2): the Postgres
// client lives in internal/storage/postgres; main-package code keeps its old
// names via these aliases until the remaining callers (dispatcher, handlers)
// move into their own packages (6.3/6.4).

import (
	"context"
	"fmt"

	"mobius/internal/storage/postgres"
)

type PGClient = postgres.Client

// maxAutoRejections / validTransitions are owned by the storage layer now;
// the dispatcher's review-claim SQL and the state-machine tests still read
// them from main.
const maxAutoRejections = postgres.MaxAutoRejections

var validTransitions = postgres.ValidTransitions

func NewPGClient(ctx context.Context, cfg PostgresConfig) (*PGClient, error) {
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.DBName)
	return postgres.New(ctx, dsn)
}
