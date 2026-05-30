package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// errGoalNotFound is returned when an operation targets a goal id that does not
// exist, letting handlers map it to 404 (distinct from a 500 on a real fault).
var errGoalNotFound = errors.New("goal not found")

// errGoalBadReference is returned when a create references a parent goal or
// project that does not exist, letting handlers map it to 400 instead of
// surfacing a raw FK-violation 500.
var errGoalBadReference = errors.New("referenced parent goal or project does not exist")

type Goal struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	ParentID    *string    `json:"parent_id,omitempty"`
	ProjectID   *string    `json:"project_id,omitempty"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (pg *PGClient) CreateGoal(ctx context.Context, g *Goal) error {
	if g.Status == "" {
		g.Status = "active"
	}
	// Validate references up front so a bad id returns a clean 400 instead of a
	// raw FK-violation 500 (ties to M10).
	if g.ParentID != nil {
		if _, err := pg.GetGoal(ctx, *g.ParentID); err != nil {
			return fmt.Errorf("%w: parent goal %s", errGoalBadReference, *g.ParentID)
		}
	}
	if g.ProjectID != nil {
		if _, err := pg.GetProject(ctx, *g.ProjectID); err != nil {
			return fmt.Errorf("%w: project %s", errGoalBadReference, *g.ProjectID)
		}
	}
	err := pg.pool.QueryRow(ctx, `
		INSERT INTO goals (title, description, parent_id, project_id, status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at
	`, g.Title, g.Description, g.ParentID, g.ProjectID, g.Status).Scan(
		&g.ID, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create goal: %w", err)
	}
	return nil
}

func (pg *PGClient) GetGoal(ctx context.Context, id string) (*Goal, error) {
	var g Goal
	err := pg.pool.QueryRow(ctx, `
		SELECT id, title, description, parent_id, project_id, status, created_at, updated_at
		FROM goals WHERE id = $1
	`, id).Scan(&g.ID, &g.Title, &g.Description, &g.ParentID, &g.ProjectID,
		&g.Status, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get goal: %w", err)
	}
	return &g, nil
}

// ListGoals returns goals for a project, or — when projectID is empty — every
// goal. The unscoped list is intentional: Mobius is single-tenant and the Goals
// page renders one global goal tree (see the single-tenant threat model).
func (pg *PGClient) ListGoals(ctx context.Context, projectID string) ([]Goal, error) {
	query := "SELECT id, title, description, parent_id, project_id, status, created_at, updated_at FROM goals"
	var args []any
	if projectID != "" {
		query += " WHERE project_id = $1"
		args = append(args, projectID)
	}
	query += " ORDER BY created_at"

	rows, err := pg.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list goals: %w", err)
	}
	defer rows.Close()

	var goals []Goal
	for rows.Next() {
		var g Goal
		if err := rows.Scan(&g.ID, &g.Title, &g.Description, &g.ParentID, &g.ProjectID,
			&g.Status, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan goal: %w", err)
		}
		goals = append(goals, g)
	}
	if goals == nil {
		goals = []Goal{}
	}
	return goals, nil
}

func (pg *PGClient) UpdateGoal(ctx context.Context, id string, title, description *string, status *string) error {
	sets := []string{"updated_at = NOW()"}
	args := []any{}
	argN := 1

	if title != nil {
		sets = append(sets, fmt.Sprintf("title = $%d", argN))
		args = append(args, *title)
		argN++
	}
	if description != nil {
		sets = append(sets, fmt.Sprintf("description = $%d", argN))
		args = append(args, *description)
		argN++
	}
	if status != nil {
		sets = append(sets, fmt.Sprintf("status = $%d", argN))
		args = append(args, *status)
		argN++
	}

	query := fmt.Sprintf("UPDATE goals SET %s WHERE id = $%d", strings.Join(sets, ", "), argN)
	args = append(args, id)

	tag, err := pg.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update goal: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errGoalNotFound
	}
	return nil
}

func (pg *PGClient) DeleteGoal(ctx context.Context, id string) error {
	_, err := pg.pool.Exec(ctx, "DELETE FROM goals WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete goal: %w", err)
	}
	return nil
}

func (pg *PGClient) GetGoalChildren(ctx context.Context, parentID string) ([]Goal, error) {
	rows, err := pg.pool.Query(ctx, `
		SELECT id, title, description, parent_id, project_id, status, created_at, updated_at
		FROM goals WHERE parent_id = $1
		ORDER BY created_at
	`, parentID)
	if err != nil {
		return nil, fmt.Errorf("get goal children: %w", err)
	}
	defer rows.Close()

	var goals []Goal
	for rows.Next() {
		var g Goal
		if err := rows.Scan(&g.ID, &g.Title, &g.Description, &g.ParentID, &g.ProjectID,
			&g.Status, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan goal child: %w", err)
		}
		goals = append(goals, g)
	}
	if goals == nil {
		goals = []Goal{}
	}
	return goals, nil
}

func buildGoalContext(ctx context.Context, pg *PGClient, task *Task) string {
	if task.GoalID == nil {
		return ""
	}

	var chain []string
	seen := map[string]bool{}
	goalID := task.GoalID
	for goalID != nil && len(chain) < 10 {
		if seen[*goalID] {
			break
		}
		seen[*goalID] = true
		goal, err := pg.GetGoal(ctx, *goalID)
		if err != nil {
			break
		}
		chain = append([]string{goal.Title}, chain...)
		goalID = goal.ParentID
	}

	if len(chain) == 0 {
		return ""
	}

	prompt := "\n\n## Why This Task Matters\n"
	for i, g := range chain {
		prompt += strings.Repeat("  ", i) + "-> " + g + "\n"
	}
	return prompt
}

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
