package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"
)

// nsjailArgs builds args up to the "--" separator. Pure function — unit testable
// without invoking nsjail. No cgroups, no systemd.
func nsjailArgs(sb SandboxConfig, workdir, jailTmp, jailRoot string, env []string, timeLimitSec int) []string {
	args := []string{
		"-Mo",                // MODE_STANDALONE_ONCE
		"--quiet",
		"--chroot", jailRoot, // fresh empty writable dir (NOT "/")
		"--cwd", "/work",
		"--bindmount", workdir + ":/work",
		"--bindmount", jailTmp + ":/tmp",
		"--user", "0",
		"--group", "0",
		"--disable_clone_newnet", // share host network (deps install)
		"--time_limit", strconv.Itoa(timeLimitSec),

		// rlimits raised so nsjail's brutal defaults don't break builds.
		"--rlimit_cpu", "max",
		"--rlimit_fsize", "max",
		"--rlimit_nofile", "max",
		"--rlimit_nproc", "max",
		"--rlimit_stack", "max",
	}

	// Optional best-effort memory ceiling (off by default; see §3.3).
	if sb.MemoryMB > 0 {
		args = append(args, "--rlimit_as", strconv.Itoa(sb.MemoryMB))
	} else {
		args = append(args, "--rlimit_as", "max")
	}

	// Read-only host toolchain dirs (skip absent symlinks on merged-/usr).
	for _, dir := range []string{"/usr", "/etc", "/sbin", "/bin", "/lib", "/lib64"} {
		if _, statErr := os.Stat(dir); statErr == nil {
			args = append(args, "--bindmount_ro", dir)
		}
	}
	// Device nodes (nsjail does NOT auto-create /dev).
	for _, dev := range []string{"/dev/null", "/dev/zero", "/dev/urandom", "/dev/random"} {
		args = append(args, "--bindmount", dev)
	}

	// Bind-mount host's bin/infinishield read-only to /usr/local/bin/infinishield
	if absPath, err := filepath.Abs("bin/infinishield"); err == nil {
		if _, statErr := os.Stat(absPath); statErr == nil {
			args = append(args, "--bindmount_ro", absPath+":/usr/local/bin/infinishield")
		}
	}

	// Caches -> the host-backed /tmp (Mode has no writable $HOME).
	args = append(args,
		"--env", "HOME=/tmp",
		"--env", "GOCACHE=/tmp/gocache",
		"--env", "GOPATH=/tmp/go",
		"--env", "CARGO_HOME=/tmp/cargo",
		"--env", "PIP_CACHE_DIR=/tmp/pip-cache",
		"--env", "npm_config_cache=/tmp/npm-cache",
	)
	for _, e := range env { // caller env wins (appended last).
		args = append(args, "--env", e)
	}
	return args
}

func ctxDeadlineSecs(ctx context.Context) int {
	if deadline, ok := ctx.Deadline(); ok {
		secs := int(time.Until(deadline).Seconds()) + 10 // 10s buffer
		if secs < 30 {
			return 30 // minimum sanity buffer
		}
		return secs
	}
	return 130 // fallback default
}

// runNsJailCommand runs `sh -c <command>` inside a host-shared nsjail jail.
// The Go context is the primary deadline; nsjail --time_limit is the backstop.
func runNsJailCommand(ctx context.Context, sb SandboxConfig, workdir, command string, env []string) (stdout, stderr string, exitCode int, err error) {
	nsjailPath := sb.NsJailPath
	if nsjailPath == "" {
		nsjailPath = "nsjail"
	}
	nsjailAbs, lookErr := exec.LookPath(nsjailPath)
	if lookErr != nil {
		return "", "", -1, fmt.Errorf("nsjail not found at %q: %w", nsjailPath, lookErr)
	}

	// Per-run writable /tmp (dodges nsjail's ~4 MB tmpfs default) and an empty
	// dir to chroot into. Both removed on return.
	jailTmp, err := os.MkdirTemp("", "mobius-jail-tmp-")
	if err != nil {
		return "", "", -1, fmt.Errorf("create jail tmp: %w", err)
	}
	defer os.RemoveAll(jailTmp)
	jailRoot, err := os.MkdirTemp("", "mobius-jail-root-")
	if err != nil {
		return "", "", -1, fmt.Errorf("create jail root: %w", err)
	}
	defer os.RemoveAll(jailRoot)

	args := nsjailArgs(sb, workdir, jailTmp, jailRoot, env, ctxDeadlineSecs(ctx))
	args = append(args, "--", "/bin/sh", "-c", command)

	cmd := exec.CommandContext(ctx, nsjailAbs, args...)
	// Own process group; SIGKILL the group on cancel so no helper (nsjail,
	// newuidmap, jailed children) is orphaned. Mirrors runHostCommand at
	// sandbox.go:105.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &outBuf, &errBuf

	runErr := cmd.Run()
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			return outBuf.String(), errBuf.String(), exitErr.ExitCode(), nil
		}
		return outBuf.String(), errBuf.String(), -1, fmt.Errorf("nsjail run failed: %w", runErr)
	}
	return outBuf.String(), errBuf.String(), 0, nil
}

// runNsJailArgv runs a command directly via argv inside a host-shared nsjail jail.
func runNsJailArgv(ctx context.Context, sb SandboxConfig, workdir string, argv []string, env []string) (stdout, stderr string, exitCode int, err error) {
	nsjailPath := sb.NsJailPath
	if nsjailPath == "" {
		nsjailPath = "nsjail"
	}
	nsjailAbs, lookErr := exec.LookPath(nsjailPath)
	if lookErr != nil {
		return "", "", -1, fmt.Errorf("nsjail not found at %q: %w", nsjailPath, lookErr)
	}

	// Per-run writable /tmp (dodges nsjail's ~4 MB tmpfs default) and an empty
	// dir to chroot into. Both removed on return.
	jailTmp, err := os.MkdirTemp("", "mobius-jail-tmp-")
	if err != nil {
		return "", "", -1, fmt.Errorf("create jail tmp: %w", err)
	}
	defer os.RemoveAll(jailTmp)
	jailRoot, err := os.MkdirTemp("", "mobius-jail-root-")
	if err != nil {
		return "", "", -1, fmt.Errorf("create jail root: %w", err)
	}
	defer os.RemoveAll(jailRoot)

	args := nsjailArgs(sb, workdir, jailTmp, jailRoot, env, ctxDeadlineSecs(ctx))
	args = append(args, "--")
	args = append(args, argv...)

	cmd := exec.CommandContext(ctx, nsjailAbs, args...)
	// Own process group; SIGKILL the group on cancel so no helper (nsjail,
	// newuidmap, jailed children) is orphaned.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &outBuf, &errBuf

	runErr := cmd.Run()
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			return outBuf.String(), errBuf.String(), exitErr.ExitCode(), nil
		}
		return outBuf.String(), errBuf.String(), -1, fmt.Errorf("nsjail run failed: %w", runErr)
	}
	return outBuf.String(), errBuf.String(), 0, nil
}

// probeNsJail runs once at boot. Proves nsjail can start a jail on this host.
// If not, sets nsjailUsable to false and falls back to Docker.
func probeNsJail(sb SandboxConfig) {
	if sb.Provider != ProviderNsJail || runtime.GOOS != "linux" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	work, err := os.MkdirTemp("", "nsjail-probe")
	if err != nil {
		slog.Warn("nsjail probe: failed to create temp dir", "err", err)
		return
	}
	defer os.RemoveAll(work)

	out, stderr, code, err := runNsJailCommand(ctx, sb, work, "echo ok", nil)
	if err != nil || code != 0 || out != "ok\n" {
		slog.Warn("nsjail probe failed; falling back to Docker",
			"err", err, "code", code, "stderr", truncateStr(stderr, 500))
		return
	}
	nsjailUsable.Store(true)
	slog.Info("nsjail sandbox active (simple mode)")
}
