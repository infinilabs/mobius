package main

import (
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
)

func (api *APIHandler) PlayablePreviewRedirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
}

func (api *APIHandler) PlayablePreviewHandler(w http.ResponseWriter, r *http.Request) {
	pipelineID := r.PathValue("pipeline_id")
	subPath := r.PathValue("path")

	if pipelineID == "" {
		http.Error(w, "pipeline_id is required", http.StatusBadRequest)
		return
	}

	projectsDir := api.config.Projects.ProjectsDir
	if projectsDir == "" {
		projectsDir = "projects"
	}

	pattern := filepath.Join(projectsDir, "*", "output", pipelineID)
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		slog.Warn("playable preview folder not found", "pipeline_id", pipelineID, "pattern", pattern, "err", err)
		http.Error(w, "Preview not found for pipeline: "+pipelineID, http.StatusNotFound)
		return
	}

	outputDir := matches[0]

	if subPath == "" {
		subPath = "preview_inline.html"
	}

	targetFile := filepath.Join(outputDir, subPath)

	absTarget, err := filepath.Abs(targetFile)
	if err != nil {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	absOutput, err := filepath.Abs(outputDir)
	if err != nil {
		http.Error(w, "invalid path", http.StatusInternalServerError)
		return
	}

	if !strings.HasPrefix(absTarget, absOutput) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	http.ServeFile(w, r, absTarget)
}
