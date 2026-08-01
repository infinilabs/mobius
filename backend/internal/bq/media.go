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
	stmt := fmt.Sprintf(
		"CREATE OR REPLACE MODEL %s REMOTE WITH CONNECTION `%s` OPTIONS(endpoint='%s')",
		bq.FQ(bq.creativesDataset, bq.taggingModel), bq.connection, bq.taggingEndpoint)
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
