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
	ConversationID string    `json:"conversation_id"`
	Message        string    `json:"message"`
	Files          []FileRef `json:"files,omitempty"`
	AgentID        string    `json:"agent_id,omitempty"`
	ModelID        string    `json:"model_id,omitempty"`
}

func NewGenAIClient(ctx context.Context, cfg *Config) (*genai.Client, error) {
	settings := cfg.GetSettings()
	gc := settings.GoogleCloud

	_, location := gc.VertexAI.DefaultLLM()
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

	if req.Message == "" && len(req.Files) == 0 {
		writeError(w, "message or files required", http.StatusBadRequest)
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
		Files:     req.Files,
	}
	h.conversations.AddMessage(req.ConversationID, userMsg)
	conv = h.conversations.Get(req.ConversationID)

	if h.esClient != nil {
		if err := h.esClient.IndexMessage(r.Context(), req.ConversationID, &userMsg, len(conv.Messages)); err != nil {
			slog.Error("ES index user message failed", "error", err)
		}
		if err := h.esClient.IndexConversation(r.Context(), conv); err != nil {
			slog.Error("ES index conversation failed", "error", err)
		}
	}

	settings := h.config.GetSettings()
	modelID, _ := settings.GoogleCloud.VertexAI.DefaultLLM()
	if modelID == "" {
		modelID = "gemini-3.1-pro-preview"
	}

	systemPrompt := "You are Mobius, an AI growth partner for advertising, marketing, and business optimization. Help users with ad performance analysis, campaign strategy, creative production, and growth planning."
	systemAck := "Understood. I'm Mobius, your AI growth partner. How can I help you today?"

	var agent *Employee
	if req.AgentID != "" && h.pgClient != nil {
		a, err := h.pgClient.GetEmployee(r.Context(), req.AgentID)
		if err == nil {
			agent = a
			systemPrompt = fmt.Sprintf("You are %s, %s. %s", agent.Name, agent.Title, agent.Backstory)

			if h.esClient != nil {
				skillIDs, _ := h.pgClient.ListEmployeeSkillIDs(r.Context(), req.AgentID)
				for _, sid := range skillIDs {
					skill, serr := h.esClient.GetSkill(r.Context(), sid)
					if serr == nil {
						systemPrompt += "\n\n## Skill: " + skill.Name + "\n" + skill.Content
					}
				}
			}

			systemAck = fmt.Sprintf("I'm %s, %s. How can I help you?", agent.Name, agent.Title)

			for _, m := range agent.Models {
				if m.Purpose == "primary_llm" && m.ModelID != "" {
					modelID = m.ModelID
					break
				}
			}
		}
	} else if req.ModelID != "" {
		modelID = req.ModelID
	}

	// Build tools for this agent
	var tools []ToolDef
	if agent != nil {
		tools = buildAgentTools(agent)

		if hasTag(agent.Tags, "manager") || agent.Role == "CEO" {
			systemPrompt += managerDirectives()
		}

		if h.esClient != nil {
			mList, _, _ := h.esClient.SearchEmployeeMemories(r.Context(), agent.ID, req.Message, 3)
			if len(mList) > 0 {
				systemPrompt += "\n\n## Retrospective Learnings (your long-term memory):\n"
				for _, m := range mList {
					id := m.ID
					if len(id) > 8 {
						id = id[:8]
					}
					systemPrompt += fmt.Sprintf("- [%s] %s\n", id, m.MemoryText)
				}
				systemPrompt += "\nUse forget_memory with the ID in brackets to remove stale entries."
			}
		}
	}

	// Build provider-neutral messages
	var messages []LLMMessage
	messages = append(messages, LLMMessage{Role: "user", Text: systemPrompt})
	messages = append(messages, LLMMessage{Role: "model", Text: systemAck})

	conv = h.conversations.Get(req.ConversationID)
	for _, msg := range conv.Messages {
		messages = append(messages, LLMMessage{
			Role: msg.Role, Text: msg.Content, Files: msg.Files,
		})
	}

	// Resolve provider
	provider := h.providers.ResolveProvider(modelID)
	if provider == nil {
		writeError(w, "no LLM provider for model: "+modelID, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	llmReq := &LLMRequest{
		Model:        modelID,
		Messages:     messages,
		Tools:        tools,
		OnText: func(text string) {
			data, _ := json.Marshal(map[string]string{"text": text})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		},
		OnToolCall: func(call ToolCall) map[string]any {
			if agent == nil {
				return map[string]any{"error": "no agent context for tool execution"}
			}
			return h.executeToolCall(r.Context(), call, agent, conv.ID)
		},
		OnToolEvent: func(name, status string) {
			data, _ := json.Marshal(map[string]any{"tool_call": name, "status": status})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		},
	}

	fullResponse, err := provider.ChatStream(r.Context(), llmReq)
	if err != nil {
		slog.Error("provider chat error", "error", err, "model", modelID)
		errData, _ := json.Marshal(map[string]string{"error": err.Error()})
		fmt.Fprintf(w, "data: %s\n\n", errData)
		flusher.Flush()
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

	if agent != nil && h.esClient != nil && fullResponse != "" &&
		len(req.Message)+len(fullResponse) > 100 {
		go absorbMemoryFromExchange(context.Background(), h.config, h.providers,
			h.esClient, agent.ID, req.Message, fullResponse, req.ConversationID)
	}

	doneData, _ := json.Marshal(map[string]bool{"done": true})
	fmt.Fprintf(w, "data: %s\n\n", doneData)
	flusher.Flush()
}
