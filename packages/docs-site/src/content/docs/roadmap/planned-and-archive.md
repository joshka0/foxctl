---
title: Planned and archive docs
description: How foxctl manages active plans, experimental features, and archived historical material.
---

foxctl organizes documentation by lifecycle class. Planned work is useful for agents and contributors, but must not read as current operator guidance. Archived material is preserved for provenance but is not canonical behavior documentation.

## Documentation lifecycle

| Class | Location | Meaning | Maintenance |
|---|---|---|---|
| **Current** | `docs/general/`, `docs/architecture/`, `docs/spec/` | Represents current behavior and contracts | Must be kept accurate with code changes |
| **Active plan** | `docs/plans/` | Forward-looking implementation work | Update as plans evolve; archive when complete or superseded |
| **Legacy plan** | `docs/impl_plan/` | Historical phased plans | Keep for provenance; do not use as canonical guidance |
| **Historical** | `docs/archive/` | Superseded or legacy material | Read-only except for link hygiene and provenance notes |
| **Generated** | `docs/codemaps/` | Generated analysis artifacts | Regenerate when needed; not canonical |
| **Working notes** | `docs/notes/`, `docs/research/`, `docs/designs/` | Exploratory or non-final material | Promote stable decisions into current docs |

### Rules

1. New canonical behavior docs go in `docs/general/`, `docs/architecture/`, or `docs/spec/`.
2. New planning docs go in `docs/plans/` (not `docs/impl_plan/`).
3. Completed or superseded plans and designs move to `docs/archive/`.
4. Historical docs may keep legacy content, but links must remain navigable.
5. When behavior changes, update the matching canonical doc in the same PR.
6. Markdown links in repo docs must pass `make check-doc-links`.

## Active plans

Active planning documents live in `docs/plans/`. These represent work in progress:

| Plan | Description |
|---|---|
| `features/internal-package-topology-migration.md` | Incremental `internal/*` family cleanup; makes legacy vs `v2` boundary explicit |
| `features/eino-go-native-runtime-plan.md` | Eino `AgentEngine` integration + Go-native `RuntimeSpawner`/reconciler |
| `features/opentui-agent-terminal-facades.md` | Facade backlog for a greenfield OpenTUI agent terminal |
| `features/remote-workbench-session-handoff.md` | Plan for moving TUI workbench to Tailscale remote |
| `features/semantic-code-anchors.md` | Typed code comments as repo graph edges and ACA retrieval signals |
| `features/official-docs-production-release.md` | Starlight docs-site plan and production release matrix |
| `features/durable-execution-recovery.md` | v2 orchestration crash recovery using card projection as durable retry queue |
| `features/durable-execution-layer1-side-effects.md` | Runner side-effect safety: Turn Request Registry, idempotent event append, effect journal |
| `features/ladybugdb-graph-projection-spike.md` | Gated spike for LadybugDB as disposable repo graph projection |
| `features/slop-function-detection.md` | Code quality detection for AI-generated sprawl |
| `features/refactor-intelligence-substrate.md` | Refactor intelligence and deterministic detection backlog |
| `features/agent-mux-room-hierarchy.md` | Room hierarchy for mux-based agent coordination |
| `features/foxctl-evolve-plan.md` | Foxctl-native repo-evolution tool with DB-backed experiment state |
| `features/longcot-eval-contract-plan.md` | LongCoT eval contract for measuring RLM scaffold and token efficiency |
| `features/aca-self-evolving-memory-layer.md` | Self-evolving memory layer for ACA |

### Using plans

- Treat plan documents as current planning references, not canonical as-built docs.
- For current runtime behavior, prefer `docs/architecture/*` and `docs/general/*`.
- When a plan is completed or superseded, move it to `docs/archive/`.

## Experimental

Experimental features can be tried but are not the default production path:

| Feature | Status |
|---|---|
| RLM runtime experiments | Research phase |
| Graph projection spikes | Gated by plan documents |
| Refactor-intelligence workflows | Depend on plan-backed docs |
| Semantic anchor behavior (pre-promotion) | Evidence signal only, not operator workflow |

Experimental features should be labeled clearly in any documentation or agent-facing surfaces.

## Archive structure

Archived documentation lives in `docs/archive/`:

| Directory | Contents | Why archived |
|---|---|---|
| `impl_plan/` | Completed implementation plans (phases 1–4, vector alignment) | Phases completed; vector alignment done |
| `designs/` | Implemented design documents (jsonv2, session lineage) | Designs implemented |
| `migrations/` | Completed migration docs (v2 migration, architecture specs) | Migrations completed; specs superseded |
| `legacy/` | Superseded root-level docs (search-guide, vector-search, embeddings) | Replaced by current docs in `docs/general/` |
| `specs/` | Implemented specs (phase 7 trajectory, coderabbit followups) | Specs implemented or superseded |

### Archive maintenance policy

- Archive docs are historical references, not canonical behavior docs.
- Content may remain stale by design, but local links should stay navigable.
- If a linked doc is moved, update archive links to the new location or to a compatibility shim.
- No content refresh requirement; link hygiene only.

### Active documentation

For current documentation, see:
- [Architecture](https://github.com/joshka0/foxctl/blob/main/docs/general/architecture.md)
- [Skills](https://github.com/joshka0/foxctl/blob/main/docs/general/skills.md)
- [Hooks](https://github.com/joshka0/foxctl/blob/main/docs/general/hooks.md)
- [Memory](https://github.com/joshka0/foxctl/blob/main/docs/general/memory.md)
- [Sessions](https://github.com/joshka0/foxctl/blob/main/docs/general/sessions.md)
- [Storage](https://github.com/joshka0/foxctl/blob/main/docs/general/storage.md)
- [Gotchas](https://github.com/joshka0/foxctl/blob/main/docs/general/gotchas.md)

## Codemaps

Generated codemap artifacts live in `docs/codemaps/`. These are produced by the `codemap/generate` skill and contain:

- ASCII trees of code structure
- Mermaid diagrams of call relationships
- Annotated code paths
- File references

Codemaps are not canonical documentation. Regenerate them when needed using:

```bash
foxctl run codemap/generate --input '{"query": "trace auth flow"}'
```
