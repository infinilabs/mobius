package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"cloud.google.com/go/bigquery"
)

type BQClient struct {
	client  *bigquery.Client
	dataset string
	table   string
}

func NewBQClient(ctx context.Context, cfg *Config) (*BQClient, error) {
	gc := cfg.GoogleCloud
	if gc.ProjectID == "" || gc.BigQuery.Dataset == "" {
		return nil, fmt.Errorf("BigQuery not configured: project_id and dataset required")
	}

	client, err := bigquery.NewClient(ctx, gc.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("failed to create BigQuery client: %w", err)
	}

	table := gc.BigQuery.EventTable
	if table == "" {
		table = "events"
	}

	bq := &BQClient{client: client, dataset: gc.BigQuery.Dataset, table: table}

	if err := bq.EnsureTable(ctx); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to ensure BQ table: %w", err)
	}

	slog.Info("BigQuery client connected", "dataset", gc.BigQuery.Dataset, "table", table)
	return bq, nil
}

func (bq *BQClient) EnsureTable(ctx context.Context) error {
	tableRef := bq.client.Dataset(bq.dataset).Table(bq.table)
	_, err := tableRef.Metadata(ctx)
	if err == nil {
		return nil
	}

	schema := bigquery.Schema{
		{Name: "id", Type: bigquery.StringFieldType, Required: true},
		{Name: "timestamp", Type: bigquery.TimestampFieldType, Required: true},
		{Name: "event_type", Type: bigquery.StringFieldType, Required: true},
		{Name: "actor_id", Type: bigquery.StringFieldType},
		{Name: "project_id", Type: bigquery.StringFieldType},
		{Name: "task_id", Type: bigquery.StringFieldType},
		{Name: "conversation_id", Type: bigquery.StringFieldType},
		{Name: "payload", Type: bigquery.JSONFieldType, Required: true},
	}
	meta := &bigquery.TableMetadata{
		Schema: schema,
		TimePartitioning: &bigquery.TimePartitioning{
			Type:  bigquery.DayPartitioningType,
			Field: "timestamp",
		},
		Clustering: &bigquery.Clustering{
			Fields: []string{"project_id", "actor_id"},
		},
	}
	if err := tableRef.Create(ctx, meta); err != nil {
		return fmt.Errorf("failed to create BQ table %s.%s: %w", bq.dataset, bq.table, err)
	}
	slog.Info("BigQuery table created", "dataset", bq.dataset, "table", bq.table)
	return nil
}

type bqEventRow struct {
	ID             string `bigquery:"id"`
	Timestamp      string `bigquery:"timestamp"`
	EventType      string `bigquery:"event_type"`
	ActorID        string `bigquery:"actor_id"`
	ProjectID      string `bigquery:"project_id"`
	TaskID         string `bigquery:"task_id"`
	ConversationID string `bigquery:"conversation_id"`
	Payload        string `bigquery:"payload"`
}

func (r *bqEventRow) Save() (map[string]bigquery.Value, string, error) {
	return map[string]bigquery.Value{
		"id":              r.ID,
		"timestamp":       r.Timestamp,
		"event_type":      r.EventType,
		"actor_id":        r.ActorID,
		"project_id":      r.ProjectID,
		"task_id":         r.TaskID,
		"conversation_id": r.ConversationID,
		"payload":         r.Payload,
	}, r.ID, nil
}

func (bq *BQClient) StreamInsertEvents(ctx context.Context, events []*Event) error {
	inserter := bq.client.Dataset(bq.dataset).Table(bq.table).Inserter()
	var rows []*bqEventRow
	for _, evt := range events {
		payloadJSON, _ := json.Marshal(evt.Payload)
		rows = append(rows, &bqEventRow{
			ID:             evt.ID,
			Timestamp:      evt.Timestamp.Format("2006-01-02 15:04:05.999999 UTC"),
			EventType:      evt.EventType,
			ActorID:        ptrToStr(evt.ActorID),
			ProjectID:      ptrToStr(evt.ProjectID),
			TaskID:         ptrToStr(evt.TaskID),
			ConversationID: ptrToStr(evt.ConversationID),
			Payload:        string(payloadJSON),
		})
	}
	if err := inserter.Put(ctx, rows); err != nil {
		return fmt.Errorf("BQ stream insert failed: %w", err)
	}
	return nil
}

func (bq *BQClient) Ping(ctx context.Context) error {
	_, err := bq.client.Dataset(bq.dataset).Metadata(ctx)
	return err
}

func (bq *BQClient) Close() error {
	return bq.client.Close()
}

func ptrToStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
