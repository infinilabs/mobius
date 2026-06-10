package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlayableLoadReferenceGame(t *testing.T) {
	// Setup dummy template
	tmpDir, _ := os.MkdirTemp("", "mobius-test-templates-")
	defer os.RemoveAll(tmpDir)

	gameDir := filepath.Join(tmpDir, "playable_ads", "match3")
	os.MkdirAll(gameDir, 0755)

	htmlContent := `<html><body><img src="data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAA..."/></body></html>`
	os.WriteFile(filepath.Join(gameDir, "index.html"), []byte(htmlContent), 0644)

	// Call implementation
	output, err := loadReferenceGameImpl(tmpDir, "match3")
	if err != nil {
		t.Fatalf("tool execution failed: %v", err)
	}

	if !strings.Contains(output, "__BASE64_DATA_OMITTED__") {
		t.Errorf("Expected base64 content to be stripped, got: %s", output)
	}
	if strings.Contains(output, "iVBORw0KGgoAAA") {
		t.Errorf("Raw base64 data was not stripped")
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
