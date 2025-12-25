---
name: Pre-Implementation
description: Pre-implementation analysis checklist - understand before you build
argument-hint: "What you want to implement"
---

# Pre-Implementation Checklist

Complete before writing any code.

## Phase 1: Understand Context

1. **Find Related Code**: `fs/find`, `text/ripgrep` - list 3-5 relevant files
2. **Read Philosophy**: `code/symbols`, `code/complexity` - patterns, abstractions, anti-patterns
3. **Semantic Analysis**: LSP references, call hierarchy, workspace symbols

## Phase 2: Challenge Assumptions

- What problem does this actually solve?
- Is this the simplest solution? (list 2 rejected alternatives)
- What assumptions am I making?
- What could go wrong? (top 3 failure modes)
- What files will I modify/create? What's OUT of scope?

## Phase 3: Propose Solution

- 3-5 sentence approach (where logic lives, patterns to follow, reuse vs create)
- Numbered implementation steps
- Test strategy with edge cases

## Output Summary

```markdown
## Pre-Impl Summary: [task]
**Problem:** [one sentence]
**Approach:** [one sentence]
**Key files:** [list]
**Assumptions:** [bullets]
**Risk:** [highest]
Ready to implement: YES/NO
```

**STOP** if checklist incomplete - ask questions instead.

Full docs: `~/repos/personal/agentctl/configs/skills/pre-impl/Skill.md`
