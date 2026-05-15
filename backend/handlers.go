package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"google.golang.org/genai"
)

type APIHandler struct {
	config        *Config
	configPath    string
	cache         *Cache
	conversations *ConversationStore
	genaiClient   *genai.Client
	esClient      *ESClient
	gcsClient     *GCSClient
	health        *HealthChecker
}

func NewAPIHandler(cfg *Config, configPath string, genaiClient *genai.Client, esClient *ESClient, gcsClient *GCSClient) *APIHandler {
	h := &APIHandler{
		config:        cfg,
		configPath:    configPath,
		cache:         NewCache(10 * time.Minute),
		conversations: NewConversationStore(),
		genaiClient:   genaiClient,
		esClient:      esClient,
		gcsClient:     gcsClient,
		health:        NewHealthChecker(5 * time.Second),
	}
	h.registerProbes()
	return h
}

func (h *APIHandler) registerProbes() {
	h.health.Register("postgres", func(ctx context.Context) ServiceStatus {
		return StatusUnconfigured("Not connected yet")
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
		return StatusUnconfigured("Not connected yet")
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
			return StatusUnavailable("Vertex AI client not initialized")
		}
		if h.config.GoogleCloud.VertexAI.LLMModelID == "" {
			return StatusUnconfigured("LLM model not set")
		}
		return StatusOK()
	})

	h.health.Register("img_model", func(ctx context.Context) ServiceStatus {
		if h.config.GoogleCloud.VertexAI.ImgModelID == "" {
			return StatusUnconfigured("Image model not set")
		}
		return StatusOK()
	})

	h.health.Register("video_model", func(ctx context.Context) ServiceStatus {
		if h.config.GoogleCloud.VertexAI.VideoModelID == "" {
			return StatusUnconfigured("Video model not set")
		}
		return StatusOK()
	})
}

func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, msg string, code int) {
	slog.Error("api error", "status", code, "error", msg)
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
	var incoming SettingsData
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		writeError(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	h.config.ApplySettings(incoming)

	if err := SaveConfig(h.configPath, h.config); err != nil {
		slog.Error("failed to save config", "error", err)
		writeError(w, "failed to persist config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("settings updated and saved", "path", h.configPath)
	writeJSON(w, h.config.GetSettings())
}

func (h *APIHandler) UploadFile(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(32 << 20)

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, "file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	fileID := generateID()
	ext := filepath.Ext(header.Filename)
	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	ref := FileRef{
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
		uploadDir := "tmp/uploads"
		os.MkdirAll(uploadDir, 0755)
		savePath := filepath.Join(uploadDir, fileID+ext)

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

