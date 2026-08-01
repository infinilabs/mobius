package tools

import (
	"context"
	"fmt"
	"log/slog"
	"mobius/internal/config"
	"mobius/internal/domain"
	evbus "mobius/internal/events"
	"mobius/internal/gcs"
	"mobius/internal/storage/postgres"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ExecWatermarkAssetsTool handles the watermark_assets tool call.
func ExecWatermarkAssetsTool(ctx context.Context, pg *postgres.Client, gcs *gcs.Client, cfg *config.Config, events *evbus.EventPipeline, agentID string, args map[string]any) map[string]any {
	inputPath, _ := args["input_path"].(string)
	outputPath, _ := args["output_path"].(string)
	message, _ := args["message"].(string)
	password, _ := args["password"].(string)
	intensityVal, _ := args["intensity"].(float64) // JSON numbers parse as float64
	intensity := int(intensityVal)

	if inputPath == "" || outputPath == "" || message == "" || password == "" {
		return map[string]any{"error": "input_path, output_path, message, and password are required"}
	}

	if len(password) < 4 {
		return map[string]any{"error": "password must be at least 4 characters"}
	}

	bucketPrefix := fmt.Sprintf("gs://%s/", cfg.GoogleCloud.GCS.Bucket)

	var isInputLocal bool
	var inputGCSPath string
	if strings.HasPrefix(inputPath, "gs://") {
		if !strings.HasPrefix(inputPath, bucketPrefix) {
			return map[string]any{"error": fmt.Sprintf("gcs paths must be inside the configured bucket gs://%s/", cfg.GoogleCloud.GCS.Bucket)}
		}
		inputGCSPath = strings.TrimPrefix(inputPath, bucketPrefix)
	} else if strings.HasPrefix(inputPath, "/") {
		isInputLocal = true
	} else {
		inputGCSPath = inputPath
	}

	if !isInputLocal && gcs == nil {
		return map[string]any{"error": "GCS is not configured; provide a local file path instead (starting with /)"}
	}

	var outputGCSKey string
	if strings.HasPrefix(outputPath, "gs://") {
		if !strings.HasPrefix(outputPath, bucketPrefix) {
			return map[string]any{"error": fmt.Sprintf("gcs paths must be inside the configured bucket gs://%s/", cfg.GoogleCloud.GCS.Bucket)}
		}
		outputGCSKey = strings.TrimPrefix(outputPath, bucketPrefix)
	} else {
		outputGCSKey = outputPath
	}

	engine := NewWatermarkEngine(cfg.Sandbox)

	isBulk := strings.Contains(inputPath, "*")
	isVideo := IsVideoFile(inputPath)

	if isBulk || isVideo {
		if pg == nil {
			return map[string]any{"error": "Database is required for asynchronous watermarking tasks"}
		}
		jobID := generateJobID()

		assigneeID := ""
		if employees, err := pg.ListEmployees(ctx); err == nil {
			for _, emp := range employees {
				if emp.Title == "Media Asset Watermarker" || emp.Name == "Asset Watermarker" {
					assigneeID = emp.ID
					break
				}
			}
		}
		if assigneeID == "" && agentID != "" {
			assigneeID = agentID
		}

		t := &domain.Task{
			Title:    "Watermark GCS Assets - Job " + jobID,
			Body:     fmt.Sprintf("Watermark assets from %s to %s with message %s", inputPath, outputPath, message),
			Status:   "todo",
			Priority: "medium",
		}
		if assigneeID != "" {
			t.Assignee = &domain.EmployeeBrief{ID: assigneeID}
		}
		if agentID != "" {
			t.Creator = &domain.EmployeeBrief{ID: agentID}
		}

		projectID, _ := args["_project_id"].(string)
		if projectID != "" {
			t.ProjectID = &projectID
		}

		if err := pg.CreateTask(ctx, t, nil); err != nil {
			return map[string]any{"error": "failed to create async task: " + err.Error()}
		}

		driver := NewWatermarkTaskDriver(pg, gcs, engine, assigneeID, password)
		detachedCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		go func() {
			defer cancel()
			driver.Run(detachedCtx, t.ID, inputGCSPath, outputGCSKey, message, password, intensity)
		}()

		pg.ReindexTask(ctx, t.ID)

		if events != nil {
			var aid *string
			if agentID != "" {
				aid = &agentID
			}
			events.Publish(evbus.New("watermark_job_created", aid, nil, nil, map[string]any{
				"task_id": t.ID, "job_id": jobID, "input_path": inputPath,
			}))
		}

		return map[string]any{
			"status":  "task_created",
			"task_id": t.ID,
			"message": "Watermarking job spawned. Monitor progress in the Tasks panel.",
		}
	}

	var localIn string
	if isInputLocal {
		validated, err := validateLocalInput(inputPath, UploadsDir)
		if err != nil {
			return map[string]any{"error": err.Error()}
		}
		localIn = validated
		if _, err := os.Stat(localIn); err != nil {
			return map[string]any{"error": fmt.Sprintf("local input file not found: %s", localIn)}
		}
	} else {
		fIn, err := os.CreateTemp("", "mobius-wm-sync-in-*.png")
		if err != nil {
			return map[string]any{"error": "failed to create temp file: " + err.Error()}
		}
		localIn = fIn.Name()
		fIn.Close()
		defer os.Remove(localIn)

		if err := gcs.Download(ctx, inputGCSPath, localIn); err != nil {
			return map[string]any{"error": "failed to download input GCS file: " + err.Error()}
		}
	}

	fOut, err := os.CreateTemp("", "mobius-wm-sync-out-*.png")
	if err != nil {
		return map[string]any{"error": "failed to create output temp file: " + err.Error()}
	}
	localOut := fOut.Name()
	fOut.Close()
	defer os.Remove(localOut)

	if err := engine.EmbedImage(ctx, localIn, localOut, message, password, intensity); err != nil {
		return map[string]any{"error": "watermark embedding failed: " + err.Error()}
	}

	ext := filepath.Ext(inputPath)
	baseName := strings.TrimSuffix(filepath.Base(inputPath), ext)
	prefix := outputGCSKey
	fileID := baseName
	outExt := ".png"

	if prefix != "" && !strings.HasSuffix(prefix, "/") && filepath.Ext(prefix) != "" {
		fileID = strings.TrimSuffix(filepath.Base(prefix), filepath.Ext(prefix))
		prefix = filepath.Dir(prefix)
	} else {
		prefix = strings.TrimSuffix(prefix, "/")
	}

	fOutRead, err := os.Open(localOut)
	if err != nil {
		return map[string]any{"error": "failed to read output temp file: " + err.Error()}
	}
	defer fOutRead.Close()

	gcsURI, err := gcs.Upload(ctx, prefix, fileID, outExt, fOutRead, "image/png")
	if err != nil {
		return map[string]any{"error": "failed to upload output back to GCS: " + err.Error()}
	}

	slog.Info("synchronous watermarking complete", "input", inputPath, "output", gcsURI)

	return map[string]any{
		"status":      "completed",
		"output_path": gcsURI,
	}
}

// ExecVerifyWatermarkTool handles verify_watermark tool call.
func ExecVerifyWatermarkTool(ctx context.Context, gcs *gcs.Client, cfg *config.Config, agentID string, args map[string]any) map[string]any {
	inputPath, _ := args["input_path"].(string)
	password, _ := args["password"].(string)

	if inputPath == "" || password == "" {
		return map[string]any{"error": "input_path and password are required"}
	}

	bucketPrefix := fmt.Sprintf("gs://%s/", cfg.GoogleCloud.GCS.Bucket)
	var isInputLocal bool
	var inputGCSPath string
	if strings.HasPrefix(inputPath, "gs://") {
		if !strings.HasPrefix(inputPath, bucketPrefix) {
			return map[string]any{"error": fmt.Sprintf("gcs paths must be inside the configured bucket gs://%s/", cfg.GoogleCloud.GCS.Bucket)}
		}
		inputGCSPath = strings.TrimPrefix(inputPath, bucketPrefix)
	} else if strings.HasPrefix(inputPath, "/") {
		isInputLocal = true
	} else {
		inputGCSPath = inputPath
	}

	if !isInputLocal && gcs == nil {
		return map[string]any{"error": "GCS is not configured; provide a local file path instead (starting with /)"}
	}

	engine := NewWatermarkEngine(cfg.Sandbox)

	var localPath string
	if isInputLocal {
		validated, err := validateLocalInput(inputPath, UploadsDir)
		if err != nil {
			return map[string]any{"error": err.Error()}
		}
		localPath = validated
		if _, err := os.Stat(localPath); err != nil {
			return map[string]any{"error": fmt.Sprintf("local input file not found: %s", localPath)}
		}
	} else {
		ext := filepath.Ext(inputPath)
		fIn, err := os.CreateTemp("", "mobius-wm-verify-*"+ext)
		if err != nil {
			return map[string]any{"error": "failed to create temp file: " + err.Error()}
		}
		localPath = fIn.Name()
		fIn.Close()
		defer os.Remove(localPath)

		if err := gcs.Download(ctx, inputGCSPath, localPath); err != nil {
			return map[string]any{"error": "failed to download verify target from GCS: " + err.Error()}
		}
	}

	isVid := IsVideoFile(inputPath)
	var detected bool
	var confidence float64
	var message string
	var err error

	if isVid {
		detected, confidence, message, err = engine.VerifyVideo(ctx, localPath, password)
	} else {
		detected, confidence, message, err = engine.VerifyImage(ctx, localPath, password)
	}

	if err != nil {
		return map[string]any{"error": "verification process failed: " + err.Error()}
	}

	if !detected {
		return map[string]any{
			"watermark_detected": false,
			"message":            "No watermark detected or password incorrect.",
		}
	}

	return map[string]any{
		"watermark_detected": true,
		"confidence":         fmt.Sprintf("%.1f%%", confidence),
		"extracted_message":  message,
	}
}

func generateJobID() string {
	return strings.ToLower(fmt.Sprintf("%d", time.Now().UnixNano()))
}

func validateLocalInput(p, allowedUploadDir string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	root, err := filepath.Abs(allowedUploadDir)
	if err != nil {
		return "", fmt.Errorf("invalid upload directory config: %w", err)
	}
	if !strings.HasPrefix(abs+string(os.PathSeparator), root+string(os.PathSeparator)) {
		return "", fmt.Errorf("local input path %q is outside the allowed upload directory %q", p, allowedUploadDir)
	}
	return abs, nil
}
