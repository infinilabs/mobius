package bq

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"cloud.google.com/go/bigquery"
	"google.golang.org/api/iterator"
)

// identEscaper escapes the two characters that could terminate or de-fang a
// back-quoted GoogleSQL identifier: the backtick itself and the backslash.
var identEscaper = strings.NewReplacer(`\`, `\\`, "`", "\\`")

// fq returns a fully-qualified, back-quoted `project.dataset.name` reference.
// Components are backtick-escaped so a crafted identifier cannot close the
// quote and inject SQL (defense in depth — callers validate names upstream).
func (bq *Client) FQ(dataset, name string) string {
	return fmt.Sprintf("`%s.%s.%s`",
		identEscaper.Replace(bq.projectID), identEscaper.Replace(dataset), identEscaper.Replace(name))
}

// RunQuery runs a read query and returns (columns, rows). Optional query
// parameters are bound by name (e.g. @asset_id).
func (bq *Client) RunQuery(ctx context.Context, sql string, params ...bigquery.QueryParameter) ([]string, [][]any, error) {
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
func (bq *Client) RunReadOnlyQuery(ctx context.Context, sql string, params ...bigquery.QueryParameter) ([]string, [][]any, error) {
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
func (bq *Client) RunStatement(ctx context.Context, sql string) error {
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

// SetTaggingEndpointResolver installs the callback that discovers the newest
// Gemini Flash model at tagging time. Only consulted when the endpoint is not
// pinned via conf.yaml (tagging_model_endpoint).
func (bq *Client) SetTaggingEndpointResolver(f func(context.Context) (string, error)) {
	bq.resolveEndpoint = f
}

// taggingEndpointFor returns the endpoint for this tagging run: the pinned
// config value verbatim, else the auto-discovered latest Flash, else the
// built-in default as a fallback when discovery fails.
func (bq *Client) taggingEndpointFor(ctx context.Context) string {
	if bq.endpointPinned || bq.resolveEndpoint == nil {
		return bq.taggingEndpoint
	}
	ep, err := bq.resolveEndpoint(ctx)
	if err != nil || ep == "" {
		slog.Warn("tagging endpoint auto-discovery failed; using fallback",
			"fallback", bq.taggingEndpoint, "error", err)
		return bq.taggingEndpoint
	}
	return ep
}

// taggingEndpointURL expands a bare Gemini model name into the fully qualified
// multi-regional Vertex endpoint for the connection's location. Newer Flash
// models (gemini-3.5+/3.6) exist ONLY behind multi-regional endpoints, and
// BigQuery's short-name resolution can route to a single region (us-central1)
// where they don't exist — "Publisher model ... not found". A full URL
// configured in conf.yaml is passed through untouched.
func (bq *Client) taggingEndpointURL(endpoint string) string {
	if strings.Contains(endpoint, "://") {
		return endpoint
	}
	loc, _, ok := strings.Cut(bq.connection, ".")
	if !ok {
		return endpoint
	}
	switch loc {
	case "us", "eu":
		return fmt.Sprintf("https://aiplatform.%s.rep.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/%s",
			loc, bq.projectID, loc, endpoint)
	case "global":
		return fmt.Sprintf("https://aiplatform.googleapis.com/v1/projects/%s/locations/global/publishers/google/models/%s",
			bq.projectID, endpoint)
	default: // single-region connection: BigQuery's own resolution is correct
		return endpoint
	}
}

// EnsureTaggingInfra idempotently creates the dedicated creatives dataset and the
// remote tagging model. Requires the cloud-resource connection to already exist
// with Vertex AI + GCS read roles (see `make bq-connection`, §6).
func (bq *Client) EnsureTaggingInfra(ctx context.Context) error {
	ds := bq.client.Dataset(bq.creativesDataset)
	if _, err := ds.Metadata(ctx); err != nil {
		if cerr := ds.Create(ctx, &bigquery.DatasetMetadata{}); cerr != nil {
			return fmt.Errorf("create creatives dataset %s: %w", bq.creativesDataset, cerr)
		}
		slog.Info("BigQuery creatives dataset created", "dataset", bq.creativesDataset)
	}
	endpoint := bq.taggingEndpointURL(bq.taggingEndpointFor(ctx))
	slog.Info("ensuring tagging remote model", "model", bq.taggingModel, "endpoint", endpoint, "pinned", bq.endpointPinned)
	stmt := fmt.Sprintf(
		"CREATE OR REPLACE MODEL %s REMOTE WITH CONNECTION `%s` OPTIONS(endpoint='%s')",
		bq.FQ(bq.creativesDataset, bq.taggingModel), bq.connection, endpoint)
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
func (bq *Client) CreateObjectTable(ctx context.Context, name, gcsPrefix string) error {
	prefix, err := validateGCSPrefix(gcsPrefix)
	if err != nil {
		return err
	}
	stmt := fmt.Sprintf(
		"CREATE OR REPLACE EXTERNAL TABLE %s WITH CONNECTION `%s` OPTIONS(object_metadata='SIMPLE', uris=['%s'])",
		bq.FQ(bq.creativesDataset, name), bq.connection, prefix)
	return bq.RunStatement(ctx, stmt)
}

// taggingSQL builds the AI.GENERATE_TABLE statement. Separated out so a test can
// assert its shape without a live BigQuery (video_tagging.md §9a).
func (bq *Client) taggingSQL(objectTable, tagsTable string) string {
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
		bq.FQ(bq.creativesDataset, tagsTable),
		bq.FQ(bq.creativesDataset, bq.taggingModel),
		bq.FQ(bq.creativesDataset, objectTable))
}

// RunTagging runs the tagging job to completion (bounded-synchronous). The
// taxonomy prompt is bound as a parameter to avoid any SQL-injection / escaping.
func (bq *Client) RunTagging(ctx context.Context, objectTable, tagsTable, promptText string) error {
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

// creativeRepoTable is the durable, cross-job repository of tagged creatives.
// Per-job <prefix>_objects/<prefix>_tags tables are scratch; rows land here
// only when the user confirms after a tagging run (add_to_creative_repo).
const creativeRepoTable = "creative_repo"

func (bq *Client) CreativeRepoTable() string { return creativeRepoTable }

// EnsureCreativeRepo idempotently creates the creative_repo table.
func (bq *Client) EnsureCreativeRepo(ctx context.Context) error {
	stmt := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  asset_id STRING,
  uri STRING,
  labels ARRAY<STRING>,
  source_path STRING,
  job_id STRING,
  tagged_at TIMESTAMP,
  added_at TIMESTAMP
)`, bq.FQ(bq.creativesDataset, creativeRepoTable))
	if err := bq.RunStatement(ctx, stmt); err != nil {
		return fmt.Errorf("ensure creative repo table: %w", err)
	}
	return nil
}

// mergeRepoSQL builds the upsert from a job's tags table into creative_repo.
// Separated out so a test can assert its shape without a live BigQuery. Only
// successfully tagged rows (empty status) are merged; uri is the merge key so
// re-adding a job, or re-tagging the same assets, never duplicates rows.
func (bq *Client) mergeRepoSQL(tagsTable string) string {
	return fmt.Sprintf(`MERGE %s T
USING (SELECT asset_id, uri, labels, tagged_at FROM %s WHERE IFNULL(status, '') = '') S
ON T.uri = S.uri
WHEN MATCHED THEN UPDATE SET
  asset_id = S.asset_id, labels = S.labels, source_path = @source_path,
  job_id = @job_id, tagged_at = S.tagged_at, added_at = CURRENT_TIMESTAMP()
WHEN NOT MATCHED THEN INSERT (asset_id, uri, labels, source_path, job_id, tagged_at, added_at)
VALUES (S.asset_id, S.uri, S.labels, @source_path, @job_id, S.tagged_at, CURRENT_TIMESTAMP())`,
		bq.FQ(bq.creativesDataset, creativeRepoTable),
		bq.FQ(bq.creativesDataset, tagsTable))
}

// MergeIntoCreativeRepo upserts one tagging job's rows into creative_repo and
// returns the number of rows inserted or updated.
func (bq *Client) MergeIntoCreativeRepo(ctx context.Context, tagsTable, sourcePath, jobID string) (int64, error) {
	q := bq.client.Query(bq.mergeRepoSQL(tagsTable))
	q.Parameters = []bigquery.QueryParameter{
		{Name: "source_path", Value: sourcePath},
		{Name: "job_id", Value: jobID},
	}
	job, err := q.Run(ctx)
	if err != nil {
		return 0, fmt.Errorf("merge creative repo: %w", err)
	}
	status, err := job.Wait(ctx)
	if err != nil {
		return 0, fmt.Errorf("wait creative repo merge: %w", err)
	}
	if status.Err() != nil {
		return 0, fmt.Errorf("creative repo merge failed: %w", status.Err())
	}
	if stats := job.LastStatus().Statistics; stats != nil {
		if qs, ok := stats.Details.(*bigquery.QueryStatistics); ok && qs.DMLStats != nil {
			return qs.DMLStats.InsertedRowCount + qs.DMLStats.UpdatedRowCount, nil
		}
	}
	return 0, nil
}

// topTagsSQL builds the label-frequency query for a tags table. Separated out
// for shape tests.
func (bq *Client) topTagsSQL(tagsTable string, limit int) string {
	return fmt.Sprintf(
		"SELECT tag, COUNT(*) AS n FROM %s, UNNEST(labels) AS tag GROUP BY tag ORDER BY n DESC LIMIT %d",
		bq.FQ(bq.creativesDataset, tagsTable), limit)
}

// TopTags returns the most frequent labels in a tags table as {tag, n} rows,
// so tag_media can hand the chat a ready-made summary without extra tool calls.
func (bq *Client) TopTags(ctx context.Context, tagsTable string, limit int) ([]map[string]any, error) {
	cols, rows, err := bq.RunQuery(ctx, bq.topTagsSQL(tagsTable, limit))
	if err != nil {
		return nil, fmt.Errorf("top tags: %w", err)
	}
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		m := make(map[string]any, len(cols))
		for i, c := range cols {
			if i < len(r) {
				m[c] = r[i]
			}
		}
		out = append(out, m)
	}
	return out, nil
}

// FindTagsTable resolves a tagging job's tags table from the short job token
// embedded in its name (<gcs-folder>_<token>_tags).
func (bq *Client) FindTagsTable(ctx context.Context, jobToken string) (string, error) {
	sql := fmt.Sprintf(
		"SELECT table_name FROM %s WHERE ENDS_WITH(table_name, CONCAT('_', @tok, '_tags')) LIMIT 1",
		bq.FQ(bq.creativesDataset, "INFORMATION_SCHEMA.TABLES"))
	_, rows, err := bq.RunQuery(ctx, sql, bigquery.QueryParameter{Name: "tok", Value: jobToken})
	if err != nil {
		return "", fmt.Errorf("look up tags table: %w", err)
	}
	if len(rows) == 0 || len(rows[0]) == 0 {
		return "", fmt.Errorf("no tags table found for job %q; pass tags_table instead", jobToken)
	}
	name, _ := rows[0][0].(string)
	if name == "" {
		return "", fmt.Errorf("no tags table found for job %q; pass tags_table instead", jobToken)
	}
	return name, nil
}

func (bq *Client) CountTable(ctx context.Context, dataset, name string) (int, error) {
	_, rows, err := bq.RunQuery(ctx, fmt.Sprintf("SELECT COUNT(*) AS c FROM %s", bq.FQ(dataset, name)))
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
