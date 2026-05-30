package main

import (
	"context"
	"sync"
	"time"
)

// terminalRunRetention keeps a finished run in an adapter's map briefly after it
// reaches a terminal state, so the dispatcher's polling Observe (every 30s) can
// still retrieve the final output and token usage before the entry is reclaimed.
const terminalRunRetention = 90 * time.Second

type AdapterType string

const (
	AdapterInternal    AdapterType = "internal_llm"
	AdapterClaudeCode  AdapterType = "claude_code_local"
	AdapterCursor      AdapterType = "cursor_local"
	AdapterCodex       AdapterType = "codex_local"
	AdapterBash        AdapterType = "bash_command"
	AdapterHTTPWebhook AdapterType = "http_webhook"
)

type HeartbeatContext struct {
	TaskID       string
	TaskTitle    string
	TaskBody     string
	TaskResult   string
	TaskStatus   string
	ProjectID    *string
	ProjectName  string
	ProjectDir   string
	AgentID      string
	AgentName    string
	AgentTitle   string
	AgentRole    string
	SystemPrompt string
	ModelID      string
	Env          map[string]string
	Comments     []TaskComment
	Tools        []ToolDef
}

type RunStatus string

const (
	RunActive    RunStatus = "active"
	RunCompleted RunStatus = "completed"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
)

type RunObservation struct {
	Status       RunStatus
	Output       string
	TokenUsage   *TokenUsage
	ErrorMessage string
}

type Adapter interface {
	Type() AdapterType
	Start(ctx context.Context, hb HeartbeatContext) (runID string, err error)
	Observe(ctx context.Context, runID string) (RunObservation, error)
	Stop(ctx context.Context, runID string) error
}

type AdapterRegistry struct {
	mu       sync.RWMutex
	adapters map[AdapterType]Adapter
}

func NewAdapterRegistry() *AdapterRegistry {
	return &AdapterRegistry{adapters: make(map[AdapterType]Adapter)}
}

func (r *AdapterRegistry) Register(t AdapterType, a Adapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[t] = a
}

func (r *AdapterRegistry) Get(t AdapterType) (Adapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[t]
	return a, ok
}
