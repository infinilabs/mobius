package main

// Transitional aliases (plan 6.4e): LLM providers live in internal/llm.

import (
	"mobius/internal/llm"
)

type (
	ToolDef          = llm.ToolDef
	ToolCall         = llm.ToolCall
	LLMMessage       = llm.LLMMessage
	LLMRequest       = llm.LLMRequest
	LLMProvider      = llm.LLMProvider
	ProviderRegistry = llm.ProviderRegistry
	GeminiProvider   = llm.GeminiProvider
	ClaudeProvider   = llm.ClaudeProvider
)

var (
	NewProviderRegistry = llm.NewProviderRegistry
	NewGeminiProvider   = llm.NewGeminiProvider
	NewClaudeProvider   = llm.NewClaudeProvider
	latestFlashEndpoint = llm.LatestFlashEndpoint
)
