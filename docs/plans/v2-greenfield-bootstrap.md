# Implementation Plan: V2 Greenfield Bootstrap (Active)

Status: Active (Hybrid runtime; v2-first direction)
Source Spec: `docs/spec/v2_greenfield_bootstrap.md`
Working Tracker: `docs/plans/v2-implementation-todo.md`

## History Archive

Historical migration detail was moved to:

- `docs/plans/archive/v2-greenfield-bootstrap-archive-2026-03-02.md`
- `docs/plans/archive/v2-implementation-todo-archive-2026-03-02.md`

This active document contains only current v2 direction and execution guidance.

## Companion and General References

- `docs/spec/v2_repo_rules_and_skills.md`
- `docs/general/runtime-orchestration.md`
- `docs/general/agent-daemon.md`
- `docs/general/companion-memory.md`
- `docs/general/context-and-observability.md`
- `docs/designs/hierarchical-memory-retrieval.md`
- `docs/designs/progressive-memory-system.md`
- `docs/observability/wide-events.md`

## Problem Statement

v1 had duplicated orchestration paths, split tool systems, and weak context durability.
v2 must preserve immutable turn evidence while making context assembly dynamic, non-blocking,
and retrieval-first.

## Architecture Decision

Use a direct v2 architecture for supported command surfaces:

- CLI/API/daemon handlers call v2 services directly.
- Services own validation/orchestration.
- Core contracts stay adapter-agnostic.
- Background/enrichment workloads run as `Run(ctx)` components with bounded queues.

## V2 Scope Guardrails

1. New or actively migrated command surfaces should route through v2 services and projections.
2. Legacy routing still exists for some agent command paths; remove it deliberately rather than assuming it is gone.
3. Turn completion must not block on enrichers, context indexing, or maintenance workers.
4. Retrieval/context outputs are deterministic (stable refs, stable ordering, bounded limits).

## Design Patterns

| Pattern | Location | Rationale |
|---------|----------|-----------|
| Command | `internal/v2/services/{spawn,ask,run,list,kill}_service.go` | One orchestration path per command |
| Pipeline | `internal/v2/runtime/runner/*` | Deterministic staged execution |
| Repository | `internal/v2/core/events` + `internal/v2/adapters/libsql/*` | Append-only event source + projection reads |
| Supervisor | `internal/v2/runtime/supervisor/*` | Predictable lifecycle for background components |
| Single Writer | runtime state loops | Race reduction and explicit ownership |
| Snapshot Cache | `internal/v2/runtime/snapshots/*` | Low-contention hot-path reads |

## Non-Blocking Contract

1. Turn persistence is the critical path.
2. Enrichment jobs consume `turn.recorded` asynchronously.
3. Missing/degraded artifacts reduce context quality only; they do not fail completed turns.
4. Queue overflow/backpressure behavior is explicit and observable.

## Turn and Artifact Model

Hierarchy:

1. `Turn`
2. `Iteration`
3. `ToolCall`
4. Derived artifacts (`embedding`, `annotation`, `classification`, `learning`, `narrative`, `episode`)

Reference forms:

1. `turn/{turn_id}`
2. `turn/{turn_id}/iter/{index}`
3. `turn/{turn_id}/iter/{index}/tool/{call_id}`
4. `turn/{turn_id}#msg:{msg_id}:{start}-{end}`

## Wave 2: Productionization + Dynamic Context (Completed)

Outcomes:

1. Major command and orchestration surfaces moved to direct v2 handlers.
2. Libsql turn/artifact persistence landed.
3. Event-to-enricher wiring is non-blocking and bounded.
4. Temporal/hierarchical context assembly (`hours -> days -> weeks -> months`) landed.
5. Companion layered assembly (`L2 -> L1 -> L0`) landed.

## Wave 3: Retrieval + Dynamic Context Intelligence (Completed)

Outcomes:

1. Artifact semantic retrieval (`SearchArtifactsByEmbedding`) shipped with deterministic ranking.
2. Native vector path + deterministic fallback are both covered.
3. Context builder integrates temporal + semantic + companion layers.
4. Source resynthesis (`sessions resynthesize-v2`) writes canonical lineage and artifacts.

## Wave 4: Identity-Guided Retrieval + Narrative Views (Completed)

Outcomes:

1. Episode layer + compiler enricher shipped (`v2_episodes`, landmark metadata).
2. `WorkingContext` gating shipped (hard filters + deterministic soft rerank + fallback ladder).
3. Narrative artifact shipped with evidence constraints and staleness metadata.

Locked rules:

1. Immutable source turns.
2. Mutable derived views only.
3. Evidence-cited claims for narrative/identity views.
4. Async compilers only (no turn-path blocking).
5. Live and resynthesis schema parity.

## Wave 5: Active Planning

Priority areas:

1. Full v2 quality-gate loop on command surfaces + context outputs.
2. GUI-agent integration parity for context refs/events/trace metadata.
3. Retrieval quality policy hardening (overlap, latency, drift alerts).
4. Remaining legacy cleanup in docs/tests/interfaces where no runtime usage remains.

## Full v2 Exit Criteria

1. `spawn`, `ask`, `run`, `list`, `kill` are direct v2 on CLI/API/daemon.
2. Turn lineage and derived artifacts are durable and queryable via stable refs.
3. Enrichment and context pipelines remain non-blocking under load.
4. Retrieval quality and observability gates are enforced in CI and runtime telemetry.
5. Historical migration surfaces remain archive-only, not active runtime dependencies.
