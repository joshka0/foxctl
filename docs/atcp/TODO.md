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

## Gap 1b — Codex input loop still inactive after termcaps shipped

**Status:** ☐ · follow-up to Gap 1 · **priority: low** (Gap 1 proved the
termcaps layer works; this is codex-specific init plumbing)

**Symptom.** After the responder, codex no longer burns CPU in a spinner
re-draw loop and emits only ~3 KB during boot. But a single byte written
via `POST /v1/terminal/text` still produces zero echo on codex's SSE stream,
and codex never responds to broadcast messages. The process is alive (in
`S+` state, waiting on `read`) but isn't consuming input.

**Confirmed not the cause.**

- Terminal capability queries — `/tmp/scan_codex_escapes.py` on a post-fix
  session log shows every query codex sent (`ESC[6n`, `ESC[?u`, `ESC[c`,
  `OSC 10`, `OSC 11`) is one the responder answers.
- Approval / sandbox gating — `codex --dangerously-bypass-approvals-and-sandbox`
  behaves identically.

**Likely causes to investigate (in order of cheapness to test).**

1. **MCP readiness gating.** Codex may refuse to enable input until all
   registered MCP servers have successfully handshaken. The smoke run had
   `paper` (127.0.0.1:29979) unreachable and `playwright` had been removed
   but others may still be slow/failing. Try with every MCP removed
   (`codex mcp list` → `codex mcp remove <each>`) and re-run.
2. **`TERM` / env mismatch.** Check what codex sees:
   `/v1/sessions/{id}` includes the adapter env. If `TERM` is empty,
   `dumb`, or unset, codex may fall into a non-interactive mode. The
   daemon currently inherits env when `CreateSessionRequest.Env` is nil;
   verify that inheritance actually carries `TERM=xterm-256color`.
3. **Stdin is not seen as a TTY.** Low probability given `pty.StartWithSize`
   sets up the slave fd as stdin, but worth a `ls -la /proc/<pid>/fd/0`
   (Linux) or `lsof -p <pid>` (macOS) check.
4. **Codex-side instrumentation.** As a last resort, strace/dtruss the
   codex child to see whether it's blocked on a `read(0, ...)` from the
   PTY slave or on some socket read (MCP, network, auth).

**Acceptance.** Reopen the smoke, broadcast `please respond with "pong"`,
and see codex emit a corresponding TUI update within ≤10 s. Document the
root cause in `LIVE-SMOKE-FINDINGS.md` even if the fix lives in codex
config rather than our code.

**Size.** 2–4 hours for steps 1–3; step 4 can blow up to a day if the
cause is obscure.

---

## Gap 4 — Agent-readiness signal on the session

**Status:** ☐ · **priority: high** (this is the next thing to land — it
unblocks reliable smoke tests for *every* agent, and gives Gap 1b an
objective signal to measure against)

**Symptom.** `POST /v1/messages` returns `delivered: true` the moment the
router's PTY write succeeds, even if the recipient agent was mid-init and
couldn't process input. We have no protocol-level way to say "is codex
ready yet?"; we infer it by eyeballing the SSE byte-rate drop.

**Root cause.** `session.Session` tracks bytes written but not the arrival
rate, and `SessionResponse` has no readiness field.

**Fix direction (cheap, minimum viable).**

1. Add an `outputRate` tracker to `session.Session`: exponentially-weighted
   bytes-per-second over the last ~1 s, updated on each PTY read. Cheap —
   one atomic float, one timestamp, updated in the existing read loop.
2. Expose on `SessionResponse` as:
   - `output_bytes_total` (int64, cumulative)
   - `output_rate_bps` (float, current EWMA)
   - `last_output_at` (RFC3339 timestamp of most recent non-empty read)
3. Add `GET /v1/sessions/{id}/readiness` that returns a simple
   `{"idle": bool, "idle_for_ms": int}` with `idle := rate < 32 B/s for
   >=500 ms`. Thresholds tunable via query params.
4. Teach `atcp-live` (and eventually `atcpctl msg send --wait-idle`) to
   poll readiness before sending and after receiving, so the driver prints
   a clear "[codex] ready" line rather than leaving the user guessing.

**Fix direction (proper, later).** The adapter declares a regex for an
"idle prompt" (e.g. codex: `_ > \z`, droid: `⛬\s+$`, gemini: `> $`). The
screen-snapshot engine from Gap 3 fires a `terminal.ready` event when the
regex matches the rendered tail. This is free to piggy-back on Gap 3 and
dovetails with `safeprompt`'s existing tail-regex machinery — don't build
the regex path until Gap 3 lands.

**Acceptance.**

- `session.Session.OutputRate()` returns a non-negative float, decays to 0
  within 2 s of no reads, rises to match sustained write rate within 1 s.
- `GET /v1/sessions/{id}/readiness` returns `idle:true` only after the
  rate has been below threshold for the full debounce window.
- Unit test: feed synthetic chunks at known timestamps, assert rate and
  idle transitions.
- `atcp-live` run against a droid session prints `[droid] ready` when
  droid reaches its input prompt. Verified by human observation.

**Size.** 0.5 day for the cheap version + tests + `atcp-live` plumbing.

---

## Gap 2 — Per-session submit key + kitty-keyboard auto-detection

**Status:** ☐ · **priority: medium** (droid works with the current `\r`
default; this matters most once we start integrating more agents)

**Symptom.** `adapter.CompileSubmit` defaults to `Enter → 0x0D` (`\r`).
Some TUIs (Go/Rust raw-mode handlers, or agents that pushed the kitty
keyboard protocol) expect `\n` or the kitty-encoded Enter (`ESC[13u`).
Today we'd have to patch adapter code to change it.

**Root cause.** No per-session override, no detection of kitty-protocol
state. `adapter.Adapter.CompileSubmit` is a pure function of the key name,
ignoring runtime terminal mode.

**Fix direction.**

1. **Per-session override.** Add `submit_key` to `CreateSessionRequest`
   (`"Enter" | "Return" | "LineFeed" | "KittyEnter"`). Store on
   `session.Session`. `CompileSubmit` consults the session's override
   before falling back to the adapter default.
2. **Kitty auto-detection.** `modetrack` already parses CSI sequences; add
   detection for `CSI > {flags} u` (push) and `CSI < u` (pop). On push,
   set `session.KittyKbdActive = true` atomically. `CompileSubmit`
   returns `ESC[13u` when that bit is set and no explicit override was
   given.
3. **Dual-submit fallback (safety net).** Until this lands, change
   `router`'s default submit to `\r\n` (both bytes). Any shell/readline
   accepts CR, any line-buffered TUI accepts LF. Costs nothing for
   well-behaved TUIs; unblocks ones picky about one variant.

**Acceptance.**

- Unit test: session with `submit_key: "LineFeed"` → `CompileSubmit`
  returns `\n`.
- Unit test: feed `ESC[>9u` to modetrack → `session.KittyKbdActive` is
  true → `CompileSubmit` returns `ESC[13u` without explicit override.
- Integration test: spawn a node REPL, send an `Enter`, observe the
  prompt advance. Repeat with a kitty-pushed stub that echoes `ESC[13u`.
- `atcpctl msg send --submit-key KittyEnter` works as an override.

**Size.** 0.5 day.

---

## Gap 3 — Phase 2 screen-snapshot rendering (vt engine)

**Status:** ✎ · design decision needed · **priority: medium** (unblocks
most observability and Gap 6; but it's real work)

**Symptom.** `atcp-live`'s stdout is a firehose of raw PTY bytes.
30 seconds of droid = ~4,900 distinct SSE chunks, each a partial ANSI
redraw. Stripping CSI codes with `sed` is useless — you get frame
fragments. Human observation is impossible; pattern-matching for agent
output (needed for Gap 6) is impossible.

**Root cause.** We broadcast raw PTY bytes. There's no interpretation
layer anywhere in the broker. Plan §6 Phase 2 describes the goal
(`terminal.screen.snapshot`) but leaves the engine unbuilt.

**Design decision needed.** Two viable paths:

- **(a) Vendor a VT emulator.** `github.com/hinshun/vt10x`,
  `github.com/charmbracelet/x/vt`, or `github.com/gdamore/tcell/v2`'s
  screen. Pro: stable, handles the full spec. Con: adds ~3-5 KLOC of
  dependency; some of them drag in a lot.
- **(b) Implement a strict subset.** ASCII + common CSI (CUP/EL/ED/SGR)
  + alt-screen buffer + scrollback. Pro: no new dependency, fits in
  ~800 LoC, tailored to our needs. Con: we own the edge cases forever.

**Recommendation.** Start with (b), structured as
`internal/atcp/broker/vtscreen`. If a real-world agent exposes a feature
we don't handle, either extend vtscreen or escape-hatch: emit
`terminal.screen.snapshot_raw` alongside the rendered snapshot for
consumers that want the bytes. Watch scope-creep carefully.

**Fix direction (minimum useful).**

1. `internal/atcp/broker/vtscreen` — per-session grid (default 80×24,
   grows to `WinSize`). Primary vs alt screen. Apply bytes incrementally
   on each PTY read, after modetrack but before subscribers fan out.
2. New event kind `terminal.screen.snapshot` with body:
   `{rows: int, cols: int, cells: string[][], cursor: {row,col,visible},
     dirty_rows: int[]}`.
3. Emit on a coalescing timer: at most one snapshot per 50 ms per session,
   unless bytes > 4 KiB accumulated (emit immediately).
4. `GET /v1/sessions/{id}/screen` for point-in-time fetch.
5. `atcp-live --render` consumes snapshots instead of raw bytes; prints
   the last `dirty_rows` line by line on change.

**Acceptance.**

- Unit: feed canonical CSI sequences (CUP to 10;10, `hello`, CR, `world`)
  → assert grid state.
- Unit: alt-screen enter/exit preserves the primary screen.
- Integration: droid smoke run with `atcp-live --render` produces a
  readable transcript of droid's prompts and responses.
- Byte budget: `terminal.screen.snapshot` per-event < 8 KB on an 80×24
  grid with typical content.

**Size.** 2–3 days (implementation + tests + atcp-live integration). Gate
on dedicated time.

---

## Gap 5 — Pre-smoke operator checklist

**Status:** ☐ · **priority: low** (docs-only; takes 30 min)

**Symptom.** Codex's failed `paper` MCP caused the whole init log to
bloat, masking the real termcap issue for ~20 minutes of debugging. An
operator checklist would save time.

**Fix direction.** New doc `docs/atcp/PRESMOKE-CHECKLIST.md` covering:

- Verify every registered MCP server is reachable (`codex mcp list`,
  `droid mcp list`, equivalent) — or disable failing ones.
- Confirm `$TERM` is `xterm-256color` in the shell that launches `atcpd`.
- Confirm the socket path is fresh (`rm -f $SOCK` before daemon start).
- Have a second shell ready with `atcpctl --socket $SOCK` pointing at the
  same socket, to inspect room/session state without disturbing the
  driver.
- Know how to kill everything cleanly (`pkill -INT atcpd; pkill -INT
  atcp-live`).

Also add a `--warmup-timeout <dur>` flag to `atcp-live` that prints a
warning like `[codex] no new output for 10s — likely stuck in init` once
a session emits nothing new for the given window.

**Acceptance.** The doc exists, is linked from `LIVE-SMOKE.md`, and the
`--warmup-timeout` flag is implemented with a regression test.

**Size.** 30 min for the doc + 1 hour for the flag.

---

## Gap 6 — Inter-agent talkback

**Status:** ✎ · blocked on Gap 3 · **priority: medium** (the headline
feature — "codex and droid coordinate" doesn't happen without this)

**Symptom.** The only message source today is a human injecting text via
`atcpctl msg send` or `atcp-live` stdin. There's no path for codex's
output to become a room message droid sees.

**Root cause.** Sessions are sinks (you write into them, they render) but
not sources of typed events. Their output flows to SSE subscribers, not
back through the router.

**Fix direction (preferred, simple).** "Opt-in talkback" in `atcp-live`
first, not in the broker.

1. New `atcp-live` flag `--talkback <name>=<prefix>` (repeatable, default
   off). When a line from agent `<name>` starts with `<prefix>`
   (e.g. `@room:`), strip the prefix and forward the rest as
   `msg send --source <name>`.
2. Requires Gap 3 first — otherwise we're pattern-matching against raw
   PTY chunks with escape codes, which is a nightmare. With screen
   snapshots we match against rendered lines.
3. Document that agent prompts (in codex / droid system prompts) should
   include "when you want to talk to a peer, emit `@room: <text>`".

**Fix direction (ambitious, deferred).** Run an ATCP client inside each
agent's environment. Either:

- A custom MCP server per-agent that exposes `atcp.send`, so the LLM can
  call it as a tool. Belongs to the agents' config, not our repo.
- `AGENTCTL_CONTROL_SOCK` env + a helper binary on the agent's `$PATH`
  that wraps `atcpctl msg send`. Already planned in §6 Phase 6 as the
  "native adapter side-channel". Reuse that plumbing.

**Acceptance (simple variant).** Smoke run with `--talkback codex=@room:`,
broadcast "codex, greet droid with @room: hello", and observe droid
receive a `msg send` with `source=codex` body `hello`. End-to-end demo.

**Size.** 0.5 day *after* Gap 3 is stable.

---

## Gap 7 — `terminal/write_bytes` opt-in

**Status:** ☐ · **priority: low** (escape-hatch; small)

**Symptom.** `POST /v1/terminal/write_bytes` rejects every call with
`"write_bytes capability is disabled"`. The safety choice is correct, but
the opt-in mechanism isn't wired for trusted callers.

**Fix direction.**

1. Add `enable_raw_bytes: bool` to `CreateSessionRequest` (default false).
2. `generictty.Adapter` consults session config when compiling the
   `write_bytes` intent and only rejects when both the session flag and
   any adapter-level policy are off.
3. Document in `LIVE-SMOKE.md` the one-liner for enabling it and why the
   default is off.
4. Log every `write_bytes` call at `info` with the byte count and the
   envelope id.

**Acceptance.** Unit test: session created without the flag → `write_bytes`
is rejected; session created with the flag → accepted and bytes appear on
the PTY.

**Size.** 1 hour.

---

## Gap 8 — Long input / paste support in `atcp-live`

**Status:** ☐ · blocked on Gap 3's mode-aware paste work · **priority:
low**

**Symptom.** `atcp-live`'s stdin forwarder uses `bufio.Scanner` with a
1 MiB buffer; multi-MB pastes silently drop. `atcpctl msg send`'s HTTP
client has a 30 s timeout that caps very-long-prompt sends.

**Fix direction.** When the recipient session's modetrack reports
bracketed-paste active, route large inputs through the `terminal.paste`
intent (`bracketed: true`) instead of `text` or `submit`. Increase the
scanner buffer to 16 MiB. Raise the `msg send` client timeout to
120 s, or make it streaming.

**Acceptance.** Paste a 5 MB markdown file into `atcp-live` → arrives at
droid as one paste (bracketed), no byte loss.

**Size.** Half a day, alongside Gap 3.

---

## Priority ladder (quick reference)

1. ☐ **Gap 4** — readiness signal (0.5 d) · unblocks every other smoke
2. ☐ **Gap 2** — per-session submit key + dual-submit default (0.5 d)
3. ✎ **Gap 3** — vt screen engine (2–3 d) · design decision first
4. ✎ **Gap 6** — talkback (0.5 d, gated on Gap 3)
5. ☐ **Gap 1b** — codex input-loop chase (2 h–1 d) · low-ROI, do after 4/2 ship
6. ☐ **Gap 7** — write_bytes opt-in (1 h)
7. ☐ **Gap 5** — pre-smoke checklist + warmup-timeout (~1.5 h total)
8. ☐ **Gap 8** — paste / long-input (0.5 d, ride-along with 3)

Total to close everything through Gap 6: ~5 focused days.

---

## Cross-references

- Root-cause narrative per gap: [LIVE-SMOKE-FINDINGS.md](LIVE-SMOKE-FINDINGS.md)
- Runbook for re-running the smoke: [LIVE-SMOKE.md](LIVE-SMOKE.md)
- Spec + Phase plan: [ATCP-v0.1.md](ATCP-v0.1.md) and
  [`../plans/atcp-rooms-replacement.md`](../plans/atcp-rooms-replacement.md)
- Gap 1 implementation: `internal/atcp/broker/termcaps/{responder.go,responder_test.go}`,
  wired at `internal/atcp/broker/session/session.go:279-313` (commit `19fe4589`).
