# Runtime Orchestration

Current map of how agent runs, ask flows, orchestration, and runtime inspection move
through the repo, plus the dependency order for getting to a Go-owned runtime.

## Scope

| In scope | Out of scope |
|---------|---------------|
| Current execution and orchestration paths for CLI, daemon, web, v2 services, Jido bridge, and runtime ownership migration | Deep store schema details (see `storage.md`) |

## Current state

The runtime is still hybrid:

1. The mailbox-driven agent runtime under `internal/agent` and
   `internal/agent/daemon` remains active for `agent run` and several classic agent
   management surfaces.
2. The newer v2 stack under `internal/v2` owns the clearest event-sourced command and
   orchestration surfaces.
3. Jido is still part of the live runtime story for some orchestration dispatch,
   reconcile, and runtime-tree inspection paths.

That means Jido is not yet purely optional in practice even though that is the intended
direction.

## Core packages

| Package(s) | Responsibility |
|-----------|----------------|
| `internal/v2/core/*` | Typed v2 command, event, spawn, orchestration, and policy contracts |
| `internal/v2/services` | Command services and orchestration services such as `ask`, `run`, `spawn`, `list`, `kill`, and orchestration dispatch |
| `internal/v2/runtime/runner` | Synchronous turn pipeline, event emission, and turn persistence wiring |
| `internal/v2/runtime/orchestration` | Long-lived scheduler/reconcile component for board-driven orchestration |
| `internal/v2/runtime/contextbuilder`, `internal/v2/runtime/enrichers`, `internal/v2/runtime/supervisor` | Context assembly, async enrichment, and component lifecycle management |
| `internal/v2/adapters/jido` | Jido JSON-RPC client, child spawner, ask/runtime adapter, orchestration reconciler, and optional companion provider |
| `internal/v2/adapters/libsql/*` | v2 events, projections, orchestration, idmap, and turn stores |
| `internal/companion` | Companion chat/memory service and adapter layer into v2 context building |
| `internal/agent`, `internal/agent/daemon` | Classic mailbox-driven agent runtime, overseer hierarchy, tool wiring, and foreground daemon loop |
| `internal/daemon` | Local daemon service; currently mixes classic runtime behavior with newer v2-backed command helpers |
| `internal/execution/agentmanager` | Legacy spawn/kill management path still used by some CLI flows |

## Surface map

| Surface | Current path | Notes |
|--------|--------------|-------|
| `agent ask` | `cmd/agentctl/cmd/agent.go` -> `internal/v2/services.AskService` | Default can be mailbox-backed; Jido remains an optional dispatcher path |
| `agent ask-status` | CLI -> v2 projections/events | Reads v2 run state and terminal callback metadata |
| Overseer orchestration component | `cmd/agentctl/cmd/overseer_v2_orchestration.go` -> `internal/v2/runtime/orchestration` + runtime adapter | Still effectively Jido-oriented in important flows today |
| Companion layered context | `internal/companion` -> `internal/v2/runtime/contextbuilder` -> optional provider | Jido-backed provider is optional, not the desired default |
| `agent run` | CLI -> `internal/agent/daemon.Run` | Still classic mailbox-driven runtime |
| `agent spawn` | CLI prefers daemon path, then falls back to legacy `agentmanager` | Not hard-cut to v2 everywhere |
| `agent list` | CLI -> local agents store | Not v2-service-only in current CLI |
| `agent kill` | Mixed; CLI still uses legacy/local management path in places | v2 kill service exists but is not the only live path |
| Web/API runtime tree views | `internal/web/api` plus optional Jido client for some runtime state | This is one of the main parity gaps for making Jido optional |

## Runtime ownership seams that matter now

These are the concrete seams that determine whether Jido is optional in fact or only in
principle.

| Concern | Current shape | Target shape |
|--------|---------------|--------------|
| Child spawn | v2 service already abstracts `RuntimeSpawner` | Default Go spawner backed by Go-owned worker state |
| Child lifecycle | Jido-oriented runtime state and reconcile for some paths | Go-owned registry, heartbeat, exit state, and reconciler |
| Runtime tree inspection | Some API handlers still query Jido runtime state directly | Trees derived from Go registry + projections |
| Control-plane truth | Split across projections and external runtime inspection | Go-owned state as the canonical source |
| Engine/backend flexibility | Constrained by runtime ownership not being fully local | Runtime contracts first, engine/backend pluggability second |

## Current Jido-backed orchestration flow

This is the important current path to understand before changing it:

1. CLI or daemon wiring enables the orchestration component when Jido-oriented runtime
   configuration is present.
2. `cmd/agentctl/cmd/overseer_v2_orchestration.go` opens the v2 event store and
   orchestration projection store.
3. The Jido adapter creates:
   - a JSON-RPC client to the Jido runtime
   - a child spawner that converts v2 spawn requests into runtime child-spawn calls
   - a reconciler that polls Jido child state and appends canonical v2 events
4. `internal/v2/runtime/orchestration.Scheduler` pulls candidate cards from the board
   read model and dispatches them through `internal/v2/services.OrchestrationService`.
5. The runtime component loops on a poll interval, dispatching new work and reconciling
   child lifecycle updates back into the v2 event/projection stores.
6. Some web/UI runtime inspection surfaces still read live runtime state through
   optional Jido-backed helpers instead of purely from Go-owned state.

## Dependency order for the Go-native migration

This is the order that now matters for planning:

1. **Define Go-owned runtime facts**
   Parent/child links, worker status, heartbeat, exit state, and runtime tree inputs
   must have a durable Go-owned model. See `docs/spec/runtime-backend-contracts.md`.
2. **Ship the default Go runtime adapter**
   A subprocess-backed `RuntimeSpawner` and supervisor become the default worker path.
3. **Move reconcile onto Go-owned worker state**
   Orchestration reconcile must stop depending on Jido runtime polling for default
   operation.
4. **Move runtime tree readers onto Go-owned state**
   CLI/web/API runtime inspection should read projections and registry state first.
5. **Make Jido explicitly optional**
   Jido remains available when configured, but no longer required for default flows.
6. **Then improve engine/backend pluggability**
   Eino or other backends can be added once runtime ownership is already stable in Go.

## Important runtime configuration

These settings still matter for the current hybrid runtime. The target is that the
default path no longer requires them for core orchestration/runtime behavior.

| Setting | Purpose |
|--------|---------|
| `AGENTCTL_JIDO_SOCKET` | Unix socket path for the Jido JSON-RPC bridge |
| `AGENTCTL_JIDO_RPC_PATH` | HTTP path used over the Jido socket |
| `AGENTCTL_JIDO_RPC_TIMEOUT_MS` | Jido RPC timeout |
| `AGENTCTL_JIDO_ORCHESTRATION_PARENT_AGENT_IDS` | Enables Jido orchestration runtime and defines parent agents to watch |
| `AGENTCTL_JIDO_ORCHESTRATION_DISPATCH_PARENT_AGENT_ID` | Parent agent ID used for new orchestration dispatches |
| `AGENTCTL_JIDO_SIGNAL_SOURCE` | Source tag for runtime signals |
| `AGENTCTL_COMPANION_CONTEXT_PROVIDER=jido` | Enables Jido-backed companion layered context fetches |

## Reading order

1. `docs/architecture/system-architecture.md`
2. `docs/architecture/go-native-runtime-and-optional-jido.md`
3. `docs/architecture/jido-hybrid-runtime.md`
4. `docs/spec/runtime-backend-contracts.md`
5. `docs/general/agent-daemon.md`
6. `docs/spec/agent_hierarchy.md`
7. `docs/spec/overseer_profile.md`

## Related docs

- `docs/architecture/system-architecture.md`
- `docs/architecture/go-native-runtime-and-optional-jido.md`
- `docs/architecture/jido-hybrid-runtime.md`
- `docs/spec/runtime-backend-contracts.md`
- `docs/general/agent-daemon.md`
- `docs/general/storage.md`
- `docs/spec/agent_hierarchy.md`
- `docs/spec/overseer_profile.md`
