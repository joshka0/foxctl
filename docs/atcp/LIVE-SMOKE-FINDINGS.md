# ATCP live-smoke findings (codex + droid)

Apr 20, 2026 — ran the first interactive smoke against a real two-agent room
using `cmd/atcpd` + `cmd/atcp-live` + `codex-cli 0.121.0` + `droid 0.104.0`.
Messages were injected from a second shell via `atcpctl msg send`.

Apr 20 follow-up — after the Gap 2/3/4/5/6/7/8 implementation pass, reran
the smoke with `atcpd`, `atcp-live --render`, `codex --no-alt-screen`, and
`droid`.

Additional proof points:

1. **Rendered screen snapshots are usable.** `atcp-live --render` produced
   readable droid and codex screen lines through `GET /v1/sessions/{id}/screen`.
2. **Readiness works for real agents.** Both codex and droid printed
   `[name] ready` after their output rate dropped below the configured
   readiness threshold.
3. **Room fan-out still works.** `atcpctl msg send --submit-key EnterLineFeed`
   returned `delivered=2 failed=0`; both sessions showed the injected prompt.
4. **Droid end-to-end response works.** Droid received the prompt and rendered
   `ATCP_PONG`.
5. **Codex input is fixed by paced submit writes.** Codex booted past MCP
   init and displayed room-injected text. An intermediate smoke showed that a
   concatenated text+key write left text in the composer, while a later direct
   `KittyEnter` submitted it. `Broker.Submit` now writes text and submit key
   as distinct PTY writes under the same lease; a follow-up smoke with
   `msg send --submit-key KittyEnter` rendered `CODEX_ROOM_PONG`.
6. **Talkback works in the harness.** A local PTY speaker emitting
   `@room: hello-from-speaker` with `--talkback speaker=@room:` forwarded a
   room message that a `cat` listener received as `hello-from-speaker`.

Known live-smoke caveats from the follow-up:

- `codex --chat` is not valid for codex-cli 0.121.0; use plain `codex` or
  `codex --no-alt-screen`.
- `droid run` is not interactive boot; it starts droid with `run` as the
  initial prompt. Use plain `droid` for the room smoke.
- The strict VT renderer still prints some OSC/title fragments and box-drawing
  mojibake; it is readable enough for smoke evidence, but not yet a polished
  terminal renderer.

Apr 20 no-paper rerun — after removing the unreachable `paper` MCP from
codex config:

- `codex mcp list` showed no `paper` entry before the run.
- Codex booted without the prior MCP failure warning.
- Codex and droid both reached readiness in the same rendered room.
- `atcpctl msg send --submit-key KittyEnter` delivered to both agents with
  `delivered=2 failed=0`.
- Codex rendered `CLEAN_ATCP_PONG`.
- Droid rendered `CLEAN_ATCP_PONG`.

Apr 20 codex-to-droid talkback rerun — with `paper` still absent and
`atcp-live --talkback codex=@room:`:

- A patched fan-out smoke delivered `PATCHED_FANOUT_PONG` to both agents:
  `delivered=2 failed=0`, with codex rendering `PATCHED_FANOUT_PONG` and
  droid rendering `PATCHED_FANOUT_PONG`.
- A codex-only message used `skip_agents:["droid"]` and returned
  `delivered=1 failed=0`, proving the initial injection went only to codex.
- Codex rendered `• @room: Hello Droid from Codex`; the rendered bullet
  decoration was tolerated by the talkback matcher.
- Droid received the bridged room message and rendered `Hello Droid from
  Codex`, then responded with `Hello! How can I help you today?`.

Apr 20 receipt-visible rerun — after adding structured receipts and the
terminal preamble:

- Default `message.send` returned a structured receipt with `message_id`,
  `room_id`, `source`, `correlation_id`, and structured
  `reply_prefix:"@room:<room_id> "`.
- Initial terminal rendering used literal `@room:` in the preamble. Droid
  accepted the message and rendered `RECEIPT_FANOUT_PONG`, but briefly opened
  its `@` file-mention UI while the preamble was being typed.
- Terminal preamble was changed to render `reply_prefix_hint:"<AT>room:<room_id>
  "` and instruct agents to replace `<AT>` with the at-sign character. The
  structured API receipt still carries the exact `reply_prefix`.
- Safe rerun delivered the fan-out message with `delivered=2 failed=0`.
  Codex rendered `@room:<room_id> RECEIPT_SAFE_FANOUT_PONG`; droid rendered
  the safe preamble and later rendered
  `@room:<room_id> RECEIPT_SAFE_FANOUT_PONG` without opening the file picker.
- Codex-only message with `skip_agents:["droid"]` returned
  `delivered=1 failed=0`. Codex rendered
  `@room:<room_id> RECEIPT_SAFE_TALKBACK_HELLO`; `atcp-live` forwarded that
  explicit-room talkback, and droid rendered
  `@room:<room_id> RECEIPT_SAFE_TALKBACK_HELLO`.

Apr 20 gemini/claude profile rerun — after moving readiness ownership into
broker adapter profiles:

- `atcp-live --render --agent gemini=gemini --agent claude=claude --no-input`
  spawned both agents in one room.
- Claude reached readiness on its rendered `❯` prompt after the profile was
  extended to match that prompt glyph.
- Gemini reached readiness on its rendered `> Type your message or
  @path/to/file` prompt after its update/auth noise settled.
- A room fan-out message returned `delivered=2 failed=0` with both members
  listed as delivered.
- The fan-out payload used the default visible receipt preamble, so both TUIs
  treated it as a real user prompt and began working. For profile-only smoke,
  prefer `--no-input` plus readiness checks, or use
  `atcpctl msg send --no-receipt-preamble` only when deliberately testing
  raw delivery.

## What we proved works

1. **Daemon + socket + transport stack.** `atcpd` booted, bound its Unix
   socket, and served every HTTP endpoint. Memory was stable at ~18 MiB
   RSS across 30+ seconds of full-TUI rendering from two agents.
2. **PTY spawning.** `CreateSession` with `Cmd=[codex]` / `Cmd=[droid]`
   successfully spawned both CLIs as children of `atcpd`, with inherited
   env (so `codex` found its credentials without extra wiring).
3. **Room fan-out with lease gating.** `POST /v1/messages` delivered
   `delivered=2 failed=0` on every attempt. The router acquired a
   `terminal.input` lease per recipient, wrote through `CompileSubmit`,
   and released the lease — unmodified since the P0 fixes landed.
4. **Droid end-to-end message processing.** Droid's TUI rendered
   normally, received our typed text as an input prompt (`⛬  Please just
   reply...` appeared in its input pane), and entered a `Streaming...`
   state with its LLM backend. We didn't capture the completion in this
   run but the full request/response loop through ATCP was clearly
   engaged.
5. **SSE per-session stream.** 4,938 distinct `[droid]`-prefixed chunks
   flowed from daemon → `atcp-live` without drops, over a long-lived
   socket connection. No timeouts, no buffer stalls.
6. **Room / session / member state reachable via `atcpctl`.** While the
   run was live, a second shell could `atcpctl room list`,
   `atcpctl session list`, and send messages without disturbing the
   driver.

## What broke / the real gap list

### Gap 1 — Codex produced almost no output and never processed our input

**Symptom.** Across two runs, codex emitted only its startup banner
(model, cwd, MCP init log) and then went silent. Sending `text\r` (CR,
default submit), `text\n` (LF via `submit_key=LineFeed`), and attempts
via both `msg send` and raw `/v1/terminal/submit` all returned
`written=N` but produced zero new output on the codex session's SSE
stream. The codex process was alive and in `S+` state — sleeping in its
input loop — but never reading.

**Likely root cause.** Codex's startup bytes include terminal capability
queries we never answer:

- `ESC [ > 7 u` — push kitty keyboard protocol flags
- `ESC ] 10 ; ? ESC \` — OSC 10: "query foreground color"
- OSC 0 window-title sets that imply xterm capability

Real terminals auto-respond to OSC 10 with a color string; our broker's
PTY master just sits there. Codex may be blocking pending a reply
before it finishes initializing the input dispatcher. (Droid did not
send these queries and was fine.)

**Ruled out.** `codex --dangerously-bypass-approvals-and-sandbox`
produces the exact same behaviour (stuck at MCP 12/13 spinner, zero
output bytes after `msg send`). The block is not approval / sandbox
gating; it's genuinely the unanswered terminal capability query.

**Status — shipped in `internal/atcp/broker/termcaps`.** A stateful
ECMA-48 parser now sits on the PTY read path in `session.run`; on every
chunk it inspects output bytes for the known query sequences and writes
canonical responses back to the PTY master. Wired queries are OSC
10/11/12 (foreground / background / cursor color), DSR 5 (status), DSR
6 (cursor position), DA1, DA2, and the kitty keyboard query. 16 unit
tests in `internal/atcp/broker/termcaps/responder_test.go` lock the
exact response bytes and the parser's resumability across partial reads.

Re-run evidence (same codex binary, same MCP set):

- Before: codex emitted ~155 KB in 20 s, spinner re-drawing MCP init
  forever, zero new bytes after `msg send`.
- After: codex emits ~3 KB, goes quiet after MCP init, all queries
  codex actually sends (verified with `/tmp/scan_codex_escapes.py`)
  are ones the responder handles.

What is *still* broken is a separate issue: even though codex is no
longer burning CPU in an init busy-loop, its input loop is still
silent — a single-byte write via `/v1/terminal/text` produces no echo.
That's not a terminal-capability gap; it's codex-specific init
plumbing (most likely MCP readiness gating or a fallback into
non-interactive mode because of some other missing env signal). It
wants its own investigation, not more termcap surface.

**Fix direction.** The broker needs a minimal "terminal responder" sitting
on the PTY master side that answers a conservative set of queries with
sane defaults:

- OSC 10/11/12 color queries → reply with fixed RGB defaults
- DSR 5 (device status) → reply `ESC[0n`
- DSR 6 (cursor position) → reply `ESC[1;1R` (or track via modetrack)
- DA1 / DA2 (device attributes) → reply with a xterm-compatible string

This is a small, bounded addition (≤200 lines) that belongs next to
`internal/atcp/broker/modetrack`. It's plan-Phase-2-adjacent but should
probably come first since it's blocking on booting widely-used agents.

### Gap 2 — Default submit key is wrong for modern TUI agents

**Symptom.** `CompileSubmit` defaults to `Enter → 0x0D` (`\r`). Droid
accepted it; codex ignored it (though see Gap 1 — codex was ignoring
everything). Many TUIs written in Go / Rust with raw-mode keyboard
handling expect `\n`, or the kitty-encoded Enter
(`ESC[13u` when the keyboard protocol is pushed).

**Status — shipped Apr 20, 2026.** Submit keys are configurable
per-session (`submit_key`) and per room message (`msg send --submit-key`).
`modetrack` detects kitty keyboard push/pop and the broker mirrors that
state into the generic adapter, so empty submit keys emit `KittyEnter`
while kitty mode is active. The fallback default is now `\r\n`.

### Gap 3 — Observer sees raw PTY bytes; rendering is unusable for humans

**Symptom.** The `atcp-live` stdout was dominated by ANSI escape codes
and alt-screen redraws. Over 30 seconds, droid emitted ~4,900 chunks,
each being a partial redraw of a TUI frame. Without interpretation, a
human observer can't read any of it. Even after stripping CSI with
`sed`, the output is just fragments from the TUI's frame-buffer
gymnastics.

**Status — minimum useful shipped Apr 20, 2026.** The broker now maintains
`internal/atcp/broker/vtscreen`, a strict VT subset with primary/alt buffers,
printable UTF-8, common cursor/erase CSI handling, and SGR ignore.
`GET /v1/sessions/{id}/screen` returns rendered lines and dirty rows;
`GET /v1/events?target=session:<id>&screen=true` emits
`terminal.screen.snapshot` SSE frames; `atcp-live --render` prints rendered
snapshots instead of raw bytes.

Still deferred from full Phase 2:

- style/cell arrays,
- expanding the parser if real agent output needs more VT coverage.

### Gap 4 — No "agent readiness" signal

**Symptom.** `msg send` returned `delivered=true` to codex when codex
was mid-MCP-init and unable to process input. We'd never know from the
return value that our message landed in a black hole. In the second
run I could only infer codex was "done booting" by watching the log
file's byte rate drop to zero.

**Fix direction.** Either:
- **(cheap)** ☑ **Shipped Apr 20, 2026.** `SessionResponse` now exposes
  `output_bytes_total`, `output_rate_bps`, and `last_output_at`.
  `GET /v1/sessions/{id}/readiness` returns the output-idle heuristic,
  and `atcp-live` prints `[name] ready` after startup/broadcast idle
  waits.
- **(screen-aware)** ☑ **Shipped Apr 20, 2026.** The readiness endpoint now
  accepts `screen_regex`; when present, the session must be output-idle and a
  rendered screen line must match. `atcp-live --render` uses codex/droid
  prompt patterns so readiness is no longer byte-rate only.
- **(broker-owned)** ☑ **Shipped Apr 20, 2026.** Session creation carries a
  readiness profile; the broker provides codex/droid/gemini/claude adapter defaults;
  `/readiness` evaluates the profile when no query regex is supplied; and
  `GET /v1/events?target=session:<id>&ready=true` emits `terminal.ready`
  events on readiness state transitions.
- **(profile packaging)** ☑ **Shipped Apr 20, 2026.** Built-in prompt
  profiles now live in `internal/atcp/adapter/profiles/profiles.json` and the
  broker consumes them through the adapter profile registry instead of owning
  the table directly.

### Gap 5 — MCP failures during agent boot silently break the smoke

**Symptom.** Codex's `paper` MCP (127.0.0.1:29979) was unreachable;
codex retried indefinitely and that kept the spinner running, bloating
logs by >1 MB in 30 s. We only noticed the playwright MCP was the
immediate blocker by reading the log carefully. Removing it
(`codex mcp remove playwright`) helped.

**Status — shipped Apr 20, 2026.** `docs/atcp/PRESMOKE-CHECKLIST.md`
now covers MCP reachability, `TERM`, socket hygiene, second-shell
inspection, and cleanup. `atcp-live --warmup-timeout` prints a warning
when a session stays output-idle through the warmup window.

### Gap 6 — No inter-agent communication primitive beyond "human injects"

**Symptom.** The test harness only has a "human in the loop" model:
human types → broadcast to all agents. There's no way for codex to say
something that droid hears. Each agent's stdout is captured in its
own session SSE, but nothing routes it back into the room as a
message. If we wanted a real `codex ↔ droid` coordination loop, we'd
need an "agent says X, the broker repackages X as a room message and
fans it to peers" bridge.

**Status — simple variant shipped Apr 20, 2026.** `atcp-live --talkback
name=prefix` forwards matching raw or rendered lines as room messages with
`source=name` and skips echoing to the source agent. The matcher allows
leading TUI decoration made of whitespace, punctuation, or symbol glyphs
before the configured prefix, which covers codex's rendered `• @room:`
output while still requiring the explicit prefix. Message sends now also
return a structured `receipt` and prepend a terminal-visible receipt by
default, including `message_id`, `room_id`, optional correlation/reply-to
metadata, and a structured `reply_prefix` such as `@room:01K... `. Talkback
accepts that explicit-room form as well as the older single-room
`@room: text` form. The terminal preamble uses `reply_prefix_hint`
(`<AT>room:01K... `) instead of a literal `@room:` prefix after the
receipt-visible smoke showed droid opening its `@` file-mention UI while the
preamble was typed. This remains opt-in and broker-protocol-pure. A native
ATCP client inside each PTY is still a future side-channel option.

### Gap 7 — `terminal/write_bytes` is capability-disabled by default

**Symptom.** Tried to write raw bytes as an escape-hatch diagnostic and
got `{"error":"...write_bytes capability is disabled"}`. Makes sense
from a safety standpoint — arbitrary byte injection is dangerous —
but the gating isn't discoverable: there's no documented way to
enable it for a trusted client.

**Status — shipped Apr 20, 2026.** `CreateSessionRequest` now accepts
`enable_raw_bytes`; the broker applies it to the generic adapter and
`atcpctl session create --enable-raw-bytes` exposes the trusted-session
escape hatch. The default remains disabled.

### Gap 8 — Line-buffered stdin forwarder truncates pastes and can't cancel mid-line

**Symptom.** Not observed in this run because I used `atcpctl msg send`
from a second shell. But `atcp-live`'s `forwardStdin` uses
`bufio.Scanner` with a 1 MiB buffer; multi-MB paste would simply drop.
Also, during `atcpctl msg send` the request timeout is 30 s — long
room messages could fail silently.

**Status — shipped Apr 20, 2026.** `atcp-live` now accepts lines up to
16 MiB and the ATCP HTTP client timeout is 120 s. The room router sends
messages >=1 MiB through `terminal.paste` with `bracketed:"auto"` and
`submit_after:true`, so bracketed-paste-aware sessions receive one paste
block instead of a giant typed submit.

## Triage: what to fix first

> **Live tracker:** [TODO.md](TODO.md) carries the current status, fix
> direction, acceptance criteria, and size estimate for each gap. Update
> there; this section stays as the narrative "triage moment" snapshot.

In order of "unblocks real integration" vs "nice to have":

1. **Gap 1 — terminal capability responder.** Blocks booting codex at
   all. ☑ **Shipped** (commit `19fe4589`,
   `internal/atcp/broker/termcaps`). Evidence above.
2. **Gap 4 — readiness signal.** ☑ **Shipped Apr 20, 2026.**
   Sessions now expose cumulative output bytes, rolling output byte-rate,
   `last_output_at`, and `GET /v1/sessions/{id}/readiness`; `atcp-live`
   polls this before startup/broadcast flow and prints `[name] ready`.
   Follow-up smoke recorded both `[codex] ready` and `[droid] ready`.
3. **Gap 2 — per-session submit key.** ☑ **Shipped Apr 20, 2026.**
4. **Gap 3 — screen snapshot rendering.** ☑ **Minimum useful shipped Apr
   20, 2026.** Coalesced SSE snapshots remain deferred.
5. **Gap 6 — inter-agent talkback.** ☑ **Simple variant shipped Apr 20,
   2026.**
6. **Gap 7 — raw write flag.** ☑ **Shipped Apr 20, 2026.**
7. **Gap 5 — pre-smoke checklist doc.** ☑ **Shipped Apr 20, 2026.**
8. **Gap 8 — paste / long input.** ☑ **Shipped Apr 20, 2026.**
9. **Gap 1b — codex input loop still silent.** ☑ **Shipped Apr 20, 2026.**
   Root cause was submit sequencing: codex needs text and the submit key as
   distinct PTY writes.

## Observations worth keeping for the runbook

- Droid launches a `droid exec --input-format stream-jsonrpc` child
  process for its agent engine. That's a child of the `droid` PTY
  process, parented through `atcpd`. No issues — just worth knowing
  when you see two `droid` processes in `ps`.
- Codex launches as a node wrapper → native binary chain (2 processes).
  When debugging, watch both.
- Daemon memory is fine under load; no leaks in a 30-second rendering
  firehose. This matches the race-free session.Manager work we just
  landed.
- The `atcpctl session list` JSON output is surprisingly handy for
  scripting — every field a jq selector needs is there.
