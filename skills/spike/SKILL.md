---
name: spike
description: "Time-boxed exploratory investigation with clear deliverable."
version: 1.0.0
category: software-development
tags: [research, exploration, investigation, spike]
---

# Spike — Time-Boxed Exploration

## Overview

A spike is a time-boxed investigation to answer a specific question or reduce uncertainty before committing to implementation. The deliverable is knowledge, not code.

## When to Use

- Unfamiliar technology or API
- Multiple viable approaches and unclear tradeoffs
- Uncertain feasibility or performance characteristics
- Need to validate an assumption before planning

## Structure

### 1. Define the Question

State the specific question the spike will answer. Be precise.

**Good:** "Can pgx/v5 handle 10k concurrent connections with connection pooling?"
**Bad:** "Research database options"

### 2. Set the Time Box

- Small spike: 30 minutes
- Medium spike: 1-2 hours
- Large spike: half day

If you haven't answered the question by the time box, stop and report what you learned.

### 3. Investigate

- Build the smallest possible prototype that answers the question
- Don't write production code — this is throwaway
- Document findings as you go
- Test assumptions with real data/APIs when possible

### 4. Report Findings

Structure:
```
Question: [what we wanted to know]
Answer: [what we found]
Evidence: [how we know — benchmarks, prototypes, docs]
Recommendation: [what to do next]
Risks: [what could go wrong]
```

## Rules

1. **Time box is sacred** — stop when time's up, report partial findings
2. **No production code** — spikes produce knowledge, not features
3. **Throwaway prototypes only** — don't get attached to spike code
4. **Document everything** — the finding is the deliverable, not the code
5. **One question per spike** — if you have 3 questions, run 3 spikes
