package tools

import (
	"context"
	"fmt"
	"log/slog"
	"mobius/internal/config"
	"mobius/internal/domain"
	"mobius/internal/gcs"
	"mobius/internal/sandbox"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	// Output regexes
	verifySuccessRegex = regexp.MustCompile(`\[验证结果\] 匹配成功！\(置信度:\s*(\d+\.\d+)%\)`)
	verifyMessageRegex = regexp.MustCompile(`\[提取内容\]\s*"(.*)"`)
	verifyFailureRegex = regexp.MustCompile(`\[验证结果\] 失败。未检测到有效水印，或密码错误。`)
	capacityErrorRegex = regexp.MustCompile(`message exceeds capacity:\s*\d+\s*bytes,\s*max\s*\d+\s*bytes`)
)

type WatermarkEngine struct {
	sb         config.SandboxConfig
	binaryPath string
}

func resolveHostBinary() string {
	if p, err := exec.LookPath("infinishield"); err == nil {
		return p
	}
	dir, err := os.Getwd()
	if err != nil {
		return "bin/infinishield"
	}
	for {
		candidate := filepath.Join(dir, "bin/infinishield")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "bin/infinishield"
}

func NewWatermarkEngine(sb config.SandboxConfig) *WatermarkEngine {
	binPath := "/usr/local/bin/infinishield"
	if sb.Provider == "none" || !sb.Enabled {
		binPath = resolveHostBinary()
	}
	return &WatermarkEngine{
		sb:         sb,
		binaryPath: binPath,
	}
}

// EmbedImage runs infinishield embed on a local image.
func (e *WatermarkEngine) EmbedImage(ctx context.Context, inputLocal, outputLocal, message, password string, intensity int) error {
	argv := []string{
		e.binaryPath,
		"embed",
		"-i", inputLocal,
		"-o", outputLocal,
		"-m", message,
		"-p", password,
	}
	if intensity > 0 {
		argv = append(argv, "--intensity", strconv.Itoa(intensity))
	}

	// Workdir is set to the directory of the input image to resolve relative paths if any.
	workdir := filepath.Dir(inputLocal)

	stdout, stderr, exitCode, err := sandbox.RunArgv(ctx, e.sb, workdir, argv, nil)
	if err != nil {
		return fmt.Errorf("failed to run sandboxed embed: %w", err)
	}

	if exitCode != 0 {
		// Check for capacity error in stderr
		if capacityErrorRegex.MatchString(stderr) {
			// Extract capacity message for user-friendly error
			return fmt.Errorf("watermark embedding failed: %s", strings.TrimSpace(stderr))
		}
		return fmt.Errorf("infinishield embed failed with exit code %d: %s", exitCode, stderr)
	}

	slog.Debug("infinishield embed succeeded", "stdout", stdout)
	return nil
}

// VerifyImage runs infinishield verify on a local image.
// Returns (detected, confidence, message, error)
func (e *WatermarkEngine) VerifyImage(ctx context.Context, inputLocal, password string) (bool, float64, string, error) {
	argv := []string{
		e.binaryPath,
		"verify",
		"-i", inputLocal,
		"-p", password,
	}
	workdir := filepath.Dir(inputLocal)

	stdout, stderr, exitCode, err := sandbox.RunArgv(ctx, e.sb, workdir, argv, nil)
	if err != nil {
		return false, 0, "", fmt.Errorf("failed to run sandboxed verify: %w", err)
	}

	// Note: infinishield verify exits with 0 even on mismatch, so we parse stdout.
	// But it might exit with 1 on actual crash or file-not-found.
	if exitCode != 0 {
		return false, 0, "", fmt.Errorf("infinishield verify failed with exit code %d: %s", exitCode, stderr)
	}

	// Parse success
	if verifySuccessRegex.MatchString(stdout) {
		matches := verifySuccessRegex.FindStringSubmatch(stdout)
		if len(matches) < 2 {
			return false, 0, "", fmt.Errorf("failed to parse confidence from stdout: %s", stdout)
		}
		confidence, parseErr := strconv.ParseFloat(matches[1], 64)
		if parseErr != nil {
			return false, 0, "", fmt.Errorf("invalid confidence float %q: %w", matches[1], parseErr)
		}

		msgMatches := verifyMessageRegex.FindStringSubmatch(stdout)
		message := ""
		if len(msgMatches) >= 2 {
			message = msgMatches[1]
		}
		return true, confidence, message, nil
	}

	// Parse failure
	if verifyFailureRegex.MatchString(stdout) {
		return false, 0, "", nil
	}

	// If neither matched, it is an unparseable error
	return false, 0, "", fmt.Errorf("unparseable verify output (version mismatch or binary crash): stdout=%q stderr=%q", stdout, stderr)
}

// GetVideoDuration queries the duration (seconds) of the video.
func (e *WatermarkEngine) GetVideoDuration(ctx context.Context, inputLocal string) (float64, error) {
	argv := []string{
		"ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		inputLocal,
	}
	workdir := filepath.Dir(inputLocal)
	stdout, stderr, exitCode, err := sandbox.RunArgv(ctx, e.sb, workdir, argv, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to run ffprobe duration check: %w", err)
	}
	if exitCode != 0 {
		return 0, fmt.Errorf("ffprobe duration check failed with exit code %d: %s", exitCode, stderr)
	}

	durationStr := strings.TrimSpace(stdout)
	duration, err := strconv.ParseFloat(durationStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse duration %q: %w", durationStr, err)
	}
	return duration, nil
}

// ExtractVideoFrames extracts frames to tmpDir at specified fps.
func (e *WatermarkEngine) ExtractVideoFrames(ctx context.Context, inputLocal, tmpDir string, fps float64) error {
	argv := []string{
		"ffmpeg",
		"-y",
		"-i", inputLocal,
		"-vf", fmt.Sprintf("fps=%f", fps),
		"-pix_fmt", "rgb24",
		filepath.Join(tmpDir, "frame_%04d.png"),
	}
	workdir := filepath.Dir(inputLocal)
	_, stderr, exitCode, err := sandbox.RunArgv(ctx, e.sb, workdir, argv, nil)
	if err != nil {
		return fmt.Errorf("failed to run ffmpeg frame extract: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("ffmpeg extract failed with exit code %d: %s", exitCode, stderr)
	}
	return nil
}

// StitchVideo stitches PNG frames back to video using CRF 18.
func (e *WatermarkEngine) StitchVideo(ctx context.Context, tmpDir, inputLocal, outputLocal string, fps float64) error {
	argv := []string{
		"ffmpeg",
		"-y",
		"-framerate", fmt.Sprintf("%f", fps),
		"-i", filepath.Join(tmpDir, "frame_%04d.png"),
		"-i", inputLocal,
		"-map", "0:v",
		"-map", "1:a?", // optional audio copy
		"-c:v", "libx264",
		"-crf", "18",
		"-pix_fmt", "yuv420p",
		"-c:a", "copy",
		outputLocal,
	}
	workdir := filepath.Dir(inputLocal)
	_, stderr, exitCode, err := sandbox.RunArgv(ctx, e.sb, workdir, argv, nil)
	if err != nil {
		return fmt.Errorf("failed to run ffmpeg stitch: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("ffmpeg stitch failed with exit code %d: %s", exitCode, stderr)
	}
	return nil
}

// EmbedVideo coordinates frame extraction, watermarking, and stitching.
func (e *WatermarkEngine) EmbedVideo(ctx context.Context, inputLocal, outputLocal, message, password string, intensity int, progress func(current, total int)) error {
	tmpDir, err := os.MkdirTemp("", "mobius-video-watermark-")
	if err != nil {
		return fmt.Errorf("create video temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	const sampleFPS = 1.0
	slog.Info("Extracting video frames...", "video", inputLocal, "tmp", tmpDir)
	if err := e.ExtractVideoFrames(ctx, inputLocal, tmpDir, sampleFPS); err != nil {
		return err
	}

	// Find extracted frame files
	files, err := filepath.Glob(filepath.Join(tmpDir, "frame_*.png"))
	if err != nil {
		return fmt.Errorf("glob frames failed: %w", err)
	}
	total := len(files)
	if total == 0 {
		return fmt.Errorf("no frames extracted from video")
	}

	slog.Info("Watermarking video frames...", "count", total)
	for i, f := range files {
		// Reuse same path for output (in-place replacement inside temp dir)
		if err := e.EmbedImage(ctx, f, f, message, password, intensity); err != nil {
			return fmt.Errorf("failed to watermark frame %d/%d: %w", i+1, total, err)
		}
		if progress != nil && ((i+1)%10 == 0 || i+1 == total) {
			progress(i+1, total)
		}
	}

	slog.Info("Stitching video back...", "output", outputLocal)
	if err := e.StitchVideo(ctx, tmpDir, inputLocal, outputLocal, sampleFPS); err != nil {
		return err
	}

	return nil
}

// VerifyVideo extracts 1 frame per second and runs majority voting.
func (e *WatermarkEngine) VerifyVideo(ctx context.Context, inputLocal, password string) (bool, float64, string, error) {
	tmpDir, err := os.MkdirTemp("", "mobius-video-verify-")
	if err != nil {
		return false, 0, "", fmt.Errorf("create video verify temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	duration, err := e.GetVideoDuration(ctx, inputLocal)
	if err != nil {
		return false, 0, "", fmt.Errorf("failed to query video duration: %w", err)
	}

	fps := 1.0
	if duration > 10.0 {
		fps = 10.0 / duration
	}

	// Extract sampled frames
	argv := []string{
		"ffmpeg",
		"-y",
		"-i", inputLocal,
		"-vf", fmt.Sprintf("fps=%f", fps),
		"-pix_fmt", "rgb24",
		filepath.Join(tmpDir, "frame_%04d.png"),
	}
	workdir := filepath.Dir(inputLocal)
	_, stderr, exitCode, err := sandbox.RunArgv(ctx, e.sb, workdir, argv, nil)
	if err != nil {
		return false, 0, "", fmt.Errorf("failed to extract verification frames: %w", err)
	}
	if exitCode != 0 {
		return false, 0, "", fmt.Errorf("verify frame extraction failed: %s", stderr)
	}

	files, err := filepath.Glob(filepath.Join(tmpDir, "frame_*.png"))
	if err != nil {
		return false, 0, "", fmt.Errorf("glob verify frames failed: %w", err)
	}
	if len(files) == 0 {
		return false, 0, "", fmt.Errorf("no frames extracted for video verification")
	}

	// Majority vote map: message -> slice of confidences
	messageConfidences := make(map[string][]float64)
	detectedCount := 0

	for _, f := range files {
		detected, confidence, msg, err := e.VerifyImage(ctx, f, password)
		if err != nil {
			slog.Warn("failed to verify frame", "file", f, "err", err)
			continue
		}
		if detected {
			detectedCount++
			messageConfidences[msg] = append(messageConfidences[msg], confidence)
		}
	}

	if detectedCount == 0 {
		return false, 0, "", nil
	}

	// Find the most frequent extracted message
	bestMsg := ""
	maxFreq := 0
	for msg, confs := range messageConfidences {
		if len(confs) > maxFreq {
			maxFreq = len(confs)
			bestMsg = msg
		}
	}

	// Calculate average confidence for the best message
	sum := 0.0
	confs := messageConfidences[bestMsg]
	for _, c := range confs {
		sum += c
	}
	avgConfidence := sum / float64(len(confs))

	if maxFreq > 0 {
		return true, avgConfidence, bestMsg, nil
	}

	return false, 0, "", nil
}

type watermarkDBClient interface {
	UpdateTaskStatus(ctx context.Context, id, newStatus, actorID string, feedback ...string) error
	AddTaskComment(ctx context.Context, taskID, authorID, content string) (*domain.TaskComment, error)
	UpdateTask(ctx context.Context, id string, title, body, priority *string, assigneeID *string, result *string) error
}

// WatermarkTaskDriver runs the background async pipeline.
type WatermarkTaskDriver struct {
	pgClient  watermarkDBClient
	gcsClient *gcs.Client
	engine    *WatermarkEngine
	actorID   string
	password  string
}

func NewWatermarkTaskDriver(pg watermarkDBClient, gcs *gcs.Client, engine *WatermarkEngine, actorID string, password string) *WatermarkTaskDriver {
	return &WatermarkTaskDriver{
		pgClient:  pg,
		gcsClient: gcs,
		engine:    engine,
		actorID:   actorID,
		password:  password,
	}
}

// Run executes the task async state machine.
func (d *WatermarkTaskDriver) Run(ctx context.Context, taskID, inputGCSPath, outputGCSPrefix, message, password string, intensity int) {
	// 1. Transition todo -> ready -> in_progress
	if err := d.transitionTask(ctx, taskID, "in_progress"); err != nil {
		slog.Error("failed to transition task to in_progress", "taskID", taskID, "err", err)
		return
	}

	d.addTaskComment(ctx, taskID, "", "System: Watermarking job started.")

	// Determine if input is a single file or a prefix (wildcard)
	var relativePaths []string
	if strings.Contains(inputGCSPath, "*") {
		prefix := strings.Split(inputGCSPath, "*")[0]
		objects, err := d.gcsClient.ListPrefix(ctx, prefix)
		if err != nil {
			d.failTask(ctx, taskID, fmt.Errorf("failed to list GCS bucket prefix: %w", err))
			return
		}
		relativePaths = objects
	} else {
		relativePaths = []string{inputGCSPath}
	}

	if len(relativePaths) == 0 {
		d.failTask(ctx, taskID, fmt.Errorf("no input files found matching GCS path %q", inputGCSPath))
		return
	}

	// Asset Caps
	const maxWatermarkImages = 50
	const maxWatermarkVideos = 2

	imagesCount := 0
	videosCount := 0
	for _, p := range relativePaths {
		if IsVideoFile(p) {
			videosCount++
		} else {
			imagesCount++
		}
	}

	if imagesCount > maxWatermarkImages {
		d.failTask(ctx, taskID, fmt.Errorf("bulk request exceeds maximum image limit of %d (found %d)", maxWatermarkImages, imagesCount))
		return
	}
	if videosCount > maxWatermarkVideos {
		d.failTask(ctx, taskID, fmt.Errorf("request exceeds maximum video limit of %d (found %d)", maxWatermarkVideos, videosCount))
		return
	}

	var outputGCSPaths []string
	totalAssets := len(relativePaths)

	for index, relPath := range relativePaths {
		err := func() error {
			d.addTaskComment(ctx, taskID, "", fmt.Sprintf("System: Processing asset %d/%d (%s)...", index+1, totalAssets, filepath.Base(relPath)))

			localTmpIn, err := d.downloadGCSObject(ctx, relPath)
			if err != nil {
				return fmt.Errorf("failed to download %s: %w", relPath, err)
			}
			defer os.Remove(localTmpIn)

			isVid := IsVideoFile(relPath)
			if isVid {
				duration, err := d.engine.GetVideoDuration(ctx, localTmpIn)
				if err != nil {
					return fmt.Errorf("failed to query video duration for %s: %w", relPath, err)
				}
				const maxVideoDuration = 60.0 // 60 seconds
				if duration > maxVideoDuration {
					return fmt.Errorf("video too long to watermark (duration %.1fs > cap %.1fs); please trim the clip or process in smaller chunks", duration, maxVideoDuration)
				}
			}

			ext := filepath.Ext(relPath)
			baseName := strings.TrimSuffix(filepath.Base(relPath), ext)

			var localTmpOut string
			var outExt string
			if isVid {
				outExt = ".mp4"
				f, err := os.CreateTemp("", "mobius-wm-out-*.mp4")
				if err != nil {
					return fmt.Errorf("failed to create temp output video: %w", err)
				}
				localTmpOut = f.Name()
				f.Close()
			} else {
				outExt = ".png"
				f, err := os.CreateTemp("", "mobius-wm-out-*.png")
				if err != nil {
					return fmt.Errorf("failed to create temp output image: %w", err)
				}
				localTmpOut = f.Name()
				f.Close()
			}
			defer os.Remove(localTmpOut)

			// Run embedding
			if isVid {
				err = d.engine.EmbedVideo(ctx, localTmpIn, localTmpOut, message, password, intensity, func(curr, tot int) {
					d.addTaskComment(ctx, taskID, "", fmt.Sprintf("System: Watermarked frames %d/%d...", curr, tot))
				})
			} else {
				err = d.engine.EmbedImage(ctx, localTmpIn, localTmpOut, message, password, intensity)
			}

			if err != nil {
				return fmt.Errorf("failed to embed watermark on %s: %w", relPath, err)
			}

			// Upload to GCS
			contentType := "image/png"
			if isVid {
				contentType = "video/mp4"
			}

			fOut, err := os.Open(localTmpOut)
			if err != nil {
				return fmt.Errorf("failed to open output temp file: %w", err)
			}
			defer fOut.Close()

			gcsURI, err := d.gcsClient.Upload(ctx, outputGCSPrefix, baseName, outExt, fOut, contentType)
			if err != nil {
				return fmt.Errorf("failed to upload watermarked asset to GCS: %w", err)
			}

			outputGCSPaths = append(outputGCSPaths, gcsURI)
			return nil
		}()

		if err != nil {
			d.failTask(ctx, taskID, err)
			return
		}
	}

	resultStr := strings.Join(outputGCSPaths, "\n")
	if err := d.completeTask(ctx, taskID, resultStr); err != nil {
		slog.Error("failed to complete task", "taskID", taskID, "err", err)
	}
}

func (d *WatermarkTaskDriver) downloadGCSObject(ctx context.Context, relativePath string) (string, error) {
	f, err := os.CreateTemp("", "mobius-wm-in-*"+filepath.Ext(relativePath))
	if err != nil {
		return "", err
	}
	localPath := f.Name()
	f.Close()

	if err := d.gcsClient.Download(ctx, relativePath, localPath); err != nil {
		os.Remove(localPath)
		return "", err
	}
	return localPath, nil
}

func (d *WatermarkTaskDriver) transitionTask(ctx context.Context, taskID, newStatus string) error {
	if newStatus == "in_progress" {
		if err := d.pgClient.UpdateTaskStatus(ctx, taskID, "ready", d.actorID); err != nil {
			slog.Debug("could not set task status to ready", "err", err)
		}
	}
	return d.pgClient.UpdateTaskStatus(ctx, taskID, newStatus, d.actorID)
}

func (d *WatermarkTaskDriver) failTask(ctx context.Context, taskID string, taskErr error) {
	slog.Error("watermark task failed", "taskID", taskID, "err", taskErr)
	d.addTaskComment(ctx, taskID, "", fmt.Sprintf("System Error: Watermarking failed: %s", d.redactSecret(taskErr.Error())))

	if err := d.transitionTask(ctx, taskID, "blocked"); err != nil {
		slog.Error("failed to transition task to blocked", "taskID", taskID, "err", err)
	}
}

func (d *WatermarkTaskDriver) completeTask(ctx context.Context, taskID, result string) error {
	if err := d.pgClient.UpdateTask(ctx, taskID, nil, nil, nil, nil, &result); err != nil {
		return fmt.Errorf("failed to update task result: %w", err)
	}

	if err := d.transitionTask(ctx, taskID, "needs_review"); err != nil {
		return fmt.Errorf("failed to set task needs_review: %w", err)
	}

	d.addTaskComment(ctx, taskID, "", "System: Watermarking job complete. Output assets uploaded to GCS. Review required.")
	return nil
}

func (d *WatermarkTaskDriver) addTaskComment(ctx context.Context, taskID, authorID, content string) {
	if _, err := d.pgClient.AddTaskComment(ctx, taskID, authorID, d.redactSecret(content)); err != nil {
		slog.Error("failed to add task comment", "taskID", taskID, "err", err)
	}
}

func IsVideoFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".mp4" || ext == ".webm" || ext == ".mov" || ext == ".avi" || ext == ".mkv"
}

func (d *WatermarkTaskDriver) redactSecret(s string) string {
	if d.password == "" {
		return s
	}
	return strings.ReplaceAll(s, d.password, "******")
}
