package main

import (
	"sync"
	"testing"
)

func TestDefaultLLM_FromModelsArray(t *testing.T) {
	v := VertexAIConfig{
		Models: []VertexModel{
			{ID: "m1", ModelID: "gemini-2.5-flash", Location: "us-central1", Type: "llm"},
			{ID: "m2", ModelID: "gemini-3.1-pro", Location: "us-east4", Type: "llm", Default: true},
		},
	}
	modelID, loc := v.DefaultLLM()
	if modelID != "gemini-3.1-pro" {
		t.Errorf("expected gemini-3.1-pro, got %s", modelID)
	}
	if loc != "us-east4" {
		t.Errorf("expected us-east4, got %s", loc)
	}
}

func TestDefaultLLM_FallbackToFirst(t *testing.T) {
	v := VertexAIConfig{
		Models: []VertexModel{
			{ID: "m1", ModelID: "gemini-2.5-flash", Location: "us-central1", Type: "llm"},
			{ID: "m2", ModelID: "imagen-4.0", Location: "us-central1", Type: "image"},
		},
	}
	modelID, _ := v.DefaultLLM()
	if modelID != "gemini-2.5-flash" {
		t.Errorf("expected first LLM model gemini-2.5-flash, got %s", modelID)
	}
}

func TestDefaultLLM_FallbackToLegacy(t *testing.T) {
	v := VertexAIConfig{
		LLMModelID:  "legacy-model",
		LLMLocation: "europe-west1",
	}
	modelID, loc := v.DefaultLLM()
	if modelID != "legacy-model" {
		t.Errorf("expected legacy-model, got %s", modelID)
	}
	if loc != "europe-west1" {
		t.Errorf("expected europe-west1, got %s", loc)
	}
}

func TestDefaultLLM_Empty(t *testing.T) {
	v := VertexAIConfig{}
	modelID, loc := v.DefaultLLM()
	if modelID != "" {
		t.Errorf("expected empty model ID, got %s", modelID)
	}
	if loc != "" {
		t.Errorf("expected empty location, got %s", loc)
	}
}

func TestDefaultModel_FiltersByType(t *testing.T) {
	v := VertexAIConfig{
		Models: []VertexModel{
			{ID: "m1", ModelID: "gemini-2.5-flash", Type: "llm"},
			{ID: "m2", ModelID: "imagen-4.0", Type: "image", Default: true},
			{ID: "m3", ModelID: "veo-3", Type: "video"},
		},
	}
	imgModel, _ := v.DefaultModel("image")
	if imgModel != "imagen-4.0" {
		t.Errorf("expected imagen-4.0 for image type, got %s", imgModel)
	}

	vidModel, _ := v.DefaultModel("video")
	if vidModel != "veo-3" {
		t.Errorf("expected veo-3 for video type, got %s", vidModel)
	}

	missing, _ := v.DefaultModel("audio")
	if missing != "" {
		t.Errorf("expected empty for missing type, got %s", missing)
	}
}

func TestGetModels_LegacyFields(t *testing.T) {
	v := VertexAIConfig{
		LLMModelID:   "gemini-pro",
		LLMLocation:  "us-central1",
		ImgModelID:   "imagen-3",
		ImgLocation:  "us-east4",
		VideoModelID: "veo-2",
		VideoLocation: "us-central1",
	}
	models := v.GetModels()
	if len(models) != 3 {
		t.Fatalf("expected 3 models from legacy fields, got %d", len(models))
	}

	types := map[string]bool{}
	for _, m := range models {
		types[m.Type] = true
	}
	for _, expected := range []string{"llm", "image", "video"} {
		if !types[expected] {
			t.Errorf("missing model type %s", expected)
		}
	}
}

func TestGetModels_ModelsArray(t *testing.T) {
	v := VertexAIConfig{
		LLMModelID: "should-be-ignored",
		Models: []VertexModel{
			{ID: "m1", ModelID: "gemini-2.5-flash", Type: "llm"},
		},
	}
	models := v.GetModels()
	if len(models) != 1 {
		t.Fatalf("expected 1 model from array, got %d", len(models))
	}
	if models[0].ModelID != "gemini-2.5-flash" {
		t.Errorf("expected gemini-2.5-flash, got %s", models[0].ModelID)
	}
}

func TestGetSettings_ThreadSafe(t *testing.T) {
	cfg := &Config{}
	cfg.GoogleCloud.ProjectID = "initial"

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			cfg.ApplySettings(SettingsData{
				GoogleCloud: GoogleCloudConfig{ProjectID: "updated"},
			})
		}()
		go func() {
			defer wg.Done()
			s := cfg.GetSettings()
			_ = s.GoogleCloud.ProjectID
		}()
	}
	wg.Wait()
}

func TestResolveModelID_SettingsDefault(t *testing.T) {
	cfg := &Config{}
	cfg.GoogleCloud.VertexAI.Models = []VertexModel{
		{ID: "m1", ModelID: "gemini-2.5-flash", Location: "us-central1", Type: "llm", Default: true},
	}
	modelID := resolveModelID(cfg, nil)
	if modelID != "gemini-2.5-flash" {
		t.Errorf("expected settings default gemini-2.5-flash, got %s", modelID)
	}
}

func TestResolveModelID_EmployeeOverride(t *testing.T) {
	cfg := &Config{}
	cfg.GoogleCloud.VertexAI.Models = []VertexModel{
		{ID: "m1", ModelID: "gemini-2.5-flash", Location: "us-central1", Type: "llm", Default: true},
	}
	emp := &Employee{
		Models: []EmployeeModel{{ModelID: "claude-sonnet-4", Purpose: "primary_llm"}},
	}
	modelID := resolveModelID(cfg, emp)
	if modelID != "claude-sonnet-4" {
		t.Errorf("expected employee override claude-sonnet-4, got %s", modelID)
	}
}

func TestResolveModelID_FallbackToHardcoded(t *testing.T) {
	cfg := &Config{}
	modelID := resolveModelID(cfg, nil)
	if modelID != defaultFallbackModel {
		t.Errorf("expected fallback %s, got %s", defaultFallbackModel, modelID)
	}
}

func TestApplyDefaults_SetsAllFields(t *testing.T) {
	pc := ProjectConfig{}
	pc.applyDefaults("/tmp/test")

	if pc.ProjectsDir == "" {
		t.Error("ProjectsDir should have a default")
	}
	if pc.MemoryMaxSize == 0 {
		t.Error("MemoryMaxSize should have a default")
	}
	if pc.MemoryCompactRatio == 0 {
		t.Error("MemoryCompactRatio should have a default")
	}
	if pc.MemoryCompactKeep == 0 {
		t.Error("MemoryCompactKeep should have a default")
	}
	if pc.MemoryInjectLimit == 0 {
		t.Error("MemoryInjectLimit should have a default")
	}
	if pc.MemoryDedupPrefix == 0 {
		t.Error("MemoryDedupPrefix should have a default")
	}
	if pc.ContentMaxIndex == 0 {
		t.Error("ContentMaxIndex should have a default")
	}
	if pc.ContentSummaryMax == 0 {
		t.Error("ContentSummaryMax should have a default")
	}
	if pc.GCSMaxRetries == 0 {
		t.Error("GCSMaxRetries should have a default")
	}
	if pc.ESShards == 0 {
		t.Error("ESShards should have a default")
	}
}

func TestApplyDefaults_PreservesExplicitValues(t *testing.T) {
	pc := ProjectConfig{
		MemoryMaxSize: 999,
		ESShards:      5,
	}
	pc.applyDefaults("/tmp/test")

	if pc.MemoryMaxSize != 999 {
		t.Errorf("should preserve explicit MemoryMaxSize 999, got %d", pc.MemoryMaxSize)
	}
	if pc.ESShards != 5 {
		t.Errorf("should preserve explicit ESShards 5, got %d", pc.ESShards)
	}
}

func TestMaxUploadBytes_Default(t *testing.T) {
	cfg := &Config{}
	got := cfg.MaxUploadBytes()
	if got != 20*1024*1024 {
		t.Errorf("expected 20MB default, got %d", got)
	}
}

func TestMaxUploadBytes_Explicit(t *testing.T) {
	cfg := &Config{}
	cfg.Upload.MaxFileSizeMB = 50
	got := cfg.MaxUploadBytes()
	if got != 50*1024*1024 {
		t.Errorf("expected 50MB, got %d", got)
	}
}

func TestGetSettings_EffectiveProvider(t *testing.T) {
	oldUsable := nsjailUsable.Load()
	defer func() { nsjailUsable.Store(oldUsable) }()

	tests := []struct {
		name     string
		enabled  bool
		provider SandboxProvider
		usable   bool
		expected string
	}{
		{"Disabled", false, ProviderNsJail, true, "none (disabled)"},
		{"Disabled None", false, ProviderNone, false, "none (disabled)"},
		{"Docker Enabled", true, ProviderDocker, false, "docker"},
		{"NsJail Usable", true, ProviderNsJail, true, "nsjail"},
		{"NsJail Unusable Fallback", true, ProviderNsJail, false, "docker (nsjail unavailable)"},
		{"None Enabled", true, ProviderNone, false, "none"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			nsjailUsable.Store(tc.usable)
			cfg := &Config{}
			cfg.Sandbox.Enabled = tc.enabled
			cfg.Sandbox.Provider = tc.provider

			settings := cfg.GetSettings()
			if settings.EffectiveProvider != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, settings.EffectiveProvider)
			}
		})
	}
}
