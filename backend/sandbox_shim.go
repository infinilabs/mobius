package main

// Transitional aliases (plan 6.4e): sandboxed execution lives in
// internal/sandbox.

import (
	"mobius/internal/sandbox"
)

const maxCommandOutput = sandbox.MaxCommandOutput

var (
	truncateOutput      = sandbox.TruncateOutput
	dockerRunArgs       = sandbox.DockerRunArgs
	runSandboxedCommand = sandbox.RunCommand
	runSandboxedArgv    = sandbox.RunArgv
	probeNsJail         = sandbox.Probe
)
