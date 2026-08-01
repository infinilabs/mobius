package config

import "testing"

func TestSandboxApplyDefaults_FillsZeroValues(t *testing.T) {
	var sb SandboxConfig
	sb.ApplyDefaults()
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
	sb.ApplyDefaults()
	if sb.Provider != ProviderNsJail || sb.Image != "custom:1" || sb.Network != "none" || sb.MemoryMB != 512 || sb.CPUs != "1" || sb.PidsLimit != 64 {
		t.Errorf("applyDefaults overrode explicit config: %+v", sb)
	}
}
