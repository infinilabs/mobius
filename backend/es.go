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
	IdxProjectAssets     = "mobius_project_assets"
	IdxEvents            = "mobius_events"
	IdxEmployees         = "mobius_employees"
	IdxProjects          = "mobius_projects"
	IdxTasks             = "mobius_tasks"
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

	// A freshly-created ES container accepts TCP before it can serve requests and
	// resets connections mid-boot (~15-30s). Retry the readiness probe with backoff
	// so `wipe -> serve` (ES still starting) doesn't permanently disable ES on a
	// single failed probe.
	const esReadyAttempts = 30
	var lastErr error
	for attempt := 1; attempt <= esReadyAttempts; attempt++ {
		info, infoErr := client.Info()
		if infoErr != nil {
			lastErr = fmt.Errorf("ES connection failed: %w", infoErr)
		} else if info.IsError() {
			lastErr = fmt.Errorf("ES returned error: %s", info.String())
			info.Body.Close()
		} else {
			info.Body.Close()
			lastErr = nil
			break
		}
		if attempt < esReadyAttempts {
			slog.Info("waiting for Elasticsearch to become ready", "url", url, "attempt", attempt)
			time.Sleep(2 * time.Second)
		}
	}
	if lastErr != nil {
		return nil, lastErr
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
		{IdxProjectAssets, "schemas/elasticsearch/003_project_assets.json"},
		{IdxEvents, "schemas/elasticsearch/004_events.json"},
		{IdxEmployees, "schemas/elasticsearch/005_employees.json"},
		{IdxProjects, "schemas/elasticsearch/006_projects.json"},
		{IdxTasks, "schemas/elasticsearch/007_tasks.json"},
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
		"project_id": conv.ProjectID,
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
					ID        string  `json:"id"`
					Title     string  `json:"title"`
					ProjectID *string `json:"project_id"`
					CreatedAt int64   `json:"created_at"`
					UpdatedAt int64   `json:"updated_at"`
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
			ProjectID: hit.Source.ProjectID,
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
	if res.IsError() {
		return fmt.Errorf("ES update conversation error: %s", res.Status())
	}
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

func (es *ESClient) DeleteEmployeeMemories(ctx context.Context, employeeID string) error {
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

// Project Asset operations

func (es *ESClient) IndexProjectAsset(ctx context.Context, asset *ProjectAsset) error {
	doc := map[string]any{
		"id":                asset.ID,
		"project_id":        asset.ProjectID,
		"filename":          asset.Filename,
		"relative_path":     asset.RelativePath,
		"mime_type":         asset.MIMEType,
		"size_bytes":        asset.SizeBytes,
		"content":           asset.Content,
		"content_summary":   asset.ContentSummary,
		"content_truncated": asset.ContentTruncated,
		"content_type":      asset.ContentType,
		"gcs_uri":           asset.GCSURI,
		"gcs_status":        asset.GCSStatus,
		"checksum_sha256":   asset.Checksum,
		"tags":              asset.Tags,
		"created_by":        asset.CreatedByID,
		"task_id":           asset.TaskID,
		"created_at":        asset.CreatedAt,
		"updated_at":        asset.UpdatedAt,
	}

	body, _ := json.Marshal(doc)
	res, err := es.client.Index(IdxProjectAssets, bytes.NewReader(body),
		es.client.Index.WithContext(ctx),
		es.client.Index.WithDocumentID(asset.ID),
	)
	if err != nil {
		return fmt.Errorf("ES index asset failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("ES index asset error: %s", res.String())
	}
	return nil
}

func (es *ESClient) SearchProjectAssets(ctx context.Context, projectID, query, contentType string, size int) ([]ProjectAsset, error) {
	var body map[string]any

	if query == "" {
		filter := []any{map[string]any{"term": map[string]any{"project_id": projectID}}}
		if contentType != "" {
			filter = append(filter, map[string]any{"term": map[string]any{"content_type": contentType}})
		}
		body = map[string]any{
			"query": map[string]any{"bool": map[string]any{"filter": filter}},
			"sort":  []any{map[string]any{"updated_at": "desc"}},
			"size":  size,
		}
	} else {
		filter := []any{map[string]any{"term": map[string]any{"project_id": projectID}}}
		if contentType != "" {
			filter = append(filter, map[string]any{"term": map[string]any{"content_type": contentType}})
		}
		body = map[string]any{
			"query": map[string]any{
				"bool": map[string]any{
					"must": []any{
						map[string]any{
							"multi_match": map[string]any{
								"query":  query,
								"fields": []string{"content", "content_summary", "filename"},
								"type":   "best_fields",
							},
						},
					},
					"filter": filter,
				},
			},
			"size": size,
		}
	}

	buf, _ := json.Marshal(body)
	res, err := es.client.Search(
		es.client.Search.WithContext(ctx),
		es.client.Search.WithIndex(IdxProjectAssets),
		es.client.Search.WithBody(bytes.NewReader(buf)),
	)
	if err != nil {
		return nil, fmt.Errorf("ES search assets failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return nil, fmt.Errorf("ES search assets error: %s", res.String())
	}

	var result struct {
		Hits struct {
			Hits []struct {
				Source ProjectAsset `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ES decode assets failed: %w", err)
	}

	assets := make([]ProjectAsset, 0, len(result.Hits.Hits))
	for _, hit := range result.Hits.Hits {
		a := hit.Source
		if a.Tags == nil {
			a.Tags = []string{}
		}
		assets = append(assets, a)
	}
	return assets, nil
}

func (es *ESClient) SearchAssetsByTask(ctx context.Context, taskID string) ([]ProjectAsset, error) {
	body := map[string]any{
		"query": map[string]any{"term": map[string]any{"task_id": taskID}},
		"sort":  []any{map[string]any{"updated_at": "desc"}},
		"size":  50,
	}
	buf, _ := json.Marshal(body)
	res, err := es.client.Search(
		es.client.Search.WithContext(ctx),
		es.client.Search.WithIndex(IdxProjectAssets),
		es.client.Search.WithBody(bytes.NewReader(buf)),
	)
	if err != nil {
		return nil, fmt.Errorf("ES search assets by task failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return nil, fmt.Errorf("ES search assets by task error: %s", res.String())
	}

	var result struct {
		Hits struct {
			Hits []struct {
				Source ProjectAsset `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ES decode assets by task failed: %w", err)
	}

	assets := make([]ProjectAsset, 0, len(result.Hits.Hits))
	for _, hit := range result.Hits.Hits {
		a := hit.Source
		if a.Tags == nil {
			a.Tags = []string{}
		}
		assets = append(assets, a)
	}
	return assets, nil
}

func (es *ESClient) GetProjectAsset(ctx context.Context, id string) (*ProjectAsset, error) {
	res, err := es.client.Get(IdxProjectAssets, id,
		es.client.Get.WithContext(ctx),
	)
	if err != nil {
		return nil, fmt.Errorf("ES get asset failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return nil, fmt.Errorf("ES get asset error: %s", res.String())
	}

	var result struct {
		Source ProjectAsset `json:"_source"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ES decode asset failed: %w", err)
	}
	if result.Source.Tags == nil {
		result.Source.Tags = []string{}
	}
	return &result.Source, nil
}

func (es *ESClient) DeleteProjectAsset(ctx context.Context, id string) error {
	res, err := es.client.Delete(IdxProjectAssets, id,
		es.client.Delete.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("ES delete asset failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("ES delete asset error: %s", res.String())
	}
	return nil
}

func (es *ESClient) DeleteProjectAssets(ctx context.Context, projectID string) error {
	query := map[string]any{
		"query": map[string]any{
			"term": map[string]any{"project_id": projectID},
		},
	}
	buf, _ := json.Marshal(query)
	res, err := es.client.DeleteByQuery(
		[]string{IdxProjectAssets},
		bytes.NewReader(buf),
		es.client.DeleteByQuery.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("ES delete project assets failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("ES delete project assets error: %s", res.Status())
	}
	return nil
}

func (es *ESClient) CountProjectAssets(ctx context.Context, projectID string) (int, error) {
	body := map[string]any{
		"query": map[string]any{
			"term": map[string]any{"project_id": projectID},
		},
	}
	buf, _ := json.Marshal(body)
	res, err := es.client.Count(
		es.client.Count.WithContext(ctx),
		es.client.Count.WithIndex(IdxProjectAssets),
		es.client.Count.WithBody(bytes.NewReader(buf)),
	)
	if err != nil {
		return 0, fmt.Errorf("ES count assets failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return 0, fmt.Errorf("ES count assets error: %s", res.String())
	}

	var result struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("ES decode count failed: %w", err)
	}
	return result.Count, nil
}

func (es *ESClient) GetProjectAssetByPath(ctx context.Context, projectID, relativePath string) (*ProjectAsset, error) {
	body := map[string]any{
		"query": map[string]any{
			"bool": map[string]any{
				"filter": []any{
					map[string]any{"term": map[string]any{"project_id": projectID}},
					map[string]any{"term": map[string]any{"relative_path": relativePath}},
				},
			},
		},
		"size": 1,
	}
	buf, _ := json.Marshal(body)
	res, err := es.client.Search(
		es.client.Search.WithContext(ctx),
		es.client.Search.WithIndex(IdxProjectAssets),
		es.client.Search.WithBody(bytes.NewReader(buf)),
	)
	if err != nil {
		return nil, fmt.Errorf("ES get asset by path failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return nil, fmt.Errorf("ES get asset by path error: %s", res.String())
	}

	var result struct {
		Hits struct {
			Hits []struct {
				Source ProjectAsset `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, err
	}
	if len(result.Hits.Hits) == 0 {
		return nil, nil
	}
	return &result.Hits.Hits[0].Source, nil
}

func (es *ESClient) UpdateProjectAssetGCS(ctx context.Context, id, gcsURI, gcsStatus string) error {
	doc := map[string]any{
		"doc": map[string]any{
			"gcs_uri":    gcsURI,
			"gcs_status": gcsStatus,
			"updated_at": time.Now().Format(time.RFC3339),
		},
	}
	body, _ := json.Marshal(doc)
	res, err := es.client.Update(IdxProjectAssets, id, bytes.NewReader(body),
		es.client.Update.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("ES update asset GCS failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("ES update asset GCS error: %s", res.Status())
	}
	return nil
}

func (es *ESClient) ExportProjectAssets(ctx context.Context, projectID string) ([]ProjectAsset, error) {
	body := map[string]any{
		"query": map[string]any{
			"term": map[string]any{"project_id": projectID},
		},
		"size": 10000,
		"sort": []any{map[string]any{"created_at": "asc"}},
	}
	buf, _ := json.Marshal(body)
	res, err := es.client.Search(
		es.client.Search.WithContext(ctx),
		es.client.Search.WithIndex(IdxProjectAssets),
		es.client.Search.WithBody(bytes.NewReader(buf)),
	)
	if err != nil {
		return nil, fmt.Errorf("ES export assets failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return nil, fmt.Errorf("ES export assets error: %s", res.String())
	}

	var result struct {
		Hits struct {
			Hits []struct {
				Source ProjectAsset `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ES decode export failed: %w", err)
	}
	assets := make([]ProjectAsset, 0, len(result.Hits.Hits))
	for _, hit := range result.Hits.Hits {
		a := hit.Source
		if a.Tags == nil {
			a.Tags = []string{}
		}
		assets = append(assets, a)
	}
	return assets, nil
}

func (es *ESClient) UpdateProjectAssetSummary(ctx context.Context, id, summary string) error {
	doc := map[string]any{
		"doc": map[string]any{
			"content_summary": summary,
			"updated_at":      time.Now().Format(time.RFC3339),
		},
	}
	body, _ := json.Marshal(doc)
	res, err := es.client.Update(IdxProjectAssets, id, bytes.NewReader(body),
		es.client.Update.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("ES update asset summary failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("ES update asset summary error: %s", res.Status())
	}
	return nil
}

// Event operations

func (es *ESClient) BulkIndexEvents(ctx context.Context, events []*Event) error {
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

func (es *ESClient) SearchEvents(ctx context.Context, eventType, actorID, projectID, taskID string, since, until time.Time, size int) ([]Event, error) {
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
				Source Event `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ES decode events failed: %w", err)
	}

	events := make([]Event, 0, len(result.Hits.Hits))
	for _, hit := range result.Hits.Hits {
		events = append(events, hit.Source)
	}
	return events, nil
}

func (es *ESClient) EventStats(ctx context.Context, projectID string, since, until time.Time) (map[string]int, int, error) {
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

func (es *ESClient) DeleteEventsOlderThan(ctx context.Context, before time.Time) (int, error) {
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

func (es *ESClient) DeleteEventsByIDs(ctx context.Context, ids []string) (int, error) {
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

func (es *ESClient) FetchEventsOlderThan(ctx context.Context, before time.Time, batchSize int) ([]Event, error) {
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
				Source Event `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ES decode stale events failed: %w", err)
	}

	events := make([]Event, 0, len(result.Hits.Hits))
	for _, hit := range result.Hits.Hits {
		events = append(events, hit.Source)
	}
	return events, nil
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

// --- Employee ES operations ---

func (es *ESClient) IndexEmployee(ctx context.Context, emp *Employee) error {
	var managerID string
	if emp.ManagerID != nil {
		managerID = *emp.ManagerID
	}
	doc := map[string]any{
		"id":         emp.ID,
		"name":       emp.Name,
		"title":      emp.Title,
		"role":       emp.Role,
		"backstory":  emp.Backstory,
		"tags":       emp.Tags,
		"manager_id": managerID,
		"avatar_url": emp.AvatarURL,
		"created_at": emp.CreatedAt,
		"updated_at": emp.UpdatedAt,
	}
	body, _ := json.Marshal(doc)
	res, err := es.client.Index(IdxEmployees, bytes.NewReader(body),
		es.client.Index.WithContext(ctx),
		es.client.Index.WithDocumentID(emp.ID),
	)
	if err != nil {
		return fmt.Errorf("ES index employee failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("ES index employee error: %s", res.String())
	}
	return nil
}

func (es *ESClient) DeleteESEmployee(ctx context.Context, id string) error {
	res, err := es.client.Delete(IdxEmployees, id,
		es.client.Delete.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("ES delete employee failed: %w", err)
	}
	defer res.Body.Close()
	return nil
}

type SearchResult struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

func (es *ESClient) SearchEmployees(ctx context.Context, query string, size int) ([]SearchResult, error) {
	var body map[string]any
	if query == "" {
		body = map[string]any{
			"query": map[string]any{"match_all": map[string]any{}},
			"sort":  []any{map[string]any{"updated_at": "desc"}},
			"size":  size,
		}
	} else {
		body = map[string]any{
			"query": map[string]any{
				"bool": map[string]any{
					"should": []any{
						map[string]any{"match_phrase_prefix": map[string]any{"name": map[string]any{"query": query, "boost": 3}}},
						map[string]any{"match": map[string]any{"name": map[string]any{"query": query, "fuzziness": "AUTO"}}},
						map[string]any{"match": map[string]any{"title": map[string]any{"query": query, "fuzziness": "AUTO"}}},
					},
				},
			},
			"size": size,
		}
	}
	bodyJSON, _ := json.Marshal(body)
	res, err := es.client.Search(
		es.client.Search.WithContext(ctx),
		es.client.Search.WithIndex(IdxEmployees),
		es.client.Search.WithBody(bytes.NewReader(bodyJSON)),
	)
	if err != nil {
		return nil, fmt.Errorf("ES search employees failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return nil, fmt.Errorf("ES search employees error: %s", res.String())
	}

	var result struct {
		Hits struct {
			Hits []struct {
				Source struct {
					ID    string `json:"id"`
					Name  string `json:"name"`
					Title string `json:"title"`
				} `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ES decode employees: %w", err)
	}

	out := make([]SearchResult, 0, len(result.Hits.Hits))
	for _, h := range result.Hits.Hits {
		label := h.Source.Name
		if h.Source.Title != "" {
			label += " — " + h.Source.Title
		}
		out = append(out, SearchResult{ID: h.Source.ID, Label: label})
	}
	return out, nil
}

// --- Project ES operations ---

func (es *ESClient) IndexProject(ctx context.Context, p *Project) error {
	var ownerID string
	if p.Owner != nil {
		ownerID = p.Owner.ID
	}
	doc := map[string]any{
		"id":          p.ID,
		"name":        p.Name,
		"description": p.Description,
		"status":      p.Status,
		"owner_id":    ownerID,
		"tags":        p.Tags,
		"source_path": p.SourcePath,
		"created_at":  p.CreatedAt,
		"updated_at":  p.UpdatedAt,
	}
	body, _ := json.Marshal(doc)
	res, err := es.client.Index(IdxProjects, bytes.NewReader(body),
		es.client.Index.WithContext(ctx),
		es.client.Index.WithDocumentID(p.ID),
	)
	if err != nil {
		return fmt.Errorf("ES index project failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("ES index project error: %s", res.String())
	}
	return nil
}

func (es *ESClient) DeleteESProject(ctx context.Context, id string) error {
	res, err := es.client.Delete(IdxProjects, id,
		es.client.Delete.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("ES delete project failed: %w", err)
	}
	defer res.Body.Close()
	return nil
}

func (es *ESClient) SearchProjects(ctx context.Context, query string, size int) ([]SearchResult, error) {
	var body map[string]any
	if query == "" {
		body = map[string]any{
			"query": map[string]any{"match_all": map[string]any{}},
			"sort":  []any{map[string]any{"updated_at": "desc"}},
			"size":  size,
		}
	} else {
		body = map[string]any{
			"query": map[string]any{
				"bool": map[string]any{
					"should": []any{
						map[string]any{"match_phrase_prefix": map[string]any{"name": map[string]any{"query": query, "boost": 3}}},
						map[string]any{"match": map[string]any{"name": map[string]any{"query": query, "fuzziness": "AUTO"}}},
						map[string]any{"match": map[string]any{"description": map[string]any{"query": query}}},
					},
				},
			},
			"size": size,
		}
	}
	bodyJSON, _ := json.Marshal(body)
	res, err := es.client.Search(
		es.client.Search.WithContext(ctx),
		es.client.Search.WithIndex(IdxProjects),
		es.client.Search.WithBody(bytes.NewReader(bodyJSON)),
	)
	if err != nil {
		return nil, fmt.Errorf("ES search projects failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return nil, fmt.Errorf("ES search projects error: %s", res.String())
	}

	var result struct {
		Hits struct {
			Hits []struct {
				Source struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ES decode projects: %w", err)
	}

	out := make([]SearchResult, 0, len(result.Hits.Hits))
	for _, h := range result.Hits.Hits {
		out = append(out, SearchResult{ID: h.Source.ID, Label: h.Source.Name})
	}
	return out, nil
}

// --- Task ES operations ---

func (es *ESClient) IndexTask(ctx context.Context, t *Task) error {
	var assigneeID, creatorID, projectID string
	if t.Assignee != nil {
		assigneeID = t.Assignee.ID
	}
	if t.Creator != nil {
		creatorID = t.Creator.ID
	}
	if t.ProjectID != nil {
		projectID = *t.ProjectID
	}
	doc := map[string]any{
		"id":          t.ID,
		"title":       t.Title,
		"body":        t.Body,
		"status":      t.Status,
		"priority":    t.Priority,
		"assignee_id": assigneeID,
		"creator_id":  creatorID,
		"project_id":  projectID,
		"created_at":  t.CreatedAt,
		"updated_at":  t.UpdatedAt,
	}
	body, _ := json.Marshal(doc)
	res, err := es.client.Index(IdxTasks, bytes.NewReader(body),
		es.client.Index.WithContext(ctx),
		es.client.Index.WithDocumentID(t.ID),
	)
	if err != nil {
		return fmt.Errorf("ES index task failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("ES index task error: %s", res.String())
	}
	return nil
}

func (es *ESClient) DeleteESTask(ctx context.Context, id string) error {
	res, err := es.client.Delete(IdxTasks, id,
		es.client.Delete.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("ES delete task failed: %w", err)
	}
	defer res.Body.Close()
	return nil
}

func (es *ESClient) SearchTasks(ctx context.Context, query string, size int) ([]SearchResult, error) {
	var body map[string]any
	if query == "" {
		body = map[string]any{
			"query": map[string]any{"match_all": map[string]any{}},
			"sort":  []any{map[string]any{"updated_at": "desc"}},
			"size":  size,
		}
	} else {
		body = map[string]any{
			"query": map[string]any{
				"bool": map[string]any{
					"should": []any{
						map[string]any{"match_phrase_prefix": map[string]any{"title": map[string]any{"query": query, "boost": 3}}},
						map[string]any{"match": map[string]any{"title": map[string]any{"query": query, "fuzziness": "AUTO"}}},
						map[string]any{"match": map[string]any{"body": map[string]any{"query": query}}},
					},
				},
			},
			"size": size,
		}
	}
	bodyJSON, _ := json.Marshal(body)
	res, err := es.client.Search(
		es.client.Search.WithContext(ctx),
		es.client.Search.WithIndex(IdxTasks),
		es.client.Search.WithBody(bytes.NewReader(bodyJSON)),
	)
	if err != nil {
		return nil, fmt.Errorf("ES search tasks failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return nil, fmt.Errorf("ES search tasks error: %s", res.String())
	}

	var result struct {
		Hits struct {
			Hits []struct {
				Source struct {
					ID    string `json:"id"`
					Title string `json:"title"`
				} `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ES decode tasks: %w", err)
	}

	out := make([]SearchResult, 0, len(result.Hits.Hits))
	for _, h := range result.Hits.Hits {
		out = append(out, SearchResult{ID: h.Source.ID, Label: h.Source.Title})
	}
	return out, nil
}

// --- ES Backfill ---

func (es *ESClient) CountIndex(ctx context.Context, index string) (int64, error) {
	res, err := es.client.Count(
		es.client.Count.WithContext(ctx),
		es.client.Count.WithIndex(index),
	)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	var result struct {
		Count int64 `json:"count"`
	}
	json.NewDecoder(res.Body).Decode(&result)
	return result.Count, nil
}

func (es *ESClient) BackfillEmployees(ctx context.Context, pg *PGClient) error {
	count, _ := es.CountIndex(ctx, IdxEmployees)
	if count > 0 {
		return nil
	}
	employees, err := pg.ListEmployees(ctx)
	if err != nil {
		return fmt.Errorf("backfill employees: list PG: %w", err)
	}
	for i := range employees {
		if err := es.IndexEmployee(ctx, &employees[i]); err != nil {
			slog.Warn("backfill employee failed", "id", employees[i].ID, "error", err)
		}
	}
	slog.Info("ES backfill employees complete", "count", len(employees))
	return nil
}

func (es *ESClient) BackfillProjects(ctx context.Context, pg *PGClient) error {
	count, _ := es.CountIndex(ctx, IdxProjects)
	if count > 0 {
		return nil
	}
	projects, err := pg.ListProjects(ctx, "")
	if err != nil {
		return fmt.Errorf("backfill projects: list PG: %w", err)
	}
	for i := range projects {
		if err := es.IndexProject(ctx, &projects[i]); err != nil {
			slog.Warn("backfill project failed", "id", projects[i].ID, "error", err)
		}
	}
	slog.Info("ES backfill projects complete", "count", len(projects))
	return nil
}

func (es *ESClient) BackfillTasks(ctx context.Context, pg *PGClient) error {
	count, _ := es.CountIndex(ctx, IdxTasks)
	if count > 0 {
		return nil
	}
	tasks, err := pg.ListTasks(ctx, "", "", "")
	if err != nil {
		return fmt.Errorf("backfill tasks: list PG: %w", err)
	}
	for i := range tasks {
		if err := es.IndexTask(ctx, &tasks[i]); err != nil {
			slog.Warn("backfill task failed", "id", tasks[i].ID, "error", err)
		}
	}
	slog.Info("ES backfill tasks complete", "count", len(tasks))
	return nil
}
