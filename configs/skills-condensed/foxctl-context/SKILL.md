---
name: foxctl-context
description: ContextWiki control plane, transcript-family history, Obsidian context architecture, and refactor scout/advisor quick entrypoint.
---

# AgentCTL Context

Use this when you are working on ContextWiki, transcript-history continuity, or context architecture in `foxctl`.

## Core ContextWiki

```bash
foxctl orient
foxctl context show
foxctl context report
foxctl context hooks install
```

## Task continuity

```bash
foxctl context task-history --workspace .
foxctl context task-history-summary --workspace .
```

## Repo-family transcript history

Persist history first:

```bash
foxctl sessions derive-memory --memory-lane insight --persist-history --source-file <session.jsonl>
foxctl sessions derive-memory-group --memory-lane insight --persist-history --source-file <a.jsonl> --source-file <b.jsonl>
```

Then summarize:

```bash
foxctl context family-history-summary --workspace .
foxctl context family-history-summary --workspace . --focus-query "recursive memory second-pass consolidation"
foxctl context family-history-summary --workspace . --date-from 2026-03-25 --date-to 2026-03-25
```

Useful outputs:

- `current_focus`
- `top_learnings`
- `recurring_learnings`
- `top_risks`
- `recurring_mistakes`
- `support_metadata`

## Obsidian / ContextWiki knowledge layer

```bash
foxctl obsidian index build --vault-path <vault>
foxctl obsidian graph build --workspace . --vault-path <vault>
foxctl obsidian graph promote --workspace . --vault-path <vault>
foxctl obsidian bridge reconcile --workspace . --vault-path <vault>
```

## Refactor layer

```bash
foxctl refactor scout --path ./internal --language go
foxctl refactor advisor --path ./internal --language go
```

Use `refactor scout` first. Use `advisor` only after the deterministic scout has narrowed the hotspot set.
