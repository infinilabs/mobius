---
name: video-tagging
description: Batch-tag video/image creatives in BigQuery and answer descriptive tag analytics in chat. Use when asked to label media under a GCS path, or to report tag counts/Top-N/distributions.
version: 1.0.0
category: general
tags:
  - media_tagger
  - bigquery
  - tagging
  - analytics
---

# Video / Image Tagging (BigQuery)

You tag media in bulk and then answer simple analytics about the resulting tags.
Tools are available only to employees with the `media_tagger` tag (e.g. **Creative
Tagger**). If you don't have `tag_media`, you can't run this — delegate to the
Creative Tagger instead.

## A. Batch tagging — `tag_media`

Goal: label every video/image under a GCS prefix against a label taxonomy and
store the results in a BigQuery tags table.

**Before calling, you MUST have:**
- `gcs_path` — the source media prefix as a **glob**, e.g. `gs://bucket/creatives/*`.
  If the user didn't give one, **ask** (use `ask_user`). Never guess a bucket.
- taxonomy — defaults to the **"video label tagging"** template. Only pass
  `taxonomy_prompt_id` if the user wants a different label set.

**Run it:**
1. Call `tag_media` with `{ gcs_path }` (and `taxonomy_prompt_id` only if specified).
2. It ensures infra, builds an object table, tags via `AI.GENERATE_TABLE`
   (schema-enforced `labels ARRAY<STRING>` — nothing to parse), and returns
   `{ job_id, asset_count, tags_table, status }`.
3. Report the `asset_count` and `tags_table` to the user. To show labels for
   specific assets, call `get_tag_results` with the `job_id` (optionally an
   `asset_id`).

**Cost & safety:**
- Multimodal inference is billable per asset. There is a per-call cap
  (200 assets); if exceeded, narrow `gcs_path`. Mention the asset count before
  large runs.
- Only Mobius-owned buckets / the caller's assets — never arbitrary buckets.
- Video over 2 minutes: only the first 2 minutes are analyzed (model limit).

**If it fails with a connection/permission error:** the one-time BigQuery
connection isn't set up. Tell the operator to run `make bq-connection` (creates
the `us.mobius_conn` cloud-resource connection and grants it Vertex AI + GCS
read). Infra (dataset + remote model) is created automatically; only the
connection + IAM is manual. Fail loud with the exact missing piece.

## B. Tag analytics — `query_tags` + a `mobius-viz` chart

When the user asks descriptive questions ("how many tags", "top 5 tags",
"tag distribution as a pie"), answer with data + a chart + a short narrative.
Scope is descriptive only: counts, Top-N, per-tag totals (`GROUP BY`, `COUNT`,
`ORDER BY ... LIMIT N`). Deeper analysis (co-occurrence, similarity, ML) is out
of scope.

**Steps:**
1. Translate the question into ONE read-only `SELECT` over the creatives tags
   dataset. Tags are an `ARRAY<STRING>` column called `labels`, so use `UNNEST`:

   ```sql
   SELECT tag, COUNT(*) AS n
   FROM `mobius_creatives.tags_<job>`, UNNEST(labels) AS tag
   GROUP BY tag
   ORDER BY n DESC
   LIMIT 5
   ```

   No DML/DDL, no comments, single statement. Results are row-capped.
2. Call `query_tags` with `{ sql }`. It returns `{ columns, rows }`.
3. Reply with a one-paragraph narrative, then emit **exactly one** fenced
   `mobius-viz` block so the chat renders a table + chart:

   ````
   ```mobius-viz
   { "type": "bar", "title": "Top 5 tags", "x": "tag", "y": "n",
     "rows": [ {"tag":"卡通","n":42}, {"tag":"竖版","n":31} ] }
   ```
   ````

**`mobius-viz` spec:**
- `type`: `"table" | "bar" | "pie" | "line"`. Omit → `bar`. Use the type the user
  asked for; a Top-N table always renders alongside the chart.
- `x`: the category/label column name (e.g. `"tag"`).
- `y`: the numeric value column name (e.g. `"n"`).
- `rows`: array of objects straight from `query_tags` (keys = column names).
- `title`: optional chart title.
- Honor a user-specified N (put `LIMIT N` in the SQL). Default chart is a bar.

Put only the chart data in the block; keep your analysis as normal text outside it.
