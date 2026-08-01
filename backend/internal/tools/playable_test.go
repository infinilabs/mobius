package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mobius/internal/config"
)

func TestPlayableLoadReferenceGame(t *testing.T) {
	// Setup dummy templates
	tmpDir, _ := os.MkdirTemp("", "mobius-test-templates-")
	defer os.RemoveAll(tmpDir)

	// 1. Test Match3 (single-file)
	match3Dir := filepath.Join(tmpDir, "playable_ads", "match3")
	os.MkdirAll(match3Dir, 0755)
	htmlContent := `<html><body><img src="data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAA..."/></body></html>`
	os.WriteFile(filepath.Join(match3Dir, "index.html"), []byte(htmlContent), 0644)

	output, err := loadReferenceGameImpl(tmpDir, "match3")
	if err != nil {
		t.Fatalf("match3 load failed: %v", err)
	}
	if !strings.Contains(output, "__BASE64_DATA_OMITTED__") {
		t.Errorf("Expected base64 content to be stripped in match3, got: %s", output)
	}

	// 2. Test Tile Match (multi-file)
	tileMatchDir := filepath.Join(tmpDir, "playable_ads", "tile_match")
	os.MkdirAll(tileMatchDir, 0755)
	os.WriteFile(filepath.Join(tileMatchDir, "index.html"), []byte("<html>index</html>"), 0644)
	os.WriteFile(filepath.Join(tileMatchDir, "style.css"), []byte("body { color: red; }"), 0644)
	os.WriteFile(filepath.Join(tileMatchDir, "playable.js"), []byte("console.log('play');"), 0644)

	outputTile, err := loadReferenceGameImpl(tmpDir, "tile_match")
	if err != nil {
		t.Fatalf("tile_match load failed: %v", err)
	}
	if !strings.Contains(outputTile, "// --- index.html ---") {
		t.Error("Expected output to contain index.html marker")
	}
	if !strings.Contains(outputTile, "// --- style.css ---") {
		t.Error("Expected output to contain style.css marker")
	}
	if !strings.Contains(outputTile, "// --- playable.js ---") {
		t.Error("Expected output to contain playable.js marker")
	}
	if !strings.Contains(outputTile, "body { color: red; }") {
		t.Error("Expected output to contain style content")
	}
}

func TestPlayableGetTrackingSDK(t *testing.T) {
	// static/tracking/sdk.js should exist relative to backend/ CWD.
	content, err := getTrackingSDKImpl("../../static/tracking/sdk.js")
	if err != nil {
		t.Fatalf("failed to load sdk: %v", err)
	}
	if !strings.Contains(content, "PlayableTracker") {
		t.Errorf("Expected sdk content to contain PlayableTracker, got: %s", content)
	}
}

func TestPlayableGetWebAudioSFX(t *testing.T) {
	content := getWebAudioSFXImpl()
	if !strings.Contains(content, "SoundEffects") {
		t.Errorf("Expected web audio sfx helper to contain SoundEffects class, got: %s", content)
	}
}

func TestPlayableWriteHTML(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "mobius-test-project-")
	defer os.RemoveAll(tmpDir)

	pipelineID := "test_pipe_1"
	outDir := filepath.Join(tmpDir, "output", pipelineID)
	os.MkdirAll(filepath.Join(outDir, "assets"), 0755)

	// Write a dummy asset
	dummyAsset := []byte("fake-image-bytes")
	os.WriteFile(filepath.Join(outDir, "assets", "logo.png"), dummyAsset, 0644)

	// HTML referencing asset
	htmlIn := `<html><body><img src="assets/logo.png"/></body></html>`

	// Dummy validation script
	valScript := filepath.Join(tmpDir, "val.js")
	os.WriteFile(valScript, []byte("// dummy val script"), 0644)

	report, err := writeHTMLImpl(tmpDir, pipelineID, htmlIn, valScript)
	if err != nil {
		t.Fatalf("writeHTMLImpl failed: %v", err)
	}

	if !report.Passed {
		t.Errorf("Expected compilation to pass, got errors: %v", report.Errors)
	}

	// Verify preview_inline.html exists and contains base64
	inlinePath := filepath.Join(outDir, "preview_inline.html")
	inlineData, err := os.ReadFile(inlinePath)
	if err != nil {
		t.Fatalf("failed to read inline file: %v", err)
	}

	if !strings.Contains(string(inlineData), "data:image/png;base64,") {
		t.Errorf("Expected base64 inlined asset, got: %s", string(inlineData))
	}

	// Test eval detection
	reportEval, _ := writeHTMLImpl(tmpDir, pipelineID, `<html><script>eval("alert(1)")</script></html>`, valScript)
	if reportEval.Passed {
		t.Error("Expected eval check to fail")
	}
	if len(reportEval.Errors) == 0 || !strings.Contains(reportEval.Errors[0], "eval()") {
		t.Errorf("Expected eval error, got: %v", reportEval.Errors)
	}

	// Test size detection (make it larger than 5MB)
	hugeHTML := strings.Repeat("a", 5*1024*1024+10)
	reportSize, _ := writeHTMLImpl(tmpDir, pipelineID, hugeHTML, valScript)
	if reportSize.Passed {
		t.Error("Expected size check to fail")
	}

	// Test network URL warning (should pass, but emit warning)
	reportUrl, _ := writeHTMLImpl(tmpDir, pipelineID, `<html><script src="https://example.com/api.js"></script></html>`, valScript)
	if !reportUrl.Passed {
		t.Error("Expected network URL check to pass (warning only)")
	}
	if len(reportUrl.Errors) == 0 || !strings.Contains(reportUrl.Errors[0], "absolute network URL") {
		t.Errorf("Expected network warning, got: %v", reportUrl.Errors)
	}

	// Test store URL in JS should pass without warnings
	reportStore, _ := writeHTMLImpl(tmpDir, pipelineID, `<html><script>mraid.open("https://play.google.com/store");</script></html>`, valScript)
	if !reportStore.Passed {
		t.Error("Expected store URL check to pass")
	}
	if len(reportStore.Errors) > 0 {
		t.Errorf("Expected no warnings for store URL in JS, got: %v", reportStore.Errors)
	}
}

func TestPlayableMediaGenerationMock(t *testing.T) {
	cfg := &config.Config{}

	tmpDir, _ := os.MkdirTemp("", "mobius-test-media-")
	defer os.RemoveAll(tmpDir)

	imgPath := filepath.Join(tmpDir, "sprite.png")
	err := generateImageAsset(context.Background(), cfg, nil, "a cute cat", "512x512", imgPath)
	if err != nil {
		t.Fatalf("generateImageAsset failed: %v", err)
	}
	if _, err := os.Stat(imgPath); os.IsNotExist(err) {
		t.Errorf("Expected image file to be created at %s", imgPath)
	}

	audioPath := filepath.Join(tmpDir, "laser.wav")
	err = generateAudioAsset(context.Background(), "laser beep", 3, audioPath)
	if err != nil {
		t.Fatalf("generateAudioAsset failed: %v", err)
	}
	if _, err := os.Stat(audioPath); os.IsNotExist(err) {
		t.Errorf("Expected audio file to be created at %s", audioPath)
	}
}

func TestPlayablePublishAd(t *testing.T) {
	cfg := &config.Config{}
	projectDir := "/work/projects/test_proj"
	pipelineID := "test_pipe_99"

	// 1. Test publishToGCS = false
	url, gcsURI, err := publishPlayableAdImpl(context.Background(), nil, cfg, projectDir, pipelineID, false)
	if err != nil {
		t.Fatalf("publishPlayableAdImpl failed: %v", err)
	}

	expectedUrl := "http://localhost:1983/playable-preview/test_pipe_99/preview_inline.html"
	if url != expectedUrl {
		t.Errorf("Expected URL %q, got %q", expectedUrl, url)
	}
	if gcsURI != "" {
		t.Errorf("Expected empty GCS URI for local-only, got %q", gcsURI)
	}

	// 2. Test publishToGCS = true but GCS is nil
	url2, gcsURI2, err := publishPlayableAdImpl(context.Background(), nil, cfg, projectDir, pipelineID, true)
	if err != nil {
		t.Fatalf("publishPlayableAdImpl failed: %v", err)
	}
	if url2 != expectedUrl {
		t.Errorf("Expected URL %q, got %q", expectedUrl, url2)
	}
	if gcsURI2 != "" {
		t.Errorf("Expected empty GCS URI when GCS not configured, got %q", gcsURI2)
	}
}
