package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	content, err := getTrackingSDKImpl("static/tracking/sdk.js")
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
	hugeHTML := strings.Repeat("a", 5*1024*1024 + 10)
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
	cfg := &Config{}
	adapter := &InternalLLMAdapter{config: cfg}

	tmpDir, _ := os.MkdirTemp("", "mobius-test-media-")
	defer os.RemoveAll(tmpDir)

	imgPath := filepath.Join(tmpDir, "sprite.png")
	err := adapter.generateImageClientCall(context.Background(), "a cute cat", "512x512", imgPath)
	if err != nil {
		t.Fatalf("generateImageClientCall failed: %v", err)
	}
	if _, err := os.Stat(imgPath); os.IsNotExist(err) {
		t.Errorf("Expected image file to be created at %s", imgPath)
	}

	audioPath := filepath.Join(tmpDir, "laser.wav")
	err = adapter.generateAudioClientCall(context.Background(), "laser beep", 3, audioPath)
	if err != nil {
		t.Fatalf("generateAudioClientCall failed: %v", err)
	}
	if _, err := os.Stat(audioPath); os.IsNotExist(err) {
		t.Errorf("Expected audio file to be created at %s", audioPath)
	}
}

func TestPlayablePublishAd(t *testing.T) {
	cfg := &Config{}
	projectDir := "/work/projects/test_proj"
	pipelineID := "test_pipe_99"

	url, err := publishPlayableAdImpl(context.Background(), nil, cfg, projectDir, pipelineID)
	if err != nil {
		t.Fatalf("publishPlayableAdImpl failed: %v", err)
	}

	expectedUrl := "http://localhost:1983/playable-preview/test_pipe_99/preview_inline.html"
	if url != expectedUrl {
		t.Errorf("Expected URL %q, got %q", expectedUrl, url)
	}
}

func TestPlayablePreviewServer(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "mobius-test-projects-")
	defer os.RemoveAll(tmpDir)

	pipelineID := "pipe_abc"
	outDir := filepath.Join(tmpDir, "proj_1", "output", pipelineID)
	os.MkdirAll(outDir, 0755)

	htmlContent := "<html><body>Preview!</body></html>"
	os.WriteFile(filepath.Join(outDir, "preview_inline.html"), []byte(htmlContent), 0644)
	os.WriteFile(filepath.Join(outDir, "extra.js"), []byte("console.log('extra')"), 0644)

	cfg := &Config{}
	cfg.Projects.ProjectsDir = tmpDir
	api := &APIHandler{config: cfg}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /playable-preview/{pipeline_id}", api.PlayablePreviewRedirect)
	mux.HandleFunc("GET /playable-preview/{pipeline_id}/{path...}", api.PlayablePreviewHandler)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(ts.URL + "/playable-preview/pipe_abc")
	if err != nil {
		t.Fatalf("GET redirect failed: %v", err)
	}
	if resp.StatusCode != http.StatusMovedPermanently {
		t.Errorf("Expected status 301, got %v", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasSuffix(loc, "/playable-preview/pipe_abc/") {
		t.Errorf("Expected redirect location to have trailing slash, got %q", loc)
	}

	resp2, err := http.Get(ts.URL + "/playable-preview/pipe_abc/")
	if err != nil {
		t.Fatalf("GET preview failed: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %v", resp2.StatusCode)
	}
	body, _ := io.ReadAll(resp2.Body)
	if string(body) != htmlContent {
		t.Errorf("Expected body %q, got %q", htmlContent, string(body))
	}

	resp3, err := http.Get(ts.URL + "/playable-preview/pipe_abc/extra.js")
	if err != nil {
		t.Fatalf("GET extra failed: %v", err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %v", resp3.StatusCode)
	}
	body3, _ := io.ReadAll(resp3.Body)
	if string(body3) != "console.log('extra')" {
		t.Errorf("Expected extra.js content, got %q", string(body3))
	}

	resp5, err := http.Get(ts.URL + "/playable-preview/missing_pipe/")
	if resp5.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 for missing pipeline, got %v", resp5.StatusCode)
	}
}
