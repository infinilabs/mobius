package httpapi

import (
	"io"
	"mobius/internal/config"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlayablePreviewServer(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "mobius-test-projects-")
	defer os.RemoveAll(tmpDir)

	pipelineID := "pipe_abc"
	outDir := filepath.Join(tmpDir, "proj_1", "output", pipelineID)
	os.MkdirAll(outDir, 0755)

	htmlContent := "<html><body>Preview!</body></html>"
	os.WriteFile(filepath.Join(outDir, "preview_inline.html"), []byte(htmlContent), 0644)
	os.WriteFile(filepath.Join(outDir, "extra.js"), []byte("console.log('extra')"), 0644)

	cfg := &config.Config{}
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

func TestPlayablePreviewPathConfinement(t *testing.T) {
	// Plan 3.6: ../ (encoded), sibling-prefix, and glob/traversal tricks in the
	// URL must not read outside the pipeline's output directory.
	tmpDir, _ := os.MkdirTemp("", "mobius-test-projects-")
	defer os.RemoveAll(tmpDir)

	outDir := filepath.Join(tmpDir, "proj_1", "output", "pipe_abc")
	os.MkdirAll(outDir, 0755)
	os.WriteFile(filepath.Join(outDir, "preview_inline.html"), []byte("ok"), 0644)

	// Files an attacker must NOT be able to read: one outside the projects tree
	// entirely, and one in a sibling dir that shares the output dir's name as a
	// prefix (defeats a bare strings.HasPrefix check).
	os.WriteFile(filepath.Join(tmpDir, "secret.txt"), []byte("secret"), 0644)
	sibling := filepath.Join(tmpDir, "proj_1", "output", "pipe_abcevil")
	os.MkdirAll(sibling, 0755)
	os.WriteFile(filepath.Join(sibling, "leak.txt"), []byte("leak"), 0644)

	cfg := &config.Config{}
	cfg.Projects.ProjectsDir = tmpDir
	api := &APIHandler{config: cfg}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /playable-preview/{pipeline_id}", api.PlayablePreviewRedirect)
	mux.HandleFunc("GET /playable-preview/{pipeline_id}/{path...}", api.PlayablePreviewHandler)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	attacks := []string{
		"/playable-preview/pipe_abc/..%2f..%2f..%2fsecret.txt",    // encoded ../ escape
		"/playable-preview/pipe_abc/..%2fpipe_abcevil%2fleak.txt", // sibling prefix escape
		"/playable-preview/%2e%2e%2f%2e%2e/secret.txt",            // traversal in pipeline_id
		"/playable-preview/pipe_%2A/preview_inline.html",          // glob metachar pipeline_id
	}
	for _, path := range attacks {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s failed: %v", path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Errorf("%s must be refused, got 200 with body %q", path, body)
		}
	}

	// The legitimate preview still serves.
	resp, err := http.Get(ts.URL + "/playable-preview/pipe_abc/")
	if err != nil {
		t.Fatalf("GET preview failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("legitimate preview should serve, got %v", resp.StatusCode)
	}
}
