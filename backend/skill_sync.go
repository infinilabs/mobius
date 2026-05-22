package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type SkillSyncSource interface {
	Name() string
	Sync(ctx context.Context, targetDir string) (added, updated int, err error)
}

type SourceResult struct {
	Name    string `json:"name"`
	Added   int    `json:"added"`
	Updated int    `json:"updated"`
	Error   string `json:"error,omitempty"`
}

type SyncResult struct {
	Sources  []SourceResult `json:"sources"`
	DiskSync struct {
		Added   int `json:"added"`
		Updated int `json:"updated"`
	} `json:"disk_sync"`
	SyncedAt time.Time `json:"synced_at"`
}

// HermesSource syncs skills from a hermes-agent repository.
type HermesSource struct {
	BasePath        string
	ManualOverrides map[string]bool
}

var defaultManualOverrides = map[string]bool{
	"systematic-debugging":        true,
	"test-driven-development":     true,
	"writing-plans":               true,
	"requesting-code-review":      true,
	"kanban-orchestrator":         true,
	"subagent-driven-development": true,
	"codebase-inspection":         true,
	"spike":                       true,
}

func NewHermesSource(basePath string) *HermesSource {
	if basePath == "" {
		basePath = "../hermes-agent"
	}
	return &HermesSource{
		BasePath:        basePath,
		ManualOverrides: defaultManualOverrides,
	}
}

func (h *HermesSource) Name() string { return "hermes" }

type hermesFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Version     string `yaml:"version"`
	Metadata    struct {
		Hermes struct {
			Tags []string `yaml:"tags"`
		} `yaml:"hermes"`
	} `yaml:"metadata"`
}

func (h *HermesSource) Sync(ctx context.Context, targetDir string) (added, updated int, err error) {
	if _, err := os.Stat(h.BasePath); os.IsNotExist(err) {
		return 0, 0, fmt.Errorf("hermes-agent not found at %s", h.BasePath)
	}

	sources := []struct {
		dir    string
		source string
	}{
		{filepath.Join(h.BasePath, "skills"), "hermes"},
		{filepath.Join(h.BasePath, "optional-skills"), "hermes-optional"},
		{filepath.Join(h.BasePath, "plugins"), "hermes-plugin"},
	}

	for _, src := range sources {
		if _, err := os.Stat(src.dir); os.IsNotExist(err) {
			continue
		}

		walkErr := filepath.WalkDir(src.dir, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil || d.IsDir() || d.Name() != "SKILL.md" {
				return nil
			}

			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}

			a, u, processErr := h.processHermesSkill(data, path, src.dir, src.source, targetDir)
			if processErr != nil {
				slog.Warn("failed to process hermes skill", "path", path, "error", processErr)
				return nil
			}
			added += a
			updated += u
			return nil
		})
		if walkErr != nil {
			slog.Error("failed to walk hermes dir", "dir", src.dir, "error", walkErr)
		}
	}

	return added, updated, nil
}

func (h *HermesSource) processHermesSkill(data []byte, path, sourceRoot, sourceTag, targetDir string) (added, updated int, err error) {
	content := string(data)
	if !strings.HasPrefix(content, "---\n") {
		return 0, 0, nil
	}
	end := strings.Index(content[4:], "\n---\n")
	if end < 0 {
		return 0, 0, nil
	}
	fmRaw := content[4 : 4+end]
	body := strings.TrimSpace(content[4+end+5:])

	var hfm hermesFrontmatter
	if err := yaml.Unmarshal([]byte(fmRaw), &hfm); err != nil {
		return 0, 0, fmt.Errorf("parse hermes frontmatter: %w", err)
	}

	if hfm.Name == "" {
		return 0, 0, nil
	}

	if h.ManualOverrides[hfm.Name] {
		return 0, 0, nil
	}

	rel, _ := filepath.Rel(sourceRoot, filepath.Dir(path))
	category := extractHermesCategory(rel)
	tags := extractHermesTags(hfm, sourceTag, category)

	version := hfm.Version
	if version == "" {
		version = "1.0.0"
	}

	newContent := buildMobiusSkillMD(hfm.Name, hfm.Description, version, category, tags, body)

	outDir := filepath.Join(targetDir, category, hfm.Name)
	outFile := filepath.Join(outDir, "SKILL.md")

	if existingData, err := os.ReadFile(outFile); err == nil {
		if fileContentHash(existingData) == fileContentHash([]byte(newContent)) {
			return 0, 0, nil
		}
		if err := os.MkdirAll(outDir, 0755); err != nil {
			return 0, 0, err
		}
		if err := os.WriteFile(outFile, []byte(newContent), 0644); err != nil {
			return 0, 0, err
		}
		return 0, 1, nil
	}

	if err := os.MkdirAll(outDir, 0755); err != nil {
		return 0, 0, err
	}
	if err := os.WriteFile(outFile, []byte(newContent), 0644); err != nil {
		return 0, 0, err
	}
	return 1, 0, nil
}

func extractHermesCategory(rel string) string {
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) <= 1 {
		return "general"
	}
	return parts[0]
}

func extractHermesTags(hfm hermesFrontmatter, source, category string) []string {
	tagSet := make(map[string]bool)
	tagSet[source] = true
	if category != "general" {
		tagSet[category] = true
	}
	for _, t := range hfm.Metadata.Hermes.Tags {
		tagSet[strings.ToLower(strings.TrimSpace(t))] = true
	}
	tags := make([]string, 0, len(tagSet))
	for t := range tagSet {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	return tags
}

func buildMobiusSkillMD(name, description, version, category string, tags []string, body string) string {
	descEscaped := strings.ReplaceAll(description, `"`, `\"`)
	tagsStr := strings.Join(tags, ", ")

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "---\n")
	fmt.Fprintf(&buf, "name: %s\n", name)
	fmt.Fprintf(&buf, "description: \"%s\"\n", descEscaped)
	fmt.Fprintf(&buf, "version: %s\n", version)
	fmt.Fprintf(&buf, "category: %s\n", category)
	fmt.Fprintf(&buf, "tags: [%s]\n", tagsStr)
	fmt.Fprintf(&buf, "---\n\n")
	buf.WriteString(body)
	buf.WriteString("\n")
	return buf.String()
}

// runFullSync executes upstream source sync followed by disk→ES sync.
func (h *APIHandler) runFullSync(ctx context.Context) *SyncResult {
	result := &SyncResult{
		SyncedAt: time.Now(),
	}

	for _, src := range h.syncSources {
		sr := SourceResult{Name: src.Name()}
		a, u, err := src.Sync(ctx, h.skillsDir)
		sr.Added = a
		sr.Updated = u
		if err != nil {
			sr.Error = err.Error()
			slog.Error("upstream sync failed", "source", src.Name(), "error", err)
		} else if a+u > 0 {
			slog.Info("upstream sync complete", "source", src.Name(), "added", a, "updated", u)
		}
		result.Sources = append(result.Sources, sr)
	}

	if h.esClient != nil {
		a, u, err := syncSkillsFromDisk(ctx, h.esClient, h.skillsDir)
		if err != nil {
			slog.Error("disk→ES sync failed after upstream", "error", err)
		}
		result.DiskSync.Added = a
		result.DiskSync.Updated = u
	}

	h.lastSyncMu.Lock()
	h.lastSyncTime = result.SyncedAt
	h.lastSyncResult = result
	h.lastSyncMu.Unlock()

	return result
}

// HTTP handlers

func (h *APIHandler) SyncSkills(w http.ResponseWriter, r *http.Request) {
	result := h.runFullSync(r.Context())
	writeJSON(w, result)
}

func (h *APIHandler) GetSyncStatus(w http.ResponseWriter, r *http.Request) {
	h.lastSyncMu.RLock()
	defer h.lastSyncMu.RUnlock()

	resp := map[string]any{
		"last_synced_at": h.lastSyncTime,
		"last_result":    h.lastSyncResult,
	}
	writeJSON(w, resp)
}
