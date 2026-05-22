---
name: code-review
description: "Pre-commit review: security, quality, and spec compliance."
version: 1.0.0
category: software-development
tags: [code-review, security, quality, verification]
---

# Code Review

## Overview

Automated verification before code lands. Check security, quality, and spec compliance.

**Core principle:** No one should verify their own work. Fresh eyes find what you miss.

## Review Dimensions

### 1. Security Scan

- [ ] No hardcoded secrets, API keys, or passwords
- [ ] No SQL injection (parameterized queries only)
- [ ] No command injection (no user input in shell commands)
- [ ] No XSS (output encoding in templates)
- [ ] No path traversal (validate file paths)
- [ ] Authentication/authorization checks present
- [ ] Sensitive data not logged

### 2. Spec Compliance

- [ ] All requirements from spec implemented
- [ ] File paths match spec
- [ ] Function signatures match spec
- [ ] Behavior matches expected
- [ ] Nothing extra added (no scope creep)

### 3. Code Quality

- [ ] Follows project conventions and style
- [ ] Proper error handling (no swallowed errors)
- [ ] Clear variable/function names
- [ ] No dead code or commented-out blocks
- [ ] No premature abstractions
- [ ] Functions are focused (single responsibility)

### 4. Test Coverage

- [ ] New functionality has tests
- [ ] Edge cases covered (empty, null, boundary values)
- [ ] Error paths tested
- [ ] Tests are deterministic and fast
- [ ] No flaky tests introduced

### 5. Performance

- [ ] No N+1 queries
- [ ] No unnecessary allocations in hot paths
- [ ] Appropriate use of caching
- [ ] No blocking calls in async contexts

## Output Format

```
- Critical Issues: [must fix before merge]
- Important Issues: [should fix]
- Minor Issues: [optional improvements]
- Verdict: APPROVED or REQUEST_CHANGES
```

## Rules

1. **Be specific** — "line 42: `err` is not checked" not "add error handling"
2. **Explain why** — "This SQL is injectable because..." not just "fix SQL"
3. **Suggest fixes** — provide the corrected code, not just the problem
4. **Prioritize** — critical > important > minor. Don't bury critical issues in nitpicks.
