package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// ---- test doubles ----

type fakeAdapter struct{ typ AdapterType }

func (f fakeAdapter) Type() AdapterType { return f.typ }
func (f fakeAdapter) Start(context.Context, HeartbeatContext) (string, error) {
	return "run-1", nil
}
func (f fakeAdapter) Observe(context.Context, string) (RunObservation, error) {
	return RunObservation{}, nil
}
func (f fakeAdapter) Stop(context.Context, string) error { return nil }

type fakeProvider struct{ id string }

func (f *fakeProvider) ChatStream(context.Context, *LLMRequest) (string, error) {
	return f.id, nil
}

// ---- AdapterRegistry (used on every dispatch to resolve an agent's runtime) ----

func TestAdapterRegistry(t *testing.T) {
	r := NewAdapterRegistry()
	r.Register(AdapterBash, fakeAdapter{typ: AdapterBash})

	got, ok := r.Get(AdapterBash)
	if !ok {
		t.Fatal("registered adapter not found")
	}
	if got.Type() != AdapterBash {
		t.Errorf("got adapter type %q, want %q", got.Type(), AdapterBash)
	}
	if _, ok := r.Get(AdapterCursor); ok {
		t.Error("unregistered adapter type reported as present")
	}
}

// ResolveProvider routes a model id to its backend: claude- prefix → claude,
// everything else → gemini. A wrong route means an agent talks to the wrong LLM.
func TestProviderRegistry_ResolveProvider(t *testing.T) {
	r := NewProviderRegistry()
	r.Register("claude", &fakeProvider{id: "C"})
	r.Register("gemini", &fakeProvider{id: "G"})

	tests := []struct {
		modelID string
		want    string
	}{
		{"claude-opus-4", "C"},
		{"claude-3-5-sonnet", "C"},
		{"gemini-2.0-flash", "G"},
		{"gpt-4o", "G"}, // unknown prefix defaults to gemini
	}
	for _, tc := range tests {
		t.Run(tc.modelID, func(t *testing.T) {
			p := r.ResolveProvider(tc.modelID)
			fp, ok := p.(*fakeProvider)
			if !ok || fp.id != tc.want {
				t.Errorf("ResolveProvider(%q) routed to %v, want id %q", tc.modelID, p, tc.want)
			}
		})
	}
}

// ---- validateCommand: defense-in-depth denylist behind the sandbox (H3) ----

func TestValidateCommand(t *testing.T) {
	blocked := []string{
		"rm -rf /",
		"RM -RF /",            // case-insensitive
		"sudo dd if=/dev/zero", // contains "dd if="
		"mkfs.ext4 /dev/sda",
		"curl | sh",
		"echo hi > /dev/sdb",
	}
	for _, c := range blocked {
		if err := validateCommand(c); err == nil {
			t.Errorf("expected %q to be blocked", c)
		}
	}

	allowed := []string{
		"ls -la",
		"go test ./...",
		"npm run build && npm test",
		"rm -rf ./tmp/build", // not the root/home patterns
	}
	for _, c := range allowed {
		if err := validateCommand(c); err != nil {
			t.Errorf("expected %q to be allowed, got %v", c, err)
		}
	}
}

// ---- resolveWithinRoot: the M5 path-traversal / symlink-escape guard ----

func TestResolveWithinRoot(t *testing.T) {
	root := t.TempDir()

	t.Run("valid nested path", func(t *testing.T) {
		got, err := resolveWithinRoot(root, "sub/file.txt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != filepath.Join(root, "sub/file.txt") {
			t.Errorf("got %q", got)
		}
	})

	t.Run("parent traversal rejected", func(t *testing.T) {
		if _, err := resolveWithinRoot(root, "../escape"); err == nil {
			t.Error("expected error for ../escape")
		}
	})

	t.Run("absolute path rejected", func(t *testing.T) {
		if _, err := resolveWithinRoot(root, "/etc/passwd"); err == nil {
			t.Error("expected error for absolute path")
		}
	})

	t.Run("symlink escaping root rejected", func(t *testing.T) {
		outside := t.TempDir() // a sibling dir, not under root
		if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
			t.Skipf("cannot create symlink: %v", err)
		}
		if _, err := resolveWithinRoot(root, "link"); err == nil {
			t.Error("expected error: symlink resolves outside project root")
		}
	})
}

// ---- MCP argument helpers ----

func TestParseArgs(t *testing.T) {
	if m := parseArgs(nil); m == nil || len(m) != 0 {
		t.Errorf("parseArgs(nil) = %v, want empty non-nil map", m)
	}
	if m := parseArgs(json.RawMessage(`{"a":"b"}`)); m["a"] != "b" {
		t.Errorf("parseArgs did not decode object: %v", m)
	}
	// Malformed JSON must yield an empty map, never nil (handlers index into it).
	if m := parseArgs(json.RawMessage(`not json`)); m == nil {
		t.Error("parseArgs(invalid) returned nil map")
	}
}

func TestArgStr(t *testing.T) {
	args := map[string]any{"s": "hello", "n": float64(5)}
	if argStr(args, "s") != "hello" {
		t.Error("argStr did not return string value")
	}
	if argStr(args, "n") != "" {
		t.Error("argStr should return \"\" for non-string value")
	}
	if argStr(args, "missing") != "" {
		t.Error("argStr should return \"\" for missing key")
	}
}

// ---- truncateOutput (rune-safe cap on command output) ----

func TestTruncateOutput(t *testing.T) {
	if got := truncateOutput("short"); got != "short" {
		t.Errorf("short output altered: %q", got)
	}

	long := strings.Repeat("a", maxCommandOutput+1000)
	got := truncateOutput(long)
	if !strings.HasSuffix(got, "... (truncated)") {
		t.Error("over-limit output not marked truncated")
	}

	// Multibyte: must not cut a rune in half → output stays valid UTF-8.
	multibyte := strings.Repeat("é", maxCommandOutput) // 2 bytes each, well over cap
	if !utf8.ValidString(truncateOutput(multibyte)) {
		t.Error("truncateOutput produced invalid UTF-8 on a rune boundary")
	}
}

// ---- VertexAIConfig model resolution ----

func TestVertexAIConfig_GetModels_FromLegacyFields(t *testing.T) {
	v := &VertexAIConfig{
		LLMModelID: "gemini-llm", LLMLocation: "us-c1",
		ImgModelID: "imagen", ImgLocation: "us-c1",
	}
	models := v.GetModels()
	if len(models) != 2 {
		t.Fatalf("expected 2 synthesized models, got %d", len(models))
	}
	if models[0].Type != "llm" || models[1].Type != "image" {
		t.Errorf("unexpected model types: %+v", models)
	}
}

func TestVertexAIConfig_GetModels_PrefersExplicitList(t *testing.T) {
	v := &VertexAIConfig{
		LLMModelID: "ignored",
		Models:     []VertexModel{{ID: "a", ModelID: "a", Type: "llm"}},
	}
	if got := v.GetModels(); len(got) != 1 || got[0].ID != "a" {
		t.Errorf("explicit Models list not preferred: %+v", got)
	}
}

func TestVertexAIConfig_DefaultModel(t *testing.T) {
	v := &VertexAIConfig{Models: []VertexModel{
		{ModelID: "llm-1", Location: "l1", Type: "llm"},
		{ModelID: "llm-2", Location: "l2", Type: "llm", Default: true},
		{ModelID: "img-1", Location: "l3", Type: "image"},
	}}
	// The Default-flagged model wins over declaration order.
	if mid, loc := v.DefaultModel("llm"); mid != "llm-2" || loc != "l2" {
		t.Errorf("DefaultModel(llm) = %q/%q, want llm-2/l2", mid, loc)
	}
	// No default flag → first of that type.
	if mid, _ := v.DefaultModel("image"); mid != "img-1" {
		t.Errorf("DefaultModel(image) = %q, want img-1", mid)
	}
	// No model of that type → empty.
	if mid, _ := v.DefaultModel("video"); mid != "" {
		t.Errorf("DefaultModel(video) = %q, want empty", mid)
	}
}

func TestVertexAIConfig_DefaultLLM_FallsBackToLegacy(t *testing.T) {
	v := &VertexAIConfig{LLMModelID: "legacy-llm", LLMLocation: "loc"}
	if mid, loc := v.DefaultLLM(); mid != "legacy-llm" || loc != "loc" {
		t.Errorf("DefaultLLM fallback = %q/%q", mid, loc)
	}
}

func TestConfigMaxUploadBytes(t *testing.T) {
	c := &Config{}
	c.Upload.MaxFileSizeMB = 0
	if got := c.MaxUploadBytes(); got != 20<<20 {
		t.Errorf("zero config should default to 20MiB, got %d", got)
	}
	c.Upload.MaxFileSizeMB = 5
	if got := c.MaxUploadBytes(); got != 5<<20 {
		t.Errorf("MaxUploadBytes(5MB) = %d", got)
	}
}

// ---- parseTokenFilters: query-string → filter struct, with sane defaults ----

func TestParseTokenFilters_Explicit(t *testing.T) {
	r := httptest.NewRequest("GET",
		"/?since=2020-01-01T00:00:00Z&until=2020-02-01T00:00:00Z&model_id=m1&model_id=m2&source=mcp", nil)
	f := parseTokenFilters(r)
	if !f.Since.Equal(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("since not parsed: %v", f.Since)
	}
	if len(f.ModelIDs) != 2 || f.ModelIDs[0] != "m1" {
		t.Errorf("model_id multi-value not captured: %v", f.ModelIDs)
	}
	if len(f.Sources) != 1 || f.Sources[0] != "mcp" {
		t.Errorf("source not captured: %v", f.Sources)
	}
}

func TestParseTokenFilters_Defaults(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	f := parseTokenFilters(r)
	// Default window is the last 7 days.
	if f.Since.IsZero() || f.Until.IsZero() {
		t.Fatal("default time window not applied")
	}
	if f.Until.Sub(f.Since) < 6*24*time.Hour {
		t.Errorf("default window too narrow: %v..%v", f.Since, f.Until)
	}
}

// ---- skill_sync extraction helpers ----

func TestExtractHermesCategory(t *testing.T) {
	sep := string(filepath.Separator)
	tests := map[string]string{
		"coding" + sep + "rust.md": "coding",
		"rust.md":                  "general", // no directory component
		"":                         "general",
	}
	for rel, want := range tests {
		if got := extractHermesCategory(rel); got != want {
			t.Errorf("extractHermesCategory(%q) = %q, want %q", rel, got, want)
		}
	}
}

func TestExtractHermesTags(t *testing.T) {
	var hfm hermesFrontmatter
	hfm.Metadata.Hermes.Tags = []string{"Foo", "  Bar  "}

	tags := extractHermesTags(hfm, "hermes", "coding")
	// Expect deduped, lowercased, trimmed, sorted: source + category + tags.
	want := []string{"bar", "coding", "foo", "hermes"}
	if strings.Join(tags, ",") != strings.Join(want, ",") {
		t.Errorf("extractHermesTags = %v, want %v", tags, want)
	}

	// category "general" must not become a tag.
	got := extractHermesTags(hermesFrontmatter{}, "src", "general")
	for _, tag := range got {
		if tag == "general" {
			t.Error("\"general\" category should not be added as a tag")
		}
	}
}
