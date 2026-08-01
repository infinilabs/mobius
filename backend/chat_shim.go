package main

// Transitional aliases (plan 6.4e): the conversation store lives in
// internal/chat.

import (
	"mobius/internal/chat"
	"mobius/internal/domain"
)

type ConversationStore = chat.ConversationStore

var (
	NewConversationStore = chat.NewConversationStore
	SaveConversation     = chat.SaveConversation
)

func generateID() string { return domain.NewID() }
