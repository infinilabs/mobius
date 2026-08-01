package httpapi

import (
	"encoding/json"
	"log/slog"
	"mobius/internal/chat"
	"mobius/internal/domain"
	"net/http"
	"os"
)

// domain.Message, domain.FileRef, domain.Conversation, domain.ConversationSummary live in
// internal/domain (plan 6.1/6.2); see domain_aliases.go.

// ConversationStore lives in internal/chat (plan 6.4e); see chat_shim.go.

// PG conversation metadata

// HTTP handlers

func (h *APIHandler) ListConversations(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.conversations.List())
}

func (h *APIHandler) GetConversation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c := h.conversations.Get(id)
	if c == nil {
		writeError(w, "conversation not found", http.StatusNotFound)
		return
	}
	writeJSON(w, c)
}

func (h *APIHandler) CreateConversation(w http.ResponseWriter, r *http.Request) {
	c := h.conversations.Create()
	slog.Info("conversation created", "id", c.ID)

	if h.esClient != nil {
		if err := h.esClient.IndexConversation(r.Context(), c); err != nil {
			slog.Error("ES index conversation failed", "id", c.ID, "error", err)
		}
	}
	if h.pgClient != nil {
		if err := h.pgClient.UpsertConversationMeta(r.Context(), c); err != nil {
			slog.Error("PG upsert conversation failed", "id", c.ID, "error", err)
		}
	}
	if err := chat.SaveConversation(h.config, c, nil); err != nil {
		slog.Error("disk save conversation failed", "id", c.ID, "error", err)
	}

	writeJSON(w, c)
}

func (h *APIHandler) RenameConversation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Title == "" {
		writeError(w, "title is required", http.StatusBadRequest)
		return
	}
	if !h.conversations.Rename(id, body.Title) {
		writeError(w, "conversation not found", http.StatusNotFound)
		return
	}

	if h.esClient != nil {
		if err := h.esClient.UpdateConversationTitle(r.Context(), id, body.Title); err != nil {
			slog.Error("ES rename conversation failed", "id", id, "error", err)
		}
	}

	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *APIHandler) TruncateConversation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		KeepCount int `json:"keep_count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if !h.conversations.TruncateAt(id, body.KeepCount) {
		writeError(w, "conversation not found", http.StatusNotFound)
		return
	}

	c := h.conversations.Get(id)

	if h.esClient != nil {
		if err := h.esClient.DeleteMessagesBeyond(r.Context(), id, body.KeepCount); err != nil {
			slog.Error("ES truncate messages failed", "id", id, "error", err)
		}
		if err := h.esClient.IndexConversation(r.Context(), c); err != nil {
			slog.Error("ES update conversation after truncate failed", "id", id, "error", err)
		}
	}

	slog.Info("conversation truncated", "id", id, "keep_count", body.KeepCount)
	writeJSON(w, c)
}

func (h *APIHandler) DeleteConversation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	conv := h.conversations.Get(id)
	if conv == nil {
		writeError(w, "conversation not found", http.StatusNotFound)
		return
	}

	var files []domain.FileRef
	for _, msg := range conv.Messages {
		files = append(files, msg.Files...)
	}

	if !h.conversations.Delete(id) {
		writeError(w, "conversation not found", http.StatusNotFound)
		return
	}

	if h.esClient != nil {
		if err := h.esClient.DeleteConversation(r.Context(), id); err != nil {
			slog.Error("ES delete conversation failed", "id", id, "error", err)
		}
	}

	if h.pgClient != nil {
		if err := h.pgClient.DeleteConversationMeta(r.Context(), id); err != nil {
			slog.Error("PG delete conversation failed", "id", id, "error", err)
		}
	}

	diskPath := chat.ConversationFilePath(h.config, id, nil)
	if err := os.Remove(diskPath); err != nil && !os.IsNotExist(err) {
		slog.Error("disk delete conversation failed", "id", id, "error", err)
	}

	for _, f := range files {
		if f.GCSURI != "" && h.gcsClient != nil {
			if err := h.gcsClient.Delete(r.Context(), f.GCSURI); err != nil {
				slog.Error("GCS delete file failed", "id", f.ID, "uri", f.GCSURI, "error", err)
			}
		}
	}

	slog.Info("conversation deleted", "id", id, "files_cleaned", len(files))
	writeJSON(w, map[string]string{"status": "ok"})
}
