// Package skills syncs SKILL.md documents between disk and the search
// index (plan 6.4f prep).
package skills

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"mobius/internal/domain"
	"mobius/internal/search"
	"mobius/internal/storage/postgres"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type skillFrontmatter struct {
	ID          string   `yaml:"id,omitempty"`
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Version     string   `yaml:"version"`
	Category    string   `yaml:"category"`
	Tags        []string `yaml:"tags"`
}

func ParseSkillMD(data []byte) (*skillFrontmatter, string, error) {
	content := string(data)
	if !strings.HasPrefix(content, "---\n") {
		return nil, content, nil
	}
	end := strings.Index(content[4:], "\n---\n")
	if end < 0 {
		return nil, content, nil
	}
	fmRaw := content[4 : 4+end]
	body := strings.TrimSpace(content[4+end+5:])

	var fm skillFrontmatter
	if err := yaml.Unmarshal([]byte(fmRaw), &fm); err != nil {
		return nil, body, fmt.Errorf("parse frontmatter: %w", err)
	}
	return &fm, body, nil
}

func FileContentHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:8])
}

func WriteToDisk(baseDir string, s *domain.Skill) error {
	cat := s.Category
	if cat == "" {
		cat = "general"
	}
	dir := filepath.Join(baseDir, cat, s.Name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	fm := skillFrontmatter{
		ID:          s.ID,
		Name:        s.Name,
		Description: s.Description,
		Version:     s.Version,
		Category:    cat,
		Tags:        s.Tags,
	}
	fmBytes, err := yaml.Marshal(&fm)
	if err != nil {
		return fmt.Errorf("marshal frontmatter: %w", err)
	}

	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(fmBytes)
	buf.WriteString("---\n\n")
	buf.WriteString(s.Content)
	buf.WriteString("\n")

	target := filepath.Join(dir, "SKILL.md")
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, target); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

func RemoveFromDisk(baseDir string, s *domain.Skill) error {
	cat := s.Category
	if cat == "" {
		cat = "general"
	}
	dir := filepath.Join(baseDir, cat, s.Name)
	target := filepath.Join(dir, "SKILL.md")

	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return err
	}
	// clean empty parent dirs
	os.Remove(dir)
	os.Remove(filepath.Join(baseDir, cat))
	return nil
}

// ES operations

// Disk → ES sync

func SyncFromDisk(ctx context.Context, esClient *search.Client, pgClient *postgres.Client, dir string) (added, updated int, err error) {
	existing, err := esClient.SearchSkills(ctx, "")
	if err != nil {
		return 0, 0, fmt.Errorf("load existing skills: %w", err)
	}
	existingIDs := make(map[string]bool, len(existing))
	esMap := make(map[string]*domain.Skill, len(existing))
	for i := range existing {
		esMap[existing[i].ID] = &existing[i]
		existingIDs[existing[i].ID] = true
	}

	// Collect disk skills into esMap, marking new/changed
	changed := make(map[string]bool)
	newSkills := make(map[string]bool)

	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || d.Name() != "SKILL.md" {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}

		fm, body, parseErr := ParseSkillMD(data)
		if parseErr != nil {
			slog.Error("failed to parse skill", "path", path, "error", parseErr)
			return nil
		}

		name := filepath.Base(filepath.Dir(path))
		if fm != nil && fm.Name != "" {
			name = fm.Name
		}

		id := domain.SkillIDFromName(name)
		if fm != nil && fm.ID != "" {
			id = fm.ID
		}

		hash := FileContentHash(data)

		if prev, ok := esMap[id]; ok {
			if prev.ContentHash == hash {
				return nil
			}
			prev.Content = body
			prev.ContentHash = hash
			prev.UpdatedAt = time.Now().UnixMilli()
			if fm != nil {
				if fm.Name != "" {
					prev.Name = fm.Name
				}
				if fm.Description != "" {
					prev.Description = fm.Description
				}
				if fm.Category != "" {
					prev.Category = fm.Category
				}
				if fm.Version != "" {
					prev.Version = fm.Version
				}
				if len(fm.Tags) > 0 {
					prev.Tags = fm.Tags
				}
			}
			changed[id] = true
			return nil
		}

		now := time.Now().UnixMilli()
		s := &domain.Skill{
			ID:          id,
			Name:        name,
			Content:     body,
			ContentHash: hash,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if fm != nil {
			if fm.Description != "" {
				s.Description = fm.Description
			}
			if fm.Category != "" {
				s.Category = fm.Category
			}
			if fm.Version != "" {
				s.Version = fm.Version
			}
			if len(fm.Tags) > 0 {
				s.Tags = fm.Tags
			}
		}
		if s.Tags == nil {
			s.Tags = []string{}
		}
		if s.Category == "" {
			s.Category = "general"
		}

		esMap[id] = s
		newSkills[id] = true
		return nil
	})
	if err != nil {
		return 0, 0, fmt.Errorf("walk skills dir: %w", err)
	}

	for id := range changed {
		s := esMap[id]
		if indexErr := esClient.IndexSkill(ctx, s); indexErr != nil {
			slog.Error("failed to update skill", "name", s.Name, "error", indexErr)
		} else {
			updated++
			slog.Info("skill updated from disk", "id", id, "name", s.Name)
		}
	}

	for id := range newSkills {
		s := esMap[id]
		if indexErr := esClient.IndexSkill(ctx, s); indexErr != nil {
			slog.Error("failed to index new skill", "name", s.Name, "error", indexErr)
		} else {
			added++
			slog.Info("skill synced from disk", "id", id, "name", s.Name)
		}
	}

	for _, orig := range existing {
		if _, stillOnDisk := esMap[orig.ID]; stillOnDisk {
			continue
		}
		if delErr := esClient.DeleteSkill(ctx, orig.ID); delErr != nil {
			slog.Warn("failed to delete orphan skill from ES", "id", orig.ID, "name", orig.Name, "error", delErr)
			continue
		}
		if pgClient != nil {
			pgClient.Pool().Exec(ctx, "DELETE FROM skill_assignments WHERE skill_id=$1", orig.ID)
		}
		slog.Info("orphan skill removed", "id", orig.ID, "name", orig.Name)
	}

	return added, updated, nil
}

// HTTP handlers
