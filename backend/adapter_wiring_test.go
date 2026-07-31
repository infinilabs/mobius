package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// writeMCPConfig must produce a Claude Code config that points the spawned CLI at
// mobius's WebSocket MCP endpoint with the minted token in the Authorization
// header — that is the entire mechanism by which an external claude agent gains
// authenticated access to mobius tools.
func TestClaudeCodeWriteMCPConfig(t *testing.T) {
	a := NewClaudeCodeAdapter(nil, "ws://localhost:1983/mcp", func(agentID, taskID string) string {
		return "tok-" + agentID + "-" + taskID
	})
	path := a.writeMCPConfig(HeartbeatContext{AgentID: "ag1", TaskID: "tk1"})
	if path == "" {
		t.Fatal("expected a config path")
	}
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg struct {
		McpServers map[string]struct {
			Type    string            `json:"type"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("config is not valid JSON: %v", err)
	}
	m, ok := cfg.McpServers["mobius"]
	if !ok {
		t.Fatal("mobius server entry missing")
	}
	if m.Type != "ws" {
		t.Errorf("transport type = %q, want ws", m.Type)
	}
	if m.URL != "ws://localhost:1983/mcp" {
		t.Errorf("url = %q", m.URL)
	}
	if m.Headers["Authorization"] != "Bearer tok-ag1-tk1" {
		t.Errorf("auth header = %q, want the minted bearer token", m.Headers["Authorization"])
	}
}

// With no minter or no address, MCP wiring is disabled and no config is written
// (the CLI then runs with no mobius tools rather than a broken/unauth config).
func TestClaudeCodeWriteMCPConfig_Disabled(t *testing.T) {
	if p := NewClaudeCodeAdapter(nil, "ws://x/mcp", nil).writeMCPConfig(HeartbeatContext{}); p != "" {
		os.Remove(p)
		t.Errorf("nil minter should produce no config, got %q", p)
	}
	mint := func(_, _ string) string { return "t" }
	if p := NewClaudeCodeAdapter(nil, "", mint).writeMCPConfig(HeartbeatContext{}); p != "" {
		os.Remove(p)
		t.Errorf("empty addr should produce no config, got %q", p)
	}
}

// CompleteRunHandler drives a webhook run to a terminal state. Without it the run
// never completes until the dispatcher's run-ctx cap expires.
func TestHTTPWebhookCompleteRunHandler(t *testing.T) {
	a := NewHTTPWebhookAdapter()
	a.runs.Store("run-1", &httpRun{status: RunActive})
	a.runs.Store("run-2", &httpRun{status: RunActive})

	post := func(bodyJSON string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/runs/complete", strings.NewReader(bodyJSON))
		w := httptest.NewRecorder()
		a.CompleteRunHandler(w, req)
		return w
	}

	// Unknown run id → 404 (capability check: only a worker that received the
	// payload knows the server-generated run id).
	if w := post(`{"run_id":"nope","success":true}`); w.Code != http.StatusNotFound {
		t.Errorf("unknown run: got %d, want 404", w.Code)
	}

	// Success → 204 and the run reports completed with its output.
	if w := post(`{"run_id":"run-1","output":"done","success":true}`); w.Code != http.StatusNoContent {
		t.Errorf("success: got %d, want 204", w.Code)
	}
	if obs, _ := a.Observe(context.Background(), "run-1"); obs.Status != RunCompleted || obs.Output != "done" {
		t.Errorf("run-1 not completed: %+v", obs)
	}

	// Failure → run reports failed with the output as the error.
	post(`{"run_id":"run-2","output":"boom","success":false}`)
	if obs, _ := a.Observe(context.Background(), "run-2"); obs.Status != RunFailed || obs.ErrorMessage != "boom" {
		t.Errorf("run-2 not failed: %+v", obs)
	}

	// Wrong method → 405; malformed body → 400.
	reqGet := httptest.NewRequest("GET", "/api/runs/complete", nil)
	wGet := httptest.NewRecorder()
	a.CompleteRunHandler(wGet, reqGet)
	if wGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: got %d, want 405", wGet.Code)
	}
	if w := post(`not json`); w.Code != http.StatusBadRequest {
		t.Errorf("bad body: got %d, want 400", w.Code)
	}
}

// Observe on a runID the adapter no longer tracks must report failure, not a
// fabricated empty completion: a GC/timing anomaly would otherwise be recorded
// as "the agent produced no output" and submitted for review (plan 4.3).
func TestInternalObserve_UnknownRunFails(t *testing.T) {
	a := &InternalLLMAdapter{}
	obs, err := a.Observe(context.Background(), "no-such-run")
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if obs.Status != RunFailed || obs.ErrorMessage == "" {
		t.Errorf("unknown run: got %+v, want RunFailed with error message", obs)
	}
}
