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

func buildAgentTools(agent *Employee) []ToolDef {
	var tools []ToolDef

	tools = append(tools, submitTaskResultToolDef, listTeamToolDef)
	tools = append(tools, storeMemoryToolDef, forgetMemoryToolDef)

	if agent.Role == "CEO" {
		tools = append(tools, delegateTaskToolDef, reviewTaskToolDef)
	}

	if hasTag(agent.Tags, "manager") {
		tools = append(tools, delegateTaskToolDef, hireEmployeeToolDef, reviewTaskToolDef)
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
		"1. Inspect the result carefully — check code, specs, or analysis for correctness.\n" +
		"2. Do NOT blindly approve. Verify the deliverable meets the goal.\n" +
		"3. If quality issues exist: call review_task with action=\"REJECT\" and specific feedback.\n" +
		"4. If the work is complete and correct: call review_task with action=\"APPROVE\"." +
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
