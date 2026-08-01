// Package chat holds interactive-chat state (plan 6.4e): the in-memory
// conversation store and its on-disk persistence.
package chat

import (
	"encoding/json"
	"fmt"
	"mobius/internal/config"
	"mobius/internal/domain"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type ConversationStore struct {
	mu    sync.RWMutex
	convs map[string]*domain.Conversation
}

func NewConversationStore() *ConversationStore {
	return &ConversationStore{convs: make(map[string]*domain.Conversation)}
}

func (s *ConversationStore) Hydrate(convs map[string]*domain.Conversation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, c := range convs {
		s.convs[id] = c
	}
}

func (s *ConversationStore) All() []*domain.Conversation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*domain.Conversation, 0, len(s.convs))
	for _, c := range s.convs {
		out = append(out, c)
	}
	return out
}

func (s *ConversationStore) Create() *domain.Conversation {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixMilli()
	c := &domain.Conversation{
		ID:        domain.NewID(),
		Title:     "New Chat",
		Messages:  []domain.Message{},
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.convs[c.ID] = c
	return c
}

func (s *ConversationStore) Get(id string) *domain.Conversation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.convs[id]
}

func (s *ConversationStore) SetProjectID(id, projectID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.convs[id]; ok && c.ProjectID == nil {
		c.ProjectID = &projectID
	}
}

func (s *ConversationStore) List() []domain.ConversationSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]domain.ConversationSummary, 0, len(s.convs))
	for _, c := range s.convs {
		out = append(out, domain.ConversationSummary{
			ID:        c.ID,
			Title:     c.Title,
			ProjectID: c.ProjectID,
			UpdatedAt: c.UpdatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	return out
}

func (s *ConversationStore) AddMessage(id string, msg domain.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, ok := s.convs[id]
	if !ok {
		return
	}
	if msg.ID == "" {
		msg.ID = domain.NewID()
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

func ConversationFilePath(cfg *config.Config, convID string, project *domain.Project) string {
	if project != nil {
		return filepath.Join(project.RootDir(cfg.Projects.ProjectsDir), ".conversations", convID+".json")
	}
	return filepath.Join(cfg.Projects.ConversationsDir, convID+".json")
}

func SaveConversation(cfg *config.Config, conv *domain.Conversation, project *domain.Project) error {
	path := ConversationFilePath(cfg, conv.ID, project)
	os.MkdirAll(filepath.Dir(path), 0755)
	data, err := json.MarshalIndent(conv, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal conversation: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}
