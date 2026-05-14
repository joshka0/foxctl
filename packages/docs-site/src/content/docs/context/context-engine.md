---
title: Context engine
description: Typed evidence, retrieval lanes, episodes, and feedback in foxctl's unified context engine.
---

Status: Current architecture guide for the implemented core.

The context engine is the typed retrieval substrate under newer context work. It
does not replace ContextWiki. ContextWiki is the workspace control and knowledge
plane; the context engine provides domain types, lane retrieval, evidence
packs, and durable episode records that ContextWiki and other callers can use.

## What it stores

The storage package defines a SQLite-backed store for nine entity families:

| Entity | Purpose |
|---|---|
| `context_events` | Append-only records of code, task, session, retrieval, and memory events |
| `evidence_packs` | Bundles returned by retrieval lanes |
| `evidence_nodes` | Individual pieces of evidence with typed refs |
| `memory_claims` | Durable claims and proposed memory facts |
| `impact_edges` | Forward and reverse impact relationships |
| `staleness_markers` | Evidence that may need refresh or invalidation |
| `projections` | Rebuilt views derived from events |
| `retrieval_episodes` | Append-only records of retrieval runs |
| `retrieval_feedback` | Append-only feedback about retrieval quality |

Large payloads can be stored in CAS instead of inline database rows.

## Domain model

Contextengine uses typed evidence refs instead of raw strings:

| Ref type | Example use |
|---|---|
| `path` | Source file or document path |
| `symbol` | Code symbol |
| `task` | Task or issue |
| `session` | Agent or CLI session |
| `memory_claim` | Durable memory claim |
| `note` | Vault or docs note |
| `artifact` | CAS artifact |
| `trajectory` | Captured run or training episode |
| `commit` | Git commit |
| `event` | Context event |
| `run` | Job or command run |
| `tool_call` | Tool-call evidence |

Evidence nodes also carry a node type and grounding value. Grounding separates
loaded, indexed, semantic, inferred, and validated evidence so retrieval can
show how strong the evidence is.

## Retrieval lanes

The context engine organizes retrieval into lanes:

| Lane | Role |
|---|---|
| `code` | Code search hits and snippets |
| `memory` | Memory claims and durable facts |
| `context` | Top-of-mind, handoffs, and ContextWiki packets |
| `task` | Task-local context and related tasks |
| `mixed` | Concurrent lane fan-out and typed ref fusion |

`RetrieveMixed` fans out to code, memory, context, and task lanes, then fuses
results by `EvidenceRef.Type` plus `EvidenceRef.Ref`. Partial lane failures are
recorded in metadata so callers can degrade instead of losing all evidence.

## Relationship to ContextWiki

ContextWiki owns workspace continuity and the knowledge-plane workflow:

- `.foxctl/runtime/top_of_mind.json`
- handoffs, observations, tensions, and proposals
- Obsidian vault search and bridge reconciliation
- retrieval inspection and correction proposals

The context engine provides the typed retrieval representation:

- `EvidencePack`
- `EvidenceNode`
- `RetrievalEpisode`
- `RetrievalFeedback`
- impact edges and stale markers

The clean boundary is:

```text
ContextWiki and callers decide what context is useful
  -> context engine records typed evidence and retrieval telemetry
  -> stores keep episodes, feedback, projections, and large payload CAS refs
```

## Design constraints

- Events, retrieval episodes, and feedback are append-only.
- Writes are serialized in-process to avoid local SQLite writer contention.
- Clocks are injected for deterministic tests.
- The storage layer imports domain types from `internal/context/contextengine`;
  the domain package stays pure.
- Impact graph traversal is explicit through forward and reverse edge queries.

## When to use it

Use contextengine when a feature needs:

- a typed evidence bundle instead of a loose prompt string
- retrieval telemetry and feedback
- cross-lane context fusion
- impact or staleness records
- repeatable retrieval evaluation

Use repoindex when the question is graph-shaped around code relationships. Use
semantic search when the first problem is meaning-based candidate discovery.

## Canonical sources

- [internal/context/contextengine](https://github.com/joshka0/foxctl/tree/main/internal/context/contextengine)
- [internal/storage/contextengine](https://github.com/joshka0/foxctl/tree/main/internal/storage/contextengine)
- [docs/architecture/context-architecture.md](https://github.com/joshka0/foxctl/blob/main/docs/architecture/context-architecture.md)
