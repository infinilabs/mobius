package main

import (
	"context"
	"fmt"
	"log/slog"

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

	const maxToolRounds = 10
	var fullText string
	for i := 0; i < maxToolRounds; i++ {
		var calls []*genai.FunctionCall
		var iterText string

		for chunk, err := range client.Models.GenerateContentStream(
			ctx, req.Model, contents, config,
		) {
			if err != nil {
				return fullText, fmt.Errorf("stream error: %w", err)
			}
			if fcs := chunk.FunctionCalls(); len(fcs) > 0 {
				calls = append(calls, fcs...)
				continue
			}
			text := chunk.Text()
			if text != "" {
				iterText += text
				if req.OnText != nil {
					req.OnText(text)
				}
			}
		}

		if len(calls) == 0 {
			fullText += iterText
			break
		}

		if i == maxToolRounds-1 {
			slog.Warn("gemini: tool call loop limit reached", "model", req.Model, "rounds", maxToolRounds)
			fullText += iterText
			break
		}

		for _, fc := range calls {
			tc := ToolCall{Name: fc.Name, Args: fc.Args}
			result := req.OnToolCall(tc)

			if req.OnToolEvent != nil {
				req.OnToolEvent(fc.Name, "executed")
			}

			contents = append(contents, &genai.Content{
				Role:  "model",
				Parts: []*genai.Part{{FunctionCall: fc}},
			})
			contents = append(contents, &genai.Content{
				Role: "user",
				Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{
					Name:     fc.Name,
					Response: result,
				}}},
			})
		}
	}

	return fullText, nil
}
