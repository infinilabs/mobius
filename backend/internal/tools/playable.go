// Package tools holds the agent tool implementations shared by the
// autonomous adapter, interactive chat, and MCP (plan 6.4e): the playable
// pipeline, media generation, media tagging, and watermarking.
package tools

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"mime"
	"mobius/internal/config"
	"mobius/internal/domain"
	"mobius/internal/gcs"
	"mobius/internal/llm"
	"mobius/internal/search"
	"mobius/internal/storage/postgres"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"google.golang.org/genai"
)

// UploadsDir is where chat uploads land before being saved to project assets.
const UploadsDir = "tmp/uploads"

// Playable-ad pipeline tools and media generation.
// Split from adapter_internal_tools.go (plan 6.5).

// ResolvePlayableProjectID resolves the active project for a playable tool call.
// The autonomous run path supplies a *domain.Task; the interactive chat path injects
// _project_id into the tool args (see chat.go). Both routers call this helper so
// they share one resolution rule.
func ResolvePlayableProjectID(task *domain.Task, args map[string]any) string {
	if task != nil && task.ProjectID != nil && *task.ProjectID != "" {
		return *task.ProjectID
	}
	if pid, ok := args["_project_id"].(string); ok && pid != "" {
		return pid
	}
	if pid, ok := args["project_id"].(string); ok && pid != "" {
		return pid
	}
	return ""
}

func ExecPlayableLoadReferenceGameTool(config *config.Config, args map[string]any) map[string]any {
	gameType, _ := args["game_type"].(string)
	if gameType == "" {
		return map[string]any{"error": "game_type is required"}
	}

	baseDir := "templates"
	if len(config.Projects.TemplateDirs) > 0 {
		baseDir = config.Projects.TemplateDirs[0]
	}

	content, err := loadReferenceGameImpl(baseDir, gameType)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"html_template": content}
}

func ExecPlayableGetTrackingSDKTool() map[string]any {
	sdkPath := filepath.Join("static", "tracking", "sdk.js")
	content, err := getTrackingSDKImpl(sdkPath)
	if err != nil {
		content, err = getTrackingSDKImpl(filepath.Join("backend", sdkPath))
		if err != nil {
			return map[string]any{"error": "failed to load tracking SDK: " + err.Error()}
		}
	}
	return map[string]any{"tracking_sdk": content}
}

func ExecPlayableGetWebAudioSFXTool() map[string]any {
	return map[string]any{"web_audio_sfx_helpers": getWebAudioSFXImpl()}
}

func ExecPlayableWriteHTMLTool(ctx context.Context, config *config.Config, pg *postgres.Client, projectID string, args map[string]any) map[string]any {
	htmlContent, _ := args["html_content"].(string)
	pipelineID, _ := args["pipeline_id"].(string)
	if htmlContent == "" || pipelineID == "" {
		return map[string]any{"error": "html_content and pipeline_id are required"}
	}
	if projectID == "" {
		return map[string]any{"error": "project context required"}
	}

	projectDir := PlayableProjectDir(ctx, pg, config, projectID)

	valScript := filepath.Join("static", "validation", "playwright_validation.js")
	if _, err := os.Stat(valScript); os.IsNotExist(err) {
		valScript = filepath.Join("backend", valScript)
	}

	report, err := writeHTMLImpl(projectDir, pipelineID, htmlContent, valScript)
	if err != nil {
		return map[string]any{"error": "compile failed: " + err.Error()}
	}

	return map[string]any{
		"status":              "compiled",
		"passed":              report.Passed,
		"errors":              report.Errors,
		"preview_inline_path": filepath.Join("output", pipelineID, "preview_inline.html"),
	}
}

// ExecSaveUploadToAssetsTool copies a file the user uploaded in chat into the project's
// asset library and indexes it, so an uploaded image can become part of a playable ad.
// srcFile is the most recent chat upload, resolved by the caller from the conversation;
// it is nil in autonomous (non-chat) runs, where there is no upload to save.
func ExecSaveUploadToAssetsTool(ctx context.Context, gcs *gcs.Client, es *search.Client, pg *postgres.Client, config *config.Config, projectID string, srcFile *domain.FileRef, args map[string]any) map[string]any {
	if es == nil || pg == nil {
		return map[string]any{"error": "required services not available"}
	}
	if projectID == "" {
		return map[string]any{"error": "project context required"}
	}
	if srcFile == nil {
		return map[string]any{"error": "no uploaded file found in this conversation to add to assets"}
	}

	project, err := pg.GetProject(ctx, projectID)
	if err != nil {
		return map[string]any{"error": "project not found: " + err.Error()}
	}

	relativePath, _ := args["relative_path"].(string)
	if relativePath == "" {
		name := filepath.Base(srcFile.Name)
		if name == "" || name == "." || name == "/" {
			name = srcFile.ID
		}
		relativePath = filepath.Join("assets", name)
	}

	root := project.RootDir(config.Projects.ProjectsDir)
	fullPath, err := domain.ResolveWithinRoot(root, relativePath)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return map[string]any{"error": "failed to create asset dir: " + err.Error()}
	}

	// Fetch the uploaded bytes into the project from GCS or the local uploads dir.
	if srcFile.GCSURI != "" && gcs != nil {
		if err := gcs.DownloadURI(ctx, srcFile.GCSURI, fullPath); err != nil {
			return map[string]any{"error": "failed to fetch uploaded file from storage: " + err.Error()}
		}
	} else {
		localSrc := filepath.Join(UploadsDir, srcFile.ID+filepath.Ext(srcFile.Name))
		data, rerr := os.ReadFile(localSrc)
		if rerr != nil {
			return map[string]any{"error": "uploaded file is no longer available: " + rerr.Error()}
		}
		if werr := os.WriteFile(fullPath, data, 0644); werr != nil {
			return map[string]any{"error": "failed to write asset: " + werr.Error()}
		}
	}

	buf, err := os.ReadFile(fullPath)
	if err != nil {
		buf = []byte{}
	}

	mimeType := srcFile.MIMEType
	if mimeType == "" {
		mimeType = domain.ResolveMimeType(srcFile.Name, "")
	}
	ct := domain.ClassifyContentType(mimeType)
	var content string
	var truncated bool
	if domain.IsTextIndexable(ct) && len(buf) > 0 {
		maxIdx := config.Projects.ContentMaxIndex
		if len(buf) > maxIdx {
			content = string(buf[:maxIdx])
			truncated = true
		} else {
			content = string(buf)
		}
	}

	var tags []string
	if v, ok := args["tags"].([]any); ok {
		for _, t := range v {
			if s, ok := t.(string); ok {
				tags = append(tags, s)
			}
		}
	}
	if len(tags) == 0 {
		tags = []string{"uploaded"}
	}

	now := time.Now().Format(time.RFC3339)
	asset := &domain.ProjectAsset{
		ID:               domain.NewID(),
		ProjectID:        projectID,
		Filename:         filepath.Base(relativePath),
		RelativePath:     relativePath,
		MIMEType:         mimeType,
		SizeBytes:        int64(len(buf)),
		Content:          content,
		ContentTruncated: truncated,
		ContentType:      ct,
		GCSStatus:        "pending",
		Checksum:         domain.CalculateSHA256(buf),
		Tags:             tags,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := es.IndexProjectAsset(ctx, asset); err != nil {
		return map[string]any{"error": "failed to index asset: " + err.Error()}
	}
	if gcs != nil {
		go UploadAssetToGCS(config, gcs, es, project, asset.ID, fullPath, relativePath)
	}

	slog.Info("chat upload saved to project assets", "project", project.Name, "path", relativePath, "asset_id", asset.ID)
	return map[string]any{
		"status":        "success",
		"asset_id":      asset.ID,
		"relative_path": relativePath,
		"content_type":  ct,
		"tags":          tags,
	}
}

// PlayableProjectDir resolves the on-disk working directory for a playable project.
// It uses the project's canonical RootDir (keyed by Name) so generated and published
// files land where ReindexProjectAssets and GCS sync expect them. Falls back to
// ProjectsDir/<projectID> only when the project cannot be resolved (e.g. no PG client).
func PlayableProjectDir(ctx context.Context, pg *postgres.Client, config *config.Config, projectID string) string {
	if pg != nil {
		if project, err := pg.GetProject(ctx, projectID); err == nil {
			return project.RootDir(config.Projects.ProjectsDir)
		}
	}
	return filepath.Join(config.Projects.ProjectsDir, projectID)
}

func ExecGenerateImageTool(ctx context.Context, config *config.Config, providers *llm.ProviderRegistry, gcs *gcs.Client, es *search.Client, pg *postgres.Client, projectID string, args map[string]any) map[string]any {
	prompt, _ := args["prompt"].(string)
	size, _ := args["size"].(string)
	outPath, _ := args["output_path"].(string)

	if prompt == "" || outPath == "" {
		return map[string]any{"error": "prompt and output_path are required"}
	}
	if projectID == "" {
		return map[string]any{"error": "project context required"}
	}
	if size == "" {
		size = "512x512"
	}

	// Resolve the canonical project so generated files land under RootDir (reindexable +
	// GCS-synced). Fall back to ProjectsDir/<id> only when the project can't be resolved.
	var project *domain.Project
	projectDir := filepath.Join(config.Projects.ProjectsDir, projectID)
	if pg != nil {
		if p, perr := pg.GetProject(ctx, projectID); perr == nil {
			project = p
			projectDir = p.RootDir(config.Projects.ProjectsDir)
		}
	}
	absOutPath := filepath.Join(projectDir, outPath)

	os.MkdirAll(filepath.Dir(absOutPath), 0755)

	err := generateImageAsset(ctx, config, providers, prompt, size, absOutPath)
	if err != nil {
		return map[string]any{"error": "image generation failed: " + err.Error()}
	}

	transparent, _ := args["transparent"].(bool)
	if transparent {
		if perr := postProcessSprite(absOutPath); perr != nil {
			slog.Warn("sprite post-processing failed; keeping raw image", "path", absOutPath, "error", perr)
		}
	}

	// Register the generated image in the asset library (best-effort) so it appears in the
	// project AssetsTab and Creatives immediately. The file is also under RootDir, so a
	// reindex would recover it even if indexing here fails.
	if es != nil {
		registerGeneratedImageAsset(ctx, gcs, es, config, project, projectID, outPath, absOutPath, transparent)
	}

	return map[string]any{"status": "success", "output_path": outPath}
}

// registerGeneratedImageAsset indexes a freshly generated image as a domain.ProjectAsset and
// kicks off async GCS sync, mirroring UploadProjectAsset.
func registerGeneratedImageAsset(ctx context.Context, gcs *gcs.Client, es *search.Client, config *config.Config, project *domain.Project, projectID, relPath, absPath string, transparent bool) {
	relativePath := filepath.Clean(relPath)
	buf, rerr := os.ReadFile(absPath)
	if rerr != nil {
		slog.Warn("generated image unreadable for asset registration", "path", absPath, "error", rerr)
		return
	}

	mimeType := domain.ResolveMimeType(relativePath, "")
	tags := []string{"ai_generated"}
	if transparent {
		tags = append(tags, "playable_asset")
	}

	now := time.Now().Format(time.RFC3339)
	asset := &domain.ProjectAsset{
		ID:           domain.NewID(),
		ProjectID:    projectID,
		Filename:     filepath.Base(relativePath),
		RelativePath: relativePath,
		MIMEType:     mimeType,
		SizeBytes:    int64(len(buf)),
		ContentType:  domain.ClassifyContentType(mimeType),
		GCSStatus:    "pending",
		Checksum:     domain.CalculateSHA256(buf),
		Tags:         tags,
		Title:        filepath.Base(relativePath),
		Status:       "draft",
		Origin:       "ai_generated",
		AspectRatio:  domain.ComputeAspectRatio(absPath, domain.ClassifyContentType(mimeType)),
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := es.IndexProjectAsset(ctx, asset); err != nil {
		slog.Warn("failed to index generated image asset", "path", relativePath, "error", err)
		return
	}
	if gcs != nil && project != nil {
		go UploadAssetToGCS(config, gcs, es, project, asset.ID, absPath, relativePath)
	}
	slog.Info("generated image registered as asset", "project_id", projectID, "path", relativePath, "asset_id", asset.ID)
}

func ExecGenerateAudioTool(ctx context.Context, config *config.Config, pg *postgres.Client, projectID string, args map[string]any) map[string]any {
	prompt, _ := args["prompt"].(string)
	duration, _ := args["duration_sec"].(float64)
	outPath, _ := args["output_path"].(string)

	if prompt == "" || outPath == "" {
		return map[string]any{"error": "prompt and output_path are required"}
	}
	if projectID == "" {
		return map[string]any{"error": "project context required"}
	}
	if duration <= 0 {
		duration = 5
	}

	projectDir := PlayableProjectDir(ctx, pg, config, projectID)
	absOutPath := filepath.Join(projectDir, outPath)

	os.MkdirAll(filepath.Dir(absOutPath), 0755)

	err := generateAudioAsset(ctx, prompt, int(duration), absOutPath)
	if err != nil {
		return map[string]any{"error": "audio generation failed: " + err.Error()}
	}

	return map[string]any{"status": "success", "output_path": outPath}
}

func ExecPublishPlayableAdTool(ctx context.Context, gcs *gcs.Client, es *search.Client, config *config.Config, pg *postgres.Client, projectID string, args map[string]any) map[string]any {
	pipelineID, _ := args["pipeline_id"].(string)
	if pipelineID == "" {
		return map[string]any{"error": "pipeline_id is required"}
	}
	if projectID == "" {
		return map[string]any{"error": "project context required"}
	}

	publishToGCS, _ := args["publish_to_gcs"].(bool)

	projectDir := PlayableProjectDir(ctx, pg, config, projectID)

	url, gcsURI, err := publishPlayableAdImpl(ctx, gcs, config, projectDir, pipelineID, publishToGCS)
	if err != nil {
		return map[string]any{"error": "publish failed: " + err.Error()}
	}

	// Register the asset in Elasticsearch (creatives library)
	if es != nil {
		outDir := filepath.Join(projectDir, "output", pipelineID)
		inlinePath := filepath.Join(outDir, "preview_inline.html")

		var sizeBytes int64
		var content string
		var checksum string

		if data, err := os.ReadFile(inlinePath); err == nil {
			sizeBytes = int64(len(data))
			checksum = domain.CalculateSHA256(data)

			maxIdx := config.Projects.ContentMaxIndex
			if maxIdx <= 0 {
				maxIdx = 100000
			}
			if len(data) > maxIdx {
				content = string(data[:maxIdx])
			} else {
				content = string(data)
			}
		} else {
			slog.Warn("Failed to read compiled playable for asset registration", "path", inlinePath, "error", err)
		}

		now := time.Now().Format(time.RFC3339)
		relativePath := filepath.Join("output", pipelineID, "preview_inline.html")

		gcsStatus := "none"
		if gcsURI != "" {
			gcsStatus = "uploaded"
		}

		asset := &domain.ProjectAsset{
			ID:           domain.NewID(),
			ProjectID:    projectID,
			Filename:     "preview_inline.html",
			RelativePath: relativePath,
			MIMEType:     "text/html",
			SizeBytes:    sizeBytes,
			Content:      content,
			ContentType:  "document",
			GCSStatus:    gcsStatus,
			GCSURI:       gcsURI,
			Checksum:     checksum,
			Tags:         []string{"playable", "html5_ad"},
			CreatedAt:    now,
			UpdatedAt:    now,
		}

		if err := es.IndexProjectAsset(ctx, asset); err != nil {
			slog.Warn("Failed to index playable asset in ES", "error", err)
		} else {
			slog.Info("Playable registered in project creatives library", "project", projectID, "path", relativePath)
		}
	}

	status := "registered"
	if publishToGCS && gcsURI != "" {
		status = "published"
	}

	return map[string]any{
		"status":         status,
		"production_url": url,
		"preview_url":    url,
	}
}

func generateImageAsset(ctx context.Context, config *config.Config, providers *llm.ProviderRegistry, prompt, size, absOutPath string) error {
	settings := config.GetSettings()
	if settings.GoogleCloud.APIKey == "" && os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") == "" {
		dummyPng := []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13, 73, 72, 68, 82, 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0, 31, 21, 196, 137, 0, 0, 0, 10, 73, 68, 65, 84, 120, 156, 99, 0, 1, 0, 0, 5, 0, 1, 13, 10, 45, 180, 0, 0, 0, 0, 73, 69, 78, 68, 174, 66, 96, 130}
		slog.Warn("No Google Cloud credentials, writing dummy PNG for local test")
		return os.WriteFile(absOutPath, dummyPng, 0644)
	}

	if providers == nil {
		dummyPng := []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13, 73, 72, 68, 82, 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0, 31, 21, 196, 137, 0, 0, 0, 10, 73, 68, 65, 84, 120, 156, 99, 0, 1, 0, 0, 5, 0, 1, 13, 10, 45, 180, 0, 0, 0, 0, 73, 69, 78, 68, 174, 66, 96, 130}
		slog.Warn("No provider registry available, writing dummy PNG for local test")
		return os.WriteFile(absOutPath, dummyPng, 0644)
	}

	gp, ok := providers.Get("gemini").(*llm.GeminiProvider)
	if !ok || gp == nil {
		return fmt.Errorf("gemini provider not found or misconfigured")
	}

	modelID, _ := config.GoogleCloud.VertexAI.DefaultModel("image")
	if modelID == "" {
		modelID = "imagen-3.0-generate-002"
	}

	client := gp.ClientForModel(modelID)
	if client == nil {
		return fmt.Errorf("gemini client for model %s not initialized", modelID)
	}

	aspectRatio := mapSizeToAspectRatio(size)

	imgConfig := &genai.GenerateImagesConfig{
		NumberOfImages: 1,
		OutputMIMEType: "image/png",
		AspectRatio:    aspectRatio,
	}

	slog.Info("Generating image using Imagen 3", "prompt", prompt, "model", modelID, "aspect_ratio", aspectRatio)
	response, err := client.Models.GenerateImages(ctx, modelID, prompt, imgConfig)
	if err != nil {
		return fmt.Errorf("imagen generation failed: %w", err)
	}

	if len(response.GeneratedImages) == 0 || response.GeneratedImages[0].Image == nil {
		return fmt.Errorf("imagen returned no images")
	}

	imgBytes := response.GeneratedImages[0].Image.ImageBytes
	if len(imgBytes) == 0 {
		return fmt.Errorf("imagen returned empty image bytes")
	}

	if err := os.MkdirAll(filepath.Dir(absOutPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory for image: %w", err)
	}

	return os.WriteFile(absOutPath, imgBytes, 0644)
}

func generateAudioAsset(ctx context.Context, prompt string, duration int, absOutPath string) error {
	slog.Info("Generating procedural ambient WAV", "prompt", prompt, "duration", duration, "path", absOutPath)
	if err := os.MkdirAll(filepath.Dir(absOutPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory for audio: %w", err)
	}
	return generateAmbientWav(float64(duration), absOutPath)
}

func mapSizeToAspectRatio(size string) string {
	parts := strings.Split(size, "x")
	if len(parts) != 2 {
		return "1:1"
	}
	var w, h float64
	fmt.Sscanf(parts[0], "%f", &w)
	fmt.Sscanf(parts[1], "%f", &h)
	if w <= 0 || h <= 0 {
		return "1:1"
	}
	ratio := w / h
	if ratio == 1.0 {
		return "1:1"
	}
	if ratio > 1.0 {
		if ratio > 1.5 {
			return "16:9"
		}
		return "4:3"
	} else {
		if ratio < 0.65 {
			return "9:16"
		}
		return "3:4"
	}
}

func generateAmbientWav(durationSec float64, absOutPath string) error {
	const sampleRate = 44100
	const bitsPerSample = 16
	const numChannels = 1

	numSamples := int(sampleRate * durationSec)
	blockAlign := numChannels * (bitsPerSample / 8)
	dataSize := numSamples * blockAlign

	f, err := os.Create(absOutPath)
	if err != nil {
		return err
	}
	defer f.Close()

	f.Write([]byte("RIFF"))
	binary.Write(f, binary.LittleEndian, uint32(36+dataSize))
	f.Write([]byte("WAVE"))

	f.Write([]byte("fmt "))
	binary.Write(f, binary.LittleEndian, uint32(16))
	binary.Write(f, binary.LittleEndian, uint16(1))
	binary.Write(f, binary.LittleEndian, uint16(numChannels))
	binary.Write(f, binary.LittleEndian, uint32(sampleRate))
	binary.Write(f, binary.LittleEndian, uint32(sampleRate*numChannels*(bitsPerSample/8)))
	binary.Write(f, binary.LittleEndian, uint16(blockAlign))
	binary.Write(f, binary.LittleEndian, uint16(bitsPerSample))

	f.Write([]byte("data"))
	binary.Write(f, binary.LittleEndian, uint32(dataSize))

	amp := 0.15
	freqs := []float64{130.81, 196.0, 261.63, 329.63}
	for i := 0; i < numSamples; i++ {
		t := float64(i) / float64(sampleRate)
		lfo := 0.5 + 0.5*math.Sin(2*math.Pi*0.1*t)
		var sample float64
		for j, freq := range freqs {
			vol := amp * 0.4
			if j >= 2 {
				vol = amp * 0.2
			}
			vol = vol * (0.6 + 0.4*lfo)
			sample += vol * math.Sin(2*math.Pi*freq*t+float64(j)*0.3)
		}
		val := int16(math.Max(-1.0, math.Min(1.0, sample)) * 32767)
		binary.Write(f, binary.LittleEndian, val)
	}

	return nil
}

// Helper core functions

var base64Regex = regexp.MustCompile(`data:[^;]+;base64,[A-Za-z0-9+/=]+`)

func loadReferenceGameImpl(templatesBaseDir, gameType string) (string, error) {
	normalized := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(gameType)), "-", "_")
	baseDir := filepath.Join(templatesBaseDir, "playable_ads", normalized)

	if normalized == "match3" || normalized == "match_3" {
		indexPath := filepath.Join(baseDir, "index.html")
		data, err := os.ReadFile(indexPath)
		if err != nil {
			return "", fmt.Errorf("failed to read match3 template: %w", err)
		}
		stripped := base64Regex.ReplaceAllString(string(data), `data:image/png;base64,__BASE64_DATA_OMITTED__`)
		return stripped, nil
	}

	if normalized == "tile_match" || normalized == "mahjong" {
		files := []string{"index.html", "style.css", "playable.js"}
		var parts []string
		parts = append(parts, "=== Animal Mahjong (Tile Match, Vanilla HTML/CSS/JS) ===")
		parts = append(parts, fmt.Sprintf("Sample path: playable_ads/%s", normalized))

		for _, file := range files {
			filePath := filepath.Join(baseDir, file)
			data, err := os.ReadFile(filePath)
			if err != nil {
				slog.Warn("Failed to read tile_match template file", "file", file, "error", err)
				continue
			}
			content := string(data)
			content = base64Regex.ReplaceAllString(content, `data:image/png;base64,__BASE64_DATA_OMITTED__`)
			parts = append(parts, fmt.Sprintf("\n// --- %s ---\n%s", file, content))
		}
		return strings.Join(parts, "\n"), nil
	}

	if normalized == "vertical_shooter" || normalized == "shooter" {
		indexPath := filepath.Join(baseDir, "index.html")
		data, err := os.ReadFile(indexPath)
		if err != nil {
			indexPath = filepath.Join(baseDir, "template", "index.html")
			data, err = os.ReadFile(indexPath)
			if err != nil {
				return "", fmt.Errorf("failed to read vertical_shooter template: %w", err)
			}
		}
		stripped := base64Regex.ReplaceAllString(string(data), `data:image/png;base64,__BASE64_DATA_OMITTED__`)
		return stripped, nil
	}

	return "", fmt.Errorf("unknown game type: %s", gameType)
}

func getTrackingSDKImpl(sdkPath string) (string, error) {
	data, err := os.ReadFile(sdkPath)
	if err != nil {
		return "", fmt.Errorf("read sdk file: %w", err)
	}
	return string(data), nil
}

func getWebAudioSFXImpl() string {
	return `
// Procedural SFX engine using Web Audio API
class SoundEffects {
    constructor() {
        this.ctx = null;
    }
    init() {
        if (!this.ctx) {
            this.ctx = new (window.AudioContext || window.webkitAudioContext)();
        }
    }
    playLaser() {
        this.init();
        if (this.ctx.state === 'suspended') return;
        let osc = this.ctx.createOscillator();
        let gain = this.ctx.createGain();
        osc.connect(gain);
        gain.connect(this.ctx.destination);
        osc.frequency.setValueAtTime(800, this.ctx.currentTime);
        osc.frequency.exponentialRampToValueAtTime(100, this.ctx.currentTime + 0.3);
        gain.gain.setValueAtTime(0.3, this.ctx.currentTime);
        gain.gain.exponentialRampToValueAtTime(0.01, this.ctx.currentTime + 0.3);
        osc.start();
        osc.stop(this.ctx.currentTime + 0.3);
    }
}`
}

type WriteHTMLReport struct {
	Passed bool     `json:"passed"`
	Errors []string `json:"errors"`
}

func writeHTMLImpl(projectDir, pipelineID, htmlContent, validationScriptPath string) (*WriteHTMLReport, error) {
	outDir := filepath.Join(projectDir, "output", pipelineID)
	os.MkdirAll(outDir, 0755)

	indexPath := filepath.Join(outDir, "index.html")
	if err := os.WriteFile(indexPath, []byte(htmlContent), 0644); err != nil {
		return nil, err
	}

	// 1. Normalization pass
	normalizedHTML := htmlContent

	// Normalize url(assets/...) and url('assets/...') to url('assets/...')
	urlRegex := regexp.MustCompile(`(?i)url\(\s*['"]?(?:\./)?(assets/[^)'"]+)['"]?\s*\)`)
	normalizedHTML = urlRegex.ReplaceAllString(normalizedHTML, "url('$1')")

	// Normalize src="./assets/..." to src="assets/..."
	attrRegex := regexp.MustCompile(`(?i)\b(src|href|poster|data-src)\s*=\s*(["'])\s*(?:\./)?(assets/[^"'\s<>]+)(["'])`)
	normalizedHTML = attrRegex.ReplaceAllStringFunc(normalizedHTML, func(match string) string {
		submatches := attrRegex.FindStringSubmatch(match)
		if len(submatches) != 5 {
			return match
		}
		attr := submatches[1]
		openQuote := submatches[2]
		assetPath := submatches[3]
		closeQuote := submatches[4]
		if openQuote != closeQuote {
			return match
		}
		return fmt.Sprintf("%s=%s%s%s", attr, openQuote, assetPath, closeQuote)
	})

	// 2. Inlining pass: replace any quoted "assets/..." string with base64 data URI
	inlineRegex := regexp.MustCompile(`(["'])(assets/[^"'\s<>]+)(["'])`)
	inlineContent := inlineRegex.ReplaceAllStringFunc(normalizedHTML, func(match string) string {
		submatches := inlineRegex.FindStringSubmatch(match)
		if len(submatches) != 4 {
			return match
		}
		quote := submatches[1]
		relPath := submatches[2]
		closeQuote := submatches[3]
		if quote != closeQuote {
			return match
		}

		cleanPath := relPath
		if idx := strings.Index(cleanPath, "?"); idx != -1 {
			cleanPath = cleanPath[:idx]
		}
		if idx := strings.Index(cleanPath, "#"); idx != -1 {
			cleanPath = cleanPath[:idx]
		}

		absAssetPath := filepath.Join(outDir, cleanPath)
		assetData, err := os.ReadFile(absAssetPath)
		if err != nil {
			basename := filepath.Base(cleanPath)
			fallbackPath := filepath.Join(outDir, "assets", basename)
			assetData, err = os.ReadFile(fallbackPath)
			if err != nil {
				slog.Warn("Asset not found for inlining", "ref", relPath, "abs_path", absAssetPath, "fallback_path", fallbackPath)
				return match
			}
		}

		mimeType := mime.TypeByExtension(filepath.Ext(cleanPath))
		if mimeType == "" {
			mimeType = http.DetectContentType(assetData)
		}
		b64 := base64.StdEncoding.EncodeToString(assetData)
		dataURI := fmt.Sprintf("data:%s;base64,%s", mimeType, b64)

		return quote + dataURI + quote
	})

	inlinePath := filepath.Join(outDir, "preview_inline.html")
	if err := os.WriteFile(inlinePath, []byte(inlineContent), 0644); err != nil {
		return nil, err
	}

	valDest := filepath.Join(outDir, "playwright_validation.js")
	if scriptData, err := os.ReadFile(validationScriptPath); err == nil {
		os.WriteFile(valDest, scriptData, 0644)
	}

	report := &WriteHTMLReport{Passed: true, Errors: []string{}}
	if len(inlineContent) > 5*1024*1024 {
		report.Passed = false
		report.Errors = append(report.Errors, "Inline HTML size exceeds 5MB")
	}
	if strings.Contains(htmlContent, "eval(") {
		report.Passed = false
		report.Errors = append(report.Errors, "Forbidden 'eval()' function call detected")
	}
	if regexp.MustCompile(`(?i)(src|href)\s*=\s*["']http(s)?://`).MatchString(htmlContent) {
		report.Errors = append(report.Errors, "Warning: absolute network URL found in src/href attributes")
	}

	return report, nil
}

func publishPlayableAdImpl(ctx context.Context, gcs *gcs.Client, config *config.Config, projectDir, pipelineID string, publishToGCS bool) (string, string, error) {
	port := config.Server.Port
	if port == 0 {
		port = 1983
	}
	localURL := fmt.Sprintf("http://localhost:%d/playable-preview/%s/preview_inline.html", port, pipelineID)

	if !publishToGCS || gcs == nil {
		if publishToGCS && gcs == nil {
			slog.Warn("GCS publish requested but GCS client not configured; falling back to local URL")
		}
		return localURL, "", nil
	}

	outDir := filepath.Join(projectDir, "output", pipelineID)
	url, err := gcs.PublishPlayable(ctx, pipelineID, outDir)
	if err != nil {
		slog.Warn("GCS upload failed, falling back to local preview", "error", err, "url", localURL)
		return localURL, "", nil
	}

	gcsURI := fmt.Sprintf("gs://%s/playable-ads/%s/index.html", gcs.Bucket(), pipelineID)
	return url, gcsURI, nil
}
