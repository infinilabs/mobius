package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mobius/internal/events"
	"time"
)

// Dispatch/audit event documents.
// Split from es.go (plan 6.5).

func (es *Client) BulkIndexEvents(ctx context.Context, events []*events.Event) error {
	var buf bytes.Buffer
	for _, evt := range events {
		meta := map[string]any{"index": map[string]any{"_index": IdxEvents, "_id": evt.ID}}
		metaBytes, _ := json.Marshal(meta)
		buf.Write(metaBytes)
		buf.WriteByte('\n')
		docBytes, _ := json.Marshal(evt)
		buf.Write(docBytes)
		buf.WriteByte('\n')
	}

	res, err := es.client.Bulk(bytes.NewReader(buf.Bytes()), es.client.Bulk.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("ES bulk index events failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("ES bulk index events error: %s", res.String())
	}
	return nil
}

func (es *Client) SearchEvents(ctx context.Context, eventType, actorID, projectID, taskID string, since, until time.Time, size int) ([]events.Event, error) {
	var filters []any

	if eventType != "" {
		filters = append(filters, map[string]any{"term": map[string]any{"event_type": eventType}})
	}
	if actorID != "" {
		filters = append(filters, map[string]any{"term": map[string]any{"actor_id": actorID}})
	}
	if projectID != "" {
		filters = append(filters, map[string]any{"term": map[string]any{"project_id": projectID}})
	}
	if taskID != "" {
		filters = append(filters, map[string]any{"term": map[string]any{"task_id": taskID}})
	}
	if !since.IsZero() || !until.IsZero() {
		rangeQ := map[string]any{}
		if !since.IsZero() {
			rangeQ["gte"] = since.Format(time.RFC3339Nano)
		}
		if !until.IsZero() {
			rangeQ["lte"] = until.Format(time.RFC3339Nano)
		}
		filters = append(filters, map[string]any{"range": map[string]any{"timestamp": rangeQ}})
	}
	if len(filters) == 0 {
		filters = append(filters, map[string]any{"match_all": map[string]any{}})
	}

	body := map[string]any{
		"query": map[string]any{"bool": map[string]any{"filter": filters}},
		"sort":  []any{map[string]any{"timestamp": "desc"}},
		"size":  size,
	}
	buf, _ := json.Marshal(body)
	res, err := es.client.Search(
		es.client.Search.WithContext(ctx),
		es.client.Search.WithIndex(IdxEvents),
		es.client.Search.WithBody(bytes.NewReader(buf)),
	)
	if err != nil {
		return nil, fmt.Errorf("ES search events failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return nil, fmt.Errorf("ES search events error: %s", res.String())
	}

	var result struct {
		Hits struct {
			Hits []struct {
				Source events.Event `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ES decode events failed: %w", err)
	}

	events := make([]events.Event, 0, len(result.Hits.Hits))
	for _, hit := range result.Hits.Hits {
		events = append(events, hit.Source)
	}
	return events, nil
}

func (es *Client) EventStats(ctx context.Context, projectID string, since, until time.Time) (map[string]int, int, error) {
	var filters []any
	if projectID != "" {
		filters = append(filters, map[string]any{"term": map[string]any{"project_id": projectID}})
	}
	if !since.IsZero() || !until.IsZero() {
		rangeQ := map[string]any{}
		if !since.IsZero() {
			rangeQ["gte"] = since.Format(time.RFC3339Nano)
		}
		if !until.IsZero() {
			rangeQ["lte"] = until.Format(time.RFC3339Nano)
		}
		filters = append(filters, map[string]any{"range": map[string]any{"timestamp": rangeQ}})
	}

	query := map[string]any{"match_all": map[string]any{}}
	if len(filters) > 0 {
		query = map[string]any{"bool": map[string]any{"filter": filters}}
	}

	body := map[string]any{
		"size":  0,
		"query": query,
		"aggs": map[string]any{
			"by_type": map[string]any{
				"terms": map[string]any{"field": "event_type", "size": 50},
			},
		},
	}
	buf, _ := json.Marshal(body)
	res, err := es.client.Search(
		es.client.Search.WithContext(ctx),
		es.client.Search.WithIndex(IdxEvents),
		es.client.Search.WithBody(bytes.NewReader(buf)),
	)
	if err != nil {
		return nil, 0, fmt.Errorf("ES event stats failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return nil, 0, fmt.Errorf("ES event stats error: %s", res.String())
	}

	var result struct {
		Hits struct {
			Total struct {
				Value int `json:"value"`
			} `json:"total"`
		} `json:"hits"`
		Aggregations struct {
			ByType struct {
				Buckets []struct {
					Key      string `json:"key"`
					DocCount int    `json:"doc_count"`
				} `json:"buckets"`
			} `json:"by_type"`
		} `json:"aggregations"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, 0, fmt.Errorf("ES decode event stats failed: %w", err)
	}

	counts := make(map[string]int)
	for _, b := range result.Aggregations.ByType.Buckets {
		counts[b.Key] = b.DocCount
	}
	return counts, result.Hits.Total.Value, nil
}

func (es *Client) DeleteEventsOlderThan(ctx context.Context, before time.Time) (int, error) {
	body := map[string]any{
		"query": map[string]any{
			"range": map[string]any{
				"timestamp": map[string]any{"lte": before.Format(time.RFC3339Nano)},
			},
		},
	}
	buf, _ := json.Marshal(body)
	res, err := es.client.DeleteByQuery(
		[]string{IdxEvents},
		bytes.NewReader(buf),
		es.client.DeleteByQuery.WithContext(ctx),
	)
	if err != nil {
		return 0, fmt.Errorf("ES delete stale events failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return 0, fmt.Errorf("ES delete stale events error: %s", res.Status())
	}
	var result struct {
		Deleted int `json:"deleted"`
	}
	json.NewDecoder(res.Body).Decode(&result)
	return result.Deleted, nil
}

func (es *Client) DeleteEventsByIDs(ctx context.Context, ids []string) (int, error) {
	body := map[string]any{
		"query": map[string]any{
			"ids": map[string]any{"values": ids},
		},
	}
	buf, _ := json.Marshal(body)
	res, err := es.client.DeleteByQuery(
		[]string{IdxEvents},
		bytes.NewReader(buf),
		es.client.DeleteByQuery.WithContext(ctx),
	)
	if err != nil {
		return 0, fmt.Errorf("ES delete events by IDs failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return 0, fmt.Errorf("ES delete events by IDs error: %s", res.Status())
	}
	var result struct {
		Deleted int `json:"deleted"`
	}
	json.NewDecoder(res.Body).Decode(&result)
	return result.Deleted, nil
}

func (es *Client) FetchEventsOlderThan(ctx context.Context, before time.Time, batchSize int) ([]events.Event, error) {
	body := map[string]any{
		"query": map[string]any{
			"range": map[string]any{
				"timestamp": map[string]any{"lte": before.Format(time.RFC3339Nano)},
			},
		},
		"sort": []any{map[string]any{"timestamp": "asc"}},
		"size": batchSize,
	}
	buf, _ := json.Marshal(body)
	res, err := es.client.Search(
		es.client.Search.WithContext(ctx),
		es.client.Search.WithIndex(IdxEvents),
		es.client.Search.WithBody(bytes.NewReader(buf)),
	)
	if err != nil {
		return nil, fmt.Errorf("ES fetch stale events failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return nil, fmt.Errorf("ES fetch stale events error: %s", res.String())
	}

	var result struct {
		Hits struct {
			Hits []struct {
				Source events.Event `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ES decode stale events failed: %w", err)
	}

	events := make([]events.Event, 0, len(result.Hits.Hits))
	for _, hit := range result.Hits.Hits {
		events = append(events, hit.Source)
	}
	return events, nil
}
