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

func TestNormalizeTagsTable(t *testing.T) {
	cases := []struct {
		name      string
		tagsTable string
		want      string
		wantErr   bool
	}{
		{"plain table", "summer_sale_a1b2c3d4_tags", "summer_sale_a1b2c3d4_tags", false},
		{"strip dataset prefix", "mobius_creatives.summer_sale_a1b2c3d4_tags", "summer_sale_a1b2c3d4_tags", false},
		{"reject injection", "tags_x; DROP TABLE y", "", true},
		{"reject empty", "", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := normalizeTagsTable(c.tagsTable)
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

func TestJobToken(t *testing.T) {
	// tag_media job ids are UUIDs; the token must match the short id embedded
	// in the job's table names (dash-free, 8 chars) or get_tag_results by
	// job_id cannot find the tables.
	cases := []struct {
		name    string
		jobID   string
		want    string
		wantErr bool
	}{
		{"uuid", "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d", "a1b2c3d4", false},
		{"already short", "abc123", "abc123", false},
		{"reject empty", "", "", true},
		{"reject junk", "a b;", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := jobToken(c.jobID)
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

func TestTableBaseName(t *testing.T) {
	// The object and tags tables must share a human-recognizable prefix derived
	// from the GCS folder, so a user can find a run's tables in the dataset.
	job := "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d"
	cases := []struct {
		name    string
		gcsPath string
		want    string
	}{
		{"folder glob", "gs://bucket/summer_sale/*", "summer_sale_a1b2c3d4"},
		{"nested folder with ext glob", "gs://bucket/2026/Summer-Sale/*.mp4", "summer_sale_a1b2c3d4"},
		{"bucket-only glob", "gs://my-bucket/*", "my_bucket_a1b2c3d4"},
		{"trailing filename skipped", "gs://bucket/ads/video.mp4", "ads_a1b2c3d4"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := tableBaseName(c.gcsPath, job); got != c.want {
				t.Errorf("tableBaseName(%q) = %q, want %q", c.gcsPath, got, c.want)
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
