package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

type ClaudeCodeAdapter struct {
	config        *Config
	mcpServerAddr string
	// mintToken issues a signed MCP session token for (agentID, taskID). When set
	// (and mcpServerAddr is non-empty) the spawned claude CLI is given an MCP
	// config so it can call mobius tools back over the WebSocket endpoint. nil
	// disables MCP wiring (the CLI runs with no mobius tools).
	mintToken func(agentID, taskID string) string
	runs      sync.Map
}

type cliRun struct {
	cmd    *exec.Cmd
	stdout bytes.Buffer
	stderr bytes.Buffer
	status RunStatus
	errMsg string
	mu     sync.Mutex
	done   chan struct{}
}

func NewClaudeCodeAdapter(cfg *Config, mcpAddr string, mintToken func(agentID, taskID string) string) *ClaudeCodeAdapter {
	return &ClaudeCodeAdapter{config: cfg, mcpServerAddr: mcpAddr, mintToken: mintToken}
}

// writeMCPConfig writes a temp Claude Code MCP config pointing the spawned CLI at
// mobius's WebSocket MCP endpoint, authenticated with a freshly minted session
// token in the Authorization header. Returns "" (no config) when MCP wiring is
// not available. The caller is responsible for removing the returned file.
func (a *ClaudeCodeAdapter) writeMCPConfig(hb HeartbeatContext) string {
	if a.mcpServerAddr == "" || a.mintToken == nil {
		return ""
	}
	token := a.mintToken(hb.AgentID, hb.TaskID)
	cfg := map[string]any{
		"mcpServers": map[string]any{
			"mobius": map[string]any{
				"type":    "ws",
				"url":     a.mcpServerAddr,
				"headers": map[string]string{"Authorization": "Bearer " + token},
			},
		},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		slog.Warn("claude code: failed to marshal MCP config", "error", err)
		return ""
	}
	f, err := os.CreateTemp("", "mobius-mcp-*.json")
	if err != nil {
		slog.Warn("claude code: failed to create MCP config file", "error", err)
		return ""
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(f.Name())
		slog.Warn("claude code: failed to write MCP config", "error", err)
		return ""
	}
	f.Close()
	return f.Name()
}

func (a *ClaudeCodeAdapter) Type() AdapterType { return AdapterClaudeCode }

func (a *ClaudeCodeAdapter) Start(ctx context.Context, hb HeartbeatContext) (string, error) {
	// The claude CLI runs with permissions bypassed, so it is gated behind the
	// same explicit sandbox opt-in as the other code-exec adapters. Note the CLI
	// process itself still executes on the host (it needs its own credentials);
	// containerizing it is tracked separately in the remediation plan.
	if a.config == nil || !a.config.Sandbox.Enabled {
		return "", fmt.Errorf("claude_code adapter requires the sandbox: enable sandbox in config (host execution is not permitted)")
	}

	runID := generateID()

	prompt := fmt.Sprintf(
		"You are %s. Complete this task:\n\n## %s\n\n%s",
		hb.AgentName, hb.TaskTitle, hb.TaskBody,
	)

	args := []string{"--print", "--dangerously-skip-permissions"}
	mcpConfigPath := a.writeMCPConfig(hb)
	if mcpConfigPath != "" {
		// --strict-mcp-config makes the CLI use ONLY our config, ignoring any
		// user/project scoped servers on the host running mobius.
		args = append(args, "--mcp-config", mcpConfigPath, "--strict-mcp-config")
	}
	args = append(args, "-p", prompt)

	cmd := exec.CommandContext(ctx, "claude", args...)
	if hb.ProjectDir != "" {
		cmd.Dir = hb.ProjectDir
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	run := &cliRun{cmd: cmd, status: RunActive, done: make(chan struct{})}
	cmd.Stdout = &run.stdout
	cmd.Stderr = &run.stderr
	a.runs.Store(runID, run)

	if err := cmd.Start(); err != nil {
		a.runs.Delete(runID)
		if mcpConfigPath != "" {
			os.Remove(mcpConfigPath)
		}
		return "", fmt.Errorf("failed to start claude: %w", err)
	}

	go func() {
		defer close(run.done)
		// Retain the finished run briefly so a polling Observe can read the final
		// output before the entry is reclaimed.
		defer time.AfterFunc(terminalRunRetention, func() { a.runs.Delete(runID) })
		// The minted token in the config is single-run; drop the file once the
		// process (which has already read it at startup) exits.
		if mcpConfigPath != "" {
			defer os.Remove(mcpConfigPath)
		}

		err := cmd.Wait()
		run.mu.Lock()
		defer run.mu.Unlock()
		if err != nil {
			run.status = RunFailed
			run.errMsg = err.Error()
			// Surface captured stderr so a non-zero exit isn't just an opaque
			// "exit status 1" with no diagnostic context.
			if se := run.stderr.String(); se != "" {
				run.errMsg += ": " + truncateStr(se, 2000)
			}
		} else {
			run.status = RunCompleted
		}
	}()

	slog.Info("claude code adapter started", "run_id", runID, "task_id", hb.TaskID)
	return runID, nil
}

func (a *ClaudeCodeAdapter) Observe(_ context.Context, runID string) (RunObservation, error) {
	val, ok := a.runs.Load(runID)
	if !ok {
		return RunObservation{Status: RunCompleted}, nil
	}
	run := val.(*cliRun)
	run.mu.Lock()
	defer run.mu.Unlock()
	return RunObservation{
		Status:       run.status,
		Output:       run.stdout.String(),
		ErrorMessage: run.errMsg,
	}, nil
}

func (a *ClaudeCodeAdapter) Stop(_ context.Context, runID string) error {
	val, ok := a.runs.Load(runID)
	if !ok {
		return nil
	}
	run := val.(*cliRun)
	if run.cmd.Process != nil {
		// Setpgid:true makes the child its own process-group leader, so its PID
		// is the PGID. Signal the whole group, then escalate to SIGKILL only if
		// the process has not been reaped within the grace period — racing the
		// timer against run.done ensures we never SIGKILL a recycled PGID.
		pid := run.cmd.Process.Pid
		syscall.Kill(-pid, syscall.SIGTERM)
		go func() {
			select {
			case <-run.done:
			case <-time.After(5 * time.Second):
				syscall.Kill(-pid, syscall.SIGKILL)
			}
		}()
	}
	run.mu.Lock()
	run.status = RunCancelled
	run.mu.Unlock()
	return nil
}
