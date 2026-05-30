package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

type BashAdapter struct {
	config *Config
	runs   sync.Map
}

type bashRun struct {
	cmd           *exec.Cmd
	stdout        bytes.Buffer
	stderr        bytes.Buffer
	status        RunStatus
	errMsg        string
	mu            sync.Mutex
	done          chan struct{}
	containerName string // non-empty when running inside a sandbox container
	tempDir       string // ephemeral host dir to clean up (single tasks, no project)
}

func NewBashAdapter(cfg *Config) *BashAdapter {
	return &BashAdapter{config: cfg}
}

func (a *BashAdapter) Type() AdapterType { return AdapterBash }

func (a *BashAdapter) Start(ctx context.Context, hb HeartbeatContext) (string, error) {
	command := hb.ModelID
	if command == "" {
		return "", fmt.Errorf("bash adapter requires a command in ModelID")
	}

	runID := generateID()

	// The writable area is the project dir, or an ephemeral host temp dir for a
	// single task with no project — nothing else is reachable when sandboxed.
	workdir := hb.ProjectDir
	var tempDir string
	if workdir == "" {
		td, err := os.MkdirTemp("", "mobius-bash-*")
		if err != nil {
			return "", fmt.Errorf("bash adapter: create temp dir: %w", err)
		}
		tempDir, workdir = td, td
	}

	env := []string{
		"MOBIUS_TASK_ID=" + hb.TaskID,
		"MOBIUS_TASK_TITLE=" + hb.TaskTitle,
		"MOBIUS_AGENT_ID=" + hb.AgentID,
		"MOBIUS_AGENT_NAME=" + hb.AgentName,
	}
	for k, v := range hb.Env {
		env = append(env, k+"="+v)
	}

	run := &bashRun{status: RunActive, done: make(chan struct{}), tempDir: tempDir}

	if a.config != nil && a.config.Sandbox.Enabled {
		if _, lookErr := exec.LookPath("docker"); lookErr != nil {
			if tempDir != "" {
				os.RemoveAll(tempDir)
			}
			return "", fmt.Errorf("sandbox enabled but docker not found: install Docker and run `make build-sandbox`")
		}
		run.containerName = "mobius-bash-" + runID
		args := a.config.Sandbox.dockerRunArgs(workdir, run.containerName, env)
		args = append(args, "sh", "-c", command)
		run.cmd = exec.CommandContext(ctx, "docker", args...)
	} else {
		run.cmd = exec.CommandContext(ctx, "sh", "-c", command)
		run.cmd.Dir = workdir
		run.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		run.cmd.Env = append(os.Environ(), env...)
	}
	run.cmd.Stdout = &run.stdout
	run.cmd.Stderr = &run.stderr

	a.runs.Store(runID, run)

	if err := run.cmd.Start(); err != nil {
		a.runs.Delete(runID)
		if tempDir != "" {
			os.RemoveAll(tempDir)
		}
		return "", fmt.Errorf("bash start failed: %w", err)
	}

	go func() {
		// Retain the finished run briefly so a polling Observe can read the final
		// output before the entry is reclaimed; the map entry is owned here.
		defer time.AfterFunc(terminalRunRetention, func() { a.runs.Delete(runID) })
		defer close(run.done)
		if run.tempDir != "" {
			defer os.RemoveAll(run.tempDir)
		}
		err := run.cmd.Wait()
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

	slog.Info("bash adapter started", "run_id", runID, "command", command,
		"task_id", hb.TaskID, "sandboxed", run.containerName != "")
	return runID, nil
}

func (a *BashAdapter) Observe(_ context.Context, runID string) (RunObservation, error) {
	val, ok := a.runs.Load(runID)
	if !ok {
		return RunObservation{Status: RunCompleted}, nil
	}
	run := val.(*bashRun)
	run.mu.Lock()
	defer run.mu.Unlock()
	return RunObservation{
		Status:       run.status,
		Output:       run.stdout.String(),
		ErrorMessage: run.errMsg,
	}, nil
}

func (a *BashAdapter) Stop(_ context.Context, runID string) error {
	val, ok := a.runs.Load(runID)
	if !ok {
		return nil
	}
	run := val.(*bashRun)
	if run.containerName != "" {
		// Sandboxed: stop the container directly. Killing the local `docker run`
		// process does not reliably terminate the container.
		exec.Command("docker", "kill", run.containerName).Run()
	} else if run.cmd.Process != nil {
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
