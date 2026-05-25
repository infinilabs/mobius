package main

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestTruncateSafeUTF8_EmojiBoundary(t *testing.T) {
	text := "Hello 😀 world"
	result := truncateSafeUTF8(text, 8)
	if result != "Hello " {
		t.Errorf("expected 'Hello ', got %q (len=%d)", result, len(result))
	}
}

func TestTruncateSafeUTF8_NoTruncation(t *testing.T) {
	result := truncateSafeUTF8("short", 100)
	if result != "short" {
		t.Errorf("expected unchanged string, got %q", result)
	}
}

func TestTruncateSafeUTF8_ExactBoundary(t *testing.T) {
	result := truncateSafeUTF8("abcd", 4)
	if result != "abcd" {
		t.Errorf("expected 'abcd', got %q", result)
	}
}

func TestTruncateSafeUTF8_MultiByteSequence(t *testing.T) {
	text := "café"
	result := truncateSafeUTF8(text, 4)
	if result != "caf" {
		t.Errorf("expected 'caf', got %q", result)
	}
}

func TestValidateProjectName_Template(t *testing.T) {
	valid := []string{"q3-campaign", "my_project", "abc", "a-b"}
	for _, name := range valid {
		if err := validateProjectName(name, false); err != nil {
			t.Errorf("expected valid template name %q, got error: %v", name, err)
		}
	}

	invalid := []string{"", "ab", "ABC", "has space", "has.dot", "-start", "end-"}
	for _, name := range invalid {
		if err := validateProjectName(name, false); err == nil {
			t.Errorf("expected invalid template name %q to be rejected", name)
		}
	}
}

func TestValidateProjectName_Import(t *testing.T) {
	valid := []string{"MyRepo", "my.project", "CamelCase-v2"}
	for _, name := range valid {
		if err := validateProjectName(name, true); err != nil {
			t.Errorf("expected valid import name %q, got error: %v", name, err)
		}
	}

	invalid := []string{"", "ab", "has space", "-start"}
	for _, name := range invalid {
		if err := validateProjectName(name, true); err == nil {
			t.Errorf("expected invalid import name %q to be rejected", name)
		}
	}
}

func TestValidateProjectPath(t *testing.T) {
	valid := []string{"reports/analysis.md", "code/main.go", "file.txt"}
	for _, p := range valid {
		if err := validateProjectPath(p); err != nil {
			t.Errorf("expected valid path %q, got error: %v", p, err)
		}
	}

	invalid := []string{"../escape", "/absolute/path", "some/../traversal"}
	for _, p := range invalid {
		if err := validateProjectPath(p); err == nil {
			t.Errorf("expected invalid path %q to be rejected", p)
		}
	}
}

func TestClassifyContentType(t *testing.T) {
	cases := map[string]string{
		"text/plain":              "text",
		"text/csv":                "text",
		"text/x-go":              "code",
		"application/json":       "code",
		"text/markdown":          "document",
		"text/html":              "document",
		"application/pdf":        "pdf",
		"image/png":              "image",
		"video/mp4":              "video",
		"audio/mpeg":             "audio",
		"application/octet-stream": "binary",
	}
	for mime, expected := range cases {
		got := classifyContentType(mime)
		if got != expected {
			t.Errorf("classifyContentType(%q) = %q, want %q", mime, got, expected)
		}
	}
}

func TestResolveMimeType(t *testing.T) {
	cases := []struct {
		filename, header, expected string
	}{
		{"main.go", "application/octet-stream", "text/x-go"},
		{"script.py", "", "text/x-py"},
		{"data.json", "application/octet-stream", "application/json"},
		{"notes.md", "", "text/markdown"},
		{"photo.jpg", "image/jpeg", "image/jpeg"},
		{"unknown.xyz", "", "application/octet-stream"},
	}
	for _, tc := range cases {
		got := resolveMimeType(tc.filename, tc.header)
		if got != tc.expected {
			t.Errorf("resolveMimeType(%q, %q) = %q, want %q", tc.filename, tc.header, got, tc.expected)
		}
	}
}

func TestCalculateSHA256(t *testing.T) {
	hash := calculateSHA256([]byte("hello"))
	if len(hash) != 64 {
		t.Errorf("expected 64-char hex hash, got len=%d", len(hash))
	}

	empty := calculateSHA256([]byte{})
	if empty != "" {
		t.Errorf("expected empty string for empty input, got %q", empty)
	}

	h1 := calculateSHA256([]byte("same"))
	h2 := calculateSHA256([]byte("same"))
	if h1 != h2 {
		t.Error("identical inputs should produce identical hashes")
	}

	h3 := calculateSHA256([]byte("different"))
	if h1 == h3 {
		t.Error("different inputs should produce different hashes")
	}
}

func TestAppendProjectMemory_ConcurrentPressure(t *testing.T) {
	dir := t.TempDir()
	project := &Project{Name: "concurrent-test"}
	cfg := &Config{}
	cfg.Projects.applyDefaults(dir)
	cfg.Projects.ProjectsDir = dir
	cfg.Projects.MemoryMaxSize = 100 * 1024

	mobiusPath := filepath.Join(dir, project.Name, "mobius.md")
	os.MkdirAll(filepath.Dir(mobiusPath), 0755)
	os.WriteFile(mobiusPath, []byte("# Test\n\n## Key Decisions\n"), 0644)

	var wg sync.WaitGroup
	writers := 10
	readsPerWriter := 5

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < readsPerWriter; j++ {
				appendProjectMemory(project, cfg, fmt.Sprintf("Fact from goroutine %d iteration %d.", n, j))
			}
		}(i)
	}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < readsPerWriter; j++ {
				readProjectMemory(project, cfg)
			}
		}()
	}
	wg.Wait()

	content, _ := os.ReadFile(mobiusPath)
	lines := strings.Split(string(content), "\n")
	factCount := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "- Fact from goroutine") {
			factCount++
		}
	}
	if factCount < writers {
		t.Errorf("expected at least %d unique facts, got %d", writers, factCount)
	}
}

func TestCompactMobiusMD_BackupUniqueness(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "backup-test")
	os.MkdirAll(projectDir, 0755)

	project := &Project{Name: "backup-test"}
	cfg := &Config{}
	cfg.Projects.applyDefaults(dir)
	cfg.Projects.ProjectsDir = dir
	cfg.Projects.MemoryMaxSize = 1024
	cfg.Projects.MemoryCompactRatio = 0.5
	cfg.Projects.MemoryCompactKeep = 5

	var content strings.Builder
	content.WriteString("# Test\n\n## Key Decisions\n\n")
	for i := 0; i < 50; i++ {
		content.WriteString(fmt.Sprintf("- Decision %d about something important\n", i))
	}
	original := content.String()

	compactMobiusMD(project, cfg, []byte(original))
	compactMobiusMD(project, cfg, []byte(original))

	bakDir := filepath.Join(projectDir, "mobius.md.bak")
	entries, err := os.ReadDir(bakDir)
	if err != nil {
		t.Fatalf("failed to read backup dir: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 distinct backup files, got %d", len(entries))
	}
	if len(entries) == 2 && entries[0].Name() == entries[1].Name() {
		t.Error("backup filenames collided")
	}
}

func TestExportProjectToZip_DotfileExclusions(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "zip-test")

	os.MkdirAll(filepath.Join(root, ".git", "objects"), 0755)
	os.WriteFile(filepath.Join(root, ".git", "config"), []byte("gitconfig"), 0644)
	os.MkdirAll(filepath.Join(root, ".conversations"), 0755)
	os.WriteFile(filepath.Join(root, ".conversations", "conv-01.json"), []byte(`{"id":"c1"}`), 0644)
	os.MkdirAll(filepath.Join(root, "code"), 0755)
	os.WriteFile(filepath.Join(root, "code", "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(root, "mobius.md"), []byte("# Test"), 0644)
	os.WriteFile(filepath.Join(root, ".env"), []byte("SECRET=hunter2"), 0644)

	zipPath := filepath.Join(dir, "export.zip")
	project := &Project{ID: "test-id", Name: "zip-test"}
	cfg := &Config{}
	cfg.Projects.applyDefaults(dir)
	cfg.Projects.ProjectsDir = dir

	h := &APIHandler{config: cfg}
	err := h.exportProjectToZip(t.Context(), project, zipPath)
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}

	r, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("failed to open zip: %v", err)
	}
	defer r.Close()

	names := make(map[string]bool)
	for _, f := range r.File {
		names[f.Name] = true
	}

	mustExist := []string{"project.json", "files/code/main.go", "files/.conversations/conv-01.json", "files/mobius.md"}
	for _, name := range mustExist {
		if !names[name] {
			t.Errorf("expected %q in zip, not found. Contents: %v", name, names)
		}
	}

	mustNotExist := []string{"files/.git/config", "files/.git/objects", "files/.env"}
	for _, name := range mustNotExist {
		if names[name] {
			t.Errorf("did NOT expect %q in zip, but found it", name)
		}
	}
}

func TestCompactMobiusMD_PreservesHeader(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "compact-hdr")
	os.MkdirAll(projectDir, 0755)

	project := &Project{Name: "compact-hdr"}
	cfg := &Config{}
	cfg.Projects.applyDefaults(dir)
	cfg.Projects.ProjectsDir = dir
	cfg.Projects.MemoryMaxSize = 2048
	cfg.Projects.MemoryCompactRatio = 0.8
	cfg.Projects.MemoryCompactKeep = 3

	var content strings.Builder
	content.WriteString("# My Project\n\n## Key Decisions\n\n")
	for i := 0; i < 30; i++ {
		content.WriteString(fmt.Sprintf("- Decision %d: important thing\n", i))
	}
	original := content.String()

	compactMobiusMD(project, cfg, []byte(original))

	mobiusPath := filepath.Join(projectDir, "mobius.md")
	result, err := os.ReadFile(mobiusPath)
	if err != nil {
		t.Fatalf("failed to read compacted file: %v", err)
	}

	resultStr := string(result)
	if !strings.HasPrefix(resultStr, "# My Project\n") {
		t.Error("header line should be preserved after compaction")
	}
	if !strings.Contains(resultStr, "## Key Decisions") {
		t.Error("section header should be preserved")
	}

	lines := strings.Split(resultStr, "\n")
	decisionCount := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "- Decision") {
			decisionCount++
		}
	}
	if decisionCount < int(cfg.Projects.MemoryCompactKeep) {
		t.Errorf("expected at least %d decisions kept, got %d", cfg.Projects.MemoryCompactKeep, decisionCount)
	}

	if len(result) > cfg.Projects.MemoryMaxSize {
		t.Errorf("compacted size %d exceeds max %d", len(result), cfg.Projects.MemoryMaxSize)
	}
}

func TestIsTextIndexable(t *testing.T) {
	indexable := []string{"text", "code", "document"}
	for _, ct := range indexable {
		if !isTextIndexable(ct) {
			t.Errorf("expected %q to be indexable", ct)
		}
	}

	notIndexable := []string{"image", "video", "audio", "binary", "pdf"}
	for _, ct := range notIndexable {
		if isTextIndexable(ct) {
			t.Errorf("expected %q to NOT be indexable", ct)
		}
	}
}

func TestProjectRootDir(t *testing.T) {
	cfg := &Config{}
	cfg.Projects.applyDefaults("/tmp/work")
	cfg.Projects.ProjectsDir = "/data/projects"

	native := &Project{Name: "my-app"}
	if got := native.RootDir(cfg); got != "/data/projects/my-app" {
		t.Errorf("native project rootDir = %q, want /data/projects/my-app", got)
	}

	src := "/home/user/repos/imported"
	imported := &Project{Name: "imported", SourcePath: &src}
	if got := imported.RootDir(cfg); got != src {
		t.Errorf("imported project rootDir = %q, want %q", got, src)
	}
}
