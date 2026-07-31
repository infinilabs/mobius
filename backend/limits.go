package main

import "fmt"

// Input length caps on LLM-supplied tool args (plan 3.5): a runaway or
// prompt-injected agent must not be able to stuff megabytes into task rows,
// employee records, or its own context window. Shared by all three dispatch
// paths (adapter, chat, MCP), so the caps cannot drift apart.

const (
	// maxTitleArgLen caps short identity fields: task titles, employee
	// names/titles.
	maxTitleArgLen = 300
	// maxTextArgLen caps long free-text fields: goal, context, backstory.
	maxTextArgLen = 16_384
	// maxToolResultBytes caps file content fed back into the LLM context by
	// read_project_file.
	maxToolResultBytes = 65_536
)

func checkArgLen(field, val string, max int) error {
	if len(val) > max {
		return fmt.Errorf("%s exceeds the %d-character limit (got %d)", field, max, len(val))
	}
	return nil
}

// validateDelegateArgs caps delegate_task's free-text args on every path.
func validateDelegateArgs(title, goal, taskContext string) error {
	if err := checkArgLen("title", title, maxTitleArgLen); err != nil {
		return err
	}
	if err := checkArgLen("goal", goal, maxTextArgLen); err != nil {
		return err
	}
	return checkArgLen("context", taskContext, maxTextArgLen)
}

// validateHireArgs caps hire_employee's free-text args on every path.
func validateHireArgs(name, title, backstory string) error {
	if err := checkArgLen("name", name, maxTitleArgLen); err != nil {
		return err
	}
	if err := checkArgLen("title", title, maxTitleArgLen); err != nil {
		return err
	}
	return checkArgLen("backstory", backstory, maxTextArgLen)
}

// truncateForContext caps tool output destined for the LLM context, keeping a
// visible marker so the model knows content is missing.
func truncateForContext(s string) (string, bool) {
	if len(s) <= maxToolResultBytes {
		return s, false
	}
	return s[:maxToolResultBytes] + fmt.Sprintf("\n…[truncated: showing %d of %d bytes]", maxToolResultBytes, len(s)), true
}
