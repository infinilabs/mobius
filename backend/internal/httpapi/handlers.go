package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mobius/internal/bq"
	"mobius/internal/chat"
	"mobius/internal/config"
	"mobius/internal/domain"
	evbus "mobius/internal/events"
	"mobius/internal/gcs"
	"mobius/internal/llm"
	"mobius/internal/search"
	"mobius/internal/storage/postgres"
	"mobius/internal/tools"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/genai"
)

type APIHandler struct {
	config         *config.Config
	configPath     string
	conversations  *chat.ConversationStore
	genaiClient    *genai.Client
	esClient       *search.Client
	gcsClient      *gcs.Client
	pgClient       *postgres.Client
	providers      *llm.ProviderRegistry
	bqClient       *bq.Client
	tokenPipeline  *bq.TokenPipeline
	events         *evbus.EventPipeline
	health         *HealthChecker
	skillsDir      string
	syncSources    []SkillSyncSource
	lastSyncMu     sync.RWMutex
	lastSyncTime   time.Time
	lastSyncResult *SyncResult
}

func NewAPIHandler(cfg *config.Config, configPath string, genaiClient *genai.Client, esClient *search.Client, gcsClient *gcs.Client, pgClient *postgres.Client, bqClient *bq.Client, skillsDir string, providers *llm.ProviderRegistry, events *evbus.EventPipeline) *APIHandler {
	var tp *bq.TokenPipeline
	if bqClient != nil {
		tp = bq.NewTokenPipeline(bqClient)
	}
	h := &APIHandler{
		config:        cfg,
		configPath:    configPath,
		conversations: chat.NewConversationStore(),
		genaiClient:   genaiClient,
		esClient:      esClient,
		gcsClient:     gcsClient,
		pgClient:      pgClient,
		bqClient:      bqClient,
		tokenPipeline: tp,
		providers:     providers,
		events:        events,
		health:        NewHealthChecker(5 * time.Second),
		skillsDir:     skillsDir,
	}
	h.registerProbes()
	return h
}

func (h *APIHandler) registerProbes() {
	h.health.Register("postgres", func(ctx context.Context) ServiceStatus {
		if h.pgClient == nil {
			return StatusUnconfigured("Client not initialized")
		}
		if err := h.pgClient.Ping(ctx); err != nil {
			return StatusUnavailable(err.Error())
		}
		return StatusOK()
	})

	h.health.Register("elasticsearch", func(ctx context.Context) ServiceStatus {
		if h.esClient == nil {
			return StatusUnavailable("Client not initialized")
		}
		if err := h.esClient.Ping(ctx); err != nil {
			return StatusUnavailable(err.Error())
		}
		return StatusOK()
	})

	h.health.Register("bigquery", func(ctx context.Context) ServiceStatus {
		if h.bqClient == nil {
			return StatusUnconfigured("Not connected")
		}
		if err := h.bqClient.Ping(ctx); err != nil {
			return StatusUnavailable(err.Error())
		}
		return StatusOK()
	})

	h.health.Register("event_pipeline", func(ctx context.Context) ServiceStatus {
		if h.events == nil {
			return StatusUnconfigured("Not configured")
		}
		queueLen, queueCap := h.events.QueueStats()
		usage := float64(queueLen) / float64(queueCap) * 100
		if usage > 80 {
			return ServiceStatus{Status: "degraded", Error: fmt.Sprintf("queue %.0f%% full (%d/%d)", usage, queueLen, queueCap)}
		}
		return StatusOK()
	})

	h.health.Register("gcs", func(ctx context.Context) ServiceStatus {
		if h.gcsClient == nil {
			if h.config.GoogleCloud.GCS.Bucket == "" {
				return StatusUnconfigured("Bucket not configured")
			}
			return StatusUnavailable("Client not initialized")
		}
		if err := h.gcsClient.Ping(ctx); err != nil {
			return StatusUnavailable(err.Error())
		}
		return StatusOK()
	})

	h.health.Register("llm", func(ctx context.Context) ServiceStatus {
		if h.genaiClient == nil {
			return StatusUnavailable("GenAI client not initialized")
		}
		modelID, _ := h.config.GoogleCloud.VertexAI.DefaultLLM()
		if modelID == "" {
			return StatusUnconfigured("LLM model not set")
		}
		return StatusOK()
	})

	h.health.Register("img_model", func(ctx context.Context) ServiceStatus {
		for _, m := range h.config.GoogleCloud.VertexAI.GetModels() {
			if m.Type == "image" {
				return StatusOK()
			}
		}
		return StatusUnconfigured("Image model not set")
	})

	h.health.Register("video_model", func(ctx context.Context) ServiceStatus {
		for _, m := range h.config.GoogleCloud.VertexAI.GetModels() {
			if m.Type == "video" {
				return StatusOK()
			}
		}
		return StatusUnconfigured("Video model not set")
	})
}

func (h *APIHandler) Shutdown(ctx context.Context) {
	if h.esClient != nil {
		h.syncAllConversations(ctx)
		if err := h.esClient.Refresh(ctx, search.IdxConversations, search.IdxMessages); err != nil {
			slog.Error("ES refresh on shutdown failed", "error", err)
		}
	}

	if h.gcsClient != nil {
		if err := h.gcsClient.Close(); err != nil {
			slog.Error("GCS client close failed", "error", err)
		}
	}

	if h.pgClient != nil {
		h.pgClient.Close()
	}

	if h.bqClient != nil {
		if err := h.bqClient.Close(); err != nil {
			slog.Error("BQ client close failed", "error", err)
		}
	}

	slog.Info("APIHandler shutdown complete")
}

func (h *APIHandler) syncAllConversations(ctx context.Context) {
	all := h.conversations.All()
	synced := 0
	for _, c := range all {
		if err := h.esClient.IndexConversation(ctx, c); err != nil {
			slog.Error("shutdown sync conversation failed", "id", c.ID, "error", err)
			continue
		}
		synced++
	}
	slog.Info("shutdown conversation sync complete", "synced", synced, "total", len(all))
}

func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("failed to encode JSON response", "error", err)
	}
}

func writeError(w http.ResponseWriter, msg string, code int) {
	if code >= 500 {
		slog.Error("api error", "status", code, "error", msg)
	} else {
		slog.Warn("api error", "status", code, "error", msg)
	}
	http.Error(w, msg, code)
}

func (h *APIHandler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"services": h.health.RunAll(r.Context())})
}

func (h *APIHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	settings := h.config.GetSettings()
	writeJSON(w, map[string]string{
		"project_id": settings.GoogleCloud.ProjectID,
	})
}

func (h *APIHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.config.GetSettings())
}

func (h *APIHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var incoming config.SettingsData
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		writeError(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	h.config.ApplySettings(incoming)

	if err := config.Save(h.configPath, h.config); err != nil {
		slog.Error("failed to save config", "error", err)
		writeError(w, "failed to persist config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("settings updated and saved", "path", h.configPath)
	writeJSON(w, h.config.GetSettings())
}

func (h *APIHandler) ListModels(w http.ResponseWriter, r *http.Request) {
	settings := h.config.GetSettings()
	writeJSON(w, settings.GoogleCloud.VertexAI.GetModels())
}

func (h *APIHandler) AddModel(w http.ResponseWriter, r *http.Request) {
	var model config.VertexModel
	if err := json.NewDecoder(r.Body).Decode(&model); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if model.ID == "" || model.ModelID == "" || model.Type == "" {
		writeError(w, "id, model_id, and type are required", http.StatusBadRequest)
		return
	}
	if model.Name == "" {
		model.Name = model.ModelID
	}
	if model.Location == "" {
		model.Location = "global"
	}

	h.config.AddModel(model)

	if err := config.Save(h.configPath, h.config); err != nil {
		writeError(w, "failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("model registered", "id", model.ID, "type", model.Type)
	writeJSON(w, model)
}

func (h *APIHandler) RemoveModel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	found := h.config.RemoveModel(id)

	if !found {
		writeError(w, "model not found", http.StatusNotFound)
		return
	}

	if err := config.Save(h.configPath, h.config); err != nil {
		writeError(w, "failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("model removed", "id", id)
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *APIHandler) UploadFile(w http.ResponseWriter, r *http.Request) {
	maxBytes := h.config.MaxUploadBytes()
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+(10<<20))
	if err := r.ParseMultipartForm(maxBytes + (1 << 20)); err != nil {
		mb := h.config.Upload.MaxFileSizeMB
		if mb <= 0 {
			mb = 20
		}
		writeError(w, fmt.Sprintf("file too large: max %d MB", mb), http.StatusRequestEntityTooLarge)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, "file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	fileID := domain.NewID()
	ext := filepath.Ext(header.Filename)
	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	ref := domain.FileRef{
		ID:       fileID,
		Name:     header.Filename,
		MIMEType: mimeType,
		Size:     header.Size,
	}

	if h.gcsClient != nil {
		gcsURI, err := h.gcsClient.Upload(r.Context(), "chat/uploads", fileID, ext, file, mimeType)
		if err != nil {
			slog.Error("GCS upload failed", "error", err)
			writeError(w, "File upload failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		ref.GCSURI = gcsURI
	} else {
		os.MkdirAll(tools.UploadsDir, 0755)
		savePath := filepath.Join(tools.UploadsDir, fileID+ext)

		dst, err := os.Create(savePath)
		if err != nil {
			writeError(w, "failed to save file", http.StatusInternalServerError)
			return
		}
		defer dst.Close()

		if _, err := io.Copy(dst, file); err != nil {
			writeError(w, "failed to write file", http.StatusInternalServerError)
			return
		}
		slog.Warn("file saved locally (GCS not configured)", "path", savePath)
	}

	slog.Info("file uploaded", "id", fileID, "name", header.Filename, "size", header.Size, "gcs", ref.GCSURI != "")
	writeJSON(w, ref)
}

// domain.Employee Memory handlers

func (h *APIHandler) ListEmployeeMemories(w http.ResponseWriter, r *http.Request) {
	if h.esClient == nil {
		writeError(w, "Elasticsearch not available", http.StatusServiceUnavailable)
		return
	}

	empID := r.PathValue("id")
	query := r.URL.Query().Get("q")

	size := 100
	if query != "" {
		size = 20
	}

	memories, _, err := h.esClient.SearchEmployeeMemories(r.Context(), empID, query, size)
	if err != nil {
		writeError(w, "failed to search memories: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, memories)
}

func (h *APIHandler) AddEmployeeMemory(w http.ResponseWriter, r *http.Request) {
	if h.esClient == nil {
		writeError(w, "Elasticsearch not available", http.StatusServiceUnavailable)
		return
	}

	empID := r.PathValue("id")
	var body struct {
		MemoryText     string `json:"memory_text"`
		ConversationID string `json:"conversation_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.MemoryText == "" {
		writeError(w, "memory_text is required", http.StatusBadRequest)
		return
	}

	if err := h.esClient.IndexEmployeeMemoryDedup(r.Context(), empID, body.ConversationID, body.MemoryText); err != nil {
		writeError(w, "failed to store memory: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("memory added via API", "employee_id", empID)
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]string{"status": "remembered", "memory_text": body.MemoryText})
}

func (h *APIHandler) DeleteEmployeeMemory(w http.ResponseWriter, r *http.Request) {
	if h.esClient == nil {
		writeError(w, "Elasticsearch not available", http.StatusServiceUnavailable)
		return
	}

	memoryID := r.PathValue("memoryId")
	if err := h.esClient.DeleteEmployeeMemory(r.Context(), memoryID); err != nil {
		writeError(w, "failed to delete memory: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("memory deleted via API", "memory_id", memoryID)
	writeJSON(w, map[string]string{"status": "forgotten"})
}

func (h *APIHandler) BrowseDirectories(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("path")
	if dir == "" {
		var err error
		dir, err = os.UserHomeDir()
		if err != nil {
			writeError(w, "cannot determine home directory", http.StatusInternalServerError)
			return
		}
	}

	dir = filepath.Clean(dir)
	if !filepath.IsAbs(dir) {
		writeError(w, "path must be absolute", http.StatusBadRequest)
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		writeError(w, "cannot read directory: "+err.Error(), http.StatusBadRequest)
		return
	}

	type dirEntry struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	dirs := make([]dirEntry, 0)
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dirs = append(dirs, dirEntry{
			Name: e.Name(),
			Path: filepath.Join(dir, e.Name()),
		})
	}

	writeJSON(w, map[string]any{
		"current": dir,
		"parent":  filepath.Dir(dir),
		"dirs":    dirs,
	})
}

func (h *APIHandler) Search(w http.ResponseWriter, r *http.Request) {
	if h.esClient == nil {
		writeError(w, "Elasticsearch not available", http.StatusServiceUnavailable)
		return
	}
	typ := r.URL.Query().Get("type")
	q := r.URL.Query().Get("q")
	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 50 {
			limit = n
		}
	}

	var results []search.SearchResult
	var err error
	switch typ {
	case "employees":
		results, err = h.esClient.SearchEmployees(r.Context(), q, limit)
	case "projects":
		results, err = h.esClient.SearchProjects(r.Context(), q, limit)
	case "tasks":
		results, err = h.esClient.SearchTasks(r.Context(), q, limit)
	default:
		writeError(w, "type must be employees, projects, or tasks", http.StatusBadRequest)
		return
	}
	if err != nil {
		writeError(w, "search failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, results)
}

// Accessors for main's wiring (plan 6.4f): startup registers skill-sync
// sources, hydrates conversations, and drives the token pipeline lifecycle.

func (h *APIHandler) AddSyncSource(src SkillSyncSource) {
	h.syncSources = append(h.syncSources, src)
}

func (h *APIHandler) SyncSources() []SkillSyncSource { return h.syncSources }

func (h *APIHandler) Conversations() *chat.ConversationStore { return h.conversations }

func (h *APIHandler) TokenPipeline() *bq.TokenPipeline { return h.tokenPipeline }
