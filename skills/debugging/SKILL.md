---
name: systematic-debugging
description: "4-phase root cause debugging: investigate before fixing."
version: 1.0.0
category: software-development
tags: [debugging, troubleshooting, root-cause, investigation]
---

# Systematic Debugging

## Overview

Random fixes waste time and create new bugs. Quick patches mask underlying issues.

**Core principle:** ALWAYS find root cause before attempting fixes.

## When to Use

Use for ANY technical issue: test failures, bugs, unexpected behavior, performance problems, build failures.

## The Four Phases

### Phase 1: Root Cause Investigation

**BEFORE attempting ANY fix:**

1. **Read error messages carefully** — don't skip past errors. They often contain the exact solution. Read stack traces completely.
2. **Reproduce consistently** — can you trigger it reliably? What are the exact steps?
3. **Check recent changes** — what changed that could cause this? Git diff, recent commits, new dependencies.
4. **Trace data flow** — where does the bad value originate? Keep tracing upstream until you find the source. Fix at the source, not the symptom.

**Completion checklist:**
- [ ] Error messages fully read and understood
- [ ] Issue reproduced consistently
- [ ] Recent changes reviewed
- [ ] Root cause hypothesis formed

**STOP:** Do not proceed to Phase 2 until you understand WHY it's happening.

### Phase 2: Pattern Analysis

1. **Find working examples** — locate similar working code in the same codebase.
2. **Compare against references** — read reference implementations completely.
3. **Identify differences** — what's different between working and broken? List every difference.

### Phase 3: Hypothesis and Testing

1. **Form a single hypothesis** — "I think X is the root cause because Y"
2. **Test minimally** — make the SMALLEST possible change. One variable at a time.
3. **Verify** — did it work? If not, form NEW hypothesis. Don't add more fixes on top.

### Phase 4: Implementation

1. **Create failing test case** — simplest possible reproduction, automated if possible.
2. **Implement single fix** — address the root cause. ONE change at a time. No "while I'm here" improvements.
3. **Verify fix** — run specific test, then full suite for regressions.
4. **Rule of Three** — if 3+ fixes failed, STOP and question the architecture.

## Red Flags — STOP and Return to Phase 1

- "Quick fix for now, investigate later"
- "Just try changing X and see if it works"
- "I don't fully understand but this might work"
- Proposing solutions before tracing data flow
- Each fix reveals a new problem in a different place

## Quick Reference

| Phase | Key Activities | Success Criteria |
|-------|---------------|------------------|
| 1. Root Cause | Read errors, reproduce, check changes, trace data | Understand WHAT and WHY |
| 2. Pattern | Find working examples, compare, identify differences | Know what's different |
| 3. Hypothesis | Form theory, test minimally, one variable at a time | Confirmed or new hypothesis |
| 4. Implementation | Create test, fix root cause, verify | Bug resolved, all tests pass |
