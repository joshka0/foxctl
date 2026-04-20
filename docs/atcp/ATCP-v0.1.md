# Agent Terminal Coordination Protocol (ATCP) v0.1

Status: draft
Scope: local-first coordination of interactive CLI processes, agent messages, reminders, and PTY-backed terminal sessions.

## 1. Purpose

ATCP exists because an interactive CLI is not a message API. It is usually a process attached to a pseudo-terminal, interpreting byte streams, terminal modes, ANSI control sequences, canonical/raw input behavior, and application-specific key handling. Treating every message as `text + \n` is unreliable.

ATCP separates intent from bytes:

- A controller sends typed intents such as `message.send`, `terminal.submit`, `terminal.key`, `terminal.paste`, or `reminder.fire`.
- A broker owns PTY sessions and serializes access to them.
- An adapter compiles typed intents into target-specific input bytes only at the edge.
- Protocol-aware agents receive structured messages directly, while opaque third-party CLIs are driven through a PTY fallback.

## 2. Roles

### Broker

Long-running local daemon. Owns sessions, PTYs, event logs, locks, reminder scheduling, and terminal mode state.

Suggested names: `agentctld`, `ptyd`, or `agentctl broker`.

### Session

One interactive process tree launched under a PTY. A session has:

- `session_id`
- process metadata
- adapter profile
- output log
- terminal screen model
- terminal mode state
- input lease state
- inbox bindings

### Adapter

A compiler from ATCP input intents to bytes or side-channel messages. Examples:

- `generic-tty`
- `posix-shell`
- `node-readline`
- `node-raw`
- `agentctl-native`
- `tmux-compat` only when unavoidable

### Controller

A client that creates sessions, sends messages, streams output, schedules reminders, or coordinates agents.

### Scheduler

A broker component that emits `reminder.fire` events and optionally routes them to rooms, sessions, or agent inboxes.

### Agent / CLI

A target process. It may be protocol-aware, terminal-only, or hybrid.

## 3. Design principles

1. Never make raw terminal bytes the primary API.
2. Use structured semantic messages whenever the receiver supports them.
3. Use PTY injection as a compatibility layer, not the source of truth.
4. Treat `Enter`, `LineFeed`, and `CarriageReturn` as distinct operations.
5. Serialize input with leases so two producers cannot type over one another.
6. Track terminal modes from child output, especially bracketed paste mode.
7. Distinguish delivery acceptance from observed completion.
8. Preserve an append-only event log for replay, debugging, and agent auditability.

## 4. Transport

ATCP is transport-neutral. Recommended local transports:

- Unix domain socket with HTTP/1.1 JSON endpoints and SSE streams.
- Unix domain socket with JSON-RPC 2.0 over newline-delimited JSON.
- WebSocket over Unix socket for browser or xterm.js frontends.

Recommended default: HTTP+JSON for commands and SSE for event streams.

Security defaults:

- Unix socket permissions `0600`.
- TCP disabled by default.
- Bearer token required for TCP.
- Capability checks for input injection, session creation, deletion, and secret-bearing output access.

## 5. Envelope

All protocol messages use a common envelope.

```json
{
  "v": "atcp/0.1",
  "id": "evt_01HX...",
  "kind": "terminal.submit",
  "ts": "2026-04-19T12:34:56.789Z",
  "source": "agentctl.scheduler",
  "target": "session:sess_01HX...",
  "seq": 1842,
  "correlation_id": "corr_01HX...",
  "idempotency_key": "optional-stable-key",
  "body": {}
}
```

Field notes:

- `id` is globally unique.
- `seq` is broker-assigned within an event stream.
- `correlation_id` links commands, output, and completion events.
- `idempotency_key` prevents duplicated reminders or repeated automation.
- `source` and `target` use URI-like addressing.

## 6. Addressing

Suggested target forms:

```text
room:<room_id>
session:<session_id>
agent:<agent_id>
inbox:<inbox_id>
scheduler:<scheduler_id>
```

A room can bind multiple sessions and agents. A session can have both a terminal endpoint and a structured inbox endpoint.

## 7. Session lifecycle

### Create session

`POST /v1/sessions`

```json
{
  "name": "claude-main",
  "cmd": ["claude"],
  "cwd": "/repo",
  "env": {
    "TERM": "xterm-256color"
  },
  "term": {
    "rows": 32,
    "cols": 120,
    "type": "xterm-256color"
  },
  "adapter": "generic-tty",
  "bindings": {
    "room": "dev",
    "inbox": "inbox_dev_claude"
  }
}
```

Response:

```json
{
  "session_id": "sess_01HX...",
  "pid": 48291,
  "adapter": "generic-tty",
  "capabilities": [
    "terminal.input.text",
    "terminal.input.key",
    "terminal.input.submit",
    "terminal.input.paste",
    "terminal.output.stream",
    "terminal.mode.track",
    "lease.input"
  ]
}
```

### Delete session

`DELETE /v1/sessions/{session_id}`

Supports graceful shutdown, signal, timeout, and force-kill policy.

## 8. Event streams

`GET /v1/events?target=session:sess_...&since=1842`

Important event kinds:

- `session.created`
- `session.started`
- `session.exited`
- `terminal.output.bytes`
- `terminal.output.frame`
- `terminal.screen.snapshot`
- `terminal.mode.changed`
- `terminal.input.accepted`
- `terminal.input.rejected`
- `transaction.started`
- `transaction.completed`
- `transaction.failed`
- `message.sent`
- `message.delivered`
- `message.acknowledged`
- `reminder.scheduled`
- `reminder.fired`

## 9. Terminal input model

### 9.1 Raw bytes escape hatch

`terminal.write_bytes` exists but should be privileged and discouraged.

```json
{
  "kind": "terminal.write_bytes",
  "target": "session:sess_...",
  "body": {
    "bytes_b64": "ZWNobyBoaQo=",
    "reason": "manual-debug"
  }
}
```

Use typed input intents whenever possible.

### 9.2 Text

Text writes printable text without submitting it.

```json
{
  "kind": "terminal.text",
  "target": "session:sess_...",
  "body": {
    "text": "hello world",
    "encoding": "utf-8"
  }
}
```

The adapter may normalize unsupported characters or reject input.

### 9.3 Key

Key sends a keypress, not text.

```json
{
  "kind": "terminal.key",
  "target": "session:sess_...",
  "body": {
    "key": "Enter",
    "modifiers": [],
    "repeat": 1
  }
}
```

Canonical key names:

- `Enter`
- `LineFeed`
- `CarriageReturn`
- `Tab`
- `Backspace`
- `Delete`
- `Escape`
- `ArrowUp`
- `ArrowDown`
- `ArrowLeft`
- `ArrowRight`
- `Home`
- `End`
- `PageUp`
- `PageDown`
- `CtrlC`
- `CtrlD`
- `CtrlU`
- `CtrlL`

Default byte compilation:

```text
Enter           -> 0x0D       CR, human Return key in most terminal input contexts
LineFeed        -> 0x0A       LF, explicit Ctrl-J-like linefeed
CarriageReturn  -> 0x0D       CR
Tab             -> 0x09
Escape          -> 0x1B
CtrlC           -> 0x03
CtrlD           -> 0x04
CtrlU           -> 0x15
CtrlL           -> 0x0C
ArrowUp         -> ESC [ A
ArrowDown       -> ESC [ B
ArrowRight      -> ESC [ C
ArrowLeft       -> ESC [ D
```

Adapters may override application-mode arrow keys or other function-key variants.

### 9.4 Submit

Submit means: write an input payload and activate the target's submit gesture. It is not defined as appending `\n`.

```json
{
  "kind": "terminal.submit",
  "target": "session:sess_...",
  "body": {
    "text": "run tests",
    "submit_key": "Enter",
    "mode": "typed",
    "expect": {
      "kind": "prompt-or-output",
      "timeout_ms": 30000
    }
  }
}
```

Default compilation:

```text
text bytes + Enter
```

where `Enter` normally compiles to `0x0D`, not `0x0A` and not `0x0D 0x0A`.

### 9.5 Paste

Paste sends a block of text as a paste operation.

```json
{
  "kind": "terminal.paste",
  "target": "session:sess_...",
  "body": {
    "text": "multi\nline\nmessage",
    "submit_after": false,
    "bracketed": "auto"
  }
}
```

`bracketed` values:

- `auto`: if the child enabled bracketed paste mode, wrap with bracketed paste delimiters.
- `force`: always wrap.
- `off`: never wrap.

When enabled, compile as:

```text
ESC [ 200 ~
<payload bytes>
ESC [ 201 ~
```

The broker tracks whether the child requested bracketed paste by observing output sequences such as:

```text
ESC [ ? 2004 h   enable bracketed paste
ESC [ ? 2004 l   disable bracketed paste
```

### 9.6 Input modes

`mode` controls how the adapter injects content:

- `typed`: write text as if typed.
- `paste`: write text as paste, honoring bracketed paste policy.
- `paced`: write characters with delay or chunk boundaries.
- `literal`: write bytes after UTF-8 encoding, with no high-level interpretation.

`paced` can be necessary for brittle CLIs that debounce, autocomplete, or react badly to large bursts.

## 10. Input leases

Only one producer may mutate a session's terminal input at a time.

Acquire:

```json
{
  "kind": "lease.acquire",
  "target": "session:sess_...",
  "body": {
    "scope": "terminal.input",
    "owner": "agentctl.scheduler",
    "ttl_ms": 15000,
    "preempt": false
  }
}
```

Release:

```json
{
  "kind": "lease.release",
  "target": "session:sess_...",
  "body": {
    "lease_id": "lease_01HX..."
  }
}
```

All terminal mutation commands may require `lease_id`.

## 11. Transactions

Transactions make automation observable and safer.

```json
{
  "kind": "transaction.run",
  "target": "session:sess_...",
  "body": {
    "name": "deliver-reminder-to-cli",
    "lease": {
      "scope": "terminal.input",
      "ttl_ms": 30000,
      "preempt": false
    },
    "steps": [
      {
        "op": "wait",
        "match": {
          "source": "screen",
          "regex": "(?m)> $"
        },
        "timeout_ms": 10000
      },
      {
        "op": "submit",
        "text": "/message reminder: standup in 5 minutes",
        "submit_key": "Enter"
      },
      {
        "op": "wait",
        "match": {
          "source": "output",
          "regex": "(?i)(sent|queued|acknowledged)"
        },
        "timeout_ms": 10000
      }
    ]
  }
}
```

Transaction result:

```json
{
  "kind": "transaction.completed",
  "target": "session:sess_...",
  "correlation_id": "corr_01HX...",
  "body": {
    "status": "observed",
    "steps_completed": 3,
    "output_seq_start": 9221,
    "output_seq_end": 9257
  }
}
```

Statuses:

- `accepted`: broker wrote intended bytes.
- `observed`: expected output or screen state was observed.
- `failed`: transaction could not complete.
- `rejected`: policy, lease, auth, or adapter rejected it.
- `ambiguous`: bytes were written but expected state was not observed.

## 12. Message coordination

Structured message delivery should be the preferred path for agent-aware participants.

```json
{
  "kind": "message.send",
  "source": "agent:planner",
  "target": "room:01HX0000000000000000000000",
  "body": {
    "message_id": "msg_01HX...",
    "correlation_id": "corr_01HX...",
    "reply_to_message_id": "msg_01HW...",
    "topic": "review",
    "priority": "normal",
    "content": [
      {
        "type": "text",
        "text": "Please review the failing test output."
      }
    ],
    "delivery": {
      "prefer": ["inbox", "native", "terminal"],
      "terminal_policy": "safe-prompt-only",
      "requires_ack": true
    },
    "receipt": {
      "message_id": "msg_01HX...",
      "room_id": "01HX0000000000000000000000",
      "source": "agent:planner",
      "correlation_id": "corr_01HX...",
      "reply_to_message_id": "msg_01HW...",
      "reply_prefix": "@room:01HX0000000000000000000000 "
    }
  }
}
```

Delivery modes:

- `inbox`: protocol-native inbox event.
- `native`: side-channel supported by the target process.
- `terminal`: adapter-mediated PTY injection.
- `overlay`: display to human/operator without mutating target CLI.

Terminal fallback should be adapter-templated. Example:

```text
[ATCP receipt] {"message_id":"msg_01HX...","room_id":"01HX0000000000000000000000","source":"agent:planner","correlation_id":"corr_01HX...","reply_to_message_id":"msg_01HW...","reply_prefix_hint":"<AT>room:01HX0000000000000000000000 "}
[ATCP reply] print reply_prefix_hint with <AT> replaced by the at-sign character, then append your message.

Please review the failing test output.
```

Terminal fallback uses `reply_prefix_hint` instead of a literal `@room:...`
prefix so TUIs with `@` file-mention shortcuts do not open completion popups
while the preamble is being typed. Structured/native delivery still carries
the exact `reply_prefix`.

For CLIs that support slash commands, adapter config may compile messages into commands:

```text
/message --from planner --topic review Please review the failing test output.
```

Room message history is replayable:

```text
GET /v1/rooms/{id}/messages?limit=100
```

The response returns chronological audit records with message metadata,
payload text, aggregate delivery counts, and per-member delivery outcomes.
The running broker keeps an in-memory audit log; SQLite-backed daemons hydrate
that log on startup and persist new sends append-only.

## 13. Reminders

Reminder scheduling emits protocol messages; it does not directly type into terminals.

Schedule:

```json
{
  "kind": "reminder.schedule",
  "source": "agentctl.user",
  "target": "agent:sess_...",
  "body": {
    "reminder_id": "rem_01HX...",
    "fire_at": "2026-04-19T13:00:00Z",
    "content": [
      {
        "type": "text",
        "text": "Check deployment logs."
      }
    ],
    "delivery": {
      "prefer": ["inbox", "overlay", "terminal"],
      "terminal_policy": "safe-prompt-only"
    }
  }
}
```

Fire:

```json
{
  "kind": "reminder.fired",
  "source": "agentctl.scheduler",
  "target": "agent:sess_...",
  "body": {
    "reminder_id": "rem_01HX...",
    "content": [
      {
        "type": "text",
        "text": "Check deployment logs."
      }
    ]
  }
}
```

The router then performs `message.send` with the configured delivery policy.

## 14. Terminal mode tracking

The broker should parse child output enough to track important terminal modes:

- bracketed paste enabled/disabled
- alternate screen
- cursor mode
- application cursor keys
- focus reporting, if needed
- terminal resize state

The minimum viable parser tracks `CSI ? 2004 h/l` because it materially changes how paste should be delivered.

## 15. Prompt and readiness detection

Generic prompt detection is best-effort. ATCP supports multiple readiness strategies:

- regex over output bytes
- regex over current screen line
- adapter-specific prompt markers
- shell-injected markers
- protocol-native ready events
- human/operator confirmation

A safe terminal delivery policy should wait for a known safe prompt before mutating input.

Current HTTP readiness supports byte-idle checks plus optional rendered-screen
matching:

```text
GET /v1/sessions/{id}/readiness?threshold_bps=32&debounce_ms=500&screen_regex=...
```

When `screen_regex` is present, readiness requires both byte-idle output and a
rendered screen line matching the regex. The response includes `screen_match`,
`screen_regex`, and `screen_line` for diagnostics.

When no query regex is supplied, the endpoint evaluates the session's readiness
profile. Session creation can set a profile directly, and adapter defaults can
provide one for known CLIs.

Readiness is also observable as an event:

```text
GET /v1/events?target=session:{id}&ready=true
```

The stream emits `terminal.ready` on readiness transitions and sends the
current state when the subscription starts.

Activity is observable separately from readiness. Use it when an operator or
coordinator wants to know whether a PTY-backed agent is still doing work
between heartbeats:

```text
GET /v1/sessions/{id}/activity?since_seq=123&since_output_bytes_total=4567
GET /v1/events?target=session:{id}&activity=true&activity_interval_ms=1000
GET /v1/events?target=room:{id}
```

The activity response and `terminal.activity` event report the current output
cursor, output-byte delta, sequence delta, rolling output rate, and
`output_changed:true` / `working:true` when the session produced output since
the supplied cursor or previous heartbeat.

Room event streams fan in `terminal.output` from the active members present
when the stream opens. Each room-level `terminal.output` body carries
`room_id`, `agent_id`, and `session_id`.

Message sends can optionally wait for this activity signal:

```json
{
  "room_id": "01HX...",
  "text": "please respond with pong",
  "await_activity_ms": 10000,
  "await_ready_ms": 30000
}
```

When requested, each delivered member in the `POST /v1/messages` response
includes `activity.first_output_ms` once output changes after delivery and
`activity.completed_ms` when that recipient returns to its configured
readiness state. `completed` is a PTY-level approximation: "the agent produced
output and the terminal became ready again", not a semantic guarantee that the
LLM answered correctly.

## 16. Preemption policies

When a reminder or message arrives while a CLI is busy, ATCP must not blindly type over work in progress.

Policies:

- `immediate`: write now; this is the default for backwards compatibility.
- `queue`: wait until safe prompt.
- `overlay`: show out-of-band only.
- `clear-line`: send `CtrlU` before injecting; risky, opt-in only.
- `interrupt`: send a configured interrupt key before injecting. Current
  terminal message delivery defaults this to `Escape`; callers can set another
  key such as `CtrlC` explicitly when they want a more disruptive interrupt.
- `safe-prompt-only` / `reject`: fail immediately if not safe.

Default for reminders: `queue` or `overlay`, not `interrupt`.

HTTP message delivery exposes the terminal subset directly:

```json
{
  "room_id": "01HX...",
  "text": "please respond with pong",
  "terminal_policy": "queue",
  "policy_timeout_ms": 30000,
  "interrupt_key": "Escape"
}
```

The policy result is reported per member. A policy failure is a delivery
failure for that recipient; other recipients are still attempted.

## 17. Adapter profiles

Built-in PTY adapter profile defaults are data, not broker policy. The current
registry is embedded from `internal/atcp/adapter/profiles/profiles.json`; the
broker looks up a profile by `session.adapter`, then merges any explicit
session-level readiness override.

### generic-tty

- `Enter` -> CR (`0x0D`)
- `submit` -> text + Enter
- `paste` -> bracketed paste if mode tracking says enabled
- does not assume prompt shape unless configured

### posix-shell

- `Enter` -> CR
- can optionally set shell prompt markers
- can run commands with explicit command correlation markers

### node-readline

- `Enter` -> CR
- multiline content should use paste mode or adapter-specific escape handling
- raw byte `\n` is not equivalent to a human Return key in all Node raw/readline cases

### agentctl-native

- terminal injection avoided
- messages delivered to side-channel inbox
- terminal only used for human display or fallback

### current built-ins

- `codex`
- `droid`
- `gemini`
- `claude` / `claude-code`

## 18. Capability negotiation

When a session starts, the broker records adapter and target capabilities.

```json
{
  "kind": "capability.report",
  "target": "session:sess_...",
  "body": {
    "adapter": "node-readline",
    "supports": [
      "terminal.submit",
      "terminal.paste.bracketed.auto",
      "terminal.mode.bracketed-paste",
      "transaction.wait.screen-regex"
    ],
    "unsafe": [
      "terminal.interrupt"
    ]
  }
}
```

Native agents can report capabilities through an environment-provided side-channel:

```text
AGENTCTL_CONTROL_SOCK=/run/user/501/agentctl/sess_...sock
AGENTCTL_SESSION_ID=sess_...
AGENTCTL_PROTOCOL=atcp/0.1
```

## 19. Suggested CLI surface

```bash
agentctl session create claude -- claude
agentctl session stream claude
agentctl term submit claude "summarize the current diff"
agentctl term key claude Enter
agentctl msg send room:dev "Please review the failing test"
agentctl remind claude "in 20 minutes" "Check deployment logs"
agentctl inbox read claude
agentctl broker events --since 0
```

`agentctl term submit` is terminal fallback. `agentctl msg send` is the preferred coordination primitive.

## 20. Implementation roadmap

Phase 1: PTY broker

- session create/delete
- PTY output log
- typed input compiler
- input leases
- `terminal.submit`, `terminal.key`, `terminal.paste`

Phase 2: terminal mode tracking

- parse bracketed paste enable/disable
- screen snapshot model
- SSE event stream

Phase 3: transactions

- wait conditions
- safe prompt policies
- observed completion status

Phase 4: message bus

- rooms
- inboxes
- delivery policies
- native side-channel for agentctl-aware processes

Phase 5: reminders

- schedule/fire events
- delivery router
- idempotency and acknowledgements

Phase 6: adapter library

- generic TTY
- POSIX shell
- Node readline/raw
- selected third-party CLI profiles

## 21. Non-goals

ATCP is not a terminal multiplexer UI. It does not need panes, layouts, scrollback UI, or attach/detach semantics. It can support viewers, but its primary responsibility is session ownership, typed input coordination, and structured message routing.

ATCP is also not a guarantee that arbitrary opaque CLIs become reliable APIs. It makes terminal automation safer and more observable, while encouraging native protocol paths whenever possible.

## 22. Source notes

Important compatibility references:

- POSIX/general termios semantics: canonical mode, input translation, and CR/LF handling.
- Linux termios documentation: `ICRNL`, `ICANON`, `ECHO`, and related flags.
- Node.js TTY/readline docs: raw mode and line-based stream reading.
- xterm bracketed paste documentation: paste delimiters and mode behavior.
