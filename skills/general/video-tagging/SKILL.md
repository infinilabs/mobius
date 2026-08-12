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

## Routing: batch vs. single upload

- **Batch / GCS folder** ("tag everything under gs://…"): this skill. Delegate
  to the **Creative Tagger** (reports to Steve) — it runs BigQuery
  `AI.GENERATE_TABLE` with the **'video label tagging' taxonomy from the prompt
  repo** and supports follow-up tag analytics. Note the delegation policy:
  Steve delegates directly; other managers route via the CEO.
- **One (or a few) images uploaded in chat**: NOT a batch job — do not create
  object tables or BigQuery infra for a single file. Route to the
  **AI Engineer** (reports to Linas), who analyzes the uploaded file directly
  with a multimodal model call, labeling against the same 'video label
  tagging' taxonomy. Cross-org delegation goes through the CEO.

## A. Batch tagging — `tag_media`

Goal: label every video/image under a GCS prefix against a label taxonomy and
store the results in a BigQuery tags table.

**Before calling, you MUST have:**
- `gcs_path` — the source media prefix as a **glob**, e.g. `gs://bucket/creatives/*`.
  If the user didn't give one, **ask** (use `ask_user`). Never guess a bucket.
- taxonomy — unless the user specified one, use the default: the
  **"video label tagging"** template from the prompt repo (the 6-category
  Chinese ad-creative taxonomy: 讲解形式 / 内容来源 / 内容形式 / 游戏玩法 /
  美术风格 / 画面布局). Only pass `taxonomy_prompt_id` if the user wants a
  different label set. **When you used the default, say so in your summary**,
  e.g. "Tagged with the default 'video label tagging' taxonomy from the prompt
  repo — say the word if you want a different taxonomy next time."

**Run it (ONE tool call, then summarize — no extra calls):**
1. Call `tag_media` with `{ gcs_path }` (and `taxonomy_prompt_id` only if
   specified). It builds the object table, runs the tagging SQL with the
   taxonomy prompt, stores the tags table, and returns
   `{ job_id, asset_count, object_table, tags_table, top_tags, status }`.
   Both tables share a prefix derived from the GCS folder (e.g.
   `summer_sale_a1b2c3d4_objects` / `summer_sale_a1b2c3d4_tags`).
2. Summarize in chat directly from the result: report `asset_count` and
   `tags_table`, then render `top_tags` as ONE `mobius-viz` bar block
   (`x: "tag"`, `y: "n"`, rows = `top_tags` — do NOT call `query_tags` for
   this; the data is already in the result). The full per-asset data lives in
   the BigQuery `tags_table`.
3. Only on follow-up requests: `get_tag_results` (per-asset labels, pass
   `tags_table`), `query_tags` (custom analytics, section B), or
   `add_to_creative_repo` with `{ tags_table, gcs_path }` if the user asks to
   keep these creatives in the durable `creative_repo` table (upsert by `uri`;
   re-adding is safe). Do not ask about the repo proactively mid-task.

**How the SQL works** (so you can explain it or adapt analytics — `tag_media`
runs this for you; never hand-write DDL yourself):
1. Object table over the GCS glob:
   `CREATE EXTERNAL TABLE ... WITH CONNECTION ... OPTIONS(object_metadata='SIMPLE', uris=['gs://…/*'])`
   — makes each video/image a queryable row with a `ref`/`uri`.
2. Tagging: `AI.GENERATE_TABLE(MODEL <remote gemini>, (SELECT (prompt_text,
   OBJ.GET_ACCESS_URL(ref,'r')) AS prompt, uri FROM <object table>),
   STRUCT('labels ARRAY<STRING>' AS output_schema, 0.2 AS temperature,
   2048 AS max_output_tokens))` — the taxonomy prompt is paired with each
   media object; the enforced output schema yields a native `labels` array
   (no JSON parsing), plus `full_response` and per-row `status`.
3. Results land in the `<prefix>_tags` table; analytics is plain SQL over
   `UNNEST(labels)` (section B).

**Cost & safety:**
- Multimodal inference is billable per asset. There is a per-call cap
  (200 assets); if exceeded, narrow `gcs_path`. Mention the asset count before
  large runs.
- Only Mobius-owned buckets / the caller's assets — never arbitrary buckets.
- Video over 2 minutes: only the first 2 minutes are analyzed (model limit).

**If it fails with a connection/permission error:** the whole stack
(connection, IAM, dataset, remote model) is set up automatically at system
start; a failure here means that setup could not complete — most often the
app's credentials lack IAM admin to grant the connection's service account
Vertex AI + GCS read. Tell the operator to check the startup logs and run
`make bq-connection` with an IAM-admin account as the manual fallback. Fail
loud with the exact missing piece; do NOT retry in a loop.

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
   FROM `mobius_creatives.<prefix>_tags`, UNNEST(labels) AS tag
   GROUP BY tag
   ORDER BY n DESC
   LIMIT 10
   ```

   (`<prefix>_tags` is the `tags_table` returned by `tag_media`; the durable
   cross-job repository is `mobius_creatives.creative_repo` and can be queried
   the same way.)

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
