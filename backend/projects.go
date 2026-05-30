package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

func calculateSHA256(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

var projectMemoryLocks sync.Map

func getProjectLock(projectName string) *sync.RWMutex {
	mu, _ := projectMemoryLocks.LoadOrStore(projectName, &sync.RWMutex{})
	return mu.(*sync.RWMutex)
}

const (
	templateNameRegex = `^[a-z0-9][a-z0-9_-]{1,62}[a-z0-9]$`
	importNameRegex   = `^[a-zA-Z0-9][a-zA-Z0-9._-]{1,62}[a-zA-Z0-9]$`
)

var (
	templateNameRe = regexp.MustCompile(templateNameRegex)
	importNameRe   = regexp.MustCompile(importNameRegex)
)

type Project struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Owner       *EmployeeBrief `json:"owner"`
	Status      string         `json:"status"`
	SourcePath  *string        `json:"source_path,omitempty"`
	Tags        []string       `json:"tags"`
	TaskCount   int            `json:"task_count"`
	AssetCount  int            `json:"asset_count"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type ProjectAsset struct {
	ID               string         `json:"id"`
	ProjectID        string         `json:"project_id"`
	Filename         string         `json:"filename"`
	RelativePath     string         `json:"relative_path"`
	MIMEType         string         `json:"mime_type"`
	SizeBytes        int64          `json:"size_bytes"`
	Content          string         `json:"content,omitempty"`
	ContentSummary   string         `json:"content_summary,omitempty"`
	ContentTruncated bool           `json:"content_truncated"`
	ContentType      string         `json:"content_type"`
	GCSURI           string         `json:"gcs_uri,omitempty"`
	GCSStatus        string         `json:"gcs_status"`
	Checksum         string         `json:"checksum_sha256,omitempty"`
	Tags             []string       `json:"tags"`
	CreatedByID      string         `json:"created_by_id,omitempty"`
	CreatedBy        *EmployeeBrief `json:"created_by,omitempty"`
	TaskID           string         `json:"task_id,omitempty"`
	CreatedAt        string         `json:"created_at"`
	UpdatedAt        string         `json:"updated_at"`
}

func (p *Project) RootDir(cfg *Config) string {
	if p.SourcePath != nil {
		return *p.SourcePath
	}
	return filepath.Join(cfg.Projects.ProjectsDir, p.Name)
}

func projectsBaseDir(cfg *Config) string {
	return cfg.Projects.ProjectsDir
}

func validateProjectName(name string, isImport bool) error {
	if isImport {
		if !importNameRe.MatchString(name) {
			return fmt.Errorf("invalid project name %q: must match %s", name, importNameRegex)
		}
	} else {
		if !templateNameRe.MatchString(name) {
			return fmt.Errorf("invalid project name %q: must match %s", name, templateNameRegex)
		}
	}
	return nil
}

func validateProjectPath(relativePath string) error {
	if filepath.IsAbs(relativePath) {
		return fmt.Errorf("absolute paths not allowed")
	}
	// Reject any ".." path *segment* (real traversal) while allowing ".." inside a
	// filename (e.g. "my..file.txt"). A bare substring check false-positives on
	// the latter; a post-Clean prefix check alone misses "a/../../b".
	for _, seg := range strings.Split(relativePath, "/") {
		if seg == ".." {
			return fmt.Errorf("path traversal not allowed")
		}
	}
	cleaned := filepath.Clean(relativePath)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escapes project root")
	}
	return nil
}

// resolveWithinRoot validates rel lexically, joins it under root, and resolves
// symlinks on the nearest existing ancestor so a pre-existing symlink cannot
// redirect the final target outside root. It returns the absolute path to use
// for the actual file operation. All agent file tools go through this.
func resolveWithinRoot(root, rel string) (string, error) {
	if err := validateProjectPath(rel); err != nil {
		return "", err
	}
	full := filepath.Join(root, filepath.Clean(rel))

	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		resolvedRoot = filepath.Clean(root) // root may not exist yet (fresh project)
	}

	// Walk up to the nearest existing ancestor and resolve its symlinks.
	check := full
	for {
		if r, rerr := filepath.EvalSymlinks(check); rerr == nil {
			check = r
			break
		}
		parent := filepath.Dir(check)
		if parent == check {
			check = filepath.Clean(full)
			break
		}
		check = parent
	}

	if check != resolvedRoot && !strings.HasPrefix(check, resolvedRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes project root")
	}
	return full, nil
}

func resolveMimeType(filename, headerMime string) string {
	headerMime = strings.TrimSpace(strings.ToLower(headerMime))
	if headerMime != "" && headerMime != "application/octet-stream" {
		return headerMime
	}
	ext := filepath.Ext(filename)
	switch ext {
	case ".go", ".py", ".js", ".ts", ".rs":
		return "text/x-" + ext[1:]
	case ".json":
		return "application/json"
	case ".yaml", ".yml":
		return "application/yaml"
	case ".md":
		return "text/markdown"
	case ".txt":
		return "text/plain"
	case ".html":
		return "text/html"
	case ".csv":
		return "text/csv"
	case ".sql":
		return "text/x-sql"
	case ".pdf":
		return "application/pdf"
	}
	return "application/octet-stream"
}

func classifyContentType(mimeType string) string {
	switch {
	case mimeType == "text/plain" || mimeType == "text/csv":
		return "text"
	case strings.HasPrefix(mimeType, "text/x-") ||
		mimeType == "application/json" || mimeType == "application/xml" ||
		mimeType == "application/javascript" || mimeType == "application/yaml" ||
		mimeType == "application/x-yaml":
		return "code"
	case mimeType == "text/markdown" || mimeType == "text/html":
		return "document"
	case mimeType == "application/pdf":
		return "pdf"
	case strings.HasPrefix(mimeType, "image/"):
		return "image"
	case strings.HasPrefix(mimeType, "video/"):
		return "video"
	case strings.HasPrefix(mimeType, "audio/"):
		return "audio"
	default:
		return "binary"
	}
}

func isTextIndexable(contentType string) bool {
	return contentType == "text" || contentType == "code" || contentType == "document"
}

// PG operations

func (pg *PGClient) ListProjects(ctx context.Context, status string) ([]Project, error) {
	query := `
		SELECT p.id, p.name, p.description, p.status, p.source_path, p.tags,
		       p.created_at, p.updated_at,
		       p.owner_id, e.name, e.title, e.role,
		       (SELECT COUNT(*) FROM tasks t WHERE t.project_id = p.id) AS task_count
		FROM projects p
		LEFT JOIN employees e ON e.id = p.owner_id
	`
	var args []any
	if status != "" {
		query += " WHERE p.status = $1"
		args = append(args, status)
	}
	query += " ORDER BY p.updated_at DESC"

	rows, err := pg.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		var p Project
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
			p.Owner = &EmployeeBrief{ID: *ownerID, Name: *ownerName, Title: *ownerTitle, Role: *ownerRole}
		}
		if p.Tags == nil {
			p.Tags = []string{}
		}
		projects = append(projects, p)
	}
	if projects == nil {
		projects = []Project{}
	}
	return projects, nil
}

func (pg *PGClient) GetProject(ctx context.Context, id string) (*Project, error) {
	var p Project
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
		p.Owner = &EmployeeBrief{ID: *ownerID, Name: *ownerName, Title: *ownerTitle, Role: *ownerRole}
	}
	if p.Tags == nil {
		p.Tags = []string{}
	}
	return &p, nil
}

func (pg *PGClient) GetProjectByName(ctx context.Context, name string) (*Project, error) {
	var id string
	err := pg.pool.QueryRow(ctx, "SELECT id FROM projects WHERE name = $1", name).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("get project by name: %w", err)
	}
	return pg.GetProject(ctx, id)
}

func (pg *PGClient) CreateProject(ctx context.Context, p *Project, cfg *Config) error {
	isImport := p.SourcePath != nil
	if err := validateProjectName(p.Name, isImport); err != nil {
		return err
	}

	rootDir := p.RootDir(cfg)

	if isImport {
		if !filepath.IsAbs(*p.SourcePath) {
			return fmt.Errorf("source_path must be an absolute path")
		}
		info, err := os.Stat(*p.SourcePath)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("source_path %q does not exist or is not a directory", *p.SourcePath)
		}
	} else {
		if _, serr := os.Stat(rootDir); !os.IsNotExist(serr) {
			return fmt.Errorf("project directory already exists on disk: %s", rootDir)
		}
	}

	var ownerID *string
	if p.Owner != nil && p.Owner.ID != "" {
		ownerID = &p.Owner.ID
	}
	if p.Tags == nil {
		p.Tags = []string{}
	}

	err := pg.pool.QueryRow(ctx, `
		INSERT INTO projects (name, description, owner_id, status, source_path, tags)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, updated_at
	`, p.Name, p.Description, ownerID, "active", p.SourcePath, p.Tags).Scan(
		&p.ID, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert project: %w", err)
	}
	p.Status = "active"

	if isImport {
		gitignorePath := filepath.Join(rootDir, ".gitignore")
		appendGitignoreEntries(gitignorePath, []string{"mobius.md", ".conversations/"})
	} else {
		os.MkdirAll(rootDir, 0755)
		for _, dir := range cfg.Projects.TemplateDirs {
			os.MkdirAll(filepath.Join(rootDir, dir), 0755)
		}
	}

	os.MkdirAll(filepath.Join(rootDir, ".conversations"), 0755)

	mobiusPath := filepath.Join(rootDir, "mobius.md")
	if _, err := os.Stat(mobiusPath); os.IsNotExist(err) {
		content := fmt.Sprintf("# %s\n\n%s\n\n## Key Decisions\n\n## Architecture Notes\n\n## Open Questions\n", p.Name, p.Description)
		os.WriteFile(mobiusPath, []byte(content), 0644)
	}

	return nil
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

func (pg *PGClient) UpdateProject(ctx context.Context, id string, name, description, status *string) error {
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

	_, err := pg.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update project: %w", err)
	}
	return nil
}

func (pg *PGClient) DeleteProject(ctx context.Context, id string) error {
	_, err := pg.pool.Exec(ctx, "DELETE FROM projects WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	return nil
}

func (pg *PGClient) NullifyProjectTasks(ctx context.Context, projectID string) error {
	_, err := pg.pool.Exec(ctx,
		"UPDATE tasks SET project_id = NULL WHERE project_id = $1", projectID)
	return err
}

// Project Memory

func readProjectMemory(project *Project, cfg *Config) string {
	mu := getProjectLock(project.Name)
	mu.RLock()
	defer mu.RUnlock()

	path := filepath.Join(project.RootDir(cfg), "mobius.md")
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(content)
}

// HTTP handlers

func (h *APIHandler) ListProjects(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}
	status := r.URL.Query().Get("status")
	projects, err := h.pgClient.ListProjects(r.Context(), status)
	if err != nil {
		writeError(w, "failed to list projects: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if h.esClient != nil {
		for i := range projects {
			count, cerr := h.esClient.CountProjectAssets(r.Context(), projects[i].ID)
			if cerr == nil {
				projects[i].AssetCount = count
			}
		}
	}

	writeJSON(w, projects)
}

func (h *APIHandler) GetProjectHandler(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	p, err := h.pgClient.GetProject(r.Context(), id)
	if err != nil {
		writeError(w, "project not found", http.StatusNotFound)
		return
	}

	if h.esClient != nil {
		count, cerr := h.esClient.CountProjectAssets(r.Context(), p.ID)
		if cerr == nil {
			p.AssetCount = count
		}
	}

	writeJSON(w, p)
}

func (h *APIHandler) CreateProject(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}

	var body struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		OwnerID     string   `json:"owner_id"`
		Tags        []string `json:"tags"`
		SourcePath  *string  `json:"source_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.Name == "" {
		writeError(w, "name is required", http.StatusBadRequest)
		return
	}

	p := &Project{
		Name:        body.Name,
		Description: body.Description,
		SourcePath:  body.SourcePath,
		Tags:        body.Tags,
	}
	if body.OwnerID != "" {
		p.Owner = &EmployeeBrief{ID: body.OwnerID}
	}

	if err := h.pgClient.CreateProject(r.Context(), p, h.config); err != nil {
		writeError(w, "failed to create project: "+err.Error(), http.StatusBadRequest)
		return
	}

	full, err := h.pgClient.GetProject(r.Context(), p.ID)
	if err != nil {
		full = p
	}

	if h.esClient != nil {
		if err := h.esClient.IndexProject(r.Context(), full); err != nil {
			slog.Warn("ES index project failed", "id", full.ID, "error", err)
		}
	}

	slog.Info("project created", "id", full.ID, "name", full.Name,
		"imported", full.SourcePath != nil)
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, full)
}

func (h *APIHandler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}

	id := r.PathValue("id")
	current, err := h.pgClient.GetProject(r.Context(), id)
	if err != nil {
		writeError(w, "project not found", http.StatusNotFound)
		return
	}

	var body struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Status      *string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if body.Name != nil && *body.Name != current.Name {
		writeError(w, "project name is immutable once created", http.StatusBadRequest)
		return
	}

	if err := h.pgClient.UpdateProject(r.Context(), id, nil, body.Description, body.Status); err != nil {
		writeError(w, "failed to update project: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if h.esClient != nil {
		if updated, err := h.pgClient.GetProject(r.Context(), id); err == nil {
			if err := h.esClient.IndexProject(r.Context(), updated); err != nil {
				slog.Warn("ES index project failed", "id", id, "error", err)
			}
		}
	}

	p, _ := h.pgClient.GetProject(r.Context(), id)
	writeJSON(w, p)
}

func (h *APIHandler) ArchiveOrDeleteProject(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}

	projectID := r.PathValue("id")
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "archive"
	}
	if mode != "archive" && mode != "delete" {
		writeError(w, "mode must be 'archive' or 'delete'", http.StatusBadRequest)
		return
	}

	project, err := h.pgClient.GetProject(r.Context(), projectID)
	if err != nil {
		writeError(w, "project not found", http.StatusNotFound)
		return
	}

	destDir := filepath.Join(projectsBaseDir(h.config), mode+"d")
	os.MkdirAll(destDir, 0755)

	timestamp := time.Now().Format("20060102_150405")
	zipPath := filepath.Join(destDir, fmt.Sprintf("%s_%s.zip", project.Name, timestamp))

	if err := h.exportProjectToZip(r.Context(), project, zipPath); err != nil {
		slog.Error("project zip export failed", "project", project.Name, "error", err)
		writeError(w, "backup failed, aborting purge: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if h.esClient != nil {
		h.esClient.DeleteProjectAssets(r.Context(), projectID)
		if err := h.esClient.DeleteESProject(r.Context(), projectID); err != nil {
			slog.Warn("ES delete project failed", "id", projectID, "error", err)
		}
	}

	tx, txErr := h.pgClient.pool.Begin(r.Context())
	if txErr != nil {
		writeError(w, "failed to begin transaction: "+txErr.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(), "UPDATE tasks SET project_id = NULL WHERE project_id = $1", projectID); err != nil {
		writeError(w, "failed to nullify tasks: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec(r.Context(), "DELETE FROM projects WHERE id = $1", projectID); err != nil {
		writeError(w, "failed to delete project: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if txErr = tx.Commit(r.Context()); txErr != nil {
		writeError(w, "failed to purge project: "+txErr.Error(), http.StatusInternalServerError)
		return
	}

	projectMemoryLocks.Delete(project.Name)

	if h.gcsClient != nil {
		gcsPrefix := filepath.Join("projects", project.Name)
		h.gcsClient.DeletePrefix(r.Context(), gcsPrefix)
	}

	rootDir := project.RootDir(h.config)
	if _, err := os.Stat(rootDir); err == nil {
		if project.SourcePath == nil {
			os.RemoveAll(rootDir)
		} else {
			os.Remove(filepath.Join(rootDir, "mobius.md"))
			os.RemoveAll(filepath.Join(rootDir, "mobius.md.bak"))
			os.RemoveAll(filepath.Join(rootDir, ".conversations"))
		}
	}

	slog.Info("project purged", "id", projectID, "name", project.Name, "mode", mode, "archive", zipPath)
	writeJSON(w, map[string]string{"status": "ok", "archive_path": zipPath})
}

func (h *APIHandler) exportProjectToZip(ctx context.Context, project *Project, zipPath string) error {
	f, err := os.Create(zipPath)
	if err != nil {
		return fmt.Errorf("create zip: %w", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	projectJSON, err := json.MarshalIndent(project, "", "  ")
	if err != nil {
		return fmt.Errorf("zip: marshal project: %w", err)
	}
	pw, err := zw.Create("project.json")
	if err != nil {
		return fmt.Errorf("zip: create project.json: %w", err)
	}
	pw.Write(projectJSON)

	var tasks []Task
	if h.pgClient != nil {
		tasks, _ = h.pgClient.ListTasks(ctx, "", "", project.ID)
	}
	if len(tasks) > 0 {
		tasksJSON, _ := json.MarshalIndent(tasks, "", "  ")
		if w, err := zw.Create("tasks.json"); err != nil {
			slog.Error("zip export: failed to create tasks.json", "error", err)
		} else {
			w.Write(tasksJSON)
		}
	}

	if h.esClient != nil {
		assets, _ := h.esClient.ExportProjectAssets(ctx, project.ID)
		if len(assets) > 0 {
			assetsJSON, _ := json.MarshalIndent(assets, "", "  ")
			if w, err := zw.Create("assets_index.json"); err != nil {
				slog.Error("zip export: failed to create assets_index.json", "error", err)
			} else {
				w.Write(assetsJSON)
			}
		}
	}

	rootDir := project.RootDir(h.config)
	if _, err := os.Stat(rootDir); err == nil {
		filepath.Walk(rootDir, func(path string, info os.FileInfo, werr error) error {
			if werr != nil {
				return nil
			}
			if strings.HasPrefix(info.Name(), ".") && info.Name() != "." && info.Name() != ".conversations" {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if info.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(rootDir, path)
			w, zerr := zw.Create(filepath.Join("files", rel))
			if zerr != nil {
				slog.Error("zip export: failed to create file entry", "path", rel, "error", zerr)
				return nil
			}
			src, rerr := os.Open(path)
			if rerr != nil {
				return nil
			}
			defer src.Close()
			io.Copy(w, src)
			return nil
		})
	}

	return nil
}

// Project Memory HTTP handlers

func (h *APIHandler) GetProjectMemory(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	project, err := h.pgClient.GetProject(r.Context(), id)
	if err != nil {
		writeError(w, "project not found", http.StatusNotFound)
		return
	}
	content := readProjectMemory(project, h.config)
	writeJSON(w, map[string]string{"content": content})
}

func (h *APIHandler) UpdateProjectMemory(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	project, err := h.pgClient.GetProject(r.Context(), id)
	if err != nil {
		writeError(w, "project not found", http.StatusNotFound)
		return
	}

	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	mu := getProjectLock(project.Name)
	mu.Lock()
	defer mu.Unlock()

	mobiusPath := filepath.Join(project.RootDir(h.config), "mobius.md")
	if err := os.WriteFile(mobiusPath, []byte(body.Content), 0644); err != nil {
		writeError(w, "failed to write memory: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

// Project Asset HTTP handlers

func (h *APIHandler) ListProjectAssets(w http.ResponseWriter, r *http.Request) {
	if h.esClient == nil {
		writeError(w, "Elasticsearch not available", http.StatusServiceUnavailable)
		return
	}
	projectID := r.PathValue("id")
	query := r.URL.Query().Get("q")
	contentType := r.URL.Query().Get("type")

	assets, err := h.esClient.SearchProjectAssets(r.Context(), projectID, query, contentType, 100)
	if err != nil {
		writeError(w, "failed to search assets: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, assets)
}

func (h *APIHandler) UploadProjectAsset(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil || h.esClient == nil {
		writeError(w, "required services not available", http.StatusServiceUnavailable)
		return
	}

	projectID := r.PathValue("id")
	project, err := h.pgClient.GetProject(r.Context(), projectID)
	if err != nil {
		writeError(w, "project not found", http.StatusNotFound)
		return
	}

	maxBytes := h.config.MaxUploadBytes()
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+(10<<20))
	if err := r.ParseMultipartForm(maxBytes + (1 << 20)); err != nil {
		writeError(w, "file too large", http.StatusRequestEntityTooLarge)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, "file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	relativePath := r.FormValue("path")
	if relativePath == "" {
		relativePath = filepath.Base(header.Filename)
	}
	rootDir := project.RootDir(h.config)
	fullPath, err := resolveWithinRoot(rootDir, relativePath)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	os.MkdirAll(filepath.Dir(fullPath), 0755)

	dst, err := os.Create(fullPath)
	if err != nil {
		writeError(w, "failed to create file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := io.Copy(dst, file); err != nil {
		dst.Close()
		writeError(w, "failed to save file content: "+err.Error(), http.StatusInternalServerError)
		return
	}
	dst.Close()

	buf, err := os.ReadFile(fullPath)
	if err != nil {
		buf = []byte{}
	}

	mimeType := resolveMimeType(header.Filename, header.Header.Get("Content-Type"))
	ct := classifyContentType(mimeType)
	var content string
	var truncated bool
	if isTextIndexable(ct) && len(buf) > 0 {
		maxIdx := h.config.Projects.ContentMaxIndex
		if len(buf) > maxIdx {
			content = string(buf[:maxIdx])
			truncated = true
		} else {
			content = string(buf)
		}
	}

	now := time.Now().Format(time.RFC3339)
	asset := &ProjectAsset{
		ID:               generateID(),
		ProjectID:        projectID,
		Filename:         filepath.Base(relativePath),
		RelativePath:     relativePath,
		MIMEType:         mimeType,
		SizeBytes:        header.Size,
		Content:          content,
		ContentTruncated: truncated,
		ContentType:      ct,
		GCSStatus:        "pending",
		Checksum:         calculateSHA256(buf),
		Tags:             []string{},
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := h.esClient.IndexProjectAsset(r.Context(), asset); err != nil {
		writeError(w, "failed to index asset: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if h.gcsClient != nil {
		go uploadAssetToGCS(h.config, h.gcsClient, h.esClient, project, asset.ID, fullPath, relativePath)
	}

	if truncated && h.providers != nil {
		go summarizeAssetContent(h.config, h.providers, h.esClient, asset.ID, string(buf))
	}

	slog.Info("project asset uploaded", "project", project.Name, "path", relativePath)
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, asset)
}

func summarizeAssetContent(cfg *Config, providers *ProviderRegistry, es *ESClient, assetID string, fullContent string) {
	ctx := context.Background()
	modelID := resolveModelID(cfg, nil)
	provider := providers.ResolveProvider(modelID)
	if provider == nil {
		return
	}

	prompt := fmt.Sprintf(`Summarize this file content in under %d characters.
Focus on: purpose, key structures, important functions/sections, and notable patterns.

%s`, cfg.Projects.ContentSummaryMax, truncateForExtraction(fullContent, maxExtractionInputLen))

	req := &LLMRequest{
		Model:    modelID,
		Messages: []LLMMessage{{Role: "user", Text: prompt}},
		OnText:   func(string) {},
	}
	result, err := provider.ChatStream(ctx, req)
	if err != nil || result == "" {
		return
	}
	summary := strings.TrimSpace(result)
	if len(summary) > cfg.Projects.ContentSummaryMax {
		summary = summary[:cfg.Projects.ContentSummaryMax]
	}
	es.UpdateProjectAssetSummary(ctx, assetID, summary)
	slog.Info("asset content summarized", "asset_id", assetID, "summary_len", len(summary))
}

func uploadAssetToGCS(cfg *Config, gcs *GCSClient, es *ESClient, project *Project, assetID, localPath, relativePath string) {
	ctx := context.Background()
	pc := cfg.Projects
	gcsKey := filepath.Join("projects", project.Name, relativePath)

	f, err := os.Open(localPath)
	if err != nil {
		es.UpdateProjectAssetGCS(ctx, assetID, "", "failed")
		return
	}
	defer f.Close()

	mimeType := "application/octet-stream"

	for attempt := 0; attempt <= pc.GCSMaxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(pc.GCSBaseBackoff) * time.Millisecond * (1 << uint(attempt-1))
			time.Sleep(backoff)
			f.Seek(0, 0)
		}
		gcsURI, uerr := gcs.Upload(ctx, filepath.Dir(gcsKey), filepath.Base(gcsKey), "", f, mimeType)
		if uerr == nil {
			es.UpdateProjectAssetGCS(ctx, assetID, gcsURI, "synced")
			return
		}
		slog.Warn("gcs asset upload retry", "asset_id", assetID, "attempt", attempt, "error", uerr)
	}
	es.UpdateProjectAssetGCS(ctx, assetID, "", "failed")
	slog.Error("gcs asset upload failed permanently", "asset_id", assetID)
}

func (h *APIHandler) GetProjectAsset(w http.ResponseWriter, r *http.Request) {
	if h.esClient == nil {
		writeError(w, "Elasticsearch not available", http.StatusServiceUnavailable)
		return
	}
	assetID := r.PathValue("assetId")
	asset, err := h.esClient.GetProjectAsset(r.Context(), assetID)
	if err != nil {
		writeError(w, "asset not found", http.StatusNotFound)
		return
	}
	writeJSON(w, asset)
}

func (h *APIHandler) UpdateProjectAsset(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil || h.esClient == nil {
		writeError(w, "required services not available", http.StatusServiceUnavailable)
		return
	}
	projectID := r.PathValue("id")
	assetID := r.PathValue("assetId")

	project, err := h.pgClient.GetProject(r.Context(), projectID)
	if err != nil {
		writeError(w, "project not found", http.StatusNotFound)
		return
	}
	asset, err := h.esClient.GetProjectAsset(r.Context(), assetID)
	if err != nil {
		writeError(w, "asset not found", http.StatusNotFound)
		return
	}

	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	fullPath := filepath.Join(project.RootDir(h.config), asset.RelativePath)
	if err := os.WriteFile(fullPath, []byte(body.Content), 0644); err != nil {
		writeError(w, "failed to write file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	maxIdx := h.config.Projects.ContentMaxIndex
	indexContent := body.Content
	truncated := false
	if len(body.Content) > maxIdx {
		indexContent = body.Content[:maxIdx]
		truncated = true
	}

	asset.Content = indexContent
	asset.ContentTruncated = truncated
	asset.SizeBytes = int64(len(body.Content))
	asset.UpdatedAt = time.Now().Format(time.RFC3339)
	h.esClient.IndexProjectAsset(r.Context(), asset)

	slog.Info("project asset updated", "id", assetID)
	writeJSON(w, asset)
}

func (h *APIHandler) DeleteProjectAsset(w http.ResponseWriter, r *http.Request) {
	if h.esClient == nil {
		writeError(w, "Elasticsearch not available", http.StatusServiceUnavailable)
		return
	}
	assetID := r.PathValue("assetId")

	asset, err := h.esClient.GetProjectAsset(r.Context(), assetID)
	if err != nil {
		writeError(w, "asset not found", http.StatusNotFound)
		return
	}

	if err := h.esClient.DeleteProjectAsset(r.Context(), assetID); err != nil {
		writeError(w, "failed to delete asset: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if asset.GCSURI != "" && h.gcsClient != nil {
		h.gcsClient.Delete(r.Context(), asset.GCSURI)
	}

	if h.pgClient != nil {
		project, perr := h.pgClient.GetProject(r.Context(), asset.ProjectID)
		if perr == nil {
			localPath := filepath.Join(project.RootDir(h.config), asset.RelativePath)
			if err := os.Remove(localPath); err != nil && !os.IsNotExist(err) {
				slog.Error("failed to delete local asset file", "path", localPath, "error", err)
			}
		}
	}

	slog.Info("project asset deleted", "id", assetID)
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *APIHandler) ReindexProjectAssets(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil || h.esClient == nil {
		writeError(w, "required services not available", http.StatusServiceUnavailable)
		return
	}

	projectID := r.PathValue("id")
	project, err := h.pgClient.GetProject(r.Context(), projectID)
	if err != nil {
		writeError(w, "project not found", http.StatusNotFound)
		return
	}

	rootDir := project.RootDir(h.config)
	var indexed int

	filepath.Walk(rootDir, func(path string, info os.FileInfo, werr error) error {
		if werr != nil {
			return nil
		}
		if strings.HasPrefix(info.Name(), ".") && info.Name() != "." {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(rootDir, path)
		if rel == "mobius.md" || strings.HasPrefix(rel, "mobius.md.bak") {
			return nil
		}

		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}

		newChecksum := calculateSHA256(data)
		if existing, eerr := h.esClient.GetProjectAssetByPath(r.Context(), projectID, rel); eerr == nil && existing != nil {
			if existing.Checksum == newChecksum {
				return nil
			}
		}

		mimeType := resolveMimeType(info.Name(), "")
		ct := classifyContentType(mimeType)
		var content string
		var truncated bool
		if isTextIndexable(ct) {
			maxIdx := h.config.Projects.ContentMaxIndex
			if len(data) > maxIdx {
				content = string(data[:maxIdx])
				truncated = true
			} else {
				content = string(data)
			}
		}

		now := time.Now().Format(time.RFC3339)
		asset := &ProjectAsset{
			ID:               generateID(),
			ProjectID:        projectID,
			Filename:         info.Name(),
			RelativePath:     rel,
			MIMEType:         mimeType,
			SizeBytes:        info.Size(),
			Content:          content,
			ContentTruncated: truncated,
			ContentType:      ct,
			GCSStatus:        "pending",
			Checksum:         newChecksum,
			Tags:             []string{},
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if h.esClient.IndexProjectAsset(r.Context(), asset) == nil {
			indexed++
			if truncated && h.providers != nil {
				go summarizeAssetContent(h.config, h.providers, h.esClient, asset.ID, string(data))
			}
		}
		return nil
	})

	slog.Info("project assets reindexed", "project", project.Name, "indexed", indexed)
	writeJSON(w, map[string]any{"indexed": indexed})
}
