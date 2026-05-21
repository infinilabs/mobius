package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
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

	ctx := context.Background()
	genaiClient, err := NewGenAIClient(ctx, cfg)
	if err != nil {
		slog.Error("failed to init Vertex AI client", "error", err)
		slog.Warn("Vertex AI chat will be unavailable until config is fixed")
		genaiClient = nil
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
	}

	api := NewAPIHandler(cfg, configPath, genaiClient, esClient, gcsClient, pgClient)

	if esClient != nil {
		if err := hydrateConversations(ctx, esClient, api.conversations); err != nil {
			slog.Error("failed to hydrate conversations from ES", "error", err)
		}

		promptsDir := "prompts"
		if _, err := os.Stat(promptsDir); os.IsNotExist(err) {
			promptsDir = "../prompts"
		}
		if _, err := os.Stat(promptsDir); err == nil {
			if err := seedPrompts(ctx, esClient, promptsDir); err != nil {
				slog.Error("failed to seed prompts", "error", err)
			}
		}
	}

	if pgClient != nil {
		if err := pgClient.SeedDefaultEmployees(ctx); err != nil {
			slog.Error("failed to seed default employees", "error", err)
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

	h := func(f http.HandlerFunc) http.Handler { return logMW(f) }

	// Config & settings
	mux.Handle("/api/health", h(api.HealthCheck))
	mux.Handle("/api/config", h(api.GetConfig))
	mux.Handle("GET /api/settings", h(api.GetSettings))
	mux.Handle("PUT /api/settings", h(api.UpdateSettings))

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

	// Employees
	mux.Handle("GET /api/employees", h(api.ListEmployees))
	mux.Handle("POST /api/employees", h(api.CreateEmployee))
	mux.Handle("GET /api/employees/{id}", h(api.GetEmployee))
	mux.Handle("PUT /api/employees/{id}", h(api.UpdateEmployee))
	mux.Handle("DELETE /api/employees/{id}", h(api.DeleteEmployee))
	mux.Handle("PUT /api/employees/{id}/manager", h(api.SetEmployeeManager))

	// Models
	mux.Handle("GET /api/models", h(api.ListModels))
	mux.Handle("POST /api/models", h(api.AddModel))
	mux.Handle("DELETE /api/models/{id}", h(api.RemoveModel))

	// Chat
	mux.Handle("POST /api/chat", h(api.Chat))

	// File upload
	mux.Handle("POST /api/upload", h(api.UploadFile))

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

	slog.Info("Starting Mobius",
		slog.Int("port", port),
		slog.String("mode", cfg.Server.Mode),
		slog.String("log_format", map[bool]string{true: "text", false: "json"}[debug]),
		slog.String("log_dir", logDir),
	)

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second,
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
