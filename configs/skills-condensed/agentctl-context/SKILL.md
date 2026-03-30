---
name: agentctl-context
description: ACA control plane, transcript-family history, Obsidian context architecture, and refactor scout/advisor quick entrypoint.
---

# AgentCTL Context

Use this when you are working on ACA, transcript-history continuity, or context architecture in `agentctl`.

## Core ACA

```bash
agentctl orient
agentctl context show
agentctl context report
agentctl context hooks install
```

## Task continuity

```bash
agentctl context task-history --workspace .
agentctl context task-history-summary --workspace .
```

## Repo-family transcript history

Persist history first:

```bash
agentctl sessions derive-memory --memory-lane insight --persist-history --source-file <session.jsonl>
agentctl sessions derive-memory-group --memory-lane insight --persist-history --source-file <a.jsonl> --source-file <b.jsonl>
```

Then summarize:

```bash
agentctl context family-history-summary --workspace .
agentctl context family-history-summary --workspace . --focus-query "recursive memory second-pass consolidation"
agentctl context family-history-summary --workspace . --date-from 2026-03-25 --date-to 2026-03-25
```

Useful outputs:

- `current_focus`
- `top_learnings`
- `recurring_learnings`
- `top_risks`
- `recurring_mistakes`
- `support_metadata`

## Obsidian / ACA knowledge layer

```bash
agentctl obsidian index build --vault-path <vault>
agentctl obsidian graph build --workspace . --vault-path <vault>
agentctl obsidian graph promote --workspace . --vault-path <vault>
agentctl obsidian bridge reconcile --workspace . --vault-path <vault>
```

## Refactor layer

```bash
agentctl refactor scout --path ./internal --language go
agentctl refactor advisor --path ./internal --language go
```

Use `refactor scout` first. Use `advisor` only after the deterministic scout has narrowed the hotspot set.
