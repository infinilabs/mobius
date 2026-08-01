// Package search is the Elasticsearch layer (plan 6.4c): entity mirrors,
// conversations/messages, employee memories, project assets, events,
// skills and prompts.
package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"mobius/internal/storage/postgres"
	"os"
	"path/filepath"
	"time"

	elasticsearch "github.com/elastic/go-elasticsearch/v9"
)

// Elasticsearch client core: connection, index bootstrap, backfills.
// Split from es.go (plan 6.5).

const (
	IdxConversations    = "mobius_conversations"
	IdxMessages         = "mobius_messages"
	IdxEmployeeMemories = "mobius_employee_memories"
	IdxProjectAssets    = "mobius_project_assets"
	IdxEvents           = "mobius_events"
	IdxEmployees        = "mobius_employees"
	IdxProjects         = "mobius_projects"
	IdxTasks            = "mobius_tasks"
)

type Client struct {
	client *elasticsearch.Client
}

func New(url string) (*Client, error) {
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

	esClient := &Client{client: client}

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

func (es *Client) CreateIndexIfNotExist(ctx context.Context, indexName, schemaPath string) error {
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

func (es *Client) Ping(ctx context.Context) error {
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

func (es *Client) Refresh(ctx context.Context, indices ...string) error {
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

func timeNowMillis() int64 {
	return time.Now().UnixMilli()
}

// domain.Employee Memory operations

func (es *Client) CountIndex(ctx context.Context, index string) (int64, error) {
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

func (es *Client) BackfillEmployees(ctx context.Context, pg *postgres.Client) error {
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

func (es *Client) BackfillProjects(ctx context.Context, pg *postgres.Client) error {
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

func (es *Client) BackfillTasks(ctx context.Context, pg *postgres.Client) error {
	count, _ := es.CountIndex(ctx, IdxTasks)
	if count > 0 {
		return nil
	}
	tasks, err := pg.ListTasks(ctx, "", "", "", "")
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
