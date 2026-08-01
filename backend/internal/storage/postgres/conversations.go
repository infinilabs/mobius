package postgres

import (
	"context"
	"fmt"
	"mobius/internal/domain"
)

func (pg *Client) UpsertConversationMeta(ctx context.Context, conv *domain.Conversation) error {
	_, err := pg.pool.Exec(ctx, `
		INSERT INTO conversations (id, title, project_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE SET title = $2, project_id = $3, updated_at = $5
	`, conv.ID, conv.Title, conv.ProjectID, conv.CreatedAt, conv.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert conversation meta: %w", err)
	}
	return nil
}

func (pg *Client) DeleteConversationMeta(ctx context.Context, id string) error {
	_, err := pg.pool.Exec(ctx, "DELETE FROM conversations WHERE id = $1", id)
	return err
}

func (pg *Client) ListConversationsMeta(ctx context.Context, projectID string) ([]domain.ConversationSummary, error) {
	query := "SELECT id, title, project_id, updated_at FROM conversations"
	var args []any
	if projectID != "" {
		query += " WHERE project_id = $1"
		args = append(args, projectID)
	}
	query += " ORDER BY updated_at DESC"

	rows, err := pg.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list conversation meta: %w", err)
	}
	defer rows.Close()

	var out []domain.ConversationSummary
	for rows.Next() {
		var s domain.ConversationSummary
		if err := rows.Scan(&s.ID, &s.Title, &s.ProjectID, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan conversation meta: %w", err)
		}
		out = append(out, s)
	}
	if out == nil {
		out = []domain.ConversationSummary{}
	}
	return out, nil
}
