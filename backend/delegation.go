package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// latestUploadedFile returns the most recent file attached to any message in the
// conversation, or nil if none. Lets save_upload_to_assets pick up "the image I just
// uploaded" without the model echoing back an exact file identifier.
func latestUploadedFile(conv *Conversation) *FileRef {
	for i := len(conv.Messages) - 1; i >= 0; i-- {
		if files := conv.Messages[i].Files; len(files) > 0 {
			f := files[len(files)-1]
			return &f
		}
	}
	return nil
}

func (h *APIHandler) executeToolCall(
	ctx context.Context,
	call ToolCall,
	agent *Employee,
	conversationID string,
) map[string]any {
	switch call.Name {
	case "delegate_task":
		return h.execDelegateTask(ctx, call.Args, agent, conversationID)
	case "hire_employee":
		return h.execHireEmployee(ctx, call.Args, agent)
	case "submit_task_result":
		return h.execSubmitResult(ctx, call.Args)
	case "review_task":
		return h.execReviewTask(ctx, call.Args, agent)
	case "list_team":
		return h.execListTeam(ctx, agent)
	case "store_memory":
		return h.execStoreMemory(ctx, call.Args, agent, conversationID)
	case "forget_memory":
		return h.execForgetMemory(ctx, call.Args)
	case "create_project":
		return h.execCreateProject(ctx, call.Args, agent, conversationID)
	case "list_tasks":
		return h.execListTasksTool(ctx, call.Args)
	case "list_projects":
		return h.execListProjectsTool(ctx)
	case "list_employees":
		return h.execListEmployeesTool(ctx)
	case "get_employee":
		return h.execGetEmployeeTool(ctx, call.Args)
	case "update_task_status":
		return h.execUpdateTaskStatusTool(ctx, call.Args, agent)
	case "get_task":
		return h.execGetTaskTool(ctx, call.Args)
	case "update_task":
		return h.execUpdateTaskTool(ctx, call.Args)
	case "add_task_comment":
		return h.execAddTaskCommentTool(ctx, call.Args, agent)
	case "update_project":
		return h.execUpdateProjectTool(ctx, call.Args)
	case "update_employee":
		return h.execUpdateEmployeeTool(ctx, call.Args)
	case "list_skills":
		return h.execListSkillsTool(ctx, call.Args)
	case "assign_skill":
		return h.execAssignSkillTool(ctx, call.Args)
	case "unassign_skill":
		return h.execUnassignSkillTool(ctx, call.Args)
	case "list_prompts":
		return h.execListPromptsTool(ctx, call.Args)
	case "create_prompt":
		return h.execCreatePromptTool(ctx, call.Args)
	case "update_prompt":
		return h.execUpdatePromptTool(ctx, call.Args)
	case "delete_prompt":
		return h.execDeletePromptTool(ctx, call.Args)
	case "write_project_file":
		return h.execWriteProjectFile(ctx, call.Args)
	case "read_project_file":
		return h.execReadProjectFile(ctx, call.Args)
	case "search_project_assets":
		return h.execSearchProjectAssets(ctx, call.Args)
	case "list_project_assets":
		return h.execListProjectAssets(ctx, call.Args)
	case "playable_load_reference_game":
		return execPlayableLoadReferenceGameTool(h.config, call.Args)
	case "playable_get_tracking_sdk":
		return execPlayableGetTrackingSDKTool()
	case "playable_get_web_audio_sfx":
		return execPlayableGetWebAudioSFXTool()
	case "playable_write_html":
		return execPlayableWriteHTMLTool(ctx, h.config, h.pgClient, resolvePlayableProjectID(nil, call.Args), call.Args)
	case "generate_image":
		return execGenerateImageTool(ctx, h.config, h.providers, h.gcsClient, h.esClient, h.pgClient, resolvePlayableProjectID(nil, call.Args), call.Args)
	case "generate_audio":
		return execGenerateAudioTool(ctx, h.config, h.pgClient, resolvePlayableProjectID(nil, call.Args), call.Args)
	case "publish_playable_ad":
		return execPublishPlayableAdTool(ctx, h.gcsClient, h.esClient, h.config, h.pgClient, resolvePlayableProjectID(nil, call.Args), call.Args)
	case "save_upload_to_assets":
		var srcFile *FileRef
		if conv := h.conversations.Get(conversationID); conv != nil {
			srcFile = latestUploadedFile(conv)
		}
		return execSaveUploadToAssetsTool(ctx, h.gcsClient, h.esClient, h.pgClient, h.config, resolvePlayableProjectID(nil, call.Args), srcFile, call.Args)
	case "tag_media":
		return execTagMediaTool(ctx, h.bqClient, h.esClient, h.events, agent.ID, call.Args)
	case "get_tag_results":
		return execGetTagResultsTool(ctx, h.bqClient, call.Args)
	case "query_tags":
		return execQueryTagsTool(ctx, h.bqClient, call.Args)
	case "watermark_assets":
		return execWatermarkAssetsTool(ctx, h.pgClient, h.gcsClient, h.config, h.events, agent.ID, call.Args)
	case "verify_watermark":
		return execVerifyWatermarkTool(ctx, h.gcsClient, h.config, agent.ID, call.Args)
	default:
		return map[string]any{"error": "unknown tool: " + call.Name}
	}
}

func (h *APIHandler) execWriteProjectFile(ctx context.Context, args map[string]any) map[string]any {
	projectID, _ := args["_project_id"].(string)
	if projectID == "" || h.pgClient == nil {
		return map[string]any{"error": "no project context"}
	}
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	if path == "" || content == "" {
		return map[string]any{"error": "path and content are required"}
	}
	project, err := h.pgClient.GetProject(ctx, projectID)
	if err != nil {
		return map[string]any{"error": "project not found"}
	}
	fullPath, err := resolveWithinRoot(project.RootDir(h.config), path)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	os.MkdirAll(filepath.Dir(fullPath), 0755)
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		return map[string]any{"error": "write failed: " + err.Error()}
	}

	if h.esClient != nil {
		ct := classifyContentType("text/plain")
		now := time.Now().Format(time.RFC3339)
		maxIdx := h.config.Projects.ContentMaxIndex
		indexContent := content
		truncated := false
		if len(content) > maxIdx {
			indexContent = content[:maxIdx]
			truncated = true
		}
		asset := &ProjectAsset{
			ID: generateID(), ProjectID: projectID,
			Filename: filepath.Base(path), RelativePath: path, AbsolutePath: fullPath,
			MIMEType: "text/plain", SizeBytes: int64(len(content)),
			Content: indexContent, ContentTruncated: truncated,
			ContentType: ct, GCSStatus: "pending",
			Tags: []string{}, CreatedAt: now, UpdatedAt: now,
		}
		h.esClient.IndexProjectAsset(ctx, asset)
	}
	return map[string]any{"status": "written", "path": path, "bytes": len(content)}
}

func (h *APIHandler) execReadProjectFile(ctx context.Context, args map[string]any) map[string]any {
	projectID, _ := args["_project_id"].(string)
	if projectID == "" || h.pgClient == nil {
		return map[string]any{"error": "no project context"}
	}
	path, _ := args["path"].(string)
	if path == "" {
		return map[string]any{"error": "path is required"}
	}
	project, err := h.pgClient.GetProject(ctx, projectID)
	if err != nil {
		return map[string]any{"error": "project not found"}
	}
	fullPath, err := resolveWithinRoot(project.RootDir(h.config), path)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return map[string]any{"error": "read failed: " + err.Error()}
	}
	return map[string]any{"content": string(data), "path": path, "bytes": len(data)}
}

func (h *APIHandler) execSearchProjectAssets(ctx context.Context, args map[string]any) map[string]any {
	projectID, _ := args["_project_id"].(string)
	if projectID == "" || h.esClient == nil {
		return map[string]any{"error": "no project context or ES unavailable"}
	}
	query, _ := args["query"].(string)
	contentType, _ := args["type"].(string)
	assets, err := h.esClient.SearchProjectAssets(ctx, projectID, query, contentType, 10)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	var results []map[string]any
	for _, a := range assets {
		results = append(results, map[string]any{"filename": a.Filename, "path": a.RelativePath, "type": a.ContentType, "size": a.SizeBytes})
	}
	if results == nil {
		results = []map[string]any{}
	}
	return map[string]any{"results": results, "count": len(results)}
}

func (h *APIHandler) execListProjectAssets(ctx context.Context, args map[string]any) map[string]any {
	projectID, _ := args["_project_id"].(string)
	if projectID == "" || h.esClient == nil {
		return map[string]any{"error": "no project context or ES unavailable"}
	}
	assets, err := h.esClient.SearchProjectAssets(ctx, projectID, "", "", 100)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	var results []map[string]any
	for _, a := range assets {
		results = append(results, map[string]any{"filename": a.Filename, "path": a.RelativePath, "type": a.ContentType, "size": a.SizeBytes})
	}
	if results == nil {
		results = []map[string]any{}
	}
	return map[string]any{"results": results, "count": len(results)}
}

func (h *APIHandler) execCreateProject(ctx context.Context, args map[string]any, agent *Employee, conversationID string) map[string]any {
	if h.pgClient == nil {
		return map[string]any{"error": "database not available"}
	}
	name, _ := args["name"].(string)
	if name == "" {
		return map[string]any{"error": "name is required"}
	}
	description, _ := args["description"].(string)

	// One project per conversation. If this chat is already bound to a project,
	// reuse it instead of fragmenting one effort across many projects.
	if conversationID != "" {
		if conv := h.conversations.Get(conversationID); conv != nil && conv.ProjectID != nil && *conv.ProjectID != "" {
			if existing, gerr := h.pgClient.GetProject(ctx, *conv.ProjectID); gerr == nil {
				slog.Info("create_project reused existing conversation project", "project_id", existing.ID, "requested_name", name)
				return map[string]any{
					"status":     "exists",
					"project_id": existing.ID,
					"name":       existing.Name,
					"note":       "This conversation already has a project; reusing it. Put ALL tasks and assets for this effort here — do not create another project.",
				}
			}
		}
	}

	p, err := h.pgClient.CreateProject(ctx, CreateProjectInput{
		Name:        name,
		Description: description,
		OwnerID:     agent.ID,
	}, h.config)
	if err != nil {
		return map[string]any{"error": "failed to create project: " + err.Error()}
	}

	if conversationID != "" {
		h.conversations.SetProjectID(conversationID, p.ID)
		if h.pgClient != nil {
			conv := h.conversations.Get(conversationID)
			if conv != nil {
				h.pgClient.UpsertConversationMeta(ctx, conv)
			}
		}
	}

	slog.Info("project created via chat", "project_id", p.ID, "name", name, "owner", agent.Name)
	return map[string]any{
		"status":     "created",
		"project_id": p.ID,
		"name":       p.Name,
		"owner":      agent.Name,
	}
}

func (h *APIHandler) execListTasksTool(ctx context.Context, args map[string]any) map[string]any {
	if h.pgClient == nil {
		return map[string]any{"error": "database not available"}
	}
	status, _ := args["status"].(string)
	assigneeID, _ := args["assignee_id"].(string)
	projectID, _ := args["project_id"].(string)

	tasks, err := h.pgClient.ListTasks(ctx, status, assigneeID, projectID, "")
	if err != nil {
		return map[string]any{"error": "failed to list tasks: " + err.Error()}
	}
	var results []map[string]any
	for _, t := range tasks {
		entry := map[string]any{
			"id": t.ID, "title": t.Title, "status": t.Status, "priority": t.Priority,
		}
		if t.Assignee != nil {
			entry["assignee"] = t.Assignee.Name
			entry["assignee_id"] = t.Assignee.ID
		}
		if t.ProjectName != "" {
			entry["project"] = t.ProjectName
		}
		results = append(results, entry)
	}
	if results == nil {
		results = []map[string]any{}
	}
	return map[string]any{"tasks": results, "count": len(results)}
}

func (h *APIHandler) execListProjectsTool(ctx context.Context) map[string]any {
	if h.pgClient == nil {
		return map[string]any{"error": "database not available"}
	}
	projects, err := h.pgClient.ListProjects(ctx, "")
	if err != nil {
		return map[string]any{"error": "failed to list projects: " + err.Error()}
	}
	var results []map[string]any
	for _, p := range projects {
		entry := map[string]any{
			"id": p.ID, "name": p.Name, "status": p.Status,
			"task_count": p.TaskCount, "asset_count": p.AssetCount,
		}
		if p.Owner != nil {
			entry["owner"] = p.Owner.Name
		}
		results = append(results, entry)
	}
	if results == nil {
		results = []map[string]any{}
	}
	return map[string]any{"projects": results, "count": len(results)}
}

func (h *APIHandler) execListEmployeesTool(ctx context.Context) map[string]any {
	if h.pgClient == nil {
		return map[string]any{"error": "database not available"}
	}
	employees, err := h.pgClient.ListEmployees(ctx)
	if err != nil {
		return map[string]any{"error": "failed to list employees: " + err.Error()}
	}
	var results []map[string]any
	for _, e := range employees {
		entry := map[string]any{
			"id": e.ID, "name": e.Name, "title": e.Title, "role": e.Role, "tags": e.Tags,
		}
		if e.ManagerID != nil {
			entry["manager_id"] = *e.ManagerID
		}
		results = append(results, entry)
	}
	if results == nil {
		results = []map[string]any{}
	}
	return map[string]any{"employees": results, "count": len(results)}
}

func (h *APIHandler) execGetEmployeeTool(ctx context.Context, args map[string]any) map[string]any {
	if h.pgClient == nil {
		return map[string]any{"error": "database not available"}
	}
	id, _ := args["employee_id"].(string)
	if id == "" {
		return map[string]any{"error": "employee_id is required"}
	}
	emp, err := h.pgClient.GetEmployee(ctx, id)
	if err != nil {
		return map[string]any{"error": "employee not found: " + err.Error()}
	}
	var reports []map[string]any
	for _, r := range emp.Reports {
		reports = append(reports, map[string]any{"id": r.ID, "name": r.Name, "title": r.Title})
	}
	if reports == nil {
		reports = []map[string]any{}
	}
	result := map[string]any{
		"id": emp.ID, "name": emp.Name, "title": emp.Title, "role": emp.Role,
		"tags": emp.Tags, "backstory": emp.Backstory, "reports": reports,
	}
	if emp.ManagerID != nil {
		result["manager_id"] = *emp.ManagerID
	}
	return result
}

func (h *APIHandler) execUpdateTaskStatusTool(ctx context.Context, args map[string]any, actor *Employee) map[string]any {
	if h.pgClient == nil {
		return map[string]any{"error": "database not available"}
	}
	taskID, _ := args["task_id"].(string)
	status, _ := args["status"].(string)
	feedback, _ := args["feedback"].(string)
	if taskID == "" || status == "" {
		return map[string]any{"error": "task_id and status are required"}
	}
	if err := h.pgClient.UpdateTaskStatus(ctx, taskID, status, actor.ID); err != nil {
		return map[string]any{"error": "failed to update status: " + err.Error()}
	}
	if feedback != "" && status == "ready" {
		h.pgClient.AddTaskComment(ctx, taskID, actor.ID, "REJECTED: "+feedback)
	}
	return map[string]any{"status": "updated", "task_id": taskID, "new_status": status}
}

func (h *APIHandler) execGetTaskTool(ctx context.Context, args map[string]any) map[string]any {
	if h.pgClient == nil {
		return map[string]any{"error": "database not available"}
	}
	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return map[string]any{"error": "task_id is required"}
	}
	t, err := h.pgClient.GetTask(ctx, taskID)
	if err != nil {
		return map[string]any{"error": "task not found: " + err.Error()}
	}
	result := map[string]any{
		"id": t.ID, "title": t.Title, "body": t.Body,
		"status": t.Status, "priority": t.Priority, "result": t.Result,
	}
	if t.Assignee != nil {
		result["assignee"] = map[string]any{"id": t.Assignee.ID, "name": t.Assignee.Name}
	}
	if t.Creator != nil {
		result["creator"] = map[string]any{"id": t.Creator.ID, "name": t.Creator.Name}
	}
	if t.ProjectName != "" {
		result["project"] = t.ProjectName
	}
	comments, _ := h.pgClient.ListTaskComments(ctx, taskID)
	var cmts []map[string]any
	for _, c := range comments {
		author := "System"
		if c.Author != nil {
			author = c.Author.Name
		}
		cmts = append(cmts, map[string]any{"author": author, "content": c.Content, "created_at": c.CreatedAt})
	}
	if cmts != nil {
		result["comments"] = cmts
	}
	return result
}

func (h *APIHandler) execUpdateTaskTool(ctx context.Context, args map[string]any) map[string]any {
	if h.pgClient == nil {
		return map[string]any{"error": "database not available"}
	}
	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return map[string]any{"error": "task_id is required"}
	}
	var title, body, priority, assigneeID *string
	if v, ok := args["title"].(string); ok && v != "" {
		title = &v
	}
	if v, ok := args["body"].(string); ok && v != "" {
		body = &v
	}
	if v, ok := args["priority"].(string); ok && v != "" {
		priority = &v
	}
	if v, ok := args["assignee_id"].(string); ok && v != "" {
		assigneeID = &v
	}
	if err := h.pgClient.UpdateTask(ctx, taskID, title, body, priority, assigneeID, nil); err != nil {
		return map[string]any{"error": "failed to update task: " + err.Error()}
	}
	return map[string]any{"status": "updated", "task_id": taskID}
}

func (h *APIHandler) execAddTaskCommentTool(ctx context.Context, args map[string]any, agent *Employee) map[string]any {
	if h.pgClient == nil {
		return map[string]any{"error": "database not available"}
	}
	taskID, _ := args["task_id"].(string)
	content, _ := args["content"].(string)
	if taskID == "" || content == "" {
		return map[string]any{"error": "task_id and content are required"}
	}
	comment, err := h.pgClient.AddTaskComment(ctx, taskID, agent.ID, content)
	if err != nil {
		return map[string]any{"error": "failed to add comment: " + err.Error()}
	}
	return map[string]any{"status": "added", "comment_id": comment.ID}
}

func (h *APIHandler) execUpdateProjectTool(ctx context.Context, args map[string]any) map[string]any {
	if h.pgClient == nil {
		return map[string]any{"error": "database not available"}
	}
	projectID, _ := args["project_id"].(string)
	if projectID == "" {
		return map[string]any{"error": "project_id is required"}
	}
	var description, status *string
	if v, ok := args["description"].(string); ok && v != "" {
		description = &v
	}
	if v, ok := args["status"].(string); ok && v != "" {
		status = &v
	}
	if err := h.pgClient.UpdateProject(ctx, projectID, nil, description, status); err != nil {
		return map[string]any{"error": "failed to update project: " + err.Error()}
	}
	return map[string]any{"status": "updated", "project_id": projectID}
}

func (h *APIHandler) execUpdateEmployeeTool(ctx context.Context, args map[string]any) map[string]any {
	if h.pgClient == nil {
		return map[string]any{"error": "database not available"}
	}
	empID, _ := args["employee_id"].(string)
	if empID == "" {
		return map[string]any{"error": "employee_id is required"}
	}
	emp, err := h.pgClient.GetEmployee(ctx, empID)
	if err != nil {
		return map[string]any{"error": "employee not found: " + err.Error()}
	}
	if v, ok := args["title"].(string); ok && v != "" {
		emp.Title = v
	}
	if v, ok := args["backstory"].(string); ok && v != "" {
		emp.Backstory = v
	}
	if v, ok := args["tags"]; ok {
		if tagSlice, ok := v.([]any); ok {
			var tags []string
			for _, t := range tagSlice {
				if s, ok := t.(string); ok {
					tags = append(tags, s)
				}
			}
			emp.Tags = tags
		}
	}
	if err := h.pgClient.UpdateEmployee(ctx, empID, emp); err != nil {
		return map[string]any{"error": "failed to update employee: " + err.Error()}
	}
	return map[string]any{"status": "updated", "employee_id": empID, "name": emp.Name}
}

func (h *APIHandler) execListSkillsTool(ctx context.Context, args map[string]any) map[string]any {
	if h.esClient == nil {
		return map[string]any{"error": "Elasticsearch not available"}
	}
	query, _ := args["query"].(string)
	skills, err := h.esClient.SearchSkills(ctx, query)
	if err != nil {
		return map[string]any{"error": "failed to search skills: " + err.Error()}
	}
	var results []map[string]any
	for _, s := range skills {
		results = append(results, map[string]any{
			"id": s.ID, "name": s.Name, "category": s.Category, "description": s.Description, "tags": s.Tags,
		})
	}
	if results == nil {
		results = []map[string]any{}
	}
	return map[string]any{"skills": results, "count": len(results)}
}

func (h *APIHandler) execAssignSkillTool(ctx context.Context, args map[string]any) map[string]any {
	if h.pgClient == nil {
		return map[string]any{"error": "database not available"}
	}
	empID, _ := args["employee_id"].(string)
	skillID, _ := args["skill_id"].(string)
	if empID == "" || skillID == "" {
		return map[string]any{"error": "employee_id and skill_id are required"}
	}
	if err := h.pgClient.AssignSkill(ctx, empID, skillID); err != nil {
		return map[string]any{"error": "failed to assign skill: " + err.Error()}
	}
	return map[string]any{"status": "assigned", "employee_id": empID, "skill_id": skillID}
}

func (h *APIHandler) execUnassignSkillTool(ctx context.Context, args map[string]any) map[string]any {
	if h.pgClient == nil {
		return map[string]any{"error": "database not available"}
	}
	empID, _ := args["employee_id"].(string)
	skillID, _ := args["skill_id"].(string)
	if empID == "" || skillID == "" {
		return map[string]any{"error": "employee_id and skill_id are required"}
	}
	if err := h.pgClient.UnassignSkill(ctx, empID, skillID); err != nil {
		return map[string]any{"error": "failed to unassign skill: " + err.Error()}
	}
	return map[string]any{"status": "unassigned", "employee_id": empID, "skill_id": skillID}
}

func (h *APIHandler) execListPromptsTool(ctx context.Context, args map[string]any) map[string]any {
	if h.esClient == nil {
		return map[string]any{"error": "Elasticsearch not available"}
	}
	query, _ := args["query"].(string)
	prompts, err := h.esClient.SearchPrompts(ctx, query)
	if err != nil {
		return map[string]any{"error": "failed to search prompts: " + err.Error()}
	}
	var results []map[string]any
	for _, p := range prompts {
		results = append(results, map[string]any{
			"id": p.ID, "title": p.Title, "tags": p.Tags,
		})
	}
	if results == nil {
		results = []map[string]any{}
	}
	return map[string]any{"prompts": results, "count": len(results)}
}

func (h *APIHandler) execCreatePromptTool(ctx context.Context, args map[string]any) map[string]any {
	if h.esClient == nil {
		return map[string]any{"error": "Elasticsearch not available"}
	}
	title, _ := args["title"].(string)
	content, _ := args["content"].(string)
	if title == "" || content == "" {
		return map[string]any{"error": "title and content are required"}
	}
	var tags []string
	if v, ok := args["tags"].([]any); ok {
		for _, t := range v {
			if s, ok := t.(string); ok {
				tags = append(tags, s)
			}
		}
	}
	if tags == nil {
		tags = []string{}
	}
	now := time.Now().UnixMilli()
	p := &Prompt{
		ID: generateID(), Title: title, Content: content, Tags: tags,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := h.esClient.IndexPrompt(ctx, p); err != nil {
		return map[string]any{"error": "failed to create prompt: " + err.Error()}
	}
	return map[string]any{"status": "created", "prompt_id": p.ID, "title": p.Title}
}

func (h *APIHandler) execUpdatePromptTool(ctx context.Context, args map[string]any) map[string]any {
	if h.esClient == nil {
		return map[string]any{"error": "Elasticsearch not available"}
	}
	promptID, _ := args["prompt_id"].(string)
	if promptID == "" {
		return map[string]any{"error": "prompt_id is required"}
	}
	p, err := h.esClient.GetPrompt(ctx, promptID)
	if err != nil {
		return map[string]any{"error": "prompt not found: " + err.Error()}
	}
	if v, ok := args["title"].(string); ok && v != "" {
		p.Title = v
	}
	if v, ok := args["content"].(string); ok && v != "" {
		p.Content = v
	}
	if v, ok := args["tags"].([]any); ok {
		var tags []string
		for _, t := range v {
			if s, ok := t.(string); ok {
				tags = append(tags, s)
			}
		}
		p.Tags = tags
	}
	p.UpdatedAt = time.Now().UnixMilli()
	if err := h.esClient.IndexPrompt(ctx, p); err != nil {
		return map[string]any{"error": "failed to update prompt: " + err.Error()}
	}
	return map[string]any{"status": "updated", "prompt_id": p.ID}
}

func (h *APIHandler) execDeletePromptTool(ctx context.Context, args map[string]any) map[string]any {
	if h.esClient == nil {
		return map[string]any{"error": "Elasticsearch not available"}
	}
	promptID, _ := args["prompt_id"].(string)
	if promptID == "" {
		return map[string]any{"error": "prompt_id is required"}
	}
	if err := h.esClient.DeletePrompt(ctx, promptID); err != nil {
		return map[string]any{"error": "failed to delete prompt: " + err.Error()}
	}
	return map[string]any{"status": "deleted", "prompt_id": promptID}
}

func (h *APIHandler) execStoreMemory(ctx context.Context, args map[string]any, agent *Employee, convID string) map[string]any {
	if h.esClient == nil {
		return map[string]any{"error": "memory storage not available"}
	}
	text, _ := args["memory_text"].(string)
	if text == "" {
		return map[string]any{"error": "memory_text is required"}
	}
	if err := h.esClient.IndexEmployeeMemoryDedup(ctx, agent.ID, convID, text); err != nil {
		return map[string]any{"error": "failed to store memory: " + err.Error()}
	}
	return map[string]any{"status": "remembered", "memory_text": text}
}

func (h *APIHandler) execForgetMemory(ctx context.Context, args map[string]any) map[string]any {
	if h.esClient == nil {
		return map[string]any{"error": "memory storage not available"}
	}
	memoryID, _ := args["memory_id"].(string)
	if memoryID == "" {
		return map[string]any{"error": "memory_id is required"}
	}
	if err := h.esClient.DeleteEmployeeMemory(ctx, memoryID); err != nil {
		return map[string]any{"error": "failed to forget: " + err.Error()}
	}
	return map[string]any{"status": "forgotten", "memory_id": memoryID}
}

func (h *APIHandler) execDelegateTask(ctx context.Context, args map[string]any, creator *Employee, conversationID string) map[string]any {
	if h.pgClient == nil {
		return map[string]any{"error": "database not available"}
	}

	assigneeID, _ := args["assignee_id"].(string)
	title, _ := args["title"].(string)
	goal, _ := args["goal"].(string)
	taskContext, _ := args["context"].(string)
	priority, _ := args["priority"].(string)

	if assigneeID == "" || title == "" || goal == "" {
		return map[string]any{"error": "assignee_id, title, and goal are required"}
	}

	assignee, err := h.pgClient.GetEmployee(ctx, assigneeID)
	if err != nil {
		return map[string]any{"error": "assignee not found: " + err.Error()}
	}

	if !canDelegate(creator, assignee) {
		return map[string]any{"error": fmt.Sprintf("cannot delegate to %s: outside your team hierarchy", assignee.Name)}
	}

	body := "## Goal\n" + goal
	if taskContext != "" {
		body += "\n\n## Context\n" + taskContext
	}

	if priority == "" {
		priority = "medium"
	}

	t := &Task{
		Title:    title,
		Body:     body,
		Priority: priority,
		Creator:  &EmployeeBrief{ID: creator.ID, Name: creator.Name, Title: creator.Title, Role: creator.Role},
		Assignee: &EmployeeBrief{ID: assignee.ID, Name: assignee.Name, Title: assignee.Title, Role: assignee.Role},
	}

	// Link the task to the project when delegated from within a project chat
	// (_project_id is injected into tool args by the chat handler).
	if pid, ok := args["_project_id"].(string); ok && pid != "" {
		t.ProjectID = &pid
	}

	if err := h.pgClient.CreateTask(ctx, t, nil); err != nil {
		return map[string]any{"error": "failed to create task: " + err.Error()}
	}

	if conversationID != "" {
		if err := h.pgClient.SetTaskConversationID(ctx, t.ID, conversationID); err != nil {
			slog.Error("failed to link task to conversation", "task_id", t.ID, "error", err)
		}
	}

	slog.Info("task delegated", "task_id", t.ID, "from", creator.Name, "to", assignee.Name, "title", title)
	return map[string]any{
		"status":  "created",
		"task_id": t.ID,
		"title":   t.Title,
		"assignee": map[string]any{
			"id":   assignee.ID,
			"name": assignee.Name,
		},
	}
}

func (h *APIHandler) execHireEmployee(ctx context.Context, args map[string]any, manager *Employee) map[string]any {
	if h.pgClient == nil {
		return map[string]any{"error": "database not available"}
	}

	if !hasTag(manager.Tags, "manager") && manager.Role != "CEO" {
		return map[string]any{"error": "only managers can hire employees"}
	}

	name, _ := args["name"].(string)
	title, _ := args["title"].(string)
	backstory, _ := args["backstory"].(string)
	primaryLLM, _ := args["primary_llm"].(string)

	if name == "" || title == "" || backstory == "" {
		return map[string]any{"error": "name, title, and backstory are required"}
	}

	fresh, err := h.pgClient.GetEmployee(ctx, manager.ID)
	if err != nil {
		return map[string]any{"error": "failed to load manager: " + err.Error()}
	}
	if reason, ok := checkHireDuplicate(fresh, title); !ok {
		return map[string]any{"error": reason}
	}

	emp := &Employee{
		Name:      name,
		Title:     title,
		Role:      "Custom",
		Backstory: backstory,
		Models:    []EmployeeModel{},
		Skills:    []EmployeeSkill{},
		Tags:      []string{},
		ManagerID: &manager.ID,
	}

	if primaryLLM != "" {
		emp.Models = append(emp.Models, EmployeeModel{ModelID: primaryLLM, Purpose: "primary_llm"})
	}

	if err := h.pgClient.CreateEmployee(ctx, emp); err != nil {
		return map[string]any{"error": "failed to create employee: " + err.Error()}
	}

	slog.Info("employee hired", "id", emp.ID, "name", name, "manager", manager.Name)
	return map[string]any{
		"status":      "hired",
		"employee_id": emp.ID,
		"name":        emp.Name,
		"title":       emp.Title,
		"reports_to":  manager.Name,
	}
}

func (h *APIHandler) execSubmitResult(ctx context.Context, args map[string]any) map[string]any {
	if h.pgClient == nil {
		return map[string]any{"error": "database not available"}
	}

	taskID, _ := args["task_id"].(string)
	result, _ := args["result"].(string)

	if taskID == "" || result == "" {
		return map[string]any{"error": "task_id and result are required"}
	}

	if err := h.pgClient.UpdateTask(ctx, taskID, nil, nil, nil, nil, &result); err != nil {
		return map[string]any{"error": "failed to update result: " + err.Error()}
	}

	if err := h.pgClient.UpdateTaskStatus(ctx, taskID, "needs_review", ""); err != nil {
		return map[string]any{"error": "failed to submit for review: " + err.Error()}
	}

	slog.Info("task result submitted", "task_id", taskID)
	return map[string]any{"status": "submitted_for_review", "task_id": taskID}
}

func (h *APIHandler) execReviewTask(ctx context.Context, args map[string]any, reviewer *Employee) map[string]any {
	if h.pgClient == nil {
		return map[string]any{"error": "database not available"}
	}

	taskID, _ := args["task_id"].(string)
	action, _ := args["action"].(string)
	feedback, _ := args["feedback"].(string)

	if taskID == "" || action == "" {
		return map[string]any{"error": "task_id and action are required"}
	}

	switch action {
	case "APPROVE":
		if err := h.pgClient.UpdateTaskStatus(ctx, taskID, "done", reviewer.ID); err != nil {
			return map[string]any{"error": "failed to approve: " + err.Error()}
		}
		slog.Info("task approved", "task_id", taskID, "reviewer", reviewer.Name)
		return map[string]any{"status": "approved", "task_id": taskID}

	case "REJECT":
		if feedback == "" {
			return map[string]any{"error": "feedback is required when rejecting"}
		}
		if err := h.pgClient.UpdateTaskStatus(ctx, taskID, "ready", reviewer.ID); err != nil {
			return map[string]any{"error": "failed to reject: " + err.Error()}
		}
		if _, err := h.pgClient.AddTaskComment(ctx, taskID, reviewer.ID, "REJECTED: "+feedback); err != nil {
			slog.Error("failed to add rejection comment", "task_id", taskID, "error", err)
		}
		slog.Info("task rejected", "task_id", taskID, "reviewer", reviewer.Name)
		return map[string]any{"status": "rejected", "task_id": taskID, "feedback": feedback}

	default:
		return map[string]any{"error": "action must be APPROVE or REJECT"}
	}
}

func (h *APIHandler) execListTeam(ctx context.Context, agent *Employee) map[string]any {
	if h.pgClient == nil {
		return map[string]any{"error": "database not available"}
	}

	emp, err := h.pgClient.GetEmployee(ctx, agent.ID)
	if err != nil {
		return map[string]any{"error": "failed to load employee: " + err.Error()}
	}

	var members []map[string]any
	for _, r := range emp.Reports {
		members = append(members, map[string]any{
			"id": r.ID, "name": r.Name, "title": r.Title, "role": r.Role,
		})
	}
	if members == nil {
		members = []map[string]any{}
	}

	return map[string]any{"team": members, "count": len(members)}
}

// HTTP handlers for REST API access (independent of chat)

func (h *APIHandler) HireEmployee(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}

	var body struct {
		HiringManagerID string          `json:"hiring_manager_id"`
		Name            string          `json:"name"`
		Title           string          `json:"title"`
		Role            string          `json:"role"`
		Backstory       string          `json:"backstory"`
		PrimaryLLM      string          `json:"primary_llm"`
		Skills          []EmployeeSkill `json:"skills"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if body.HiringManagerID == "" || body.Name == "" {
		writeError(w, "hiring_manager_id and name are required", http.StatusBadRequest)
		return
	}

	manager, err := h.pgClient.GetEmployee(r.Context(), body.HiringManagerID)
	if err != nil {
		writeError(w, "hiring manager not found", http.StatusNotFound)
		return
	}

	if !hasTag(manager.Tags, "manager") && manager.Role != "CEO" {
		writeError(w, "only managers can hire employees", http.StatusForbidden)
		return
	}

	if reason, ok := checkHireDuplicate(manager, body.Title); !ok {
		writeError(w, reason, http.StatusConflict)
		return
	}

	role := body.Role
	if role == "" {
		role = "Custom"
	}

	emp := &Employee{
		Name:      body.Name,
		Title:     body.Title,
		Role:      role,
		Backstory: body.Backstory,
		Models:    []EmployeeModel{},
		Skills:    body.Skills,
		Tags:      []string{},
		ManagerID: &body.HiringManagerID,
	}
	if emp.Skills == nil {
		emp.Skills = []EmployeeSkill{}
	}
	if body.PrimaryLLM != "" {
		emp.Models = append(emp.Models, EmployeeModel{ModelID: body.PrimaryLLM, Purpose: "primary_llm"})
	}

	if err := h.pgClient.CreateEmployee(r.Context(), emp); err != nil {
		writeError(w, "failed to create employee: "+err.Error(), http.StatusInternalServerError)
		return
	}

	full, err := h.pgClient.GetEmployee(r.Context(), emp.ID)
	if err != nil {
		full = emp
	}

	slog.Info("employee hired via API", "id", full.ID, "name", full.Name, "manager", manager.Name)
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, full)
}

func (h *APIHandler) DelegateTask(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}

	var body struct {
		CreatorID      string   `json:"creator_id"`
		AssigneeID     string   `json:"assignee_id"`
		Title          string   `json:"title"`
		Goal           string   `json:"goal"`
		Context        string   `json:"context"`
		Priority       string   `json:"priority"`
		ConversationID string   `json:"conversation_id"`
		Dependencies   []string `json:"dependencies"`
		ProjectID      string   `json:"project_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if body.CreatorID == "" || body.AssigneeID == "" || body.Title == "" || body.Goal == "" {
		writeError(w, "creator_id, assignee_id, title, and goal are required", http.StatusBadRequest)
		return
	}

	creator, err := h.pgClient.GetEmployee(r.Context(), body.CreatorID)
	if err != nil {
		writeError(w, "creator not found", http.StatusNotFound)
		return
	}
	assignee, err := h.pgClient.GetEmployee(r.Context(), body.AssigneeID)
	if err != nil {
		writeError(w, "assignee not found", http.StatusNotFound)
		return
	}

	if !canDelegate(creator, assignee) {
		writeError(w, fmt.Sprintf("cannot delegate to %s: outside team hierarchy", assignee.Name), http.StatusForbidden)
		return
	}

	taskBody := "## Goal\n" + body.Goal
	if body.Context != "" {
		taskBody += "\n\n## Context\n" + body.Context
	}

	priority := body.Priority
	if priority == "" {
		priority = "medium"
	}

	t := &Task{
		Title:    body.Title,
		Body:     taskBody,
		Priority: priority,
		Creator:  &EmployeeBrief{ID: creator.ID, Name: creator.Name, Title: creator.Title, Role: creator.Role},
		Assignee: &EmployeeBrief{ID: assignee.ID, Name: assignee.Name, Title: assignee.Title, Role: assignee.Role},
	}
	if body.ProjectID != "" {
		t.ProjectID = &body.ProjectID
	}

	deps := body.Dependencies
	if deps == nil {
		deps = []string{}
	}

	if err := h.pgClient.CreateTask(r.Context(), t, deps); err != nil {
		writeError(w, "failed to create task: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if body.ConversationID != "" {
		if err := h.pgClient.SetTaskConversationID(r.Context(), t.ID, body.ConversationID); err != nil {
			slog.Error("failed to link task to conversation", "task_id", t.ID, "error", err)
		}
	}

	full, err := h.pgClient.GetTask(r.Context(), t.ID)
	if err != nil {
		full = t
	}

	slog.Info("task delegated via API", "task_id", full.ID, "from", creator.Name, "to", assignee.Name)
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, full)
}
