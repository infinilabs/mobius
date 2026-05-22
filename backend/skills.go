package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const IdxSkills = "mobius_skills"

type Skill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Content     string   `json:"content"`
	Tags        []string `json:"tags"`
	Version     string   `json:"version"`
	CreatedAt   int64    `json:"created_at"`
	UpdatedAt   int64    `json:"updated_at"`
}

type skillFrontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Version     string   `yaml:"version"`
	Category    string   `yaml:"category"`
	Tags        []string `yaml:"tags"`
}

func parseSkillMD(data []byte) (*skillFrontmatter, string, error) {
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

func skillIDFromName(name string) string {
	hash := sha256.Sum256([]byte(name))
	return hex.EncodeToString(hash[:8])
}

// ES operations

func (es *ESClient) IndexSkill(ctx context.Context, s *Skill) error {
	body, _ := json.Marshal(s)
	res, err := es.client.Index(IdxSkills, bytes.NewReader(body),
		es.client.Index.WithContext(ctx),
		es.client.Index.WithDocumentID(s.ID),
	)
	if err != nil {
		return fmt.Errorf("ES index skill failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("ES index skill error: %s", res.String())
	}
	return nil
}

func (es *ESClient) GetSkill(ctx context.Context, id string) (*Skill, error) {
	res, err := es.client.Get(IdxSkills, id,
		es.client.Get.WithContext(ctx),
	)
	if err != nil {
		return nil, fmt.Errorf("ES get skill failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return nil, fmt.Errorf("ES get skill error: %s", res.Status())
	}

	var result struct {
		Source Skill `json:"_source"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ES decode skill failed: %w", err)
	}
	return &result.Source, nil
}

func (es *ESClient) SearchSkills(ctx context.Context, query string) ([]Skill, error) {
	var body map[string]any
	if query == "" {
		body = map[string]any{
			"query": map[string]any{"match_all": map[string]any{}},
			"sort":  []any{map[string]any{"name.keyword": map[string]any{"order": "asc", "unmapped_type": "keyword"}}},
			"size":  200,
		}
	} else {
		words := strings.Fields(query)
		wildcards := make([]string, len(words))
		for i, w := range words {
			wildcards[i] = "*" + w + "*"
		}
		qs := strings.Join(wildcards, " AND ")

		body = map[string]any{
			"query": map[string]any{
				"query_string": map[string]any{
					"query":            qs,
					"fields":           []string{"name^3", "description^2", "content", "tags", "category"},
					"default_operator": "AND",
					"analyze_wildcard": true,
				},
			},
			"size": 200,
		}
	}

	buf, _ := json.Marshal(body)
	res, err := es.client.Search(
		es.client.Search.WithContext(ctx),
		es.client.Search.WithIndex(IdxSkills),
		es.client.Search.WithBody(bytes.NewReader(buf)),
	)
	if err != nil {
		return nil, fmt.Errorf("ES search skills failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return nil, fmt.Errorf("ES search skills error: %s", res.String())
	}

	var result struct {
		Hits struct {
			Hits []struct {
				Source Skill `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ES decode skill search failed: %w", err)
	}

	skills := make([]Skill, 0, len(result.Hits.Hits))
	for _, hit := range result.Hits.Hits {
		skills = append(skills, hit.Source)
	}
	return skills, nil
}

func (es *ESClient) DeleteSkill(ctx context.Context, id string) error {
	res, err := es.client.Delete(IdxSkills, id,
		es.client.Delete.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("ES delete skill failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("ES delete skill error: %s", res.String())
	}
	return nil
}

// Seed function

func seedSkills(ctx context.Context, es *ESClient, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read skills dir: %w", err)
	}

	seeded := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillFile := filepath.Join(dir, entry.Name(), "SKILL.md")
		data, err := os.ReadFile(skillFile)
		if err != nil {
			continue
		}

		fm, body, err := parseSkillMD(data)
		if err != nil {
			slog.Error("failed to parse skill", "dir", entry.Name(), "error", err)
			continue
		}

		name := entry.Name()
		if fm != nil && fm.Name != "" {
			name = fm.Name
		}
		id := skillIDFromName(name)

		_, err = es.GetSkill(ctx, id)
		if err == nil {
			continue
		}

		now := time.Now().UnixMilli()
		s := &Skill{
			ID:        id,
			Name:      name,
			Content:   body,
			CreatedAt: now,
			UpdatedAt: now,
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

		if err := es.IndexSkill(ctx, s); err != nil {
			slog.Error("failed to seed skill", "name", name, "error", err)
			continue
		}
		seeded++
		slog.Info("skill seeded", "id", id, "name", name)
	}

	if seeded > 0 {
		slog.Info("skill seeding complete", "seeded", seeded)
	}
	return nil
}

// PG operations for skill-employee assignments

func (pg *PGClient) AssignSkill(ctx context.Context, employeeID, skillID string) error {
	_, err := pg.pool.Exec(ctx,
		"INSERT INTO skill_assignments (employee_id, skill_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
		employeeID, skillID)
	if err != nil {
		return fmt.Errorf("assign skill: %w", err)
	}
	return nil
}

func (pg *PGClient) UnassignSkill(ctx context.Context, employeeID, skillID string) error {
	_, err := pg.pool.Exec(ctx,
		"DELETE FROM skill_assignments WHERE employee_id=$1 AND skill_id=$2",
		employeeID, skillID)
	if err != nil {
		return fmt.Errorf("unassign skill: %w", err)
	}
	return nil
}

func (pg *PGClient) ListEmployeeSkillIDs(ctx context.Context, employeeID string) ([]string, error) {
	rows, err := pg.pool.Query(ctx,
		"SELECT skill_id FROM skill_assignments WHERE employee_id=$1 ORDER BY assigned_at",
		employeeID)
	if err != nil {
		return nil, fmt.Errorf("list employee skills: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan skill id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (pg *PGClient) SeedDefaultSkillAssignments(ctx context.Context, es *ESClient) error {
	if es == nil {
		return nil
	}

	defaults := map[string][]string{
		"Elong": {"task-decomposition"},
		"Steve": {"writing-plans", "code-review"},
		"Linas": {"test-driven-development", "systematic-debugging", "codebase-inspection", "spike"},
		"Allen": {"code-review", "systematic-debugging", "test-driven-development"},
	}

	for empName, skillNames := range defaults {
		var empID string
		err := pg.pool.QueryRow(ctx, "SELECT id FROM employees WHERE name=$1", empName).Scan(&empID)
		if err != nil {
			continue
		}

		for _, sn := range skillNames {
			skillID := skillIDFromName(sn)
			pg.pool.Exec(ctx,
				"INSERT INTO skill_assignments (employee_id, skill_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
				empID, skillID)
		}
	}

	slog.Info("default skill assignments seeded")
	return nil
}

// HTTP handlers

func (h *APIHandler) ListSkills(w http.ResponseWriter, r *http.Request) {
	if h.esClient == nil {
		writeError(w, "Elasticsearch not available", http.StatusServiceUnavailable)
		return
	}

	query := r.URL.Query().Get("q")
	skills, err := h.esClient.SearchSkills(r.Context(), query)
	if err != nil {
		writeError(w, "failed to search skills: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, skills)
}

func (h *APIHandler) GetSkill(w http.ResponseWriter, r *http.Request) {
	if h.esClient == nil {
		writeError(w, "Elasticsearch not available", http.StatusServiceUnavailable)
		return
	}

	id := r.PathValue("id")
	skill, err := h.esClient.GetSkill(r.Context(), id)
	if err != nil {
		writeError(w, "skill not found", http.StatusNotFound)
		return
	}
	writeJSON(w, skill)
}

func (h *APIHandler) CreateSkill(w http.ResponseWriter, r *http.Request) {
	if h.esClient == nil {
		writeError(w, "Elasticsearch not available", http.StatusServiceUnavailable)
		return
	}

	var body struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Category    string   `json:"category"`
		Content     string   `json:"content"`
		Tags        []string `json:"tags"`
		Version     string   `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.Name == "" || body.Content == "" {
		writeError(w, "name and content are required", http.StatusBadRequest)
		return
	}

	now := time.Now().UnixMilli()
	s := &Skill{
		ID:          generateID(),
		Name:        body.Name,
		Description: body.Description,
		Category:    body.Category,
		Content:     body.Content,
		Tags:        body.Tags,
		Version:     body.Version,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if s.Tags == nil {
		s.Tags = []string{}
	}
	if s.Category == "" {
		s.Category = "general"
	}
	if s.Version == "" {
		s.Version = "1.0.0"
	}

	if err := h.esClient.IndexSkill(r.Context(), s); err != nil {
		writeError(w, "failed to save skill: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("skill created", "id", s.ID, "name", s.Name)
	writeJSON(w, s)
}

func (h *APIHandler) UpdateSkill(w http.ResponseWriter, r *http.Request) {
	if h.esClient == nil {
		writeError(w, "Elasticsearch not available", http.StatusServiceUnavailable)
		return
	}

	id := r.PathValue("id")
	existing, err := h.esClient.GetSkill(r.Context(), id)
	if err != nil {
		writeError(w, "skill not found", http.StatusNotFound)
		return
	}

	var body struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Category    string   `json:"category"`
		Content     string   `json:"content"`
		Tags        []string `json:"tags"`
		Version     string   `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if body.Name != "" {
		existing.Name = body.Name
	}
	if body.Description != "" {
		existing.Description = body.Description
	}
	if body.Category != "" {
		existing.Category = body.Category
	}
	if body.Content != "" {
		existing.Content = body.Content
	}
	if body.Tags != nil {
		existing.Tags = body.Tags
	}
	if body.Version != "" {
		existing.Version = body.Version
	}
	existing.UpdatedAt = time.Now().UnixMilli()

	if err := h.esClient.IndexSkill(r.Context(), existing); err != nil {
		writeError(w, "failed to update skill: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("skill updated", "id", id)
	writeJSON(w, existing)
}

func (h *APIHandler) DeleteSkill(w http.ResponseWriter, r *http.Request) {
	if h.esClient == nil {
		writeError(w, "Elasticsearch not available", http.StatusServiceUnavailable)
		return
	}

	id := r.PathValue("id")
	if err := h.esClient.DeleteSkill(r.Context(), id); err != nil {
		writeError(w, "failed to delete skill: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("skill deleted", "id", id)
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *APIHandler) ListEmployeeSkills(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil || h.esClient == nil {
		writeError(w, "service not available", http.StatusServiceUnavailable)
		return
	}

	empID := r.PathValue("id")
	skillIDs, err := h.pgClient.ListEmployeeSkillIDs(r.Context(), empID)
	if err != nil {
		writeError(w, "failed to list skills: "+err.Error(), http.StatusInternalServerError)
		return
	}

	skills := make([]Skill, 0, len(skillIDs))
	for _, sid := range skillIDs {
		s, err := h.esClient.GetSkill(r.Context(), sid)
		if err != nil {
			continue
		}
		skills = append(skills, *s)
	}
	writeJSON(w, skills)
}

func (h *APIHandler) AssignSkillToEmployee(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}

	empID := r.PathValue("id")
	var body struct {
		SkillID string `json:"skill_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SkillID == "" {
		writeError(w, "skill_id is required", http.StatusBadRequest)
		return
	}

	if err := h.pgClient.AssignSkill(r.Context(), empID, body.SkillID); err != nil {
		writeError(w, "failed to assign skill: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("skill assigned", "employee", empID, "skill", body.SkillID)
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *APIHandler) UnassignSkillFromEmployee(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}

	empID := r.PathValue("id")
	skillID := r.PathValue("skillId")

	if err := h.pgClient.UnassignSkill(r.Context(), empID, skillID); err != nil {
		writeError(w, "failed to unassign skill: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("skill unassigned", "employee", empID, "skill", skillID)
	writeJSON(w, map[string]string{"status": "ok"})
}
