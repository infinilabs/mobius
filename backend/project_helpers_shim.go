package main

// Transitional wrappers (plan 6.4f): the shared project/file helpers live in
// internal/domain and internal/tools; the adapters, dispatcher, and MCP server
// below main still use the historical names.

import (
	"mobius/internal/domain"
	"mobius/internal/tools"
)

func projectsBaseDir(cfg *Config) string { return cfg.Projects.ProjectsDir }

func resolveWithinRoot(root, rel string) (string, error) { return domain.ResolveWithinRoot(root, rel) }

func validateProjectPath(relativePath string) error { return domain.ValidateProjectPath(relativePath) }

func classifyContentType(mimeType string) string { return domain.ClassifyContentType(mimeType) }

func resolveMimeType(filename, headerMime string) string {
	return domain.ResolveMimeType(filename, headerMime)
}

func isTextIndexable(contentType string) bool { return domain.IsTextIndexable(contentType) }

func calculateSHA256(data []byte) string { return domain.CalculateSHA256(data) }

func readProjectMemory(project *Project, cfg *Config) string {
	return tools.ReadProjectMemory(project, cfg)
}
