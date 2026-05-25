package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Message struct {
	ID         string    `json:"id"`
	Role       string    `json:"role"`
	Content    string    `json:"content"`
	Timestamp  int64     `json:"timestamp"`
	TokenCount int       `json:"token_count,omitempty"`
	Files      []FileRef `json:"files,omitempty"`
}

type FileRef struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	GCSURI   string `json:"gcs_uri,omitempty"`
	MIMEType string `json:"mime_type,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

type Conversation struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	ProjectID *string   `json:"project_id,omitempty"`
	Messages  []Message `json:"messages"`
	CreatedAt int64     `json:"created_at"`
	UpdatedAt int64     `json:"updated_at"`
}

type ConversationSummary struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	ProjectID *string `json:"project_id,omitempty"`
	UpdatedAt int64   `json:"updated_at"`
}

type ConversationStore struct {
	mu    sync.RWMutex
	convs map[string]*Conversation
}

func NewConversationStore() *ConversationStore {
	return &ConversationStore{convs: make(map[string]*Conversation)}
}

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func (s *ConversationStore) Hydrate(convs map[string]*Conversation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, c := range convs {
		s.convs[id] = c
	}
}

func (s *ConversationStore) All() []*Conversation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Conversation, 0, len(s.convs))
	for _, c := range s.convs {
		out = append(out, c)
	}
	return out
}

func (s *ConversationStore) Create() *Conversation {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixMilli()
	c := &Conversation{
		ID:        generateID(),
		Title:     "New Chat",
		Messages:  []Message{},
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.convs[c.ID] = c
	return c
}

func (s *ConversationStore) Get(id string) *Conversation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.convs[id]
}

func (s *ConversationStore) List() []ConversationSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]ConversationSummary, 0, len(s.convs))
	for _, c := range s.convs {
		out = append(out, ConversationSummary{
			ID:        c.ID,
			Title:     c.Title,
			UpdatedAt: c.UpdatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	return out
}

func (s *ConversationStore) AddMessage(id string, msg Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, ok := s.convs[id]
	if !ok {
		return
	}
	if msg.ID == "" {
		msg.ID = generateID()
	}
	c.Messages = append(c.Messages, msg)
	c.UpdatedAt = time.Now().UnixMilli()

	if msg.Role == "user" && c.Title == "New Chat" {
		title := msg.Content
		if len(title) > 40 {
			title = title[:40] + "..."
		}
		c.Title = title
	}
}

func (s *ConversationStore) TruncateAt(id string, keepCount int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, ok := s.convs[id]
	if !ok {
		return false
	}
	if keepCount < 0 {
		keepCount = 0
	}
	if keepCount < len(c.Messages) {
		c.Messages = c.Messages[:keepCount]
		c.UpdatedAt = time.Now().UnixMilli()
	}
	return true
}

func (s *ConversationStore) Rename(id, title string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, ok := s.convs[id]
	if !ok {
		return false
	}
	c.Title = title
	c.UpdatedAt = time.Now().UnixMilli()
	return true
}

func (s *ConversationStore) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.convs[id]; !ok {
		return false
	}
	delete(s.convs, id)
	return true
}

// Disk persistence

func conversationFilePath(cfg *Config, convID string, project *Project) string {
	if project != nil {
		return filepath.Join(project.RootDir(cfg), ".conversations", convID+".json")
	}
	return filepath.Join(cfg.Projects.ConversationsDir, convID+".json")
}

func SaveConversation(cfg *Config, conv *Conversation, project *Project) error {
	path := conversationFilePath(cfg, conv.ID, project)
	os.MkdirAll(filepath.Dir(path), 0755)
	data, err := json.MarshalIndent(conv, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal conversation: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

func LoadConversation(cfg *Config, convID string, project *Project) (*Conversation, error) {
	path := conversationFilePath(cfg, convID, project)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read conversation: %w", err)
	}
	var conv Conversation
	if err := json.Unmarshal(data, &conv); err != nil {
		return nil, fmt.Errorf("unmarshal conversation: %w", err)
	}
	return &conv, nil
}

// PG conversation metadata

func (pg *PGClient) UpsertConversationMeta(ctx context.Context, conv *Conversation) error {
	_, err := pg.pool.Exec(ctx, `
		INSERT INTO conversations (id, title, project_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE SET title = $2, project_id = $3, updated_at = $5
	`, conv.ID, conv.Title, conv.ProjectID, conv.CreatedAt, conv.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert conversation meta: %w", err)
	}
	return nil
}

func (pg *PGClient) ListConversationsMeta(ctx context.Context, projectID string) ([]ConversationSummary, error) {
	query := "SELECT id, title, project_id, updated_at FROM conversations"
	var args []any
	if projectID != "" {
		query += " WHERE project_id = $1"
		args = append(args, projectID)
	}
	query += " ORDER BY updated_at DESC"

	rows, err := pg.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list conversation meta: %w", err)
	}
	defer rows.Close()

	var out []ConversationSummary
	for rows.Next() {
		var s ConversationSummary
		if err := rows.Scan(&s.ID, &s.Title, &s.ProjectID, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan conversation meta: %w", err)
		}
		out = append(out, s)
	}
	if out == nil {
		out = []ConversationSummary{}
	}
	return out, nil
}

// HTTP handlers

func (h *APIHandler) ListConversations(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.conversations.List())
}

func (h *APIHandler) GetConversation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c := h.conversations.Get(id)
	if c == nil {
		writeError(w, "conversation not found", http.StatusNotFound)
		return
	}
	writeJSON(w, c)
}

func (h *APIHandler) CreateConversation(w http.ResponseWriter, r *http.Request) {
	c := h.conversations.Create()
	slog.Info("conversation created", "id", c.ID)

	if h.esClient != nil {
		if err := h.esClient.IndexConversation(r.Context(), c); err != nil {
			slog.Error("ES index conversation failed", "id", c.ID, "error", err)
		}
	}
	if h.pgClient != nil {
		if err := h.pgClient.UpsertConversationMeta(r.Context(), c); err != nil {
			slog.Error("PG upsert conversation failed", "id", c.ID, "error", err)
		}
	}
	SaveConversation(h.config, c, nil)

	writeJSON(w, c)
}

func (h *APIHandler) RenameConversation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Title == "" {
		writeError(w, "title is required", http.StatusBadRequest)
		return
	}
	if !h.conversations.Rename(id, body.Title) {
		writeError(w, "conversation not found", http.StatusNotFound)
		return
	}

	if h.esClient != nil {
		if err := h.esClient.UpdateConversationTitle(r.Context(), id, body.Title); err != nil {
			slog.Error("ES rename conversation failed", "id", id, "error", err)
		}
	}

	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *APIHandler) TruncateConversation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		KeepCount int `json:"keep_count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if !h.conversations.TruncateAt(id, body.KeepCount) {
		writeError(w, "conversation not found", http.StatusNotFound)
		return
	}

	c := h.conversations.Get(id)

	if h.esClient != nil {
		if err := h.esClient.DeleteMessagesBeyond(r.Context(), id, body.KeepCount); err != nil {
			slog.Error("ES truncate messages failed", "id", id, "error", err)
		}
		if err := h.esClient.IndexConversation(r.Context(), c); err != nil {
			slog.Error("ES update conversation after truncate failed", "id", id, "error", err)
		}
	}

	slog.Info("conversation truncated", "id", id, "keep_count", body.KeepCount)
	writeJSON(w, c)
}

func (h *APIHandler) DeleteConversation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	conv := h.conversations.Get(id)
	if conv == nil {
		writeError(w, "conversation not found", http.StatusNotFound)
		return
	}

	var files []FileRef
	for _, msg := range conv.Messages {
		files = append(files, msg.Files...)
	}

	if !h.conversations.Delete(id) {
		writeError(w, "conversation not found", http.StatusNotFound)
		return
	}

	if h.esClient != nil {
		if err := h.esClient.DeleteConversation(r.Context(), id); err != nil {
			slog.Error("ES delete conversation failed", "id", id, "error", err)
		}
	}

	for _, f := range files {
		if f.GCSURI != "" && h.gcsClient != nil {
			if err := h.gcsClient.Delete(r.Context(), f.GCSURI); err != nil {
				slog.Error("GCS delete file failed", "id", f.ID, "uri", f.GCSURI, "error", err)
			}
		}
	}

	slog.Info("conversation deleted", "id", id, "files_cleaned", len(files))
	writeJSON(w, map[string]string{"status": "ok"})
}
