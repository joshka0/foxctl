# OpenTUI Agent Terminal Facades

Status: Draft implementation plan  
Owner: Solo maintainer  
Last Updated: 2026-04-22

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
- top-level v2 event replay is available through `/api/v2/events`, and
  replay-then-subscribe live streaming is available through
  `/api/v2/events/stream`.
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
- greenfield OpenTUI client scaffold is available under `packages/foxterm`
  with a keyboard-first v2 runs cockpit and live run event stream panel.
- foxterm Runs now has a first terminal workflow layer: a keyboard prompt
  composer, `POST /api/v2/runs` submission, selected-run assistant output, and
  compact activity below the output.
- selected active runs can be killed from foxterm through a guarded
  confirmation flow backed by `POST /api/v2/runs/{run_id}/kill`.
- `POST /api/v2/runs?async=true` starts a run under a cancelable background
  context and returns immediately with stable run/turn/request IDs; foxterm
  uses this path so the event stream becomes the live run surface.
- `GET /api/v2/runs/{run_id}/transcript` returns a typed transcript derived
  from v2 events, including prompt, tool calls/results, turn records, errors,
  and assistant output.
- foxterm can submit follow-up turns against the selected run ID, keeping the
  conversation in one run stream and transcript.
- foxterm can seed a new run from the selected room task or orchestration card,
  and cards with `run_id` can jump directly to the linked run.
- foxterm can create a room from the Rooms scope through `POST /api/rooms`,
  then refresh and select the newly created room summary.
- foxterm room creation now attaches the local actor as coordinator, and the
  Rooms scope can read recent room messages and send a room message through
  `/api/rooms/{id}/messages`.
- foxterm reads `/api/rooms/{id}/loop` for room-loop heartbeat and delivery
  owner status, and shows the `foxctl room loop` start command when message
  delivery is not ready.
- foxterm can spawn a foxctl daemon agent into the selected room through
  `/api/agents/spawn`, using the existing room-aware prompt/onboarding and room
  membership attachment path.
- foxterm cross-references room members with `/api/agents` so the room detail
  can show member agent state, role, model, and execution mode after spawn.
- CLI-backed agents through ATCP are a distinct runtime path from daemon
  agents. A first ATCP web facade is available for health, sessions, rooms,
  and foxctl-room-linked CLI spawn; foxterm can start a CLI session with
  `agent@adapter: command args` and join it to the selected room's linked ATCP
  room.
- foxterm reads the linked ATCP room sessions and readiness state so CLI-backed
  agents appear in the room detail panel after spawn.
- foxterm ATCP CLI spawn now uses an OpenTUI-style preset picker with editable
  agent, adapter, and command fields, while preserving raw
  `agent@adapter: command args` custom input.
- ATCP session summaries now include rendered screen snapshots, and foxterm
  shows the latest non-empty screen line for each CLI-backed room participant.
- foxterm can cycle the focused ATCP CLI session and render a larger screen
  excerpt in the room inspector.
- foxterm can prompt the focused ATCP CLI session through
  `POST /api/atcp/foxctl-rooms/{room_id}/messages`; the facade resolves the
  room-linked ATCP room and targets the selected participant by structured
  member identity.

Remaining hard blockers:

- room task/card linking is currently a narrow `issue_id == task_id` bridge;
  durable typed linking still needs schema-backed work.
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

## High-Agent UX Model

Foxterm is a work coordination terminal, not an agent process list.

When many agents are running, the TUI must keep work contexts primary and make
agents secondary metadata. A developer usually needs to know which room, task,
card, run, or job needs attention; the owning agent matters once that work item
is selected or becomes blocked, failed, or otherwise actionable.

Primary hierarchy:

```text
Workspace
  Room
    Epic / task / card
      Run
        Agent / worker
          events / tools / artifacts
```

Default screens should show the middle of this hierarchy: rooms, tasks/cards,
runs, jobs, and important activity. Agents should appear as owner/status badges,
participant summaries, and drill-in details rather than as a global flat list.

### Default Control View

The default view should aggregate active work, not enumerate every participant.

```text
Scope              Active Work                         Inspector
Control            alpha / T-42     running   5 agents Room alpha
Rooms              alpha / T-41     blocked   2 agents loop ok
Runs               beta  / T-09     review    1 agent  agents 14 total
Jobs               gamma / T-02     failed    3 agents running 5 idle 7 failed 2
Tools

Activity
#182 alpha  T-42  tool.responded   context/show        @worker-a
#183 alpha  T-41  blocked          missing provider    @worker-b
#184 beta   T-09  run.completed    summary available   @reviewer
```

Agent summary counters should be visible without forcing a flat agent list:

- total agents
- running
- blocked
- failed
- idle
- stale heartbeat / missing transport, when known

### Agent View

An explicit Agents view can exist, but it should group by state and context.

```text
Agents
Running 5
> @worker-a     alpha / T-42      context/show       12s
  @worker-c     alpha / T-43      code/search        31s
  @reviewer     beta  / T-09      reviewing          1m

Blocked 2
  @worker-b     alpha / T-41      provider missing   8m

Idle 7
  @planner      alpha             waiting            3m
```

Even in the Agents view, the key columns are context, current work, current
activity, status, and age. Agent identity alone is not enough.

### Activity Scopes

Live streams should default to focused, useful activity. A global firehose must
be opt-in.

Activity modes:

- `focused`: selected room/task/card/run/job only
- `important`: failures, blocked work, completions, handoffs, stale bindings
- `all`: raw event stream, opt-in
- `debug`: verbose logs and tool payloads, opt-in

Default streams should suppress low-value noise:

- heartbeats with no status change
- repeated idle updates
- unchanged status refreshes
- verbose logs unless the selected pane is a debug/log view

### Stable Selection Rules

High-agent interfaces fail when live updates move the user's focus.

Rules:

- track selection by stable ID, not by row index
- do not auto-select newly inserted rows
- do not move the selected row just because timestamps changed
- if sorting by recency, keep the selected identity pinned in view when possible
- show `N new` or `N updates` instead of jumping the viewport
- if the selected item completes or fails, update the inspector but preserve
  focus and scroll position

### Required UI Primitives

Before adding more resource views, build these primitives:

1. grouped worklist with stable ID selection
2. filter mode for the active pane (`/`)
3. activity scope control (`focused`, `important`, `all`, `debug`)
4. status counters for rooms, runs, jobs, agents, and blocked/failed work
5. event compaction from raw event payloads to semantic summaries
6. contextual command palette with destructive-action confirmation
7. payload/detail overlay for raw JSON, logs, CAS artifacts, and errors

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

The app has started as `packages/foxterm`. Build it layer by layer so each
slice remains usable and does not collapse under many concurrent agents.

### Layer 0: Shell and Runtime Safety

- alternate-screen renderer with guaranteed cleanup
- fixed header/footer
- responsive sidebar/worklist/inspector layout
- visible focus state
- `Tab`, `Shift+Tab`, `j/k`, arrows, `?`, `Esc`, `q`, `Ctrl+C`
- contextual help overlay

Status: initial scaffold exists.

### Layer 1: Stable Worklist Foundation

- extract reusable shell/panel/list/inspector components
- track selection by stable item ID instead of row index
- preserve selection across refreshes and live updates
- add grouped worklist sections
- add empty, loading, stale, and error states that preserve layout

Primary target: v2 runs first, then room tasks/cards.

Status: v2 runs, room tasks, and orchestration cards now use reusable
panel/worklist primitives, stable ID selection, grouped sections, preserved
selection across refreshes, and loading/error/stale states. Room tasks load
from `/api/rooms` plus `/api/rooms/{id}/tasks`; cards load from
`/api/orchestration/board-get`. Both use `FOXTERM_WORKSPACE_ID` or
`FOXCTL_WORKSPACE_ID`, with `.` as the local default.

### Layer 2: Filtering and Activity Scope

- `/` filter mode for active pane
- filter by status, room, actor, agent, command, and text
- activity modes: `focused`, `important`, `all`, `debug`
- event compaction and severity tagging
- suppress repeated low-value updates by default

Status: `/` filter mode is available for the active worklist across runs, room
tasks, and orchestration cards. Filters match explicit visible fields such as
status, room, actor, agent, command, ID, title, lane, and policy. `a` cycles the
run detail activity scope through `focused`, `important`, `all`, and `debug`;
`important` uses explicit v2 event types rather than substring matching. Run
events render as compact severity-tagged activity rows, and repeated adjacent
activity rows are folded with counts outside `debug` scope. Broader live-stream
dedupe across room/card activity is still pending.

### Layer 3: Control View

- default cockpit over rooms, active work, active runs, jobs, and important
  activity
- room loop health counters
- agent status counters, not flat agent rows
- blocked/failed/stale summaries
- inspector that explains why a selected work item needs attention

### Layer 4: Contextual Inspectors

- run inspector: stages, tool calls, stage failures, event payloads
- room inspector: participants, transport bindings, loop state, tasks
- job inspector: progress, stderr, result envelope, CAS artifacts
- artifact inspector: CAS preview with pagination/copyable content

### Layer 5: Command Palette and Safe Actions

- `:` or `Ctrl+P` command palette
- contextual commands by selected work item
- confirmation overlay for destructive actions
- first safe writes: refresh, kill run, cancel job
- next writes: send room message, claim/block/complete task, ask participant

### Layer 6: Integrations and Tooling

- skill manifest and run forms
- MCP status/tool inventory
- provider/integration status
- OpenAPI dry-run/execute workflow
- command history and recent artifacts

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
