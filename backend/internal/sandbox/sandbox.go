// Package sandbox executes untrusted commands in nsjail or Docker
// confinement (plan 6.4e; policy from plan 0.4: no host execution).
package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"mobius/internal/config"
	"mobius/internal/domain"
	"os/exec"
	"runtime"
)

// config.SandboxConfig controls how host-executing commands (run_command, the bash
// adapter) are confined. The threat model is misfire prevention, not a hostile
// attacker: running inside an ephemeral container whose only writable mount is
// the project (or an ephemeral temp dir) means a runaway `rm -rf /` cannot reach
// anything on the host outside that directory.

// DockerRunArgs builds the `docker run ...` argument list (up to and including
// the image, not the command). workdir is bind-mounted read-write at /work and
// is the container's working directory; nothing else on the host is reachable.
func DockerRunArgs(sb config.SandboxConfig, workdir, name string, env []string) []string {
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

// RunCommand runs `sh -c <command>` inside a sandbox (either NsJail or Docker)
// depending on configuration and host capabilities.
func RunCommand(ctx context.Context, sb config.SandboxConfig, workdir, command string, env []string) (stdout, stderr string, exitCode int, err error) {
	provider := sb.Provider

	if provider == config.ProviderNsJail {
		if runtime.GOOS != "linux" {
			slog.Warn("nsjail unsupported off Linux; using Docker", "os", runtime.GOOS)
			provider = config.ProviderDocker
		} else if !config.NsJailUsable.Load() {
			slog.Warn("nsjail probe failed on this host; using Docker")
			provider = config.ProviderDocker
		}
	}

	switch provider {
	case config.ProviderNsJail:
		return runNsJailCommand(ctx, sb, workdir, command, env)
	case config.ProviderDocker:
		return runDockerCommand(ctx, sb, workdir, command, env)
	default:
		return "", "", -1, fmt.Errorf("sandbox provider %q cannot execute commands: configure provider \"docker\" or \"nsjail\" (host execution is not permitted)", provider)
	}
}

// runDockerCommand runs `sh -c <command>` inside an ephemeral container.
func runDockerCommand(ctx context.Context, sb config.SandboxConfig, workdir, command string, env []string) (stdout, stderr string, exitCode int, err error) {
	if _, lookErr := exec.LookPath("docker"); lookErr != nil {
		return "", "", -1, fmt.Errorf("sandbox enabled but docker not found: install Docker and run `make build-sandbox`")
	}

	args := DockerRunArgs(sb, workdir, "", env)
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

// RunArgv runs a command directly via argv inside a sandbox
func RunArgv(ctx context.Context, sb config.SandboxConfig, workdir string, argv []string, env []string) (stdout, stderr string, exitCode int, err error) {
	provider := sb.Provider

	if provider == config.ProviderNsJail {
		if runtime.GOOS != "linux" {
			slog.Warn("nsjail unsupported off Linux; using Docker", "os", runtime.GOOS)
			provider = config.ProviderDocker
		} else if !config.NsJailUsable.Load() {
			slog.Warn("nsjail probe failed on this host; using Docker")
			provider = config.ProviderDocker
		}
	}

	switch provider {
	case config.ProviderNsJail:
		return runNsJailArgv(ctx, sb, workdir, argv, env)
	case config.ProviderDocker:
		return runDockerArgv(ctx, sb, workdir, argv, env)
	default:
		return "", "", -1, fmt.Errorf("sandbox provider %q cannot execute commands: configure provider \"docker\" or \"nsjail\" (host execution is not permitted)", provider)
	}
}

func runDockerArgv(ctx context.Context, sb config.SandboxConfig, workdir string, argv []string, env []string) (stdout, stderr string, exitCode int, err error) {
	if _, lookErr := exec.LookPath("docker"); lookErr != nil {
		return "", "", -1, fmt.Errorf("sandbox enabled but docker not found: install Docker and run `make build-sandbox`")
	}

	args := DockerRunArgs(sb, workdir, "", env)
	args = append(args, argv...)

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

// MaxCommandOutput caps captured command output returned to agents.
const MaxCommandOutput = 4000

// TruncateOutput caps command output to MaxCommandOutput bytes (rune-safe).
func TruncateOutput(s string) string {
	if len(s) > MaxCommandOutput {
		return domain.TruncateStr(s, MaxCommandOutput) + "\n... (truncated)"
	}
	return s
}
