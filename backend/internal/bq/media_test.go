package bq

import (
	"strings"
	"testing"
)

func taggingTestClient() *Client {
	return &Client{
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

func TestFQ(t *testing.T) {
	bq := taggingTestClient()

	// Normal identifiers keep the plain `project.dataset.name` form.
	if got, want := bq.FQ("mobius_creatives", "tags_x"), "`du-hast-mich.mobius_creatives.tags_x`"; got != want {
		t.Errorf("fq = %q, want %q", got, want)
	}

	// A crafted name must not be able to close the backtick quote and inject SQL:
	// every backtick (and backslash, so the escape itself can't be neutralized)
	// inside a component must come out escaped.
	got := bq.FQ("mobius_creatives", "x` OPTIONS(uris=['gs://evil']) --")
	if strings.Contains(got, "x` ") {
		t.Errorf("fq must escape backticks in identifiers, got %q", got)
	}
	if !strings.Contains(got, "x\\`") {
		t.Errorf("fq should backslash-escape inner backticks, got %q", got)
	}
	got = bq.FQ("ds\\", "n")
	if !strings.Contains(got, "ds\\\\") {
		t.Errorf("fq should escape backslashes, got %q", got)
	}
}

func TestRequireSelectStatement(t *testing.T) {
	if err := requireSelectStatement("SELECT"); err != nil {
		t.Errorf("SELECT must be allowed: %v", err)
	}
	for _, st := range []string{"INSERT", "DELETE", "MERGE", "CREATE_TABLE_AS_SELECT", "SCRIPT", ""} {
		if err := requireSelectStatement(st); err == nil {
			t.Errorf("statement type %q must be refused", st)
		}
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
