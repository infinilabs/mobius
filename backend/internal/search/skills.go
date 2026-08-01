package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mobius/internal/domain"
	"strings"
)

const IdxSkills = "mobius_skills"

func (es *Client) IndexSkill(ctx context.Context, s *domain.Skill) error {
	body, _ := json.Marshal(s)
	res, err := es.client.Index(IdxSkills, bytes.NewReader(body),
		es.client.Index.WithContext(ctx),
		es.client.Index.WithDocumentID(s.ID),
	)
	if err != nil {
		return fmt.Errorf("ES index skill failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("ES index skill error: %s", res.String())
	}
	return nil
}

func (es *Client) GetSkill(ctx context.Context, id string) (*domain.Skill, error) {
	res, err := es.client.Get(IdxSkills, id,
		es.client.Get.WithContext(ctx),
	)
	if err != nil {
		return nil, fmt.Errorf("ES get skill failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return nil, fmt.Errorf("ES get skill error: %s", res.Status())
	}

	var result struct {
		Source domain.Skill `json:"_source"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ES decode skill failed: %w", err)
	}
	return &result.Source, nil
}

func (es *Client) SearchSkills(ctx context.Context, query string) ([]domain.Skill, error) {
	var body map[string]any
	if query == "" {
		body = map[string]any{
			"query": map[string]any{"match_all": map[string]any{}},
			"sort":  []any{map[string]any{"name.keyword": map[string]any{"order": "asc", "unmapped_type": "keyword"}}},
			"size":  500,
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
					"fields":           []string{"name^3", "description^2", "content", "tags", "category"},
					"default_operator": "AND",
					"analyze_wildcard": true,
				},
			},
			"size": 500,
		}
	}

	buf, _ := json.Marshal(body)
	res, err := es.client.Search(
		es.client.Search.WithContext(ctx),
		es.client.Search.WithIndex(IdxSkills),
		es.client.Search.WithBody(bytes.NewReader(buf)),
	)
	if err != nil {
		return nil, fmt.Errorf("ES search skills failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return nil, fmt.Errorf("ES search skills error: %s", res.String())
	}

	var result struct {
		Hits struct {
			Hits []struct {
				Source domain.Skill `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ES decode skill search failed: %w", err)
	}

	skills := make([]domain.Skill, 0, len(result.Hits.Hits))
	for _, hit := range result.Hits.Hits {
		skills = append(skills, hit.Source)
	}
	return skills, nil
}

func (es *Client) DeleteSkill(ctx context.Context, id string) error {
	res, err := es.client.Delete(IdxSkills, id,
		es.client.Delete.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("ES delete skill failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("ES delete skill error: %s", res.String())
	}
	return nil
}
