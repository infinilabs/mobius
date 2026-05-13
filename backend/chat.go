package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"google.golang.org/genai"
)

type ChatRequest struct {
	ConversationID string   `json:"conversation_id"`
	Message        string   `json:"message"`
	Files          []string `json:"files,omitempty"`
}

func NewGenAIClient(ctx context.Context, cfg *Config) (*genai.Client, error) {
	settings := cfg.GetSettings()
	gc := settings.GoogleCloud

	location := gc.VertexAI.LLMLocation
	if location == "" {
		location = "global"
	}

	if gc.CredentialsPath != "" {
		os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", gc.CredentialsPath)
	}

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		Project:  gc.ProjectID,
		Location: location,
		Backend:  genai.BackendVertexAI,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create genai client: %w", err)
	}
	return client, nil
}

func (h *APIHandler) Chat(w http.ResponseWriter, r *http.Request) {
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Message == "" {
		writeError(w, "message is required", http.StatusBadRequest)
		return
	}

	conv := h.conversations.Get(req.ConversationID)
	if conv == nil {
		writeError(w, "conversation not found", http.StatusNotFound)
		return
	}

	userMsg := Message{
		ID:        generateID(),
		Role:      "user",
		Content:   req.Message,
		Timestamp: time.Now().UnixMilli(),
	}
	h.conversations.AddMessage(req.ConversationID, userMsg)

	if h.esClient != nil {
		conv = h.conversations.Get(req.ConversationID)
		if err := h.esClient.IndexMessage(r.Context(), req.ConversationID, &userMsg, len(conv.Messages)); err != nil {
			slog.Error("ES index user message failed", "error", err)
		}
	}

	settings := h.config.GetSettings()
	modelID := settings.GoogleCloud.VertexAI.LLMModelID
	if modelID == "" {
		modelID = "gemini-3.1-pro-preview"
	}

	var contents []*genai.Content

	contents = append(contents, &genai.Content{
		Role:  "user",
		Parts: []*genai.Part{{Text: "You are Mobius, an AI growth partner for advertising, marketing, and business optimization. Help users with ad performance analysis, campaign strategy, creative production, and growth planning."}},
	})
	contents = append(contents, &genai.Content{
		Role:  "model",
		Parts: []*genai.Part{{Text: "Understood. I'm Mobius, your AI growth partner. How can I help you today?"}},
	})

	conv = h.conversations.Get(req.ConversationID)
	for _, msg := range conv.Messages {
		role := msg.Role
		if role == "model" {
			role = "model"
		}
		contents = append(contents, &genai.Content{
			Role:  role,
			Parts: []*genai.Part{{Text: msg.Content}},
		})
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()
	var fullResponse string

	for chunk, err := range h.genaiClient.Models.GenerateContentStream(
		ctx,
		modelID,
		contents,
		nil,
	) {
		if err != nil {
			slog.Error("genai stream error", "error", err)
			errData, _ := json.Marshal(map[string]string{"error": err.Error()})
			fmt.Fprintf(w, "data: %s\n\n", errData)
			flusher.Flush()
			break
		}

		text := chunk.Text()
		if text != "" {
			fullResponse += text
			data, _ := json.Marshal(map[string]string{"text": text})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}

	if fullResponse != "" {
		modelMsg := Message{
			ID:        generateID(),
			Role:      "model",
			Content:   fullResponse,
			Timestamp: time.Now().UnixMilli(),
		}
		h.conversations.AddMessage(req.ConversationID, modelMsg)

		if h.esClient != nil {
			conv = h.conversations.Get(req.ConversationID)
			if err := h.esClient.IndexMessage(r.Context(), req.ConversationID, &modelMsg, len(conv.Messages)); err != nil {
				slog.Error("ES index model message failed", "error", err)
			}
			if err := h.esClient.IndexConversation(r.Context(), conv); err != nil {
				slog.Error("ES update conversation metadata failed", "error", err)
			}
		}
	}

	doneData, _ := json.Marshal(map[string]bool{"done": true})
	fmt.Fprintf(w, "data: %s\n\n", doneData)
	flusher.Flush()
}
