package main

import (
	"context"
	"encoding/json"
	"fmt"
)

// TaskInteraction lives in internal/domain (plan 6.2); see domain_aliases.go.

// buildInteractionContext injects previously resolved questions/answers into an
// agent's system prompt so it continues from the human's responses instead of
// re-asking. Returns "" when the task has no resolved interactions.
func buildInteractionContext(ctx context.Context, pg *PGClient, taskID string) string {
	interactions, err := pg.ListResolvedInteractions(ctx, taskID)
	if err != nil || len(interactions) == 0 {
		return ""
	}

	prompt := "\n\n## Resolved Questions\n"
	prompt += "The following questions were previously asked and answered:\n"
	for _, ix := range interactions {
		var question string
		var payloadMap map[string]any
		if ix.Payload != nil {
			if err := json.Unmarshal(ix.Payload, &payloadMap); err == nil {
				question, _ = payloadMap["question"].(string)
			}
		}

		var answer string
		if ix.Response != nil {
			var respMap map[string]any
			if err := json.Unmarshal(ix.Response, &respMap); err == nil {
				answer, _ = respMap["answer"].(string)
			}
		}

		if question != "" {
			prompt += fmt.Sprintf("- Q: %s\n  A: %s\n", question, answer)
		}
	}
	prompt += "\nUse these answers to continue your work. Do NOT re-ask resolved questions.\n"
	return prompt
}
