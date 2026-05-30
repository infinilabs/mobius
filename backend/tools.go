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

func canDelegate(creator, assignee *Employee) bool {
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
