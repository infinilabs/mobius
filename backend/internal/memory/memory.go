// Package memory distills durable per-employee memories from chat/run
// exchanges and resolves the model used for extraction (plan 6.4f prep).
package memory

import (
	"context"
	"fmt"
	"log/slog"
	"mobius/internal/config"
	"mobius/internal/domain"
	"mobius/internal/llm"
	"mobius/internal/search"
	"strings"
)

const defaultFallbackModel = "gemini-3.1-pro-preview"

const (
	MaxExtractionInputLen  = 2000
	maxExtractionOutputLen = 200
)

func ResolveModelID(cfg *config.Config, employee *domain.Employee) string {
	settings := cfg.GetSettings()
	modelID, _ := settings.GoogleCloud.VertexAI.DefaultLLM()
	if modelID == "" {
		modelID = defaultFallbackModel
	}
	if employee != nil {
		for _, m := range employee.Models {
			if m.Purpose == "primary_llm" && m.ModelID != "" {
				modelID = m.ModelID
				break
			}
		}
	}
	return modelID
}

func TruncateForExtraction(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return domain.TruncateStr(s, maxLen) + "...[truncated]"
}

func isValidMemory(text string) bool {
	if len(text) > maxExtractionOutputLen {
		return false
	}
	if len(text) < 10 {
		return false
	}
	lower := strings.ToLower(text)
	for _, pattern := range []string{
		"ignore previous",
		"ignore all",
		"disregard",
		"new instructions",
		"system prompt",
		"you are now",
		"act as",
		"pretend to",
	} {
		if strings.Contains(lower, pattern) {
			return false
		}
	}
	return true
}

func AbsorbFromExchange(ctx context.Context, cfg *config.Config, providers *llm.ProviderRegistry,
	esClient *search.Client, employeeID, input, response, sourceID string) {
	if cfg == nil || esClient == nil {
		return
	}

	settings := cfg.GetSettings()
	modelID, _ := settings.GoogleCloud.VertexAI.DefaultLLM()
	if modelID == "" {
		return
	}
	provider := providers.ResolveProvider(modelID)
	if provider == nil {
		return
	}

	safeInput := TruncateForExtraction(input, MaxExtractionInputLen)
	safeResponse := TruncateForExtraction(response, MaxExtractionInputLen)

	prompt := fmt.Sprintf(`You extract concise facts from conversations.

Review this exchange:
<user_message>
%s
</user_message>
<assistant_response>
%s
</assistant_response>

If a new technical decision, convention, constraint, or user preference was established, output it as a single concise sentence (max 200 characters).
Examples:
- "We use pgx/v5 for PostgreSQL transactions in this project."
- "The user prefers CamelCase for Go struct field names."

If nothing new was decided, output exactly: NONE`, safeInput, safeResponse)

	req := &llm.LLMRequest{
		Model:    modelID,
		Messages: []llm.LLMMessage{{Role: "user", Text: prompt}},
		OnText:   func(string) {},
	}

	result, err := provider.ChatStream(ctx, req)
	if err != nil || result == "" || strings.TrimSpace(strings.ToUpper(result)) == "NONE" {
		return
	}

	memoryText := strings.TrimSpace(result)
	if memoryText != "" && isValidMemory(memoryText) {
		esClient.IndexEmployeeMemoryDedup(ctx, employeeID, sourceID, memoryText)
		slog.Info("memory absorbed", "employee_id", employeeID, "memory", memoryText)
	} else if memoryText != "" {
		slog.Warn("memory rejected by validation", "employee_id", employeeID, "text", TruncateForExtraction(memoryText, 100))
	}
}
