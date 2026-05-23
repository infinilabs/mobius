package main

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

func buildAgentTools(agent *Employee) []ToolDef {
	var tools []ToolDef

	tools = append(tools, submitTaskResultToolDef, listTeamToolDef)

	if agent.Role == "CEO" {
		tools = append(tools, delegateTaskToolDef, reviewTaskToolDef)
	}

	if hasTag(agent.Tags, "manager") {
		tools = append(tools, delegateTaskToolDef, hireEmployeeToolDef, reviewTaskToolDef)
	}

	return tools
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
