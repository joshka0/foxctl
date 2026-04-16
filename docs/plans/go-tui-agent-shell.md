---
vault_refs:
  - notes/repo/foxctl/platform-and-web.md
  - notes/repo/foxctl/semantic-and-memory.md
  - notes/repo/foxctl/skills-runtime-wiring.md
---

# Go TUI Agent Shell Plan

Status: Draft  
Owner: Solo maintainer  
Last Updated: 2026-04-16

## Goal

Build a Go-native terminal shell for `foxctl` using `go-tui` that feels closer to
Codex / Claude Code than to a dashboard.

The shell should be centered on one foreground assistant with:

1. a live transcript
2. streamed tool activity
3. an operator-visible memory layer
4. bounded worker/subagent support
5. task and orchestration awareness

This plan intentionally ignores previous TUI implementations as design inputs.
It only reuses current backend/runtime contracts that are already part of
`foxctl`.

## Assumptions

These assumptions are baked into the first draft unless changed later:

1. The first implementation is a separate Go binary, not a `foxctl` subcommand.
2. The first implementation uses `go-tui` and `.gsx` generation.
3. The first implementation reads from live `foxctl` HTTP + SSE/WebSocket
   surfaces instead of scraping CLI output.
4. The first implementation treats one foreground assistant as the primary UX,
   with workers and orchestration as supporting views.
5. The first implementation exposes memory as an inspectable and controllable
   subsystem, not as hidden prompt magic.

## Product Shape

The shell should feel like a coding workbench, not a tabbed admin console.

Primary object model:

1. `Foreground Assistant`
   - the active agent/session the user is talking to
2. `Transcript`
   - streamed interaction with that assistant
3. `Memory`
   - the current layered memory context and searchable persistent artifacts
4. `Continuity`
   - ACA/top-of-mind/task-history state for the current workspace/task
5. `Workers`
   - spawned task-bounded helpers and their runtime state
6. `Board`
   - orchestration/task/room state linked to the current workstream

The mental model is:

```text
user -> foreground assistant -> tools / workers / rooms / board
                         \-> memory + continuity layers
```

## Screen Spec

### 1. Global Layout

Use a four-region shell:

1. `Top bar`
   - assistant name/id
   - workspace
   - provider/model
   - execution mode
   - memory scope
   - memory retention
   - in-flight status
2. `Center`
   - transcript view
3. `Bottom`
   - composer / command line
4. `Right rail`
   - tabbed side rail: `Memory`, `Continuity`, `Workers`, `Task`

Recommended focus keys:

1. `Ctrl+L` transcript
2. `Ctrl+J` composer
3. `Ctrl+M` memory tab
4. `Ctrl+Y` continuity tab
5. `Ctrl+W` workers tab
6. `Ctrl+B` task/board tab
7. `Ctrl+C` cancel current run
8. `Tab` cycle active pane

### 2. Transcript View

The transcript is the primary surface.

It should render:

1. user messages
2. assistant deltas
3. tool calls
4. tool results
5. completion/error/cancel states

Design rules:

1. tool activity should be visible inline, not hidden behind a log page
2. long tool payloads should collapse by default
3. one selected transcript item can drive detail in the right rail
4. status changes should appear as lightweight system rows, not modal popups

### 3. Composer

The composer should support:

1. free text input
2. slash commands for shell-local actions
3. send / cancel
4. assistant switch
5. quick model/provider override
6. quick memory-policy switch

First slash commands:

1. `/agent`
2. `/spawn`
3. `/memory`
4. `/context`
5. `/task`
6. `/room`
7. `/model`
8. `/clear`

### 4. Memory Tab

The memory tab is a first-class operator surface and should contain four
sections.

#### 4.1 `Injected Now`

Show the exact current layered memory context for the selected
assistant/session.

Sections:

1. hard state
2. active assumptions
3. episode context
4. evidence
5. recent turns

Primary question answered:

`What will the model remember if I send a prompt right now?`

#### 4.2 `Search`

Search persistent memory artifacts for the selected assistant/session.

Each result should show:

1. name
2. type
3. score
4. summary
5. session id if present
6. updated time

Actions:

1. `Pin to next turn`
2. `Open source detail`
3. `Open linked session`

#### 4.3 `Stats`

Show memory health and size:

1. turn count
2. hard-state count
3. episode count
4. evidence count
5. assumptions count
6. context token hint
7. retention preset
8. scope

#### 4.4 `Controls`

Hide destructive or high-impact operations in a dedicated section:

1. compress
2. export
3. import
4. clear conversation/session memory
5. change retention preset

### 5. Continuity Tab

Continuity is not the same as chat memory.

It should expose ACA/task continuity surfaces:

1. top-of-mind summary
2. task-history summary
3. latest handoff
4. open tensions / observations
5. optional family-history summary for the repo/workstream

Actions:

1. `Inject into next turn`
2. `Open artifact`
3. `Refresh continuity`

Primary question answered:

`What is the workspace carrying forward that the assistant should know?`

### 6. Workers Tab

Workers are task-bounded subagents or helpers.

Each worker row should show:

1. name / role
2. state
3. current task
4. memory scope
5. memory retention
6. last event
7. parent link

Worker detail should show:

1. recent transcript preview
2. current memory stats
3. last tool activity
4. linked room / task / board references

Primary actions:

1. spawn worker
2. interrupt/cancel worker
3. open worker detail
4. promote worker result into foreground context

### 7. Task Tab

This tab binds the foreground session to orchestration state.

Show:

1. current issue/task
2. board lane/status
3. related room
4. linked workers
5. recent artifacts/evidence
6. dispatch / refresh actions

Primary question answered:

`How does this conversation connect to the broader workstream?`

## Memory Information Architecture

The shell must preserve separation between three memory planes.

### 1. Companion / Agent Memory

This is the reply-time layered memory for one assistant or conversation.

Use for:

1. what the current assistant remembers
2. what gets injected into the next turn
3. what to inspect when responses feel stale, noisy, or over-attached

Sources:

1. [docs/general/companion-memory.md](../general/companion-memory.md)
2. [internal/context/companion/service.go](../../internal/context/companion/service.go)
3. [internal/context/companion/memory_behavior.go](../../internal/context/companion/memory_behavior.go)

### 2. Named Durable Memory

This is workspace-scoped long-term memory in `memory.db`.

Use for:

1. gotchas
2. decisions
3. summaries
4. semantic lookup across prior durable artifacts

Sources:

1. [docs/general/memory.md](../general/memory.md)

### 3. ACA / Continuity Layer

This is active-work continuity, not conversational memory.

Use for:

1. task continuity
2. handoffs
3. observations/tensions
4. top-of-mind
5. family-history summary

Sources:

1. [docs/architecture/context-architecture.md](../architecture/context-architecture.md)

### Design Rule

Do not merge these three planes into one generic `memory` list.

The shell should expose:

1. `Memory` for companion/agent memory
2. `Continuity` for ACA/task continuity
3. explicit search/pin flows into the transcript

## Memory Policy Defaults

Use retention presets and lineage scope deliberately.

The current domain semantics are defined in
[internal/domain/agent/agent.go](../../internal/domain/agent/agent.go).

### Foreground Assistant

Default:

1. `memory_scope=agent`
2. `memory_retention=durable`

Why:

1. stable identity across shell restarts
2. retains meaningful coding/project continuity
3. avoids persona-heavy companion behavior as the default

### Overseer / Planner

Default:

1. `memory_scope=agent`
2. `memory_retention=durable`

### Task Worker

Default:

1. `memory_scope=session`
2. `memory_retention=task`

Why:

1. bounded memory lineage
2. useful during one unit of work
3. does not pollute the foreground assistant’s durable memory by default

### Scratch Explorer

Default:

1. `memory_scope=session`
2. `memory_retention=ephemeral`

### Companion Persona

Only use when intentionally building a relationship-heavy assistant:

1. `memory_scope=agent`
2. `memory_retention=companion`

## Backend Contract Plan

The first implementation should use live backend surfaces that already exist.

### Foreground Transcript / Console

Use:

1. `POST /api/console/sessions`
2. `POST /api/console/sessions/{id}/ask`
3. `POST /api/console/sessions/{id}/cancel`
4. `GET /api/console/sessions/{id}/events`
5. `WS /ws/console/{id}`

Key files:

1. [internal/interfaces/web/api/console.go](../../internal/interfaces/web/api/console.go)
2. [internal/interfaces/web/consolews/hub.go](../../internal/interfaces/web/consolews/hub.go)
3. [internal/console/session.go](../../internal/console/session.go)
4. [internal/console/app/runner.go](../../internal/console/app/runner.go)

### Agent and Worker Control

Use:

1. `POST /api/agents/spawn`
2. `POST /api/agents/{id}/ask`
3. `POST /api/agents/{id}/ask-stream`
4. `POST /api/agents/{id}/ask-stream/cancel`
5. `GET /api/agents`
6. `GET /api/agents/{id}`

Key files:

1. [internal/interfaces/web/api/agents.go](../../internal/interfaces/web/api/agents.go)
2. [docs/general/agent-daemon.md](../general/agent-daemon.md)
3. [docs/spec/agent_hierarchy.md](../spec/agent_hierarchy.md)

### Memory Surfaces

Use:

1. `GET /api/agents/{id}/memory/stats`
2. `GET /api/agents/{id}/memory/context`
3. `GET /api/agents/{id}/memory/search`
4. `GET /api/companion/memory/{conversation_id}/stats`
5. `GET /api/companion/memory/{conversation_id}/context`
6. `GET /api/companion/memory/{conversation_id}/search`
7. export/import/clear companion memory endpoints where needed

### Continuity and Orchestration

Use:

1. ACA/task continuity commands and wrappers
2. `GET /api/orchestration/board-get`
3. `GET /api/orchestration/board-card-runtime-get`
4. `POST /api/orchestration/refresh`
5. room and global event feeds where needed

## Package Layout

The first Go TUI should live as a separate binary plus one interface-family UI
package.

```text
cmd/foxctl_tui/
  main.go

internal/interfaces/tui/
  app.go
  shell_state.go
  api_client.go
  console_adapter.go
  agent_adapter.go
  memory_adapter.go
  continuity_adapter.go
  worker_adapter.go
  board_adapter.go
  event_stream.go
  keys.go
  models.go
  views/
    shell.gsx
    topbar.gsx
    transcript.gsx
    composer.gsx
    rail_memory.gsx
    rail_continuity.gsx
    rail_workers.gsx
    rail_task.gsx
```

### Placement Rationale

This belongs in `internal/interfaces/*`, not `internal/v2/*`, because it is an
interactive terminal surface over existing runtime and storage contracts.

Source:

1. [docs/architecture/package-topology.md](../architecture/package-topology.md)

## State Model

The shell state should be explicit and typed.

High-level model:

```text
ShellState
  ActiveWorkspace
  ActiveAssistant
  ActiveConsoleSession
  TranscriptState
  ComposerState
  MemoryRailState
  ContinuityRailState
  WorkersRailState
  TaskRailState
  FocusState
```

Rules:

1. normalize backend payloads immediately into typed UI view models
2. keep backend contract structs near adapters, not spread through the view tree
3. treat the transcript as the source of current foreground interaction truth
4. treat side rails as projections over selected assistant / task / transcript item

## Delivery Phases

### Phase 0: Skeleton

Deliver:

1. `cmd/foxctl_tui` binary
2. `internal/interfaces/tui` shell package
3. `go-tui` wiring and generation path
4. one empty shell with mock tabs and focus handling

Definition of done:

1. shell starts
2. keyboard focus works
3. top bar, transcript region, composer, and rail are visible

### Phase 1: Foreground Console

Deliver:

1. create/select console session
2. ask/cancel
3. transcript rendering
4. SSE or WebSocket event handling
5. inline tool activity rows

Definition of done:

1. a user can chat with one foreground assistant/session end to end
2. transcript updates stream without blocking the UI

### Phase 2: Memory Rail

Deliver:

1. `Injected Now`
2. `Search`
3. `Stats`
4. `Controls`

Definition of done:

1. the user can inspect the assistant’s layered memory
2. the user can search memory and pin results into the next turn

### Phase 3: Continuity Rail

Deliver:

1. task-history summary card
2. top-of-mind panel
3. handoff and tension views
4. inject-into-next-turn action

Definition of done:

1. workspace continuity is visible without leaving the shell

### Phase 4: Workers

Deliver:

1. spawn worker flow
2. workers list and detail
3. worker event/status updates
4. retention/scope defaults per worker type

Definition of done:

1. the foreground assistant can delegate
2. the user can inspect and manage spawned workers

### Phase 5: Task / Board Integration

Deliver:

1. current task card
2. board lane/status
3. room/task linkage
4. worker-to-task visibility

Definition of done:

1. the shell ties conversation to orchestration state

## Risks

### 1. Memory Confusion

If `Memory` and `Continuity` are mixed together, users will not understand what
the assistant remembered versus what the workspace continuity layer supplied.

Mitigation:

1. keep separate tabs
2. label injection origin explicitly

### 2. Over-Attached Workers

If workers inherit durable foreground memory by default, subagent behavior will
be noisy and bleed across tasks.

Mitigation:

1. default worker scope to `session`
2. default worker retention to `task` or `ephemeral`

### 3. UI Blocking on Runtime Calls

Memory search, export/import, and long-running ask flows can stall the render
loop if handled synchronously.

Mitigation:

1. run all backend I/O behind background tasks/watchers
2. surface in-flight state explicitly in the top bar or rail

### 4. Framework Churn

`go-tui` is pre-1.0 and may evolve.

Mitigation:

1. isolate it inside `internal/interfaces/tui`
2. keep transport and state adapters independent of view syntax

## Open Questions

1. Should the first shell foreground surface be bound directly to console
   sessions, or should it create/select a real agent first and project console
   semantics over that agent?
2. Should `Pin to next turn` inject raw snippets, summarized refs, or a bounded
   synthetic context block?
3. Should worker results flow back through rooms/orchestration only, or also be
   attachable directly into the foreground transcript?
4. Should the first release include agent memory policy editing from the shell,
   or keep that read-only at first?

## Recommended First Slice

The highest-signal first slice is:

1. foreground transcript + composer
2. assistant/session selector
3. memory rail with `Injected Now`, `Search`, and `Stats`
4. continuity summary card
5. workers list

That slice is enough to feel like a real coding shell with inspectable memory,
without requiring full orchestration parity on day one.
