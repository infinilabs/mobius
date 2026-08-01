package tools

import (
	"strings"
	"testing"

	"mobius/internal/domain"
)

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

func TestDefaultTaxonomyPromptID_MatchesSeed(t *testing.T) {
	// Must equal seedPrompts' id for "video_label_tagging" (prompts.go), else the
	// default taxonomy can't be resolved.
	if got := defaultTaxonomyPromptID(); got != domain.SkillIDFromName("video_label_tagging") {
		t.Errorf("defaultTaxonomyPromptID=%q, expected sha256(name)[:8]=%q", got, domain.SkillIDFromName("video_label_tagging"))
	}
}
