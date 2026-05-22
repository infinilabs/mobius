---
name: writing-plans
description: "Write implementation plans: bite-sized tasks with paths and code."
version: 1.0.0
category: software-development
tags: [planning, design, implementation, workflow]
---

# Writing Implementation Plans

## Overview

Write comprehensive implementation plans assuming the implementer has zero context for the codebase. Document everything: which files to touch, complete code, testing commands, how to verify. Give them bite-sized tasks. DRY. YAGNI. TDD. Frequent commits.

## Plan Structure

### 1. Context Section

- What problem are we solving?
- Why now?
- What's the intended outcome?
- What constraints exist?

### 2. Task Breakdown

Each task should be 2-5 minutes of focused work. Include:
- **What to do** — specific files to create/modify
- **How to verify** — exact command to run
- **Dependencies** — what must be done first

**Too big:** "Implement user authentication"
**Right size:** "Add password hashing function", "Create login endpoint", "Add JWT generation"

### 3. Per-Task Details

For each task provide:
- File paths (exact, not vague)
- Code snippets (complete, not pseudocode)
- Test commands
- Success criteria

### 4. Verification Section

- How to test end-to-end
- What to check manually
- Expected outputs

## Rules

1. **No vague tasks** — "improve performance" is not a task. "Add index on users.email" is.
2. **No assumptions** — spell out every step. The implementer doesn't know your shortcuts.
3. **Order matters** — tasks must be sequenced so each one compiles independently.
4. **Include rollback** — what to undo if something goes wrong.
5. **Keep it DRY** — if you repeat yourself, extract a common section.

## Task Granularity

Each task = one atomic change that:
- Compiles on its own
- Can be tested independently
- Can be committed separately
- Takes 2-5 minutes

## Template

```markdown
## Task N: [Verb] [What]

**Files:** `path/to/file.go`

**Changes:**
[Exact code or description]

**Verify:**
\`\`\`bash
go build ./... && go test ./...
\`\`\`

**Done when:** [Specific observable outcome]
```
