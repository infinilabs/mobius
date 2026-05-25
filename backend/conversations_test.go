package main

import (
	"testing"
	"time"
)

func TestConversationStore_CreateAndGet(t *testing.T) {
	s := NewConversationStore()
	c := s.Create()
	if c.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if c.Title != "New Chat" {
		t.Errorf("expected default title 'New Chat', got %q", c.Title)
	}

	got := s.Get(c.ID)
	if got == nil || got.ID != c.ID {
		t.Error("Get should return the created conversation")
	}

	if s.Get("nonexistent") != nil {
		t.Error("Get should return nil for unknown ID")
	}
}

func TestConversationStore_AddMessage_AutoTitle(t *testing.T) {
	s := NewConversationStore()
	c := s.Create()

	s.AddMessage(c.ID, Message{ID: "m1", Role: "user", Content:"How do I configure authentication?"})

	got := s.Get(c.ID)
	if got.Title == "New Chat" {
		t.Error("title should be updated from first user message")
	}
	if len(got.Title) > 40 {
		t.Errorf("title should be truncated to 40 chars, got %d", len(got.Title))
	}
}

func TestConversationStore_AddMessage_KeepsCustomTitle(t *testing.T) {
	s := NewConversationStore()
	c := s.Create()
	s.Rename(c.ID, "My Custom Title")

	s.AddMessage(c.ID, Message{ID: "m1", Role: "user", Content:"some question"})

	got := s.Get(c.ID)
	if got.Title != "My Custom Title" {
		t.Errorf("custom title should be preserved, got %q", got.Title)
	}
}

func TestConversationStore_Delete(t *testing.T) {
	s := NewConversationStore()
	c := s.Create()
	if !s.Delete(c.ID) {
		t.Error("Delete should return true for existing conversation")
	}
	if s.Get(c.ID) != nil {
		t.Error("Get should return nil after deletion")
	}
	if s.Delete(c.ID) {
		t.Error("Delete should return false for already-deleted conversation")
	}
}

func TestConversationStore_List_SortedByUpdatedAt(t *testing.T) {
	s := NewConversationStore()
	c1 := s.Create()
	time.Sleep(5 * time.Millisecond)
	c2 := s.Create()
	time.Sleep(5 * time.Millisecond)

	s.AddMessage(c1.ID, Message{ID: "m1", Role: "user", Content:"update c1"})

	list := s.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 conversations, got %d", len(list))
	}
	if list[0].ID != c1.ID {
		t.Errorf("most recently updated conversation (c1) should be first, got %s", list[0].ID)
	}
	_ = c2
}

func TestConversationStore_TruncateAt(t *testing.T) {
	s := NewConversationStore()
	c := s.Create()

	for i := 0; i < 5; i++ {
		s.AddMessage(c.ID, Message{ID: generateID(), Role: "user", Content:"msg"})
	}

	if !s.TruncateAt(c.ID, 2) {
		t.Error("TruncateAt should return true for existing conversation")
	}
	got := s.Get(c.ID)
	if len(got.Messages) != 2 {
		t.Errorf("expected 2 messages after truncation, got %d", len(got.Messages))
	}
}

func TestConversationStore_TruncateAt_OverCount(t *testing.T) {
	s := NewConversationStore()
	c := s.Create()
	s.AddMessage(c.ID, Message{ID: "m1", Role: "user", Content:"only one"})

	if !s.TruncateAt(c.ID, 100) {
		t.Error("TruncateAt should succeed even if keepCount > len(messages)")
	}
	got := s.Get(c.ID)
	if len(got.Messages) != 1 {
		t.Errorf("messages should be preserved when keepCount > len, got %d", len(got.Messages))
	}
}

func TestConversationStore_SetProjectID(t *testing.T) {
	s := NewConversationStore()
	c := s.Create()

	s.SetProjectID(c.ID, "proj-123")
	got := s.Get(c.ID)
	if got.ProjectID == nil || *got.ProjectID != "proj-123" {
		t.Error("SetProjectID should set project ID")
	}

	s.SetProjectID(c.ID, "proj-456")
	got = s.Get(c.ID)
	if *got.ProjectID != "proj-123" {
		t.Error("SetProjectID should not overwrite existing project ID")
	}
}
