package main

import (
	"context"
	"fmt"
	"log/slog"
	"mobius/internal/service"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Project tools: files, commands, projects, prompts.
// Split from adapter_internal_tools.go (plan 6.5).

var blockedCommandPatterns = []string{
	"rm -rf /", "rm -rf ~", "mkfs", "dd if=",
	":(){", "fork bomb",
	"chmod -R 777 /", "chown -R",
	"> /dev/sd", "> /dev/null",
	"curl | sh", "wget | sh", "curl|sh", "wget|sh",
}

func validateCommand(command string) error {
	lower := strings.ToLower(command)
	for _, p := range blockedCommandPatterns {
		if strings.Contains(lower, p) {
			return fmt.Errorf("blocked command pattern: %s", p)
		}
	}
	return nil
}

func (a *InternalLLMAdapter) execWriteProjectFile(ctx context.Context, args map[string]any, task *Task) map[string]any {
	if task.ProjectID == nil {
		return map[string]any{"error": "no project context"}
	}
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	if path == "" || content == "" {
		return map[string]any{"error": "path and content are required"}
	}

	project, err := a.pgClient.GetProject(ctx, *task.ProjectID)
	if err != nil {
		return map[string]any{"error": "project not found"}
	}

	fullPath, err := resolveWithinRoot(project.RootDir(projectsBaseDir(a.config)), path)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return map[string]any{"error": "mkdir failed: " + err.Error()}
	}
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		return map[string]any{"error": "write failed: " + err.Error()}
	}

	if a.esClient != nil {
		mimeType := "text/plain"
		ct := classifyContentType(mimeType)
		now := time.Now().Format(time.RFC3339)
		maxIdx := a.config.Projects.ContentMaxIndex
		indexContent := content
		truncated := false
		if len(content) > maxIdx {
			indexContent = content[:maxIdx]
			truncated = true
		}
		asset := &ProjectAsset{
			ID: generateID(), ProjectID: *task.ProjectID,
			Filename: filepath.Base(path), RelativePath: path, AbsolutePath: fullPath,
			MIMEType: mimeType, SizeBytes: int64(len(content)),
			Content: indexContent, ContentTruncated: truncated,
			ContentType: ct, GCSStatus: "pending",
			Tags: []string{}, CreatedByID: task.Assignee.ID, TaskID: task.ID,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := a.esClient.IndexProjectAsset(ctx, asset); err != nil {
			slog.Warn("ES index project asset failed", "path", path, "error", err)
		}
	}

	if a.events != nil {
		a.events.Publish(newEvent("file_written",
			&task.Assignee.ID, task.ProjectID, &task.ID,
			map[string]any{"path": path, "size_bytes": len(content)}))
	}

	return map[string]any{"status": "written", "path": path, "bytes": len(content)}
}

func (a *InternalLLMAdapter) execReadProjectFile(ctx context.Context, args map[string]any, task *Task) map[string]any {
	if task.ProjectID == nil {
		return map[string]any{"error": "no project context"}
	}
	path, _ := args["path"].(string)
	if path == "" {
		return map[string]any{"error": "path is required"}
	}

	project, err := a.pgClient.GetProject(ctx, *task.ProjectID)
	if err != nil {
		return map[string]any{"error": "project not found"}
	}

	fullPath, err := resolveWithinRoot(project.RootDir(projectsBaseDir(a.config)), path)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return map[string]any{"error": "read failed: " + err.Error()}
	}
	content, truncated := service.TruncateForContext(string(data))
	return map[string]any{"content": content, "path": path, "bytes": len(data), "truncated": truncated}
}

func (a *InternalLLMAdapter) execSearchProjectAssets(ctx context.Context, args map[string]any, task *Task) map[string]any {
	if task.ProjectID == nil || a.esClient == nil {
		return map[string]any{"error": "no project context or ES unavailable"}
	}
	query, _ := args["query"].(string)
	contentType, _ := args["type"].(string)
	assets, err := a.esClient.SearchProjectAssets(ctx, *task.ProjectID, query, contentType, 10)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	var results []map[string]any
	for _, asset := range assets {
		results = append(results, map[string]any{
			"filename": asset.Filename, "path": asset.RelativePath, "type": asset.ContentType, "size": asset.SizeBytes,
		})
	}
	if results == nil {
		results = []map[string]any{}
	}
	return map[string]any{"results": results, "count": len(results)}
}

func (a *InternalLLMAdapter) execListProjectAssets(ctx context.Context, task *Task) map[string]any {
	if task.ProjectID == nil || a.esClient == nil {
		return map[string]any{"error": "no project context or ES unavailable"}
	}
	assets, err := a.esClient.SearchProjectAssets(ctx, *task.ProjectID, "", "", 100)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	var results []map[string]any
	for _, asset := range assets {
		results = append(results, map[string]any{
			"filename": asset.Filename, "path": asset.RelativePath, "type": asset.ContentType, "size": asset.SizeBytes,
		})
	}
	if results == nil {
		results = []map[string]any{}
	}
	return map[string]any{"results": results, "count": len(results)}
}

func (a *InternalLLMAdapter) execRunProjectCommand(ctx context.Context, args map[string]any, task *Task) map[string]any {
	if task.ProjectID == nil {
		return map[string]any{"error": "no project context"}
	}
	command, _ := args["command"].(string)
	if command == "" {
		return map[string]any{"error": "command is required"}
	}
	if err := validateCommand(command); err != nil {
		slog.Warn("blocked dangerous command", "task_id", task.ID, "command", command, "reason", err)
		return map[string]any{"error": "command rejected: " + err.Error()}
	}
	// Code execution is sandbox-only: never fall back to running on the host.
	if a.config == nil || !a.config.Sandbox.Enabled {
		return map[string]any{"error": "command execution requires the sandbox: enable sandbox in config (host execution is not permitted)"}
	}

	project, err := a.pgClient.GetProject(ctx, *task.ProjectID)
	if err != nil {
		return map[string]any{"error": "project not found"}
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	workdir := project.RootDir(projectsBaseDir(a.config))
	out, errOut, exitCode, execErr := runSandboxedCommand(cmdCtx, a.config.Sandbox, workdir, command, nil)
	if execErr != nil {
		return map[string]any{"error": "command execution failed: " + execErr.Error()}
	}
	out = truncateOutput(out)
	errOut = truncateOutput(errOut)

	slog.Info("project command executed", "task_id", task.ID, "command", command, "exit_code", exitCode)

	if a.events != nil {
		a.events.Publish(newEvent("command_execution",
			&task.Assignee.ID, task.ProjectID, &task.ID,
			map[string]any{
				"command":        command,
				"exit_code":      exitCode,
				"stdout_preview": truncateStr(out, 200),
				"success":        exitCode == 0,
			}))
	}

	return map[string]any{
		"stdout": out, "stderr": errOut,
		"exit_code": exitCode, "success": exitCode == 0,
	}
}

func (a *InternalLLMAdapter) execListProjects(ctx context.Context) map[string]any {
	projects, err := a.pgClient.ListProjects(ctx, "")
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	var results []map[string]any
	for _, p := range projects {
		r := map[string]any{"id": p.ID, "name": p.Name, "status": p.Status, "task_count": p.TaskCount, "asset_count": p.AssetCount}
		if p.Owner != nil {
			r["owner"] = p.Owner.Name
		}
		results = append(results, r)
	}
	if results == nil {
		results = []map[string]any{}
	}
	return map[string]any{"projects": results, "count": len(results)}
}

func (a *InternalLLMAdapter) execCreateProject(ctx context.Context, args map[string]any, agent *Employee) map[string]any {
	name, _ := args["name"].(string)
	description, _ := args["description"].(string)
	if name == "" {
		return map[string]any{"error": "name is required"}
	}
	p, err := a.pgClient.CreateProject(ctx, CreateProjectInput{
		Name:        name,
		Description: description,
		OwnerID:     agent.ID,
	}, projectsBaseDir(a.config), a.config.Projects.TemplateDirs)
	if err != nil {
		return map[string]any{"error": "failed to create project: " + err.Error()}
	}
	return map[string]any{"status": "created", "project_id": p.ID, "name": name}
}

func (a *InternalLLMAdapter) execUpdateProject(ctx context.Context, args map[string]any) map[string]any {
	projectID, _ := args["project_id"].(string)
	if projectID == "" {
		return map[string]any{"error": "project_id is required"}
	}
	var description, status *string
	if v, ok := args["description"].(string); ok {
		description = &v
	}
	if v, ok := args["status"].(string); ok {
		status = &v
	}
	if err := a.pgClient.UpdateProject(ctx, projectID, nil, description, status); err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"status": "updated", "project_id": projectID}
}

func (a *InternalLLMAdapter) execListPrompts(ctx context.Context, args map[string]any) map[string]any {
	if a.esClient == nil {
		return map[string]any{"error": "ES unavailable"}
	}
	query, _ := args["query"].(string)
	prompts, err := a.esClient.SearchPrompts(ctx, query)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	var results []map[string]any
	for _, p := range prompts {
		results = append(results, map[string]any{"id": p.ID, "title": p.Title, "tags": p.Tags})
	}
	if results == nil {
		results = []map[string]any{}
	}
	return map[string]any{"prompts": results, "count": len(results)}
}

func (a *InternalLLMAdapter) execCreatePrompt(ctx context.Context, args map[string]any) map[string]any {
	if a.esClient == nil {
		return map[string]any{"error": "ES unavailable"}
	}
	title, _ := args["title"].(string)
	content, _ := args["content"].(string)
	if title == "" || content == "" {
		return map[string]any{"error": "title and content are required"}
	}
	var tags []string
	if t, ok := args["tags"].([]any); ok {
		for _, v := range t {
			if s, ok := v.(string); ok {
				tags = append(tags, s)
			}
		}
	}
	if tags == nil {
		tags = []string{}
	}
	p := &Prompt{Title: title, Content: content, Tags: tags}
	if err := a.esClient.IndexPrompt(ctx, p); err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"status": "created", "prompt_id": p.ID}
}

func (a *InternalLLMAdapter) execUpdatePrompt(ctx context.Context, args map[string]any) map[string]any {
	if a.esClient == nil {
		return map[string]any{"error": "ES unavailable"}
	}
	promptID, _ := args["prompt_id"].(string)
	if promptID == "" {
		return map[string]any{"error": "prompt_id is required"}
	}
	p, err := a.esClient.GetPrompt(ctx, promptID)
	if err != nil {
		return map[string]any{"error": "prompt not found"}
	}
	if v, ok := args["title"].(string); ok {
		p.Title = v
	}
	if v, ok := args["content"].(string); ok {
		p.Content = v
	}
	if t, ok := args["tags"].([]any); ok {
		tags := make([]string, 0, len(t))
		for _, v := range t {
			if s, ok := v.(string); ok {
				tags = append(tags, s)
			}
		}
		p.Tags = tags
	}
	if err := a.esClient.IndexPrompt(ctx, p); err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"status": "updated", "prompt_id": promptID}
}

func (a *InternalLLMAdapter) execDeletePrompt(ctx context.Context, args map[string]any) map[string]any {
	if a.esClient == nil {
		return map[string]any{"error": "ES unavailable"}
	}
	promptID, _ := args["prompt_id"].(string)
	if promptID == "" {
		return map[string]any{"error": "prompt_id is required"}
	}
	if err := a.esClient.DeletePrompt(ctx, promptID); err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"status": "deleted", "prompt_id": promptID}
}
