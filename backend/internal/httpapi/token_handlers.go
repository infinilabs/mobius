package httpapi

import (
	"fmt"
	"mobius/internal/bq"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/bigquery"
)

type tokenFilters struct {
	Since       time.Time
	Until       time.Time
	ModelIDs    []string
	EmployeeIDs []string
	ProjectIDs  []string
	TaskIDs     []string
	Sources     []string
}

func parseTokenFilters(r *http.Request) tokenFilters {
	q := r.URL.Query()
	f := tokenFilters{}

	if s := q.Get("since"); s != "" {
		f.Since, _ = time.Parse(time.RFC3339, s)
	}
	if s := q.Get("until"); s != "" {
		f.Until, _ = time.Parse(time.RFC3339, s)
	}
	if f.Since.IsZero() {
		f.Since = time.Now().AddDate(0, 0, -7)
	}
	if f.Until.IsZero() {
		f.Until = time.Now()
	}

	f.ModelIDs = q["model_id"]
	f.EmployeeIDs = q["employee_id"]
	f.ProjectIDs = q["project_id"]
	f.TaskIDs = q["task_id"]
	f.Sources = q["source"]
	return f
}

func (f *tokenFilters) buildWhere(dataset, table string) (string, []bigquery.QueryParameter) {
	clauses := []string{"timestamp BETWEEN @since AND @until"}
	params := []bigquery.QueryParameter{
		{Name: "since", Value: f.Since},
		{Name: "until", Value: f.Until},
	}

	addFilter := func(col, paramName string, vals []string) {
		if len(vals) == 0 {
			return
		}
		clauses = append(clauses, fmt.Sprintf("%s IN UNNEST(@%s)", col, paramName))
		params = append(params, bigquery.QueryParameter{Name: paramName, Value: vals})
	}

	addFilter("model_id", "model_ids", f.ModelIDs)
	addFilter("employee_id", "employee_ids", f.EmployeeIDs)
	addFilter("project_id", "project_ids", f.ProjectIDs)
	addFilter("task_id", "task_ids", f.TaskIDs)
	addFilter("source", "sources", f.Sources)

	where := strings.Join(clauses, " AND ")
	fqTable := fmt.Sprintf("`%s.%s`", dataset, table)
	return fmt.Sprintf("FROM %s WHERE %s", fqTable, where), params
}

func (h *APIHandler) TokenSummary(w http.ResponseWriter, r *http.Request) {
	if h.bqClient == nil {
		writeError(w, "BigQuery not available", http.StatusServiceUnavailable)
		return
	}

	f := parseTokenFilters(r)
	fromWhere, params := f.buildWhere(h.bqClient.Dataset(), h.bqClient.TokenTable())

	sql := fmt.Sprintf(`SELECT
		COUNT(*) as total_calls,
		COALESCE(SUM(prompt_tokens), 0) as total_prompt,
		COALESCE(SUM(completion_tokens), 0) as total_completion,
		COALESCE(SUM(total_tokens), 0) as total_tokens,
		COUNT(DISTINCT NULLIF(employee_id, '')) as active_agents,
		COUNT(DISTINCT NULLIF(project_id, '')) as active_projects,
		COUNTIF(status = 'error') as error_count
	%s`, fromWhere)

	duration := f.Until.Sub(f.Since)
	prevSince := f.Since.Add(-duration)
	prevUntil := f.Since
	prevParams := make([]bigquery.QueryParameter, len(params))
	copy(prevParams, params)
	prevParams[0] = bigquery.QueryParameter{Name: "since", Value: prevSince}
	prevParams[1] = bigquery.QueryParameter{Name: "until", Value: prevUntil}

	prevFromWhere, _ := f.buildWhere(h.bqClient.Dataset(), h.bqClient.TokenTable())
	prevSQL := fmt.Sprintf(`SELECT
		COUNT(*) as total_calls,
		COALESCE(SUM(total_tokens), 0) as total_tokens,
		COUNTIF(status = 'error') as error_count,
		COUNT(DISTINCT NULLIF(employee_id, '')) as active_agents
	%s`, prevFromWhere)

	type summaryRow struct {
		TotalCalls      int64 `bigquery:"total_calls" json:"total_calls"`
		TotalPrompt     int64 `bigquery:"total_prompt" json:"total_prompt"`
		TotalCompletion int64 `bigquery:"total_completion" json:"total_completion"`
		TotalTokens     int64 `bigquery:"total_tokens" json:"total_tokens"`
		ActiveAgents    int64 `bigquery:"active_agents" json:"active_agents"`
		ActiveProjects  int64 `bigquery:"active_projects" json:"active_projects"`
		ErrorCount      int64 `bigquery:"error_count" json:"error_count"`
	}

	type prevRow struct {
		TotalCalls   int64 `bigquery:"total_calls"`
		TotalTokens  int64 `bigquery:"total_tokens"`
		ErrorCount   int64 `bigquery:"error_count"`
		ActiveAgents int64 `bigquery:"active_agents"`
	}

	ctx := r.Context()
	current, err := bq.QueryRow[summaryRow](ctx, h.bqClient, sql, params)
	if err != nil {
		writeError(w, "query failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	prev, _ := bq.QueryRow[prevRow](ctx, h.bqClient, prevSQL, prevParams)

	writeJSON(w, map[string]any{
		"current":  current,
		"previous": prev,
	})
}

func (h *APIHandler) TokenTimeseries(w http.ResponseWriter, r *http.Request) {
	if h.bqClient == nil {
		writeError(w, "BigQuery not available", http.StatusServiceUnavailable)
		return
	}

	f := parseTokenFilters(r)
	interval := r.URL.Query().Get("interval")
	if interval != "HOUR" {
		interval = "DAY"
	}

	fromWhere, params := f.buildWhere(h.bqClient.Dataset(), h.bqClient.TokenTable())
	sql := fmt.Sprintf(`SELECT
		TIMESTAMP_TRUNC(timestamp, %s) as bucket,
		COALESCE(SUM(prompt_tokens), 0) as prompt_tokens,
		COALESCE(SUM(completion_tokens), 0) as completion_tokens,
		COALESCE(SUM(total_tokens), 0) as total_tokens,
		COUNT(*) as calls
	%s
	GROUP BY bucket ORDER BY bucket`, interval, fromWhere)

	type tsRow struct {
		Bucket           time.Time `bigquery:"bucket" json:"bucket"`
		PromptTokens     int64     `bigquery:"prompt_tokens" json:"prompt_tokens"`
		CompletionTokens int64     `bigquery:"completion_tokens" json:"completion_tokens"`
		TotalTokens      int64     `bigquery:"total_tokens" json:"total_tokens"`
		Calls            int64     `bigquery:"calls" json:"calls"`
	}

	rows, err := bq.QueryRows[tsRow](r.Context(), h.bqClient, sql, params)
	if err != nil {
		writeError(w, "query failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, rows)
}

func (h *APIHandler) TokenBreakdown(w http.ResponseWriter, r *http.Request) {
	if h.bqClient == nil {
		writeError(w, "BigQuery not available", http.StatusServiceUnavailable)
		return
	}

	groupByParam := r.URL.Query().Get("group_by")
	allowedGroupBy := map[string]string{
		"model_id":      "model_id",
		"employee_id":   "employee_id",
		"employee_name": "employee_name",
		"project_id":    "project_id",
		"task_id":       "task_id",
		"source":        "source",
		"provider":      "provider",
	}
	groupBy, ok := allowedGroupBy[groupByParam]
	if !ok {
		groupBy = "model_id"
	}

	f := parseTokenFilters(r)
	fromWhere, params := f.buildWhere(h.bqClient.Dataset(), h.bqClient.TokenTable())
	sql := fmt.Sprintf(`SELECT
		%s as dimension,
		COALESCE(SUM(total_tokens), 0) as total_tokens,
		COALESCE(SUM(prompt_tokens), 0) as prompt_tokens,
		COALESCE(SUM(completion_tokens), 0) as completion_tokens,
		COUNT(*) as calls
	%s
	GROUP BY dimension
	HAVING dimension != ''
	ORDER BY total_tokens DESC
	LIMIT 10`, groupBy, fromWhere)

	type bdRow struct {
		Dimension        string `bigquery:"dimension" json:"dimension"`
		TotalTokens      int64  `bigquery:"total_tokens" json:"total_tokens"`
		PromptTokens     int64  `bigquery:"prompt_tokens" json:"prompt_tokens"`
		CompletionTokens int64  `bigquery:"completion_tokens" json:"completion_tokens"`
		Calls            int64  `bigquery:"calls" json:"calls"`
	}

	rows, err := bq.QueryRows[bdRow](r.Context(), h.bqClient, sql, params)
	if err != nil {
		writeError(w, "query failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, rows)
}

func (h *APIHandler) TokenDetails(w http.ResponseWriter, r *http.Request) {
	if h.bqClient == nil {
		writeError(w, "BigQuery not available", http.StatusServiceUnavailable)
		return
	}

	f := parseTokenFilters(r)
	limit := 50
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil && n >= 0 {
			offset = n
		}
	}

	sortCol := r.URL.Query().Get("sort")
	allowedSort := map[string]bool{"timestamp": true, "total_tokens": true, "prompt_tokens": true, "completion_tokens": true, "latency_ms": true, "model_id": true}
	if !allowedSort[sortCol] {
		sortCol = "timestamp"
	}
	sortDir := "DESC"
	if r.URL.Query().Get("order") == "asc" {
		sortDir = "ASC"
	}

	fromWhere, params := f.buildWhere(h.bqClient.Dataset(), h.bqClient.TokenTable())
	params = append(params,
		bigquery.QueryParameter{Name: "lim", Value: int64(limit)},
		bigquery.QueryParameter{Name: "off", Value: int64(offset)},
	)

	sql := fmt.Sprintf(`SELECT
		id, timestamp, model_id, provider, employee_id, employee_name,
		project_id, task_id, conversation_id,
		prompt_tokens, completion_tokens, total_tokens,
		cached_tokens, thoughts_tokens, tool_use_tokens,
		latency_ms, status, error_message, source
	%s
	ORDER BY %s %s
	LIMIT @lim OFFSET @off`, fromWhere, sortCol, sortDir)

	type detailRow struct {
		ID               string    `bigquery:"id" json:"id"`
		Timestamp        time.Time `bigquery:"timestamp" json:"timestamp"`
		ModelID          string    `bigquery:"model_id" json:"model_id"`
		Provider         string    `bigquery:"provider" json:"provider"`
		EmployeeID       string    `bigquery:"employee_id" json:"employee_id"`
		EmployeeName     string    `bigquery:"employee_name" json:"employee_name"`
		ProjectID        string    `bigquery:"project_id" json:"project_id"`
		TaskID           string    `bigquery:"task_id" json:"task_id"`
		ConversationID   string    `bigquery:"conversation_id" json:"conversation_id"`
		PromptTokens     int64     `bigquery:"prompt_tokens" json:"prompt_tokens"`
		CompletionTokens int64     `bigquery:"completion_tokens" json:"completion_tokens"`
		TotalTokens      int64     `bigquery:"total_tokens" json:"total_tokens"`
		CachedTokens     int64     `bigquery:"cached_tokens" json:"cached_tokens"`
		ThoughtsTokens   int64     `bigquery:"thoughts_tokens" json:"thoughts_tokens"`
		ToolUseTokens    int64     `bigquery:"tool_use_tokens" json:"tool_use_tokens"`
		LatencyMs        int64     `bigquery:"latency_ms" json:"latency_ms"`
		Status           string    `bigquery:"status" json:"status"`
		ErrorMessage     string    `bigquery:"error_message" json:"error_message"`
		Source           string    `bigquery:"source" json:"source"`
	}

	rows, err := bq.QueryRows[detailRow](r.Context(), h.bqClient, sql, params)
	if err != nil {
		writeError(w, "query failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, rows)
}

// --- BQ query helpers ---
