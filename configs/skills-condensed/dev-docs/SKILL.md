---
name: Dev Docs
description: Create a comprehensive strategic plan with structured task breakdown
argument-hint: "Describe what you need planned"
---

# Strategic Planning

Create actionable plans with task breakdowns.

## Output Structure

1. Executive Summary
2. Current/Future State Analysis
3. Implementation Phases with Tasks
4. Risk Assessment & Mitigation
5. Success Metrics & Dependencies

## Task Breakdown

- Sections = phases/components
- Numbered, prioritized tasks with acceptance criteria
- Dependencies between tasks
- Effort levels: S/M/L/XL

## File Generation

Create in `dev/active/[task-name]/`:
- `[task-name]-plan.md` - Full plan
- `[task-name]-context.md` - Key files, decisions
- `[task-name]-tasks.md` - Checklist for tracking

Use AFTER plan mode when vision is clear.

Full docs: `~/repos/personal/agentctl/configs/skills/dev-docs/Skill.md`
