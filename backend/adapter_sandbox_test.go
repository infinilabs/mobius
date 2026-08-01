package main

import (
	"context"
	"testing"
)

// The code-exec adapters must refuse to start at all when the sandbox is
// disabled — a silent host fallback here is the exact hole plan 0.4 closes.
func TestCodeExecAdapters_RequireSandbox(t *testing.T) {
	cfg := &Config{}
	cfg.Sandbox.Enabled = false
	hb := HeartbeatContext{ModelID: "echo pwned", TaskID: "t1"}

	if _, err := NewBashAdapter(cfg).Start(context.Background(), hb); err == nil {
		t.Error("bash adapter started without sandbox; want error")
	}
	if _, err := NewBashAdapter(nil).Start(context.Background(), hb); err == nil {
		t.Error("bash adapter started with nil config; want error")
	}
	if _, err := NewClaudeCodeAdapter(cfg, "", nil).Start(context.Background(), hb); err == nil {
		t.Error("claude_code adapter started without sandbox; want error")
	}
	if _, err := NewClaudeCodeAdapter(nil, "", nil).Start(context.Background(), hb); err == nil {
		t.Error("claude_code adapter started with nil config; want error")
	}
}
