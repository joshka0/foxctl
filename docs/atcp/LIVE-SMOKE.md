# ATCP live smoke — codex + droid + gemini

A step-by-step guide for driving a live multi-agent ATCP room from the
shell. Uses the `atcpd` daemon and the `atcp-live` driver binary. No
client-library work required — this is the "fast path" harness while we
decide what to promote into `atcpctl` proper.

## Prerequisites

- `atcpd` binary built: `go build -o /tmp/atcpd ./cmd/atcpd`
- `atcp-live` binary built: `go build -o /tmp/atcp-live ./cmd/atcp-live`
- Agent CLIs on `$PATH`: `codex`, `droid`, `gemini` (or whatever names /
  commands you want to coordinate).

## 1. Start the daemon

The daemon owns every PTY and the broker state. Run it in its own terminal
(or as a background process) so crashes are visible.

```sh
rm -f /tmp/atcp-live.sock
/tmp/atcpd --socket /tmp/atcp-live.sock
```

Default socket is `$XDG_RUNTIME_DIR/foxctl/atcp.sock` (or
`$HOME/.foxctl/atcp.sock` on macOS). Passing `--socket` makes it
obvious which instance you're driving, which matters when debugging.

## 2. Verify the socket

```sh
ls -la /tmp/atcp-live.sock   # should be mode 0600 and owned by you
```

## 3. Launch the room with three agents

```sh
/tmp/atcp-live \
  --socket /tmp/atcp-live.sock \
  --workspace live \
  --room-title "codex+droid+gemini" \
  --agent codex='codex --chat' \
  --agent droid='droid run' \
  --agent gemini='gemini chat' \
  --source human
```

What happens:

1. `atcp-live` hits `/v1/health` to confirm the daemon is up.
2. It creates a room (`workspace=live, title="codex+droid+gemini"`) and
   prints the room id.
3. For each `--agent NAME=CMD` it POSTs a session (the daemon spawns the
   PTY) and joins the session to the room with `CanMutate=true`, so every
   member both receives and is addressable by the fan-out router.
4. It opens a Server-Sent-Events stream (`GET /v1/events?target=session:<id>`)
   per agent and prints each decoded PTY byte chunk to stdout with a
   `[name] ` prefix.
5. Whatever you type on `atcp-live`'s stdin is forwarded — one line at a
   time — as a room message with `source="human"`. Every mutable member
   receives it.

## 4. Interact

- Type a line and press `Enter` to broadcast.
- `Ctrl+D` (or `Ctrl+C`) exits. On exit `atcp-live` leaves every member,
  deletes every session it created, and tears down its SSE readers. The
  room itself is left intact for later inspection (its message history is
  persisted in `atcp.db`).

## 5. Observer-only mode

Want to watch a room without being able to inject text? Pass `--no-input`:

```sh
/tmp/atcp-live --socket /tmp/atcp-live.sock --no-input \
  --room-id ROOM_ID \
  --agent codex='codex --chat'
```

The driver will still spawn the agent(s) you pass, but it won't read
stdin. Combine with `--room-id` to attach a new agent to an existing
room.

## Known limitations (fast-path harness)

- **No lease awareness.** `atcp-live` joins members with `CanMutate=true`
  but never calls `/v1/leases/acquire`. That's fine for `msg send`
  broadcasts, but it means you cannot safely send `terminal.input` intents
  to a single agent from this driver. Use `atcpctl` once the terminal
  intents land there.
- **Raw PTY bytes, no screen renderer.** ANSI escape sequences print as
  literal bytes. Agents with full-screen TUIs (curses, alternate screen,
  etc.) will look garbled. Phase 2 of the plan (modetrack + screen
  snapshots) is the fix.
- **No message-history replay.** Late joiners see only what was said after
  they joined. Add `GET /v1/rooms/{id}/messages` when that matters.
- **Per-session SSE only.** One connection per member. A room-level event
  stream (`target=room:ID`) is listed in the plan but not implemented.

## Debug tips

- Check daemon logs (`atcpd` writes to stderr) for `SaveRoom` /
  `SaveMember` / `AppendMessage` errors. Those indicate SQLite issues,
  which also surface in the driver as "atcp client: ...: 500".
- Use `atcpctl --socket /tmp/atcp-live.sock room list` to inspect room
  state from a second shell while the driver is running.
- Drop in `--since-seq 0` (the default) or a specific seq to replay log
  history when reattaching.
