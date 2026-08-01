package main

import (
	"context"
	"mobius/internal/seed"
	"mobius/internal/tools"
	"testing"
)

func TestTaggingTools_NilBQ(t *testing.T) {
	ctx := context.Background()
	if _, ok := tools.ExecTagMediaTool(ctx, nil, nil, nil, "emp", map[string]any{"gcs_path": "gs://b/*"})["error"]; !ok {
		t.Error("tag_media should error with nil bqClient")
	}
	if _, ok := tools.ExecGetTagResultsTool(ctx, nil, map[string]any{"job_id": "x"})["error"]; !ok {
		t.Error("get_tag_results should error with nil bqClient")
	}
	if _, ok := tools.ExecQueryTagsTool(ctx, nil, map[string]any{"sql": "SELECT 1"})["error"]; !ok {
		t.Error("query_tags should error with nil bqClient")
	}
}

func TestCreativeTaggerHasVideoTaggingSkill(t *testing.T) {
	// The video-tagging SKILL.md must be a default assignment for Creative Tagger.
	// Tool ACCESS is tag-gated (media_tagger), but the SKILL.md guidance only
	// reaches the prompt via skill_assignments (chat.go:142, dispatcher.go:410).
	// Without this binding the employee has the tools but no manual. Creative
	// Tagger is a non-founder, so it lives in employeeSkillDefaults, not
	// founderSkillDefaults (the latter feeds the founder-only reset path).
	var found bool
	for _, s := range seed.EmployeeSkillDefaults()["Creative Tagger"] {
		if s == "video-tagging" {
			found = true
		}
	}
	if !found {
		t.Errorf("Creative Tagger default skills must include video-tagging, got %v",
			seed.EmployeeSkillDefaults()["Creative Tagger"])
	}
}
