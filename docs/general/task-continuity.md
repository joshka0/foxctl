# Task Continuity

Status: current first slice

This document describes the deterministic task continuity layer used by hooks,
Codex-style command callers, and Jido-backed agents.

## Purpose

Task continuity reconstitutes a bounded, hierarchical view of active work from:

- ContextWiki task packet state
- ContextWiki handoffs and durable notes
- linked sessions and timeline evidence
- touched files and recent git history
- repoindex anchors and DAG anchors

The output is designed to be deterministic first and model-assisted second.

## Commands

### Structured command

Use this for Codex, scripts, agents, and other programmatic callers:

```bash
foxctl context task-history-summary --workspace .
```

This returns a structured envelope with:

- `data.rendered`
- `data.summary`
- `data.artifact`
- `data.task_id`
- `data.task_title`

Use the full pack command when a caller needs the entire collected bundle:

```bash
foxctl context task-history --workspace .
```

That returns:

- `data.pack`
- `data.summary`
- `data.artifact`

### Repo-family transcript summary

Use this when you want a repo-family or lane-specific transcript-history view
rather than one active task:

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

This surface:

- summarizes persisted transcript-history records across the repo family
- can bias toward one coherent lane with `--focus-query`
- can bound selection by transcript/session date with `--date-from` / `--date-to`
- returns `support_metadata` so callers can inspect:
  - `owner_count`
  - `latest_updated_at`
  - `latest_age_days`
  - `source_owners`

Precondition:

- transcript history must have been persisted first through:
  - `foxctl sessions derive-memory --memory-lane insight --persist-history`
  - or `foxctl sessions derive-memory-group --memory-lane insight --persist-history`

## Hook Wrapper

Use the thin shell wrapper when a hook needs prompt-ready JSON:

- `configs/hooks/task-continuity-summary.sh`

This wrapper is smaller than the structured command output and already matches
the hook injection shape:

- `hookSpecificOutput.additionalContext`
- `hookSpecificOutput.metadata.task_continuity_artifact`

## Interface Split

Use the command when you need machine-readable continuity:

- Codex
- Jido/agent runtime integrations
- scripts and automation

Use the wrapper when you need hook-native prompt injection:

- Claude hook flows
- prompt submission hooks
- post-compaction context injection

## Artifact Contract

Large continuity packs are persisted to CAS and returned as an artifact digest.

This keeps prompt-sized surfaces small while preserving a stable pointer to the
full pack for later expansion.

## Current Integrations

### Hook path

Post-compaction/session-restore flows append a rendered task continuity summary
plus artifact digest.

### Jido path

Jido-backed agents get task continuity in three places:

1. initial spawn state
2. ask-signal metadata refresh
3. runtime-state inspection refresh

Runtime-state refresh depends on `workspace_root` being present in the Jido
agent state. When available, the inspection path recomputes `task_continuity`
instead of relying only on the original spawn-time snapshot.

## Family-history notes

`family-history-summary` is deterministic first and model-assisted second, the
same as task continuity:

- owner selection happens before summarization
- transcript-time windows apply before owner selection
- summary modes may be:
  - `deterministic`
  - `llm`
  - `llm_cleanup`

## Related Docs

- [docs/architecture/context-architecture.md](../architecture/context-architecture.md)
- [docs/general/hooks.md](hooks.md)
- [docs/general/agent-daemon.md](agent-daemon.md)
