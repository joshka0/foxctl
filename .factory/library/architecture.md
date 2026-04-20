# Architecture — foxctl TUI Operator Cockpit

How the system works at a high level. Workers and validators read this before touching code.

**What belongs here:** Components, how they relate, data flows, invariants, key abstractions. No implementation details.
**What does NOT belong here:** Step-by-step commands (see `.factory/services.yaml`), env vars (see `environment.md`), testing surface (see `user-testing.md`).

---

## System Overview

The **foxctl TUI** is the Go-native terminal cockpit that operators use to
inspect and steer the foxctl daemon. It is a separate surface from the React
`packages/gui-agent/` (which is out of scope for this mission).

```
┌────────────────────────────┐        HTTP/SSE/JSON-RPC        ┌──────────────────────────┐
│ cmd/foxctl_tui (Go binary) │ ──────────────────────────────▶ │ foxctl web serve daemon │
│                            │                                 │   • agents                │
│  internal/interfaces/tui/  │ ◀──────────────────────────────│   • rooms                 │
│  (grindlemire/go-tui)      │   GET/POST + SSE + JSON-RPC     │   • events               │
└────────────────────────────┘                                 └──────────────────────────┘
```

The TUI is a pure **consumer** of the existing daemon API. No new endpoints
are added in this mission. Any gaps identified during M1 are noted as
follow-up work, not fixed here.

---

## Three Planes

Conceptually the cockpit UI is organized into three interaction lanes
(per DESIGN.md) and three memory planes (per docs/plans/go-tui-agent-shell.md):

**Interaction lanes (UI layout):**

- **Main lane** — primary operational surface (agent inventory, rooms list).
- **Detail lane** — selected entity (agent detail, hierarchy, runtime snapshot).
- **Evidence lane** — raw payloads, tool calls, errors (drawer; progressive reveal).

**Memory planes (conceptual, surfaced honestly):**

- **Companion Memory** — per-agent conversation history.
- **Named Durable Memory** — workspace-scoped durable memory.
- **ACA / Continuity** — Obsidian knowledge layer.

The information-architecture doc (M1 deliverable) specifies which cockpit
surfaces expose which planes and how projection/heuristic data is labeled.

---

## Key Components (target design, delivered by the mission)

### M1 — Docs (no code)

All delivered under `docs/plans/tui-redesign/`:

- `research-go-tui.md` — reference for LLM authors (core types, widgets, event loop, idioms, anti-patterns, testing).
- `audit-current-tui.md` — file:line audit of `internal/interfaces/tui/` + DESIGN.md gap analysis.
- `architecture.md` — explicit decisions (coexist vs refactor, runtime registry, typed entities, async boot, `.gsx` toolchain, surface ownership).
- `information-architecture.md` — three-lane layout, primary flow, keybindings, progressive reveal, three-plane memory.
- `component-spec.md` — per-widget contract for M2.
- `integration-map.md` — table of every API/stream the cockpit consumes.
- `adrs/` — 5+ ADRs with Context/Decision/Alternatives/Consequences/Status.

### M2 — Component library seed (code)

Placed under `internal/interfaces/tui/` at a sub-path chosen in M1 (e.g.,
`internal/interfaces/tui/components/` and `internal/interfaces/tui/runtime/`).

- **`runtime.Bounded[Req, Upd]`** — generic bounded-queue runtime replacing
  duplicated scaffolding in `console_stream_pump.go`, `console_ask_runtime.go`,
  `console_cancel_runtime.go`. Exposes `Enqueue(ctx, req) error`,
  `Updates() <-chan Upd`, `Stop()`, `Close()`.
- **Typed entities package** — `Agent`, `AgentNode`, `Room`, `RoomMessage`,
  `EventRow`, plus `EntryKind` typed enum replacing the string-keyed
  transcript/event kinds (18 current values covered; legacy-string mapper
  provided for backward compat).
- **Widget primitives** — `EntityList`, `DetailPane`, `Tabs`, `Drawer`,
  `StreamViewer`, `EmptyState`, `LoadingState`, `StatusBadge`, `KeybindHint`.
- **Theme tokens** — color + spacing constants in a dedicated package;
  raw color literals forbidden in widget implementations.

### M3 — Walking skeleton (code)

End-to-end proof of the M2 patterns against the live daemon. Reachable via a
documented invocation on `cmd/foxctl_tui` (flag or subcommand, per M1).

- **Async boot** — loading state within 500ms; transitions to ready or error
  without blocking the UI thread.
- **Three-lane layout** — Main (live agent inventory), Detail (selected
  agent's runtime + hierarchy + transcript preview), Evidence (drawer for
  raw payloads).
- **Ask/chat** — streams tokens via `POST /api/agents/{id}/ask-stream`; cancels
  via `POST /api/agents/{id}/ask-stream/cancel`.
- **Live refresh** — single subscription to `/api/events` (topic filter for
  agent events); inventory updates within 5s of external spawn/delete.
- **Status footer** — connection health, active entity, compact keybindings.

---

## Runtime & Concurrency Invariants

These invariants apply to every new component introduced in M2 and consumed in
M3. Violations are auto-rejected during scrutiny review.

- **Single-writer state ownership.** Each piece of mutable state has one owner
  goroutine. Cross-goroutine updates go through channels or message queues,
  never shared mutable maps.
- **Bounded queues.** All async queues are bounded; backpressure policy is
  explicit (drop, block, or error). No unbounded channels.
- **Context threading.** Every long-lived operation accepts
  `context.Context`. No goroutine survives `Stop()` longer than 100ms.
- **Leak-free.** `goleak.VerifyNone` or a `runtime.NumGoroutine()` delta check
  is part of every runtime test.
- **Snapshot reads.** Hot read paths (render) use immutable snapshots
  (`atomic.Value`/`atomic.Pointer`) to avoid contention.
- **Determinism.** No `time.Now()`, `rand.*`, or `os.Getenv` inside pure render
  or state-reducer functions. Injected deps only.

---

## Integration Points

The cockpit consumes these daemon endpoints. Full table with methods,
cancellation, backpressure is documented in `integration-map.md` (M1
deliverable).

| Endpoint                                        | Transport | Purpose                         |
| ----------------------------------------------- | --------- | ------------------------------- |
| `GET /api/agents`                               | HTTP      | Agent inventory                 |
| `GET /api/agents/{id}`                          | HTTP      | Agent detail / runtime snapshot |
| `POST /api/agents/{id}/ask-stream`              | SSE       | Streaming ask                   |
| `POST /api/agents/{id}/ask-stream/cancel`       | HTTP      | Cancel in-flight ask            |
| Agent hierarchy                                 | JSON-RPC  | Tree of parent/child agents     |
| `/api/events`                                   | SSE       | Live event hub (topic filtered) |
| `GET /api/rooms`                                | HTTP      | Rooms directory                 |
| `/api/rooms/{id}/events`                        | SSE       | Per-room event stream           |

**Gaps identified** (not fixed this mission; captured as follow-ups in M1):
agent hierarchy HTTP parity (currently JSON-RPC only), agent watch NDJSON
endpoint alignment.

---

## Testing Strategy

**Unit (M2):** MockTerminal-based go-tui tests for every widget and runtime.
Table-driven. Tests written before implementation (TDD red → green).

**Integration (M2):** Adapter tests against the existing `APIClient` using
`httptest` servers (extend patterns in `api_client_test.go`,
`agent_adapter_test.go`).

**End-to-end (M3):** Tuistory-driven snapshot + interaction tests for the
walking-skeleton flows. Each test uses the **per-test-daemon fixture** — a
helper that boots `foxctl web serve -p 0` with a temp `FOXCTL_STORAGE_ROOT`,
parses the chosen port, seeds N agents deterministically, and tears down on
`t.Cleanup`.

**Determinism:** all tests use injected clock/UUID where needed; golden
outputs are sorted.

---

## Authoring Ergonomics (LLM-first)

A primary motivation for this mission is that Codex/Claude struggle to extend
the current TUI correctly. The new patterns are explicitly designed to be
easy to extend:

- **Explicit constructors.** No chained `NewShell*` setters; each constructor
  takes all deps.
- **Typed enums.** `EntryKind` typed constants replace string-keyed kinds;
  adding a new kind is one const + exhaustive switch coverage.
- **Generic runtimes.** `Bounded[Req, Upd]` replaces three near-duplicate
  goroutine scaffolds; adding a new runtime is one type parameterization.
- **Single-source renders.** Each widget has one render function; no
  ambient state.
- **Authoring README.** The M2 component package ships with a README
  explaining "how to add a new widget" with exact commands, paths, and
  required tests.

---

## Unicode Width

The go-tui framework provides `tui.RuneWidth(r rune) int` as the canonical way
to compute terminal display width. All widget truncation, padding, and centering
must use display width (not rune count or byte count). CJK characters are
2 cells wide; combining marks and ZWJ sequences are 0 cells.

The M2 component library uses three key utility functions in the `components`
package that all rely on `tui.RuneWidth`: `truncate()`, `center()`, and
`padOrTruncateWidth()`. Future widget authors should use these or follow the
same pattern.

---

## Mission Boundaries

See `missionDir/AGENTS.md` for the full list. Highlights:

- Framework stays on `github.com/grindlemire/go-tui v0.11.0` — no framework
  swaps, no version upgrades unless M1 explicitly motivates one.
- `packages/gui-agent/` (React console) is **not touched**.
- `archive/` is read-only; no reviving archived TUIs.
- No new daemon endpoints or API routes.
- No pushes to remote; work stays on branch `feat/tui-go`.
