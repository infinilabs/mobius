package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/genai"
)

type GeminiProvider struct {
	vertexClient   *genai.Client
	studioClient   *genai.Client
	studioModels   map[string]bool
}

func NewGeminiProvider(vertexClient, studioClient *genai.Client, studioModels map[string]bool) *GeminiProvider {
	return &GeminiProvider{
		vertexClient: vertexClient,
		studioClient: studioClient,
		studioModels: studioModels,
	}
}

func (g *GeminiProvider) clientForModel(modelID string) *genai.Client {
	if g.studioClient != nil && g.studioModels[modelID] {
		return g.studioClient
	}
	return g.vertexClient
}

func (g *GeminiProvider) ChatStream(ctx context.Context, req *LLMRequest) (string, error) {
	client := g.clientForModel(req.Model)
	var contents []*genai.Content
	for _, m := range req.Messages {
		role := m.Role
		if role == "assistant" {
			role = "model"
		}
		parts := []*genai.Part{{Text: m.Text}}
		for _, f := range m.Files {
			if f.GCSURI != "" {
				parts = append(parts, genai.NewPartFromURI(f.GCSURI, f.MIMEType))
			}
		}
		contents = append(contents, &genai.Content{Role: role, Parts: parts})
	}

	var tools []*genai.Tool
	if len(req.Tools) > 0 {
		var decls []*genai.FunctionDeclaration
		for _, td := range req.Tools {
			decls = append(decls, &genai.FunctionDeclaration{
				Name:                 td.Name,
				Description:          td.Description,
				ParametersJsonSchema: td.Parameters,
			})
		}
		tools = []*genai.Tool{{FunctionDeclarations: decls}}
	}

	config := &genai.GenerateContentConfig{Tools: tools}
	if req.SystemPrompt != "" {
		config.SystemInstruction = &genai.Content{
			Parts: []*genai.Part{{Text: req.SystemPrompt}},
		}
	}

	const maxToolRounds = 40
	var fullText string
	for i := 0; i < maxToolRounds; i++ {
		var fcParts []*genai.Part
		var iterText string
		var turnUsage TokenUsage
		roundStart := time.Now()

		for chunk, err := range client.Models.GenerateContentStream(
			ctx, req.Model, contents, config,
		) {
			if err != nil {
				return fullText, fmt.Errorf("stream error: %w", err)
			}
			if len(chunk.Candidates) > 0 && chunk.Candidates[0].Content != nil {
				for _, p := range chunk.Candidates[0].Content.Parts {
					if p.FunctionCall != nil {
						fcParts = append(fcParts, p)
					}
				}
			}
			if chunk.UsageMetadata != nil {
				turnUsage = TokenUsage{
					PromptTokens:     chunk.UsageMetadata.PromptTokenCount,
					CompletionTokens: chunk.UsageMetadata.CandidatesTokenCount,
					TotalTokens:      chunk.UsageMetadata.TotalTokenCount,
					CachedTokens:     chunk.UsageMetadata.CachedContentTokenCount,
					ThoughtsTokens:   chunk.UsageMetadata.ThoughtsTokenCount,
					ToolUseTokens:    chunk.UsageMetadata.ToolUsePromptTokenCount,
				}
			}
			text := chunk.Text()
			if text != "" {
				iterText += text
				if req.OnText != nil {
					req.OnText(text)
				}
			}
		}

		turnUsage.LatencyMs = time.Since(roundStart).Milliseconds()
		if req.OnUsage != nil {
			req.OnUsage(turnUsage)
		}

		if len(fcParts) == 0 {
			fullText += iterText
			break
		}

		// Execute this round's tool calls (every round, including the last) so a
		// final submit_task_result or file write lands before we stop.
		contents = append(contents, &genai.Content{
			Role:  "model",
			Parts: fcParts,
		})

		var responseParts []*genai.Part
		for _, p := range fcParts {
			fc := p.FunctionCall
			tc := ToolCall{Name: fc.Name, Args: fc.Args}
			result := req.OnToolCall(tc)

			if req.OnToolEvent != nil {
				req.OnToolEvent(fc.Name, "executed")
			}

			responseParts = append(responseParts, &genai.Part{
				FunctionResponse: &genai.FunctionResponse{
					Name:     fc.Name,
					Response: result,
				},
			})
		}
		contents = append(contents, &genai.Content{
			Role:  "user",
			Parts: responseParts,
		})

		// Out of tool-call budget: do ONE final tool-less generation so the agent
		// produces a textual result (a summary of what it did) instead of leaving
		// the run output empty — which would otherwise fail the task despite real
		// work having been done.
		if i == maxToolRounds-1 {
			slog.Warn("gemini: tool call loop limit reached; forcing final summary",
				"model", req.Model, "rounds", maxToolRounds)
			contents = append(contents, &genai.Content{
				Role: "user",
				Parts: []*genai.Part{{Text: "You have reached your tool-call limit. Do not call any more tools. " +
					"Write your final result now as plain text: what you produced, where the files/assets live, and anything still incomplete."}},
			})
			graceCfg := &genai.GenerateContentConfig{}
			if config.SystemInstruction != nil {
				graceCfg.SystemInstruction = config.SystemInstruction
			}
			for chunk, err := range client.Models.GenerateContentStream(ctx, req.Model, contents, graceCfg) {
				if err != nil {
					return fullText, fmt.Errorf("final-summary stream error: %w", err)
				}
				if t := chunk.Text(); t != "" {
					fullText += t
					if req.OnText != nil {
						req.OnText(t)
					}
				}
				if chunk.UsageMetadata != nil && req.OnUsage != nil {
					req.OnUsage(TokenUsage{
						PromptTokens:     chunk.UsageMetadata.PromptTokenCount,
						CompletionTokens: chunk.UsageMetadata.CandidatesTokenCount,
						TotalTokens:      chunk.UsageMetadata.TotalTokenCount,
						CachedTokens:     chunk.UsageMetadata.CachedContentTokenCount,
						ThoughtsTokens:   chunk.UsageMetadata.ThoughtsTokenCount,
					})
				}
			}
		}
	}

	return fullText, nil
}
