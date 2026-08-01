package domain

import (
	"fmt"
	"path/filepath"
	"regexp"
	"time"
)

type Project struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Owner       *EmployeeBrief `json:"owner"`
	Status      string         `json:"status"`
	SourcePath  *string        `json:"source_path,omitempty"`
	Tags        []string       `json:"tags"`
	TaskCount   int            `json:"task_count"`
	AssetCount  int            `json:"asset_count"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type ProjectAsset struct {
	ID               string         `json:"id"`
	ProjectID        string         `json:"project_id"`
	Filename         string         `json:"filename"`
	RelativePath     string         `json:"relative_path"`
	AbsolutePath     string         `json:"absolute_path,omitempty"`
	MIMEType         string         `json:"mime_type"`
	SizeBytes        int64          `json:"size_bytes"`
	Content          string         `json:"content,omitempty"`
	ContentSummary   string         `json:"content_summary,omitempty"`
	ContentTruncated bool           `json:"content_truncated"`
	ContentType      string         `json:"content_type"`
	GCSURI           string         `json:"gcs_uri,omitempty"`
	GCSStatus        string         `json:"gcs_status"`
	Checksum         string         `json:"checksum_sha256,omitempty"`
	Tags             []string       `json:"tags"`
	Title            string         `json:"title,omitempty"`
	Description      string         `json:"description,omitempty"`
	Status           string         `json:"status,omitempty"`
	Origin           string         `json:"origin,omitempty"`
	AspectRatio      string         `json:"aspect_ratio,omitempty"`
	PublishedAt      string         `json:"published_at,omitempty"`
	CreatedByID      string         `json:"created_by_id,omitempty"`
	CreatedBy        *EmployeeBrief `json:"created_by,omitempty"`
	TaskID           string         `json:"task_id,omitempty"`
	CreatedAt        string         `json:"created_at"`
	UpdatedAt        string         `json:"updated_at"`
}

// RootDir returns the project's working directory: its imported source path
// when set, otherwise a directory named after the project under baseDir (the
// configured projects dir).
func (p *Project) RootDir(baseDir string) string {
	if p.SourcePath != nil {
		return *p.SourcePath
	}
	return filepath.Join(baseDir, p.Name)
}

// CreateProjectInput is the normalized input for the single project-creation
// interface. Every entry point (REST, chat, dispatcher, MCP) builds this and calls
// CreateProject — there is no other way to create a project.
type CreateProjectInput struct {
	Name        string
	Description string
	OwnerID     string
	SourcePath  *string
	Tags        []string
}

const (
	templateNameRegex = `^[a-z0-9][a-z0-9_-]{1,62}[a-z0-9]$`
	importNameRegex   = `^[a-zA-Z0-9][a-zA-Z0-9._-]{1,62}[a-zA-Z0-9]$`
)

var (
	templateNameRe = regexp.MustCompile(templateNameRegex)
	importNameRe   = regexp.MustCompile(importNameRegex)
)

// ValidateProjectName enforces the project naming rules (imports allow dots
// and uppercase; template-created projects are stricter).
func ValidateProjectName(name string, isImport bool) error {
	if isImport {
		if !importNameRe.MatchString(name) {
			return fmt.Errorf("invalid project name %q: must match %s", name, importNameRegex)
		}
	} else {
		if !templateNameRe.MatchString(name) {
			return fmt.Errorf("invalid project name %q: must match %s", name, templateNameRegex)
		}
	}
	return nil
}
