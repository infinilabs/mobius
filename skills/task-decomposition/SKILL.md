---
name: task-decomposition
description: "Decompose goals into routable tasks for team execution."
version: 1.0.0
category: software-development
tags: [delegation, orchestration, routing, multi-agent]
---

# Task Decomposition

## Overview

Decompose user goals into concrete tasks that can be routed to the right team member. The orchestrator's job is to route, not execute.

**Core principle:** Decompose, route, and summarize — that's the whole job.

## When to Decompose

1. **Multiple specialists needed** — research + implementation + review
2. **Work should be tracked** — important or long-running tasks
3. **Multiple subtasks can run in parallel** — fan-out for speed
4. **Review/iteration expected** — reviewer loops on implementer output

If none apply — it's a small one-shot task — handle directly.

## The Process

### Step 1: Understand the Goal

Ask clarifying questions if the goal is ambiguous. Cheap to ask; expensive to route wrong.

### Step 2: Sketch the Task Graph

Before creating tasks, draft the graph:
1. Extract independent workstreams from the request
2. Map each workstream to the right team member by role
3. Decide which are independent vs. dependent
4. Independent tasks run in parallel
5. Dependent tasks wait for prerequisites

### Step 3: Create and Route Tasks

For each task specify:
- **Title:** concise, actionable (verb + noun)
- **Assignee:** which team member by role
- **Body:** full context the assignee needs
- **Dependencies:** which tasks must complete first

### Step 4: Monitor and Summarize

- Track completion of each task
- Resolve blockers
- Summarize results back to the user

## Anti-Temptation Rules

- **Do not execute work yourself** — route to the right specialist
- **Split multi-part requests before routing** — one task per workstream
- **Run independent tasks in parallel** — only link true data dependencies
- **Never create dependent work as independent** — explicit dependencies

## Role Mapping

| Role | Best For |
|------|----------|
| CEO | Strategic decisions, final approval |
| PM | Planning, spec writing, requirements |
| Engineer | Implementation, debugging, coding |
| QA | Testing, code review, verification |

## Task Sizing

Each task should take 2-10 minutes of focused work. If a task feels bigger, decompose further.

**Too big:** "Build the authentication system"
**Right size:** "Create login endpoint", "Add password validation", "Write login tests"
