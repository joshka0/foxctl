# Zellij Pane PTY Transport

## Goal

Replace unreliable cross-pane input injection with an `foxctl`-owned pane wrapper that
launches each agent CLI behind a PTY and accepts room messages through a local control channel.

Zellij remains the visual multiplexer. `foxctl` becomes the transport owner.

## Problem

The current Zellij room relay can resolve the right room member and pane target, but successful
"delivery" does not reliably mean bytes reached the child process in the target pane.

Observed failure modes:

- room loop reports `delivered_to` for named Zellij panes
- probe panes that should persist stdin to a file remain empty
- direct pane writes through the Zellij plugin API are not trustworthy enough as the transport

This makes the current "inject into another pane" model unsuitable for durable room messaging.

## Proposed Model

Launch a wrapper process inside each pane instead of launching the agent CLI directly:

```text
zellij pane
  -> foxctl pane serve
       -> owns PTY master
       -> child agent CLI attached to PTY slave
       -> control socket / inbox for room messages
```

Example launch shape:

```bash
foxctl pane serve --participant claude-a --room-id room-123 -- claude
foxctl pane serve --participant gemini-a --room-id room-123 -- gemini
foxctl pane serve --participant droid-a --room-id room-123 -- droid
```

## Responsibilities

### Zellij

- pane layout
- local human visibility
- focus management
- nothing transport-critical

### `foxctl pane serve`

- create PTY pair
- spawn child with slave PTY as stdin/stdout/stderr
- read child output from PTY master and forward to pane stdout
- accept inbound room messages over a local control channel
- write message bytes directly to the PTY master
- apply per-agent submit behavior
- publish heartbeat / readiness / transport metadata

### Room loop

- durable routing and reminders
- resolve participant -> pane transport endpoint
- deliver to pane wrapper control endpoint, not to Zellij

## Why PTY Ownership Fixes This

The wrapper owns the real child input stream.

That means:

- input injection does not depend on Zellij plugin write semantics
- focus does not matter
- hidden/suppressed pane state does not matter
- delivery success can mean "bytes written to child PTY", not "target name resolved"

## Transport Contract

Each pane wrapper should publish a local endpoint plus health metadata.

Suggested fields:

```json
{
  "backend": "zellij",
  "session": "didactic-drum",
  "pane_id": "claude-a",
  "participant_id": "claude-a",
  "transport_kind": "pane_socket",
  "transport_endpoint": "/path/to/socket",
  "transport_state": "ready"
}
```

This can extend or sit alongside the existing `TerminalBinding`.

## Control Channel

Preferred first version: unix domain socket per pane wrapper.

Message shape:

```json
{
  "kind": "room_message",
  "room_id": "room-123",
  "message_id": "01...",
  "sender": "codex-a",
  "recipient": "claude-a",
  "interrupt": false,
  "content": "[room room-123 from=codex-a to=claude-a] ...",
  "submit_mode": "composer_ctrl_enter"
}
```

Response shape:

```json
{
  "ok": true,
  "accepted_at": "2026-04-10T13:00:00Z",
  "bytes_written": 128
}
```

## Submit Adapters

The wrapper should own submit behavior by agent family, not the room loop:

- `claude`, `gemini`, `droid`, `codex`, `cursor`: composer-style adapter
- shell / plain terminal: newline adapter

Adapter interface:

```go
type PaneSubmitAdapter interface {
    InjectMessage(payload string, interrupt bool) []byte
}
```

The adapter returns the exact PTY bytes to write.

## Process Model

### Wrapper lifecycle

1. wrapper starts
2. wrapper creates PTY
3. wrapper spawns child
4. wrapper creates socket and writes readiness marker
5. wrapper forwards PTY output to pane stdout
6. wrapper handles inbound control messages until context cancellation

### Failure handling

- if child exits, wrapper marks transport unavailable
- room loop should treat unavailable transport as failed delivery
- wrapper can optionally restart child, but first version should prefer explicit failure

## Integration Points

### `mux create`

For Zellij pane-backed agents, replace direct command launch with:

```text
foxctl pane serve --participant <id> --room-id <id> -- <agent command>
```

### room membership

When provisioning or rebinding pane-backed members, persist:

- participant id
- Zellij session / pane label
- wrapper transport endpoint

### room loop

Delivery order:

1. pane wrapper endpoint if present
2. legacy tmux direct transport if present
3. legacy Zellij plugin path only as fallback during migration

## Suggested Implementation Slices

### Slice 1

Add `foxctl pane serve` with:

- PTY spawn
- stdout forwarding
- unix socket control
- simple newline adapter

Use only a probe child first (`cat`, `tee`) to prove transport.

### Slice 2

Integrate `mux create --backend zellij` so provisioned panes launch through the wrapper.

### Slice 3

Teach room loop to prefer pane wrapper socket delivery over Zellij plugin delivery.

### Slice 4

Add per-agent adapters for composer UIs.

### Slice 5

Demote the current Zellij plugin relay to optional fallback / diagnostics.

## Success Criteria

- probe pane receives direct messages through wrapper transport with no Zellij plugin dependency
- room loop delivery success means socket accepted + PTY write succeeded
- direct messages reach unfocused Zellij panes reliably
- coordinator pulses and reminders use the same transport

## Non-Goals For First Version

- semantic parsing of agent UI state
- visual automation inside panes
- guaranteed confirmation that an agent "understood" a message

The first goal is only reliable byte delivery into the target child PTY.
