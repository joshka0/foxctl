# ATCP v0.1 — Full Replacement of Rooms Protocol

Status: draft
Owner: joshka
Source spec: [../atcp/ATCP-v0.1.md](../atcp/ATCP-v0.1.md)
Draft types: [../atcp/types.go](../atcp/types.go)

## 1. Goals

Replace the current rooms/BoardMessage coordination protocol with ATCP v0.1 as the single canonical contract for:

- Session + PTY ownership
- Typed terminal input (no raw byte API as primary path)
- Room and inbox message delivery with delivery policies
- Reminder scheduling and durable routing
- Transactions with observed completion

This is a **hard cut** per the `hard-cut` skill: one canonical contract, no compatibility shim, no dual-writer. The existing `BoardMessage`, `roomruntime.SendMessage`, tmux/zellij direct injection, and ad-hoc reminder paths are replaced.

Priority slices (per scoping decisions):

1. Terminal input reliability (typed intents, leases, bracketed-paste awareness, adapter profiles)
2. Reminders + scheduling (durable scheduler, idempotency, delivery routing)
3. Rooms + inboxes (message.send, delivery policies)
4. Transactions (wait conditions, observed completion)

## 2. Non-goals

- Not a terminal multiplexer UI (no panes/layouts; tmux/zellij remain optional viewers only)
- Not a remote protocol in v0.1 (local Unix socket only; TCP deferred)
- Not a guarantee that arbitrary opaque CLIs become reliable APIs

## 3. Decisions (from scoping)

| Decision | Choice |
|---|---|
| Migration strategy | Full replacement (hard cut) |
| Broker location | Integrated subsystem inside existing foxctl agent daemon |
| Transport | Unix socket + HTTP/1.1 JSON + SSE (defaults from spec §4) |
| Envelope | ATCP envelope (`v`, `id`, `kind`, `ts`, `source`, `target`, `seq`, `correlation_id`, `idempotency_key`, `body`) |
| Existing `envelope.Envelope` | Retained for command/response skill protocol; ATCP envelope is a distinct wire type for coordination events |

Rationale for keeping the two envelopes distinct:

- `internal/domain/envelope` is the **command/response** contract for skills and CLI commands (request → single result).
- ATCP is an **event-stream** contract for sessions, rooms, inboxes, reminders (continuous bidirectional flow).
- Forcing one into the other produces awkward overloading. Both remain canonical in their own lane.

## 4. Package Layout

New canonical packages:

```
internal/atcp/
  envelope/           # ATCP envelope type, encode/decode, validation
  addressing/         # URI-like target parsing (room:, session:, agent:, inbox:, scheduler:)
  kinds/              # Typed kind constants + body schemas per kind
  broker/             # Broker core: session registry, event log, lease manager, router
    session/          # PTY session lifecycle, output log, screen model
    lease/            # Input lease acquire/release, TTL, preempt
    router/           # Deliver message.send per delivery policy
    scheduler/        # Reminder schedule/fire, idempotency keys
    modetrack/        # Terminal mode parsing (bracketed paste, alt screen, etc.)
  adapter/            # Adapter profiles
    generictty/
    posixshell/
    nodereadline/
    tmuxcompat/       # Legacy tmux-compat only
    native/           # agentctl-native side-channel (AGENTCTL_CONTROL_SOCK env)
  transport/
    httpjson/         # HTTP/1.1 JSON handlers
    sse/              # Event SSE streams
    unixsocket/       # Unix socket listener wiring into daemon
  store/              # Persistence for sessions, events, reminders, messages
```

Removed / replaced:

- `internal/runtime/orchestration/roomruntime/send.go` → folded into `internal/atcp/broker/router`
- `internal/runtime/terminal/tmuxbridge/` → becomes `internal/atcp/adapter/tmuxcompat` (opt-in only)
- `internal/runtime/terminal/zellijbridge/` → same
- `internal/storage/blackboard` BoardMessage schema → replaced by `internal/atcp/store` event/message tables
- `internal/runtime/terminal/agentpane/room_*` → replaced by ATCP room bindings

CLI:

- `cmd/foxctl/cmd/room*.go` commands are rewritten to emit ATCP intents via the daemon socket. Command names follow spec §19:
  - `foxctl session create|stream|delete`
  - `foxctl term submit|key|text|paste`
  - `foxctl msg send`
  - `foxctl remind add|list|cancel`
  - `foxctl inbox read|ack`
  - `foxctl room create|join|leave|list` (thin wrappers around message bus bindings)
  - `foxctl broker events --since <seq>`

## 5. Data Model

### 5.1 Envelope

Mirror `docs/atcp/types.go` verbatim in `internal/atcp/envelope`:

```go
type Envelope struct {
  Version        string    `json:"v"`         // "atcp/0.1"
  ID             string    `json:"id"`        // ULID, globally unique
  Kind           string    `json:"kind"`
  Timestamp      time.Time `json:"ts"`
  Source         string    `json:"source,omitempty"`
  Target         string    `json:"target,omitempty"`
  Seq            uint64    `json:"seq,omitempty"`
  CorrelationID  string    `json:"correlation_id,omitempty"`
  IdempotencyKey string    `json:"idempotency_key,omitempty"`
  Body           any       `json:"body"`
}
```

Validation rules (broker-side):

- `v` must equal `atcp/0.1`
- `id` must be ULID
- `kind` must be a registered kind
- `ts` must be UTC RFC3339
- `target` required for intents that mutate state

### 5.2 Storage tables

SQLite (reuse existing daemon DB pool):

- `atcp_sessions(session_id, pid, cmd_json, cwd, env_json, adapter, bindings_json, created_at, exited_at, exit_code)`
- `atcp_events(seq INTEGER PK, session_id, target, kind, envelope_json, ts)` — append-only event log per §8
- `atcp_output_log(session_id, seq, bytes BLOB, ts)` — PTY output bytes, chunked
- `atcp_leases(lease_id, session_id, scope, owner, acquired_at, ttl_ms, released_at)`
- `atcp_messages(message_id, source, target, topic, priority, content_json, delivery_json, created_at, acknowledged_at)`
- `atcp_reminders(reminder_id, source, target, fire_at, content_json, delivery_json, idempotency_key, fired_at, status)`
- `atcp_room_bindings(room_id, session_id, inbox_id, actor_id, transport, endpoint)`

All writes are append-only or idempotent-by-key; `idempotency_key` enforced with a UNIQUE index on reminders and messages.

## 6. Broker Subsystem (inside foxctl daemon)

Broker is composed into `internal/agent/daemon/daemon.go` as a new subsystem with its own goroutines:

```
Daemon
 └─ atcp.Broker
     ├─ SessionManager   (PTY lifecycle, output log)
     ├─ LeaseManager     (serialize terminal.input producers)
     ├─ EventLog         (append-only, SSE fan-out)
     ├─ Router           (message.send delivery)
     ├─ Scheduler        (reminder.fire, durable timers)
     ├─ ModeTracker      (bracketed-paste, alt-screen)
     └─ AdapterRegistry  (resolve adapter profile per session)
```

Component ownership rules (Go-native runtime rules from AGENTS.md):

- Every subsystem is a `Run(ctx)` component with bounded channels
- Session state is single-writer (owned by the session goroutine); readers get immutable snapshots
- Event log is append-only with an atomic counter for `seq`

### 6.1 HTTP endpoints

Served on the daemon Unix socket (path: `$XDG_RUNTIME_DIR/foxctl/atcp.sock`, mode `0600`):

```
POST   /v1/sessions                         # create
DELETE /v1/sessions/{session_id}            # delete
GET    /v1/sessions                         # list
GET    /v1/sessions/{session_id}            # info
GET    /v1/events?target=...&since=<seq>    # SSE stream
POST   /v1/terminal/{kind}                  # text|key|submit|paste|write_bytes
POST   /v1/leases/acquire
POST   /v1/leases/release
POST   /v1/transactions/run
POST   /v1/messages/send
GET    /v1/inboxes/{inbox_id}
POST   /v1/inboxes/{inbox_id}/ack
POST   /v1/reminders/schedule
DELETE /v1/reminders/{reminder_id}
GET    /v1/reminders
POST   /v1/capabilities/report              # adapter-reported caps
```

## 7. Phased Implementation

Phase order tracks spec §20 but reprioritizes Phase 5 ahead of Phase 3/4 per scoping.

### Phase 1 — PTY broker foundation (highest priority)

Deliverables:

- `internal/atcp/envelope` + `addressing` + `kinds` (types, validation, codec)
- `internal/atcp/broker/session` (PTY create/delete, output log, screen model stub)
- `internal/atcp/broker/lease` (acquire/release, TTL, preempt policy)
- `internal/atcp/adapter/generictty` compiling `text`, `key`, `submit`, `paste` to bytes
- `internal/atcp/transport/httpjson` + `unixsocket` with session + terminal endpoints
- Daemon wiring in `internal/agent/daemon/daemon.go`
- CLI: `foxctl session create|delete|stream`, `foxctl term submit|key|text|paste`

Exit criteria:

- Can create a session, submit text, observe output via SSE
- Two concurrent producers cannot both mutate input (lease enforced)
- `Enter` compiles to `0x0D`, `LineFeed` to `0x0A`, not interchangeable

Tests:

- Unit: key compilation table matches spec §9.3
- Unit: lease acquire/release concurrency
- Integration: spawn a Node readline REPL, submit text, observe output
- Golden: envelope encode/decode round-trip

### Phase 2 — Terminal mode tracking

Deliverables:

- `internal/atcp/broker/modetrack` parsing `CSI ? 2004 h/l` at minimum
- `terminal.mode.changed` events emitted
- `paste` with `bracketed: "auto"` wraps only when child enabled bracketed paste
- Screen snapshot model (`terminal.screen.snapshot` events)

Exit criteria:

- Bracketed paste policy correctly toggles per child mode
- `terminal.mode.changed` observable in event stream

### Phase 3 — Reminders + scheduling (second priority)

Deliverables:

- `internal/atcp/broker/scheduler` with durable timer persistence
- `reminder.schedule` / `reminder.fired` flow
- Idempotency by `idempotency_key`
- Router integration: fired reminders emit `message.send` with configured delivery policy
- CLI: `foxctl remind add|list|cancel`

Exit criteria:

- Daemon restart preserves scheduled reminders (pulled from `atcp_reminders`)
- Duplicate schedule with same `idempotency_key` is a no-op
- Reminder can route to `inbox` (no terminal mutation) or `terminal` (via safe-prompt policy)

Tests:

- Unit: idempotency collision returns 200 with same reminder_id
- Integration: schedule reminder, restart daemon, verify still fires
- Integration: reminder → inbox delivery path

### Phase 4 — Message bus (rooms + inboxes)

Deliverables:

- `internal/atcp/store` message + room binding tables
- `internal/atcp/broker/router` implementing delivery policies: `inbox`, `native`, `terminal`, `overlay`
- `message.send`, `message.delivered`, `message.acknowledged` events
- CLI: `foxctl msg send`, `foxctl inbox read|ack`, `foxctl room create|join|leave|list`

Exit criteria:

- Messages persist across daemon restart
- Delivery policy `prefer: ["inbox", "terminal"]` tries inbox first, falls back to terminal with safe-prompt policy
- `requires_ack: true` blocks until `message.acknowledged` or TTL

### Phase 5 — Transactions

Deliverables:

- `transaction.run` with `wait`, `submit`, `key`, `paste`, `text` steps
- Wait sources: `output`, `screen` with regex
- Statuses: `accepted`, `observed`, `failed`, `rejected`, `ambiguous`
- CLI: `foxctl broker transaction run --file <atcp.json>`

### Phase 6 — Adapter library

Deliverables:

- `posixshell`, `nodereadline`, `tmuxcompat`, `native`
- Capability negotiation (`capability.report`)
- Native side-channel env: `AGENTCTL_CONTROL_SOCK`, `AGENTCTL_SESSION_ID`, `AGENTCTL_PROTOCOL`

## 8. Migration (Hard Cut)

Because this is a full replacement:

### 8.1 Deleted APIs

- `internal/runtime/orchestration/roomruntime.SendMessage`
- `internal/storage/blackboard.BoardStore.SendMessage` and related (messages migrate to `atcp_messages`)
- `internal/runtime/terminal/agentpane/room_service.go`, `room_registry.go`, `room_config.go`
- `cmd/foxctl/cmd/room_send_mux.go`, `room_relay_tmux*.go`
- Existing `foxctl room send`, `foxctl room remind *` commands (replaced by new CLI surface)

### 8.2 Data migration

One-shot migration at first-run of ATCP-enabled daemon:

- Read `board.db` → copy open `BoardMessage` rows into `atcp_messages` with mapped fields:
  - `sender` → `source: agent:<id>`
  - `recipient` → `target: inbox:<id>` or `room:<id>`
  - `subject + body` → `content: [{type: text, text: subject+body}]`
- Drop `BoardMessage` tables after migration completes (behind a `atcp.migrated=1` row in a migration table)
- Reminders migrated similarly if any exist

No dual-write, no fallback path. Migration runs once; daemon refuses to start with mixed state.

### 8.3 External consumers

Surfaces currently depending on BoardMessage:

- `internal/interfaces/web/api/rooms.go`, `mailbox.go`, `room_control.go` → rewritten to call ATCP broker HTTP endpoints (or in-process broker interface)
- `internal/interfaces/chatadapter/{discord,telegram}` → emit ATCP `message.send` instead of BoardMessage
- `internal/agent/tools/mail_tools.go` → call ATCP inbox API
- TUI `internal/interfaces/tui/*` room views → subscribe to SSE `GET /v1/events?target=room:...`

Each must be updated in the same PR that deletes the old API (no temporary shims).

## 9. Addressing

Target URIs (enforced by `internal/atcp/addressing`):

```
room:<room_id>
session:<session_id>
agent:<agent_id>
inbox:<inbox_id>
scheduler:<scheduler_id>
```

Parsing rejects unknown schemes. Broker rejects `message.send` with unknown target scheme (ECore policy violation).

## 10. Observability

- All envelopes flow through the append-only `atcp_events` table
- `GET /v1/events` SSE fan-out for live viewers (TUI, web, CLI)
- Structured slog output keyed by `envelope.id`, `correlation_id`, `seq`
- Metrics: sessions active, leases held, messages delivered, reminders pending, transaction outcomes

## 11. Security

- Unix socket mode `0600`
- TCP disabled; deferred to post-v0.1
- `terminal.write_bytes` requires a capability flag; default denied
- `interrupt` preemption policy requires explicit per-session capability
- Input injection rejected if no valid lease for that `scope`

## 12. Test Strategy

- Unit tests per package (≥ 80% for new code in `internal/atcp/*`)
- Integration tests in `test/integration/atcp_*_test.go`:
  - Session + submit + observed output
  - Lease contention
  - Reminder durability across daemon restart
  - Message delivery policy fallback
  - Transaction observed completion
- Regression test for the migration: seed a `board.db`, run migration, verify `atcp_messages` parity
- Golden envelope fixtures for each kind under `test/golden/atcp/<kind>.json`

## 13. Open Questions

1. Do we keep `envelope.Envelope` (skill/command) and ATCP envelope strictly separate, or unify under a single event-capable envelope? Current plan: keep separate.
2. Should ATCP persist output logs in SQLite blobs or CAS? Start with SQLite; revisit once volume measured.
3. How do overseer/agent-hierarchy spawn rules interact with ATCP session bindings? Likely: ATCP `bindings.room` is orthogonal to spawn depth; overseer policy gates `session.create` capability.
4. tmux/zellij as viewers only — do we need a pane-attach compatibility layer, or is SSE to `foxctl session stream` sufficient?
5. Do we keep a single global broker, or scope per workspace? Current plan: single broker, workspace-scoped addressing via `target`.

## 14. Milestones (sequence + rough sizing)

| # | Milestone | Phase | Size |
|---|---|---|---|
| M1 | Envelope + addressing + kinds packages | 1 | S |
| M2 | Session manager + PTY + output log | 1 | M |
| M3 | Lease manager | 1 | S |
| M4 | generic-tty adapter + CLI (submit/key/text/paste) | 1 | M |
| M5 | HTTP + Unix socket + SSE transport | 1 | M |
| M6 | Mode tracker (bracketed paste) | 2 | S |
| M7 | Scheduler + reminders durability | 3 | M |
| M8 | Router + delivery policies + rooms/inboxes | 4 | L |
| M9 | Transactions | 5 | M |
| M10 | Adapter library (posix, node, native, tmux-compat) | 6 | M |
| M11 | Migration from board.db | cross-cutting | M |
| M12 | Rewire web API, chat adapters, TUI, mail tools | cross-cutting | L |
| M13 | Delete legacy packages + CLI commands | cross-cutting | S |

Recommended PR shape: one PR per milestone, with M11–M13 landing together as the final cut-over.

## 15. Definition of Done

- All legacy rooms/BoardMessage code deleted
- All consumers (CLI, web API, chat adapters, TUI, tools) speak ATCP
- `docs/architecture/` has a new `atcp-runtime.md` describing the as-built system
- `docs/atcp/ATCP-v0.1.md` moves from `docs/atcp/` to `docs/spec/atcp/v0.1.md` as the canonical spec
- `make check-doc-links` passes
- All ATCP integration tests green in CI
- One clean migration path from `board.db` → `atcp_*` tables, idempotent, append-only
