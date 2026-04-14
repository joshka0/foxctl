# gui-agent Room Control Center

Status: proposed  
Audience: gui-agent frontend, web/API backend, room/coordinator runtime owners  
Scope: `gui-agent` room-management panel for coordinator-driven multi-agent collaboration

## Why This Exists

`foxctl room` now has the backend primitives needed for durable multi-agent coordination:

- direct and broadcast room messages
- `ack` and coordinator `resolve`
- `status`, `inbox`, and `task` coordination primitives
- coordinator handoff via `room coordinator set`
- reminder pulses and chain-aware reminder cleanup
- assignee-aware task ownership, reclaim, reassign, and heartbeat refresh

Those primitives currently live mostly in the CLI and are only partially exposed in the web/UI layer. `gui-agent` already has multiple room-related surfaces, but they are fragmented across:

- `RoomsView`
- `RuntimeRoomPanel`
- agent list/detail room shortcuts

The result is that room management is technically possible but operationally noisy. This spec consolidates room operations into one operator-first surface.

## Product Goal

Create a dedicated `gui-agent` room-management panel that lets a coordinator:

1. see room health at a glance
2. process direct obligations quickly
3. manage task flow without dropping into the CLI
4. hand off coordinator responsibility explicitly
5. keep the room loop active and correctly tuned without losing auditability

## Canonical Position

`gui-agent` is a client of the headless `foxctl` room/runtime kernel.

It must not depend on CLI-shaped implementation details or mux-shaped runtime
truth. Its room surfaces should be built against shared room services and
persisted/evented room state.

## Success Criteria

The first production-quality panel should let an operator do all of the following without leaving `gui-agent`:

- identify the current coordinator
- hand off coordinator role to another participant
- inspect direct obligations (`ack`, `reply`, `blocked`, `stale`)
- claim, block, unblock, touch, complete, assign, reassign, and reclaim room tasks
- acknowledge or resolve stale room requests
- send direct messages, including messages to `@coordinator`
- inspect a compact audit timeline of room mutations
- configure room reminder policy through a server-managed room loop surface

## Non-Goals

This spec does not cover:

- replacing the raw room timeline with a full chat client
- exposing arbitrary terminal transport controls in the panel
- making zellij parity a prerequisite for the first panel
- adding free-form AI summarization to the room control plane
- automatic agent delegation logic beyond current coordinator/task primitives

## Product Principles

### 1. Obligation-Driven, Not Log-Driven

The main panel should foreground unresolved obligations:

- awaiting ack
- awaiting reply
- blocked tasks
- stale tasks
- assigned-but-unclaimed tasks

Informational broadcasts belong in the timeline, not the inbox.

### 2. Coordinator Actions Must Be Explicit

Coordinator-only actions must be visually distinct and guarded:

- hand off coordinator
- reassign task
- reclaim task
- bulk resolve reminder chains
- tune reminder policy

Disruptive actions require confirm dialogs and produce visible audit entries.

### 3. Timeline Remains the Audit Surface

Every mutation remains durably visible in the room timeline. The panel may summarize, collapse, or filter, but must not create hidden state.

### 4. Server-Managed Loop, Not Manual Shell Reliance

The GUI must not depend on a manually launched CLI `room loop` process. Pulse/reminder controls require a server-managed room loop policy and runtime state surface.

### 5. Compact by Default, Detailed on Demand

The default operator view should be concise. Rich payloads should appear only in expanded cards, drawers, or verbose detail states.

Completed tasks may be hidden from the task board by UI preference, but never removed or hidden from the durable audit timeline by backend default.

## Current Reality

### Existing frontend surfaces

- `packages/gui-agent/src/components/rooms/RoomsView.tsx`
- `packages/gui-agent/src/components/rooms/RuntimeRoomPanel.tsx`
- room affordances in agent list/detail views

### Existing HTTP/API room support

Already available over the web API:

- room list
- room create/patch
- room member patch
- room detail
- room message list
- room message send

Missing from the web/API layer but already present in CLI/runtime semantics:

- room status
- room inbox
- room ack
- room resolve
- room coordinator set
- room task operations
- room loop runtime/policy inspection and mutation

## Primary User Stories

### Coordinator

- I can see what needs action now without scanning the full timeline.
- I can hand off coordinator role explicitly and durably.
- I can reassign or reclaim stuck work with auditable reasons.
- I can adjust reminder cadence without disabling the room’s safety guarantees.

### Participant

- I can see only the obligations relevant to me.
- I can claim work, mark it blocked, update heartbeat, and complete it quickly.
- I can message the coordinator without knowing the coordinator actor id.

### Reviewer / Operator

- I can inspect the room timeline to understand why a coordinator action happened.
- I can trust that actions in the panel map to durable room/task mutations, not local-only UI state.

## Information Architecture

The Room Control Center should be a dedicated room surface inside `gui-agent`, not a secondary widget.

### Layout

#### 1. Room Header

Always visible.

Displays:

- room name
- room policy
- workspace
- your role
- current coordinator
- room loop status

Actions:

- `Send Message`
- `Transfer Lead`
- `Open Timeline`

#### 2. Action Ribbon

Always visible, immediately under the header.

Displays counts for:

- `ready to claim`
- `awaiting ack`
- `awaiting reply`
- `assigned`
- `blocked`
- `stale`

Behavior:

- click a chip to filter the panel to that obligation lane
- counts should reflect collapsed unresolved chains, not historical backlog totals

#### 3. Task Board

Primary middle surface.

Columns:

- `pending`
- `claimed`
- `blocked`
- `done`

Card fields:

- title
- owner / assignee
- age
- priority
- stale indicator
- stale duration
- last heartbeat / updated time
- nudge count / last nudged at

Inline actions:

- participant: `claim`, `touch`, `block`, `unblock`, `complete`, `abandon`
- coordinator: `assign`, `reassign`, `reclaim`
- `nudge` with scoping rules defined below

Disruptive actions:

- `reassign`
- `reclaim`
- `transfer lead`

must use confirmation dialogs.

#### 4. Inbox

Obligation-first, actor-scoped by default.

Segments:

- `Direct`
- `Ack Required`
- `Reply Expected`
- `Alerts`

Row fields:

- sender
- recipient
- subject
- age
- obligation flags
- compact preview

Inline actions:

- `Ack`
- `Resolve`
- `Reply`
- `Nudge`

Behavior:

- resolved or acked items disappear from the default actionable view
- info broadcasts do not appear in the inbox

#### 5. Timeline

Collapsed by default.

Purpose:

- append-only audit trail
- coordinator action trace
- room mutation debugging

UI controls:

- quick filters by event kind, at minimum:
  - `messages`
  - `handoffs`
  - `reclaims`
  - `reassignments`
  - `reminders`
  - `all`

Should include:

- direct messages
- task state transitions
- coordinator handoff
- reminder events
- resolution events

## Key UX Interactions

### Coordinator Handoff

Flow:

1. coordinator clicks `Transfer Lead`
2. participant picker opens
3. optional transition note entry
4. confirm dialog
5. backend executes coordinator handoff
6. timeline records a durable `lead_change` event
7. crown/role indicator moves immediately

Phase 1 decision:

- handoff is immediate after confirmation by the current coordinator
- a secondary `proposed -> accepted` lead-transfer handshake is deferred
- reason: phase-1 must not create a coordinator vacancy or a half-transferred room state

### Direct Message to Coordinator

UI action:

- message composer includes `To: Coordinator`

Behavior:

- frontend resolves this as alias semantics, not hardcoded actor ids
- backend supports coordinator alias directly

### Reminder / Nudge

Two modes:

- manual `Nudge`
- automatic room loop pulse

Manual nudge:

- available from inbox rows and task cards
- always records a timeline event
- scoping:
  - coordinator may nudge any unresolved room obligation
  - non-coordinator participants may only nudge:
    - tasks they currently own
    - direct requests addressed to themselves
    - coordinator via `@coordinator`

Automatic pulse:

- controlled from room loop policy
- panel may expose intensity controls, but backend must enforce a minimum pulse floor

Phase 1 decision:

- no explicit participant `deep work` state is added yet
- reason: it weakens reminder semantics before the room loop policy surface is fully server-managed

### Task Progress Update

The panel should treat `room task touch` as a first-class inline “progress update” control:

- quick touch action
- optional note box in future slice
- suppresses stale-task reminders without forcing a status change

## Backend / API Contract

The panel should not shell out to CLI commands. It needs HTTP-level read models and mutation endpoints.

### Existing API to Reuse

- `GET /api/rooms`
- `POST /api/rooms`
- `GET /api/rooms/:id`
- `PATCH /api/rooms/:id`
- `PATCH /api/rooms/:id/members`
- `GET /api/rooms/:id/messages`
- `POST /api/rooms/:id/messages`

### New Read Models Required

#### `GET /api/rooms/:id/status`

Returns the coordinator-facing compact status model currently exposed by `foxctl room status`.

Parameters:

- `workspace_id`
- `actor_id`
- `stale_after`
- `only=ack,reply,assigned,blocked,stale,all`
- `verbose`
- `limit`

Should include:

- room summary
- participants
- task pulse
- action-required summary
- top entries / top tasks

#### `GET /api/rooms/:id/inbox`

Returns actor-scoped actionable room entries.

Parameters:

- `workspace_id`
- `actor_id`
- `only=direct,ack,reply,alerts,all`
- `limit`

Should return already-collapsed actionable entries, not raw historical room messages.

Collapse contract:

- entries collapse by unresolved chain root, not by raw message id
- a chain root is:
  - the original direct request id, or
  - `related_message_id` when present
- reminder follow-ups for the same chain collapse to one actionable inbox entry
- the rendered entry is the latest unresolved message in the chain
- chain counts in `room status` and inbox summaries are chain counts, not historical message counts

#### `GET /api/rooms/:id/tasks`

Returns room-scoped tasks in a UI-friendly board model.

Parameters:

- `workspace_id`
- `stale_after`
- `include_completed`

Should include:

- assignment metadata
- ownership metadata
- block metadata
- heartbeat metadata
- stale classification
- reclaim audit metadata:
  - `previous_owner`
  - `reclaimed_by`
  - `reclaimed_at`
  - `reclaim_reason`
  - `stale_duration`
- reassign audit metadata:
  - `reassigned_by`
  - `reassigned_at`
  - `reassign_reason`
  - `previous_assignee`

#### `GET /api/rooms/:id/loop`

Returns room loop runtime and policy state.

Should include:

- `enabled`
- `managed_by`
- `last_tick_at`
- `pulse_interval`
- `reply_stale_after`
- `task_stale_after`
- `min_pulse_floor`
- `coordinator_pulse_enabled`

Read access:

- readable by any current room member
- editable only by the coordinator

Default policy values:

- `pulse_interval`: `30m`
- `reply_stale_after`: `2h`
- `task_stale_after`: `4h`
- `coordinator_pulse_enabled`: `true`
- `min_pulse_floor`: one unresolved reminder pulse per chain per 24h and one coordinator pulse per 24h while unresolved work remains

### New Mutation Endpoints Required

All mutation endpoints must document role requirements explicitly.

#### Message actions

- `POST /api/rooms/:id/messages/:message_id/ack`
  - role: addressed participant or coordinator
- `POST /api/rooms/:id/messages/:message_id/resolve`
  - role: coordinator
- `POST /api/rooms/:id/messages/resolve`
  - bulk mode
  - filter support (`ack`, `reply`, `direct`, `all`)
  - role: coordinator
  - each resolved message must emit its own audit event; a single aggregate “resolved N messages” event is insufficient

#### Coordinator actions

- `POST /api/rooms/:id/coordinator`
  - role: current coordinator
  - transfer to another participant
  - optional transition note
  - emits `lead_change`

#### Task actions

- `POST /api/rooms/:id/tasks`
  - role: room member
- `POST /api/rooms/:id/tasks/:task_id/claim`
  - role: assignee or unassigned room member, subject to assignment rules
- `POST /api/rooms/:id/tasks/:task_id/touch`
  - role: current owner
- `POST /api/rooms/:id/tasks/:task_id/block`
  - role: current owner
- `POST /api/rooms/:id/tasks/:task_id/unblock`
  - role: current owner
- `POST /api/rooms/:id/tasks/:task_id/complete`
  - role: current owner
- `POST /api/rooms/:id/tasks/:task_id/abandon`
  - role: current owner
- `POST /api/rooms/:id/tasks/:task_id/assign`
  - role: coordinator
- `POST /api/rooms/:id/tasks/:task_id/reassign`
  - role: coordinator
- `POST /api/rooms/:id/tasks/:task_id/reclaim`
  - role: coordinator

#### Loop policy actions

- `PATCH /api/rooms/:id/loop`
  - role: coordinator

Mutable fields:

- `pulse_interval`
- `reply_stale_after`
- `task_stale_after`
- `coordinator_pulse_enabled`

Constraints:

- backend must enforce a minimum reminder floor
- “disable everything forever” is not allowed
- user-configurable reminder policy may not suppress the non-configurable floor

## Event and Audit Model

The timeline must show all material mutations. Existing room messages are sufficient for many of these, but the panel spec should treat the following as first-class audit events:

- direct room message
- room ack
- room resolve
- task claimed
- task blocked
- task unblocked
- task completed
- task reassigned
- task reclaimed
- reminder pulse
- `lead_change`

Unauthorized attempts must also be audited, but not as normal room timeline chatter.

### Unauthorized action handling

- unauthorized coordinator-only mutations return `403`
- they emit a structured audit/security event
- they do not create a normal room timeline entry by default
- coordinator-facing admin/event surfaces may choose to render these events separately

### `lead_change`

Add a distinct event kind for coordinator transfer rather than treating it as a generic info message or a projection over member mutation.

This event must be stored durably and immutably as a first-class event, not derived from a later room-member snapshot.

Minimum fields:

- room id
- previous coordinator
- new coordinator
- actor who initiated the transfer
- optional note
- created at

## Phase-1 Acceptance Decisions

The following review outcomes are locked for the first implementation slice:

- accepted:
  - `lead_change` is a first-class immutable audit event
  - room/task read models expose explicit reclaim/reassign audit fields
  - bulk resolve produces per-message audit events
  - reminder-chain collapse is defined at the API contract level
  - loop policy has explicit defaults and a server-enforced pulse floor
  - `ready to claim` appears in the action ribbon
  - task cards surface nudge history
  - reclaim audit includes `stale_duration`
  - timeline supports event-kind quick filters for coordinator audit speed
- deferred:
  - two-step `proposed -> accepted` lead handoff
  - participant `deep work` reminder suppression state

Deferred items may be revisited once the panel and server-managed loop policy are stable.

## Frontend State and Data Model

### Recommendation

Create a dedicated room control store rather than spreading this panel across:

- `viewStore`
- existing room queries
- ad hoc component-local state

Suggested store: `roomControlStore`

Responsibilities:

- selected room
- selected filters
- active obligation lane
- loop policy draft state
- coordinator action dialogs
- optimistic mutation state

### Query boundaries

Use server-shaped read models instead of reconstructing coordinator semantics from raw room message lists in the browser.

This is especially important for:

- collapsed reminder chains
- stale task classification
- top actionable entries
- role-gated actions

## Placement in gui-agent

### First choice

Make this the main room-management surface under the existing Rooms navigation path.

### Follow-up cleanup

Once the Room Control Center exists:

- `RuntimeRoomPanel` should degrade to a shortcut/entry point only
- agent detail room controls should link into the dedicated panel instead of duplicating room management semantics
- `RoomsView` should be simplified into:
  - room directory
  - room create/edit
  - room control center entry

This resolves the fragmentation already called out in `docs/plans/gui-agent-improvement-roadmap.md`.

## Implementation Phases

### Phase 1 — Backend Read Models

- expose HTTP endpoints for room status, inbox, tasks
- expose coordinator handoff and resolve/ack endpoints
- expose room loop runtime/policy endpoints
- define `lead_change` event

### Phase 2 — Panel Skeleton

- create dedicated room control center route/view
- implement header, action ribbon, task board shell, inbox shell, timeline drawer
- wire read-only room status/inbox/tasks

### Phase 3 — Mutations

- ack / resolve
- claim / touch / block / unblock / complete
- assign / reassign / reclaim
- coordinator transfer

### Phase 4 — Loop Controls

- server-managed loop policy editing
- pulse cadence controls
- minimum floor enforcement surfaced in UI

### Phase 5 — UX Polish

- optimistic updates
- keyboard shortcuts
- richer timeline filters
- completed-task auto-hide controls

## Suggested Work Split

### Backend logic

- implement room HTTP read models and action endpoints
- define `lead_change` event contract
- define room loop policy persistence/runtime ownership

### UI implementation

- build panel layout and interaction states in `gui-agent`
- integrate action ribbon, task board, inbox, timeline
- ensure destructive actions use confirm flows

### Review / QA

- protocol and audit trail review
- role-gating review
- reminder floor review
- regression coverage over both CLI and HTTP layers

## Testing Strategy

### Backend

- room API handler tests
- chain-aware resolve tests
- coordinator handoff tests
- loop policy validation tests
- `lead_change` event tests

### Frontend

- component interaction tests
- role-gating UI tests
- optimistic mutation tests
- filter/view-model rendering tests

### End-to-end

- create room
- assign coordinator
- send direct request
- ack / resolve from panel
- claim/block/touch/complete task from panel
- reassign and reclaim from panel
- transfer coordinator
- verify timeline reflects all of the above

## Open Questions

1. Should loop policy be persisted in the room record, or in a separate room runtime config object?
2. Should completed tasks auto-hide by backend default or UI preference only?
3. Should manual `Nudge` create a separate explicit message kind, or just a structured alert message?

## Immediate Next Step

Use this spec as the split point:

- backend work can start on HTTP read models + mutation endpoints
- UI work can start on panel layout and state model
- review can focus on auditability, role-gating, and operational safety
