package main

import (
	"fmt"
	"strings"
)

var delegateTaskToolDef = ToolDef{
	Name:        "delegate_task",
	Description: "Delegate a task to a team member or peer manager. Creates a task on the task board.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"assignee_id": map[string]any{
				"type":        "string",
				"description": "UUID of the employee to delegate to (must be your direct report or a peer manager)",
			},
			"title": map[string]any{
				"type":        "string",
				"description": "Concise task title",
			},
			"goal": map[string]any{
				"type":        "string",
				"description": "Exact deliverables and requirements",
			},
			"context": map[string]any{
				"type":        "string",
				"description": "Background info: directories, rules, constraints",
			},
			"priority": map[string]any{
				"type":        "string",
				"description": "Task priority",
				"enum":        []string{"low", "medium", "high", "urgent"},
			},
			"project_id": map[string]any{
				"type":        "string",
				"description": "Optional project UUID. Omit to inherit from current task's project.",
			},
		},
		"required": []string{"assignee_id", "title", "goal"},
	},
}

var hireEmployeeToolDef = ToolDef{
	Name:        "hire_employee",
	Description: "Hire a specialized employee who reports directly to you. Give them a narrow, focused backstory — NOT general purpose.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Name reflecting their specialty, e.g. 'Bob (Rust Coder)'",
			},
			"title": map[string]any{
				"type":        "string",
				"description": "Specialized title, e.g. 'Rust Systems Developer'",
			},
			"backstory": map[string]any{
				"type":        "string",
				"description": "Highly focused backstory defining exact expertise and limitations",
			},
			"primary_llm": map[string]any{
				"type":        "string",
				"description": "LLM model ID for this employee, e.g. 'gemini-2.5-flash'",
			},
		},
		"required": []string{"name", "title", "backstory"},
	},
}

var submitTaskResultToolDef = ToolDef{
	Name:        "submit_task_result",
	Description: "Submit your completed work for review by the task creator.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id": map[string]any{
				"type":        "string",
				"description": "UUID of the task being completed",
			},
			"result": map[string]any{
				"type":        "string",
				"description": "The deliverable: code, analysis, design doc, etc.",
			},
		},
		"required": []string{"task_id", "result"},
	},
}

var reviewTaskToolDef = ToolDef{
	Name:        "review_task",
	Description: "Review a submitted task. Approve to mark done, or reject with feedback for revision.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id": map[string]any{
				"type":        "string",
				"description": "UUID of the task to review",
			},
			"action": map[string]any{
				"type":        "string",
				"description": "APPROVE or REJECT",
				"enum":        []string{"APPROVE", "REJECT"},
			},
			"feedback": map[string]any{
				"type":        "string",
				"description": "Required if rejecting. Specific revision instructions.",
			},
		},
		"required": []string{"task_id", "action"},
	},
}

var listTeamToolDef = ToolDef{
	Name:        "list_team",
	Description: "List your direct reports (employees on your team).",
	Parameters: map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	},
}

var storeMemoryToolDef = ToolDef{
	Name:        "store_memory",
	Description: "Record a critical fact, decision, or preference into your long-term memory for future conversations.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"memory_text": map[string]any{
				"type":        "string",
				"description": "A concise, single-sentence fact (e.g. 'We use pgx/v5 for PostgreSQL in this project').",
			},
		},
		"required": []string{"memory_text"},
	},
}

var forgetMemoryToolDef = ToolDef{
	Name:        "forget_memory",
	Description: "Remove an outdated or incorrect memory from your long-term memory.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"memory_id": map[string]any{
				"type":        "string",
				"description": "The ID of the memory to forget (shown in your Retrospective Learnings).",
			},
		},
		"required": []string{"memory_id"},
	},
}

var writeFileToolDef = ToolDef{
	Name:        "write_project_file",
	Description: "Write or overwrite a file in the current project's folder. Automatically indexes the file for search.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Relative path within the project, e.g. 'reports/analysis.md'",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "File content to write",
			},
		},
		"required": []string{"path", "content"},
	},
}

var readFileToolDef = ToolDef{
	Name:        "read_project_file",
	Description: "Read a file from the current project's folder.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Relative path within the project, e.g. 'reports/analysis.md'",
			},
		},
		"required": []string{"path"},
	},
}

var searchAssetsToolDef = ToolDef{
	Name:        "search_project_assets",
	Description: "Search for files in the current project by content or filename.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query to match against file content and names",
			},
			"type": map[string]any{
				"type":        "string",
				"description": "Filter by content type",
				"enum":        []string{"text", "code", "document", "image", "video", "audio", "binary"},
			},
		},
		"required": []string{"query"},
	},
}

var listAssetsToolDef = ToolDef{
	Name:        "list_project_assets",
	Description: "List all files in the current project.",
	Parameters: map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	},
}

var listTasksToolDef = ToolDef{
	Name:        "list_tasks",
	Description: "List tasks with optional filters. Returns task id, title, status, priority, assignee, and project.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"status": map[string]any{
				"type":        "string",
				"description": "Filter by status: todo, ready, in_progress, needs_review, done, blocked",
			},
			"assignee_id": map[string]any{
				"type":        "string",
				"description": "Filter by assignee employee UUID",
			},
			"project_id": map[string]any{
				"type":        "string",
				"description": "Filter by project UUID",
			},
		},
	},
}

var listProjectsToolDef = ToolDef{
	Name:        "list_projects",
	Description: "List all projects. Returns project id, name, owner, status, task count, and asset count.",
	Parameters: map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	},
}

var listEmployeesToolDef = ToolDef{
	Name:        "list_employees",
	Description: "List all employees. Returns id, name, title, role, tags, and manager.",
	Parameters: map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	},
}

var getEmployeeToolDef = ToolDef{
	Name:        "get_employee",
	Description: "Get detailed information about a specific employee, including their skills, models, reports, and manager.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"employee_id": map[string]any{
				"type":        "string",
				"description": "UUID of the employee",
			},
		},
		"required": []string{"employee_id"},
	},
}

var updateTaskStatusToolDef = ToolDef{
	Name:        "update_task_status",
	Description: "Move a task to a new status. Valid transitions: todo→ready, ready→in_progress, in_progress→needs_review, needs_review→done/ready, blocked→ready. Rejecting (needs_review→ready) requires feedback.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id": map[string]any{
				"type":        "string",
				"description": "UUID of the task",
			},
			"status": map[string]any{
				"type":        "string",
				"description": "Target status",
				"enum":        []string{"todo", "ready", "in_progress", "needs_review", "done", "blocked"},
			},
			"feedback": map[string]any{
				"type":        "string",
				"description": "Required when rejecting (needs_review → ready). Specific feedback for revision.",
			},
		},
		"required": []string{"task_id", "status"},
	},
}

// --- Prompt tools ---

var listPromptsToolDef = ToolDef{
	Name:        "list_prompts",
	Description: "Search or list saved prompt templates.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": "Search query (optional, lists all if empty)"},
		},
	},
}

var createPromptToolDef = ToolDef{
	Name:        "create_prompt",
	Description: "Create a new prompt template.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title":   map[string]any{"type": "string", "description": "Prompt title"},
			"content": map[string]any{"type": "string", "description": "Prompt content/template text"},
			"tags":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Tags for categorization"},
		},
		"required": []string{"title", "content"},
	},
}

var updatePromptToolDef = ToolDef{
	Name:        "update_prompt",
	Description: "Update an existing prompt template.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt_id": map[string]any{"type": "string", "description": "UUID of the prompt"},
			"title":     map[string]any{"type": "string", "description": "New title (optional)"},
			"content":   map[string]any{"type": "string", "description": "New content (optional)"},
			"tags":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "New tags (optional)"},
		},
		"required": []string{"prompt_id"},
	},
}

var deletePromptToolDef = ToolDef{
	Name:        "delete_prompt",
	Description: "Delete a prompt template.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt_id": map[string]any{"type": "string", "description": "UUID of the prompt to delete"},
		},
		"required": []string{"prompt_id"},
	},
}

// --- Skill tools ---

var listSkillsToolDef = ToolDef{
	Name:        "list_skills",
	Description: "Search or list skills from the catalog.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": "Search query (optional, lists all if empty)"},
		},
	},
}

var assignSkillToolDef = ToolDef{
	Name:        "assign_skill",
	Description: "Assign a catalog skill to an employee.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"employee_id": map[string]any{"type": "string", "description": "UUID of the employee"},
			"skill_id":    map[string]any{"type": "string", "description": "UUID of the catalog skill"},
		},
		"required": []string{"employee_id", "skill_id"},
	},
}

var unassignSkillToolDef = ToolDef{
	Name:        "unassign_skill",
	Description: "Remove a catalog skill from an employee.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"employee_id": map[string]any{"type": "string", "description": "UUID of the employee"},
			"skill_id":    map[string]any{"type": "string", "description": "UUID of the catalog skill to remove"},
		},
		"required": []string{"employee_id", "skill_id"},
	},
}

// --- Employee tools ---

var updateEmployeeToolDef = ToolDef{
	Name:        "update_employee",
	Description: "Update an employee's details: title, backstory, tags, or role.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"employee_id": map[string]any{"type": "string", "description": "UUID of the employee"},
			"title":       map[string]any{"type": "string", "description": "New title (optional)"},
			"backstory":   map[string]any{"type": "string", "description": "New backstory (optional)"},
			"tags":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "New tags (optional)"},
		},
		"required": []string{"employee_id"},
	},
}

// --- Task tools ---

var updateTaskToolDef = ToolDef{
	Name:        "update_task",
	Description: "Update a task's title, body, priority, or assignee.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id":     map[string]any{"type": "string", "description": "UUID of the task"},
			"title":       map[string]any{"type": "string", "description": "New title (optional)"},
			"body":        map[string]any{"type": "string", "description": "New body/description (optional)"},
			"priority":    map[string]any{"type": "string", "description": "New priority: low, medium, high, urgent (optional)"},
			"assignee_id": map[string]any{"type": "string", "description": "New assignee employee UUID (optional)"},
		},
		"required": []string{"task_id"},
	},
}

var addTaskCommentToolDef = ToolDef{
	Name:        "add_task_comment",
	Description: "Add a comment to a task's history.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id": map[string]any{"type": "string", "description": "UUID of the task"},
			"content": map[string]any{"type": "string", "description": "Comment text"},
		},
		"required": []string{"task_id", "content"},
	},
}

var getTaskToolDef = ToolDef{
	Name:        "get_task",
	Description: "Get detailed information about a specific task, including status, result, and comments.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id": map[string]any{"type": "string", "description": "UUID of the task"},
		},
		"required": []string{"task_id"},
	},
}

// --- Project tools ---

var updateProjectToolDef = ToolDef{
	Name:        "update_project",
	Description: "Update a project's description or status.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"project_id":  map[string]any{"type": "string", "description": "UUID of the project"},
			"description": map[string]any{"type": "string", "description": "New description (optional)"},
			"status":      map[string]any{"type": "string", "description": "New status: active or paused (optional)"},
		},
		"required": []string{"project_id"},
	},
}

var createProjectToolDef = ToolDef{
	Name:        "create_project",
	Description: "Create a new project. You become the project owner. Use this when the user explicitly asks to create a project, or suggest it when a task is complex enough to warrant its own project workspace.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Short, lowercase, hyphen-separated project name (e.g. 'q3-campaign', 'backend-rewrite')",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "Brief description of the project's purpose and scope",
			},
		},
		"required": []string{"name"},
	},
}

var runCommandToolDef = ToolDef{
	Name:        "run_project_command",
	Description: "Execute a shell command in the project directory. Use for running tests, builds, linters, or any verification command. Returns stdout, stderr, and exit code. Timeout: 2 minutes.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The shell command to execute, e.g. 'go test ./...', 'npm test', 'make build'",
			},
		},
		"required": []string{"command"},
	},
}

var verifyDeliverableToolDef = ToolDef{
	Name:        "verify_deliverable",
	Description: "Inspect what files a task produced. Returns the list of project files created or modified by the task, with sizes and content previews. Use this BEFORE approving a task to verify actual work was done.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id": map[string]any{
				"type":        "string",
				"description": "UUID of the task whose deliverables to inspect",
			},
		},
		"required": []string{"task_id"},
	},
}

var askUserToolDef = ToolDef{
	Name:        "ask_user",
	Description: "Ask the task creator or board a blocking question. The task will pause until answered. Use when you need clarification, approval, or a decision before proceeding.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"question": map[string]any{
				"type":        "string",
				"description": "The question to ask",
			},
			"options": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional list of choices for the user to pick from",
			},
		},
		"required": []string{"question"},
	},
}

var suggestTasksToolDef = ToolDef{
	Name:        "suggest_tasks",
	Description: "Propose a list of sub-tasks for approval. The manager or board will review and approve, modify, or reject the breakdown.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tasks": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"title":       map[string]any{"type": "string", "description": "Task title"},
						"goal":        map[string]any{"type": "string", "description": "What this sub-task should achieve"},
						"assignee_id": map[string]any{"type": "string", "description": "Optional employee UUID to assign to"},
						"priority":    map[string]any{"type": "string", "description": "low, medium, high, or urgent"},
					},
					"required": []string{"title", "goal"},
				},
			},
			"rationale": map[string]any{
				"type":        "string",
				"description": "Why this decomposition makes sense",
			},
		},
		"required": []string{"tasks"},
	},
}

// --- Media tagging (video_tagging.md §4.4, §11). Gated on the media_tagger tag. ---

var tagMediaToolDef = ToolDef{
	Name: "tag_media",
	Description: "Batch-tag video/image media under a GCS path using BigQuery multimodal AI. " +
		"Creates an object table over the media, runs schema-enforced tagging, and stores a tags table. " +
		"Collect gcs_path (and bq_dataset if non-default) from the user before calling; ask if unknown.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"gcs_path": map[string]any{
				"type":        "string",
				"description": "Source media prefix as a glob, e.g. gs://bucket/creatives/*",
			},
			"taxonomy_prompt_id": map[string]any{
				"type":        "string",
				"description": "Optional prompt ID for the label taxonomy. Defaults to the 'video label tagging' template.",
			},
		},
		"required": []string{"gcs_path"},
	},
}

var getTagResultsToolDef = ToolDef{
	Name:        "get_tag_results",
	Description: "Fetch per-asset labels for a completed tagging job. Provide job_id (or tags_table); optionally asset_id.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"job_id":     map[string]any{"type": "string", "description": "Job ID returned by tag_media"},
			"tags_table": map[string]any{"type": "string", "description": "Tags table name (alternative to job_id)"},
			"asset_id":   map[string]any{"type": "string", "description": "Optional: filter to a single asset"},
		},
	},
}

var queryTagsToolDef = ToolDef{
	Name: "query_tags",
	Description: "Run a READ-ONLY SELECT over the creatives tags dataset to answer analytics questions " +
		"(counts, Top-N, per-tag totals). Translate the user's question into a single SELECT (use UNNEST(labels) " +
		"to count tags). Results are row-capped. Then narrate the answer and emit ONE ```mobius-viz``` JSON block.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"sql": map[string]any{
				"type":        "string",
				"description": "A single SELECT statement over the creatives tags dataset. No DML/DDL, no comments.",
			},
		},
		"required": []string{"sql"},
	},
}

var watermarkAssetsToolDef = ToolDef{
	Name: "watermark_assets",
	Description: "Embed an invisible watermark into an image or video asset (chat upload) or bulk prefix inside the configured GCS bucket. Large folders or videos run asynchronously.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input_path": map[string]any{
				"type":        "string",
				"description": "GCS path relative to configured bucket (e.g. inputs/image.png) or local file path from chat upload",
			},
			"output_path": map[string]any{
				"type":        "string",
				"description": "GCS destination path or directory relative to bucket (e.g. outputs/)",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "Message to embed",
			},
			"password": map[string]any{
				"type":        "string",
				"description": "Password for encryption. Must be at least 4 characters.",
			},
			"intensity": map[string]any{
				"type":        "integer",
				"description": "Optional: intensity 1-10. Defaults to 0 (auto).",
			},
		},
		"required": []string{"input_path", "output_path", "message", "password"},
	},
}

var verifyWatermarkToolDef = ToolDef{
	Name: "verify_watermark",
	Description: "Extract and verify an invisible watermark from an image or video file path in the configured GCS bucket (or local path) using a password.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input_path": map[string]any{
				"type":        "string",
				"description": "GCS path relative to bucket (e.g. inputs/image.png) or local file path",
			},
			"password": map[string]any{
				"type":        "string",
				"description": "Password used during embedding.",
			},
		},
		"required": []string{"input_path", "password"},
	},
}

var playableLoadReferenceGameToolDef = ToolDef{
	Name:        "playable_load_reference_game",
	Description: "Load game reference template code. Massive base64 media data URIs are stripped automatically to save context window tokens.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"game_type": map[string]any{
				"type":        "string",
				"description": "Game genre, e.g. 'match3', 'tile_match', 'vertical_shooter'",
			},
		},
		"required": []string{"game_type"},
	},
}

var playableGetTrackingSDKToolDef = ToolDef{
	Name:        "playable_get_tracking_sdk",
	Description: "Get the PlayableTracker tracking SDK snippet to inject into the game HTML's <head>.",
	Parameters: map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	},
}

var playableGetWebAudioSFXToolDef = ToolDef{
	Name:        "playable_get_web_audio_sfx",
	Description: "Get helper JS code templates for synthesizing sound effects procedures (laser, explosion, click) using browser Web Audio API.",
	Parameters: map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	},
}

var playableWriteHTMLToolDef = ToolDef{
	Name:        "playable_write_html",
	Description: "Write HTML game code and compile local assets into a self-contained inlined preview. Runs basic compliance size and eval check.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"html_content": map[string]any{
				"type":        "string",
				"description": "The target raw index.html content (referencing assets/image.png etc).",
			},
			"pipeline_id": map[string]any{
				"type":        "string",
				"description": "Pipeline or run ID used to identify directory.",
			},
		},
		"required": []string{"html_content", "pipeline_id"},
	},
}

var generateImageToolDef = ToolDef{
	Name:        "generate_image",
	Description: "Generate a new image asset from a text prompt using Vertex AI Imagen 3.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "Detailed visual prompt describing the desired sprite/background/UI asset.",
			},
			"size": map[string]any{
				"type":        "string",
				"description": "Target dimensions, e.g. '256x256', '512x512', '1024x1024'. Defaults to '512x512'.",
			},
			"output_path": map[string]any{
				"type":        "string",
				"description": "Relative output path in project (e.g. output/pipeline_123/assets/tomato.png)",
			},
			"transparent": map[string]any{
				"type":        "boolean",
				"description": "Set true for sprites/icons/game pieces: removes the flat chroma-key background, crops to content, and adds a white outline. Leave false/unset for full-bleed backgrounds. Requires the prompt to render the subject on a solid magenta (#ff00ff) background.",
			},
		},
		"required": []string{"prompt", "output_path"},
	},
}

var generateAudioToolDef = ToolDef{
	Name:        "generate_audio",
	Description: "Generate a short sound effect or music clip from a text prompt using Vertex AI.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "Acoustic prompt, e.g. 'retro game laser beep', 'farm ambient music loop'.",
			},
			"duration_sec": map[string]any{
				"type":        "number",
				"description": "Desired duration in seconds (1 to 15).",
			},
			"output_path": map[string]any{
				"type":        "string",
				"description": "Relative output path in project (e.g. output/pipeline_123/assets/laser.wav)",
			},
		},
		"required": []string{"prompt", "output_path"},
	},
}

var publishPlayableAdToolDef = ToolDef{
	Name:        "publish_playable_ad",
	Description: "Registers the compiled playable ad in the project creatives library and optionally uploads to production GCS.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pipeline_id": map[string]any{
				"type":        "string",
				"description": "Pipeline or run ID to locate the compiled preview files.",
			},
			"publish_to_gcs": map[string]any{
				"type":        "boolean",
				"description": "If true, uploads assets to production GCS bucket and returns a signed production URL. Otherwise, registers locally and returns a local preview URL.",
			},
		},
		"required": []string{"pipeline_id"},
	},
}

var saveUploadToAssetsToolDef = ToolDef{
	Name:        "save_upload_to_assets",
	Description: "Save the image or file the user most recently uploaded in this chat into the current project's asset library, so it can be used as part of a playable ad. Optionally place it at a specific relative path and tag it.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"relative_path": map[string]any{
				"type":        "string",
				"description": "Destination path within the project, e.g. \"assets/hero.png\". Defaults to assets/<original filename>.",
			},
			"tags": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional tags to attach to the asset, e.g. [\"playable_asset\",\"background\"].",
			},
		},
	},
}

func buildAgentTools(agent *Employee, task *Task) []ToolDef {
	var tools []ToolDef

	tools = append(tools, submitTaskResultToolDef, listTeamToolDef)
	tools = append(tools, storeMemoryToolDef, forgetMemoryToolDef)
	tools = append(tools, askUserToolDef, suggestTasksToolDef)

	if agent.Role == "CEO" || hasTag(agent.Tags, "manager") || hasTag(agent.Tags, "founder") {
		tools = append(tools, createProjectToolDef, updateProjectToolDef)
		tools = append(tools, listTasksToolDef, getTaskToolDef, updateTaskToolDef, updateTaskStatusToolDef, addTaskCommentToolDef)
		tools = append(tools, listProjectsToolDef, listEmployeesToolDef, getEmployeeToolDef, updateEmployeeToolDef)
		tools = append(tools, listSkillsToolDef, assignSkillToolDef, unassignSkillToolDef)
		tools = append(tools, listPromptsToolDef, createPromptToolDef, updatePromptToolDef, deletePromptToolDef)
	}

	if agent.Role == "CEO" || hasTag(agent.Tags, "manager") {
		tools = append(tools, delegateTaskToolDef, reviewTaskToolDef, verifyDeliverableToolDef)
	}

	if hasTag(agent.Tags, "manager") {
		tools = append(tools, hireEmployeeToolDef)
	}

	if task != nil && task.ProjectID != nil {
		tools = append(tools, writeFileToolDef, readFileToolDef, searchAssetsToolDef, listAssetsToolDef, runCommandToolDef)
	}

	// Media tagging is gated on the media_tagger tag (not project context): the
	// Creative Tagger employee batch-tags GCS prefixes and answers tag analytics.
	if hasTag(agent.Tags, "media_tagger") {
		tools = append(tools, tagMediaToolDef, getTagResultsToolDef, queryTagsToolDef)
	}

	if hasTag(agent.Tags, "media_watermarker") {
		tools = append(tools, watermarkAssetsToolDef, verifyWatermarkToolDef)
	}

	// Playable Ads team tools
	if hasTag(agent.Tags, "playable_planner") {
		// Planner tools (if any)
	}
	if hasTag(agent.Tags, "playable_designer") {
		tools = append(tools, generateImageToolDef)
	}
	if hasTag(agent.Tags, "playable_developer") {
		tools = append(tools, playableLoadReferenceGameToolDef, playableGetTrackingSDKToolDef, playableGetWebAudioSFXToolDef, playableWriteHTMLToolDef, generateImageToolDef, generateAudioToolDef)
	}
	if hasTag(agent.Tags, "playable_publisher") {
		tools = append(tools, publishPlayableAdToolDef)
	}

	// Adding user chat uploads to a project's assets is available to management and the
	// playable team regardless of task context, since it is driven from interactive chat.
	if agent.Role == "CEO" || hasTag(agent.Tags, "manager") || hasTag(agent.Tags, "founder") ||
		hasTag(agent.Tags, "playable_planner") || hasTag(agent.Tags, "playable_designer") ||
		hasTag(agent.Tags, "playable_developer") {
		tools = append(tools, saveUploadToAssetsToolDef)
	}

	return tools
}

const maxDirectReports = 8

func checkHireDuplicate(manager *Employee, newTitle string) (string, bool) {
	if len(manager.Reports) >= maxDirectReports {
		return fmt.Sprintf("team full: you already have %d direct reports (max %d). Delegate to an existing member instead", len(manager.Reports), maxDirectReports), false
	}

	newLower := strings.ToLower(newTitle)
	for _, r := range manager.Reports {
		existingLower := strings.ToLower(r.Title)
		if existingLower == newLower {
			return fmt.Sprintf("duplicate hire blocked: you already have '%s' (%s) with title '%s'. Delegate to them instead of hiring again", r.Name, r.ID, r.Title), false
		}
		if titleOverlap(newLower, existingLower) {
			return fmt.Sprintf("similar role exists: '%s' (%s) with title '%s'. Delegate to them or justify why a separate hire is needed", r.Name, r.ID, r.Title), false
		}
	}
	return "", true
}

func titleOverlap(a, b string) bool {
	keywords := func(s string) map[string]bool {
		m := make(map[string]bool)
		for _, w := range strings.Fields(s) {
			w = strings.Trim(w, "()[]{}.,;:!?-")
			if len(w) > 2 {
				m[w] = true
			}
		}
		return m
	}
	ka, kb := keywords(a), keywords(b)
	noise := map[string]bool{"the": true, "and": true, "for": true, "with": true, "senior": true, "junior": true, "lead": true, "staff": true, "principal": true}
	var overlap int
	var total int
	for w := range ka {
		if noise[w] {
			continue
		}
		total++
		if kb[w] {
			overlap++
		}
	}
	return total > 0 && overlap > 0 && float64(overlap)/float64(total) >= 0.5
}

func managerDirectives() string {
	return "\n\n## SYSTEM DIRECTIVE: Quality Gate\n" +
		"As a manager, you review work from your team. When a task has status 'needs_review':\n" +
		"1. Call verify_deliverable to see what files were produced.\n" +
		"2. Read key deliverable files with read_project_file to check code quality.\n" +
		"3. Run tests with run_project_command (e.g. 'go test ./...', 'npm test', 'pytest') to verify correctness.\n" +
		"4. If tests fail or quality issues exist: call review_task with action=\"REJECT\" and specific feedback.\n" +
		"5. If the work is correct AND tests pass: call review_task with action=\"APPROVE\".\n" +
		"Do NOT approve without running tests. Reading code alone is not sufficient — you must execute verification.\n" +
		"If you lack the expertise to verify, delegate a verification task to a QA/testing team member." +
		"\n\n## SYSTEM DIRECTIVE: Delegation & Hiring\n" +
		"You are a manager. You do NOT do implementation work yourself. Your job is to delegate.\n\n" +
		"### Step 1: Check your existing team\n" +
		"ALWAYS call list_team FIRST. If a direct report already has the right expertise, delegate to them.\n\n" +
		"### Step 2: Hire only when no existing report fits\n" +
		"If the task requires domain expertise not covered by your current team, hire a specialist.\n\n" +
		"### Step 3: Hire FOCUSED specialists, never generalists\n" +
		"When hiring, the backstory MUST define:\n" +
		"- EXACT technology or domain (e.g. 'Rust async I/O with Tokio', not 'backend development')\n" +
		"- What they should NOT attempt (e.g. 'Does not handle frontend, databases, or DevOps')\n" +
		"- Expected output format (e.g. 'Produces production-ready Go code with tests')\n\n" +
		"BAD hire: name='Alex', title='Software Engineer', backstory='A skilled developer who can build anything.'\n" +
		"GOOD hire: name='Alex (Rust Coder)', title='Rust Systems Developer', " +
		"backstory='Expert in Rust systems programming with deep knowledge of async runtimes (Tokio), " +
		"zero-copy parsing, and unsafe FFI boundaries. Produces idiomatic, safe Rust with comprehensive " +
		"error handling. Does not handle frontend, databases, or infrastructure work.'\n\n" +
		"### Step 4: One specialist per skill domain\n" +
		"Do NOT hire one person for multiple unrelated domains. If a task needs both Rust and React, " +
		"hire two specialists and delegate separate sub-tasks to each.\n\n" +
		"### Step 5: NO DUPLICATE HIRES\n" +
		"You may only have ONE specialist per domain. If you already have a Rust developer, " +
		"delegate all Rust work to that person — do NOT hire a second Rust developer. " +
		"The system enforces this: hiring a duplicate role will be rejected. " +
		"Reuse your existing team members across multiple tasks."
}

// maxDelegationDepth bounds delegation chains (plan 1.1). A delegated task
// carries its parent's depth + 1, so a runaway delegate spiral — including an
// A→B→A ping-pong, which depth alone cannot distinguish from a legitimate
// chain — terminates instead of recursing forever.
const maxDelegationDepth = 5

// exceedsDelegationDepth reports whether delegating from a task at parentDepth
// would push the chain past maxDelegationDepth.
func exceedsDelegationDepth(parentDepth int) bool {
	return parentDepth+1 > maxDelegationDepth
}

func canDelegate(creator, assignee *Employee) bool {
	// Delegating to yourself creates a task loop that never converges: the same
	// agent keeps re-receiving its own work. Refused for everyone, CEO included.
	if creator.ID == assignee.ID {
		return false
	}
	if creator.Role == "CEO" {
		return true
	}
	if !hasTag(creator.Tags, "manager") {
		return false
	}
	if assignee.ManagerID != nil && *assignee.ManagerID == creator.ID {
		return true
	}
	if hasTag(assignee.Tags, "manager") {
		return true
	}
	return false
}

func hasTag(tags []string, target string) bool {
	for _, t := range tags {
		if t == target {
			return true
		}
	}
	return false
}
