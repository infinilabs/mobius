//go:build linux

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIntegration_NsJail(t *testing.T) {
	// 1. Probe first
	sb := SandboxConfig{
		Enabled:    true,
		Provider:   ProviderNsJail,
		NsJailPath: "../bin/nsjail", // relative to backend/
	}
	probeNsJail(sb)

	if !nsjailUsable.Load() {
		t.Skip("nsjail not usable on this host, skipping integration test")
	}

	tmp, err := os.MkdirTemp("", "mobius-nsjail-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmp)

	// Create a dummy project structure
	workdir := filepath.Join(tmp, "project")
	err = os.Mkdir(workdir, 0755)
	if err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	t.Run("Echo Hello", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		stdout, stderr, code, err := runNsJailCommand(ctx, sb, workdir, "echo 'hello world'", nil)
		if err != nil {
			t.Fatalf("run failed: %v", err)
		}
		if code != 0 {
			t.Errorf("expected exit code 0, got %d. stderr: %s", code, stderr)
		}
		if strings.TrimSpace(stdout) != "hello world" {
			t.Errorf("expected 'hello world', got %q", stdout)
		}
	})

	t.Run("Confinement - Write to workdir allowed", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, stderr, code, err := runNsJailCommand(ctx, sb, workdir, "echo 'data' > /work/test.txt", nil)
		if err != nil {
			t.Fatalf("run failed: %v", err)
		}
		if code != 0 {
			t.Errorf("expected exit code 0, got %d. stderr: %s", code, stderr)
		}

		// Verify file exists on host
		hostFile := filepath.Join(workdir, "test.txt")
		data, err := os.ReadFile(hostFile)
		if err != nil {
			t.Fatalf("failed to read file on host: %v", err)
		}
		if string(data) != "data\n" {
			t.Errorf("expected 'data\\n', got %q", string(data))
		}
	})

	t.Run("Confinement - Write to system dir blocked", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, stderr, code, err := runNsJailCommand(ctx, sb, workdir, "echo 'data' > /etc/test.txt", nil)
		if err != nil {
			t.Fatalf("run failed: %v", err)
		}
		if code == 0 {
			t.Errorf("expected exit code non-zero (should fail to write to /etc), got %d. stderr: %s", code, stderr)
		}
	})

	t.Run("Confinement - Host home hidden", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		currentUser := os.Getenv("USER")
		if currentUser != "" {
			_, _, code, _ := runNsJailCommand(ctx, sb, workdir, "ls /home/"+currentUser, nil)
			if code == 0 {
				t.Errorf("expected access to /home/%s to fail, but succeeded", currentUser)
			}
		}
	})

	t.Run("Large Write", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// Write 20MB of data
		_, stderr, code, err := runNsJailCommand(ctx, sb, workdir, "dd if=/dev/zero of=/work/large.bin bs=1M count=20", nil)
		if err != nil {
			t.Fatalf("run failed: %v", err)
		}
		if code != 0 {
			t.Errorf("expected exit code 0, got %d. stderr: %s", code, stderr)
		}
		// Verify file size on host
		hostFile := filepath.Join(workdir, "large.bin")
		fi, err := os.Stat(hostFile)
		if err != nil {
			t.Fatalf("failed to stat host file: %v", err)
		}
		if fi.Size() != 20*1024*1024 {
			t.Errorf("expected file size 20MB, got %d", fi.Size())
		}
	})

	t.Run("Timeout kills process", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		start := time.Now()
		_, _, code, err := runNsJailCommand(ctx, sb, workdir, "sleep 10", nil)
		duration := time.Since(start)

		if err != nil {
			t.Errorf("expected err to be nil (ExitError handled), got %v", err)
		}
		if code == 0 {
			t.Errorf("expected non-zero exit code for killed process, got 0")
		}
		if duration > 4*time.Second {
			t.Errorf("expected timeout to kill process in ~2s, took %v", duration)
		}
	})
}

func TestIntegration_DockerFallback(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found, skipping fallback test")
	}

	// Preserve global state and restore on cleanup
	oldUsable := nsjailUsable.Load()
	t.Cleanup(func() {
		nsjailUsable.Store(oldUsable)
	})

	// 1. Force probe failure by using bad path
	sb := SandboxConfig{
		Enabled:    true,
		Provider:   ProviderNsJail,
		NsJailPath: "/nonexistent/nsjail",
		Image:      "alpine", // Use local image
	}
	sb.applyDefaults()

	nsjailUsable.Store(false)
	probeNsJail(sb)

	if nsjailUsable.Load() {
		t.Fatalf("expected nsjail to be unusable, but it was marked usable")
	}

	// 2. Run command, should fallback to Docker
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tmp, _ := os.MkdirTemp("", "mobius-fallback-test-")
	defer os.RemoveAll(tmp)

	stdout, stderr, code, err := runSandboxedCommand(ctx, sb, tmp, "echo 'fallback works'", nil)
	if err != nil {
		t.Fatalf("fallback execution failed: %v. stderr: %s", err, stderr)
	}
	if code != 0 {
		t.Errorf("expected exit code 0, got %d. stderr: %s", code, stderr)
	}
	if strings.TrimSpace(stdout) != "fallback works" {
		t.Errorf("expected 'fallback works', got %q", stdout)
	}
}
