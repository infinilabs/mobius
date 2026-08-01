package domain

// TokenUsage is the per-run token/latency accounting recorded on task runs.
type TokenUsage struct {
	PromptTokens     int32
	CompletionTokens int32
	TotalTokens      int32
	CachedTokens     int32
	ThoughtsTokens   int32
	ToolUseTokens    int32
	LatencyMs        int64
}
