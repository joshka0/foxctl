# Daemon Protocol (Draft)

**Status:** Draft\
**Last Updated:** 2025-12-21\
**Related:** `agent_profile_v1.md`, `../mailbox_blackboard.md`,
`core_profile_v1.md`

---

## 1. Overview

The agent daemon is a long-lived process started with:

```bash
foxctl agent run <agent-id>
```

It is responsible for:

- Polling the **daemon mailbox queue** (`internal/storage/mailbox`) for messages
  addressed to the agent’s namespace.
- Executing work (LLM tool-loop turns) for `agent.ask` and certain `agent.cmd` messages.
- Emitting `agent.reply` messages back to the caller namespace.
- Maintaining agent state and heartbeats.

This document specifies the daemon’s lifecycle and mailbox processing behavior.

---

## 2. Terminology

- **Agent ID**: the stable agent identifier (ULID string).
- **Agent namespace (`ns`)**: the mailbox address for an agent (stored on the
  agent record).
- **Daemon mailbox queue**: the SQLite-backed queue storing
  `agent.ask|reply|cmd|event` messages (see `internal/storage/mailbox`).
- **Mailbox message**: a stored record with `from_ns`, `to_ns`, `type`,
  `headers`, `payload`, `visible_at`, `attempt`, `ts`, `ttl_ms`.
- **Correlation**: `headers.correlation` used to link ask/reply.

---

## 3. Lifecycle

### 3.1 Agent record states

Agent lifecycle state values are:

- `starting`
- `running`
- `stopped`
- `error`

### 3.2 Daemon start

On startup, the daemon:

- Opens required stores under the configured storage root.
- Loads the agent record.
- Refuses to run if the agent state is `stopped`.
- Transitions the agent state to `running`.
- Starts periodic heartbeats.
- Enters the poll loop.

### 3.3 Daemon stop

The daemon exits when:

- The process context is canceled (e.g., foreground CLI interruption), or
- The agent record is observed to be in `stopped`.

On context cancellation, the daemon attempts to persist `state=stopped` before
returning.

---

## 4. Heartbeats

While running, the daemon periodically updates the agent’s `heartbeat_at` field.

- Heartbeats are written on a fixed interval (`HeartbeatInterval`).
- Heartbeat update failures are logged, but do not stop the daemon.

This protocol does not require in-daemon liveness enforcement; liveness can be
inferred by observers by comparing `heartbeat_at` to the current time.

---

## 5. Mailbox Polling & Leasing

### 5.1 Poll selection

Polling selects messages:

- Where `to_ns == <agent namespace>`
- And `visible_at <= now` (epoch seconds)

Messages are returned ordered by ascending `ts`.

### 5.2 Visibility lease

On poll, the mailbox implementation leases each returned message by:

- Setting `visible_at = now + 30s` (default lease duration)
- Incrementing `attempt`

The lease prevents concurrent consumers from receiving the same message during
the lease window.

### 5.3 Ack / Nack

- **Ack**: deletes the message.
- **Nack**: updates `visible_at = now + visibility_timeout`.

The daemon uses an exponential backoff for `visibility_timeout` on processing
errors:

- `backoff = 5s * 2^attempt` (capped at `attempt=5`, max 160s)

---

## 6. TTL Expiry

Messages may include a TTL in milliseconds (`ttl_ms`). The daemon computes
expiry as:

- `expires_at_ms = (ts * 1000) + ttl_ms`

If `ttl_ms > 0` and `now_ms > expires_at_ms`, the message is considered expired
and should be acked without processing.

---

## 7. Deduplication

Mailbox delivery is **at-least-once**. The daemon MUST de-duplicate by message
ID.

### 7.1 Dedupe key

The dedupe key is:

- `(agent_id, message_id)`

### 7.2 Dedupe behavior

For each polled message:

- If `(agent_id, message_id)` is already marked processed:
  - The daemon acks the message without processing.
- Otherwise:
  - The daemon processes the message.
  - On success:
    - The daemon marks `(agent_id, message_id)` processed.
    - The daemon acks the message.
  - On error:
    - The daemon nacks the message with backoff.

### 7.3 SQLite dedupe schema

When using SQLite dedupe, records are stored in `daemon_dedupe.db` under the
storage root.

```sql
CREATE TABLE IF NOT EXISTS daemon_dedupe (
  agent_id     TEXT NOT NULL,
  message_id   TEXT NOT NULL,
  processed_at INTEGER NOT NULL,
  PRIMARY KEY (agent_id, message_id)
);
CREATE INDEX IF NOT EXISTS idx_dedupe_processed_at ON daemon_dedupe(processed_at);
```

The daemon performs a best-effort cleanup at startup, deleting records older
than 7 days.

---

## 8. Message Handling

### 8.1 Common fields

Each daemon mailbox message includes:

- `type`: `agent.ask | agent.reply | agent.cmd | agent.event`
- `headers.correlation` (optional): correlation ID
- `payload`: JSON object; producers typically embed a Core envelope. The daemon
  handlers rely primarily on the `data` field for dispatch.

### 8.2 Ask/reply flow

High-level flow:

```text
foxctl agent ask
  -> mailbox.Send(type=agent.ask, headers.correlation=<ask_id>)
  -> daemon polls + leases message
  -> daemon executes LLM tool-loop turn
  -> mailbox.Send(type=agent.reply, headers.correlation=<ask_id>)
  -> (optional) caller polls for agent.reply and acks it
```

#### Handling `agent.ask`

- Parse `payload.data` as `AskData`.
- Execute an LLM tool-loop turn with a timeout derived from the agent policy
  (`policy.timeout`), falling back to 10 minutes.
- Construct and send an `agent.reply` message:
  - `to_ns = <original from_ns>`
  - `headers.correlation = <ask_id>`
  - `ttl_ms = 300000` (5 minutes)

#### Handling `agent.reply`

- Replies received by the daemon are logged and acked.

### 8.3 Handling `agent.cmd`

The daemon supports:

- `action=run_turn` or `action=do_work`: executes an LLM tool-loop turn.
- `action=run_skill`: currently returns an error and will be retried via
  nack/backoff.

### 8.4 Handling `agent.event`

The daemon parses `payload.data` as `EventData` and logs the event.

---

## 9. Configuration Defaults (CLI)

When started via `foxctl agent run`, defaults are:

- `PollInterval`: 500ms
- `HeartbeatInterval`: 10s
- `MaxPollMessages`: 10

Mailbox lease duration is currently 30s (implemented in the mailbox store).

---

## 10. References

- Daemon loop: `internal/agent/daemon/daemon.go`
- Message handlers: `internal/agent/daemon/handlers.go`
- Mailbox store: `internal/storage/mailbox/store.go`
- Dedupe store: `internal/agent/daemon/dedupe_sqlite.go`
