# Room Runtime Delivery Contract

## Goal

Give the room system one explicit runtime contract for delivery, loop policy, and
participant routing so future refactors are measured against clear invariants
 instead of ad hoc relay behavior.

This document is intentionally narrower than the full room feature. It focuses
on the live-delivery/runtime seams that currently make the room loop feel
unstable.

This contract sits underneath the broader architectural direction in
[room-headless-kernel-and-clients.md](./room-headless-kernel-and-clients.md):

- `agentctl` is the headless room/runtime kernel
- CLI, web, GUI, mux, and chat adapters are clients or presentation layers
- runtime behavior should converge into shared services rather than CLI-shaped
  implementations

## Core Invariants

### 1. Durable room state is canonical

Room messages, reminders, task transitions, milestone/story state, and
coordinator decisions are durable room artifacts first.

Live relay is downstream from persistence. A room must remain usable even when
no live pane or participant runtime is attached.

### 2. Participant transport and presentation are different layers

For any participant, keep these distinct:

- membership: are they in the room, and with what role
- transport/runtime: can the system trigger work for them right now
- presentation: is there a tmux/zellij/gui PTY currently attached for humans

Do not use presentation attachment as the source of truth for delivery when a
real transport binding already exists.

### 3. A primary transport failure is not automatically authoritative

If the primary participant route fails but the participant still has a valid
fallback transport, relay must be allowed to fall through.

This is the minimum correctness rule for mixed transport states like:

- stale pane socket + live tmux pane
- stale pane socket + live zellij binding

### 4. Operational status must reflect persisted loop truth

Room status and loop inspection APIs must return the actual persisted
`coordination.RoomLoop` row, not synthetic defaults, whenever a row exists.

Defaults are only for rooms that truly have no persisted loop state yet.

## Known Structural Gaps

These are real design issues, but they are not all part of the first patch.

### Workspace identity is still split

The CLI currently persists room state against an absolute workspace path, while
the web API accepts a raw `workspace_id` string that may be either a logical ID
or a path-like value.

This needs a separate migration because existing board-store and API tests rely
on stable logical IDs like `ws1`, while current CLI room operations use
absolute paths directly.

The fix for that should introduce a clear contract between:

- `workspace_id`: stable logical identifier
- `workspace_root`: filesystem root when present
- `workspace_source`: how the identity was derived

### Live delivery still has multiple owners

Today, the API can trigger live relay immediately after persisting a room
message, and the room loop can later relay the same persisted message again.

That must converge to one room-level delivery owner. This document keeps the
first implementation slice smaller, but the target state is singular ownership,
not an indefinitely preserved dual path.

### Loop behavioral memory is still in-memory

The room loop persists policy and heartbeat through `coordination.RoomLoop`, but
reminder suppression and escalation counters remain process-local.

That needs a later persistence pass once delivery ownership and workspace
identity are stable enough.

## First Implementation Slice

This branch should only claim the following runtime corrections:

1. room status returns the persisted loop row instead of a synthetic default
2. primary participant failures no longer suppress viable legacy fallback
3. primary participant skips no longer suppress viable legacy fallback

It does **not** yet claim:

- one room, one live-delivery owner
- canonical workspace-ID migration
- durable pulse/reminder suppression state
- chain-aware reply tracking

## Follow-on Phases

### Phase 1: workspace identity unification

Unify CLI and API room storage around one canonical workspace identity model
with explicit separation between logical ID and filesystem root.

### Phase 2: single live-delivery owner

Move live delivery to one authoritative room runtime and remove the API
subprocess bridge to `room relay-once`.

### Phase 3: canonical participant delivery binding

Promote participant transport bindings to the single routing substrate, with
explicit fallback policy instead of heuristic mux-name inference.

### Phase 4: durable loop operational state

Persist reminder counts, suppression windows, escalation markers, and delivery
cursors so loop restart does not reset behavior.
