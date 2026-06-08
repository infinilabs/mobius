package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"sync/atomic"
	"syscall"
)

// SandboxConfig controls how host-executing commands (run_command, the bash
// adapter) are confined. The threat model is misfire prevention, not a hostile
// attacker: running inside an ephemeral container whose only writable mount is
// the project (or an ephemeral temp dir) means a runaway `rm -rf /` cannot reach
// anything on the host outside that directory.
type SandboxProvider string

const (
	ProviderDocker SandboxProvider = "docker"
	ProviderNsJail SandboxProvider = "nsjail"
	ProviderNone   SandboxProvider = "none"
)

type SandboxConfig struct {
	Enabled    bool            `yaml:"enabled"     json:"enabled"`
	Provider   SandboxProvider `yaml:"provider"    json:"provider"`
	NsJailPath string          `yaml:"nsjail_path" json:"nsjail_path"`
	Image      string          `yaml:"image"       json:"image"`
	Network    string          `yaml:"network"     json:"network"`     // docker fallback
	MemoryMB   int             `yaml:"memory_mb"   json:"memory_mb"`   // nsjail: rlimit_as (0=off); docker: --memory
	CPUs       string          `yaml:"cpus"        json:"cpus"`        // docker fallback
	PidsLimit  int             `yaml:"pids_limit"  json:"pids_limit"`  // docker fallback
}

func (sb *SandboxConfig) applyDefaults() {
	if sb.Provider == "" {
		sb.Provider = ProviderDocker
	}
	if sb.Image == "" {
		sb.Image = "mobius-agent:latest"
	}
	if sb.Network == "" {
		sb.Network = "bridge"
	}
	if sb.CPUs == "" {
		sb.CPUs = "2"
	}
	if sb.PidsLimit == 0 {
		sb.PidsLimit = 512
	}
	// MemoryMB intentionally defaults to 0 (no rlimit_as cap).
}

// dockerRunArgs builds the `docker run ...` argument list (up to and including
// the image, not the command). workdir is bind-mounted read-write at /work and
// is the container's working directory; nothing else on the host is reachable.
func (sb SandboxConfig) dockerRunArgs(workdir, name string, env []string) []string {
	args := []string{"run", "--rm"}
	if name != "" {
		args = append(args, "--name", name)
	}
	mem := sb.MemoryMB
	if mem == 0 {
		mem = 2048 // Default memory for Docker fallback
	}
	args = append(args,
		"--network", sb.Network,
		"--memory", fmt.Sprintf("%dm", mem),
		"--cpus", sb.CPUs,
		"--pids-limit", fmt.Sprintf("%d", sb.PidsLimit),
		"-v", workdir+":/work",
		"-w", "/work",
	)
	for _, e := range env {
		args = append(args, "-e", e)
	}
	args = append(args, sb.Image)
	return args
}

// nsjailUsable is set once by the startup probe. When nsjail is
// configured but unusable on this host, the dispatcher transparently uses Docker.
var nsjailUsable atomic.Bool

// runSandboxedCommand runs `sh -c <command>` inside a sandbox (either NsJail or Docker)
// depending on configuration and host capabilities.
func runSandboxedCommand(ctx context.Context, sb SandboxConfig, workdir, command string, env []string) (stdout, stderr string, exitCode int, err error) {
	provider := sb.Provider

	if provider == ProviderNsJail {
		if runtime.GOOS != "linux" {
			slog.Warn("nsjail unsupported off Linux; using Docker", "os", runtime.GOOS)
			provider = ProviderDocker
		} else if !nsjailUsable.Load() {
			slog.Warn("nsjail probe failed on this host; using Docker")
			provider = ProviderDocker
		}
	}

	switch provider {
	case ProviderNsJail:
		return runNsJailCommand(ctx, sb, workdir, command, env)
	case ProviderDocker:
		return runDockerCommand(ctx, sb, workdir, command, env)
	default:
		return runHostCommand(ctx, workdir, command, env)
	}
}

// runDockerCommand runs `sh -c <command>` inside an ephemeral container.
func runDockerCommand(ctx context.Context, sb SandboxConfig, workdir, command string, env []string) (stdout, stderr string, exitCode int, err error) {
	if _, lookErr := exec.LookPath("docker"); lookErr != nil {
		return "", "", -1, fmt.Errorf("sandbox enabled but docker not found: install Docker and run `make build-sandbox`")
	}

	args := sb.dockerRunArgs(workdir, "", env)
	args = append(args, "sh", "-c", command)

	cmd := exec.CommandContext(ctx, "docker", args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	runErr := cmd.Run()
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			return outBuf.String(), errBuf.String(), exitErr.ExitCode(), nil
		}
		return outBuf.String(), errBuf.String(), -1, fmt.Errorf("sandbox execution failed: %w", runErr)
	}
	return outBuf.String(), errBuf.String(), 0, nil
}

// runHostCommand is the legacy unsandboxed path: `sh -c <command>` directly on
// the host, used only when the sandbox is disabled in config.
func runHostCommand(ctx context.Context, workdir, command string, env []string) (stdout, stderr string, exitCode int, err error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(), "PATH=/usr/local/bin:/usr/bin:/bin")
	cmd.Env = append(cmd.Env, env...)

	// Run sh in its own process group and, on ctx timeout/cancel, kill the whole
	// group — otherwise CommandContext only signals sh and leaves its children
	// (e.g. a spawned `npm`/`go build`) orphaned and still running.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	runErr := cmd.Run()
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			return outBuf.String(), errBuf.String(), exitErr.ExitCode(), nil
		}
		return outBuf.String(), errBuf.String(), -1, fmt.Errorf("command execution failed: %w", runErr)
	}
	return outBuf.String(), errBuf.String(), 0, nil
}

// truncateOutput caps command output to maxCommandOutput bytes (rune-safe).
func truncateOutput(s string) string {
	if len(s) > maxCommandOutput {
		return truncateStr(s, maxCommandOutput) + "\n... (truncated)"
	}
	return s
}
