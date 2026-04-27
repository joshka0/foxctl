---
name: foxctl-context
description: "Foxctl context workflows for ACA, transcript history, context architecture, Obsidian knowledge, and refactor scouting."
---

## What I do

- Bundle the current context-architecture surfaces into one pack.
- Cover the control plane (`context`, task continuity, family history).
- Cover transcript-history ingest and lane summarization.
- Cover ACA/Obsidian knowledge-layer workflows.
- Cover the local refactor scout/advisor layer when structural cleanup is part of the same context work.

## When to use me

- You are working on ACA or context architecture in `foxctl`.
- You need to restore or summarize transcript-derived continuity.
- You want a repo-family or lane-specific transcript-history summary.
- You are wiring or checking Obsidian/ACA bridge surfaces.
- You want refactor-entrypoint discovery that stays close to the current context/memory work.

## Core Commands

### ACA / Context control plane

```bash
foxctl orient
foxctl context show
foxctl context report
foxctl context hooks install
```

### Task continuity

```bash
foxctl context task-history --workspace .
foxctl context task-history-summary --workspace .
```

Use `task-history-summary` for one active task.

### Repo-family transcript history

```bash
foxctl context family-history-summary --workspace .
```

Useful filters:

```bash
foxctl context family-history-summary --workspace . \
  --focus-query "recursive memory second-pass consolidation"

foxctl context family-history-summary --workspace . \
  --date-from 2026-03-25 \
  --date-to 2026-03-25
```

Important:

- `family-history-summary` only works after transcript history has been persisted.
- Persist transcript history first with the `insight` lane:

```bash
foxctl sessions derive-memory --memory-lane insight --persist-history --source-file <session.jsonl>
foxctl sessions derive-memory-group --memory-lane insight --persist-history --source-file <a.jsonl> --source-file <b.jsonl>
```

### Transcript-memory / history workflow

Use this when the goal is decision-useful history, not doctrine promotion:

```bash
foxctl sessions derive-memory --memory-lane insight --source-file <session.jsonl>
foxctl sessions derive-memory-group --memory-lane insight --source-file <a.jsonl> --source-file <b.jsonl>
```

What the history layer now surfaces:

- `insight_brief`
- `insight_timeline`
- `notable_insights`
- `history_answers`
- `history_pack`
- `history_records`

Family summaries add:

- `current_focus`
- `recent_changes`
- `top_learnings`
- `recurring_learnings`
- `top_risks`
- `top_surprises`
- `next_work`
- `recurring_mistakes`
- `support_metadata`

### Obsidian / ACA knowledge layer

```bash
foxctl obsidian index build --vault-path <vault>
foxctl obsidian graph build --workspace . --vault-path <vault>
foxctl obsidian graph promote --workspace . --vault-path <vault>
foxctl obsidian bridge reconcile --workspace . --vault-path <vault>
```

Use these when repo docs, ACA notes, or bridge metadata changed.

### Refactor layer

```bash
foxctl refactor scout --path ./internal --language go
foxctl refactor advisor --path ./internal --language go
```

Use `refactor scout` first for deterministic hotspot discovery.
Use `refactor advisor` when you want a second-stage shortlist over the scout output.

## Suggested Workflows

### 1) Restore one task

```bash
foxctl context task-history-summary --workspace .
```

### 2) Summarize one transcript lane

```bash
foxctl context family-history-summary --workspace . \
  --focus-query "recursive memory second-pass consolidation transcript pipeline"
```

### 3) Bound the lane by date

```bash
foxctl context family-history-summary --workspace . \
  --focus-query "recursive memory second-pass consolidation transcript pipeline" \
  --date-from 2026-03-25 \
  --date-to 2026-03-25
```

### 4) Move from transcript history into durable ACA notes

1. Persist transcript history with `derive-memory --memory-lane insight --persist-history`.
2. Read task continuity or family history.
3. Capture/promo through ACA:

```bash
foxctl capture --workspace . --task-id <id> --summary "..."
foxctl context promote --workspace . --path <handoff-or-observation>
```

### 5) Run refactor discovery inside the same lane

```bash
foxctl refactor scout --path ./internal --language go
```

Then keep the durable seam decision in ACA or transcript history instead of only in ad-hoc chat.

## Current Read

This pack is strongest for:

- ACA and context-architecture implementation
- transcript-history continuity work
- repo-family/lane summaries
- Obsidian bridge and graph workflows
- structural cleanup around these same surfaces

This pack is weaker for:

- unrelated app/product implementation work
- generic repo summaries when transcript history has not been persisted
- final refactor decisions without reading code after the scout
