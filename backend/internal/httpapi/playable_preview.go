package httpapi

import (
	"log/slog"
	"mobius/internal/domain"
	"net/http"
	"path/filepath"
	"regexp"
)

func (api *APIHandler) PlayablePreviewRedirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
}

// pipelineIDRe confines pipeline_id to a plain identifier: it is interpolated
// into a filepath.Glob pattern, so traversal (`..`, `/`) and glob metacharacters
// (`*?[`) must never reach it (plan 3.6).
var pipelineIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func (api *APIHandler) PlayablePreviewHandler(w http.ResponseWriter, r *http.Request) {
	pipelineID := r.PathValue("pipeline_id")
	subPath := r.PathValue("path")

	if !pipelineIDRe.MatchString(pipelineID) {
		http.Error(w, "invalid pipeline_id", http.StatusBadRequest)
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

	// Confine to the pipeline output dir with the same lexical + symlink
	// resolution every agent file tool uses (plan 3.6). A bare prefix check is
	// not enough: it passes sibling dirs sharing the name as a prefix and
	// follows symlinks out of the tree.
	absTarget, err := domain.ResolveWithinRoot(outputDir, subPath)
	if err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	http.ServeFile(w, r, absTarget)
}
