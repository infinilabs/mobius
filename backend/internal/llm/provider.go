// Package llm holds the LLM provider abstraction (plan 6.4e): the
// request/response types, the provider interface, and the registry, with
// Gemini (Vertex/Studio) and Claude implementations.
package llm

import (
	"context"
	"mobius/internal/domain"
	"strings"
)

// ToolDef is a provider-neutral tool/function definition.
// Parameters uses standard JSON Schema, which all providers accept.
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ToolCall represents a function call emitted by a model.
type ToolCall struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

// LLMMessage is a provider-neutral chat message.
type LLMMessage struct {
	Role  string           `json:"role"`
	Text  string           `json:"text"`
	Files []domain.FileRef `json:"files,omitempty"`
}

// LLMRequest contains everything needed for a single chat turn.
type LLMRequest struct {
	Model        string
	SystemPrompt string
	Messages     []LLMMessage
	Tools        []ToolDef
	OnText       func(text string)
	OnToolCall   func(call ToolCall) map[string]any
	OnToolEvent  func(name, status string)
	OnUsage      func(usage domain.TokenUsage)
}

// domain.TokenUsage lives in internal/domain (plan 6.2); see domain_aliases.go.

// LLMProvider abstracts chat + tool-calling across LLM backends.
type LLMProvider interface {
	ChatStream(ctx context.Context, req *LLMRequest) (string, error)
}

// ProviderRegistry maps provider names to instances.
type ProviderRegistry struct {
	providers map[string]LLMProvider
}

func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{providers: make(map[string]LLMProvider)}
}

func (r *ProviderRegistry) Register(name string, p LLMProvider) {
	r.providers[name] = p
}

func (r *ProviderRegistry) Get(name string) LLMProvider {
	return r.providers[name]
}

func (r *ProviderRegistry) ResolveProvider(modelID string) LLMProvider {
	if strings.HasPrefix(modelID, "claude-") {
		return r.Get("claude")
	}
	return r.Get("gemini")
}
