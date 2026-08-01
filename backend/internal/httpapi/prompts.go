package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"mobius/internal/domain"
	"mobius/internal/search"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// domain.Prompt lives in internal/domain (plan 6.4c); see domain_aliases.go.

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
	p := &domain.Prompt{
		ID:        domain.NewID(),
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

func SeedPrompts(ctx context.Context, es *search.Client, dir string) error {
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
		p := &domain.Prompt{
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
