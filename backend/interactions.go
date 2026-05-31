package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type TaskInteraction struct {
	ID                string          `json:"id"`
	TaskID            string          `json:"task_id"`
	CreatorEmployeeID string          `json:"creator_employee_id"`
	Kind              string          `json:"kind"`
	Status            string          `json:"status"`
	Payload           json.RawMessage `json:"payload"`
	Response          json.RawMessage `json:"response,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	ResolvedAt        *time.Time      `json:"resolved_at,omitempty"`
	ResolvedBy        *string         `json:"resolved_by,omitempty"`
}

func (pg *PGClient) CreateInteraction(ctx context.Context, ix *TaskInteraction) error {
	if ix.Payload == nil {
		ix.Payload = json.RawMessage("{}")
	}
	err := pg.pool.QueryRow(ctx, `
		INSERT INTO task_interactions (task_id, creator_employee_id, kind, payload)
		VALUES ($1, $2, $3, $4)
		RETURNING id, status, created_at
	`, ix.TaskID, ix.CreatorEmployeeID, ix.Kind, ix.Payload).Scan(
		&ix.ID, &ix.Status, &ix.CreatedAt)
	if err != nil {
		return fmt.Errorf("create interaction: %w", err)
	}
	return nil
}

func (pg *PGClient) ListInteractions(ctx context.Context, taskID string) ([]TaskInteraction, error) {
	rows, err := pg.pool.Query(ctx, `
		SELECT id, task_id, creator_employee_id, kind, status, payload, response,
		       created_at, resolved_at, resolved_by
		FROM task_interactions
		WHERE task_id = $1
		ORDER BY created_at
	`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list interactions: %w", err)
	}
	defer rows.Close()

	var interactions []TaskInteraction
	for rows.Next() {
		var ix TaskInteraction
		if err := rows.Scan(&ix.ID, &ix.TaskID, &ix.CreatorEmployeeID, &ix.Kind,
			&ix.Status, &ix.Payload, &ix.Response,
			&ix.CreatedAt, &ix.ResolvedAt, &ix.ResolvedBy); err != nil {
			return nil, fmt.Errorf("scan interaction: %w", err)
		}
		interactions = append(interactions, ix)
	}
	if interactions == nil {
		interactions = []TaskInteraction{}
	}
	return interactions, nil
}

func (pg *PGClient) ListResolvedInteractions(ctx context.Context, taskID string) ([]TaskInteraction, error) {
	rows, err := pg.pool.Query(ctx, `
		SELECT id, task_id, creator_employee_id, kind, status, payload, response,
		       created_at, resolved_at, resolved_by
		FROM task_interactions
		WHERE task_id = $1 AND status = 'resolved'
		ORDER BY created_at
	`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list resolved interactions: %w", err)
	}
	defer rows.Close()

	var interactions []TaskInteraction
	for rows.Next() {
		var ix TaskInteraction
		if err := rows.Scan(&ix.ID, &ix.TaskID, &ix.CreatorEmployeeID, &ix.Kind,
			&ix.Status, &ix.Payload, &ix.Response,
			&ix.CreatedAt, &ix.ResolvedAt, &ix.ResolvedBy); err != nil {
			return nil, fmt.Errorf("scan interaction: %w", err)
		}
		interactions = append(interactions, ix)
	}
	if interactions == nil {
		interactions = []TaskInteraction{}
	}
	return interactions, nil
}

func (pg *PGClient) ResolveInteraction(ctx context.Context, interactionID string, resolvedBy string, response json.RawMessage) error {
	if response == nil {
		response = json.RawMessage("{}")
	}
	// resolved_by is a nullable UUID FK; an empty string is not a valid UUID, so
	// store NULL when the resolver is unspecified.
	var resolvedByArg any
	if resolvedBy != "" {
		resolvedByArg = resolvedBy
	}
	result, err := pg.pool.Exec(ctx, `
		UPDATE task_interactions
		SET status = 'resolved', response = $1, resolved_at = NOW(), resolved_by = $2
		WHERE id = $3 AND status = 'pending'
	`, response, resolvedByArg, interactionID)
	if err != nil {
		return fmt.Errorf("resolve interaction: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("interaction not found or already resolved")
	}
	return nil
}

func (pg *PGClient) DismissInteraction(ctx context.Context, interactionID string) error {
	_, err := pg.pool.Exec(ctx, `
		UPDATE task_interactions
		SET status = 'dismissed'
		WHERE id = $1 AND status = 'pending'
	`, interactionID)
	if err != nil {
		return fmt.Errorf("dismiss interaction: %w", err)
	}
	return nil
}

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
