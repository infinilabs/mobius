---
name: codebase-inspection
description: "Deep codebase analysis: trace architecture and patterns."
version: 1.0.0
category: software-development
tags: [analysis, architecture, codebase, patterns]
---

# Codebase Inspection

## Overview

Systematically understand a codebase before making changes. Map the architecture, identify patterns, and find the right integration points.

## When to Use

- Before implementing a new feature
- When onboarding to an unfamiliar codebase
- Before refactoring
- When debugging requires understanding the broader system

## Inspection Process

### 1. Project Structure

- Map the directory layout and file organization
- Identify entry points (main files, routers, handlers)
- Find configuration files and their format
- Understand the build system and dependencies

### 2. Architecture Patterns

- Identify the architectural style (MVC, layered, microservices, etc.)
- Map data flow: request → handler → service → data layer → response
- Find shared utilities and helpers
- Identify cross-cutting concerns (logging, auth, error handling)

### 3. Conventions

- Naming conventions (files, functions, variables)
- Error handling patterns (return error? panic? middleware?)
- Testing patterns (table-driven? mocks? integration?)
- Code organization within files (struct → methods → handlers)

### 4. Integration Points

- Where does new code plug in?
- What existing patterns should be followed?
- What utilities can be reused?
- What tests exist that might break?

## Output

After inspection, document:
1. **Architecture summary** — 3-5 sentences on how the system is organized
2. **Key files** — the 5-10 files most relevant to the current task
3. **Patterns to follow** — what conventions to match
4. **Risks** — what might break or be harder than expected

## Rules

1. **Read before you write** — understand exports, callers, shared utilities
2. **Find 3 similar features** — learn from existing implementations
3. **Match existing style** — even if you'd do it differently
4. **Note dependencies** — what other code depends on what you'll change
