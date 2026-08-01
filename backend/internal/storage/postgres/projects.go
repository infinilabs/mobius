package postgres

import (
	"context"
	"fmt"
	"mobius/internal/domain"
	"os"
	"path/filepath"
	"strings"
)

// PG operations

func (pg *Client) ListProjects(ctx context.Context, status string) ([]domain.Project, error) {
	query := `
		SELECT p.id, p.name, p.description, p.status, p.source_path, p.tags,
		       p.created_at, p.updated_at,
		       p.owner_id, e.name, e.title, e.role,
		       (SELECT COUNT(*) FROM tasks t WHERE t.project_id = p.id) AS task_count
		FROM projects p
		LEFT JOIN employees e ON e.id = p.owner_id
		WHERE p.name <> '__creatives__'
	`
	var args []any
	if status != "" {
		query += " AND p.status = $1"
		args = append(args, status)
	}
	query += " ORDER BY p.updated_at DESC"

	rows, err := pg.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	var projects []domain.Project
	for rows.Next() {
		var p domain.Project
		var ownerID, ownerName, ownerTitle, ownerRole *string
		if err := rows.Scan(
			&p.ID, &p.Name, &p.Description, &p.Status, &p.SourcePath, &p.Tags,
			&p.CreatedAt, &p.UpdatedAt,
			&ownerID, &ownerName, &ownerTitle, &ownerRole,
			&p.TaskCount,
		); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		if ownerID != nil {
			p.Owner = &domain.EmployeeBrief{ID: *ownerID, Name: *ownerName, Title: *ownerTitle, Role: *ownerRole}
		}
		if p.Tags == nil {
			p.Tags = []string{}
		}
		projects = append(projects, p)
	}
	if projects == nil {
		projects = []domain.Project{}
	}
	return projects, nil
}

func (pg *Client) GetProject(ctx context.Context, id string) (*domain.Project, error) {
	var p domain.Project
	var ownerID, ownerName, ownerTitle, ownerRole *string

	err := pg.pool.QueryRow(ctx, `
		SELECT p.id, p.name, p.description, p.status, p.source_path, p.tags,
		       p.created_at, p.updated_at,
		       p.owner_id, e.name, e.title, e.role,
		       (SELECT COUNT(*) FROM tasks t WHERE t.project_id = p.id) AS task_count
		FROM projects p
		LEFT JOIN employees e ON e.id = p.owner_id
		WHERE p.id = $1
	`, id).Scan(
		&p.ID, &p.Name, &p.Description, &p.Status, &p.SourcePath, &p.Tags,
		&p.CreatedAt, &p.UpdatedAt,
		&ownerID, &ownerName, &ownerTitle, &ownerRole,
		&p.TaskCount,
	)
	if err != nil {
		return nil, fmt.Errorf("get project: %w", err)
	}
	if ownerID != nil {
		p.Owner = &domain.EmployeeBrief{ID: *ownerID, Name: *ownerName, Title: *ownerTitle, Role: *ownerRole}
	}
	if p.Tags == nil {
		p.Tags = []string{}
	}
	return &p, nil
}

func (pg *Client) GetProjectByName(ctx context.Context, name string) (*domain.Project, error) {
	var id string
	err := pg.pool.QueryRow(ctx, "SELECT id FROM projects WHERE name = $1", name).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("get project by name: %w", err)
	}
	return pg.GetProject(ctx, id)
}

// CreateProject is the one create interface every entry point (REST, chat,
// dispatcher, MCP) funnels through. After the row is written it re-mirrors the
// project into ES via reindexProject (best-effort, centralized). Returns the
// fully-populated project (owner, counts).
func (pg *Client) CreateProject(ctx context.Context, in domain.CreateProjectInput, baseDir string, templateDirs []string) (*domain.Project, error) {
	isImport := in.SourcePath != nil
	if err := domain.ValidateProjectName(in.Name, isImport); err != nil {
		return nil, err
	}

	rootDir := (&domain.Project{Name: in.Name, SourcePath: in.SourcePath}).RootDir(baseDir)

	if isImport {
		if !filepath.IsAbs(*in.SourcePath) {
			return nil, fmt.Errorf("source_path must be an absolute path")
		}
		info, err := os.Stat(*in.SourcePath)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("source_path %q does not exist or is not a directory", *in.SourcePath)
		}
	} else {
		if _, serr := os.Stat(rootDir); !os.IsNotExist(serr) {
			return nil, fmt.Errorf("project directory already exists on disk: %s", rootDir)
		}
	}

	var ownerID *string
	if in.OwnerID != "" {
		ownerID = &in.OwnerID
	}
	tags := in.Tags
	if tags == nil {
		tags = []string{}
	}

	var id string
	if err := pg.pool.QueryRow(ctx, `
		INSERT INTO projects (name, description, owner_id, status, source_path, tags)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`, in.Name, in.Description, ownerID, "active", in.SourcePath, tags).Scan(&id); err != nil {
		return nil, fmt.Errorf("insert project: %w", err)
	}

	if isImport {
		gitignorePath := filepath.Join(rootDir, ".gitignore")
		appendGitignoreEntries(gitignorePath, []string{"mobius.md", ".conversations/"})
	} else {
		os.MkdirAll(rootDir, 0755)
		for _, dir := range templateDirs {
			os.MkdirAll(filepath.Join(rootDir, dir), 0755)
		}
	}

	os.MkdirAll(filepath.Join(rootDir, ".conversations"), 0755)

	mobiusPath := filepath.Join(rootDir, "mobius.md")
	if _, err := os.Stat(mobiusPath); os.IsNotExist(err) {
		content := fmt.Sprintf("# %s\n\n%s\n\n## Key Decisions\n\n## Architecture Notes\n\n## Open Questions\n", in.Name, in.Description)
		os.WriteFile(mobiusPath, []byte(content), 0644)
	}

	pg.reindexProject(ctx, id)
	return pg.GetProject(ctx, id)
}

func appendGitignoreEntries(path string, entries []string) {
	existing, _ := os.ReadFile(path)
	content := string(existing)

	var toAdd []string
	for _, entry := range entries {
		if !strings.Contains(content, entry) {
			toAdd = append(toAdd, entry)
		}
	}
	if len(toAdd) == 0 {
		return
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		f.WriteString("\n")
	}
	for _, entry := range toAdd {
		f.WriteString(entry + "\n")
	}
}

func (pg *Client) UpdateProject(ctx context.Context, id string, name, description, status *string) error {
	sets := []string{"updated_at = NOW()"}
	args := []any{}
	argN := 1

	if name != nil {
		sets = append(sets, fmt.Sprintf("name = $%d", argN))
		args = append(args, *name)
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

	if len(sets) == 1 {
		return nil
	}

	query := fmt.Sprintf("UPDATE projects SET %s WHERE id = $%d", strings.Join(sets, ", "), argN)
	args = append(args, id)

	if _, err := pg.pool.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("update project: %w", err)
	}
	pg.reindexProject(ctx, id)
	return nil
}

func (pg *Client) DeleteProject(ctx context.Context, id string) error {
	_, err := pg.pool.Exec(ctx, "DELETE FROM projects WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	return nil
}

func (pg *Client) NullifyProjectTasks(ctx context.Context, projectID string) error {
	_, err := pg.pool.Exec(ctx,
		"UPDATE tasks SET project_id = NULL WHERE project_id = $1", projectID)
	return err
}

// domain.Project Memory
