---
title: ContextWiki
description: How foxctl organizes repo context, memory, Obsidian knowledge, and retrieval evidence.
---

Status: Current architecture guide.

ContextWiki is foxctl's living workspace knowledge layer. It connects repo docs,
code graph evidence, memory, handoffs, observations, reviewed notes, and
retrieval policy so agents can recover current context without treating every
note as an instruction.

Use **context system** when describing the implementation that powers it.

The short version:

```text
ContextWiki control plane
  -> top-of-mind, handoffs, observations, tensions, proposals

Knowledge plane
  -> Obsidian notes, vault index, bridge metadata, reviewed promotions

Retrieval plane
  -> repoindex hints, semantic note search, context engine evidence packs
```

## What ContextWiki Coordinates

| Layer | Purpose |
|---|---|
| Repo evidence | Code, docs, symbols, graph edges, and snippets |
| Memory | Reusable facts, decisions, and task continuity summaries |
| Obsidian bridge | External knowledge graph and vault reconciliation |
| Retrieval policy | Evidence ranking, provenance, and safety boundaries |
| Agent surface | Context bundles consumed by skills and agents |

## Current command surface

| Need | Command family |
|---|---|
| Current workspace orientation | `foxctl orient`, `foxctl context show`, `foxctl context report` |
| Capture continuity | `foxctl capture`, `foxctl context task-history-summary` |
| Retrieve blended context | `foxctl context retrieve` |
| Inspect retrieval misses | `foxctl context retrieve-inspect`, `foxctl context retrieve-inspect-suite` |
| Review durable proposals | `foxctl context proposals`, `foxctl context proposal merge` |
| Refresh vault knowledge | `foxctl obsidian index build`, `foxctl obsidian graph build` |
| Reconcile docs and vault notes | `foxctl obsidian bridge reconcile`, `bridge apply`, `bridge tidy` |

## Typical refresh flow

After repo docs, repo graph, or bridge metadata changes:

```bash
foxctl obsidian graph build --workspace . --vault-path "/path/to/vault"
```

```bash
foxctl obsidian graph promote --workspace . --vault-path "/path/to/vault"
```

```bash
foxctl obsidian bridge reconcile --workspace . --vault-path "/path/to/vault"
```

```bash
foxctl obsidian index build --vault-path "/path/to/vault"
```

## Relationship to the context engine

[Context engine](/context/context-engine/) is the typed evidence substrate. ContextWiki
decides which workspace state and knowledge should be retrieved; the context engine
can record evidence packs, retrieval episodes, feedback, impact edges, and
staleness markers.

Use ContextWiki when the question is "what does this workspace know right now?" Use
the context engine when the feature needs typed evidence, lane fusion, or retrieval
feedback records.

## Production boundaries

- Evidence is not instruction unless a policy explicitly promotes it.
- Generated summaries need provenance.
- Retrieval should prefer typed signals over ad hoc keyword routing.
- Planned memory-promotion behavior must stay labeled planned until promoted to
  current docs.
- Vault bridge operations draft and patch metadata deliberately; they do not
  silently rewrite canonical prose.

## Term Choice

Use the names this way:

| Term | Use it for |
|---|---|
| ContextWiki | The user-facing workspace knowledge layer |
| Context system | The implementation behind ContextWiki |
| ContextWiki control plane | Current work state: top-of-mind, handoffs, observations, tensions, proposals |
| Knowledge plane | Durable docs and vault notes |
| Retrieval plane | Repoindex hints, semantic note search, and context engine evidence |

## Canonical sources

- [docs/architecture/context-architecture.md](https://github.com/joshka0/foxctl/blob/main/docs/architecture/context-architecture.md)
- [docs/architecture/memory-core.md](https://github.com/joshka0/foxctl/blob/main/docs/architecture/memory-core.md)
- [docs/general/context-and-observability.md](https://github.com/joshka0/foxctl/blob/main/docs/general/context-and-observability.md)
- [docs/general/companion-memory.md](https://github.com/joshka0/foxctl/blob/main/docs/general/companion-memory.md)
