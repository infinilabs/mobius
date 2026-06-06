package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"cloud.google.com/go/bigquery"
)

type BQClient struct {
	client     *bigquery.Client
	projectID  string
	dataset    string
	table      string
	tokenTable string

	// Media tagging (video_tagging.md §4.1). Defaulted in NewBQClient.
	creativesDataset string
	connection       string
	taggingEndpoint  string
	taggingModel     string
}

func NewBQClient(ctx context.Context, cfg *Config) (*BQClient, error) {
	gc := cfg.GoogleCloud
	if gc.ProjectID == "" || gc.BigQuery.Dataset == "" {
		return nil, fmt.Errorf("BigQuery not configured: project_id and dataset required")
	}

	projectID := gc.BigQuery.ProjectID
	if projectID == "" {
		projectID = gc.ProjectID
	}
	client, err := bigquery.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to create BigQuery client: %w", err)
	}

	table := gc.BigQuery.EventTable
	if table == "" {
		table = "events"
	}
	tokenTable := gc.BigQuery.TokenTable
	if tokenTable == "" {
		tokenTable = "token_usage"
	}

	creativesDataset := gc.BigQuery.CreativesDataset
	if creativesDataset == "" {
		creativesDataset = "mobius_creatives"
	}
	connection := gc.BigQuery.Connection
	if connection == "" {
		connection = "us.mobius_conn"
	}
	taggingEndpoint := gc.BigQuery.TaggingModelEndpoint
	if taggingEndpoint == "" {
		taggingEndpoint = "gemini-2.5-flash"
	}
	taggingModel := gc.BigQuery.TaggingModelName
	if taggingModel == "" {
		taggingModel = "tagging_gemini"
	}

	bq := &BQClient{
		client: client, projectID: projectID,
		dataset: gc.BigQuery.Dataset, table: table, tokenTable: tokenTable,
		creativesDataset: creativesDataset, connection: connection,
		taggingEndpoint: taggingEndpoint, taggingModel: taggingModel,
	}

	if err := bq.EnsureDataset(ctx); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to ensure BQ dataset: %w", err)
	}
	if err := bq.EnsureTable(ctx); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to ensure BQ table: %w", err)
	}
	if err := bq.EnsureTokenTable(ctx); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to ensure BQ token table: %w", err)
	}

	slog.Info("BigQuery client connected", "dataset", gc.BigQuery.Dataset, "table", table)
	return bq, nil
}

func (bq *BQClient) EnsureDataset(ctx context.Context) error {
	ds := bq.client.Dataset(bq.dataset)
	_, err := ds.Metadata(ctx)
	if err == nil {
		return nil
	}
	if err := ds.Create(ctx, &bigquery.DatasetMetadata{}); err != nil {
		return fmt.Errorf("failed to create BQ dataset %s: %w", bq.dataset, err)
	}
	slog.Info("BigQuery dataset created", "dataset", bq.dataset)
	return nil
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

// --- Token Usage ---

func (bq *BQClient) EnsureTokenTable(ctx context.Context) error {
	tableRef := bq.client.Dataset(bq.dataset).Table(bq.tokenTable)
	_, err := tableRef.Metadata(ctx)
	if err == nil {
		return nil
	}

	schema := bigquery.Schema{
		{Name: "id", Type: bigquery.StringFieldType, Required: true},
		{Name: "timestamp", Type: bigquery.TimestampFieldType, Required: true},
		{Name: "model_id", Type: bigquery.StringFieldType, Required: true},
		{Name: "provider", Type: bigquery.StringFieldType, Required: true},
		{Name: "employee_id", Type: bigquery.StringFieldType},
		{Name: "employee_name", Type: bigquery.StringFieldType},
		{Name: "project_id", Type: bigquery.StringFieldType},
		{Name: "task_id", Type: bigquery.StringFieldType},
		{Name: "conversation_id", Type: bigquery.StringFieldType},
		{Name: "prompt_tokens", Type: bigquery.IntegerFieldType, Required: true},
		{Name: "completion_tokens", Type: bigquery.IntegerFieldType, Required: true},
		{Name: "total_tokens", Type: bigquery.IntegerFieldType, Required: true},
		{Name: "cached_tokens", Type: bigquery.IntegerFieldType},
		{Name: "thoughts_tokens", Type: bigquery.IntegerFieldType},
		{Name: "tool_use_tokens", Type: bigquery.IntegerFieldType},
		{Name: "latency_ms", Type: bigquery.IntegerFieldType, Required: true},
		{Name: "status", Type: bigquery.StringFieldType, Required: true},
		{Name: "error_message", Type: bigquery.StringFieldType},
		{Name: "source", Type: bigquery.StringFieldType, Required: true},
	}
	meta := &bigquery.TableMetadata{
		Schema: schema,
		TimePartitioning: &bigquery.TimePartitioning{
			Type:  bigquery.DayPartitioningType,
			Field: "timestamp",
		},
		Clustering: &bigquery.Clustering{
			Fields: []string{"model_id", "employee_id", "project_id", "task_id"},
		},
	}
	if err := tableRef.Create(ctx, meta); err != nil {
		return fmt.Errorf("failed to create BQ token table %s.%s: %w", bq.dataset, bq.tokenTable, err)
	}
	slog.Info("BigQuery token table created", "dataset", bq.dataset, "table", bq.tokenTable)
	return nil
}

type bqTokenRow struct {
	ID               string `bigquery:"id"`
	Timestamp        string `bigquery:"timestamp"`
	ModelID          string `bigquery:"model_id"`
	Provider         string `bigquery:"provider"`
	EmployeeID       string `bigquery:"employee_id"`
	EmployeeName     string `bigquery:"employee_name"`
	ProjectID        string `bigquery:"project_id"`
	TaskID           string `bigquery:"task_id"`
	ConversationID   string `bigquery:"conversation_id"`
	PromptTokens     int64  `bigquery:"prompt_tokens"`
	CompletionTokens int64  `bigquery:"completion_tokens"`
	TotalTokens      int64  `bigquery:"total_tokens"`
	CachedTokens     int64  `bigquery:"cached_tokens"`
	ThoughtsTokens   int64  `bigquery:"thoughts_tokens"`
	ToolUseTokens    int64  `bigquery:"tool_use_tokens"`
	LatencyMs        int64  `bigquery:"latency_ms"`
	Status           string `bigquery:"status"`
	ErrorMessage     string `bigquery:"error_message"`
	Source           string `bigquery:"source"`
}

func (r *bqTokenRow) Save() (map[string]bigquery.Value, string, error) {
	return map[string]bigquery.Value{
		"id":               r.ID,
		"timestamp":        r.Timestamp,
		"model_id":         r.ModelID,
		"provider":         r.Provider,
		"employee_id":      r.EmployeeID,
		"employee_name":    r.EmployeeName,
		"project_id":       r.ProjectID,
		"task_id":          r.TaskID,
		"conversation_id":  r.ConversationID,
		"prompt_tokens":    r.PromptTokens,
		"completion_tokens": r.CompletionTokens,
		"total_tokens":     r.TotalTokens,
		"cached_tokens":    r.CachedTokens,
		"thoughts_tokens":  r.ThoughtsTokens,
		"tool_use_tokens":  r.ToolUseTokens,
		"latency_ms":       r.LatencyMs,
		"status":           r.Status,
		"error_message":    r.ErrorMessage,
		"source":           r.Source,
	}, r.ID, nil
}

func (bq *BQClient) StreamInsertTokenUsage(ctx context.Context, rows []*bqTokenRow) error {
	inserter := bq.client.Dataset(bq.dataset).Table(bq.tokenTable).Inserter()
	if err := inserter.Put(ctx, rows); err != nil {
		return fmt.Errorf("BQ token batch insert failed: %w", err)
	}
	return nil
}

type TokenPipeline struct {
	queue    chan *bqTokenRow
	bqClient *BQClient
	done     chan struct{}
}

func NewTokenPipeline(bq *BQClient) *TokenPipeline {
	return &TokenPipeline{
		queue:    make(chan *bqTokenRow, 1000),
		bqClient: bq,
		done:     make(chan struct{}),
	}
}

func (tp *TokenPipeline) Record(row *bqTokenRow) {
	select {
	case tp.queue <- row:
	default:
		slog.Warn("token pipeline queue full, row dropped")
	}
}

func (tp *TokenPipeline) Start(ctx context.Context) {
	defer close(tp.done)

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	batch := make([]*bqTokenRow, 0, 50)

	flush := func(flushCtx context.Context) {
		if len(batch) == 0 {
			return
		}
		toFlush := batch
		batch = make([]*bqTokenRow, 0, 50)
		if err := tp.bqClient.StreamInsertTokenUsage(flushCtx, toFlush); err != nil {
			slog.Error("token pipeline flush failed", "count", len(toFlush), "error", err)
		} else {
			slog.Debug("token pipeline flushed", "count", len(toFlush))
		}
	}

	for {
		select {
		case row := <-tp.queue:
			batch = append(batch, row)
			if len(batch) >= 50 {
				flush(ctx)
			}
		case <-ticker.C:
			flush(ctx)
		case <-ctx.Done():
			for {
				select {
				case row := <-tp.queue:
					batch = append(batch, row)
				default:
					drainCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					flush(drainCtx)
					cancel()
					return
				}
			}
		}
	}
}

func (tp *TokenPipeline) Wait() {
	<-tp.done
}
