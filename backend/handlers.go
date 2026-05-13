package main

import (
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
}

func NewAPIHandler(cfg *Config, configPath string, genaiClient *genai.Client, esClient *ESClient) *APIHandler {
	return &APIHandler{
		config:        cfg,
		configPath:    configPath,
		cache:         NewCache(10 * time.Minute),
		conversations: NewConversationStore(),
		genaiClient:   genaiClient,
		esClient:      esClient,
	}
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
	writeJSON(w, map[string]string{"status": "ok"})
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

	uploadDir := "tmp/uploads"
	os.MkdirAll(uploadDir, 0755)

	fileID := generateID()
	ext := filepath.Ext(header.Filename)
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

	slog.Info("file uploaded", "id", fileID, "name", header.Filename, "size", header.Size)
	writeJSON(w, map[string]string{
		"id":   fileID,
		"name": header.Filename,
		"path": savePath,
	})
}

