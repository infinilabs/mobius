package httpapi

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mobius/internal/config"
	"mobius/internal/domain"
	"mobius/internal/llm"
	"mobius/internal/memory"
	"mobius/internal/search"
	"mobius/internal/tools"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// domain.Project, domain.ProjectAsset, domain.CreateProjectInput live in internal/domain
// (plan 6.1); see domain_aliases.go.

func projectsBaseDir(cfg *config.Config) string {
	return cfg.Projects.ProjectsDir
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

	full, err := h.pgClient.CreateProject(r.Context(), domain.CreateProjectInput{
		Name:        body.Name,
		Description: body.Description,
		OwnerID:     body.OwnerID,
		SourcePath:  body.SourcePath,
		Tags:        body.Tags,
	}, h.config.Projects.ProjectsDir, h.config.Projects.TemplateDirs)
	if err != nil {
		writeError(w, "failed to create project: "+err.Error(), http.StatusBadRequest)
		return
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

	// pg.UpdateProject re-mirrors the row into ES centrally (see reindexProject).
	if err := h.pgClient.UpdateProject(r.Context(), id, nil, body.Description, body.Status); err != nil {
		writeError(w, "failed to update project: "+err.Error(), http.StatusInternalServerError)
		return
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

	destDir := filepath.Join(h.config.Projects.ProjectsDir, mode+"d")
	os.MkdirAll(destDir, 0755)

	timestamp := time.Now().Format("20060102_150405")
	zipPath := filepath.Join(destDir, fmt.Sprintf("%s_%s.zip", project.Name, timestamp))

	if err := h.exportProjectToZip(r.Context(), project, zipPath); err != nil {
		slog.Error("project zip export failed", "project", project.Name, "error", err)
		writeError(w, "backup failed, aborting purge: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Capture tasks still pointing at this project so we can re-mirror them into
	// ES after their project_id is nullified below.
	var affectedTaskIDs []string
	if rows, qerr := h.pgClient.Pool().Query(r.Context(), "SELECT id FROM tasks WHERE project_id = $1", projectID); qerr == nil {
		for rows.Next() {
			var tid string
			if rows.Scan(&tid) == nil {
				affectedTaskIDs = append(affectedTaskIDs, tid)
			}
		}
		rows.Close()
	}

	tx, txErr := h.pgClient.Pool().Begin(r.Context())
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

	tools.ForgetProjectLock(project.Name)

	// Remove the project (and its ES-only asset docs) from the search index after
	// the row is durably gone. Best-effort, like reindexProject on the write paths.
	if h.esClient != nil {
		if err := h.esClient.DeleteESProject(r.Context(), projectID); err != nil {
			slog.Warn("ES delete project failed", "id", projectID, "error", err)
		}
		h.esClient.DeleteProjectAssets(r.Context(), projectID)
	}

	for _, tid := range affectedTaskIDs {
		h.pgClient.ReindexTask(r.Context(), tid)
	}

	if h.gcsClient != nil {
		gcsPrefix := filepath.Join("projects", project.Name)
		h.gcsClient.DeletePrefix(r.Context(), gcsPrefix)
	}

	rootDir := project.RootDir(h.config.Projects.ProjectsDir)
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

func (h *APIHandler) exportProjectToZip(ctx context.Context, project *domain.Project, zipPath string) error {
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

	var tasks []domain.Task
	if h.pgClient != nil {
		tasks, _ = h.pgClient.ListTasks(ctx, "", "", project.ID, "")
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

	rootDir := project.RootDir(h.config.Projects.ProjectsDir)
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

// domain.Project Memory HTTP handlers

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
	content := tools.ReadProjectMemory(project, h.config)
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

	mu := tools.ProjectLock(project.Name)
	mu.Lock()
	defer mu.Unlock()

	mobiusPath := filepath.Join(project.RootDir(h.config.Projects.ProjectsDir), "mobius.md")
	if err := os.WriteFile(mobiusPath, []byte(body.Content), 0644); err != nil {
		writeError(w, "failed to write memory: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

// domain.Project Asset HTTP handlers

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

// ListCreatives serves the global Creatives library across all projects, optionally
// filtered by tag (e.g. "playable") or content type. Backs the Creatives UI page.
func (h *APIHandler) ListCreatives(w http.ResponseWriter, r *http.Request) {
	if h.esClient == nil {
		writeError(w, "Elasticsearch not available", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	filters := search.CreativeFilters{
		Query:       q.Get("q"),
		ContentType: q.Get("type"),
		Tag:         q.Get("tag"),
		Origin:      q.Get("origin"),
		AspectRatio: q.Get("aspect_ratio"),
		Status:      q.Get("status"),
		DateFrom:    q.Get("date_from"),
		DateTo:      q.Get("date_to"),
	}

	assets, err := h.esClient.SearchCreatives(r.Context(), filters, 200)
	if err != nil {
		writeError(w, "failed to search creatives: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, assets)
}

// ListCreativeTags returns the distinct tags across creatives for the quick-filter chips.
func (h *APIHandler) ListCreativeTags(w http.ResponseWriter, r *http.Request) {
	if h.esClient == nil {
		writeError(w, "Elasticsearch not available", http.StatusServiceUnavailable)
		return
	}
	tags, err := h.esClient.SearchCreativeTags(r.Context(), 100)
	if err != nil {
		writeError(w, "failed to list creative tags: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, tags)
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
	rootDir := project.RootDir(h.config.Projects.ProjectsDir)
	fullPath, err := domain.ResolveWithinRoot(rootDir, relativePath)
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

	mimeType := domain.ResolveMimeType(header.Filename, header.Header.Get("Content-Type"))
	ct := domain.ClassifyContentType(mimeType)
	var content string
	var truncated bool
	if domain.IsTextIndexable(ct) && len(buf) > 0 {
		maxIdx := h.config.Projects.ContentMaxIndex
		if len(buf) > maxIdx {
			content = string(buf[:maxIdx])
			truncated = true
		} else {
			content = string(buf)
		}
	}

	now := time.Now().Format(time.RFC3339)
	asset := &domain.ProjectAsset{
		ID:               domain.NewID(),
		ProjectID:        projectID,
		Filename:         filepath.Base(relativePath),
		RelativePath:     relativePath,
		AbsolutePath:     fullPath,
		MIMEType:         mimeType,
		SizeBytes:        header.Size,
		Content:          content,
		ContentTruncated: truncated,
		ContentType:      ct,
		GCSStatus:        "pending",
		Checksum:         domain.CalculateSHA256(buf),
		Tags:             []string{},
		Title:            filepath.Base(relativePath),
		Status:           "draft",
		Origin:           "local",
		AspectRatio:      domain.ComputeAspectRatio(fullPath, ct),
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := h.esClient.IndexProjectAsset(r.Context(), asset); err != nil {
		writeError(w, "failed to index asset: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if h.gcsClient != nil {
		go tools.UploadAssetToGCS(h.config, h.gcsClient, h.esClient, project, asset.ID, fullPath, relativePath)
	}

	if truncated && h.providers != nil {
		go summarizeAssetContent(h.config, h.providers, h.esClient, asset.ID, string(buf))
	}

	slog.Info("project asset uploaded", "project", project.Name, "path", relativePath)
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, asset)
}

// creativesLibraryName is the reserved project that holds creatives uploaded
// directly from a local computer (the "Add → Local Computer" flow). It is hidden
// from ListProjects and exists only so every creative still has a valid
// project_id for storage.
const creativesLibraryName = "__creatives__"

// getOrCreateCreativesProject returns the reserved creatives-library project,
// creating it on first use. It bypasses CreateProject (whose name validation
// rejects the sentinel name) and inserts the row directly.
func (h *APIHandler) getOrCreateCreativesProject(ctx context.Context) (*domain.Project, error) {
	if p, err := h.pgClient.GetProjectByName(ctx, creativesLibraryName); err == nil {
		return p, nil
	}
	var id string
	if err := h.pgClient.Pool().QueryRow(ctx, `
		INSERT INTO projects (name, description, owner_id, status, source_path, tags)
		VALUES ($1, $2, NULL, 'active', NULL, '{}')
		RETURNING id
	`, creativesLibraryName, "Reserved library for directly-uploaded creatives").Scan(&id); err != nil {
		return nil, fmt.Errorf("create creatives library: %w", err)
	}
	p, err := h.pgClient.GetProject(ctx, id)
	if err != nil {
		return nil, err
	}
	os.MkdirAll(p.RootDir(h.config.Projects.ProjectsDir), 0755)
	return p, nil
}

// UploadCreative handles a direct creative upload from a local computer. The file
// is stored under the reserved creatives library, persisted to GCS, tagged
// "creative", and indexed — combining UploadProjectAsset + AddAssetToCreatives.
func (h *APIHandler) UploadCreative(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil || h.esClient == nil {
		writeError(w, "required services not available", http.StatusServiceUnavailable)
		return
	}
	if h.gcsClient == nil {
		writeError(w, "GCS not available; cannot publish creative", http.StatusServiceUnavailable)
		return
	}

	project, err := h.getOrCreateCreativesProject(r.Context())
	if err != nil {
		writeError(w, "failed to prepare creatives library: "+err.Error(), http.StatusInternalServerError)
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

	relativePath := filepath.Base(header.Filename)
	fullPath, err := domain.ResolveWithinRoot(project.RootDir(h.config.Projects.ProjectsDir), relativePath)
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

	buf, _ := os.ReadFile(fullPath)
	mimeType := domain.ResolveMimeType(header.Filename, header.Header.Get("Content-Type"))
	ct := domain.ClassifyContentType(mimeType)

	now := time.Now().Format(time.RFC3339)
	asset := &domain.ProjectAsset{
		ID:           domain.NewID(),
		ProjectID:    project.ID,
		Filename:     relativePath,
		RelativePath: relativePath,
		AbsolutePath: fullPath,
		MIMEType:     mimeType,
		SizeBytes:    header.Size,
		ContentType:  ct,
		Checksum:     domain.CalculateSHA256(buf),
		Tags:         []string{"creative"},
		Title:        relativePath,
		Status:       "draft",
		Origin:       "local",
		AspectRatio:  domain.ComputeAspectRatio(fullPath, ct),
		PublishedAt:  now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	f, ferr := os.Open(fullPath)
	if ferr != nil {
		writeError(w, "asset file not found after write", http.StatusInternalServerError)
		return
	}
	gcsURI, uerr := h.gcsClient.Upload(r.Context(), "creatives", asset.ID, filepath.Ext(asset.Filename), f, asset.MIMEType)
	f.Close()
	if uerr != nil {
		writeError(w, "failed to persist creative to GCS: "+uerr.Error(), http.StatusInternalServerError)
		return
	}
	asset.GCSURI = gcsURI
	asset.GCSStatus = "synced"

	if err := h.esClient.IndexProjectAsset(r.Context(), asset); err != nil {
		writeError(w, "failed to index creative: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("creative uploaded", "asset_id", asset.ID, "gcs", asset.GCSURI)
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, asset)
}

func summarizeAssetContent(cfg *config.Config, providers *llm.ProviderRegistry, es *search.Client, assetID string, fullContent string) {
	ctx := context.Background()
	modelID := memory.ResolveModelID(cfg, nil)
	provider := providers.ResolveProvider(modelID)
	if provider == nil {
		return
	}

	prompt := fmt.Sprintf(`Summarize this file content in under %d characters.
Focus on: purpose, key structures, important functions/sections, and notable patterns.

%s`, cfg.Projects.ContentSummaryMax, memory.TruncateForExtraction(fullContent, memory.MaxExtractionInputLen))

	req := &llm.LLMRequest{
		Model:    modelID,
		Messages: []llm.LLMMessage{{Role: "user", Text: prompt}},
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

// GetProjectAssetContent streams the raw bytes of an asset from disk so the UI can
// preview images and render playable HTML in an iframe. GetProjectAsset returns only
// JSON metadata; this serves the actual file content with its stored MIME type.
func (h *APIHandler) GetProjectAssetContent(w http.ResponseWriter, r *http.Request) {
	if h.esClient == nil || h.pgClient == nil {
		writeError(w, "required services not available", http.StatusServiceUnavailable)
		return
	}
	projectID := r.PathValue("id")
	assetID := r.PathValue("assetId")

	asset, err := h.esClient.GetProjectAsset(r.Context(), assetID)
	if err != nil || asset.ProjectID != projectID {
		writeError(w, "asset not found", http.StatusNotFound)
		return
	}

	project, err := h.pgClient.GetProject(r.Context(), projectID)
	if err != nil {
		writeError(w, "project not found", http.StatusNotFound)
		return
	}

	fullPath, err := domain.ResolveWithinRoot(project.RootDir(h.config.Projects.ProjectsDir), asset.RelativePath)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}

	f, err := os.Open(fullPath)
	if err != nil {
		// Local file gone (e.g. archived project) — fall back to the durable GCS
		// copy for published creatives. Serve local-first for speed, GCS for durability.
		if h.serveAssetFromGCS(w, r, asset) {
			return
		}
		writeError(w, "asset file not found", http.StatusNotFound)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		writeError(w, "failed to stat asset", http.StatusInternalServerError)
		return
	}

	if asset.MIMEType != "" {
		w.Header().Set("Content-Type", asset.MIMEType)
	}
	http.ServeContent(w, r, asset.Filename, info.ModTime(), f)
}

// serveAssetFromGCS streams a creative's durable GCS copy by downloading it to a
// temp file (GCS readers aren't seekable, http.ServeContent needs a ReadSeeker).
// Returns false if no GCS copy exists or the fetch fails.
func (h *APIHandler) serveAssetFromGCS(w http.ResponseWriter, r *http.Request, asset *domain.ProjectAsset) bool {
	if asset.GCSURI == "" || h.gcsClient == nil {
		return false
	}
	tmp, err := os.CreateTemp("", "creative-*"+filepath.Ext(asset.Filename))
	if err != nil {
		return false
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	if err := h.gcsClient.DownloadURI(r.Context(), asset.GCSURI, tmpPath); err != nil {
		slog.Warn("creative GCS fallback download failed", "asset_id", asset.ID, "uri", asset.GCSURI, "error", err)
		return false
	}
	f, err := os.Open(tmpPath)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return false
	}
	if asset.MIMEType != "" {
		w.Header().Set("Content-Type", asset.MIMEType)
	}
	http.ServeContent(w, r, asset.Filename, info.ModTime(), f)
	return true
}

// AddAssetToCreatives promotes a project asset into the global Creatives library:
// persists the bytes to GCS under a unique hash id (the asset ID) so the creative
// is durable independent of local files, tags it "creative", sets status=draft +
// published_at, and reindexes. Idempotent.
func (h *APIHandler) AddAssetToCreatives(w http.ResponseWriter, r *http.Request) {
	if h.esClient == nil || h.pgClient == nil {
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
	if err != nil || asset.ProjectID != projectID {
		writeError(w, "asset not found", http.StatusNotFound)
		return
	}

	// Persist to GCS synchronously under a unique hash-id object name (asset.ID)
	// so the creative survives local project cleanup.
	if asset.GCSStatus != "synced" {
		if h.gcsClient == nil {
			writeError(w, "GCS not available; cannot publish creative", http.StatusServiceUnavailable)
			return
		}
		fullPath, perr := domain.ResolveWithinRoot(project.RootDir(h.config.Projects.ProjectsDir), asset.RelativePath)
		if perr != nil {
			writeError(w, perr.Error(), http.StatusBadRequest)
			return
		}
		f, ferr := os.Open(fullPath)
		if ferr != nil {
			writeError(w, "asset file not found", http.StatusNotFound)
			return
		}
		gcsURI, uerr := h.gcsClient.Upload(r.Context(), "creatives", asset.ID, filepath.Ext(asset.Filename), f, asset.MIMEType)
		f.Close()
		if uerr != nil {
			writeError(w, "failed to persist creative to GCS: "+uerr.Error(), http.StatusInternalServerError)
			return
		}
		asset.GCSURI = gcsURI
		asset.GCSStatus = "synced"
	}

	hasCreative := false
	for _, t := range asset.Tags {
		if t == "creative" {
			hasCreative = true
			break
		}
	}
	if !hasCreative {
		asset.Tags = append(asset.Tags, "creative")
	}
	if asset.Status == "" {
		asset.Status = "draft"
	}
	if asset.Title == "" {
		asset.Title = asset.Filename
	}
	if asset.PublishedAt == "" {
		asset.PublishedAt = time.Now().Format(time.RFC3339)
	}
	asset.UpdatedAt = time.Now().Format(time.RFC3339)

	if err := h.esClient.IndexProjectAsset(r.Context(), asset); err != nil {
		writeError(w, "failed to index creative: "+err.Error(), http.StatusInternalServerError)
		return
	}
	slog.Info("asset added to creatives", "asset_id", assetID, "gcs", asset.GCSURI)
	writeJSON(w, asset)
}

// UpdateCreativeMeta updates asset/creative metadata (title, description, status,
// tags) without touching file content. Distinct from UpdateProjectAsset (content PUT).
func (h *APIHandler) UpdateCreativeMeta(w http.ResponseWriter, r *http.Request) {
	if h.esClient == nil {
		writeError(w, "Elasticsearch not available", http.StatusServiceUnavailable)
		return
	}
	projectID := r.PathValue("id")
	assetID := r.PathValue("assetId")

	asset, err := h.esClient.GetProjectAsset(r.Context(), assetID)
	if err != nil || asset.ProjectID != projectID {
		writeError(w, "asset not found", http.StatusNotFound)
		return
	}

	var body struct {
		Title       *string   `json:"title"`
		Description *string   `json:"description"`
		Status      *string   `json:"status"`
		Tags        *[]string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.Title != nil {
		asset.Title = *body.Title
	}
	if body.Description != nil {
		asset.Description = *body.Description
	}
	if body.Status != nil {
		asset.Status = *body.Status
	}
	if body.Tags != nil {
		asset.Tags = *body.Tags
	}
	asset.UpdatedAt = time.Now().Format(time.RFC3339)

	if err := h.esClient.IndexProjectAsset(r.Context(), asset); err != nil {
		writeError(w, "failed to update creative: "+err.Error(), http.StatusInternalServerError)
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

	fullPath := filepath.Join(project.RootDir(h.config.Projects.ProjectsDir), asset.RelativePath)
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
			localPath := filepath.Join(project.RootDir(h.config.Projects.ProjectsDir), asset.RelativePath)
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

	rootDir := project.RootDir(h.config.Projects.ProjectsDir)

	// Rebuild from disk (the source of truth): drop stale/duplicate docs first so
	// the index exactly mirrors the project folder on every sync.
	if err := h.esClient.DeleteProjectAssets(r.Context(), projectID); err != nil {
		slog.Warn("reindex: failed to clear existing assets", "project", project.Name, "error", err)
	}
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

		newChecksum := domain.CalculateSHA256(data)

		mimeType := domain.ResolveMimeType(info.Name(), "")
		ct := domain.ClassifyContentType(mimeType)
		var content string
		var truncated bool
		if domain.IsTextIndexable(ct) {
			maxIdx := h.config.Projects.ContentMaxIndex
			if len(data) > maxIdx {
				content = string(data[:maxIdx])
				truncated = true
			} else {
				content = string(data)
			}
		}

		now := time.Now().Format(time.RFC3339)
		asset := &domain.ProjectAsset{
			ID:               domain.NewID(),
			ProjectID:        projectID,
			Filename:         info.Name(),
			RelativePath:     rel,
			AbsolutePath:     path,
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
