package main

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

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"cloud.google.com/go/bigquery"
	"google.golang.org/api/iterator"
)

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

var identRe = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// identEscaper escapes the two characters that could terminate or de-fang a
// back-quoted GoogleSQL identifier: the backtick itself and the backslash.
var identEscaper = strings.NewReplacer(`\`, `\\`, "`", "\\`")

// fq returns a fully-qualified, back-quoted `project.dataset.name` reference.
// Components are backtick-escaped so a crafted identifier cannot close the
// quote and inject SQL (defense in depth — callers validate names upstream).
func (bq *BQClient) fq(dataset, name string) string {
	return fmt.Sprintf("`%s.%s.%s`",
		identEscaper.Replace(bq.projectID), identEscaper.Replace(dataset), identEscaper.Replace(name))
}

// RunQuery runs a read query and returns (columns, rows). Optional query
// parameters are bound by name (e.g. @asset_id).
func (bq *BQClient) RunQuery(ctx context.Context, sql string, params ...bigquery.QueryParameter) ([]string, [][]any, error) {
	q := bq.client.Query(sql)
	if len(params) > 0 {
		q.Parameters = params
	}
	it, err := q.Read(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("bq query: %w", err)
	}
	var cols []string
	var rows [][]any
	for {
		var row []bigquery.Value
		err := it.Next(&row)
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("bq read: %w", err)
		}
		if cols == nil {
			for _, f := range it.Schema {
				cols = append(cols, f.Name)
			}
		}
		r := make([]any, len(row))
		for i, v := range row {
			r[i] = v
		}
		rows = append(rows, r)
	}
	return cols, rows, nil
}

// RunReadOnlyQuery enforces read-only structurally: it dry-runs sql first and
// refuses to execute unless BigQuery itself reports the statement type as
// SELECT. Unlike keyword matching, this cannot be bypassed with casing or
// comment tricks. Used for user-supplied analytics SQL (query_tags, §11).
func (bq *BQClient) RunReadOnlyQuery(ctx context.Context, sql string, params ...bigquery.QueryParameter) ([]string, [][]any, error) {
	q := bq.client.Query(sql)
	if len(params) > 0 {
		q.Parameters = params
	}
	q.DryRun = true
	job, err := q.Run(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("bq dry run: %w", err)
	}
	var stmtType string
	if stats := job.LastStatus().Statistics; stats != nil {
		if qs, ok := stats.Details.(*bigquery.QueryStatistics); ok {
			stmtType = qs.StatementType
		}
	}
	if err := requireSelectStatement(stmtType); err != nil {
		return nil, nil, err
	}
	return bq.RunQuery(ctx, sql, params...)
}

// requireSelectStatement is the read-only gate for RunReadOnlyQuery, split out
// so it can be unit-tested without a live BigQuery client.
func requireSelectStatement(stmtType string) error {
	if stmtType != "SELECT" {
		return fmt.Errorf("only SELECT queries are allowed (BigQuery reports statement type %q)", stmtType)
	}
	return nil
}

// RunStatement runs a DDL/DML statement to completion (bounded-synchronous, §5).
func (bq *BQClient) RunStatement(ctx context.Context, sql string) error {
	q := bq.client.Query(sql)
	job, err := q.Run(ctx)
	if err != nil {
		return fmt.Errorf("bq run: %w", err)
	}
	status, err := job.Wait(ctx)
	if err != nil {
		return fmt.Errorf("bq wait: %w", err)
	}
	if status.Err() != nil {
		return fmt.Errorf("bq job failed: %w", status.Err())
	}
	return nil
}

// EnsureTaggingInfra idempotently creates the dedicated creatives dataset and the
// remote tagging model. Requires the cloud-resource connection to already exist
// with Vertex AI + GCS read roles (see `make bq-connection`, §6).
func (bq *BQClient) EnsureTaggingInfra(ctx context.Context) error {
	ds := bq.client.Dataset(bq.creativesDataset)
	if _, err := ds.Metadata(ctx); err != nil {
		if cerr := ds.Create(ctx, &bigquery.DatasetMetadata{}); cerr != nil {
			return fmt.Errorf("create creatives dataset %s: %w", bq.creativesDataset, cerr)
		}
		slog.Info("BigQuery creatives dataset created", "dataset", bq.creativesDataset)
	}
	stmt := fmt.Sprintf(
		"CREATE OR REPLACE MODEL %s REMOTE WITH CONNECTION `%s` OPTIONS(endpoint='%s')",
		bq.fq(bq.creativesDataset, bq.taggingModel), bq.connection, bq.taggingEndpoint)
	if err := bq.RunStatement(ctx, stmt); err != nil {
		return fmt.Errorf("ensure remote model (connection %q must exist with Vertex AI + GCS read roles — run `make bq-connection`): %w", bq.connection, err)
	}
	return nil
}

// validateGCSPrefix rejects anything that is not a clean gs:// URI/glob, since the
// prefix is interpolated into the object-table DDL (no parameter possible there).
func validateGCSPrefix(p string) (string, error) {
	p = strings.TrimSpace(p)
	if !strings.HasPrefix(p, "gs://") {
		return "", fmt.Errorf("gcs_path must start with gs:// (got %q)", p)
	}
	if strings.ContainsAny(p, "'\"`\n\r;") {
		return "", fmt.Errorf("gcs_path contains illegal characters")
	}
	return p, nil
}

// CreateObjectTable exposes a GCS media prefix to BigQuery as an object table.
func (bq *BQClient) CreateObjectTable(ctx context.Context, name, gcsPrefix string) error {
	prefix, err := validateGCSPrefix(gcsPrefix)
	if err != nil {
		return err
	}
	stmt := fmt.Sprintf(
		"CREATE OR REPLACE EXTERNAL TABLE %s WITH CONNECTION `%s` OPTIONS(object_metadata='SIMPLE', uris=['%s'])",
		bq.fq(bq.creativesDataset, name), bq.connection, prefix)
	return bq.RunStatement(ctx, stmt)
}

// taggingSQL builds the AI.GENERATE_TABLE statement. Separated out so a test can
// assert its shape without a live BigQuery (video_tagging.md §9a).
func (bq *BQClient) taggingSQL(objectTable, tagsTable string) string {
	return fmt.Sprintf(`CREATE OR REPLACE TABLE %s AS
SELECT
  REGEXP_EXTRACT(uri, r'/([^/]+)\.[^.]+$') AS asset_id,
  uri,
  labels,
  full_response,
  status,
  CURRENT_TIMESTAMP() AS tagged_at
FROM AI.GENERATE_TABLE(
  MODEL %s,
  (SELECT (@prompt_text, OBJ.GET_ACCESS_URL(ref, 'r')) AS prompt, uri FROM %s),
  STRUCT('labels ARRAY<STRING>' AS output_schema, 0.2 AS temperature, 2048 AS max_output_tokens))`,
		bq.fq(bq.creativesDataset, tagsTable),
		bq.fq(bq.creativesDataset, bq.taggingModel),
		bq.fq(bq.creativesDataset, objectTable))
}

// RunTagging runs the tagging job to completion (bounded-synchronous). The
// taxonomy prompt is bound as a parameter to avoid any SQL-injection / escaping.
func (bq *BQClient) RunTagging(ctx context.Context, objectTable, tagsTable, promptText string) error {
	q := bq.client.Query(bq.taggingSQL(objectTable, tagsTable))
	q.Parameters = []bigquery.QueryParameter{{Name: "prompt_text", Value: promptText}}
	job, err := q.Run(ctx)
	if err != nil {
		return fmt.Errorf("run tagging: %w", err)
	}
	status, err := job.Wait(ctx)
	if err != nil {
		return fmt.Errorf("wait tagging: %w", err)
	}
	if status.Err() != nil {
		return fmt.Errorf("tagging job failed: %w", status.Err())
	}
	return nil
}

func (bq *BQClient) countTable(ctx context.Context, dataset, name string) (int, error) {
	_, rows, err := bq.RunQuery(ctx, fmt.Sprintf("SELECT COUNT(*) AS c FROM %s", bq.fq(dataset, name)))
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 || len(rows[0]) == 0 {
		return 0, nil
	}
	switch v := rows[0][0].(type) {
	case int64:
		return int(v), nil
	case int:
		return v, nil
	default:
		return 0, nil
	}
}

// defaultTaxonomyPromptID mirrors seedPrompts (prompts.go): id = sha256(name)[:8]
// for the "video_label_tagging.md" template.
func defaultTaxonomyPromptID() string {
	h := sha256.Sum256([]byte("video_label_tagging"))
	return hex.EncodeToString(h[:8])
}

func resolveTaxonomyPrompt(ctx context.Context, es *ESClient, args map[string]any) (string, error) {
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

// execTagMediaTool runs the full Mode-B pipeline. A free function so both tool
// routers (adapter_internal.routeToolCall and APIHandler.executeToolCall) share it.
func execTagMediaTool(ctx context.Context, bq *BQClient, es *ESClient, events *EventPipeline, agentID string, args map[string]any) map[string]any {
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

	jobID := generateID()
	objTable := "obj_" + jobID
	tagsTable := "tags_" + jobID

	if err := bq.CreateObjectTable(ctx, objTable, gcsPath); err != nil {
		return map[string]any{"error": "create object table: " + err.Error()}
	}
	count, err := bq.countTable(ctx, bq.creativesDataset, objTable)
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
		events.Publish(newEvent("media_tagged", aid, nil, nil, map[string]any{
			"gcs_path": gcsPath, "asset_count": count, "tags_table": tagsTable, "job_id": jobID,
		}))
	}
	slog.Info("media tagging completed", "job_id", jobID, "assets", count, "table", tagsTable)
	return map[string]any{
		"status":      "completed",
		"job_id":      jobID,
		"asset_count": count,
		"tags_table":  fmt.Sprintf("%s.%s", bq.creativesDataset, tagsTable),
		"dataset":     bq.creativesDataset,
	}
}

func taggingTableName(jobID, tagsTable string) (string, error) {
	if strings.TrimSpace(tagsTable) != "" {
		name := tagsTable
		if i := strings.LastIndex(name, "."); i >= 0 {
			name = name[i+1:] // drop any dataset/project prefix; we scope to creativesDataset
		}
		name = strings.TrimSpace(name)
		if !identRe.MatchString(name) {
			return "", fmt.Errorf("invalid tags_table %q", tagsTable)
		}
		return name, nil
	}
	j := strings.TrimSpace(jobID)
	if !identRe.MatchString(j) {
		return "", fmt.Errorf("invalid or missing job_id")
	}
	return "tags_" + j, nil
}

func execGetTagResultsTool(ctx context.Context, bq *BQClient, args map[string]any) map[string]any {
	if bq == nil {
		return map[string]any{"error": "BigQuery is not configured"}
	}
	jobID, _ := args["job_id"].(string)
	tagsTable, _ := args["tags_table"].(string)
	assetID, _ := args["asset_id"].(string)

	table, err := taggingTableName(jobID, tagsTable)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}

	sql := fmt.Sprintf("SELECT asset_id, uri, labels, status FROM %s", bq.fq(bq.creativesDataset, table))
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
		"tags_table": fmt.Sprintf("%s.%s", bq.creativesDataset, table),
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

func execQueryTagsTool(ctx context.Context, bq *BQClient, args map[string]any) map[string]any {
	if bq == nil {
		return map[string]any{"error": "BigQuery is not configured"}
	}
	rawSQL, _ := args["sql"].(string)
	safe, err := guardSelect(rawSQL, bq.creativesDataset)
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
