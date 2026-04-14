# Interactive Actor Console (agentctl_viewer integration)

## Purpose & scope

Enable interactive, per-actor consoles using `agentctl_viewer` as the UI,
without introducing a new transport. All I/O goes through the existing mailbox
(leased queue) and follows foxctl envelope/CAS rules. This is an additive
design that layers on top of:

- **Reactive Actor System** (`docs/designs/reactive-actor-system.md`): watcher +
  supervisor + mailbox Poll/Ack/Nack semantics.
- **Actor Progressive Memory** (`docs/designs/actor-progressive-memory.md`):
  console sessions produce turns/events that can be distilled without losing raw
  data.

Non-goals: new network listeners; bypassing mailbox leases; changing envelope
shape; adding secret-bearing side channels.

## Contracts (must not break)

- **Delivery:** at-least-once via mailbox `Poll → Ack/Nack` with lease/backoff;
  watcher/notify only wakes consumers.
- **Envelopes:** stdout is JSON envelope with `meta.ts` (RFC3339 UTC). Large
  outputs → `data.summary` ≤2 KiB + `data.artifact` CAS digest; optional
  `meta.cas_digest` must match if set. No non-JSON on stdout.
- **CAS & storage:** large transcripts go to CAS; trajectory/events go to the
  existing durable store; no partial writes.
- **Secrets:** redact before emitting `event/reply`; never log secrets; respect
  path validator for any file references.
- **Idempotency:** actor handlers must be idempotent per `correlation_id`;
  retries after lease expiry must be safe.

## Components

- **Console session registry:** `(actor_id, console_id, session_id, workspace)`
  recorded so users can reattach. `console_id` and `correlation_id` are ULIDs.
- **agentctl_viewer console client:** TUI front-end; renders to stderr, emits
  envelopes on stdout; per-tab state tracks pending correlations.
- **Overseer integration:** when spawning an actor, optionally create a console
  session and auto-launch
  `foxctl viewer --actor-console <actor_id> --console <console_id>` (in a new
  terminal/tab).
- **Mailbox transport:** reuses existing mailbox rows and notify/trigger. No
  direct DB joins to read messages; consumption is via `Poll` to claim a lease.

## Message payload (mailbox body)

JSON payload inside mailbox message (stored in `body`):

```jsonc
{
  "type": "ask" | "reply" | "event" | "cmd",
  "actor_id": "<actor-ulid>",
  "console_id": "<console-ulid>",
  "correlation_id": "<ulid>",
  "content": "<text or chunk>",
  "metadata": {
    "mime": "text/plain|text/markdown|application/json",
    "partial": true,
    "exit_code": 0,
    "error": "",
    "progress": { "pct": 0-100, "phase": "str" },
    "tool": "name"
  },
  "cmd": { "name": "cancel" }
}
```

Notes:

- `event` is for streaming/progress; `reply` is final.
- `cmd.cancel` targets a `correlation_id`; actor should honor cancellation.
- All mailbox delivery uses `Poll/Ack/Nack` with leases; watcher/notify only
  signals “there is work.”

## Console flow (per tab)

1. **Send**: user input → `ask` with new `correlation_id` → `Send` to mailbox.
2. **Receive**: `Poll` for messages addressed to `console_id`.
   - On `event`: render streaming chunk; `Ack`.
   - On `reply`: render final; `Ack`; clear pending correlation.
   - On error to render: `Nack` to allow retry after lease expiry.
3. **Cancel**: Ctrl+C (or UI action) sends `cmd:cancel` for the active
   correlation.
4. **Backpressure**: default max in-flight correlations per console = 1
   (configurable small window). Supervisor does not claim additional
   console-directed messages for this actor while a correlation is active if
   concurrency=1.

## UI/UX (agentctl_viewer)

- Tabs per actor console; unread badge on unfocused tabs.
- Input pane for active tab; Enter sends `ask`; Ctrl+C issues `cancel` for
  active correlation.
- Streaming render for `event` chunks; highlight errors; action to “save
  transcript to CAS”.
- Status strip: actor state (running/idle), queue depth (from mailbox stats),
  attempts/backoff.
- Stdout: envelopes with summary + optional CAS digest; stderr: TUI
  drawing/logs.

## Observability & durability

- **Trajectory:** log `event/reply/cancel` into trajectory store with
  `actor_id`, `console_id`, `correlation_id`, `session_id`.
- **CAS:** transcripts over inline limit go to CAS; include digest in final
  `reply` summary.
- **Metrics:** counts for asks/replies/events, latency per correlation, retries,
  cancels.

## Progressive memory interplay

- Console session ID is a normal session ULID; raw turns/events are durable in
  session/trajectory storage.
- Compaction uses cursors (from progressive-memory design): do not delete raw
  turns until L1/L2 artifacts are persisted.
- Redaction runs before summarization to prevent secret leakage into L1/L2.

## CLI ergonomics

- `foxctl actor console --actor <id> [--console <cid>] [--session <sid>]` to
  attach or create.
- Overseer option: `--auto-console` to spawn a new tab/terminal for each actor
  at creation.
- `foxctl actor consoles list` to show attachable consoles (actor_id,
  console_id, session_id, workspace).

## DB schema notes (minimal)

- **console_sessions**:
  `(console_id PK, actor_id, session_id, workspace, created_at, last_attached_at, meta JSON)`.
- **mailbox_notify**: existing notify table/trigger reused; no schema change to
  mailbox rows (payload is JSON in `body`).
- Optional: **console_transcripts**:
  `(console_id, correlation_id, cas_digest, summary, created_at)` if you want a
  fast lookup of past conversations without scanning trajectory.

## CLI flags (viewer/overseer)

- `foxctl actor console --actor <id> [--console <cid>] [--session <sid>] [--workspace <path>]`
- `foxctl actor consoles list [--workspace <path>]`
- `foxctl actor consoles rm --console <cid>`
- Overseer spawn: `--auto-console` (bool), `--console-workspace <path>`
  (override), `--console-detach` (spawn without attaching UI).

## Implementation plan (phased)

1. **Plumbing**: console registry table + CLI attach command; mailbox payload
   struct; correlation tracking in viewer; cancel command wiring.
2. **Viewer TUI**: tabs, streaming renderer, CAS transcript action,
   Ctrl+C→cancel.
3. **Overseer hook**: optional auto-launch console on actor spawn; registry
   population.
4. **Progressive memory hooks**: ensure console events enter trajectory/session
   logs; add compaction cursors for console sessions.

## Test plan (minimum)

- Mailbox delivery: ask→event→reply round-trip; at-least-once with lease expiry
  retry; cancel honored.
- Backpressure: single in-flight blocks additional claims; configurable small >1
  works.
- CAS rollover: long transcript stored in CAS with summary on stdout; digest
  matches.
- Redaction: secret-like tokens not present in stored events/summaries.
- CLI: attach/reattach flows; auto-console launch produces a usable tab; stdout
  stays envelopes only.
