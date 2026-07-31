package main

import (
	"context"
	"strings"
	"testing"
)

func TestSandboxApplyDefaults_FillsZeroValues(t *testing.T) {
	var sb SandboxConfig
	sb.applyDefaults()
	if sb.Image == "" || sb.Network == "" || sb.CPUs == "" || sb.PidsLimit == 0 {
		t.Errorf("applyDefaults left a zero value: %+v", sb)
	}
	if sb.Provider != ProviderDocker {
		t.Errorf("expected default provider to be %q, got %q", ProviderDocker, sb.Provider)
	}
	if sb.MemoryMB != 0 {
		t.Errorf("expected default MemoryMB to be 0, got %d", sb.MemoryMB)
	}
}

func TestSandboxApplyDefaults_PreservesExplicit(t *testing.T) {
	sb := SandboxConfig{
		Provider:  ProviderNsJail,
		Image:     "custom:1",
		Network:   "none",
		MemoryMB:  512,
		CPUs:      "1",
		PidsLimit: 64,
	}
	sb.applyDefaults()
	if sb.Provider != ProviderNsJail || sb.Image != "custom:1" || sb.Network != "none" || sb.MemoryMB != 512 || sb.CPUs != "1" || sb.PidsLimit != 64 {
		t.Errorf("applyDefaults overrode explicit config: %+v", sb)
	}
}

// dockerRunArgs encodes the misfire-prevention guarantees (H3): the workdir is
// the ONLY bind mount (nothing else on the host is writable) and the resource
// caps (memory/cpus/pids) are always present. A regression that dropped any of
// these would let a runaway command escape its blast radius — this test fails
// loudly if that happens.
func TestSandboxDockerRunArgs_Confinement(t *testing.T) {
	sb := SandboxConfig{Image: "img:test", Network: "bridge", MemoryMB: 2048, CPUs: "2", PidsLimit: 512}
	args := sb.dockerRunArgs("/projects/p1", "c1", []string{"FOO=bar"})
	joined := strings.Join(args, " ")

	mustContain := []string{
		"run --rm",
		"--name c1",
		"--network bridge",
		"--memory 2048m",
		"--cpus 2",
		"--pids-limit 512",
		"-v /projects/p1:/work",
		"-w /work",
		"-e FOO=bar",
	}
	for _, frag := range mustContain {
		if !strings.Contains(joined, frag) {
			t.Errorf("dockerRunArgs missing %q\nfull: %s", frag, joined)
		}
	}

	// The image must be the final element (the command is appended after it).
	if args[len(args)-1] != "img:test" {
		t.Errorf("image must be the last arg, got %q", args[len(args)-1])
	}

	// Exactly one bind mount — the whole point is that nothing else is writable.
	if n := strings.Count(joined, "-v "); n != 1 {
		t.Errorf("expected exactly one bind mount, got %d\nargs: %s", n, joined)
	}
}

// A nameless run (the run_command / project-command path) must still be fully
// confined; only the --name flag is omitted.
func TestSandboxDockerRunArgs_NoName(t *testing.T) {
	sb := SandboxConfig{Image: "img:test", Network: "none", MemoryMB: 1024, CPUs: "1", PidsLimit: 256}
	args := sb.dockerRunArgs("/tmp/work", "", nil)
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--name") {
		t.Errorf("expected no --name flag for nameless run, got: %s", joined)
	}
	if !strings.Contains(joined, "--network none") || !strings.Contains(joined, "-v /tmp/work:/work") {
		t.Errorf("nameless run lost confinement flags: %s", joined)
	}
}

// Code execution is sandbox-only (plan 0.4/0.5): with no usable provider the
// exec helpers must return an error, never fall back to running on the host.
func TestRunSandboxed_RefusesHostExecution(t *testing.T) {
	sb := SandboxConfig{Provider: ProviderNone}

	if _, _, _, err := runSandboxedCommand(context.Background(), sb, t.TempDir(), "echo pwned", nil); err == nil {
		t.Error("runSandboxedCommand with provider none: expected error, command ran")
	}
	if _, _, _, err := runSandboxedArgv(context.Background(), sb, t.TempDir(), []string{"echo", "pwned"}, nil); err == nil {
		t.Error("runSandboxedArgv with provider none: expected error, command ran")
	}
}

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
