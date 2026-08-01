package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mobius/internal/domain"
	"time"
)

// domain.Project assets and the creatives library.
// Split from es.go (plan 6.5).

func (es *Client) IndexProjectAsset(ctx context.Context, asset *domain.ProjectAsset) error {
	doc := map[string]any{
		"id":                asset.ID,
		"project_id":        asset.ProjectID,
		"filename":          asset.Filename,
		"relative_path":     asset.RelativePath,
		"absolute_path":     asset.AbsolutePath,
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
		"title":             asset.Title,
		"description":       asset.Description,
		"status":            asset.Status,
		"origin":            asset.Origin,
		"aspect_ratio":      asset.AspectRatio,
		"created_by_id":     asset.CreatedByID,
		"task_id":           asset.TaskID,
		"created_at":        asset.CreatedAt,
		"updated_at":        asset.UpdatedAt,
	}
	// published_at is a date field; omit when empty so ES doesn't reject "".
	if asset.PublishedAt != "" {
		doc["published_at"] = asset.PublishedAt
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

func (es *Client) SearchProjectAssets(ctx context.Context, projectID, query, contentType string, size int) ([]domain.ProjectAsset, error) {
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
				Source domain.ProjectAsset `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ES decode assets failed: %w", err)
	}

	assets := make([]domain.ProjectAsset, 0, len(result.Hits.Hits))
	for _, hit := range result.Hits.Hits {
		a := hit.Source
		if a.Tags == nil {
			a.Tags = []string{}
		}
		assets = append(assets, a)
	}
	return assets, nil
}

func (es *Client) SearchAssetsByTask(ctx context.Context, taskID string) ([]domain.ProjectAsset, error) {
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
				Source domain.ProjectAsset `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ES decode assets by task failed: %w", err)
	}

	assets := make([]domain.ProjectAsset, 0, len(result.Hits.Hits))
	for _, hit := range result.Hits.Hits {
		a := hit.Source
		if a.Tags == nil {
			a.Tags = []string{}
		}
		assets = append(assets, a)
	}
	return assets, nil
}

// SearchCreatives returns visual creative assets across ALL projects for the Creatives
// library UI. Creatives are curated-only: an asset must carry the "creative" tag to
// appear here (added explicitly via "Add to Creatives"). Additional facets narrow
// results: content type, tag, origin, aspect ratio, status, and published-date range.
type CreativeFilters struct {
	Query       string
	ContentType string
	Tag         string
	Origin      string
	AspectRatio string
	Status      string
	DateFrom    string
	DateTo      string
}

func (es *Client) SearchCreatives(ctx context.Context, f CreativeFilters, size int) ([]domain.ProjectAsset, error) {
	// Curated-only: every creative carries the "creative" tag.
	filter := []any{
		map[string]any{"term": map[string]any{"tags": "creative"}},
	}
	if f.ContentType != "" {
		filter = append(filter, map[string]any{"term": map[string]any{"content_type": f.ContentType}})
	}
	if f.Tag != "" {
		filter = append(filter, map[string]any{"term": map[string]any{"tags": f.Tag}})
	}
	if f.Origin != "" {
		filter = append(filter, map[string]any{"term": map[string]any{"origin": f.Origin}})
	}
	if f.AspectRatio != "" {
		filter = append(filter, map[string]any{"term": map[string]any{"aspect_ratio": f.AspectRatio}})
	}
	if f.Status != "" {
		filter = append(filter, map[string]any{"term": map[string]any{"status": f.Status}})
	}
	if f.DateFrom != "" || f.DateTo != "" {
		rng := map[string]any{}
		if f.DateFrom != "" {
			rng["gte"] = f.DateFrom
		}
		if f.DateTo != "" {
			rng["lte"] = f.DateTo
		}
		filter = append(filter, map[string]any{"range": map[string]any{"published_at": rng}})
	}

	boolQuery := map[string]any{"filter": filter}
	if f.Query != "" {
		boolQuery["must"] = []any{
			map[string]any{
				"multi_match": map[string]any{
					"query":  f.Query,
					"fields": []string{"content", "content_summary", "filename", "title", "description", "tags"},
					"type":   "best_fields",
				},
			},
		}
	}

	body := map[string]any{
		"query": map[string]any{"bool": boolQuery},
		"sort":  []any{map[string]any{"published_at": map[string]any{"order": "desc", "missing": "_last"}}},
		"size":  size,
	}

	buf, _ := json.Marshal(body)
	res, err := es.client.Search(
		es.client.Search.WithContext(ctx),
		es.client.Search.WithIndex(IdxProjectAssets),
		es.client.Search.WithBody(bytes.NewReader(buf)),
	)
	if err != nil {
		return nil, fmt.Errorf("ES search creatives failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return nil, fmt.Errorf("ES search creatives error: %s", res.String())
	}

	var result struct {
		Hits struct {
			Hits []struct {
				Source domain.ProjectAsset `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ES decode creatives failed: %w", err)
	}

	assets := make([]domain.ProjectAsset, 0, len(result.Hits.Hits))
	for _, hit := range result.Hits.Hits {
		a := hit.Source
		if a.Tags == nil {
			a.Tags = []string{}
		}
		assets = append(assets, a)
	}
	return assets, nil
}

// SearchCreativeTags returns the distinct tags used across creatives (excluding the
// "creative" marker tag) for the quick-filter chip row.
func (es *Client) SearchCreativeTags(ctx context.Context, size int) ([]string, error) {
	body := map[string]any{
		"size":  0,
		"query": map[string]any{"term": map[string]any{"tags": "creative"}},
		"aggs": map[string]any{
			"tags": map[string]any{"terms": map[string]any{"field": "tags", "size": size}},
		},
	}
	buf, _ := json.Marshal(body)
	res, err := es.client.Search(
		es.client.Search.WithContext(ctx),
		es.client.Search.WithIndex(IdxProjectAssets),
		es.client.Search.WithBody(bytes.NewReader(buf)),
	)
	if err != nil {
		return nil, fmt.Errorf("ES creative tags failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return nil, fmt.Errorf("ES creative tags error: %s", res.String())
	}
	var result struct {
		Aggregations struct {
			Tags struct {
				Buckets []struct {
					Key string `json:"key"`
				} `json:"buckets"`
			} `json:"tags"`
		} `json:"aggregations"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ES decode creative tags failed: %w", err)
	}
	tags := make([]string, 0, len(result.Aggregations.Tags.Buckets))
	for _, b := range result.Aggregations.Tags.Buckets {
		if b.Key == "creative" || b.Key == "" {
			continue
		}
		tags = append(tags, b.Key)
	}
	return tags, nil
}

func (es *Client) GetProjectAsset(ctx context.Context, id string) (*domain.ProjectAsset, error) {
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
		Source domain.ProjectAsset `json:"_source"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ES decode asset failed: %w", err)
	}
	if result.Source.Tags == nil {
		result.Source.Tags = []string{}
	}
	return &result.Source, nil
}

func (es *Client) DeleteProjectAsset(ctx context.Context, id string) error {
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

func (es *Client) DeleteProjectAssets(ctx context.Context, projectID string) error {
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

func (es *Client) CountProjectAssets(ctx context.Context, projectID string) (int, error) {
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

func (es *Client) GetProjectAssetByPath(ctx context.Context, projectID, relativePath string) (*domain.ProjectAsset, error) {
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
				Source domain.ProjectAsset `json:"_source"`
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

func (es *Client) UpdateProjectAssetGCS(ctx context.Context, id, gcsURI, gcsStatus string) error {
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

func (es *Client) ExportProjectAssets(ctx context.Context, projectID string) ([]domain.ProjectAsset, error) {
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
				Source domain.ProjectAsset `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ES decode export failed: %w", err)
	}
	assets := make([]domain.ProjectAsset, 0, len(result.Hits.Hits))
	for _, hit := range result.Hits.Hits {
		a := hit.Source
		if a.Tags == nil {
			a.Tags = []string{}
		}
		assets = append(assets, a)
	}
	return assets, nil
}

func (es *Client) UpdateProjectAssetSummary(ctx context.Context, id, summary string) error {
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

// events.Event operations
