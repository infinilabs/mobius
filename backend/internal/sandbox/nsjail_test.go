package sandbox

import (
	"mobius/internal/config"
	"os"
	"strings"
	"testing"
)

func TestNsJailArgs_Basic(t *testing.T) {
	sb := config.SandboxConfig{
		NsJailPath: "./bin/nsjail",
		MemoryMB:   0, // no limit
	}
	workdir := "/projects/myproj"
	jailTmp := "/tmp/jail-tmp-123"
	jailRoot := "/tmp/jail-root-123"
	env := []string{"MOBIUS_ENV=test"}
	timeLimit := 130

	args := nsjailArgs(sb, workdir, jailTmp, jailRoot, env, timeLimit)

	// Confinement and directories
	assertContainsArg(t, args, "--chroot", jailRoot)
	assertContainsArg(t, args, "--cwd", "/work")
	assertContainsArg(t, args, "--bindmount", workdir+":/work")
	assertContainsArg(t, args, "--bindmount", jailTmp+":/tmp")

	// Devices
	for _, dev := range []string{"/dev/null", "/dev/zero", "/dev/urandom", "/dev/random"} {
		assertContainsArg(t, args, "--bindmount", dev)
	}

	// Read-only host toolchain dirs (should match host existence)
	for _, dir := range []string{"/usr", "/etc", "/sbin", "/bin", "/lib", "/lib64"} {
		if _, statErr := os.Stat(dir); statErr == nil {
			assertContainsArg(t, args, "--bindmount_ro", dir)
		}
	}

	// Network
	assertContainsFlag(t, args, "--disable_clone_newnet")

	// Timeout
	assertContainsArg(t, args, "--time_limit", "130")

	// Rlimits (raised)
	assertContainsArg(t, args, "--rlimit_cpu", "max")
	assertContainsArg(t, args, "--rlimit_fsize", "max")
	assertContainsArg(t, args, "--rlimit_nofile", "max")
	assertContainsArg(t, args, "--rlimit_nproc", "max")
	assertContainsArg(t, args, "--rlimit_stack", "max")
	assertContainsArg(t, args, "--rlimit_as", "max")

	// Env
	assertContainsArg(t, args, "--env", "MOBIUS_ENV=test")
	assertContainsArg(t, args, "--env", "HOME=/tmp")

	// No cgroups or mount-proc
	assertNotContainsFlagPrefix(t, args, "--cgroup")
	assertNotContainsFlagPrefix(t, args, "--mount")
}

func TestNsJailArgs_MemoryLimit(t *testing.T) {
	sb := config.SandboxConfig{
		MemoryMB: 512,
	}
	args := nsjailArgs(sb, "/work", "/tmp/t", "/tmp/r", nil, 100)

	assertContainsArg(t, args, "--rlimit_as", "512")
}

// Helpers for strict argument checking

func assertContainsArg(t *testing.T, args []string, flag, value string) {
	t.Helper()
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag && args[i+1] == value {
			return
		}
	}
	t.Errorf("expected arg pair %q %q not found in %v", flag, value, args)
}

func assertContainsFlag(t *testing.T, args []string, flag string) {
	t.Helper()
	for _, arg := range args {
		if arg == flag {
			return
		}
	}
	t.Errorf("expected flag %q not found in %v", flag, args)
}

func assertNotContainsFlagPrefix(t *testing.T, args []string, prefix string) {
	t.Helper()
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			t.Errorf("unexpected flag prefix %q found in %q (args: %v)", prefix, arg, args)
		}
	}
}
