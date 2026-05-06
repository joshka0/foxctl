# Runtime Orchestration

Current map of how agent runs, ask flows, orchestration, and runtime inspection move
through the repo, plus the dependency order for getting to a Go-owned runtime.

## Scope

| In scope | Out of scope |
|---------|---------------|
| Current execution and orchestration paths for CLI, daemon, web, v2 services, Jido bridge, and runtime ownership migration | Deep store schema details (see `storage.md`) |

## Current state

The runtime is still hybrid, but v2 orchestration defaults to the Go-owned
runtime:

1. The mailbox-driven agent runtime under `internal/agent` and
   `internal/agent/daemon` remains active for `agent run` and several classic agent
   management surfaces.
2. The newer v2 stack under `internal/v2` owns the clearest event-sourced command and
   orchestration surfaces.
3. Go runtime state is the default orchestration backend. Jido remains as an
   explicit optional backend for compatibility and bridge-specific flows.

That means new durable-execution work should target Go runtime plus Turso-backed
v2 stores first, then keep Jido behavior as optional compatibility where it is
still wired.

Important scope note:

- `internal/v2/*` should be read as the newer **agent/runtime/orchestration**
  lane.
- It is not the intended replacement namespace for context, retrieval, storage,
  or interface packages outside that lane.
- The canonical package grouping map and explicit legacy-to-v2 replacement table
  live in `docs/architecture/package-topology.md`.

## Core packages

| Package(s) | Responsibility |
|-----------|----------------|
| `internal/v2/core/*` | Typed v2 command, event, spawn, orchestration, and policy contracts |
| `internal/v2/services` | Command services and orchestration services such as `ask`, `run`, `spawn`, `list`, `kill`, and orchestration dispatch |
| `internal/v2/runtime/runner` | Synchronous turn pipeline, event emission, and turn persistence wiring |
| `internal/v2/runtime/orchestration` | Long-lived scheduler/reconcile component for board-driven orchestration |
| `internal/v2/runtime/contextbuilder`, `internal/v2/runtime/enrichers`, `internal/v2/runtime/supervisor` | Context assembly, async enrichment, and component lifecycle management |
| `internal/v2/adapters/jido` | Jido JSON-RPC client, child spawner, ask/runtime adapter, orchestration reconciler, and optional companion provider |
| `internal/v2/adapters/turso/*` | v2 events, projections, orchestration, idmap, and turn stores. The old `internal/v2/adapters/libsql` path remains only in `main` history at `938733293b81c9be8787e15300661cf587baa8af`. |
| `internal/context/companion` | Companion chat/memory service and adapter layer into v2 context building |
| `internal/agent`, `internal/agent/daemon` | Classic mailbox-driven agent runtime, overseer hierarchy, tool wiring, and foreground daemon loop |
| `internal/runtime/daemon` | Local daemon service; currently mixes classic runtime behavior with newer v2-backed command helpers |
| `internal/runtime/execution/agentmanager` | Legacy spawn/kill management path still used by some CLI flows |

## Legacy vs V2 Runtime Boundary

Use this shorthand when talking about “legacy” vs “v2” in runtime discussions:

| Legacy/current path | V2 replacement target | Notes |
|---------------------|-----------------------|-------|
| `internal/agent/runtime` | `internal/v2/runtime/*` plus `internal/v2/services/*` | Core agent session/runtime replacement seam |
| `internal/agent/daemon` | `internal/v2/runtime/{runner,orchestration,supervisor}` | Foreground daemon loop replacement is partial |
| `internal/runtime/execution/agentmanager` | `internal/v2/services/{spawn,kill,list,run}` | Still used as fallback in some CLI flows |
| agent-management logic in `internal/runtime/daemon` | prefer `internal/v2/services/*` semantics | `internal/runtime/daemon` remains the hosting shell in places |
| live Jido runtime-state dependencies in `internal/v2/adapters/jido` | Go-owned runtime state with Jido optional | Adapter remains for explicit compatibility paths |

For the broader package topology, including what `v2` is **not** replacing, see
`docs/architecture/package-topology.md`.

## Surface map

| Surface | Current path | Notes |
|--------|--------------|-------|
| `agent ask` | `cmd/foxctl/cmd/agent.go` -> `internal/v2/services.AskService` | Default can be mailbox-backed; Jido remains an optional dispatcher path |
| `agent ask-status` | CLI -> v2 projections/events | Reads v2 run state and terminal callback metadata |
| Overseer orchestration component | `cmd/foxctl/cmd/overseer_v2_orchestration.go` -> `internal/v2/runtime/orchestration` + runtime adapter | Defaults to Go runtime; set `FOXCTL_V2_ORCHESTRATION_RUNTIME_BACKEND=jido` for the bridge path |
| Companion layered context | `internal/context/companion` -> `internal/v2/runtime/contextbuilder` -> optional provider | Jido-backed provider is optional, not the desired default |
| `agent run` | CLI -> `internal/agent/daemon.Run` | Still classic mailbox-driven runtime |
| `agent spawn` | CLI prefers daemon path, then falls back to legacy `agentmanager` | Not hard-cut to v2 everywhere |
| `agent list` | CLI -> local agents store | Not v2-service-only in current CLI |
| `agent kill` | Mixed; CLI still uses legacy/local management path in places | v2 kill service exists but is not the only live path |
| Web/API runtime tree views | `internal/interfaces/web/api` plus Go worker state, with optional Jido client for explicit bridge paths | Default runtime-tree reads use Go-owned worker state |

## Runtime ownership seams that matter now

These are the concrete seams that determine how far the Go-owned runtime default
has replaced older bridge-dependent paths.

| Concern | Current shape | Target shape |
|--------|---------------|--------------|
| Child spawn | v2 service already abstracts `RuntimeSpawner` | Default Go spawner backed by Go-owned worker state |
| Child lifecycle | Go-owned registry and reconciler are the default; Jido remains explicit | Harden heartbeat, exit state, and orphan recovery |
| Runtime tree inspection | Go registry first, optional Jido branch when selected | Trees derived from Go registry + projections |
| Control-plane truth | Projections plus Go-owned runtime state | Keep projections and worker registry canonical for default flows |
| Engine/backend flexibility | Constrained by runtime ownership not being fully local | Runtime contracts first, engine/backend pluggability second |

## Optional Jido-backed orchestration flow

This path remains available only when `FOXCTL_V2_ORCHESTRATION_RUNTIME_BACKEND`
is set to `jido`:

1. CLI or daemon wiring selects the Jido orchestration component when explicitly
   configured.
2. `cmd/foxctl/cmd/overseer_v2_orchestration.go` opens the v2 event store and
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
| `FOXCTL_JIDO_SOCKET` | Unix socket path for the Jido JSON-RPC bridge |
| `FOXCTL_JIDO_RPC_PATH` | HTTP path used over the Jido socket |
| `FOXCTL_JIDO_RPC_TIMEOUT_MS` | Jido RPC timeout |
| `FOXCTL_JIDO_ORCHESTRATION_PARENT_AGENT_IDS` | Enables Jido orchestration runtime and defines parent agents to watch |
| `FOXCTL_JIDO_ORCHESTRATION_DISPATCH_PARENT_AGENT_ID` | Parent agent ID used for new orchestration dispatches |
| `FOXCTL_JIDO_SIGNAL_SOURCE` | Source tag for runtime signals |
| `FOXCTL_COMPANION_CONTEXT_PROVIDER=jido` | Enables Jido-backed companion layered context fetches |

## Reading order

1. `docs/architecture/system-architecture.md`
2. `docs/architecture/package-topology.md`
3. `docs/architecture/go-native-runtime-and-optional-jido.md`
4. `docs/architecture/jido-hybrid-runtime.md`
5. `docs/spec/runtime-backend-contracts.md`
6. `docs/general/agent-daemon.md`
7. `docs/spec/agent_hierarchy.md`
8. `docs/spec/overseer_profile.md`

## Related docs

- `docs/architecture/system-architecture.md`
- `docs/architecture/package-topology.md`
- `docs/architecture/go-native-runtime-and-optional-jido.md`
- `docs/architecture/jido-hybrid-runtime.md`
- `docs/spec/runtime-backend-contracts.md`
- `docs/general/agent-daemon.md`
- `docs/general/storage.md`
- `docs/spec/agent_hierarchy.md`
- `docs/spec/overseer_profile.md`
