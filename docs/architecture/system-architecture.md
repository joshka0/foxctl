# System Architecture (Canonical)

This is the canonical current-state architecture map for `agentctl`.

## Metadata

| Field | Value |
|------|-------|
| Status | Current |
| Canonical scope | Runtime/component architecture for `cmd/agentctl` + `internal/*` |
| Last reviewed | 2026-03-06 |

## Runtime Topology

```mermaid
flowchart TD
    CLI[cmd/agentctl]
    Web[internal/web + API handlers]
    Legacy[legacy agent runtime\ninternal/agent + internal/agent/daemon]
    V2[v2 services/runtime\ninternal/v2/services + internal/v2/runtime/*]
    Jido[Jido bridge\ninternal/v2/adapters/jido]
    Stores[storage + libsql projections/CAS]
    Context[companion + contextbuilder]
    Indexing[indexing/retrieval]
    Obs[observability + hooks]

    CLI --> Legacy
    CLI --> V2
    CLI --> Web
    Web --> V2
    Web --> Legacy
    Legacy --> Stores
    V2 --> Jido
    V2 --> Stores
    V2 --> Context
    Context --> Stores
    V2 --> Indexing
    Legacy --> Obs
    V2 --> Obs
    Jido --> Stores
```

## Current State Notes

1. The repo is not a single-runtime system yet. Legacy mailbox-driven agent
   execution and newer v2/Jido-backed services both exist.
2. The clearest v2 ownership today is ask/projection handling, orchestration
   scheduling/reconciliation, v2 event stores, context building, and companion
   integration points.
3. Some CLI agent management surfaces still route through legacy stores or
   `agentmanager` fallback paths.

## Core Package Groups

| Group | Key packages | Responsibility | Primary docs |
|------|--------------|----------------|--------------|
| Legacy agent runtime | `internal/agent`, `internal/agent/daemon`, `internal/execution/agentmanager` | Mailbox-driven sessions, overseer hierarchy, legacy spawn/list/run/kill paths still used by some CLI flows | `docs/general/agent-daemon.md`, `docs/spec/agent_hierarchy.md` |
| V2 command and orchestration stack | `internal/v2/core/*`, `internal/v2/services`, `internal/v2/runtime/{runner,orchestration,supervisor,tools,snapshots,profiles}` | Typed v2 commands, event-sourced orchestration, staged turn execution, long-lived components | `docs/general/runtime-orchestration.md`, `docs/spec/v2_symphony_kanban_orchestration.md` |
| Jido execution bridge | `internal/v2/adapters/jido` | JSON-RPC client, child spawn bridge, ask/runtime adapter, orchestration reconciliation, companion provider | `docs/general/runtime-orchestration.md` |
| Companion and context assembly | `internal/companion`, `internal/v2/runtime/contextbuilder`, `internal/v2/runtime/enrichers` | Conversation memory, layered context assembly, async derived artifacts, companion bridge integration | `docs/general/companion-memory.md`, `docs/general/context-and-observability.md` |
| State and persistence | `internal/storage/*`, `internal/v2/adapters/libsql/*` | Durable stores, CAS, mailbox/task/session persistence, v2 events and projections | `docs/general/storage.md`, `docs/architecture/postgres-storage.md` |
| Retrieval and indexing | `internal/indexing/*`, `internal/retrieval`, `internal/codecontext`, `internal/codemap` | Semantic/symbol/repo indexing and context extraction | `docs/general/search.md`, `docs/general/repoindex.md` |
| Interface layers | `internal/web`, `internal/chatadapter`, `internal/openapi`, `internal/providers` | API/server surfaces and external platform integrations | `docs/general/api-server.md`, `docs/architecture/chat-platform-adapter.md` |
| Observability and hooks | `internal/observability`, `internal/hooks`, `internal/context/updater` | Trace/event propagation, hook execution, proactive context surfacing | `docs/general/context-and-observability.md`, `docs/general/hooks.md` |
| Foundations | `internal/domain`, `internal/platform`, `internal/protocol`, `internal/tools`, `internal/tooling` | Core types, config/platform utilities, protocol helpers | `docs/general/architecture.md`, `docs/spec/README.md` |

## Architectural Invariants

| Invariant | Why it matters |
|----------|----------------|
| Envelope contract stability (`meta.*` and shape) | Prevents breakage across hooks/UI/golden tests |
| Append-first v2 events plus projection reads | Keeps orchestration and ask state replayable |
| Jido bridge remains adapter-scoped | Prevents runtime transport concerns from leaking into v2 core contracts |
| Workspace-constrained IO | Prevents path escapes and unsafe file access |
| CAS-backed large outputs | Avoids oversized envelopes and preserves replayability |
| Context propagation (`context.Context`) | Enables cancellation/timeouts across both legacy and v2 stacks |

## Related Architecture Docs

- `docs/general/runtime-orchestration.md`
- `docs/architecture/jido-hybrid-runtime.md`
- `docs/general/agent-daemon.md`
- `docs/architecture/chat-platform-adapter.md`
- `docs/architecture/kubernetes-runtime.md`
- `docs/architecture/postgres-storage.md`
