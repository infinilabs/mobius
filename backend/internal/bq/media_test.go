package bq

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func taggingTestClient() *Client {
	return &Client{
		projectID:        "du-hast-mich",
		creativesDataset: "mobius_creatives",
		connection:       "us.mobius_conn",
		taggingEndpoint:  "gemini-3.6-flash",
		taggingModel:     "tagging_gemini",
	}
}

func TestTaggingEndpointFor(t *testing.T) {
	ctx := t.Context()

	// Unpinned + resolver → the auto-discovered endpoint wins.
	bq := taggingTestClient()
	bq.SetTaggingEndpointResolver(func(context.Context) (string, error) { return "gemini-9.9-flash", nil })
	if got := bq.taggingEndpointFor(ctx); got != "gemini-9.9-flash" {
		t.Errorf("unpinned: got %q, want auto-discovered gemini-9.9-flash", got)
	}

	// Pinned via conf.yaml → the configured endpoint is used verbatim, no discovery.
	pinned := taggingTestClient()
	pinned.endpointPinned = true
	pinned.taggingEndpoint = "gemini-2.5-flash"
	pinned.SetTaggingEndpointResolver(func(context.Context) (string, error) {
		t.Error("resolver must not be called when the endpoint is pinned")
		return "", nil
	})
	if got := pinned.taggingEndpointFor(ctx); got != "gemini-2.5-flash" {
		t.Errorf("pinned: got %q, want configured gemini-2.5-flash", got)
	}

	// Discovery failure → fall back to the built-in default.
	failing := taggingTestClient()
	failing.SetTaggingEndpointResolver(func(context.Context) (string, error) { return "", fmt.Errorf("boom") })
	if got := failing.taggingEndpointFor(ctx); got != "gemini-3.6-flash" {
		t.Errorf("fallback: got %q, want default gemini-3.6-flash", got)
	}

	// No resolver wired (e.g. Vertex client unavailable) → default.
	if got := taggingTestClient().taggingEndpointFor(ctx); got != "gemini-3.6-flash" {
		t.Errorf("no resolver: got %q, want default gemini-3.6-flash", got)
	}
}

func TestTaggingEndpointURL(t *testing.T) {
	// Newer Flash models exist only behind multi-regional endpoints; a bare
	// name lets BigQuery route to us-central1 where they don't exist
	// ("Publisher model ... not found" — the exact failure from the field).
	bq := taggingTestClient()
	cases := []struct {
		name       string
		connection string
		endpoint   string
		want       string
	}{
		{"us multi-region expands", "us.mobius_conn", "gemini-3.6-flash",
			"https://aiplatform.us.rep.googleapis.com/v1/projects/du-hast-mich/locations/us/publishers/google/models/gemini-3.6-flash"},
		{"eu multi-region expands", "eu.mobius_conn", "gemini-3.6-flash",
			"https://aiplatform.eu.rep.googleapis.com/v1/projects/du-hast-mich/locations/eu/publishers/google/models/gemini-3.6-flash"},
		{"global expands", "global.mobius_conn", "gemini-3.6-flash",
			"https://aiplatform.googleapis.com/v1/projects/du-hast-mich/locations/global/publishers/google/models/gemini-3.6-flash"},
		{"single region passes through", "us-central1.mobius_conn", "gemini-2.5-flash", "gemini-2.5-flash"},
		{"full URL passes through", "us.mobius_conn", "https://example.com/v1/x", "https://example.com/v1/x"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bq.connection = c.connection
			if got := bq.taggingEndpointURL(c.endpoint); got != c.want {
				t.Errorf("got %q\nwant %q", got, c.want)
			}
		})
	}
}

func TestTopTagsSQL_Shape(t *testing.T) {
	bq := taggingTestClient()
	sql := bq.topTagsSQL("summer_sale_a1b2c3d4_tags", 10)
	for _, frag := range []string{
		"FROM `du-hast-mich.mobius_creatives.summer_sale_a1b2c3d4_tags`",
		"UNNEST(labels)",
		"GROUP BY tag",
		"ORDER BY n DESC",
		"LIMIT 10",
	} {
		if !strings.Contains(sql, frag) {
			t.Errorf("topTagsSQL missing %q\nSQL:\n%s", frag, sql)
		}
	}
}

func TestMergeRepoSQL_Shape(t *testing.T) {
	bq := taggingTestClient()
	sql := bq.mergeRepoSQL("summer_sale_a1b2c3d4_tags")

	mustContain := []string{
		"MERGE `du-hast-mich.mobius_creatives.creative_repo` T",
		"FROM `du-hast-mich.mobius_creatives.summer_sale_a1b2c3d4_tags`",
		"IFNULL(status, '') = ''", // failed rows never reach the repo
		"ON T.uri = S.uri",        // upsert key: re-adding a job cannot duplicate
		"@source_path",
		"@job_id",
		"WHEN MATCHED THEN UPDATE",
		"WHEN NOT MATCHED THEN INSERT",
	}
	for _, frag := range mustContain {
		if !strings.Contains(sql, frag) {
			t.Errorf("mergeRepoSQL missing %q\nSQL:\n%s", frag, sql)
		}
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
