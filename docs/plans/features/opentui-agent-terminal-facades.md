# OpenTUI Agent Terminal Facades

Status: Draft implementation plan  
Owner: Solo maintainer  
Last Updated: 2026-04-21

## Implementation Progress

The first backend facade tranche is partially implemented:

- v2 run listing, run detail, and persisted event replay are available through
  `/api/v2/runs` and `/api/v2/runs/{run_id}/events`.
- v2 run creation is wired through `POST /api/v2/runs` using the default v2
  `RunService`, libsql event storage, immediate projection updates, and a
  tool-aware LLM model adapter.
- v2 model calls now receive profile-scoped tool definitions and message
  history, including prior assistant tool calls and tool result messages.
- v2 run kill is wired through `POST /api/v2/runs/{run_id}/kill`; active
  local runs are cancelled and stale running projections receive a durable
  `run.failed` kill event.
- top-level v2 event replay is available through `/api/v2/events`.
- v2 projection storage now supports request-id lookup and filtered run-state
  listing through a service adapter.
- room control snapshots are available through
  `/api/rooms/{id}/control-snapshot`.
- room participant status includes transport and binding fields.
- room task and loop mutations emit room-scoped invalidation events.
- skill manifest detail is available through `/api/skills/manifest/{name}`.
- job progress, events, cancel, and wait are available through
  `/api/jobs/{id}/progress`, `/events`, `/cancel`, and `/wait`.
- CAS list/read/pin/unpin web routes are available.
- read-only MCP status and skill-backed tool inventory routes are available.

Remaining hard blockers:

- room task/card linking is currently a narrow `issue_id == task_id` bridge;
  durable typed linking still needs schema-backed work.
- v2 event replay is deterministic, but live replay-then-subscribe fanout over
  the runtime event bus is not wired yet.
- MCP tool inventory is skill-backed, not live daemon `ListTools` introspection.

## Goal

Prepare `foxctl` for a greenfield OpenTUI agent terminal by adding stable
backend facades before building the terminal client.

The target terminal is a developer-facing control plane for rooms, agents,
v2 runs, skills, integrations, and artifacts. It should consume `foxctl` as a
headless runtime kernel through HTTP/SSE/WebSocket APIs rather than importing Go
internals or reimplementing tool execution in TypeScript.

## Non-Goals

- Do not reuse or depend on previous TUI implementation code.
- Do not place terminal client code under `internal/v2`; `internal/v2` remains
  the agent/runtime/orchestration lane.
- Do not make OpenTUI own room or agent semantics.
- Do not make terminal scrollback the source of truth for room history,
  runtime events, or skill output.
- Do not introduce keyword/body-marker routing for task/card actions; use
  explicit fields and typed links.

## Architectural Boundary

The OpenTUI app should be a Bun/TypeScript workspace package, likely
`packages/opentui-agent` or a product name such as `packages/foxterm`.

The app should use a small API adapter layer, preferably shared through
`@foxctl/data`, and consume:

- v2 runtime command and event facades
- room control snapshots and room-scoped events
- orchestration board/card/runtime APIs
- job-backed skill execution
- MCP, provider, CAS, and job inspection APIs

The Go backend remains the source of truth for:

- v2 runs, turns, tools, profiles, and events
- room timeline, participants, loop state, tasks, reminders, and delivery
- orchestration cards, runtime trees, and worker lifecycle
- skills, jobs, CAS artifacts, provider config, and MCP tool inventory

## Required Facades

### P0: V2 Runtime Command Facade

Add a v2-first HTTP route group that wraps the canonical v2 services directly.

Proposed routes:

- `POST /api/v2/runs`
- `GET /api/v2/runs`
- `GET /api/v2/runs/{run_id}`
- `GET /api/v2/runs/{run_id}/events?after_version=N`
- `POST /api/v2/runs/{run_id}/kill`
- `POST /api/v2/agents/spawn`
- `POST /api/v2/agents/{agent_id}/ask`

Service mapping:

- `RunService.Run(ctx, run.TurnInput)`
- `ListService.List(ctx, list.Request)`
- `KillService.Kill(ctx, kill.Request)`
- `SpawnService.Spawn(ctx, spawn.Request)`
- `AskService.Ask(ctx, ask.Request)`
- event store `ListStream(ctx, events.StreamFilter)`

Acceptance:

- terminal clients can create, list, inspect, kill, and replay v2 runs without
  using legacy `/api/agents/{id}/ask-stream`
- responses preserve v2 request IDs, run IDs, turn IDs, agent IDs, actor IDs,
  status, degraded state, and stage failures
- handlers are thin interfaces over `internal/v2/services`
- service errors map deterministically to API errors

Likely files:

- `internal/interfaces/web/api/v2_runtime.go`
- `internal/interfaces/web/api/v2_runtime_test.go`
- `internal/interfaces/web/server.go`
- `internal/v2/adapters/libsql/projections/store.go`
- `internal/v2/adapters/libsql/projections/store_test.go`

### P0: V2 Projection Adapter Completion

Finish the projection adapter methods required by `v2services.ProjectionStore`.

Required methods:

- `GetRunStateByRequestID(ctx, requestID)`
- `ListRunStates(ctx, filter)`

Implementation notes:

- query `v2_run_state.request_id` for idempotency
- support deterministic filtering by status, command, actor ID, and limit
- normalize missing rows to the not-found error expected by v2 services
- avoid importing interface/API packages into adapter packages

Acceptance:

- `SpawnService` idempotency can be wired to the projection store
- `ListService` can serve `/api/v2/runs`
- tests cover empty filters, clamped limits, request-id lookup, and not-found
  behavior

### P0: V2 Event Replay and Live Stream

Add a canonical event stream for v2 runtime events.

Proposed route:

- `GET /api/v2/events?stream_id=&stream_type=&after_version=`

Behavior:

1. validate stream filters
2. replay persisted events from `ListStream`
3. optionally continue with live events from the v2 runtime event bus
4. preserve canonical v2 event envelope fields

Payload fields:

- `id`
- `stream_id`
- `stream_type`
- `stream_version`
- `sequence`
- `event_type`
- `occurred_at`
- `correlation_id`
- `causation_id`
- `actor_id`
- `request_id`
- `command`
- `payload`

Acceptance:

- clients can reconnect using `after_version`
- replayed events are ordered by stream version
- live fanout is best-effort, but replay remains authoritative
- the terminal does not rely on global `/api/events` for v2 run recovery

### P1: Room Control Snapshot Facade

Add a single bootstrap endpoint for room terminals.

Proposed route:

- `GET /api/rooms/{id}/control-snapshot?workspace_id=...&actor_id=...`

Payload should include:

- room summary
- members and roles
- participant transport and delivery binding
- participant runtime/agent/worker summary when known
- loop state, heartbeat, delivery owner, and last delivery trace
- inbox summary/counts for the actor
- active reminders
- linked room tasks
- linked orchestration cards
- recent messages
- relevant runtime summaries

Acceptance:

- initial OpenTUI room load needs one read request plus event subscription
- loop health is explicit: active, stale, missing heartbeat, missing owner
- participant membership, runtime process, transport binding, and viewer/mux
  state are distinct fields

Likely files:

- `internal/interfaces/web/api/rooms.go`
- `internal/interfaces/web/api/room_control.go`
- `internal/interfaces/web/server.go`

### P1: Room-Scoped Event Coverage

Expand room-scoped event emission beyond room messages.

Events to add:

- `room.task.updated`
- `room.reminder.updated`
- `room.loop.updated`
- `room.member.updated`
- `room.card.updated`
- `room.runtime.updated`

Acceptance:

- task lifecycle actions update room clients without polling
- reminder add/cancel updates room clients without polling
- loop patch/heartbeat/delivery-owner changes are visible
- member binding and coordinator changes are visible
- orchestration/card changes linked to a room invalidate the room view

### P1: Room Task and Orchestration Card Cross-Links

Introduce explicit typed links between room tasks, orchestration cards, agents,
and runtime runs.

Possible implementation choices:

- add `room_id` and `task_id` fields to orchestration card metadata/projection
- or add a join table with `room_id`, `task_id`, `issue_id`, `run_id`,
  `agent_id`, and timestamps

Also add board/card filters:

- `room_id`
- `task_id`
- `agent_id`
- `run_id`
- `parent_agent_id`
- `only_active_runtime`

Acceptance:

- terminal can navigate task -> card -> agent -> runtime tree without parsing
  message bodies
- room-to-card actions use explicit context or dedicated endpoints
- message body markers are not required for room/card action routing

Likely files:

- `internal/v2/core/orchestration/types.go`
- `internal/v2/adapters/libsql/orchestration/schema.go`
- `internal/v2/adapters/libsql/orchestration/store.go`
- `internal/interfaces/web/api/orchestration.go`
- `internal/interfaces/web/api/rooms.go`

### P1: Runtime Signal Facade

Expose room/card-scoped runtime controls without requiring clients to map
everything back to agent-only endpoints.

Proposed routes:

- `POST /api/orchestration/cards/{issue_id}/runtime/signal`
- `POST /api/rooms/{id}/members/{actor_id}/signal`

Signals:

- `interrupt`
- `cancel`
- `kill`
- `restart` later, after lifecycle semantics are nailed down

Acceptance:

- all signal requests include `request_id`
- repeated signal requests are idempotent where possible
- room/card endpoints resolve to the same runtime host and worker signal path
- errors distinguish missing runtime, unsupported backend, and stale binding

Likely files:

- `internal/interfaces/web/api/orchestration_runtime_host.go`
- `internal/v2/adapters/goruntime/signaler.go`
- `internal/v2/core/worker/types.go`
- `internal/interfaces/web/api/agents.go`
- `internal/interfaces/web/api/rooms.go`

### P2: Job-Backed Skill Runner Facade

Upgrade skill execution for command-palette use.

Proposed request fields:

- `skill`
- `input`
- `mode`: `ephemeral` or `job`
- `async`
- `dedupe`
- `timeout_ms`
- `workspace`
- `correlation_id`

Proposed job routes:

- `GET /api/jobs/{id}/progress`
- `GET /api/jobs/{id}/events`
- `POST /api/jobs/{id}/cancel`
- `POST /api/jobs/{id}/wait`

Acceptance:

- short skill runs can return immediately
- long skill runs return a job ID and can be followed/cancelled
- stderr, output envelope, CAS artifacts, and duration are inspectable
- global events and job-specific progress are typed enough for terminal panes

Likely files:

- `internal/interfaces/web/api/skills.go`
- `internal/interfaces/web/api/jobs.go`
- `cmd/foxctl/cmd/run.go`
- `cmd/foxctl/cmd/jobs.go`
- `internal/storage/jobs`

### P2: Command Palette Inventory

Add one synthesized action inventory for terminal command palettes.

Proposed route:

- `GET /api/command-palette/actions`

Model:

```ts
type CommandPaletteAction = {
  id: string
  title: string
  category: string
  source: "skill" | "mcp" | "job" | "cas" | "provider" | "agent" | "room" | "orchestration"
  command: string
  schema?: unknown
  destructive: boolean
  async_capable: boolean
  requires_confirmation: boolean
}
```

Acceptance:

- terminal can render a unified command palette from one endpoint
- destructive actions are flagged explicitly
- async-capable actions are flagged explicitly
- actions can be filtered by current workspace, room, agent, or card context

### P2: MCP, Provider, CAS, and Artifact Facades

Add machine-readable supporting facades for integrations and artifacts.

Suggested routes:

- `GET /api/mcp/status`
- `GET /api/mcp/tools`
- `POST /api/mcp/start`
- `POST /api/mcp/stop`
- `GET /api/providers`
- `POST /api/providers/config`
- `GET /api/cas`
- `GET /api/cas/{digest}/read?page=&page_size=`
- `POST /api/cas`
- `POST /api/cas/{digest}/pin`
- `POST /api/cas/{digest}/unpin`

Acceptance:

- MCP inventory includes enabled groups, transport, tool names, schemas, and
  health/error state
- provider config operations can be previewed and applied safely
- CAS objects can be listed, previewed, pinned, and unpinned from terminal UI

## OpenTUI Client Sequencing

Do not start the full OpenTUI app until these backend conditions are met:

1. v2 run/list/replay facade exists
2. room control snapshot exists
3. room event coverage covers tasks, reminders, loop, and members
4. job-backed skill execution has at least a narrow async path

After that, build the first client slice:

1. read-only cockpit: rooms, participants, agents, loop health, v2 runs,
   orchestration board, and activity stream
2. controlled writes: send room message, direct ask, claim/touch/block/complete
   task, schedule/cancel reminder
3. command palette: skill run, job follow/cancel, OpenAPI dry-run/execute,
   MCP tool preview/call
4. artifact and evidence panes: CAS preview, logs, job stderr, v2 event detail

## Worker Split

### Worker A: V2 Runtime Facade

Owns:

- projection adapter completion
- v2 run/list/kill/spawn/ask HTTP handlers
- v2 run event replay endpoint
- tests for service wiring and error mapping

Write scope:

- `internal/v2/adapters/libsql/projections/*`
- `internal/interfaces/web/api/v2_runtime*`
- `internal/interfaces/web/server.go`

### Worker B: Room and Orchestration Facades

Owns:

- room control snapshot endpoint
- participant runtime/transport status fields
- room-scoped event expansion
- task/card link design and narrow first implementation
- room/card signal facade if feasible

Write scope:

- `internal/interfaces/web/api/rooms.go`
- `internal/interfaces/web/api/room_control.go`
- `internal/interfaces/web/api/orchestration.go`
- `internal/v2/core/orchestration/*`
- `internal/v2/adapters/libsql/orchestration/*`

### Worker C: Skills, Jobs, MCP, CAS, and Palette Facades

Owns:

- skill manifest/detail endpoint
- job-backed skill run proposal/implementation slice
- job progress/cancel/wait web routes
- MCP status/tools routes or skill-backed facade
- CAS list/read/pin/unpin route parity
- command palette inventory model and route if feasible

Write scope:

- `internal/interfaces/web/api/skills.go`
- `internal/interfaces/web/api/jobs.go`
- `internal/interfaces/web/api/cas.go`
- `internal/interfaces/web/server.go`
- `cmd/foxctl/cmd/mcp.go` only if needed for reusable MCP inventory helpers
- `packages/data/src/*` only for type/client updates

## Verification

Required checks depend on touched files:

- Go API/service changes: targeted `go test` package runs
- v2 adapter changes: `go test ./internal/v2/...` or narrowed packages
- web API changes: `go test ./internal/interfaces/web/...`
- docs changes: `make check-doc-links`
- TypeScript client changes: `bun run typecheck` or package-local typecheck

## Open Questions

- Should v2 ask lifecycle get explicit `ask.dispatched`, `ask.replied`, and
  `ask.failed` event types, or stay encoded as run events for the first slice?
- Should v2 event live streaming use the existing web SSE hub, a dedicated SSE
  handler, or both?
- Should task/card cross-linking live in orchestration card metadata, a join
  table, or task metadata?
- Should provider config remain skill-backed or become a first-class web API?
- Should OpenTUI eventually use a WebSocket command channel, or is HTTP plus
  replayable SSE enough?
