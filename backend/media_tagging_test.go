package main

import (
	"context"
	"strings"
	"testing"
)

func TestBuildAgentTools_MediaTagger(t *testing.T) {
	tagger := makeEmployee("Custom", []string{"media_tagger"}, strPtr("mgr-001"), nil)
	names := toolNames(buildAgentTools(tagger, nil))
	for _, required := range []string{"tag_media", "get_tag_results", "query_tags"} {
		assertContains(t, names, required, "media_tagger should have "+required)
	}
}

func TestBuildAgentTools_NonTaggerHasNoTaggingTools(t *testing.T) {
	worker := makeEmployee("Custom", []string{}, strPtr("mgr-001"), nil)
	names := toolNames(buildAgentTools(worker, nil))
	for _, forbidden := range []string{"tag_media", "get_tag_results", "query_tags"} {
		assertNotContains(t, names, forbidden, "worker without media_tagger should NOT have "+forbidden)
	}
	// A CEO without the tag also must not get tagging tools (capability, not seniority).
	ceo := makeEmployee("CEO", []string{"executive", "manager"}, nil, nil)
	ceoNames := toolNames(buildAgentTools(ceo, nil))
	assertNotContains(t, ceoNames, "tag_media", "CEO without media_tagger should NOT have tag_media")
}

func taggingTestClient() *BQClient {
	return &BQClient{
		projectID:        "du-hast-mich",
		creativesDataset: "mobius_creatives",
		connection:       "us.mobius_conn",
		taggingEndpoint:  "gemini-2.5-flash",
		taggingModel:     "tagging_gemini",
	}
}

func TestTaggingSQL_Shape(t *testing.T) {
	bq := taggingTestClient()
	sql := bq.taggingSQL("obj_abc", "tags_abc")

	mustContain := []string{
		"CREATE OR REPLACE TABLE `du-hast-mich.mobius_creatives.tags_abc`",
		"AI.GENERATE_TABLE(",
		"MODEL `du-hast-mich.mobius_creatives.tagging_gemini`",
		"FROM `du-hast-mich.mobius_creatives.obj_abc`",
		"@prompt_text",
		"OBJ.GET_ACCESS_URL(ref, 'r')",
		"'labels ARRAY<STRING>' AS output_schema",
		"labels",
		"full_response",
		"status",
	}
	for _, frag := range mustContain {
		if !strings.Contains(sql, frag) {
			t.Errorf("taggingSQL missing %q\nSQL:\n%s", frag, sql)
		}
	}
	// The taxonomy prompt must never be string-concatenated — only the @param.
	if strings.Contains(sql, "$Answer") || strings.Contains(sql, "短情景剧") {
		t.Error("taggingSQL must not embed prompt text; it must use @prompt_text")
	}
}

func TestGuardSelect(t *testing.T) {
	ds := "mobius_creatives"
	cases := []struct {
		name    string
		sql     string
		wantErr bool
	}{
		{"valid select", "SELECT tag, COUNT(*) n FROM `mobius_creatives.tags_x`, UNNEST(labels) tag GROUP BY tag", false},
		{"valid with CTE", "WITH t AS (SELECT 1) SELECT * FROM `mobius_creatives.tags_x`", false},
		{"reject insert", "INSERT INTO mobius_creatives.x VALUES (1)", true},
		{"reject update", "UPDATE mobius_creatives.x SET a=1", true},
		{"reject delete", "DELETE FROM mobius_creatives.x", true},
		{"reject drop", "DROP TABLE mobius_creatives.x", true},
		{"reject multi-statement", "SELECT 1 FROM mobius_creatives.x; DROP TABLE y", true},
		{"reject comment", "SELECT 1 FROM mobius_creatives.x -- nope", true},
		{"reject other dataset", "SELECT * FROM mobius.events", true},
		{"reject empty", "   ", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := guardSelect(c.sql, ds)
			if c.wantErr && err == nil {
				t.Errorf("expected error, got none (out=%q)", out)
			}
			if !c.wantErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if !strings.Contains(out, "LIMIT") {
					t.Errorf("guarded SQL must enforce a LIMIT cap: %q", out)
				}
			}
		})
	}
}

func TestTaggingTableName(t *testing.T) {
	cases := []struct {
		name      string
		jobID     string
		tagsTable string
		want      string
		wantErr   bool
	}{
		{"from job id", "abc123", "", "tags_abc123", false},
		{"from table", "", "tags_abc123", "tags_abc123", false},
		{"strip dataset prefix", "", "mobius_creatives.tags_abc123", "tags_abc123", false},
		{"reject injection", "", "tags_x; DROP TABLE y", "", true},
		{"reject empty", "", "", "", true},
		{"reject bad job", "a b", "", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := taggingTableName(c.jobID, c.tagsTable)
			if c.wantErr {
				if err == nil {
					t.Errorf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestValidateGCSPrefix(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"gs://bucket/creatives/*", false},
		{"gs://bucket/a.jpg", false},
		{"https://bucket/x", true},
		{"gs://bucket/'; DROP", true},
		{"", true},
	}
	for _, c := range cases {
		_, err := validateGCSPrefix(c.in)
		if c.wantErr != (err != nil) {
			t.Errorf("validateGCSPrefix(%q): wantErr=%v gotErr=%v", c.in, c.wantErr, err)
		}
	}
}

func TestTaggingTools_NilBQ(t *testing.T) {
	ctx := context.Background()
	if _, ok := execTagMediaTool(ctx, nil, nil, nil, "emp", map[string]any{"gcs_path": "gs://b/*"})["error"]; !ok {
		t.Error("tag_media should error with nil bqClient")
	}
	if _, ok := execGetTagResultsTool(ctx, nil, map[string]any{"job_id": "x"})["error"]; !ok {
		t.Error("get_tag_results should error with nil bqClient")
	}
	if _, ok := execQueryTagsTool(ctx, nil, map[string]any{"sql": "SELECT 1"})["error"]; !ok {
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
	for _, s := range employeeSkillDefaults()["Creative Tagger"] {
		if s == "video-tagging" {
			found = true
		}
	}
	if !found {
		t.Errorf("Creative Tagger default skills must include video-tagging, got %v",
			employeeSkillDefaults()["Creative Tagger"])
	}
}

func TestDefaultTaxonomyPromptID_MatchesSeed(t *testing.T) {
	// Must equal seedPrompts' id for "video_label_tagging" (prompts.go), else the
	// default taxonomy can't be resolved.
	if got := defaultTaxonomyPromptID(); got != skillIDFromName("video_label_tagging") {
		t.Errorf("defaultTaxonomyPromptID=%q, expected sha256(name)[:8]=%q", got, skillIDFromName("video_label_tagging"))
	}
}
