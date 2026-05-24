package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/elastic/go-elasticsearch/v9"
)

const (
	IdxConversations     = "mobius_conversations"
	IdxMessages          = "mobius_messages"
	IdxEmployeeMemories  = "mobius_employee_memories"
)

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

type ESClient struct {
	client *elasticsearch.Client
}

func NewESClient(url string) (*ESClient, error) {
	if url == "" {
		url = "http://localhost:9200"
	}

	cfg := elasticsearch.Config{
		Addresses: []string{url},
	}
	client, err := elasticsearch.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create ES client: %w", err)
	}

	info, err := client.Info()
	if err != nil {
		return nil, fmt.Errorf("ES connection failed: %w", err)
	}
	defer info.Body.Close()

	if info.IsError() {
		return nil, fmt.Errorf("ES returned error: %s", info.String())
	}

	esClient := &ESClient{client: client}

	ctx := context.Background()
	esIndices := []struct {
		name, schema string
	}{
		{IdxConversations, "schemas/elasticsearch/mobius_conversations.json"},
		{IdxMessages, "schemas/elasticsearch/mobius_messages.json"},
		{IdxEmployeeMemories, "schemas/elasticsearch/002_employee_memories.json"},
		{IdxPrompts, "schemas/elasticsearch/mobius_prompts.json"},
		{IdxSkills, "schemas/elasticsearch/mobius_skills.json"},
	}
	for _, idx := range esIndices {
		if err := esClient.CreateIndexIfNotExist(ctx, idx.name, idx.schema); err != nil {
			slog.Error("failed to bootstrap ES index", "index", idx.name, "error", err)
		}
	}

	slog.Info("Elasticsearch connected", "url", url)
	return esClient, nil
}

func (es *ESClient) CreateIndexIfNotExist(ctx context.Context, indexName, schemaPath string) error {
	res, err := es.client.Indices.Exists([]string{indexName}, es.client.Indices.Exists.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("check index %s: %w", indexName, err)
	}
	defer res.Body.Close()

	if res.StatusCode == 200 {
		return nil
	}

	schemaBytes, err := os.ReadFile(schemaPath)
	if err != nil {
		schemaBytes, err = os.ReadFile(filepath.Join("..", schemaPath))
		if err != nil {
			return fmt.Errorf("read ES schema %s: %w", schemaPath, err)
		}
	}

	cRes, err := es.client.Indices.Create(indexName,
		es.client.Indices.Create.WithContext(ctx),
		es.client.Indices.Create.WithBody(bytes.NewReader(schemaBytes)),
	)
	if err != nil {
		return fmt.Errorf("create ES index %s: %w", indexName, err)
	}
	defer cRes.Body.Close()
	if cRes.IsError() {
		return fmt.Errorf("create ES index %s error: %s", indexName, cRes.String())
	}

	slog.Info("ES index created with custom mapping", "index", indexName)
	return nil
}

func (es *ESClient) Ping(ctx context.Context) error {
	res, err := es.client.Info(es.client.Info.WithContext(ctx))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("ES ping error: %s", res.Status())
	}
	return nil
}

func (es *ESClient) IndexConversation(ctx context.Context, conv *Conversation) error {
	doc := map[string]any{
		"id":         conv.ID,
		"title":      conv.Title,
		"turn_count": len(conv.Messages),
		"created_at": conv.CreatedAt,
		"updated_at": conv.UpdatedAt,
	}

	body, _ := json.Marshal(doc)
	res, err := es.client.Index(IdxConversations, bytes.NewReader(body),
		es.client.Index.WithContext(ctx),
		es.client.Index.WithDocumentID(conv.ID),
	)
	if err != nil {
		return fmt.Errorf("ES index conversation failed: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("ES index conversation error: %s", res.String())
	}
	return nil
}

func (es *ESClient) IndexMessage(ctx context.Context, convID string, msg *Message, turnNumber int) error {
	doc := map[string]any{
		"id":              msg.ID,
		"conversation_id": convID,
		"role":            msg.Role,
		"content":         msg.Content,
		"timestamp":       msg.Timestamp,
		"turn_number":     turnNumber,
		"token_count":     msg.TokenCount,
		"summarized":      false,
	}
	if len(msg.Files) > 0 {
		doc["files"] = msg.Files
	}

	body, _ := json.Marshal(doc)
	res, err := es.client.Index(IdxMessages, bytes.NewReader(body),
		es.client.Index.WithContext(ctx),
		es.client.Index.WithDocumentID(msg.ID),
	)
	if err != nil {
		return fmt.Errorf("ES index message failed: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("ES index message error: %s", res.String())
	}
	return nil
}

func (es *ESClient) DeleteConversation(ctx context.Context, convID string) error {
	// Delete the conversation document
	res, err := es.client.Delete(IdxConversations, convID,
		es.client.Delete.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("ES delete conversation failed: %w", err)
	}
	defer res.Body.Close()

	// Delete all messages belonging to this conversation
	query := map[string]any{
		"query": map[string]any{
			"term": map[string]any{
				"conversation_id": convID,
			},
		},
	}
	qBody, _ := json.Marshal(query)
	delRes, err := es.client.DeleteByQuery(
		[]string{IdxMessages},
		bytes.NewReader(qBody),
		es.client.DeleteByQuery.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("ES delete messages failed: %w", err)
	}
	defer delRes.Body.Close()

	if delRes.IsError() {
		return fmt.Errorf("ES delete messages error: %s", delRes.String())
	}

	slog.Info("ES conversation deleted", "id", convID)
	return nil
}

func (es *ESClient) ListAllConversations(ctx context.Context) ([]Conversation, error) {
	query := map[string]any{
		"query": map[string]any{"match_all": map[string]any{}},
		"sort":  []any{map[string]any{"updated_at": "desc"}},
		"size":  10000,
	}

	buf, _ := json.Marshal(query)
	res, err := es.client.Search(
		es.client.Search.WithContext(ctx),
		es.client.Search.WithIndex(IdxConversations),
		es.client.Search.WithBody(bytes.NewReader(buf)),
	)
	if err != nil {
		return nil, fmt.Errorf("ES list conversations failed: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("ES list conversations error: %s", res.String())
	}

	var result struct {
		Hits struct {
			Hits []struct {
				Source struct {
					ID        string `json:"id"`
					Title     string `json:"title"`
					CreatedAt int64  `json:"created_at"`
					UpdatedAt int64  `json:"updated_at"`
				} `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ES decode conversations failed: %w", err)
	}

	convs := make([]Conversation, 0, len(result.Hits.Hits))
	for _, hit := range result.Hits.Hits {
		convs = append(convs, Conversation{
			ID:        hit.Source.ID,
			Title:     hit.Source.Title,
			Messages:  []Message{},
			CreatedAt: hit.Source.CreatedAt,
			UpdatedAt: hit.Source.UpdatedAt,
		})
	}
	return convs, nil
}

func (es *ESClient) ListMessages(ctx context.Context, convID string) ([]Message, error) {
	query := map[string]any{
		"query": map[string]any{
			"term": map[string]any{"conversation_id": convID},
		},
		"sort": []any{map[string]any{"turn_number": "asc"}},
		"size": 10000,
	}

	buf, _ := json.Marshal(query)
	res, err := es.client.Search(
		es.client.Search.WithContext(ctx),
		es.client.Search.WithIndex(IdxMessages),
		es.client.Search.WithBody(bytes.NewReader(buf)),
	)
	if err != nil {
		return nil, fmt.Errorf("ES list messages failed: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("ES list messages error: %s", res.String())
	}

	var result struct {
		Hits struct {
			Hits []struct {
				Source struct {
					ID         string    `json:"id"`
					Role       string    `json:"role"`
					Content    string    `json:"content"`
					Timestamp  int64     `json:"timestamp"`
					TokenCount int       `json:"token_count"`
					Files      []FileRef `json:"files"`
				} `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ES decode messages failed: %w", err)
	}

	msgs := make([]Message, 0, len(result.Hits.Hits))
	for _, hit := range result.Hits.Hits {
		msgs = append(msgs, Message{
			ID:         hit.Source.ID,
			Role:       hit.Source.Role,
			Content:    hit.Source.Content,
			Timestamp:  hit.Source.Timestamp,
			TokenCount: hit.Source.TokenCount,
			Files:      hit.Source.Files,
		})
	}
	return msgs, nil
}

func (es *ESClient) DeleteMessagesBeyond(ctx context.Context, convID string, keepCount int) error {
	query := map[string]any{
		"query": map[string]any{
			"bool": map[string]any{
				"must": []any{
					map[string]any{"term": map[string]any{"conversation_id": convID}},
					map[string]any{"range": map[string]any{"turn_number": map[string]any{"gt": keepCount}}},
				},
			},
		},
	}

	buf, _ := json.Marshal(query)
	res, err := es.client.DeleteByQuery(
		[]string{IdxMessages},
		bytes.NewReader(buf),
		es.client.DeleteByQuery.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("ES delete messages beyond failed: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("ES delete messages beyond error: %s", res.String())
	}
	return nil
}

func (es *ESClient) Refresh(ctx context.Context, indices ...string) error {
	res, err := es.client.Indices.Refresh(
		es.client.Indices.Refresh.WithContext(ctx),
		es.client.Indices.Refresh.WithIndex(indices...),
	)
	if err != nil {
		return fmt.Errorf("ES refresh failed: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("ES refresh error: %s", res.String())
	}
	slog.Info("ES indices refreshed", "indices", indices)
	return nil
}

func (es *ESClient) UpdateConversationTitle(ctx context.Context, convID, title string) error {
	doc := map[string]any{
		"doc": map[string]any{
			"title":      title,
			"updated_at": timeNowMillis(),
		},
	}
	body, _ := json.Marshal(doc)
	res, err := es.client.Update(IdxConversations, convID, bytes.NewReader(body),
		es.client.Update.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("ES update conversation failed: %w", err)
	}
	defer res.Body.Close()
	return nil
}

func timeNowMillis() int64 {
	return time.Now().UnixMilli()
}

// Employee Memory operations

func (es *ESClient) IndexEmployeeMemory(ctx context.Context, employeeID, convID, text string) error {
	now := time.Now().Format(time.RFC3339)
	doc := map[string]any{
		"id":              generateID(),
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

func (es *ESClient) SearchEmployeeMemories(ctx context.Context, employeeID, query string, size int) ([]EmployeeMemory, float64, error) {
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

func (es *ESClient) DeleteEmployeeMemory(ctx context.Context, id string) error {
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

func (es *ESClient) CountEmployeeMemories(ctx context.Context, employeeID string) (int, error) {
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

func (es *ESClient) OldestEmployeeMemory(ctx context.Context, employeeID string) (*EmployeeMemory, error) {
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

func (es *ESClient) IndexEmployeeMemoryDedup(ctx context.Context, employeeID, convID, text string) error {
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
