package main

// Transitional aliases (plan 6.4d): BigQuery and GCS clients live in
// internal/bq and internal/gcs.

import (
	"context"

	"mobius/internal/bq"
	"mobius/internal/gcs"
)

type (
	BQClient      = bq.Client
	TokenPipeline = bq.TokenPipeline
	bqTokenRow    = bq.TokenRow

	GCSClient = gcs.Client
)

func NewBQClient(ctx context.Context, cfg *Config) (*BQClient, error) { return bq.New(ctx, cfg) }

func NewTokenPipeline(c *BQClient) *TokenPipeline { return bq.NewTokenPipeline(c) }

func NewGCSClient(ctx context.Context, cfg *Config) (*GCSClient, error) { return gcs.New(ctx, cfg) }
