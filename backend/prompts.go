package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const IdxPrompts = "mobius_prompts"

type Prompt struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Content   string   `json:"content"`
	Tags      []string `json:"tags"`
	CreatedAt int64    `json:"created_at"`
	UpdatedAt int64    `json:"updated_at"`
}

// ES operations

func (es *ESClient) IndexPrompt(ctx context.Context, p *Prompt) error {
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

func (es *ESClient) GetPrompt(ctx context.Context, id string) (*Prompt, error) {
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
		Source Prompt `json:"_source"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ES decode prompt failed: %w", err)
	}
	return &result.Source, nil
}

func (es *ESClient) SearchPrompts(ctx context.Context, query string) ([]Prompt, error) {
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
				Source Prompt `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ES decode search results failed: %w", err)
	}

	prompts := make([]Prompt, 0, len(result.Hits.Hits))
	for _, hit := range result.Hits.Hits {
		prompts = append(prompts, hit.Source)
	}
	return prompts, nil
}

func (es *ESClient) DeletePrompt(ctx context.Context, id string) error {
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

// HTTP handlers

func (h *APIHandler) ListPrompts(w http.ResponseWriter, r *http.Request) {
	if h.esClient == nil {
		writeError(w, "Elasticsearch not available", http.StatusServiceUnavailable)
		return
	}

	query := r.URL.Query().Get("q")
	prompts, err := h.esClient.SearchPrompts(r.Context(), query)
	if err != nil {
		writeError(w, "failed to search prompts: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, prompts)
}

func (h *APIHandler) GetPrompt(w http.ResponseWriter, r *http.Request) {
	if h.esClient == nil {
		writeError(w, "Elasticsearch not available", http.StatusServiceUnavailable)
		return
	}

	id := r.PathValue("id")
	prompt, err := h.esClient.GetPrompt(r.Context(), id)
	if err != nil {
		writeError(w, "prompt not found", http.StatusNotFound)
		return
	}
	writeJSON(w, prompt)
}

func (h *APIHandler) CreatePrompt(w http.ResponseWriter, r *http.Request) {
	if h.esClient == nil {
		writeError(w, "Elasticsearch not available", http.StatusServiceUnavailable)
		return
	}

	var body struct {
		Title   string   `json:"title"`
		Content string   `json:"content"`
		Tags    []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.Title == "" || body.Content == "" {
		writeError(w, "title and content are required", http.StatusBadRequest)
		return
	}

	now := time.Now().UnixMilli()
	p := &Prompt{
		ID:        generateID(),
		Title:     body.Title,
		Content:   body.Content,
		Tags:      body.Tags,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if p.Tags == nil {
		p.Tags = []string{}
	}

	if err := h.esClient.IndexPrompt(r.Context(), p); err != nil {
		writeError(w, "failed to save prompt: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("prompt created", "id", p.ID, "title", p.Title)
	writeJSON(w, p)
}

func (h *APIHandler) UpdatePrompt(w http.ResponseWriter, r *http.Request) {
	if h.esClient == nil {
		writeError(w, "Elasticsearch not available", http.StatusServiceUnavailable)
		return
	}

	id := r.PathValue("id")
	existing, err := h.esClient.GetPrompt(r.Context(), id)
	if err != nil {
		writeError(w, "prompt not found", http.StatusNotFound)
		return
	}

	var body struct {
		Title   string   `json:"title"`
		Content string   `json:"content"`
		Tags    []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if body.Title != "" {
		existing.Title = body.Title
	}
	if body.Content != "" {
		existing.Content = body.Content
	}
	if body.Tags != nil {
		existing.Tags = body.Tags
	}
	existing.UpdatedAt = time.Now().UnixMilli()

	if err := h.esClient.IndexPrompt(r.Context(), existing); err != nil {
		writeError(w, "failed to update prompt: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("prompt updated", "id", id)
	writeJSON(w, existing)
}

func seedPrompts(ctx context.Context, es *ESClient, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read prompts dir: %w", err)
	}

	seeded := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			slog.Error("failed to read prompt file", "file", entry.Name(), "error", err)
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".md")
		hash := sha256.Sum256([]byte(name))
		id := hex.EncodeToString(hash[:8])

		_, err = es.GetPrompt(ctx, id)
		if err == nil {
			continue
		}

		title := strings.ReplaceAll(name, "_", " ")
		now := time.Now().UnixMilli()
		p := &Prompt{
			ID:        id,
			Title:     title,
			Content:   string(content),
			Tags:      []string{"template"},
			CreatedAt: now,
			UpdatedAt: now,
		}

		if err := es.IndexPrompt(ctx, p); err != nil {
			slog.Error("failed to seed prompt", "file", entry.Name(), "error", err)
			continue
		}
		seeded++
		slog.Info("prompt seeded", "id", id, "title", title)
	}

	if seeded > 0 {
		slog.Info("prompt seeding complete", "seeded", seeded)
	}
	return nil
}

func (h *APIHandler) DeletePrompt(w http.ResponseWriter, r *http.Request) {
	if h.esClient == nil {
		writeError(w, "Elasticsearch not available", http.StatusServiceUnavailable)
		return
	}

	id := r.PathValue("id")
	if err := h.esClient.DeletePrompt(r.Context(), id); err != nil {
		writeError(w, "failed to delete prompt: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("prompt deleted", "id", id)
	writeJSON(w, map[string]string{"status": "ok"})
}
