package memory

import (
	"mobius/internal/config"
	"mobius/internal/domain"
	"testing"
)

func TestResolveModelID_SettingsDefault(t *testing.T) {
	cfg := &config.Config{}
	cfg.GoogleCloud.VertexAI.Models = []config.VertexModel{
		{ID: "m1", ModelID: "gemini-2.5-flash", Location: "us-central1", Type: "llm", Default: true},
	}
	modelID := ResolveModelID(cfg, nil)
	if modelID != "gemini-2.5-flash" {
		t.Errorf("expected settings default gemini-2.5-flash, got %s", modelID)
	}
}

func TestResolveModelID_EmployeeOverride(t *testing.T) {
	cfg := &config.Config{}
	cfg.GoogleCloud.VertexAI.Models = []config.VertexModel{
		{ID: "m1", ModelID: "gemini-2.5-flash", Location: "us-central1", Type: "llm", Default: true},
	}
	emp := &domain.Employee{
		Models: []domain.EmployeeModel{{ModelID: "claude-sonnet-4", Purpose: "primary_llm"}},
	}
	modelID := ResolveModelID(cfg, emp)
	if modelID != "claude-sonnet-4" {
		t.Errorf("expected employee override claude-sonnet-4, got %s", modelID)
	}
}

func TestResolveModelID_FallbackToHardcoded(t *testing.T) {
	cfg := &config.Config{}
	modelID := ResolveModelID(cfg, nil)
	if modelID != defaultFallbackModel {
		t.Errorf("expected fallback %s, got %s", defaultFallbackModel, modelID)
	}
}
