package httpapi

import (
	"encoding/json"
	"log/slog"
	"mobius/internal/domain"
	"mobius/internal/skills"
	"net/http"
	"time"
)

// domain.Skill sync lives in internal/skills (plan 6.4f); handlers below call it.

// domain.Skill lives in internal/domain (plan 6.2); see domain_aliases.go.

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
	s := &domain.Skill{
		ID:          domain.NewID(),
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

	if h.skillsDir != "" {
		if err := skills.WriteToDisk(h.skillsDir, s); err != nil {
			slog.Warn("failed to write skill to disk", "name", s.Name, "error", err)
		}
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

	oldName := existing.Name
	oldCategory := existing.Category

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

	if h.skillsDir != "" {
		if oldName != existing.Name || oldCategory != existing.Category {
			old := &domain.Skill{Name: oldName, Category: oldCategory}
			skills.RemoveFromDisk(h.skillsDir, old)
		}
		if err := skills.WriteToDisk(h.skillsDir, existing); err != nil {
			slog.Warn("failed to write skill to disk", "name", existing.Name, "error", err)
		}
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

	skill, _ := h.esClient.GetSkill(r.Context(), id)

	if err := h.esClient.DeleteSkill(r.Context(), id); err != nil {
		writeError(w, "failed to delete skill: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if h.pgClient != nil {
		if _, err := h.pgClient.Pool().Exec(r.Context(),
			"DELETE FROM skill_assignments WHERE skill_id=$1", id); err != nil {
			slog.Warn("failed to clean up skill assignments", "skill_id", id, "error", err)
		}
	}

	if h.skillsDir != "" && skill != nil {
		if err := skills.RemoveFromDisk(h.skillsDir, skill); err != nil {
			slog.Warn("failed to remove skill from disk", "name", skill.Name, "error", err)
		}
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

	skills := make([]domain.Skill, 0, len(skillIDs))
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

func (h *APIHandler) ResetEmployeeSkills(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil || h.esClient == nil {
		writeError(w, "service not available", http.StatusServiceUnavailable)
		return
	}

	empID := r.PathValue("id")
	if err := h.pgClient.ResetEmployeeSkills(r.Context(), h.esClient, empID); err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}

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
