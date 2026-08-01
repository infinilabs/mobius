package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mobius/internal/domain"
)

// domain.Employee/project/task entity mirrors and entity search.
// Split from es.go (plan 6.5).

func (es *Client) IndexEmployee(ctx context.Context, emp *domain.Employee) error {
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

func (es *Client) DeleteESEmployee(ctx context.Context, id string) error {
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

func (es *Client) SearchEmployees(ctx context.Context, query string, size int) ([]SearchResult, error) {
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

// --- domain.Project ES operations ---

func (es *Client) IndexProject(ctx context.Context, p *domain.Project) error {
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

func (es *Client) DeleteESProject(ctx context.Context, id string) error {
	res, err := es.client.Delete(IdxProjects, id,
		es.client.Delete.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("ES delete project failed: %w", err)
	}
	defer res.Body.Close()
	return nil
}

func (es *Client) SearchProjects(ctx context.Context, query string, size int) ([]SearchResult, error) {
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

// --- domain.Task ES operations ---

func (es *Client) IndexTask(ctx context.Context, t *domain.Task) error {
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

func (es *Client) DeleteESTask(ctx context.Context, id string) error {
	res, err := es.client.Delete(IdxTasks, id,
		es.client.Delete.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("ES delete task failed: %w", err)
	}
	defer res.Body.Close()
	return nil
}

func (es *Client) SearchTasks(ctx context.Context, query string, size int) ([]SearchResult, error) {
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
