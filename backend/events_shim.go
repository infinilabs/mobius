package main

// Transitional aliases (plan 6.4b): the event pipeline lives in
// internal/events. The constructors below also convert possibly-nil concrete
// clients to untyped-nil interface values so the pipeline's nil checks hold.

import (
	"context"

	"mobius/internal/domain"
	"mobius/internal/events"
)

type (
	Event         = events.Event
	EventPipeline = events.EventPipeline
)

func newEvent(eventType string, actorID, projectID, taskID *string, payload map[string]any) *Event {
	return events.New(eventType, actorID, projectID, taskID, payload)
}

func NewEventPipeline(es *ESClient, bq *BQClient, cfg EventConfig) *EventPipeline {
	var sink events.Sink
	if es != nil {
		sink = es
	}
	var stream events.StreamSink
	if bq != nil {
		stream = bq
	}
	return events.NewEventPipeline(sink, stream, cfg)
}

func StartArchiver(ctx context.Context, cfg *Config, es *ESClient, gcs *GCSClient) {
	if es == nil || gcs == nil {
		// keep the "archiver disabled" decision here where the concrete nils are known
		events.StartArchiver(ctx, cfg, nil, nil)
		return
	}
	events.StartArchiver(ctx, cfg, es, gcs)
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func truncateStr(s string, n int) string { return domain.TruncateStr(s, n) }
