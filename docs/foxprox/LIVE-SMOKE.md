# ATCP live smoke — codex + droid + gemini + claude

A step-by-step guide for driving a live multi-agent ATCP room from the
shell. Uses the `atcpd` daemon and the `atcp-live` driver binary. No
client-library work required — this is the "fast path" harness while we
decide what to promote into `atcpctl` proper.

## Prerequisites

- `atcpd` binary built: `go build -o /tmp/atcpd ./cmd/atcpd`
- `atcp-live` binary built: `go build -o /tmp/atcp-live ./cmd/atcp-live`
- Agent CLIs on `$PATH`: `codex`, `droid`, `gemini`, `claude` (or whatever
  names / commands you want to coordinate).
- Preflight complete: [PRESMOKE-CHECKLIST.md](PRESMOKE-CHECKLIST.md)

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

## 3. Launch the room with agents

```sh
/tmp/atcp-live \
  --socket /tmp/atcp-live.sock \
  --workspace live \
  --room-title "codex+droid+gemini+claude" \
  --render \
  --warmup-timeout 10s \
  --agent codex='codex --no-alt-screen' \
  --agent droid='droid' \
  --agent gemini='gemini' \
  --agent claude='claude' \
  --source human
```

What happens:

1. `atcp-live` hits `/v1/health` to confirm the daemon is up.
2. It creates a room (`workspace=live, title="codex+droid+gemini+claude"`) and
   prints the room id.
3. For each `--agent NAME=CMD` it POSTs a session (the daemon spawns the
   PTY) and joins the session to the room with `CanMutate=true`, so every
   member both receives and is addressable by the fan-out router.
4. It tails each agent. By default this uses raw
   `GET /v1/events?target=session:<id>` terminal-output SSE chunks. Operators
   can also subscribe once to `GET /v1/events?target=room:<id>` to fan in
   terminal output for all active room members. With `--render`, the driver
   polls `GET /v1/sessions/{id}/screen` and prints readable virtual-screen
   lines instead of raw ANSI bytes.
5. It polls `GET /v1/sessions/{id}/readiness` until each spawned session is
   output-idle, then prints `[name] ready (idle_for=Nms rate=XB/s)`.
6. Whatever you type on `atcp-live`'s stdin is forwarded — one line at a
   time — as a room message with `source="human"`. Every mutable member
   receives it. The driver polls readiness before and after each broadcast
   so a busy TUI is less likely to receive text mid-init or mid-render.

## 4. Interact

- Type a line and press `Enter` to broadcast.
- Readiness defaults are `--readiness-timeout 30s`,
  `--idle-threshold-bps 32`, and `--idle-debounce 500ms`. Pass
  `--readiness-timeout 0` to disable startup/broadcast idle waiting.
- With `--render`, readiness uses the session's broker-owned adapter profile.
  Current built-in profiles cover codex, droid, gemini, and claude. A session
  is considered ready only when output is byte-idle and the rendered screen
  matches the agent's prompt pattern. Built-in prompt patterns live in
  `internal/atcp/adapter/profiles/profiles.json`.
- `--render` prints screen snapshots instead of raw PTY byte chunks. This is
  the better default for full-screen TUIs.
- `--talkback codex=@room:` forwards rendered or line-buffered output from
  `codex` that contains `@room:` after only TUI decoration (for example
  `• @room:`) as a room message with `source=codex`.
- Room messages include a visible ATCP receipt preamble by default. It carries
  `message_id`, `room_id`, optional correlation/reply-to metadata, and a
  `reply_prefix` such as `@room:01K... ` so terminal-only agents know which
  room to talk back to. Terminal preambles render that as
  `reply_prefix_hint` (`<AT>room:01K... `) to avoid triggering `@` mention
  popups while typing into TUIs; agents should replace `<AT>` with the
  at-sign character before printing a talkback line. Use
  `atcpctl msg send --no-receipt-preamble` only for low-level diagnostics
  where the exact terminal payload matters.
- Lines up to 16 MiB are accepted. Room messages >=1 MiB are delivered via
  `terminal.paste` with bracketed-paste auto detection.
- `Ctrl+D` (or `Ctrl+C`) exits. On exit `atcp-live` leaves every member,
  deletes every session it created, and tears down its SSE readers. The
  room itself is left intact for later inspection. Message history is kept in
  the running daemon and is persisted in `atcp.db` when `atcpd --data-dir` is
  used.

## 5. Observer-only mode

Want to watch a room without being able to inject text? Pass `--no-input`:

```sh
/tmp/atcp-live --socket /tmp/atcp-live.sock --no-input \
  --room-id ROOM_ID \
  --agent codex='codex --no-alt-screen'
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
- **Raw PTY mode is still available.** ANSI escape sequences print as literal
  bytes unless `--render` is enabled. The built-in renderer is a strict VT
  subset; keep raw mode available when debugging unsupported escape sequences.
- **Raw byte writes are opt-in.** Create a trusted diagnostic session with
  `atcpctl session create --cmd "..." --enable-raw-bytes` before using
  `POST /v1/terminal/write_bytes`. The default is off because raw writes
  bypass typed key/paste safety.
- **Room SSE membership is point-in-time.** `target=room:ID` currently fans in
  members active when the stream opens. Reconnect after joins/leaves if you
  need a fresh member set.

## Debug tips

- Check daemon logs (`atcpd` writes to stderr) for `SaveRoom` /
  `SaveMember` / `AppendMessage` errors. Those indicate SQLite issues,
  which also surface in the driver as "atcp client: ...: 500".
- Use `atcpctl --socket /tmp/atcp-live.sock room list` to inspect room
  state from a second shell while the driver is running.
- Replay room message history:
  `atcpctl --socket /tmp/atcp-live.sock room messages ROOM_ID --limit 50`.
  This is useful for late joiners and for verifying fan-out without scraping
  terminal output.
- Query readiness directly when debugging a stuck session:
  `curl --unix-socket /tmp/atcp-live.sock 'http://atcp/v1/sessions/SESSION_ID/readiness'`.
  Add `&screen_regex=...` to require a rendered-screen prompt match; the
  response includes `screen_match` and `screen_line`. Without a query regex,
  the endpoint uses the session's configured readiness profile.
- Query the rendered screen directly:
  `curl --unix-socket /tmp/atcp-live.sock 'http://atcp/v1/sessions/SESSION_ID/screen'`.
- Subscribe to rendered screen events:
  `curl --unix-socket /tmp/atcp-live.sock 'http://atcp/v1/events?target=session:SESSION_ID&screen=true'`.
  The stream still includes `terminal.output`; `terminal.screen.snapshot`
  frames carry `{session_id, screen}` with line-string snapshots.
- Subscribe to all active room members' terminal output:
  `curl --unix-socket /tmp/atcp-live.sock 'http://atcp/v1/events?target=room:ROOM_ID'`.
  Room stream `terminal.output` bodies include `room_id`, `agent_id`, and
  `session_id`.
- Subscribe to readiness events:
  `curl --unix-socket /tmp/atcp-live.sock 'http://atcp/v1/events?target=session:SESSION_ID&ready=true'`.
  The stream emits `terminal.ready` on readiness transitions and includes the
  current readiness state when the subscription starts.
- Query an activity heartbeat directly:
  `curl --unix-socket /tmp/atcp-live.sock 'http://atcp/v1/sessions/SESSION_ID/activity?since_seq=LAST_SEQ&since_output_bytes_total=LAST_BYTES'`.
  The response reports `output_changed`, `seq_delta`, and
  `output_bytes_delta` so operators can tell whether the agent did work since
  their previous heartbeat.
- Subscribe to activity heartbeats:
  `curl --unix-socket /tmp/atcp-live.sock 'http://atcp/v1/events?target=session:SESSION_ID&activity=true&activity_interval_ms=1000'`.
  The stream emits `terminal.activity` frames even when output does not change.
- Measure per-message response timing:
  `atcpctl --socket /tmp/atcp-live.sock msg send --room ROOM_ID --text "ping" --await-activity 10s --await-ready 30s`.
  Each delivered member reports whether output changed, first-output latency,
  and return-to-ready completion latency.
- Choose a terminal delivery policy:
  `atcpctl --socket /tmp/atcp-live.sock msg send --room ROOM_ID --text "ping" --terminal-policy queue --policy-timeout 30s`.
  `queue` waits for readiness, `safe-prompt-only`/`reject` fail per member
  unless the session is already ready, and `interrupt` sends `Escape` before
  delivery unless `--interrupt-key` overrides it.
- Drop in `--since-seq 0` (the default) or a specific seq to replay log
  history when reattaching.

## Related docs

- [LIVE-SMOKE-FINDINGS.md](LIVE-SMOKE-FINDINGS.md) — narrative write-up
  of the first real multi-agent smoke, with root-cause analysis per gap.
- [TODO.md](TODO.md) — live tracker of remaining gaps with status, fix
  direction, acceptance criteria, and size estimates. Start here when
  picking up the next slice.
- [PRESMOKE-CHECKLIST.md](PRESMOKE-CHECKLIST.md) — operator preflight
  before running a real agent smoke.
