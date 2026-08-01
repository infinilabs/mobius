package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mobius/internal/domain"
	"strings"
)

const IdxPrompts = "mobius_prompts"

// ES operations

func (es *Client) IndexPrompt(ctx context.Context, p *domain.Prompt) error {
	body, _ := json.Marshal(p)
	res, err := es.client.Index(IdxPrompts, bytes.NewReader(body),
		es.client.Index.WithContext(ctx),
		es.client.Index.WithDocumentID(p.ID),
	)
	if err != nil {
		return fmt.Errorf("ES index prompt failed: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("ES index prompt error: %s", res.String())
	}
	return nil
}

func (es *Client) GetPrompt(ctx context.Context, id string) (*domain.Prompt, error) {
	res, err := es.client.Get(IdxPrompts, id,
		es.client.Get.WithContext(ctx),
	)
	if err != nil {
		return nil, fmt.Errorf("ES get prompt failed: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("ES get prompt error: %s", res.Status())
	}

	var result struct {
		Source domain.Prompt `json:"_source"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ES decode prompt failed: %w", err)
	}
	return &result.Source, nil
}

func (es *Client) SearchPrompts(ctx context.Context, query string) ([]domain.Prompt, error) {
	var body map[string]any
	if query == "" {
		body = map[string]any{
			"query": map[string]any{"match_all": map[string]any{}},
			"sort":  []any{map[string]any{"updated_at": "desc"}},
			"size":  200,
		}
	} else {
		words := strings.Fields(query)
		wildcards := make([]string, len(words))
		for i, w := range words {
			wildcards[i] = "*" + w + "*"
		}
		qs := strings.Join(wildcards, " AND ")

		body = map[string]any{
			"query": map[string]any{
				"query_string": map[string]any{
					"query":            qs,
					"fields":           []string{"title^2", "content", "tags"},
					"default_operator": "AND",
					"analyze_wildcard": true,
				},
			},
			"size": 200,
		}
	}

	buf, _ := json.Marshal(body)
	res, err := es.client.Search(
		es.client.Search.WithContext(ctx),
		es.client.Search.WithIndex(IdxPrompts),
		es.client.Search.WithBody(bytes.NewReader(buf)),
	)
	if err != nil {
		return nil, fmt.Errorf("ES search prompts failed: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("ES search prompts error: %s", res.String())
	}

	var result struct {
		Hits struct {
			Hits []struct {
				Source domain.Prompt `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ES decode search results failed: %w", err)
	}

	prompts := make([]domain.Prompt, 0, len(result.Hits.Hits))
	for _, hit := range result.Hits.Hits {
		prompts = append(prompts, hit.Source)
	}
	return prompts, nil
}

func (es *Client) DeletePrompt(ctx context.Context, id string) error {
	res, err := es.client.Delete(IdxPrompts, id,
		es.client.Delete.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("ES delete prompt failed: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("ES delete prompt error: %s", res.String())
	}
	return nil
}
