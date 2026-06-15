package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

type InternalLLMAdapter struct {
	providers     *ProviderRegistry
	pgClient      *PGClient
	esClient      *ESClient
	gcsClient     *GCSClient
	bqClient      *BQClient
	config        *Config
	tokenPipeline *TokenPipeline
	events        *EventPipeline
	runs          sync.Map
}

type internalRun struct {
	cancel context.CancelFunc
	output strings.Builder
	usage  TokenUsage
	errMsg string
	status RunStatus
	mu     sync.Mutex
}

func NewInternalLLMAdapter(providers *ProviderRegistry, pg *PGClient, es *ESClient, bq *BQClient, gcs *GCSClient,
	cfg *Config, tp *TokenPipeline, events *EventPipeline) *InternalLLMAdapter {
	return &InternalLLMAdapter{
		providers:     providers,
		pgClient:      pg,
		esClient:      es,
		gcsClient:     gcs,
		bqClient:      bq,
		config:        cfg,
		tokenPipeline: tp,
		events:        events,
	}
}

func (a *InternalLLMAdapter) Type() AdapterType { return AdapterInternal }

func (a *InternalLLMAdapter) Start(ctx context.Context, hb HeartbeatContext) (string, error) {
	runID := generateID()
	// The run ctx is intentionally derived from the caller's ctx, not
	// context.Background(): the dispatcher passes a run-scoped ctx capped at
	// 2*staleTimeout that also cancels on shutdown (see executeAgentTask / H5).
	// Detaching to Background here would leak wedged runs past that cap and
	// ignore graceful shutdown. Callers that need the run to outlive a request
	// must pass a non-request ctx, as the dispatcher already does.
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)

	run := &internalRun{cancel: cancel, status: RunActive}
	a.runs.Store(runID, run)

	go func() {
		defer cancel()
		// Retain the terminal run briefly so a polling Observe can read the final
		// output/usage before the entry is reclaimed (avoids a synthetic empty
		// {RunCompleted}); Stop deletes immediately when cancelled.
		defer time.AfterFunc(terminalRunRetention, func() { a.runs.Delete(runID) })

		// OnText streams the response text into run.output as it arrives, so the
		// final text is already captured by the time this returns — don't write it
		// again here or it would double.
		_, err := a.executeInternalChat(runCtx, hb, run)

		// Terminal state is set under lock; the dispatcher's monitorRun observes
		// it and finalizes the heartbeat_runs row (it owns that row's lifecycle).
		run.mu.Lock()
		if err != nil {
			run.status = RunFailed
			run.errMsg = err.Error()
		} else {
			run.status = RunCompleted
		}
		run.mu.Unlock()
	}()

	return runID, nil
}

func (a *InternalLLMAdapter) Observe(_ context.Context, runID string) (RunObservation, error) {
	val, ok := a.runs.Load(runID)
	if !ok {
		return RunObservation{Status: RunCompleted}, nil
	}
	run := val.(*internalRun)
	run.mu.Lock()
	defer run.mu.Unlock()
	return RunObservation{
		Status:       run.status,
		Output:       run.output.String(),
		TokenUsage:   &run.usage,
		ErrorMessage: run.errMsg,
	}, nil
}

func (a *InternalLLMAdapter) Stop(_ context.Context, runID string) error {
	val, ok := a.runs.Load(runID)
	if !ok {
		return nil
	}
	run := val.(*internalRun)
	run.cancel()
	run.mu.Lock()
	run.status = RunCancelled
	run.mu.Unlock()
	a.runs.Delete(runID)
	return nil
}

func (a *InternalLLMAdapter) executeInternalChat(ctx context.Context, hb HeartbeatContext, run *internalRun) (string, error) {
	modelID := hb.ModelID
	provider := a.providers.ResolveProvider(modelID)
	if provider == nil {
		return "", fmt.Errorf("no provider for model: %s", modelID)
	}

	var messages []LLMMessage
	messages = append(messages, LLMMessage{Role: "user", Text: hb.SystemPrompt})
	messages = append(messages, LLMMessage{Role: "model", Text: fmt.Sprintf("I'm %s, %s. Ready.", hb.AgentName, hb.AgentTitle)})
	messages = append(messages, LLMMessage{Role: "user", Text: fmt.Sprintf("Please complete this task:\n\n%s", hb.TaskBody)})

	if hb.TaskStatus == "needs_review" && hb.TaskResult != "" {
		messages = append(messages, LLMMessage{
			Role: "user",
			Text: fmt.Sprintf("A team member has submitted this work for your review:\n\n%s\n\nReview it carefully. Use verify_deliverable to inspect files produced, then read_project_file to check quality. Only then approve or reject.", hb.TaskResult),
		})
	}

	providerName := "gemini"
	if strings.HasPrefix(modelID, "claude-") {
		providerName = "claude"
	}
	projectID := ""
	if hb.ProjectID != nil {
		projectID = *hb.ProjectID
	}

	assignee, err := a.pgClient.GetEmployee(ctx, hb.AgentID)
	if err != nil {
		return "", fmt.Errorf("failed to load agent: %w", err)
	}

	task, err := a.pgClient.GetTask(ctx, hb.TaskID)
	if err != nil {
		return "", fmt.Errorf("failed to load task: %w", err)
	}

	llmReq := &LLMRequest{
		Model:    modelID,
		Messages: messages,
		Tools:    hb.Tools,
		OnText: func(s string) {
			// Stream the model's response text into the run's output buffer so a
			// polling Observe sees live progress (mid-run output + a readable
			// transcript), instead of an empty buffer until completion. Also feeds
			// the dispatcher's stall-guard output-progress signal (see monitorRun).
			run.mu.Lock()
			run.output.WriteString(s)
			run.mu.Unlock()
		},
		OnToolCall: func(call ToolCall) map[string]any {
			return a.dispatchToolCall(ctx, call, assignee, task)
		},
		OnUsage: func(usage TokenUsage) {
			run.mu.Lock()
			run.usage.PromptTokens += usage.PromptTokens
			run.usage.CompletionTokens += usage.CompletionTokens
			run.usage.TotalTokens += usage.TotalTokens
			run.usage.CachedTokens += usage.CachedTokens
			run.usage.ThoughtsTokens += usage.ThoughtsTokens
			run.usage.ToolUseTokens += usage.ToolUseTokens
			run.mu.Unlock()

			if a.tokenPipeline == nil {
				return
			}
			a.tokenPipeline.Record(&bqTokenRow{
				ID:               generateID(),
				Timestamp:        time.Now().Format("2006-01-02 15:04:05.999999 UTC"),
				ModelID:          modelID,
				Provider:         providerName,
				EmployeeID:       hb.AgentID,
				EmployeeName:     hb.AgentName,
				ProjectID:        projectID,
				TaskID:           hb.TaskID,
				PromptTokens:     int64(usage.PromptTokens),
				CompletionTokens: int64(usage.CompletionTokens),
				TotalTokens:      int64(usage.TotalTokens),
				CachedTokens:     int64(usage.CachedTokens),
				ThoughtsTokens:   int64(usage.ThoughtsTokens),
				ToolUseTokens:    int64(usage.ToolUseTokens),
				LatencyMs:        usage.LatencyMs,
				Status:           "success",
				Source:           "task",
			})
		},
	}

	fullResponse, err := provider.ChatStream(ctx, llmReq)
	if err != nil {
		return "", fmt.Errorf("LLM execution failed: %w", err)
	}

	if a.esClient != nil && fullResponse != "" && len(hb.TaskBody)+len(fullResponse) > 100 {
		go absorbMemoryFromExchange(context.Background(), a.config, a.providers,
			a.esClient, hb.AgentID, hb.TaskBody, fullResponse, hb.TaskID)
	}

	return fullResponse, nil
}

// dispatchToolCall routes a tool call and records a generic 'tool_call' event
// for the per-run timeline. Tools that already publish a richer, curated event
// (see toolsWithDomainEvent) are skipped here so they aren't double-logged.
func (a *InternalLLMAdapter) dispatchToolCall(ctx context.Context, call ToolCall, agent *Employee, task *Task) map[string]any {
	start := time.Now()
	result := a.routeToolCall(ctx, call, agent, task)
	a.recordToolCall(call, result, time.Since(start), agent, task)
	return result
}

// toolsWithDomainEvent lists tools that publish their own curated domain event
// (e.g. file_written, task_delegated) elsewhere in adapter_internal*.go. The
// generic tool_call event is suppressed for these to avoid duplicate entries in
// the activity timeline. Keep in sync with the newEvent(...) calls in this package.
var toolsWithDomainEvent = map[string]bool{
	"delegate_task":       true,
	"hire_employee":       true,
	"submit_task_result":  true,
	"review_task":         true,
	"write_project_file":  true,
	"run_project_command": true,
	"store_memory":        true,
}

func (a *InternalLLMAdapter) recordToolCall(call ToolCall, result map[string]any, dur time.Duration, agent *Employee, task *Task) {
	if a.events == nil || toolsWithDomainEvent[call.Name] {
		return
	}
	payload := map[string]any{
		"tool":        call.Name,
		"status":      "ok",
		"duration_ms": dur.Milliseconds(),
	}
	if em, ok := result["error"].(string); ok && em != "" {
		payload["status"] = "error"
		payload["error"] = truncateStr(em, 200)
	}
	// Capture full tool args as a nested object for rich BQ analysis
	// (payload is a JSON column in BQ; the ES events index stores payload with
	// enabled=false, so arbitrary nested args don't cause mapping growth).
	if len(call.Args) > 0 {
		payload["args"] = call.Args
	}
	var actorID, projectID, taskID *string
	if agent != nil {
		actorID = &agent.ID
	}
	if task != nil {
		taskID = &task.ID
		projectID = task.ProjectID
	}
	a.events.Publish(newEvent("tool_call", actorID, projectID, taskID, payload))
}

func (a *InternalLLMAdapter) routeToolCall(ctx context.Context, call ToolCall, agent *Employee, task *Task) map[string]any {
	taskID := task.ID
	switch call.Name {
	case "delegate_task":
		return a.execDelegate(ctx, call.Args, agent, task)
	case "hire_employee":
		return a.execHire(ctx, call.Args, agent)
	case "submit_task_result":
		return a.execSubmit(ctx, call.Args, task)
	case "review_task":
		return a.execReview(ctx, call.Args, agent)
	case "verify_deliverable":
		return a.execVerifyDeliverable(ctx, call.Args)
	case "list_team":
		emp, err := a.pgClient.GetEmployee(ctx, agent.ID)
		if err != nil {
			return map[string]any{"error": err.Error()}
		}
		var members []map[string]any
		for _, r := range emp.Reports {
			members = append(members, map[string]any{"id": r.ID, "name": r.Name, "title": r.Title, "role": r.Role})
		}
		if members == nil {
			members = []map[string]any{}
		}
		return map[string]any{"team": members, "count": len(members)}
	case "store_memory":
		text, _ := call.Args["memory_text"].(string)
		if text == "" || a.esClient == nil {
			return map[string]any{"error": "memory_text is required"}
		}
		if err := a.esClient.IndexEmployeeMemoryDedup(ctx, agent.ID, taskID, text); err != nil {
			return map[string]any{"error": "failed to store memory: " + err.Error()}
		}
		if a.events != nil {
			a.events.Publish(newEvent("memory_stored", &agent.ID, nil, &taskID,
				map[string]any{"memory_text": truncateStr(text, 200)}))
		}
		return map[string]any{"status": "remembered", "memory_text": text}
	case "forget_memory":
		memoryID, _ := call.Args["memory_id"].(string)
		if memoryID == "" || a.esClient == nil {
			return map[string]any{"error": "memory_id is required"}
		}
		if err := a.esClient.DeleteEmployeeMemory(ctx, memoryID); err != nil {
			return map[string]any{"error": "failed to forget: " + err.Error()}
		}
		return map[string]any{"status": "forgotten", "memory_id": memoryID}
	case "write_project_file":
		return a.execWriteProjectFile(ctx, call.Args, task)
	case "read_project_file":
		return a.execReadProjectFile(ctx, call.Args, task)
	case "search_project_assets":
		return a.execSearchProjectAssets(ctx, call.Args, task)
	case "list_project_assets":
		return a.execListProjectAssets(ctx, task)
	case "run_project_command":
		return a.execRunProjectCommand(ctx, call.Args, task)
	case "list_tasks":
		return a.execListTasks(ctx, call.Args)
	case "get_task":
		return a.execGetTask(ctx, call.Args)
	case "update_task":
		return a.execUpdateTask(ctx, call.Args)
	case "update_task_status":
		return a.execUpdateTaskStatus(ctx, call.Args, agent)
	case "add_task_comment":
		return a.execAddTaskComment(ctx, call.Args, agent)
	case "list_employees":
		return a.execListEmployees(ctx)
	case "get_employee":
		return a.execGetEmployee(ctx, call.Args)
	case "update_employee":
		return a.execUpdateEmployee(ctx, call.Args)
	case "list_projects":
		return a.execListProjects(ctx)
	case "create_project":
		return a.execCreateProject(ctx, call.Args, agent)
	case "update_project":
		return a.execUpdateProject(ctx, call.Args)
	case "list_prompts":
		return a.execListPrompts(ctx, call.Args)
	case "create_prompt":
		return a.execCreatePrompt(ctx, call.Args)
	case "update_prompt":
		return a.execUpdatePrompt(ctx, call.Args)
	case "delete_prompt":
		return a.execDeletePrompt(ctx, call.Args)
	case "list_skills":
		return a.execListSkills(ctx, call.Args)
	case "assign_skill":
		return a.execAssignSkill(ctx, call.Args)
	case "unassign_skill":
		return a.execUnassignSkill(ctx, call.Args)
	case "ask_user":
		return a.execAskUser(ctx, call.Args, agent, task)
	case "suggest_tasks":
		return a.execSuggestTasks(ctx, call.Args, agent, task)
	case "tag_media":
		return execTagMediaTool(ctx, a.bqClient, a.esClient, a.events, agent.ID, call.Args)
	case "get_tag_results":
		return execGetTagResultsTool(ctx, a.bqClient, call.Args)
	case "query_tags":
		return execQueryTagsTool(ctx, a.bqClient, call.Args)
	case "watermark_assets":
		return execWatermarkAssetsTool(ctx, a.pgClient, a.gcsClient, a.config, a.events, agent.ID, call.Args)
	case "verify_watermark":
		return execVerifyWatermarkTool(ctx, a.gcsClient, a.config, agent.ID, call.Args)
	case "playable_load_reference_game":
		return execPlayableLoadReferenceGameTool(a.config, call.Args)
	case "playable_get_tracking_sdk":
		return execPlayableGetTrackingSDKTool()
	case "playable_get_web_audio_sfx":
		return execPlayableGetWebAudioSFXTool()
	case "playable_write_html":
		return execPlayableWriteHTMLTool(ctx, a.config, a.pgClient, resolvePlayableProjectID(task, call.Args), call.Args)
	case "generate_image":
		return execGenerateImageTool(ctx, a.config, a.providers, a.gcsClient, a.esClient, a.pgClient, resolvePlayableProjectID(task, call.Args), call.Args)
	case "generate_audio":
		return execGenerateAudioTool(ctx, a.config, a.pgClient, resolvePlayableProjectID(task, call.Args), call.Args)
	case "publish_playable_ad":
		return execPublishPlayableAdTool(ctx, a.gcsClient, a.esClient, a.config, a.pgClient, resolvePlayableProjectID(task, call.Args), call.Args)
	case "save_upload_to_assets":
		// Autonomous task runs have no chat upload to save; srcFile is nil.
		return execSaveUploadToAssetsTool(ctx, a.gcsClient, a.esClient, a.pgClient, a.config, resolvePlayableProjectID(task, call.Args), nil, call.Args)
	default:
		return map[string]any{"error": "unknown tool: " + call.Name}
	}
}

func (a *InternalLLMAdapter) summarizeForHandoff(ctx context.Context, taskBody, result, event string) string {
	if a.config == nil {
		return ""
	}
	settings := a.config.GetSettings()
	modelID, _ := settings.GoogleCloud.VertexAI.DefaultLLM()
	if modelID == "" {
		return ""
	}
	provider := a.providers.ResolveProvider(modelID)
	if provider == nil {
		return ""
	}

	input := truncateForExtraction(taskBody, maxExtractionInputLen)
	output := truncateForExtraction(result, maxExtractionInputLen)

	prompt := fmt.Sprintf(`Summarize this task handoff in one concise paragraph (max 300 characters).

Event: %s
Task goal:
%s

Deliverable:
%s

Include: what was accomplished, key decisions made, and any open issues. Be specific and factual.`, event, input, output)

	req := &LLMRequest{
		Model:    modelID,
		Messages: []LLMMessage{{Role: "user", Text: prompt}},
		OnText:   func(string) {},
	}

	summary, err := provider.ChatStream(ctx, req)
	if err != nil {
		slog.Warn("handoff summarization failed", "event", event, "error", err)
		return ""
	}
	return strings.TrimSpace(summary)
}
