# ATCP room integrations roadmap

Status: planning

This document turns the room-integration direction into an implementation
roadmap. The current ATCP room surface is strong enough for PTY-backed live
smoke:

- rooms can fan out messages to mutable PTY members;
- sessions expose readiness, liveness, and rendered screen state;
- room messages return structured receipts;
- room message history is replayable;
- room event streams can fan in active session output.

The next step is to make rooms first-class integration hubs: durable room
events, native participants, integration delivery receipts, webhooks, inboxes,
and provider bridges.

## Goals

1. Make `room:<id>` a durable event bus that external integrations can follow
   with cursors.
2. Support non-PTY participants without pretending every room member has a
   terminal session.
3. Give bridges stable delivery receipts with provider/external IDs.
4. Keep terminal output opt-in for integrations because raw PTY output can be
   large or sensitive.
5. Preserve the current PTY fallback path for agents that only know how to
   read and write terminal text.

## Non-goals

- Do not make Slack, Discord, GitHub, or other providers special in the broker
  core.
- Do not make raw terminal output a default integration payload.
- Do not treat PTY-level liveness as semantic task completion.
- Do not give integrations terminal mutation authority unless explicitly
  configured.

## Phase 1 — Canonical room event log

**Status:** ☐ not started

Today, room stream fan-in is live and point-in-time. Message history is
replayable through a separate endpoint. The next step is a canonical
append-only room event log.

Event families:

- `room.member.joined`
- `room.member.left`
- `message.sent`
- `message.delivered`
- `message.failed`
- `terminal.ready`
- `terminal.activity`
- `terminal.output` (opt-in for external integrations)
- `terminal.screen.snapshot` (opt-in for external integrations)
- `integration.delivered`
- `integration.failed`

API target:

```text
GET /v1/events?target=room:{id}&since={seq}&kinds=message.sent,terminal.ready
```

Acceptance:

- ☐ Room events are stored append-only with monotonic room-local sequence.
- ☐ `GET /v1/events?target=room:<id>&since=N` replays retained/persisted
  events and then follows live events.
- ☐ Existing `message.send` appends `message.sent` and `message.delivered`
  events.
- ☐ Existing room member join/leave appends member lifecycle events.
- ☐ Event filters by `kind` work without substring heuristics.
- ☐ Existing point-in-time room terminal fan-in can be reimplemented on top of
  the room event log or kept as a compatibility path with clear docs.

Implementation notes:

- Use structured event kinds and explicit filters, not keyword routing.
- Persist room event records when `atcpd --data-dir` is configured.
- Return large event bodies through CAS/artifact pointers if the event exceeds
  the existing large-output threshold.

## Phase 2 — Native room participants

**Status:** ☐ not started

Room members need a participant model that is broader than
`agent_id + session_id`.

Candidate member shape:

```json
{
  "room_id": "01HX...",
  "agent_id": "slack:team-channel",
  "participant_kind": "integration",
  "session_id": "",
  "inbox_id": "inbox:01HX...",
  "integration_id": "integration:slack:ops",
  "can_mutate": false,
  "capabilities": {
    "receives": ["message.sent", "terminal.ready"],
    "emits": ["message.send"]
  }
}
```

Participant kinds:

- `pty-session`
- `inbox`
- `webhook`
- `integration`
- `observer`
- `scheduler`
- `human`

Acceptance:

- ☐ A room member can be active without a `session_id`.
- ☐ Router delivery can target inbox/integration members without terminal
  injection.
- ☐ `CanMutate` remains the terminal-input authority boundary.
- ☐ Integration participants cannot mutate PTYs unless explicitly granted.
- ☐ Room member listing exposes participant kind and capabilities.

## Phase 3 — Delivery receipts with provider metadata

**Status:** ☐ not started

Current receipts identify ATCP message context. Integration receipts should
also carry provider delivery metadata.

Candidate member result:

```json
{
  "agent_id": "slack:#ops",
  "delivery": "webhook",
  "delivered": true,
  "provider": "slack",
  "external_id": "1712345678.9012",
  "attempt": 1
}
```

Failure fields:

- `error_kind`
- `error_code`
- `error`
- `retry_after_ms`
- `attempt`

Acceptance:

- ☐ `POST /v1/messages` member results include delivery mode.
- ☐ Integration delivery results can include provider and external ID.
- ☐ Failed integration delivery reports retryability without parsing strings.
- ☐ Message history replay includes provider delivery metadata.

## Phase 4 — Integration adapter boundary

**Status:** ☐ not started

Provider-specific code should live behind an adapter boundary.

Candidate Go interface:

```go
type RoomIntegration interface {
    ID() string
    Capabilities() IntegrationCapabilities
    Deliver(ctx context.Context, event RoomEvent) (DeliveryResult, error)
    Run(ctx context.Context, sink RoomEventSink) error
}
```

Initial adapters:

- `webhook`
- `stdio-jsonl`
- `inbox`

Later adapters:

- Slack
- Discord
- GitHub issue/PR comments
- Obsidian log
- MCP bridge

Acceptance:

- ☐ Broker core depends on an integration interface, not provider packages.
- ☐ Adapter delivery is bounded by context and retry policy.
- ☐ Adapter input events go through the same room event append path as native
  room messages.

## Phase 5 — Room webhooks

**Status:** ☐ not started

Webhooks are the simplest external bridge and should land before
provider-specific integrations.

API sketch:

```text
POST /v1/rooms/{id}/webhooks
GET /v1/rooms/{id}/webhooks
DELETE /v1/rooms/{id}/webhooks/{webhook_id}
```

Create body:

```json
{
  "url": "https://example.com/atcp",
  "events": ["message.sent", "message.delivered", "terminal.ready"],
  "secret_ref": "room-webhook-secret",
  "delivery_policy": {
    "retry": "exponential",
    "max_attempts": 5
  }
}
```

Suggested headers:

```text
X-ATCP-Room: 01HX...
X-ATCP-Event: 01HX...
X-ATCP-Signature: sha256=...
```

Acceptance:

- ☐ Webhooks subscribe to explicit event kinds.
- ☐ Webhook requests are signed.
- ☐ Delivery retries are bounded and recorded.
- ☐ Raw terminal output is excluded unless explicitly requested.

## Phase 6 — Durable inbox delivery

**Status:** ☐ not started

Inboxes let non-PTY agents and polling integrations participate without a live
SSE connection.

API sketch:

```text
GET /v1/inboxes/{id}/messages?ack=false
POST /v1/inboxes/{id}/ack
```

Acceptance:

- ☐ Room members can receive messages by inbox.
- ☐ Inbox messages are durable when storage is configured.
- ☐ Acknowledgement is explicit and idempotent.
- ☐ Inbox delivery appears in `POST /v1/messages` member results.

## Phase 7 — Threading model

**Status:** ☐ not started

External platforms have thread concepts. ATCP rooms need enough structure to
map to them cleanly.

Fields:

- `thread_id`
- `parent_message_id`
- `reply_to_message_id`
- `topic`
- `external_thread_id`

Acceptance:

- ☐ Message send accepts explicit thread fields.
- ☐ Message history and room events preserve thread fields.
- ☐ Provider adapters can map ATCP thread IDs to external thread IDs.

## Phase 8 — Event filters

**Status:** ☐ not started

Room streams and integrations need explicit filters so observers can avoid
raw output floods.

Filter examples:

```text
GET /v1/events?target=room:ID&kinds=message.sent,terminal.ready
GET /v1/events?target=room:ID&agents=codex,droid
```

Acceptance:

- ☐ `kinds` filter is parsed as a structured list.
- ☐ `agents` filter is parsed as a structured list.
- ☐ Unknown event kinds are rejected with a typed error.
- ☐ Filter behavior is covered by tests.

## Safe defaults

Default integration event set:

- `message.sent`
- `message.delivered`
- `message.failed`
- `terminal.ready`
- `terminal.activity`

Explicit opt-in only:

- `terminal.output`
- `terminal.screen.snapshot`

Reason: raw PTY output can contain secrets, huge payloads, or transient TUI
state that is not appropriate for external systems.

## Suggested implementation order

1. Canonical room event log with replay/follow.
2. `message.sent` / `message.delivered` event emission.
3. Event filters (`kinds`, then `agents`).
4. Native inbox-only participants.
5. Durable inbox delivery and ack.
6. Webhook integration adapter.
7. Provider-specific adapters after webhook/inbox contracts settle.

## Open questions

- Should room event sequence be room-local only, or also have a broker-global
  sequence for cross-room observers?
- What retention policy should volatile room event logs use when no SQLite
  store is configured?
- Should webhooks receive full ATCP envelopes or a provider-friendly wrapper
  around envelopes?
- How should secrets be referenced for local-only webhook signing?
- Which integration should be the first provider-specific proof point after
  generic webhooks?
