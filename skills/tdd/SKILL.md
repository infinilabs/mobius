---
name: test-driven-development
description: "TDD: enforce RED-GREEN-REFACTOR, tests before code."
version: 1.0.0
category: software-development
tags: [testing, tdd, quality, red-green-refactor]
---

# Test-Driven Development (TDD)

## Overview

Write the test first. Watch it fail. Write minimal code to pass.

**Core principle:** If you didn't watch the test fail, you don't know if it tests the right thing.

## The Iron Law

```
NO PRODUCTION CODE WITHOUT A FAILING TEST FIRST
```

Write code before the test? Delete it. Start over. No exceptions.

## Red-Green-Refactor Cycle

### RED — Write Failing Test

Write one minimal test showing what should happen.

**Good test:**
- One behavior per test
- Clear descriptive name ("and" in name? Split it)
- Real code, not mocks (unless truly unavoidable)
- Name describes behavior, not implementation

### Verify RED — Watch It Fail

**MANDATORY. Never skip.** Run the test and confirm it fails for the RIGHT reason.

### GREEN — Write Minimal Code

- Write the LEAST code to make the test pass
- No extra features, no "while I'm here"
- Copy-paste is fine at this stage
- Don't refactor yet — just make it green

### Verify GREEN — Watch It Pass

Run the test. If it doesn't pass, fix the implementation (not the test).

### REFACTOR — Clean Up

- Remove duplication
- Improve names
- Extract functions
- Run tests after EVERY change
- If tests break during refactor, you changed behavior — undo and try again

## Test Quality Checklist

- [ ] Tests run fast (< 1 second each)
- [ ] Tests are independent (no shared state)
- [ ] Tests are deterministic (same result every run)
- [ ] Test names describe behavior, not implementation
- [ ] Each test has one clear assertion
- [ ] Tests exercise edge cases (empty, null, boundary)

## When NOT to Write Tests First

Ask the user before skipping:
- Throwaway prototypes
- Generated code
- Configuration files

## Red Flags

- "Skip TDD just this once" — that's rationalization
- "The test is obvious" — obvious tests still catch regressions
- "I'll write tests after" — you won't, and if you do, they'll be weak
- "Mocking everything" — if you need 5 mocks, your design is wrong
