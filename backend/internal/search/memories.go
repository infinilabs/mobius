package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"mobius/internal/domain"
	"time"
)

// Per-employee long-term memories.
// Split from es.go (plan 6.5).

const maxMemoriesPerEmployee = 100

const dedupScoreThreshold = 8.0

type EmployeeMemory struct {
	ID             string  `json:"id"`
	EmployeeID     string  `json:"employee_id"`
	ConversationID string  `json:"conversation_id"`
	MemoryText     string  `json:"memory_text"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
	Score          float64 `json:"-"`
}

func (es *Client) IndexEmployeeMemory(ctx context.Context, employeeID, convID, text string) error {
	now := time.Now().Format(time.RFC3339)
	doc := map[string]any{
		"id":              domain.NewID(),
		"employee_id":     employeeID,
		"conversation_id": convID,
		"memory_text":     text,
		"created_at":      now,
		"updated_at":      now,
	}

	body, _ := json.Marshal(doc)
	res, err := es.client.Index(IdxEmployeeMemories, bytes.NewReader(body),
		es.client.Index.WithContext(ctx),
		es.client.Index.WithDocumentID(doc["id"].(string)),
	)
	if err != nil {
		return fmt.Errorf("ES index memory failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("ES index memory error: %s", res.String())
	}
	return nil
}

func (es *Client) SearchEmployeeMemories(ctx context.Context, employeeID, query string, size int) ([]EmployeeMemory, float64, error) {
	var body map[string]any
	if query == "" {
		body = map[string]any{
			"query": map[string]any{
				"term": map[string]any{"employee_id": employeeID},
			},
			"sort": []any{map[string]any{"created_at": "desc"}},
			"size": size,
		}
	} else {
		body = map[string]any{
			"query": map[string]any{
				"bool": map[string]any{
					"must":   []any{map[string]any{"match": map[string]any{"memory_text": query}}},
					"filter": []any{map[string]any{"term": map[string]any{"employee_id": employeeID}}},
				},
			},
			"size": size,
		}
	}

	buf, _ := json.Marshal(body)
	res, err := es.client.Search(
		es.client.Search.WithContext(ctx),
		es.client.Search.WithIndex(IdxEmployeeMemories),
		es.client.Search.WithBody(bytes.NewReader(buf)),
	)
	if err != nil {
		return nil, 0, fmt.Errorf("ES search memories failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return nil, 0, fmt.Errorf("ES search memories error: %s", res.String())
	}

	var result struct {
		Hits struct {
			Hits []struct {
				Source EmployeeMemory `json:"_source"`
				Score  float64        `json:"_score"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, 0, fmt.Errorf("ES decode memories failed: %w", err)
	}

	var topScore float64
	memories := make([]EmployeeMemory, 0, len(result.Hits.Hits))
	for i, hit := range result.Hits.Hits {
		m := hit.Source
		m.Score = hit.Score
		memories = append(memories, m)
		if i == 0 {
			topScore = hit.Score
		}
	}
	return memories, topScore, nil
}

func (es *Client) DeleteEmployeeMemory(ctx context.Context, id string) error {
	res, err := es.client.Delete(IdxEmployeeMemories, id,
		es.client.Delete.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("ES delete memory failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("ES delete memory error: %s", res.String())
	}
	return nil
}

func (es *Client) DeleteEmployeeMemories(ctx context.Context, employeeID string) error {
	query := map[string]any{
		"query": map[string]any{
			"term": map[string]any{"employee_id": employeeID},
		},
	}
	buf, _ := json.Marshal(query)
	res, err := es.client.DeleteByQuery(
		[]string{IdxEmployeeMemories},
		bytes.NewReader(buf),
		es.client.DeleteByQuery.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("ES delete employee memories failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("ES delete employee memories error: %s", res.Status())
	}
	return nil
}

func (es *Client) CountEmployeeMemories(ctx context.Context, employeeID string) (int, error) {
	body := map[string]any{
		"query": map[string]any{
			"term": map[string]any{"employee_id": employeeID},
		},
	}
	buf, _ := json.Marshal(body)
	res, err := es.client.Count(
		es.client.Count.WithContext(ctx),
		es.client.Count.WithIndex(IdxEmployeeMemories),
		es.client.Count.WithBody(bytes.NewReader(buf)),
	)
	if err != nil {
		return 0, fmt.Errorf("ES count memories failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return 0, fmt.Errorf("ES count memories error: %s", res.String())
	}

	var result struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("ES decode count failed: %w", err)
	}
	return result.Count, nil
}

func (es *Client) OldestEmployeeMemory(ctx context.Context, employeeID string) (*EmployeeMemory, error) {
	body := map[string]any{
		"query": map[string]any{
			"term": map[string]any{"employee_id": employeeID},
		},
		"sort": []any{map[string]any{"created_at": "asc"}},
		"size": 1,
	}
	buf, _ := json.Marshal(body)
	res, err := es.client.Search(
		es.client.Search.WithContext(ctx),
		es.client.Search.WithIndex(IdxEmployeeMemories),
		es.client.Search.WithBody(bytes.NewReader(buf)),
	)
	if err != nil {
		return nil, fmt.Errorf("ES oldest memory failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return nil, fmt.Errorf("ES oldest memory error: %s", res.String())
	}

	var result struct {
		Hits struct {
			Hits []struct {
				Source EmployeeMemory `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ES decode oldest memory failed: %w", err)
	}
	if len(result.Hits.Hits) == 0 {
		return nil, nil
	}
	return &result.Hits.Hits[0].Source, nil
}

// domain.Project Asset operations

func (es *Client) IndexEmployeeMemoryDedup(ctx context.Context, employeeID, convID, text string) error {
	_, topScore, err := es.SearchEmployeeMemories(ctx, employeeID, text, 1)
	if err == nil && topScore >= dedupScoreThreshold {
		slog.Debug("memory dedup: skipping duplicate", "employee_id", employeeID, "score", topScore)
		return nil
	}

	if err := es.IndexEmployeeMemory(ctx, employeeID, convID, text); err != nil {
		return err
	}

	count, cerr := es.CountEmployeeMemories(ctx, employeeID)
	if cerr == nil && count > maxMemoriesPerEmployee {
		oldest, oerr := es.OldestEmployeeMemory(ctx, employeeID)
		if oerr == nil && oldest != nil {
			es.DeleteEmployeeMemory(ctx, oldest.ID)
			slog.Info("memory evicted (cap)", "employee_id", employeeID, "evicted_id", oldest.ID)
		}
	}

	return nil
}

// --- domain.Employee ES operations ---
