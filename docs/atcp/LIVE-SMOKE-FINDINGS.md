# ATCP live-smoke findings (codex + droid)

Apr 20, 2026 — ran the first interactive smoke against a real two-agent room
using `cmd/atcpd` + `cmd/atcp-live` + `codex-cli 0.121.0` + `droid 0.104.0`.
Messages were injected from a second shell via `atcpctl msg send`.

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

**Fix direction.** Make the submit key configurable per-session and
auto-detect when a kitty keyboard push is observed on output. Store the
detection in `session.Session` and have `CompileSubmit` consult it
before defaulting to `\r`. Until this lands, the `msg send` path should
probably default to `\r\n` (both) so TUIs that take either still work.

### Gap 3 — Observer sees raw PTY bytes; rendering is unusable for humans

**Symptom.** The `atcp-live` stdout was dominated by ANSI escape codes
and alt-screen redraws. Over 30 seconds, droid emitted ~4,900 chunks,
each being a partial redraw of a TUI frame. Without interpretation, a
human observer can't read any of it. Even after stripping CSI with
`sed`, the output is just fragments from the TUI's frame-buffer
gymnastics.

**Fix direction.** Plan Phase 2 screen snapshots. Minimum viable:

- `internal/atcp/broker/vtscreen` (or similar) keeps a per-session
  virtual terminal buffer, applied as bytes arrive. Consumers get
  `terminal.screen.snapshot` events with the rendered text (no
  escapes).
- `atcp-live --render` (or a new `atcpctl session watch`) consumes the
  snapshots instead of raw bytes, prints only the diff, and optionally
  suppresses cursor-movement noise entirely.
- This is meaningful work — there's a reason `vt10x` and similar
  libraries exist. We'd either vendor one or implement a strict
  subset. Probably 2-3 days of careful work.

### Gap 4 — No "agent readiness" signal

**Symptom.** `msg send` returned `delivered=true` to codex when codex
was mid-MCP-init and unable to process input. We'd never know from the
return value that our message landed in a black hole. In the second
run I could only infer codex was "done booting" by watching the log
file's byte rate drop to zero.

**Fix direction.** Either:
- **(cheap)** Expose the session's recent-output-bytes-per-second as a
  field on `SessionResponse`; callers can poll for "idle".
- **(proper)** Let an adapter declare an "idle regex" per agent type
  (e.g. codex idle is the `_ > ` prompt rendering); the broker's
  modetrack / screen engine fires a `terminal.ready` event when it
  matches. This dovetails with the Phase 2 screen work. The safeprompt
  package already does regex-on-tail detection — the pieces exist.

### Gap 5 — MCP failures during agent boot silently break the smoke

**Symptom.** Codex's `paper` MCP (127.0.0.1:29979) was unreachable;
codex retried indefinitely and that kept the spinner running, bloating
logs by >1 MB in 30 s. We only noticed the playwright MCP was the
immediate blocker by reading the log carefully. Removing it
(`codex mcp remove playwright`) helped.

**Fix direction.** Not an ATCP bug but an operator concern. A
`docs/atcp/PRESMOKE-CHECKLIST.md` that lists "make sure every MCP
server your agent depends on is reachable before spawning" would save
time. The smoke driver could also offer a `--warmup-timeout` flag and
print a warning when an agent emits nothing new for N seconds but is
still "running".

### Gap 6 — No inter-agent communication primitive beyond "human injects"

**Symptom.** The test harness only has a "human in the loop" model:
human types → broadcast to all agents. There's no way for codex to say
something that droid hears. Each agent's stdout is captured in its
own session SSE, but nothing routes it back into the room as a
message. If we wanted a real `codex ↔ droid` coordination loop, we'd
need an "agent says X, the broker repackages X as a room message and
fans it to peers" bridge.

**Fix direction.** Add a "talkback" mode to `atcp-live`: each agent's
output is pattern-matched for an explicit send envelope (e.g. lines
starting with `@room:`) and those lines get forwarded as `msg send`
with `source=agent-name`. This is opt-in per-agent and keeps the
broker protocol-pure. Alternatively, codex/droid would need an ATCP
client of their own inside the PTY (via a custom MCP server or similar).

### Gap 7 — `terminal/write_bytes` is capability-disabled by default

**Symptom.** Tried to write raw bytes as an escape-hatch diagnostic and
got `{"error":"...write_bytes capability is disabled"}`. Makes sense
from a safety standpoint — arbitrary byte injection is dangerous —
but the gating isn't discoverable: there's no documented way to
enable it for a trusted client.

**Fix direction.** Document the adapter capability model, and make
`write_bytes` opt-in per-session via `CreateSessionRequest` (already
has `Adapter string` — we could add a flag like
`enable_raw_bytes: true` that the adapter reads).

### Gap 8 — Line-buffered stdin forwarder truncates pastes and can't cancel mid-line

**Symptom.** Not observed in this run because I used `atcpctl msg send`
from a second shell. But `atcp-live`'s `forwardStdin` uses
`bufio.Scanner` with a 1 MiB buffer; multi-MB paste would simply drop.
Also, during `atcpctl msg send` the request timeout is 30 s — long
room messages could fail silently.

**Fix direction.** Multi-line paste support would route through the
paste intent (`terminal.paste` with `bracketed: true` when the
adapter's mode tracker says the child enabled bracketed paste). That's
Phase-2 territory again.

## Triage: what to fix first

In order of "unblocks real integration" vs "nice to have":

1. **Gap 1 — terminal capability responder.** Blocks booting codex at
   all. Must fix first. 1-2 days.
2. **Gap 4 — readiness signal.** We can't run reliable smokes without
   knowing when an agent is ready. Can be a cheap version (poll
   output-byte-rate) initially. 0.5 day.
3. **Gap 2 — per-session submit key.** So the happy path works on more
   agents. 0.5 day including detection of kitty mode.
4. **Gap 3 — screen snapshot rendering.** Transforms the observer
   experience from unusable to mux-equivalent. This is Plan Phase 2
   proper. 2-3 days.
5. **Gap 6 — inter-agent talkback.** Required for "codex tells droid
   something" scenarios. Blocked on screen rendering (so the talkback
   pattern-matcher has clean text to match on). 0.5 day after Gap 3.
6. **Gap 7 — raw write flag.** Small. Schedule whenever.
7. **Gap 5 — pre-smoke checklist doc.** 30 minutes.
8. **Gap 8 — paste / long input.** Ride along with Phase 2 paste work.

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
