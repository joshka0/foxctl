# ATCP — remaining gap TODO

Live-action follow-ups to [LIVE-SMOKE-FINDINGS.md](LIVE-SMOKE-FINDINGS.md).
Gap 1 (terminal capability responder) shipped in `internal/atcp/broker/termcaps`
(commit `19fe4589`). This file tracks what's left, in priority order, with
enough detail that any of us can pick up any item cold.

Each entry has: **status · symptom · root cause · fix direction · acceptance
criteria · size**. Sizes assume focused work, no meeting overhead.

Status legend: ☐ not started · ▣ in progress · ☑ done · ✎ needs design
decision first.

---

## Next track — Room integrations

**Status:** ☐ · planning captured · **priority: high**

The next major ATCP track is to make rooms first-class integration hubs, not
only PTY fan-out containers. The detailed roadmap lives in
[ROOM-INTEGRATIONS.md](ROOM-INTEGRATIONS.md).

Recommended implementation order:

1. ☐ Canonical room event log with replay/follow.
2. ☐ Emit `message.sent` / `message.delivered` into the room event log.
3. ☐ Add structured room event filters (`kinds`, then `agents`).
4. ☐ Support native inbox-only room participants.
5. ☐ Add durable inbox delivery and ack.
6. ☐ Add signed room webhooks.
7. ☐ Add provider-specific adapters only after webhook/inbox contracts settle.

Acceptance for the first slice:

- ☐ `GET /v1/events?target=room:<id>&since=N` replays room events and then
  follows live events.
- ☐ Room member join/leave and `message.send` append structured room events.
- ☐ Existing point-in-time room terminal fan-in is either backed by the room
  event log or documented as a compatibility path.
- ☐ Raw terminal output remains opt-in for external integrations.

---

## Gap 1b — Codex input loop still inactive after termcaps shipped

**Status:** ☑ · shipped Apr 20, 2026 · follow-up to Gap 1

**Resolved symptom.** After the responder and submit-key work, codex booted
past MCP init and displayed room-injected text, but it needed text and submit
as distinct PTY writes. `Broker.Submit` now writes text, pauses briefly, then
writes the submit key under the same lease. Live smoke observed
`msg send --submit-key KittyEnter` produce `CODEX_ROOM_PONG`.

**Confirmed not the cause.**

- Terminal capability queries — `/tmp/scan_codex_escapes.py` on a post-fix
  session log shows every query codex sent (`ESC[6n`, `ESC[?u`, `ESC[c`,
  `OSC 10`, `OSC 11`) is one the responder answers.
- Approval / sandbox gating — `codex --dangerously-bypass-approvals-and-sandbox`
  behaves identically.
- PTY input visibility — codex now displays injected text and responds after
  a direct leased `KittyEnter` key press.

**Likely causes to investigate (in order of cheapness to test).**

1. **Atomic submit sequencing.** Confirmed. Codex does not reliably treat
   `text + KittyEnter` in one PTY write the same as text followed by a later
   key event.
2. **MCP readiness gating.** Codex still reports `paper`
   (`127.0.0.1:29979`) unreachable. This no longer blocks boot.

**Acceptance.** ☑ Reopen the smoke, broadcast `please respond with "pong"`,
and see codex emit a corresponding TUI update within ≤10 s without requiring
a separate direct key press. Document the root cause in
`LIVE-SMOKE-FINDINGS.md`.

**Size.** 2–4 hours for steps 1–3; step 4 can blow up to a day if the
cause is obscure.

---

## Gap 4 — Agent-readiness signal on the session

**Status:** ☑ · shipped Apr 20, 2026 · **priority: high** (this unblocks
reliable smoke tests for *every* agent, and gives Gap 1b an objective
signal to measure against)

**Symptom.** `POST /v1/messages` returns `delivered: true` the moment the
router's PTY write succeeds, even if the recipient agent was mid-init and
couldn't process input. We have no protocol-level way to say "is codex
ready yet?"; we infer it by eyeballing the SSE byte-rate drop.

**Root cause.** `session.Session` tracks bytes written but not the arrival
rate, and `SessionResponse` has no readiness field.

**Implementation shipped.**

1. `session.Session` tracks cumulative output bytes, a rolling one-second
   output byte rate, and the timestamp of the last non-empty PTY read.
2. `SessionResponse` exposes:
   - `output_bytes_total` (int64, cumulative)
   - `output_rate_bps` (float, rolling bytes/sec)
   - `last_output_at` (RFC3339 timestamp of most recent non-empty read)
3. `GET /v1/sessions/{id}/readiness` returns
   `{"idle": bool, "idle_for_ms": int}` plus the current rate/counter
   fields. Defaults are `threshold_bps=32` and `debounce_ms=500`; both are
   tunable via query params.
4. Session creation accepts a readiness profile. The broker merges explicit
   overrides with adapter defaults; codex, droid, gemini, and claude profiles
   now live in `internal/atcp/adapter/profiles/profiles.json` instead of in
   `atcp-live` or broker control flow.
5. The readiness endpoint also accepts `screen_regex`. When present, the
   session must be output-idle and the rendered screen must match the regex;
   the response includes `screen_match`, `screen_regex`, and `screen_line`.
   When no query regex is provided, the endpoint evaluates the session's
   broker-owned profile.
6. `GET /v1/events?target=session:<id>&ready=true` emits `terminal.ready`
   on readiness state transitions. The stream also emits an initial
   `terminal.ready` state, and a small ticker catches debounce-only transitions
   when output stops.
7. `GET /v1/sessions/{id}/activity` compares a caller-supplied heartbeat
   cursor (`since_seq`, `since_output_bytes_total`) against the current PTY
   output cursor and reports `working`, `output_changed`, `seq_delta`, and
   `output_bytes_delta`.
8. `GET /v1/events?target=session:<id>&activity=true` emits periodic
   `terminal.activity` heartbeats. Each heartbeat compares current output to
   the previous heartbeat, so a watcher can distinguish "quiet and idle" from
   "still producing output".
9. `POST /v1/messages` accepts `await_activity_ms` and `await_ready_ms`.
   When set, each delivered member reports first-output latency and
   return-to-ready completion latency. Completion is PTY-level: output changed
   after delivery and the session returned to its readiness profile.
10. `atcp-live --render` passes `adapter=<agent name>` at session creation and
   relies on the broker-owned readiness profile, printing `[name] ready
   (idle_for=Nms rate=XB/s)` only after byte-rate idle and prompt matching
   both pass.
11. `atcp-live` polls readiness after startup and around stdin broadcasts,
   printing `[name] ready (idle_for=Nms rate=XB/s)` when a session is
   output-idle. Flags: `--readiness-timeout`, `--idle-threshold-bps`,
   `--idle-debounce`.

**Configuration packaging.** ☑ **Shipped Apr 20, 2026.** Built-in readiness
profiles now load from the embedded adapter profile registry at
`internal/atcp/adapter/profiles/profiles.json`. The broker still owns merging
explicit session overrides, but no longer owns the built-in profile table.

**Acceptance.**

- ☑ `session.Session.OutputRate()` returns a non-negative float, decays to
  0 within 2 s of no reads, and rises to sustained output rate within 1 s.
- ☑ `GET /v1/sessions/{id}/readiness` returns `idle:true` only after the
  rate has been below threshold for the full debounce window.
- ☑ Unit test: feed synthetic chunks at known timestamps, assert rate and
  idle transitions.
- ☑ Live re-smoke: `atcp-live` run against a droid session printed
  `[droid] ready` when droid reached its input prompt.
- ☑ HTTP test: `screen_regex=PROMPT%3E` requires the rendered screen to match;
  a non-matching regex keeps `idle:false` even when output is byte-idle.
- ☑ HTTP test: a session readiness profile is exposed on `SessionResponse`
  and is used by `/readiness` when no query regex is provided.
- ☑ SSE test: `ready=true` emits a canonical `terminal.ready` envelope once
  byte-idle debounce and rendered-screen matching both pass.
- ☑ HTTP test: `/activity` reports output deltas from a previous
  `last_seq`/`output_bytes_total` heartbeat cursor.
- ☑ SSE test: `activity=true` emits canonical `terminal.activity`
  heartbeat envelopes and marks output changes.
- ☑ HTTP test: `POST /v1/messages` with `await_activity_ms` and
  `await_ready_ms` reports first-output and completion timing per member.
- ☑ Unit test: the embedded adapter profile registry matches real prompt
  samples for codex, droid, gemini, claude, and the claude-code alias.
- ☑ `atcp-live --render` now passes adapter names at session creation and
  relies on broker-owned codex/droid/gemini/claude readiness profiles.
- ☑ Live re-smoke: `atcp-live --render --agent gemini=gemini
  --agent claude=claude --no-input` printed `[claude] ready` on Claude's
  `❯` prompt and `[gemini] ready` on Gemini's `> Type your message...`
  prompt. A follow-up room message returned `delivered=2 failed=0`.

**Size.** 0.5 day for the cheap version + tests + `atcp-live` plumbing.

---

## Gap 2 — Per-session submit key + kitty-keyboard auto-detection

**Status:** ☑ · shipped Apr 20, 2026 · **priority: medium**

**Symptom.** `adapter.CompileSubmit` defaults to `Enter → 0x0D` (`\r`).
Some TUIs (Go/Rust raw-mode handlers, or agents that pushed the kitty
keyboard protocol) expect `\n` or the kitty-encoded Enter (`ESC[13u`).
Today we'd have to patch adapter code to change it.

**Root cause.** No per-session override, no detection of kitty-protocol
state. `adapter.Adapter.CompileSubmit` is a pure function of the key name,
ignoring runtime terminal mode.

**Implementation shipped.**

1. `CreateSessionRequest` accepts `submit_key`; `SessionResponse` echoes it.
   The generic adapter uses it as the default when a `TerminalSubmit` intent
   leaves `submit_key` blank.
2. `modetrack` detects kitty keyboard push/pop (`CSI > n u`, `CSI < u`).
   The broker mirrors that mode into the session adapter; empty submit keys
   compile to `KittyEnter` while kitty mode is active.
3. The generic default submit key is now `EnterLineFeed` (`\r\n`) for broad
   shell/TUI compatibility when no explicit key and no kitty mode are active.
4. `atcpctl msg send --submit-key KittyEnter` and the HTTP
   `SendMessageRequest.submit_key` field pass per-message overrides through
   the room router.

**Acceptance.**

- ☑ Unit test: session/adapter with `submit_key: "LineFeed"` → `CompileSubmit`
  returns `\n`.
- ☑ Unit test: feed `ESC[>9u` to modetrack → kitty keyboard mode is
  true → `CompileSubmit` returns `ESC[13u` without explicit override.
- ☑ Live smoke: codex accepted `msg send --submit-key KittyEnter` and
  rendered `CODEX_ROOM_PONG`.
- ☑ `atcpctl msg send --submit-key KittyEnter` works as an override.

**Size.** 0.5 day.

---

## Gap 3 — Phase 2 screen-snapshot rendering (vt engine)

**Status:** ☑ · minimum useful shipped Apr 20, 2026 · **priority: medium**

**Symptom.** `atcp-live`'s stdout is a firehose of raw PTY bytes.
30 seconds of droid = ~4,900 distinct SSE chunks, each a partial ANSI
redraw. Stripping CSI codes with `sed` is useless — you get frame
fragments. Human observation is impossible; pattern-matching for agent
output (needed for Gap 6) is impossible.

**Root cause.** We broadcast raw PTY bytes. There's no interpretation
layer anywhere in the broker. Plan §6 Phase 2 describes the goal
(`terminal.screen.snapshot`) but leaves the engine unbuilt.

**Decision.** Start with the strict-subset implementation, not a vendored VT
emulator. Revisit a dependency only if the live smoke exposes escape
sequences that the subset cannot cover cleanly.

- **(a) Vendor a VT emulator.** `github.com/hinshun/vt10x`,
  `github.com/charmbracelet/x/vt`, or `github.com/gdamore/tcell/v2`'s
  screen. Pro: stable, handles the full spec. Con: adds ~3-5 KLOC of
  dependency; some of them drag in a lot.
- **(b) Implement a strict subset.** ASCII + common CSI (CUP/EL/ED/SGR)
  + alt-screen buffer + scrollback. Pro: no new dependency, fits in
  ~800 LoC, tailored to our needs. Con: we own the edge cases forever.

**Implementation shipped (minimum useful).**

1. `internal/atcp/broker/vtscreen` keeps a per-session grid with primary and
   alt screen buffers. It applies printable UTF-8, CR/LF/backspace/tab,
   common CSI cursor movement, erase display/line, SGR ignore, and alt-screen
   enter/exit on each PTY read.
2. `GET /v1/sessions/{id}/screen` returns
   `{rows, cols, lines, dirty_rows, cursor, alt_screen}`.
3. `atcp-live --render` polls screen snapshots and prints changed rendered
   lines instead of raw ANSI chunks.
4. `GET /v1/events?target=session:<id>&screen=true` emits
   `terminal.screen.snapshot` SSE frames alongside `terminal.output`, so
   clients can subscribe to rendered screen state without polling.

**Deferred from the original full Phase 2 scope.** Cell-style arrays are still
future work; the event stream currently emits line-string snapshots, matching
the polling endpoint.

**Acceptance.**

- ☑ Unit: feed canonical CSI sequences (CUP to 10;10, `hello`, CR, `world`)
  → assert grid state.
- ☑ Unit: alt-screen enter/exit preserves the primary screen.
- ☑ Live smoke: droid smoke run with `atcp-live --render` produced a
  readable transcript of droid's prompts and responses.
- ☑ Byte budget: screen endpoint uses line strings, not per-cell objects.
- ☑ SSE test: `screen=true` stream emits a canonical
  `terminal.screen.snapshot` envelope whose rendered lines contain live PTY
  output.

**Size.** 2–3 days (implementation + tests + atcp-live integration). Gate
on dedicated time.

---

## Gap 5 — Pre-smoke operator checklist

**Status:** ☑ · shipped Apr 20, 2026 · **priority: low**

**Symptom.** Codex's failed `paper` MCP caused the whole init log to
bloat, masking the real termcap issue for ~20 minutes of debugging. An
operator checklist would save time.

**Implementation shipped.** New doc `docs/atcp/PRESMOKE-CHECKLIST.md` covers:

- Verify every registered MCP server is reachable (`codex mcp list`,
  `droid mcp list`, equivalent) — or disable failing ones.
- Confirm `$TERM` is `xterm-256color` in the shell that launches `atcpd`.
- Confirm the socket path is fresh (`rm -f $SOCK` before daemon start).
- Have a second shell ready with `atcpctl --socket $SOCK` pointing at the
  same socket, to inspect room/session state without disturbing the
  driver.
- Know how to kill everything cleanly (`pkill -INT atcpd; pkill -INT
  atcp-live`).

Also added a `--warmup-timeout <dur>` flag to `atcp-live` that prints a
warning like `[codex] no new output for 10s — likely stuck in init` once
a session emits nothing new for the given window.

**Acceptance.** ☑ The doc exists, is linked from `LIVE-SMOKE.md`, and the
`--warmup-timeout` flag is implemented with a regression test.

**Size.** 30 min for the doc + 1 hour for the flag.

---

## Gap 6 — Inter-agent talkback

**Status:** ☑ · simple variant shipped Apr 20, 2026 · **priority: medium**

**Symptom.** The only message source today is a human injecting text via
`atcpctl msg send` or `atcp-live` stdin. There's no path for codex's
output to become a room message droid sees.

**Root cause.** Sessions are sinks (you write into them, they render) but
not sources of typed events. Their output flows to SSE subscribers, not
back through the router.

**Implementation shipped (simple variant).** "Opt-in talkback" in
`atcp-live`, not in the broker.

1. New `atcp-live` flag `--talkback <name>=<prefix>` (repeatable, default
   off). When a line from agent `<name>` contains `<prefix>` after only
   whitespace / punctuation / symbol TUI decoration (e.g. codex rendering
   `• @room:`), strip the prefix and forward the rest as
   `msg send --source <name>`.
2. Message delivery now returns a structured `receipt` and prepends a
   terminal-visible receipt by default. The receipt includes `message_id`,
   `room_id`, optional `correlation_id` / `reply_to_message_id`, and a
   structured `reply_prefix` such as `@room:01K... ` so native clients know
   which room to target. The terminal preamble renders this as
   `reply_prefix_hint` (`<AT>room:01K... `) to avoid opening `@` mention
   popups while the preamble is typed into TUIs.
3. Talkback accepts either old single-room output (`@room: hello`) or explicit
   room output (`@room:01K... hello`). Explicit room IDs are parsed as ULIDs,
   not by keyword matching.
4. Works in both raw line mode and `--render` mode; `--render` is preferred
   for full-screen TUIs because it matches clean snapshot lines.
5. Document that agent prompts (in codex / droid system prompts) should
   include "when you want to talk to a peer, emit the receipt's
   `reply_prefix`, or replace `<AT>` in `reply_prefix_hint` with the at-sign
   character, followed by your message".

**Fix direction (ambitious, deferred).** Run an ATCP client inside each
agent's environment. Either:

- A custom MCP server per-agent that exposes `atcp.send`, so the LLM can
  call it as a tool. Belongs to the agents' config, not our repo.
- `AGENTCTL_CONTROL_SOCK` env + a helper binary on the agent's `$PATH`
  that wraps `atcpctl msg send`. Already planned in §6 Phase 6 as the
  "native adapter side-channel". Reuse that plumbing.

**Acceptance (simple variant).** ☑ Smoke run with
`--talkback speaker=@room:` forwarded `hello-from-speaker` from a local PTY
speaker to a `cat` listener through the room router. ☑ Live codex-to-droid
smoke with `--talkback codex=@room:` forwarded codex-rendered
`• @room: Hello Droid from Codex` to droid as `Hello Droid from Codex`. ☑
Receipt-visible rerun proved the structured API `reply_prefix` plus terminal
`reply_prefix_hint` lets codex emit explicit-room talkback that droid receives,
without triggering droid's `@` file picker during preamble delivery.

**Size.** 0.5 day *after* Gap 3 is stable.

---

## Gap 7 — `terminal/write_bytes` opt-in

**Status:** ☑ · shipped Apr 20, 2026 · **priority: low**

**Symptom.** `POST /v1/terminal/write_bytes` rejects every call with
`"write_bytes capability is disabled"`. The safety choice is correct, but
the opt-in mechanism isn't wired for trusted callers.

**Implementation shipped.**

1. Added `enable_raw_bytes: bool` to `CreateSessionRequest` (default false).
2. `generictty.Adapter` consults session config when compiling the
   `write_bytes` intent and only rejects when both the session flag and
   any adapter-level policy are off.
3. Documented in `LIVE-SMOKE.md` the one-liner for enabling it and why the
   default is off.
4. `atcpctl session create --enable-raw-bytes` exposes the opt-in for trusted
   diagnostic sessions.

**Acceptance.** ☑ Unit test: session created without the flag →
`write_bytes` is rejected; session created with the flag → accepted and
bytes appear on the PTY.

**Size.** 1 hour.

---

## Gap 8 — Long input / paste support in `atcp-live`

**Status:** ☑ · shipped Apr 20, 2026 · **priority: low**

**Symptom.** `atcp-live`'s stdin forwarder uses `bufio.Scanner` with a
1 MiB buffer; multi-MB pastes silently drop. `atcpctl msg send`'s HTTP
client has a 30 s timeout that caps very-long-prompt sends.

**Implementation shipped.** `atcp-live`'s stdin scanner buffer is now 16 MiB
and the ATCP HTTP client timeout is now 120 s. The room router sends
messages >=1 MiB through `terminal.paste` with `bracketed:"auto"` and
`submit_after:true`, so sessions with bracketed paste enabled receive a
single bracketed paste.

**Acceptance.** ☑ Unit coverage proves large room messages use
`terminal.paste` with bracketed auto mode. The 5 MB droid paste remains a
manual stress test rather than a blocker for the TODO.

**Size.** Half a day, alongside Gap 3.

---

## Hardening — Room message history replay

**Status:** ☑ · shipped Apr 20, 2026 · **priority: medium**

**Symptom.** Late joiners and operators had to infer room history from live
terminal output. The daemon already persisted message audit rows when SQLite
was enabled, but there was no replay endpoint and no in-memory history for
the default volatile daemon mode.

**Implementation shipped.**

1. The broker keeps an in-memory append-only message audit log for the running
   daemon. When a SQLite store is configured, broker startup hydrates that log
   from persisted rows.
2. `storage.Store` now exposes `LoadMessages(ctx, roomID, limit)`.
   The SQLite implementation returns chronological messages and per-member
   delivery outcomes without deadlocking its single-connection setup.
3. `GET /v1/rooms/{id}/messages?limit=N` returns replayable message audit
   records. `limit=0` means all messages; default is 100.
4. `atcpctl room messages ROOM_ID --limit N` exposes the replay endpoint from
   the shell.

**Acceptance.**

- ☑ Broker test: `SendMessage` appends to the in-memory audit log and
  `ListMessages` returns the sent message with member delivery records.
- ☑ SQLite test: `AppendMessage` then `LoadMessages` round-trips message
  metadata and delivery rows.
- ☑ HTTP test: `/v1/rooms/{id}/messages` returns the fan-out sent during the
  room happy path.

**Size.** 0.5 day.

---

## Hardening — Room event stream fan-in

**Status:** ☑ · shipped Apr 20, 2026 · **priority: medium**

**Symptom.** Operators needed one SSE connection per session to observe a
room, which made multi-agent smoke tests noisy and forced clients to discover
members before subscribing.

**Implementation shipped.**

1. `GET /v1/events?target=room:<id>` now fans in `terminal.output` events
   from the active members present when the stream opens.
2. Room stream output bodies include `room_id`, `agent_id`, and `session_id`
   while preserving canonical ATCP envelopes.

**Acceptance.**

- ☑ SSE test: a room stream receives output from a joined `cat` session and
  emits a canonical `terminal.output` envelope targeted at the room.

**Known limitation.** Membership is point-in-time; reconnect after joins or
leaves to refresh the fan-in set.

---

## Hardening — Terminal delivery policy

**Status:** ☑ · shipped Apr 20, 2026 · **priority: high**

**Symptom.** `message.send` always typed into recipients immediately. That is
useful for smoke tests but unsafe for busy full-screen TUIs.

**Implementation shipped.**

1. `SendMessageRequest` accepts `terminal_policy`, `policy_timeout_ms`, and
   `interrupt_key`.
2. Policies:
   - `immediate` / empty: current write-now behavior.
   - `queue`: wait for the recipient's readiness profile before delivery.
   - `safe-prompt-only` / `reject`: fail that member unless it is already
     ready.
   - `interrupt`: send `interrupt_key` before delivery. Empty defaults to
     `Escape`, matching the common "close transient UI, then send" path.
3. Policy failures are per-member delivery failures; fan-out still attempts
   the other recipients.
4. `atcpctl msg send` exposes `--terminal-policy`, `--policy-timeout`, and
   `--interrupt-key`.

**Acceptance.**

- ☑ Router test: `safe-prompt-only` refuses a busy recipient without writing
  the message.
- ☑ Router test: `interrupt` sends `Escape` before the message submit.

---

## Priority ladder (quick reference)

1. ☑ **Gap 4** — readiness signal (0.5 d) · unblocks every other smoke
2. ☑ **Gap 2** — per-session submit key + dual-submit default (0.5 d)
3. ☑ **Gap 3** — vt screen engine minimum useful slice
4. ☑ **Gap 6** — talkback simple variant
5. ☑ **Gap 1b** — codex input-loop submit sequencing
6. ☑ **Gap 7** — write_bytes opt-in (1 h)
7. ☑ **Gap 5** — pre-smoke checklist + warmup-timeout (~1.5 h total)
8. ☑ **Gap 8** — paste / long-input
9. ☑ **Hardening** — room message history replay
10. ☑ **Hardening** — room event stream fan-in
11. ☑ **Hardening** — terminal delivery policy

Total to close everything through Gap 6: ~5 focused days.

---

## Cross-references

- Root-cause narrative per gap: [LIVE-SMOKE-FINDINGS.md](LIVE-SMOKE-FINDINGS.md)
- Runbook for re-running the smoke: [LIVE-SMOKE.md](LIVE-SMOKE.md)
- Spec + Phase plan: [ATCP-v0.1.md](ATCP-v0.1.md) and
  [`../plans/atcp-rooms-replacement.md`](../plans/atcp-rooms-replacement.md)
- Gap 1 implementation: `internal/atcp/broker/termcaps/{responder.go,responder_test.go}`,
  wired at `internal/atcp/broker/session/session.go:279-313` (commit `19fe4589`).
