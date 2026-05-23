package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type ClaudeProvider struct {
	projectID   string
	location    string
	tokenSource oauth2.TokenSource
}

func NewClaudeProvider(projectID, location string) *ClaudeProvider {
	ctx := context.Background()
	creds, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
	var ts oauth2.TokenSource
	if err != nil {
		slog.Warn("claude provider: failed to discover credentials", "error", err)
	} else {
		ts = creds.TokenSource
	}
	return &ClaudeProvider{projectID: projectID, location: location, tokenSource: ts}
}

func (c *ClaudeProvider) endpoint(model string) string {
	return fmt.Sprintf(
		"https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/anthropic/models/%s:streamRawPredict",
		c.location, c.projectID, c.location, model,
	)
}

func (c *ClaudeProvider) ChatStream(ctx context.Context, req *LLMRequest) (string, error) {
	var messages []map[string]any
	for _, m := range req.Messages {
		role := m.Role
		if role == "model" {
			role = "assistant"
		}

		var content any
		if len(m.Files) == 0 {
			content = m.Text
		} else {
			var parts []map[string]any
			parts = append(parts, map[string]any{"type": "text", "text": m.Text})
			for _, f := range m.Files {
				if strings.HasPrefix(f.MIMEType, "image/") && f.GCSURI != "" {
					parts = append(parts, map[string]any{
						"type": "image",
						"source": map[string]any{
							"type":       "url",
							"url":        f.GCSURI,
							"media_type": f.MIMEType,
						},
					})
				}
			}
			content = parts
		}

		messages = append(messages, map[string]any{"role": role, "content": content})
	}

	var tools []map[string]any
	for _, td := range req.Tools {
		tools = append(tools, map[string]any{
			"name":         td.Name,
			"description":  td.Description,
			"input_schema": td.Parameters,
		})
	}

	var fullText string
	for i := 0; i < 5; i++ {
		body := map[string]any{
			"anthropic_version": "vertex-2023-10-16",
			"max_tokens":       4096,
			"messages":         messages,
			"stream":           true,
		}
		if req.SystemPrompt != "" {
			body["system"] = req.SystemPrompt
		}
		if len(tools) > 0 {
			body["tools"] = tools
		}

		text, toolCalls, err := c.doStream(ctx, req.Model, body, req.OnText)
		if err != nil {
			return fullText, err
		}
		fullText += text

		if len(toolCalls) == 0 {
			break
		}

		var assistantContent []map[string]any
		if text != "" {
			assistantContent = append(assistantContent, map[string]any{
				"type": "text", "text": text,
			})
		}
		for _, tc := range toolCalls {
			assistantContent = append(assistantContent, map[string]any{
				"type":  "tool_use",
				"id":    tc.ID,
				"name":  tc.Name,
				"input": tc.Args,
			})
		}
		messages = append(messages, map[string]any{
			"role": "assistant", "content": assistantContent,
		})

		var resultContent []map[string]any
		for _, tc := range toolCalls {
			result := req.OnToolCall(tc)
			if req.OnToolEvent != nil {
				req.OnToolEvent(tc.Name, "executed")
			}
			resultJSON, _ := json.Marshal(result)
			resultContent = append(resultContent, map[string]any{
				"type":        "tool_result",
				"tool_use_id": tc.ID,
				"content":     string(resultJSON),
			})
		}
		messages = append(messages, map[string]any{
			"role": "user", "content": resultContent,
		})
	}

	return fullText, nil
}

func (c *ClaudeProvider) doStream(
	ctx context.Context,
	model string,
	body map[string]any,
	onText func(string),
) (string, []ToolCall, error) {
	payload, _ := json.Marshal(body)

	if c.tokenSource == nil {
		return "", nil, fmt.Errorf("oauth token source not initialized")
	}
	token, err := c.tokenSource.Token()
	if err != nil {
		return "", nil, fmt.Errorf("token: %w", err)
	}

	httpReq, _ := http.NewRequestWithContext(ctx, "POST",
		c.endpoint(model), bytes.NewReader(payload))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token.AccessToken)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return "", nil, fmt.Errorf("claude API %d: %s", resp.StatusCode, string(b))
	}

	var text string
	var toolCalls []ToolCall
	var currentToolCall *ToolCall
	var currentToolInput strings.Builder

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := line[6:]
		if data == "[DONE]" {
			break
		}

		var event map[string]any
		if json.Unmarshal([]byte(data), &event) != nil {
			continue
		}

		switch event["type"] {
		case "content_block_start":
			if cb, ok := event["content_block"].(map[string]any); ok {
				if cb["type"] == "tool_use" {
					currentToolCall = &ToolCall{
						ID:   cb["id"].(string),
						Name: cb["name"].(string),
					}
					currentToolInput.Reset()
				}
			}

		case "content_block_delta":
			if delta, ok := event["delta"].(map[string]any); ok {
				if delta["type"] == "text_delta" {
					t, _ := delta["text"].(string)
					text += t
					if onText != nil {
						onText(t)
					}
				} else if delta["type"] == "input_json_delta" {
					partial, _ := delta["partial_json"].(string)
					currentToolInput.WriteString(partial)
				}
			}

		case "content_block_stop":
			if currentToolCall != nil {
				var args map[string]any
				json.Unmarshal([]byte(currentToolInput.String()), &args)
				currentToolCall.Args = args
				toolCalls = append(toolCalls, *currentToolCall)
				currentToolCall = nil
			}
		}
	}

	return text, toolCalls, nil
}
