package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"mobius/internal/bq"
	"mobius/internal/domain"
	evbus "mobius/internal/events"
	"mobius/internal/search"
	"regexp"
	"strings"
	"time"

	"cloud.google.com/go/bigquery"
)

// Media tagging engine + tools (video_tagging.md §4.2-4.4, §11).
//
// Mode B (batch via BigQuery): an object table over a GCS prefix is tagged with
// AI.GENERATE_TABLE, whose output_schema enforces a native labels ARRAY<STRING>
// column — there is nothing to parse. The result table also carries full_response
// (JSON) and status (STRING, empty on success → free per-row failure visibility),
// per the AI.GENERATE_TABLE reference Output section.
//
// The taxonomy prompt is user free-text (the "video label tagging" template), so
// it is passed as a query PARAMETER (@prompt_text), never concatenated into SQL.
// Identifiers (dataset/table/model) and the GCS prefix cannot be parameters, so
// they are config-derived or validated before interpolation.

const (
	// maxTagAssets caps assets per interactive tag_media call (cost guard, §7).
	maxTagAssets = 200
	// query_tags analytics row caps (§11).
	defaultQueryTagsLimit = 20
	maxQueryTagsLimit     = 1000
)

// labelArrayDirective reconciles the seeded taxonomy prompt (which historically
// ends with a 94-field JSON object contract) with AI.GENERATE_TABLE's
// schema-enforced labels ARRAY<STRING> output. Appended only to the prompt text
// sent to BigQuery; the stored prompt artifact is left untouched (Mode A still
// uses it verbatim).
const labelArrayDirective = "\n\n---\nOUTPUT OVERRIDE: Return ONLY the labels that apply, as an array of label-name strings (the `labels` field). Omit any label that does not apply. Do not return an object keyed by every label."

var (
	identRe    = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
	nonIdentRe = regexp.MustCompile(`[^a-z0-9_]+`)
)

// tableBaseName derives the shared name prefix for one tagging job's tables
// from the deepest non-wildcard GCS folder plus a short job-id suffix, so the
// object and tags tables sort together and are recognizable in the dataset:
// gs://bucket/summer_sale/* → summer_sale_a1b2c3d4_objects / ..._tags.
func tableBaseName(gcsPath, jobID string) string {
	short := strings.ReplaceAll(jobID, "-", "")
	if len(short) > 8 {
		short = short[:8]
	}
	var seg string
	parts := strings.Split(strings.TrimPrefix(gcsPath, "gs://"), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		p := parts[i]
		if p == "" || strings.ContainsAny(p, "*?[") {
			continue
		}
		if i == len(parts)-1 && strings.Contains(p, ".") {
			continue // trailing filename, not a folder
		}
		seg = p // deepest folder, else the bucket
		break
	}
	san := strings.Trim(nonIdentRe.ReplaceAllString(strings.ToLower(seg), "_"), "_")
	if san == "" {
		san = "creatives"
	}
	if len(san) > 30 {
		san = san[:30]
	}
	return san + "_" + short
}

// defaultTaxonomyPromptID mirrors seedPrompts (prompts.go): id = sha256(name)[:8]
// for the "video_label_tagging.md" template.
func defaultTaxonomyPromptID() string {
	h := sha256.Sum256([]byte("video_label_tagging"))
	return hex.EncodeToString(h[:8])
}

func resolveTaxonomyPrompt(ctx context.Context, es *search.Client, args map[string]any) (string, error) {
	if es == nil {
		return "", fmt.Errorf("prompt store (Elasticsearch) is unavailable")
	}
	id, _ := args["taxonomy_prompt_id"].(string)
	if strings.TrimSpace(id) == "" {
		id = defaultTaxonomyPromptID()
	}
	p, err := es.GetPrompt(ctx, id)
	if err != nil {
		return "", fmt.Errorf("taxonomy prompt %q not found (default is the 'video label tagging' template): %w", id, err)
	}
	if strings.TrimSpace(p.Content) == "" {
		return "", fmt.Errorf("taxonomy prompt %q is empty", id)
	}
	return p.Content + labelArrayDirective, nil
}

// ExecTagMediaTool runs the full Mode-B pipeline. A free function so both tool
// routers (adapter_internal.routeToolCall and APIHandler.executeToolCall) share it.
func ExecTagMediaTool(ctx context.Context, bq *bq.Client, es *search.Client, events *evbus.EventPipeline, agentID string, args map[string]any) map[string]any {
	if bq == nil {
		return map[string]any{"error": "BigQuery is not configured"}
	}
	gcsPath, _ := args["gcs_path"].(string)
	if strings.TrimSpace(gcsPath) == "" {
		return map[string]any{"error": "gcs_path is required (e.g. gs://bucket/creatives/*); ask the user if unknown"}
	}

	promptText, err := resolveTaxonomyPrompt(ctx, es, args)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	if err := bq.EnsureTaggingInfra(ctx); err != nil {
		return map[string]any{"error": err.Error()}
	}

	jobID := domain.NewID()
	base := tableBaseName(gcsPath, jobID)
	objTable := base + "_objects"
	tagsTable := base + "_tags"

	if err := bq.CreateObjectTable(ctx, objTable, gcsPath); err != nil {
		return map[string]any{"error": "create object table: " + err.Error()}
	}
	count, err := bq.CountTable(ctx, bq.CreativesDataset(), objTable)
	if err != nil {
		return map[string]any{"error": "count assets: " + err.Error()}
	}
	if count == 0 {
		return map[string]any{"error": "no media found under " + gcsPath + " (use a glob like gs://bucket/prefix/*)"}
	}
	if count > maxTagAssets {
		return map[string]any{"error": fmt.Sprintf("asset count %d exceeds the per-call cap of %d (cost guard); narrow gcs_path", count, maxTagAssets)}
	}

	if err := bq.RunTagging(ctx, objTable, tagsTable, promptText); err != nil {
		return map[string]any{"error": "tagging: " + err.Error()}
	}

	if events != nil {
		var aid *string
		if agentID != "" {
			aid = &agentID
		}
		events.Publish(evbus.New("media_tagged", aid, nil, nil, map[string]any{
			"gcs_path": gcsPath, "asset_count": count, "tags_table": tagsTable, "job_id": jobID,
		}))
	}
	slog.Info("media tagging completed", "job_id", jobID, "assets", count, "table", tagsTable)
	result := map[string]any{
		"status":       "completed",
		"job_id":       jobID,
		"asset_count":  count,
		"object_table": fmt.Sprintf("%s.%s", bq.CreativesDataset(), objTable),
		"tags_table":   fmt.Sprintf("%s.%s", bq.CreativesDataset(), tagsTable),
		"dataset":      bq.CreativesDataset(),
	}
	// Best-effort top-10 summary so the chat can present results without
	// another tool round-trip; the full data stays in the tags table.
	if top, err := bq.TopTags(ctx, tagsTable, 10); err == nil {
		result["top_tags"] = top
	} else {
		slog.Warn("top tags summary failed", "table", tagsTable, "error", err)
	}
	return result
}

// normalizeTagsTable validates a caller-supplied tags table name, dropping any
// dataset/project prefix (we scope to creativesDataset).
func normalizeTagsTable(tagsTable string) (string, error) {
	name := tagsTable
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSpace(name)
	if !identRe.MatchString(name) {
		return "", fmt.Errorf("invalid tags_table %q", tagsTable)
	}
	return name, nil
}

// jobToken normalizes a tag_media job id (UUID) to the short token embedded in
// the job's table names by tableBaseName.
func jobToken(jobID string) (string, error) {
	tok := strings.ReplaceAll(strings.TrimSpace(jobID), "-", "")
	if len(tok) > 8 {
		tok = tok[:8]
	}
	if tok == "" || !identRe.MatchString(tok) {
		return "", fmt.Errorf("invalid or missing job_id")
	}
	return tok, nil
}

// resolveTagsTable turns (job_id | tags_table) tool args into a validated
// table name inside the creatives dataset. tags_table wins when both are set.
func resolveTagsTable(ctx context.Context, bqc *bq.Client, jobID, tagsTable string) (string, error) {
	if strings.TrimSpace(tagsTable) != "" {
		return normalizeTagsTable(tagsTable)
	}
	tok, err := jobToken(jobID)
	if err != nil {
		return "", err
	}
	return bqc.FindTagsTable(ctx, tok)
}

func ExecGetTagResultsTool(ctx context.Context, bq *bq.Client, args map[string]any) map[string]any {
	if bq == nil {
		return map[string]any{"error": "BigQuery is not configured"}
	}
	jobID, _ := args["job_id"].(string)
	tagsTable, _ := args["tags_table"].(string)
	assetID, _ := args["asset_id"].(string)

	table, err := resolveTagsTable(ctx, bq, jobID, tagsTable)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}

	sql := fmt.Sprintf("SELECT asset_id, uri, labels, status FROM %s", bq.FQ(bq.CreativesDataset(), table))
	var params []bigquery.QueryParameter
	if strings.TrimSpace(assetID) != "" {
		sql += " WHERE asset_id = @asset_id"
		params = append(params, bigquery.QueryParameter{Name: "asset_id", Value: assetID})
	}
	sql += fmt.Sprintf(" LIMIT %d", maxTagAssets)

	cols, rows, err := bq.RunQuery(ctx, sql, params...)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{
		"results":    rowsToMaps(cols, rows),
		"count":      len(rows),
		"tags_table": fmt.Sprintf("%s.%s", bq.CreativesDataset(), table),
	}
}

// guardSelect enforces the dataset-scoped, single-statement, row-capped contract
// for the query_tags analytics tool (§11). It is NOT a generic SQL tool (§10).
// Read-only is NOT enforced here by string matching — that is done structurally
// by RunReadOnlyQuery via the dry-run statement type; the prefix check below is
// only a cheap early error for obvious non-SELECT input.
func guardSelect(sql, dataset string) (string, error) {
	s := strings.TrimSpace(sql)
	s = strings.TrimSuffix(strings.TrimSpace(s), ";")
	if s == "" {
		return "", fmt.Errorf("sql is required")
	}
	if strings.Contains(s, ";") {
		return "", fmt.Errorf("only a single statement is allowed")
	}
	if strings.Contains(s, "--") || strings.Contains(s, "/*") {
		return "", fmt.Errorf("SQL comments are not allowed")
	}
	low := strings.ToLower(s)
	if !strings.HasPrefix(low, "select") && !strings.HasPrefix(low, "with") {
		return "", fmt.Errorf("only SELECT queries are allowed")
	}
	if !strings.Contains(low, strings.ToLower(dataset)) {
		return "", fmt.Errorf("query must read from the %s dataset", dataset)
	}
	// Hard row cap regardless of any inner LIMIT (§11).
	return fmt.Sprintf("SELECT * FROM (\n%s\n) LIMIT %d", s, maxQueryTagsLimit), nil
}

// ExecAddToCreativeRepoTool upserts one tagging job's successful rows into the
// durable creative_repo table. Called only after the user confirms in chat.
func ExecAddToCreativeRepoTool(ctx context.Context, bqc *bq.Client, events *evbus.EventPipeline, agentID string, args map[string]any) map[string]any {
	if bqc == nil {
		return map[string]any{"error": "BigQuery is not configured"}
	}
	jobID, _ := args["job_id"].(string)
	tagsTable, _ := args["tags_table"].(string)
	sourcePath, _ := args["gcs_path"].(string)

	table, err := resolveTagsTable(ctx, bqc, jobID, tagsTable)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	if err := bqc.EnsureCreativeRepo(ctx); err != nil {
		return map[string]any{"error": err.Error()}
	}
	merged, err := bqc.MergeIntoCreativeRepo(ctx, table, sourcePath, jobID)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}

	if events != nil {
		var aid *string
		if agentID != "" {
			aid = &agentID
		}
		events.Publish(evbus.New("creatives_added_to_repo", aid, nil, nil, map[string]any{
			"tags_table": table, "assets_merged": merged, "job_id": jobID,
		}))
	}
	slog.Info("creatives added to repo", "tags_table", table, "assets_merged", merged)
	return map[string]any{
		"status":        "completed",
		"assets_merged": merged,
		"repo_table":    fmt.Sprintf("%s.%s", bqc.CreativesDataset(), bqc.CreativeRepoTable()),
	}
}

func ExecQueryTagsTool(ctx context.Context, bq *bq.Client, args map[string]any) map[string]any {
	if bq == nil {
		return map[string]any{"error": "BigQuery is not configured"}
	}
	rawSQL, _ := args["sql"].(string)
	safe, err := guardSelect(rawSQL, bq.CreativesDataset())
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	cols, rows, err := bq.RunReadOnlyQuery(ctx, safe)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{
		"columns":   cols,
		"rows":      rowsToMaps(cols, rows),
		"row_count": len(rows),
	}
}

func rowsToMaps(cols []string, rows [][]any) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		m := make(map[string]any, len(cols))
		for i, c := range cols {
			if i < len(r) {
				m[c] = bqValueToJSON(r[i])
			}
		}
		out = append(out, m)
	}
	return out
}

// bqValueToJSON normalizes BigQuery values (arrays, timestamps) into JSON-friendly
// forms for the tool result.
func bqValueToJSON(v any) any {
	switch t := v.(type) {
	case []bigquery.Value:
		arr := make([]any, len(t))
		for i, e := range t {
			arr[i] = bqValueToJSON(e)
		}
		return arr
	case time.Time:
		return t.Format(time.RFC3339)
	default:
		return v
	}
}
