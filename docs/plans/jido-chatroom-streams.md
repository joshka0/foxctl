# Jido Chatroom Streams Plan

Status: Draft  
Owner: Solo maintainer  
Last Updated: 2026-03-06

## Goal

Enable a chatroom pattern for Jido-backed agents that can be displayed in `gui-agent`
without forcing multi-participant coordination into the existing 1:1 companion
conversation model.

## Decision

Treat chatrooms as `agentctl` room-scoped streams, not as a new Jido runtime
primitive.

Jido remains responsible for:

1. Agent execution
2. Signal routing and fanout
3. Parent/child orchestration
4. Per-agent local thread state

`agentctl` becomes responsible for:

1. Durable room history
2. Room metadata and membership
3. Read/ack lifecycle
4. GUI read models and room projections

## Findings

### 1. Jido does not currently expose a first-class room abstraction

Observed primitives in the referenced repo:

1. Signal routing and dispatch
2. Parent/child communication
3. PubSub fanout
4. Per-agent append-only thread state

These are useful building blocks, but they do not add up to:

1. Durable shared room history
2. Shared room membership/state
3. Read state
4. GUI-ready room projection

### 2. `agentctl` already has a near-fit storage primitive

Board messages already carry:

1. `workspace_id`
2. `task_id`
3. `stream`
4. `sender`
5. `recipient`
6. `status`

This makes the board/message layer the best MVP substrate for room timelines.

### 3. Companion conversations are the wrong abstraction

Companion currently models:

1. One conversation ID
2. Optional agent link
3. Per-conversation memory

It already supports many conversations linked to one agent, but not one
conversation shared by many active agents. Reusing it for chatrooms would blur
private memory and shared coordination.

## Target Model

### Room identity

MVP:

1. A room is identified by `room_id`
2. Durable message grouping is stored as `stream = "room:<room_id>"`

Later:

1. Add a first-class `rooms` table for metadata
2. Add `room_members` or equivalent derived membership projection

### Message model

For room-scoped messages, persist:

1. `workspace_id`
2. `stream`
3. `sender`
4. `recipient`
5. `subject`
6. `body`
7. `kind`
8. `status`
9. `task_id` when room is attached to issue/run/task context

### Runtime model

Jido child spawns and room messages should carry `room_id` in payload/state
and metadata, not only in signal headers. The current bridge path reconstructs
runtime signals primarily from payload data, so payload ownership is the safer
contract.

## API Direction

### Phase 1: Reuse mailbox API for room timelines

Add or standardize:

1. `GET /api/mailbox?workspace_id=...&stream=room:<id>&all=true`
2. `PATCH /api/mailbox` for `read`, `surfaced`, `ack`

This gives the GUI enough to:

1. Fetch room timelines
2. Filter by room
3. Update room read state

### Phase 2: Dedicated room endpoints

Add:

1. `GET /api/rooms`
2. `POST /api/rooms`
3. `GET /api/rooms/:id`
4. `GET /api/rooms/:id/messages`
5. `PATCH /api/rooms/:id/members`

Room endpoints should return room-centric read models instead of generic board
message envelopes.

## GUI Direction

Do not force rooms into the existing `Companion` surface.

Preferred IA:

1. Keep `Companion` for 1:1 or agent-linked memory-rich chat
2. Add a dedicated `Rooms` surface or a room drawer inside `Runtime`
3. Show:
   - room title
   - participants
   - related issue/run/task refs
   - transcript
   - unread count
   - runtime participant status

## Phased Delivery

### Phase 1: Backend MVP

Scope:

1. Expose `stream` and `task_id` filters on mailbox reads
2. Expose message lifecycle updates through HTTP
3. Standardize room stream naming: `room:<room_id>`

Definition of done:

1. Room transcript can be read from HTTP using board messages
2. Messages can be marked `read`, `surfaced`, or `ack`

### Phase 2: Runtime propagation

Scope:

1. Pass `room_id` through Jido spawn metadata and initial state
2. Add room-aware signal payload conventions
3. Emit room-targeted coordination messages from runtime agents

Definition of done:

1. Spawned child agents inherit room context
2. Runtime-originated room messages can be correlated to one room timeline

### Phase 3: GUI room surface

Scope:

1. Add room list and room transcript panel
2. Join room transcript with runtime participant summaries
3. Support unread/read interactions

Definition of done:

1. Operator can open a room and see multi-agent coordination in one place

### Phase 4: First-class room storage

Scope:

1. Add explicit room metadata storage
2. Add room membership projection
3. Add room-specific APIs

Definition of done:

1. Rooms no longer rely only on implicit `stream` naming

## Risks

1. Mixing companion memory and shared room history will create ambiguous
   identity and privacy boundaries.
2. Reusing priority-sorted inbox reads for chat transcript UX may feel wrong.
   Room-specific reads should eventually support chronological ordering.
3. The current runtime bridge preserves payload data more reliably than richer
   signal envelope semantics. Room routing must not depend only on headers.

## First Patch

The first backend patch should stay intentionally small:

1. mailbox GET supports `stream` and `task_id`
2. mailbox PATCH supports `read`, `surfaced`, `ack`
3. add storage-level coverage for stream/task filtering

This unlocks room timeline exploration without committing to the full room
schema yet.

## Verification

1. `go test ./internal/storage/blackboard ./internal/domain/agent`
2. `go test ./internal/web/api`
3. `make check-doc-links`

Note:

`internal/web/api` currently has unrelated compile failures in the active
worktree. Phase 1 implementation should still add handler coverage, but the
package test run may stay blocked until that in-flight work is resolved.

## References

1. `internal/domain/agent/board_message.go`
2. `internal/storage/blackboard/board_store.go`
3. `internal/web/api/mailbox.go`
4. `packages/gui-agent/src/components/conversations/ConversationsList.tsx`
5. `packages/gui-agent/src/components/v2/RuntimeSummaryPanel.tsx`
6. `~/repos/githubs/jido/lib/jido/thread.ex`
7. `~/repos/githubs/jido/lib/jido/thread/plugin.ex`
8. `~/repos/githubs/jido/lib/jido/integrations/agentctl/plugin.ex`
