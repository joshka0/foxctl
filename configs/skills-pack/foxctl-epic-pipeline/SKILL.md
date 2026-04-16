---
name: foxctl-epic-pipeline
description: "Run the foxctl room-agile epic creation pipeline from intake to dispatch readiness. Use when importing Factory missions or plan files into foxctl, shaping room epics, grading readiness, computing frontiers, and keeping epic creation separate from implementation."
---

# foxctl-epic-pipeline

Use this skill when turning an intake source into a canonical foxctl room-agile epic that can later drive implementation.

This skill is for `epic creation` and dispatch planning. Do not implement product code while using it.

## Mental model

All intake paths converge onto the same room-agile model:

```text
intake source -> canonical epic -> status -> snapshot -> scout -> grade -> frontier -> dispatch
```

The pipeline has two separate outputs:

- `grade`: whether the epic is good enough as an implementation plan
- `frontier`: which stories can run now, which are blocked, and why

An epic can be `READY 100/100` while still having a blocked execution frontier. That means the plan is complete and the blocker is a real implementation constraint, not a missing planning field.

## Intake adapters

### Factory import

Use Factory import when the source is `~/.factory/missions/<mission-id>`.

```bash
foxctl room epic import-factory <room-id> \
  --mission-dir ~/.factory/missions/<mission-id> \
  --sender <coordinator>
```

Factory import preserves source state and evidence:

- mission brief and working directory
- features as milestones/stories
- feature status as story state
- validation state as story validation
- progress log as delivery log
- handoff artifacts when present

Treat Factory import as source fidelity first. Do not rewrite blocked, paused, failed, or pending Factory work into successful implementation progress.

### Plan import

Use plan import when the source is a markdown or OpenCode todo plan.

```bash
foxctl room epic import-plan <room-id> \
  --plan-file docs/plans/<plan>.md \
  --provider auto \
  --sender <coordinator>
```

Plan import creates a planning draft:

- top-level sections become milestones when they contain actionable steps
- ordered list items or step headings become stories
- question-like ordered items ending in `?` become epic intake questions
- source metadata is preserved in epic metadata: kind, provider, path, title, content hash

Plan import does not imply source fidelity to runtime state. It usually needs shaping before dispatch.

## Standard pipeline

Run these in order after import or direct authoring:

```bash
foxctl room epic status <room-id> <epic-id>
foxctl room epic snapshot <room-id> <epic-id>
foxctl room epic scout <room-id> <epic-id>
foxctl room epic grade <room-id> <epic-id>
foxctl room epic frontier <room-id> <epic-id>
foxctl room epic dispatch-frontier <room-id> <epic-id>
```

Use `status` to choose the next stage:

- `intake`: answer questions and finalize the epic
- `repair`: fill planning gaps found by scout/grade
- `grade`: grade the completed planning artifact
- `dispatch`: use the frontier for implementation handoff

## Shaping rules

Only make planning changes:

- answer intake questions
- finalize the epic
- set outcome, horizon, scope, and success signals
- add milestone contracts and criteria
- assign story owners
- add story descriptions when import produced terse titles
- add planning validation records that define expected evidence
- add dependency metadata when order matters

Do not:

- move stories to implementation progress just to raise a grade
- mark implementation validations as passed unless implementation evidence exists
- clear real imported blockers
- dispatch work from a `NOT READY` epic unless the user explicitly wants a partial frontier experiment

## Repair checklist

Use `grade` and `scout` as the source of truth. Typical repairs are:

- `brief_boundary`: add outcome, horizon, scope, or success
- `milestone_contract`: add objective, validator lanes, evidence lanes, exit criteria
- `criteria_coverage`: add milestone criteria
- `owner_dispatch`: assign story owners
- `evidence_expectation`: add planning validation records or evidence expectations
- `dependency_topology`: fix unresolved, self, or cyclic dependencies

For imported Factory blockers, keep the blocker visible and make the plan ready around it. Example: a story can stay `blocked`, while the epic grades `READY` because the blocker is explicit and the frontier identifies the unblock candidate.

## Dispatch rules

Use `frontier` before any implementation handoff:

```bash
foxctl room epic frontier <room-id> <epic-id>
foxctl room epic dispatch-frontier <room-id> <epic-id>
```

Dispatch only ready frontier stories. If multiple stories are ready, choose an explicit story for targeted spawn:

```bash
foxctl room epic dispatch-frontier <room-id> <epic-id> \
  --spawn --story-id <story-id> \
  --llm-provider lmstudio
```

Use local providers, native subagents, or tmux agents for dispatch. Do not route through OpenRouter for this workflow.

## Completion target

Stop the creation pipeline when:

- `grade` reports `READY`
- `scout` has zero blocking findings
- `status` has no structural gaps
- `frontier` clearly separates ready stories from blocked stories
- any remaining blockers are real implementation blockers, not missing planning metadata

At that point, report the grade, frontier, blocked candidates, and snapshot path. Do not start implementation unless asked.
