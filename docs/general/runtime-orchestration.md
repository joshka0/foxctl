# Runtime Orchestration

Current map of how agent runs, ask flows, and overseer orchestration move through
the repo.

## Scope

| In scope | Out of scope |
|---------|---------------|
| Current execution and orchestration paths for CLI, daemon, web, v2 services, Jido bridge, and companion context assembly | Deep store schema details (see `storage.md`) |

## Current State

The runtime is currently hybrid:

1. The mailbox-driven agent runtime under `internal/agent` and
   `internal/agent/daemon` is still active for `agent run` and some CLI agent
   management surfaces.
2. The newer v2 stack under `internal/v2` owns the clearest event-sourced and
   Jido-backed paths, especially `ask`, orchestration scheduling/reconciliation,
   projections, context building, and companion integration points.
3. Jido is the execution bridge for v2 orchestration and optional companion
   context retrieval, not a replacement for every legacy agent path yet.

## Core Packages

| Package(s) | Responsibility |
|-----------|----------------|
| `internal/v2/core/*` | Typed v2 command, event, spawn, orchestration, and policy contracts |
| `internal/v2/services` | Command services and orchestration services (`ask`, `run`, `spawn`, `list`, `kill`, orchestration dispatch) |
| `internal/v2/runtime/runner` | Synchronous turn pipeline, event emission, turn persistence wiring |
| `internal/v2/runtime/orchestration` | Long-lived scheduler/reconcile component for board-driven orchestration |
| `internal/v2/runtime/contextbuilder`, `internal/v2/runtime/enrichers`, `internal/v2/runtime/supervisor` | Context assembly, async enrichment, and component lifecycle management |
| `internal/v2/adapters/jido` | Jido JSON-RPC client, child spawner, ask/runtime adapter, orchestration reconciler, companion provider |
| `internal/v2/adapters/libsql/*` | v2 events, projections, orchestration, idmap, and turn stores |
| `internal/companion` | Companion chat/memory service and adapter layer into v2 context building |
| `internal/agent`, `internal/agent/daemon` | Legacy mailbox-driven agent runtime, overseer hierarchy, tool wiring, and foreground daemon loop |
| `internal/daemon` | Local daemon service; currently mixes legacy agent runtime behavior with newer v2-backed command helpers |
| `internal/execution/agentmanager` | Legacy spawn/kill management path still used by some CLI flows |

## Surface Map

| Surface | Current path | Notes |
|--------|--------------|-------|
| `agent ask` | `cmd/agentctl/cmd/agent.go` -> `internal/v2/services.AskService` | Dispatcher can be mailbox or Jido-backed |
| `agent ask-status` | CLI -> v2 projections/events | Reads v2 run state and terminal callback metadata |
| Overseer orchestration component | `cmd/agentctl/cmd/overseer_v2_orchestration.go` -> `internal/v2/runtime/orchestration` + `internal/v2/adapters/jido` | Jido-backed dispatch/reconcile loop |
| Companion layered context | `internal/companion` -> `internal/v2/runtime/contextbuilder` -> optional Jido provider | Enabled via `AGENTCTL_COMPANION_CONTEXT_PROVIDER=jido` |
| `agent run` | CLI -> `internal/agent/daemon.Run` | Still mailbox-driven legacy agent runtime |
| `agent spawn` | CLI prefers daemon path, then falls back to legacy `agentmanager` | Not hard-cut to v2 everywhere |
| `agent list` | CLI -> local agents store | Not v2-service-only in current CLI |
| `agent kill` | Mixed; CLI still uses legacy/local management path in places | v2 kill service exists but is not the only live path |

## Jido-Backed Orchestration Flow

1. CLI or daemon wiring enables the orchestration component when
   `AGENTCTL_JIDO_ORCHESTRATION_PARENT_AGENT_IDS` is configured.
2. `cmd/agentctl/cmd/overseer_v2_orchestration.go` opens the v2 event store and
   orchestration projection store.
3. `internal/v2/adapters/jido.NewOrchestrationRuntime` creates:
   - a JSON-RPC client to the Jido runtime
   - a child spawner that converts v2 spawn requests into `runtime.spawn_child`
   - a reconciler that polls Jido child state and appends canonical v2 events
4. `internal/v2/runtime/orchestration.Scheduler` pulls candidate cards from the
   board read model and dispatches them through `internal/v2/services.OrchestrationService`.
5. The runtime component loops on a poll interval, dispatching new work and
   reconciling child lifecycle updates back into the v2 event/projection stores.
6. Web/UI surfaces read the board and runtime state through projection-backed API
   handlers under `internal/web/api`.

## Important Runtime Configuration

| Setting | Purpose |
|--------|---------|
| `AGENTCTL_JIDO_SOCKET` | Unix socket path for the Jido JSON-RPC bridge |
| `AGENTCTL_JIDO_RPC_PATH` | HTTP path used over the Jido socket |
| `AGENTCTL_JIDO_RPC_TIMEOUT_MS` | Jido RPC timeout |
| `AGENTCTL_JIDO_ORCHESTRATION_PARENT_AGENT_IDS` | Enables Jido orchestration runtime and defines parent agents to watch |
| `AGENTCTL_JIDO_ORCHESTRATION_DISPATCH_PARENT_AGENT_ID` | Parent agent ID used for new orchestration dispatches |
| `AGENTCTL_JIDO_SIGNAL_SOURCE` | Source tag for runtime signals |
| `AGENTCTL_COMPANION_CONTEXT_PROVIDER=jido` | Enables Jido-backed companion layered context fetches |

## Reading Order

1. `docs/architecture/system-architecture.md`
2. `docs/general/agent-daemon.md`
3. `docs/spec/agent_hierarchy.md`
4. `docs/spec/overseer_profile.md`
5. `docs/general/companion-memory.md`

## Related Docs

- `docs/architecture/system-architecture.md`
- `docs/architecture/jido-hybrid-runtime.md`
- `docs/general/agent-daemon.md`
- `docs/general/storage.md`
- `docs/spec/agent_hierarchy.md`
- `docs/spec/overseer_profile.md`
