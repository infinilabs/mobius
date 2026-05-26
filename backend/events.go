package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

type Event struct {
	ID             string         `json:"id"`
	Timestamp      time.Time      `json:"timestamp"`
	EventType      string         `json:"event_type"`
	ActorID        *string        `json:"actor_id,omitempty"`
	ProjectID      *string        `json:"project_id,omitempty"`
	TaskID         *string        `json:"task_id,omitempty"`
	ConversationID *string        `json:"conversation_id,omitempty"`
	Payload        map[string]any `json:"payload"`
}

type EventPipeline struct {
	queue    chan *Event
	esClient *ESClient
	bqClient *BQClient
	cfg      EventConfig
}

func NewEventPipeline(es *ESClient, bq *BQClient, cfg EventConfig) *EventPipeline {
	cfg.applyDefaults()
	return &EventPipeline{
		queue:    make(chan *Event, cfg.BufferSize),
		esClient: es,
		bqClient: bq,
		cfg:      cfg,
	}
}

func (ep *EventPipeline) Publish(evt *Event) {
	if evt.ID == "" {
		evt.ID = generateID()
	}
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now()
	}
	select {
	case ep.queue <- evt:
	default:
		slog.Error("event pipeline queue full, event dropped",
			"event_type", evt.EventType, "queue_len", len(ep.queue))
	}
}

func (ep *EventPipeline) Start(ctx context.Context) {
	flushInterval := time.Duration(ep.cfg.FlushIntervalS) * time.Second
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	batch := make([]*Event, 0, ep.cfg.BatchSize)

	flush := func() {
		if len(batch) == 0 {
			return
		}
		toFlush := batch
		batch = make([]*Event, 0, ep.cfg.BatchSize)
		go ep.flushBatch(context.Background(), toFlush)
	}

	for {
		select {
		case evt := <-ep.queue:
			batch = append(batch, evt)
			if len(batch) >= ep.cfg.BatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-ctx.Done():
		drainLoop:
			for {
				select {
				case evt := <-ep.queue:
					batch = append(batch, evt)
				default:
					break drainLoop
				}
			}
			flush()
			return
		}
	}
}

func (ep *EventPipeline) flushBatch(ctx context.Context, batch []*Event) {
	if ep.esClient != nil {
		if err := ep.esClient.BulkIndexEvents(ctx, batch); err != nil {
			slog.Error("event flush to ES failed", "count", len(batch), "error", err)
		}
	}
	if ep.bqClient != nil {
		if err := ep.bqClient.StreamInsertEvents(ctx, batch); err != nil {
			slog.Error("event flush to BQ failed", "count", len(batch), "error", err)
		}
	}
	slog.Debug("event batch flushed", "count", len(batch))
}

func newEvent(eventType string, actorID, projectID, taskID *string, payload map[string]any) *Event {
	return &Event{
		ID:        generateID(),
		Timestamp: time.Now(),
		EventType: eventType,
		ActorID:   actorID,
		ProjectID: projectID,
		TaskID:    taskID,
		Payload:   payload,
	}
}

func strPtr(s string) *string { return &s }

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// Archiver

func StartArchiver(ctx context.Context, cfg *Config, es *ESClient, gcs *GCSClient) {
	if es == nil || gcs == nil {
		slog.Info("event archiver disabled: ES or GCS not configured")
		return
	}

	retentionDays := cfg.Elasticsearch.Events.RetentionDays
	if retentionDays == 0 {
		retentionDays = 90
	}

	prefix := cfg.GoogleCloud.GCS.EventArchivePrefix
	if prefix == "" {
		prefix = "events/archived"
	}

	for {
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day()+1, 3, 0, 0, 0, now.Location())
		if now.Hour() < 3 {
			next = time.Date(now.Year(), now.Month(), now.Day(), 3, 0, 0, 0, now.Location())
		}
		timer := time.NewTimer(time.Until(next))
		select {
		case <-timer.C:
			archiveEvents(ctx, es, gcs, retentionDays, prefix)
		case <-ctx.Done():
			timer.Stop()
			return
		}
	}
}

const archiveBatchSize = 2000

func archiveEvents(ctx context.Context, es *ESClient, gcs *GCSClient, retentionDays int, prefix string) {
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	slog.Info("event archiver: starting", "cutoff", cutoff.Format(time.RFC3339))

	var totalArchived, totalDeleted int

	for {
		events, err := es.FetchEventsOlderThan(ctx, cutoff, archiveBatchSize)
		if err != nil {
			slog.Error("event archiver: fetch failed", "error", err)
			return
		}
		if len(events) == 0 {
			break
		}

		var buf bytes.Buffer
		gzWriter := gzip.NewWriter(&buf)
		encoder := json.NewEncoder(gzWriter)
		ids := make([]string, 0, len(events))
		for _, evt := range events {
			encoder.Encode(evt)
			ids = append(ids, evt.ID)
		}
		gzWriter.Close()

		datePath := cutoff.Format("2006/01/02")
		objectPrefix := fmt.Sprintf("%s/%s", prefix, datePath)
		fileID := fmt.Sprintf("events_%d", time.Now().UnixNano())

		_, err = gcs.Upload(ctx, objectPrefix, fileID, ".json.gz",
			bytes.NewReader(buf.Bytes()), "application/gzip")
		if err != nil {
			slog.Error("event archiver: GCS upload failed, aborting batch", "error", err)
			return
		}

		deleted, err := es.DeleteEventsByIDs(ctx, ids)
		if err != nil {
			slog.Error("event archiver: ES prune failed", "error", err)
			return
		}

		totalArchived += len(events)
		totalDeleted += deleted
		slog.Info("event archiver: batch complete",
			"batch_archived", len(events), "batch_deleted", deleted)
	}

	if totalArchived == 0 {
		slog.Info("event archiver: no stale events")
		return
	}

	slog.Info("event archiver: complete",
		"total_archived", totalArchived, "total_deleted", totalDeleted)
}
