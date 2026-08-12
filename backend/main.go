package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"mobius/internal/httpapi"
	"mobius/internal/skills"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

func newLogWriter(filename string, maxSizeMB, maxBackups, maxAgeDays int) *lumberjack.Logger {
	if maxSizeMB <= 0 {
		maxSizeMB = 32
	}
	if maxBackups <= 0 {
		maxBackups = 5
	}
	if maxAgeDays <= 0 {
		maxAgeDays = 30
	}
	return &lumberjack.Logger{
		Filename:   filename,
		MaxSize:    maxSizeMB,
		MaxBackups: maxBackups,
		MaxAge:     maxAgeDays,
		Compress:   true,
	}
}

func newLogger(w io.Writer, debug bool) *slog.Logger {
	opts := &slog.HandlerOptions{}
	if debug {
		opts.Level = slog.LevelDebug
		opts.AddSource = true
		return slog.New(slog.NewTextHandler(w, opts))
	}
	return slog.New(slog.NewJSONHandler(w, opts))
}

func main() {
	configPath := "conf.yaml"
	cfg, err := LoadConfig(configPath)
	if err != nil {
		configPath = "../conf.yaml"
		cfg, err = LoadConfig(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "FATAL: failed to load config: %v\n", err)
			os.Exit(1)
		}
	}

	debug := cfg.Server.Mode == "debug"

	logDir := "logs"
	if _, err := os.Stat(logDir); os.IsNotExist(err) {
		os.Mkdir(logDir, 0755)
	}

	serverLogWriter := newLogWriter(fmt.Sprintf("%s/server.log", logDir), cfg.Server.LogMaxSizeMB, cfg.Server.LogMaxBackups, cfg.Server.LogMaxAgeDays)
	accessLogWriter := newLogWriter(fmt.Sprintf("%s/access.log", logDir), cfg.Server.LogMaxSizeMB, cfg.Server.LogMaxBackups, cfg.Server.LogMaxAgeDays)

	var serverWriter io.Writer = serverLogWriter
	if debug {
		serverWriter = io.MultiWriter(os.Stdout, serverLogWriter)
	}

	serverLogger := newLogger(serverWriter, debug)
	accessLogger := newLogger(accessLogWriter, debug)
	slog.SetDefault(serverLogger)

	probeNsJail(cfg.Sandbox)

	ctx := context.Background()
	vertexClient, geminiClient, err := httpapi.NewGenAIClients(ctx, cfg)
	if err != nil {
		slog.Error("failed to init GenAI clients", "error", err)
		slog.Warn("GenAI features will be unavailable until config is fixed")
		vertexClient = nil
		geminiClient = nil
	}

	var esClient *ESClient
	esURL := cfg.GetSettings().Elasticsearch.URL
	esClient, err = NewESClient(esURL)
	if err != nil {
		slog.Error("failed to init Elasticsearch client", "url", esURL, "error", err)
		slog.Warn("Elasticsearch persistence will be unavailable")
		esClient = nil
	}

	var gcsClient *GCSClient
	if cfg.GoogleCloud.GCS.Bucket != "" {
		gcsClient, err = NewGCSClient(ctx, cfg)
		if err != nil {
			slog.Error("failed to init GCS client", "error", err)
			slog.Warn("GCS uploads unavailable, falling back to local disk")
			gcsClient = nil
		}
	}

	var bqClient *BQClient
	if cfg.GoogleCloud.BigQuery.Dataset != "" {
		bqClient, err = NewBQClient(ctx, cfg)
		if err != nil {
			slog.Error("failed to init BigQuery client", "error", err)
			slog.Warn("BigQuery event analytics unavailable")
			bqClient = nil
		}
	}
	// Keep the tagging remote model on the newest Gemini Flash. Only consulted
	// when tagging_model_endpoint is NOT set in conf.yaml (a configured value
	// is pinned and used verbatim).
	if bqClient != nil && vertexClient != nil {
		bqClient.SetTaggingEndpointResolver(func(ctx context.Context) (string, error) {
			return latestFlashEndpoint(ctx, vertexClient)
		})
	}
	// Stand up the whole tagging stack (connection → IAM → dataset + remote
	// model) at boot so tag_media never trips over missing infra mid-task.
	// Async: BQ DDL takes seconds and must not delay serving.
	if bqClient != nil {
		go func() {
			if err := bqClient.EnsureTaggingSetup(ctx); err != nil {
				slog.Error("media tagging setup incomplete; tag_media will fail until fixed", "error", err)
			} else {
				slog.Info("media tagging stack ready (connection, IAM, dataset, remote model)")
			}
		}()
	}

	var pgClient *PGClient
	pgClient, err = NewPGClient(ctx, cfg.Postgres)
	if err != nil {
		slog.Error("failed to init PostgreSQL client", "error", err)
		slog.Warn("PostgreSQL features will be unavailable")
		pgClient = nil
	}

	if pgClient != nil {
		migrationsDir := "schemas/postgres"
		if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
			migrationsDir = "../schemas/postgres"
		}
		if err := pgClient.RunMigrations(ctx, migrationsDir); err != nil {
			slog.Error("failed to run PG migrations", "error", err)
		}
		// Wire the ES mirror so task-status mutations keep ES in sync at the PG
		// layer, regardless of which caller (HTTP, agent, MCP, delegation) drives them.
		// Guard the nil case: storage takes an interface, and a typed-nil would
		// defeat its internal nil checks.
		if esClient != nil {
			pgClient.SetIndexer(esClient)
		}
	}

	skillsDir := "skills"
	if _, err := os.Stat(skillsDir); os.IsNotExist(err) {
		skillsDir = "../skills"
	}

	providers := NewProviderRegistry()
	if vertexClient != nil {
		var studioModels map[string]bool
		if geminiClient != vertexClient {
			slog.Info("listing AI Studio models for dynamic routing...")
			studioModels = httpapi.ListAvailableModels(ctx, geminiClient)
			slog.Info("AI Studio models discovered", "count", len(studioModels))
		}
		providers.Register("gemini", NewGeminiProvider(vertexClient, geminiClient, studioModels))
	}
	gc := cfg.GetSettings().GoogleCloud
	claudeProjectID := gc.VertexAI.ProjectID
	if claudeProjectID == "" {
		claudeProjectID = gc.ProjectID
	}
	if claudeProjectID != "" {
		providers.Register("claude", NewClaudeProvider(claudeProjectID, "us-east5"))
	}

	var eventPipeline *EventPipeline
	if esClient != nil || bqClient != nil {
		eventPipeline = NewEventPipeline(esClient, bqClient, cfg.Elasticsearch.Events)
		slog.Info("event pipeline initialized",
			"buffer", cfg.Elasticsearch.Events.BufferSize,
			"batch", cfg.Elasticsearch.Events.BatchSize)
	}

	api := httpapi.NewAPIHandler(cfg, configPath, vertexClient, esClient, gcsClient, pgClient, bqClient, skillsDir, providers, eventPipeline)

	// Skill sync sources
	hermesPath := cfg.SkillSync.HermesPath
	if hermesPath == "" {
		hermesPath = "../hermes-agent"
	}
	if _, err := os.Stat(hermesPath); err == nil {
		api.AddSyncSource(httpapi.NewHermesSource(hermesPath))
		slog.Info("skill sync source configured", "source", "hermes", "path", hermesPath)
	}

	defaultRepos := []struct {
		name, path, category string
		dirs                 []string
		anyMD                bool
	}{
		{"anthropic", "../anthropic-skills", "anthropic", []string{"skills"}, false},
		{"addyosmani", "../addyosmani-skills", "engineering", []string{"skills"}, false},
		{"vercel", "../vercel-skills", "frontend", []string{"skills"}, false},
		{"trailofbits", "../trailofbits-skills", "security", []string{"plugins", ".codex/skills"}, false},
		{"agency-agents", "../agency-agents", "", []string{
			"academic", "design", "engineering", "finance", "game-development",
			"gis", "marketing", "paid-media", "product", "project-management",
			"sales", "security", "spatial-computing", "specialized", "support", "testing",
		}, true},
	}
	for _, r := range defaultRepos {
		if _, err := os.Stat(r.path); err == nil {
			api.AddSyncSource(&httpapi.GitRepoSource{
				SourceName: r.name,
				BasePath:   r.path,
				Category:   r.category,
				SkillsDirs: r.dirs,
				AnyMD:      r.anyMD,
			})
			slog.Info("skill sync source configured", "source", r.name, "path", r.path)
		}
	}

	for _, r := range cfg.SkillSync.Repos {
		if _, err := os.Stat(r.Path); err == nil {
			dirs := r.Dirs
			if len(dirs) == 0 {
				dirs = []string{"skills"}
			}
			api.AddSyncSource(&httpapi.GitRepoSource{
				SourceName: r.Name,
				BasePath:   r.Path,
				Category:   r.Category,
				SkillsDirs: dirs,
			})
			slog.Info("skill sync source configured (config)", "source", r.Name, "path", r.Path)
		}
	}

	if esClient != nil {
		if err := hydrateConversations(ctx, esClient, api.Conversations()); err != nil {
			slog.Error("failed to hydrate conversations from ES", "error", err)
		}

		if pgClient != nil {
			metas, merr := pgClient.ListConversationsMeta(ctx, "")
			if merr == nil {
				for _, m := range metas {
					if m.ProjectID != nil {
						api.Conversations().SetProjectID(m.ID, *m.ProjectID)
					}
				}
				slog.Info("conversation project_ids backfilled from PG", "count", len(metas))
			}
		}

		promptsDir := "prompts"
		if _, err := os.Stat(promptsDir); os.IsNotExist(err) {
			promptsDir = "../prompts"
		}
		if _, err := os.Stat(promptsDir); err == nil {
			if err := httpapi.SeedPrompts(ctx, esClient, promptsDir); err != nil {
				slog.Error("failed to seed prompts", "error", err)
			}
		}

		for _, src := range api.SyncSources() {
			a, u, syncErr := src.Sync(ctx, skillsDir)
			if syncErr != nil {
				slog.Warn("startup upstream sync failed", "source", src.Name(), "error", syncErr)
			} else if a+u > 0 {
				slog.Info("startup upstream sync", "source", src.Name(), "added", a, "updated", u)
			}
		}

		if _, err := os.Stat(skillsDir); err == nil {
			added, updated, syncErr := skills.SyncFromDisk(ctx, esClient, pgClient, skillsDir)
			if syncErr != nil {
				slog.Error("failed to sync skills from disk", "error", syncErr)
			} else if added+updated > 0 {
				slog.Info("skills synced from disk", "added", added, "updated", updated)
			}
		}
	}

	// Context + WaitGroup for background goroutines so shutdown cancels and
	// drains them instead of orphaning them.
	syncCtx, syncCancel := context.WithCancel(context.Background())
	var bgWG sync.WaitGroup

	if pgClient != nil {
		if err := pgClient.SeedDefaultEmployees(ctx); err != nil {
			slog.Error("failed to seed default employees", "error", err)
		}
		if esClient != nil {
			if err := pgClient.SeedDefaultSkillAssignments(ctx, esClient); err != nil {
				slog.Error("failed to seed skill assignments", "error", err)
			}
			bgWG.Add(1)
			go func() {
				defer bgWG.Done()
				if err := esClient.BackfillEmployees(syncCtx, pgClient); err != nil {
					slog.Error("ES backfill employees failed", "error", err)
				}
				if err := esClient.BackfillProjects(syncCtx, pgClient); err != nil {
					slog.Error("ES backfill projects failed", "error", err)
				}
				if err := esClient.BackfillTasks(syncCtx, pgClient); err != nil {
					slog.Error("ES backfill tasks failed", "error", err)
				}
			}()
		}
	}

	mux := http.NewServeMux()

	logMW := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)

			duration := time.Since(start)
			accessLogger.Info("request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", sw.status),
				slog.Duration("duration", duration),
				slog.String("ip", r.RemoteAddr),
			)

			if debug && sw.status >= 400 {
				slog.Debug("slow or failed request",
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Int("status", sw.status),
					slog.Duration("duration", duration),
				)
			}
		})
	}

	auth := httpapi.NewAuth(cfg.Server.APIToken)
	if !auth.Enabled() {
		slog.Warn("API authentication disabled: set server.api_token or MOBIUS_API_TOKEN to require a token on /api/* routes")
	}

	h := func(f http.HandlerFunc) http.Handler { return logMW(auth.Middleware(f)) }

	// Config & settings
	mux.Handle("/api/health", h(api.HealthCheck))
	mux.Handle("/api/config", h(api.GetConfig))
	mux.Handle("GET /api/settings", h(api.GetSettings))
	mux.Handle("PUT /api/settings", h(api.UpdateSettings))
	mux.Handle("GET /api/browse-directories", h(api.BrowseDirectories))

	// Conversations
	mux.Handle("GET /api/conversations", h(api.ListConversations))
	mux.Handle("POST /api/conversations", h(api.CreateConversation))
	mux.Handle("GET /api/conversations/{id}", h(api.GetConversation))
	mux.Handle("PUT /api/conversations/{id}", h(api.RenameConversation))
	mux.Handle("DELETE /api/conversations/{id}", h(api.DeleteConversation))
	mux.Handle("POST /api/conversations/{id}/truncate", h(api.TruncateConversation))

	// Prompts
	mux.Handle("GET /api/prompts", h(api.ListPrompts))
	mux.Handle("POST /api/prompts", h(api.CreatePrompt))
	mux.Handle("GET /api/prompts/{id}", h(api.GetPrompt))
	mux.Handle("PUT /api/prompts/{id}", h(api.UpdatePrompt))
	mux.Handle("DELETE /api/prompts/{id}", h(api.DeletePrompt))

	// Skills
	mux.Handle("GET /api/skills", h(api.ListSkills))
	mux.Handle("POST /api/skills", h(api.CreateSkill))
	mux.Handle("POST /api/skills/sync", h(api.SyncSkills))
	mux.Handle("GET /api/skills/sync/status", h(api.GetSyncStatus))
	mux.Handle("GET /api/skills/{id}", h(api.GetSkill))
	mux.Handle("PUT /api/skills/{id}", h(api.UpdateSkill))
	mux.Handle("DELETE /api/skills/{id}", h(api.DeleteSkill))

	// Employees
	mux.Handle("GET /api/employees", h(api.ListEmployees))
	mux.Handle("POST /api/employees", h(api.CreateEmployee))
	mux.Handle("GET /api/employees/{id}", h(api.GetEmployee))
	mux.Handle("PUT /api/employees/{id}", h(api.UpdateEmployee))
	mux.Handle("DELETE /api/employees/{id}", h(api.DeleteEmployee))
	mux.Handle("PUT /api/employees/{id}/manager", h(api.SetEmployeeManager))
	mux.Handle("GET /api/employees/{id}/skills", h(api.ListEmployeeSkills))
	mux.Handle("POST /api/employees/{id}/skills", h(api.AssignSkillToEmployee))
	mux.Handle("POST /api/employees/{id}/skills/reset", h(api.ResetEmployeeSkills))
	mux.Handle("DELETE /api/employees/{id}/skills/{skillId}", h(api.UnassignSkillFromEmployee))

	// Employee Memories
	mux.Handle("GET /api/employees/{id}/memories", h(api.ListEmployeeMemories))
	mux.Handle("POST /api/employees/{id}/memories", h(api.AddEmployeeMemory))
	mux.Handle("DELETE /api/employees/{id}/memories/{memoryId}", h(api.DeleteEmployeeMemory))

	// Projects
	mux.Handle("GET /api/projects", h(api.ListProjects))
	mux.Handle("POST /api/projects", h(api.CreateProject))
	mux.Handle("GET /api/projects/{id}", h(api.GetProjectHandler))
	mux.Handle("PUT /api/projects/{id}", h(api.UpdateProject))
	mux.Handle("DELETE /api/projects/{id}", h(api.ArchiveOrDeleteProject))

	// Project Assets
	mux.Handle("GET /api/projects/{id}/assets", h(api.ListProjectAssets))
	mux.Handle("POST /api/projects/{id}/assets", h(api.UploadProjectAsset))
	mux.Handle("GET /api/projects/{id}/assets/{assetId}", h(api.GetProjectAsset))
	mux.Handle("GET /api/projects/{id}/assets/{assetId}/content", h(api.GetProjectAssetContent))
	mux.Handle("PUT /api/projects/{id}/assets/{assetId}", h(api.UpdateProjectAsset))
	mux.Handle("PATCH /api/projects/{id}/assets/{assetId}/meta", h(api.UpdateCreativeMeta))
	mux.Handle("POST /api/projects/{id}/assets/{assetId}/creative", h(api.AddAssetToCreatives))
	mux.Handle("DELETE /api/projects/{id}/assets/{assetId}", h(api.DeleteProjectAsset))
	mux.Handle("POST /api/projects/{id}/assets/reindex", h(api.ReindexProjectAssets))

	// Creatives library (global, cross-project)
	mux.Handle("GET /api/creatives", h(api.ListCreatives))
	mux.Handle("GET /api/creatives/tags", h(api.ListCreativeTags))
	mux.Handle("POST /api/creatives/upload", h(api.UploadCreative))

	// Project Memory
	mux.Handle("GET /api/projects/{id}/memory", h(api.GetProjectMemory))
	mux.Handle("PUT /api/projects/{id}/memory", h(api.UpdateProjectMemory))

	// Delegation
	mux.Handle("POST /api/employees/hire", h(api.HireEmployee))
	mux.Handle("POST /api/tasks/delegate", h(api.DelegateTask))

	// Tasks
	mux.Handle("GET /api/tasks", h(api.ListTasks))
	mux.Handle("POST /api/tasks", h(api.CreateTask))
	mux.Handle("GET /api/tasks/{id}", h(api.GetTask))
	mux.Handle("PUT /api/tasks/{id}", h(api.UpdateTaskFields))
	mux.Handle("DELETE /api/tasks/{id}", h(api.DeleteTask))
	mux.Handle("PUT /api/tasks/{id}/status", h(api.UpdateTaskStatus))
	mux.Handle("GET /api/tasks/{id}/comments", h(api.ListTaskComments))
	mux.Handle("POST /api/tasks/{id}/comments", h(api.AddTaskComment))
	mux.Handle("GET /api/tasks/{id}/runs", h(api.ListTaskRuns))
	mux.Handle("PUT /api/tasks/{id}/schedule", h(api.UpdateTaskSchedule))

	// Models
	mux.Handle("GET /api/models", h(api.ListModels))
	mux.Handle("POST /api/models", h(api.AddModel))
	mux.Handle("DELETE /api/models/{id}", h(api.RemoveModel))

	// Search
	mux.Handle("GET /api/search", h(api.Search))

	// Token Monitor
	mux.Handle("GET /api/tokens/summary", h(api.TokenSummary))
	mux.Handle("GET /api/tokens/timeseries", h(api.TokenTimeseries))
	mux.Handle("GET /api/tokens/breakdown", h(api.TokenBreakdown))
	mux.Handle("GET /api/tokens/details", h(api.TokenDetails))

	// Task Interactions
	mux.Handle("GET /api/tasks/{id}/interactions", h(api.ListInteractions))
	mux.Handle("PUT /api/tasks/{id}/interactions/{interactionId}", h(api.ResolveInteraction))

	// Events
	mux.Handle("GET /api/events", h(api.ListEvents))
	mux.Handle("GET /api/events/stats", h(api.EventStats))
	mux.Handle("GET /api/events/ws", h(eventsWSHandler(eventPipeline)))

	// Chat
	mux.Handle("POST /api/chat", h(api.Chat))

	// File upload
	mux.Handle("POST /api/upload", h(api.UploadFile))

	// Playable Previews
	mux.HandleFunc("GET /playable-preview/{pipeline_id}", api.PlayablePreviewRedirect)
	mux.Handle("GET /playable-preview/{pipeline_id}/{path...}", h(api.PlayablePreviewHandler))

	// Observability (plan 7.5): top-level unauthenticated probes + metrics.
	// Registered without logMW so per-few-seconds orchestrator probes don't
	// flood the access log.
	mux.HandleFunc("GET /live", liveHandler)
	mux.Handle("GET /ready", readyHandler(pgClient))
	mux.Handle("GET /metrics", metricsHandler(eventPipeline))

	// Static files
	staticDir := "backend/static"
	if _, err := os.Stat(staticDir); os.IsNotExist(err) {
		staticDir = "static"
	}
	mux.Handle("/", logMW(http.FileServer(http.Dir(staticDir))))

	port := cfg.Server.Port
	if port == 0 {
		port = 1983
	}
	host := cfg.Server.Host
	if host == "" {
		host = "127.0.0.1"
	}
	if !httpapi.IsLoopbackHost(host) && !auth.Enabled() {
		fmt.Fprintf(os.Stderr, "FATAL: refusing to bind %s without authentication: set server.api_token or MOBIUS_API_TOKEN\n", host)
		os.Exit(1)
	}

	slog.Info("Starting Mobius",
		slog.String("host", host),
		slog.Int("port", port),
		slog.String("mode", cfg.Server.Mode),
		slog.String("log_format", map[bool]string{true: "text", false: "json"}[debug]),
		slog.String("log_dir", logDir),
	)

	server := &http.Server{
		Addr:         net.JoinHostPort(host, strconv.Itoa(port)),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Event pipeline goroutine
	if eventPipeline != nil {
		go eventPipeline.Start(syncCtx)
	}

	// Token usage pipeline goroutine
	if api.TokenPipeline() != nil {
		go api.TokenPipeline().Start(syncCtx)
	}

	// Event archiver (GCS + ES pruning)
	if esClient != nil && gcsClient != nil {
		go StartArchiver(syncCtx, cfg, esClient, gcsClient)
	}

	// MCP Server for external agents
	var mcpServer *MCPServer
	if pgClient != nil {
		mcpServer = NewMCPServer(pgClient, esClient, cfg)
		slog.Info("MCP server initialized", "tools", len(mcpServer.tools))
		mux.Handle("/mcp", mcpWebSocketHandler(mcpServer))
	}

	// Agent adapter registry
	adapterRegistry := NewAdapterRegistry()
	adapterRegistry.Register(AdapterInternal, NewInternalLLMAdapter(
		providers, pgClient, esClient, bqClient, gcsClient, cfg, api.TokenPipeline(), eventPipeline,
	))
	var claudeMintSession func(agentID, taskID string) string
	if mcpServer != nil {
		claudeMintSession = mcpServer.MintSession
	}
	adapterRegistry.Register(AdapterClaudeCode, NewClaudeCodeAdapter(
		cfg, fmt.Sprintf("ws://localhost:%d/mcp", port), claudeMintSession,
	))
	adapterRegistry.Register(AdapterBash, NewBashAdapter(cfg))
	httpWebhookAdapter := NewHTTPWebhookAdapter()
	adapterRegistry.Register(AdapterHTTPWebhook, httpWebhookAdapter)
	// Callback for external webhook workers to report run completion.
	mux.Handle("POST /api/runs/complete", h(httpWebhookAdapter.CompleteRunHandler))

	// Background task dispatcher
	if pgClient != nil {
		dispatcher := NewTaskDispatcher(pgClient, esClient, api.TokenPipeline(), adapterRegistry, 5, cfg, eventPipeline)
		go dispatcher.Start(syncCtx)
	}

	// Periodic disk→ES sync (every 60s)
	if esClient != nil {
		go func() {
			ticker := time.NewTicker(60 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					added, updated, err := skills.SyncFromDisk(syncCtx, esClient, pgClient, skillsDir)
					if err != nil {
						slog.Error("disk sync failed", "error", err)
					} else if added+updated > 0 {
						slog.Info("disk sync", "added", added, "updated", updated)
					}
				case <-syncCtx.Done():
					return
				}
			}
		}()

		// Daily upstream sync at 2am
		go func() {
			for {
				now := time.Now()
				next := time.Date(now.Year(), now.Month(), now.Day(), 2, 0, 0, 0, now.Location())
				if now.After(next) {
					next = next.Add(24 * time.Hour)
				}
				slog.Info("next upstream skill sync scheduled", "at", next.Format(time.RFC3339))
				timer := time.NewTimer(time.Until(next))
				select {
				case <-timer.C:
					slog.Info("running scheduled upstream skill sync")
					api.RunFullSync(syncCtx)
				case <-syncCtx.Done():
					timer.Stop()
					return
				}
			}
		}()
	}

	shutdownCh := make(chan os.Signal, 1)
	signal.Notify(shutdownCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("listen failed", "error", err)
			os.Exit(1)
		}
	}()

	sig := <-shutdownCh
	slog.Info("Shutdown signal received", "signal", sig.String())

	syncCancel()
	bgWG.Wait()
	slog.Info("background goroutines drained")

	if eventPipeline != nil {
		eventPipeline.Wait()
		slog.Info("event pipeline drained")
	}

	if api.TokenPipeline() != nil {
		api.TokenPipeline().Wait()
		slog.Info("token pipeline drained")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("HTTP server forced shutdown", "error", err)
	}

	api.Shutdown(shutdownCtx)

	slog.Info("Server stopped gracefully")
}

func hydrateConversations(ctx context.Context, es *ESClient, store *ConversationStore) error {
	convList, err := es.ListAllConversations(ctx)
	if err != nil {
		return fmt.Errorf("list conversations: %w", err)
	}

	convMap := make(map[string]*Conversation, len(convList))
	for i := range convList {
		c := &convList[i]
		msgs, err := es.ListMessages(ctx, c.ID)
		if err != nil {
			slog.Error("failed to load messages for conversation", "id", c.ID, "error", err)
			msgs = []Message{}
		}
		c.Messages = msgs
		convMap[c.ID] = c
	}

	store.Hydrate(convMap)
	slog.Info("conversations hydrated from ES", "count", len(convMap))
	return nil
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
