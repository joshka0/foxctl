# Architecture Decisions — foxctl TUI Operator Cockpit Redesign

This document is the key decision record for the foxctl TUI operator cockpit
redesign. It covers nine architectural decisions, the `.gsx` toolchain
contract for LLM authors, a traceability table mapping audit findings to
decisions, a surface-ownership reconciliation against DESIGN.md, and a
reconciliation section relating the new three-lane model to the prior
four-region shell plan.

It is an M1 deliverable satisfying VAL-DOCS-003, VAL-DOCS-009, VAL-DOCS-010,
VAL-DOCS-012, and VAL-DOCS-014.

**Prerequisite:** [audit-current-tui.md](./audit-current-tui.md) must be read
first — the audit findings are referenced throughout.

---

## Decisions

### Decision (a): Coexist vs Refactor-in-Place

**Decision:** Coexist. The new cockpit screens live alongside the existing
shell as a separate entry point on `cmd/foxctl_tui`, reachable via a documented
flag or subcommand (e.g., `-screen agents`). The existing shell and its smoke
modes (`-smoke-agent`, `-smoke-console`) remain unchanged.

**Rationale:** The existing shell at `internal/interfaces/tui/shell.gsx`
(~706 lines of `.gsx`) is a working companion-agent surface with comprehensive
test coverage (~2,500 lines across 12 test files). Refactoring it in-place
would risk breaking the smoke modes that CI depends on, would require
simultaneous `.gsx` regeneration, and would make it impossible to validate the
new three-lane layout against the old four-region shell in parallel. The
coexist strategy lets the new cockpit prove itself against the live daemon
without endangering the existing companion workflow. When the new cockpit
reaches feature parity for the companion flow, a follow-up mission can
deprecate the old shell. The shared code (adapters, runtimes, event parsing)
is reused via direct import from the same `internal/interfaces/tui/` package.

**Alternatives Considered:**

- **Refactor-in-place** — modify the existing `Shell` component to support a
  three-lane layout, agent inventory, and rooms. Rejected because: (1) the
  `.gsx` source is tightly coupled to the four-region layout and cannot be
  incrementally refactored without a full rewrite of `shell.gsx`; (2) the
  existing smoke modes would break during the transition; (3) git bisect
  becomes unreliable during a large in-place refactor.

### Decision (b): Runtime Registry Pattern

**Decision:** Use an explicit runtime registry — a `[]Runtime` slice held by
the cockpit's root component, where each `Runtime` is an interface with
`Start()`, `Stop()`, and `Updates() <-chan any`. The root component starts all
registered runtimes in `Init()` and stops them in the cleanup function. New
runtimes (events watcher, rooms subscription) register via a
`RegisterRuntime(rt Runtime)` call at construction time.

**Rationale:** The current code wires runtimes through a chain of four
constructors (`NewShell()` → `NewShellWithStream()` → `NewShellWithRuntime()`
→ `NewShellWithRuntimes()` at `internal/interfaces/tui/shell.gsx:38–95`).
Adding a new runtime (e.g., an events subscription) requires modifying all
four constructors and understanding parameter propagation. A registry
collapses this into a single list: each runtime self-describes its lifecycle,
and the root component manages start/stop uniformly. This pattern also makes
it straightforward to add runtime-level health checks and to verify that all
goroutines are stopped on shutdown (a current testing requirement). The
registry is typed via Go generics — each concrete runtime wraps
`Bounded[Req, Upd]` and implements the `Runtime` interface.

**Alternatives Considered:**

- **Constructor-chain continuation** — keep extending `NewShellWithRuntimes`
  with additional channel parameters. Rejected because: the chain is already
  four constructors deep and adding more parameters makes it harder for LLM
  authors to extend correctly (cited as authoring pain point #1 in the audit).

### Decision (c): Shared Generic Bounded-Runtime Design

**Decision:** Introduce `runtime.Bounded[Req, Upd]` — a generic bounded-queue
runtime that encapsulates the goroutine lifecycle pattern shared by the
request-driven runtimes. The existing `ConsoleAskRuntime` and
`ConsoleCancelRuntime` delegate to `Bounded` internally. `ConsoleStreamPump`
retains an independent goroutine lifecycle because its source-driven SSE
callback model does not fit `Bounded`'s request-driven handler pattern.

**How the three existing runtimes collapse or coexist:**

1. **`ConsoleStreamPump`** (`internal/interfaces/tui/console_stream_pump.go:63`)
   is source-driven (no `Enqueue` — it reads from an SSE connection and pushes
   updates via a callback). It **does not delegate to `Bounded`**. The pump's
   lifecycle is driven by the SSE source: when a frame arrives, the callback
   fires immediately. `Bounded`'s request-driven `Enqueue`/`handle` pattern
   would force an unnatural inversion — the pump would have to enqueue
   synthetic requests to itself on every SSE frame. This is accurately
documented in
   [`internal/interfaces/tui/components/README.md`](../../../internal/interfaces/tui/components/README.md)
   (see the "Hand-written files" table which lists `console_*.go` as
   "Console runtimes (delegating to `runtime.Bounded`)" — a shorthand that
   applies to the two request-driven runtimes, not the pump). The pump
   retains its own `context.WithCancel`, `sync.Once`, `sync.WaitGroup`, and
   `run()` loop, preserving its existing public API and test semantics.

2. **`ConsoleAskRuntime`** (`internal/interfaces/tui/console_ask_runtime.go:109`)
   is request-driven (`Enqueue(ctx, req)`). It delegates to
   `Bounded[ConsoleAskRequest, ConsoleAskUpdate]`. The runtime supplies a
   `handle(ctx, req) Upd` function to `Bounded`, which runs it in the bounded
   goroutine. The existing `Enqueue()`, `Updates()`, `Stop()`, `Close()` API
   is preserved as a thin wrapper around `Bounded`'s methods.

3. **`ConsoleCancelRuntime`** (`internal/interfaces/tui/console_cancel_runtime.go:85`)
   is request-driven like `ConsoleAskRuntime`. It delegates identically:
   `Bounded[ConsoleCancelRequest, ConsoleCancelUpdate]` with a `handle`
   function. The existing API surface is preserved.

Two of the three runtimes delegate to `Bounded`; the pump stays independent.
The two delegating runtimes eliminate ~80% of their duplicated boilerplate.
New request-driven runtimes (events subscription, rooms subscription) are
built directly on `Bounded` without copying boilerplate. The pump's
independence is a deliberate exception, not an oversight — forcing it into
`Bounded` would add indirection without reducing complexity.

**Rationale:** The three runtimes share ~80% structural similarity (cited in
audit section (h), pain point #2). Each has its own `sendUpdate()` helper, its
own buffer-size constants, its own `Enqueue()`/`Updates()`/`Stop()`/`Close()`
surface, and its own `requests`/`updates` channel pair. A generic `Bounded`
extracts this common pattern into a tested, reusable abstraction for the two
request-driven runtimes. Adding a new request-driven runtime (e.g., an events
watcher) goes from ~150 lines of boilerplate to ~20 lines of handler function
plus a `Bounded` constructor call. The generic also enforces the mission's
bounded-channel invariant at the type level — the buffer size is a required
constructor parameter, and the backpressure policy (block, drop-oldest, or
error) is explicit. The pump's independent lifecycle is preserved because its
SSE callback model is fundamentally source-driven: the server pushes frames,
and the pump forwards them. Wrapping this in a request-driven `Bounded` would
require the pump to enqueue synthetic requests to itself on every frame,
adding indirection without reducing code.

**Alternatives Considered:**

- **Keep three independent runtimes** — preserve the existing code as-is and
  copy the pattern for new runtimes. Rejected because: copying ~150 lines of
  boilerplate per runtime is the exact pain point the audit identified, and
  it makes the testing burden quadratic (each new runtime needs its own
  lifecycle tests).

- **Force the pump into `Bounded` with synthetic Enqueue** — rewrite
  `ConsoleStreamPump` so that every SSE callback enqueues a request to a
  `Bounded[StreamReq, ConsoleStreamUpdate]`. Rejected because: it inverts the
  natural control flow (source-driven → request-driven), adds a channel hop
  per frame with no benefit, and complicates cancellation (the SSE connection
  and the `Bounded` goroutine would each need their own `context.WithCancel`).
  The pump's existing `sync.Once` + `sync.WaitGroup` lifecycle is simpler and
  already tested.

- **Interface-based runtime without generics** — define a `Runtime` interface
  with `Enqueue(any)` and `Updates() <-chan any`, losing type safety. Rejected
  because: Go generics (available since 1.18, required by the project's 1.25+
  minimum) provide compile-time type safety for the request/update channel
  pair without any runtime cost.

### Decision (d): Typed Entity Model + State Reducer Pattern

**Decision:** Introduce a typed entities package
(`internal/interfaces/tui/entities/`) with structured types (`Agent`,
`AgentNode`, `Room`, `RoomMessage`, `EventRow`) and a typed `EntryKind` enum.
State mutations go through a single reducer function
`Reduce(state CockpitState, event Event) CockpitState` instead of scattered
`state.Update()` closures.

**How ambient `ShellState` is replaced or defended:**

The current `ShellState` at `internal/interfaces/tui/shell_state.go:8` is a
mutable value type with methods like `ApplyConsoleStreamEvent()` (line 55) and
`AttachAskCorrelation()` (line 74) that return new states. However, the `Shell`
component also mutates state directly through `s.state.Update()` closures
scattered across `shell.gsx` — e.g., `submitComposer()` at
`internal/interfaces/tui/shell.gsx:356`, `updateComposer()` at line 338,
`backspaceComposer()` at line 345. There is no single reducer boundary.

The new cockpit replaces `ShellState` with `CockpitState` — an immutable
snapshot containing typed entity slices and a typed enum for transcript kinds.
All mutations go through `Reduce(state, event)`, which is a pure function:
it accepts the current state and a typed event, returns a new state, and
performs no IO or side effects. The go-tui `State[CockpitState]` is updated
only by calling `state.Set(Reduce(state.Get(), event))` from watcher callbacks
or key handlers. No closure may directly call `state.Update()` with arbitrary
mutations — the reducer is the single entry point.

The existing `ShellState` is preserved for the legacy shell (coexist decision)
but is not used by the new cockpit screens. The old shell continues to mutate
`ShellState` through its scattered closures; the new cockpit uses the reducer
exclusively.

**Rationale:** Scattered state mutations make it impossible to reason about
state transitions, reproduce bugs, or write deterministic golden tests. A
single reducer function enables: (1) logging every state transition for
debugging; (2) time-travel debugging by recording the event stream; (3)
snapshot testing by comparing `CockpitState` before and after known events;
(4) exhaustive coverage — every possible event is handled in one place. The
typed `EntryKind` enum (replacing the 18+ string-keyed transcript kinds
scattered across `models.go:75` and `event_stream.go:254`) ensures
compile-time exhaustiveness checks when new kinds are added.

**Alternatives Considered:**

- **Defend `ShellState` with a wrapper** — keep `ShellState` but add a
  `Mutate(fn func(*ShellState))` method that logs all mutations. Rejected
  because: this addresses observability but not correctness — scattered
  closures can still produce inconsistent intermediate states. A reducer
  guarantees that every state transition is atomic and reproducible.

- **Immutable event-sourced model** — store the full event log and recompute
  state from scratch on every render. Rejected because: the transcript can
  grow unbounded (capped at `transcriptLimit` per `shell_state.go:145`), and
  recomputing from scratch would be O(n) per render. The reducer pattern is
  O(1) per event while still providing reproducibility.

### Decision (e): Typed Enum for Transcript/Event Kinds

**Decision:** Define `type EntryKind int` with named constants covering all 18
current string kinds: `KindPending`, `KindAsk`, `KindReply`, `KindEvent`,
`KindCmd`, `KindDraft`, `KindStatus`, `KindError`, `KindTool`, `KindCounts`,
`KindNext`, `KindBrief`, `KindEpic`, `KindInflight`, `KindAgent`,
`KindConsole`, `KindConnected`, `KindHeartbeat`. A helper function
`ParseEntryKind(s string) EntryKind` maps legacy strings to the typed enum.
New kinds are added as constants; the compiler enforces exhaustive handling in
switch statements.

**Rationale:** `TranscriptEntry.Kind` at `internal/interfaces/tui/models.go:75`
is a `string`. The 18 kind values are scattered across the codebase with no
central definition, no exhaustive switch, and no compile-time guarantee that
a new kind is handled everywhere. The `MapConsoleStreamEventToTranscriptEntry`
function at `internal/interfaces/tui/event_stream.go:254` maps event types to
kinds using string comparison — adding a new event type requires finding and
updating every string comparison. A typed enum centralizes the definition,
enables exhaustive switch checks (Go's `go vet` catches missing cases when
`default` is omitted), and makes the kind set discoverable via `go doc`.

**Alternatives Considered:**

- **String constants (`const KindAsk = "ask"`)** — centralize the string
  values but keep the type as `string`. Rejected because: string constants do
  not enable exhaustive switch checking and string comparisons are still
  scattered across the codebase.

### Decision (f): Async Boot with Loading State

**Decision:** The new cockpit boots asynchronously: `NewCockpit()` returns
immediately without making any HTTP calls. The initial `CockpitState` has a
`BootPhase` field set to `BootLoading`. The go-tui app starts rendering
immediately, showing a loading skeleton in the Main lane. A background
goroutine fetches initial data (agent list, optionally agent detail) and
enqueues a `BootReady` or `BootError` event through the reducer.

**What renders during the boot gap:** The Main lane renders a `LoadingState`
widget — a centered spinner animation (frame-cycled via `tui.OnTimer(500ms)`)
with the text `"Connecting to {api-base-url}..."`. The Detail lane renders an
`EmptyState` widget with the text `"Select an agent to inspect"`. The Evidence
lane is collapsed (drawer closed). The status footer shows a degraded
connection indicator. No mock data, no static marketing strings, no fabricated
entries. The user sees an honest loading state that names the target URL and
can press ESC to quit at any time.

If the API is unreachable, the loading state transitions to an error state
after a configurable timeout (default 5s). The error state names the URL,
shows a retry hint (`"Press r to retry or ESC to quit"`), and preserves
keyboard responsiveness. If the daemon comes online within the timeout window,
loading transitions directly to `BootReady` — never to error.

**Rationale:** The current boot at `internal/interfaces/tui/live_state.go:12`
calls `LoadInitialShellState()` synchronously during `NewApp()`, blocking the
UI thread until all HTTP calls complete. If the API is slow or unreachable,
the terminal hangs with no loading indicator and no way to quit. This violates
DESIGN.md's "Runtime First" principle and makes the TUI unusable during
network issues. Async boot ensures the terminal is painted within the first
frame (typically <100ms), and the user can interact (quit, resize) during the
loading phase.

**Alternatives Considered:**

- **Timeout-only fix** — keep synchronous boot but add a context timeout.
  Rejected because: even with a timeout, the UI thread blocks for the
  duration of the timeout. A 5-second block is unacceptable for an operator
  console.

### Decision (g): `.gsx` Toolchain Documentation

**Decision:** The `.gsx` toolchain is documented in full in the
[.gsx Toolchain](#gsx-toolchain) section below. This section serves as the
canonical reference for LLM authors working on the TUI.

**Rationale:** The audit (section (h), pain point #5) identified that the
relationship between `shell.gsx` and `shell_gsx.go` is undocumented. An LLM
that edits `shell_gsx.go` directly will have its changes overwritten on the
next `go generate`. This is a critical authoring hazard that must be addressed
explicitly.

**Alternatives Considered:**

- **Pointer to go-tui docs only** — link to the upstream `go-tui` `.gsx`
  documentation without project-specific guidance. Rejected because: the
  upstream docs do not specify the exact regeneration command, the
  forbidden-edit glob, or the project-specific workflow for adding new views.

### Decision (h): Empty/Loading/Error State Conventions

**Decision:** Every widget and screen must handle three states explicitly:

1. **Empty** — no data available (zero agents, no transcript, no rooms). Shows
   an `EmptyState` widget with: an icon or status indicator, a one-line
   description (`"No agents running"`), and a CTA with the action the user
   should take (`"Run foxctl agent spawn --role researcher to create one"`).
   Empty state copy must reference the specific entity type and suggest a
   concrete next step.

2. **Loading** — data is being fetched. Shows a `LoadingState` widget with: a
   spinner animation (cycled via `tui.OnTimer`), a one-line description
   (`"Loading agents..."`), and a pulsing border or background accent. Loading
   states must not show stale data — the widget is replaced entirely, not
   overlaid.

3. **Error** — a fetch or operation failed. Shows an `ErrorState` widget with:
   a red status indicator, the error message, the endpoint or operation that
   failed, a retry hint (`"Press r to retry"`), and an option to dismiss
   (`ESC`). Error states must preserve keyboard responsiveness — the user
   must be able to quit, navigate away, or retry without restarting the TUI.

These three states are encoded as a `Phase` enum on each widget's state. The
widget's render function switches on `Phase` and delegates to the appropriate
sub-widget. No widget renders a blank box — every state has visible content.

**Rationale:** The audit (section (h), pain point #7) found no widget-level
conventions for empty, loading, or error states. The current `WorkersRail` at
`internal/interfaces/tui/shell_gsx.go:1164` shows an empty box when no agents
exist, and the TUI fails to start entirely when the API is unreachable.
DESIGN.md requires "empty states with action" and "loading states that explain
what is being prepared" and "error states that preserve nearby context". The
three-state convention makes every widget's behavior predictable and ensures
the operator always knows what is happening.

**Alternatives Considered:**

- **Per-widget ad-hoc states** — let each widget define its own empty/loading
 /error rendering. Rejected because: ad-hoc states lead to inconsistent
  behavior (some widgets show empty boxes, others show error text, others
  crash). A shared convention ensures uniformity and makes it easy for LLM
  authors to implement new widgets correctly.

### Decision (i): Fixture Strategy for Testing

**Decision:** Use a two-tier fixture strategy:

1. **Per-test isolated daemon** — an exported Go function
   (`testfixture.BootDaemon(ctx, t, seedOpts)`) that boots `foxctl web serve -p 0`
   with a temp `FOXCTL_STORAGE_ROOT`, parses the OS-chosen port from stderr,
   seeds N agents deterministically (returning IDs to the caller), and
   registers `t.Cleanup` for teardown. This is used for all integration and
   end-to-end tests that need a live daemon. The fixture handles fatal paths —
   `t.Fatal` still triggers cleanup.

2. **In-process fake API** — `httptest.Server`-based fakes for adapter-level
   unit tests. Each adapter test creates a fake server that returns
   deterministic responses without booting the full daemon. This is the fast
   path (milliseconds per test) and covers adapter serialization, error
   handling, and edge cases.

MockTerminal-based go-tui widget tests use neither fixture — they test the
render and input handling of isolated widgets against typed state, with no
HTTP at all. Tuistory flow tests use the per-test daemon fixture.

**Rationale:** The existing test suite uses `httptest.Server` fakes
effectively (e.g., `api_client_test.go`, `agent_adapter_test.go`) for adapter
tests. The per-test daemon fixture extends this pattern to integration and
end-to-end tests, providing a real daemon with real agent state. The two-tier
strategy separates fast unit tests (milliseconds, no daemon) from slower
integration tests (seconds, real daemon), keeping CI fast while ensuring
end-to-end correctness. The fixture must use `-p 0` to avoid colliding with
the user's running daemon on port 8090 (a mission boundary).

**Alternatives Considered:**

- **Daemon-only testing** — run all tests against the live daemon. Rejected
  because: booting the daemon takes ~1–2 seconds per test, making the unit
  test suite unacceptably slow for TDD cycles. The two-tier strategy keeps
  unit tests fast.

- **Record/replay fixtures** — capture HTTP responses and replay them. Rejected
  because: the daemon API is not stable enough for recorded fixtures to
  remain valid across development iterations. Record/replay also does not
  exercise the full SSE lifecycle (connection, streaming, cancellation).

---

## .gsx Toolchain

This section documents the `.gsx` code generation toolchain for LLM authors
working on the TUI. It satisfies VAL-DOCS-010.

### Regeneration Command

```bash
go generate github.com/grindlemire/go-tui/cmd/tui@v0.11.0 ./internal/interfaces/tui/...
```

Or equivalently (if the `tui` CLI is installed):

```bash
tui generate ./internal/interfaces/tui/...
```

The generated file header confirms the toolchain:

```go
// Code generated by tui generate. DO NOT EDIT.
// Source: shell.gsx
```

### Editable and Forbidden-Edit Globs

| Pattern | Rule |
|---------|------|
| `*.gsx` | **Editable.** These are the source-of-truth template files. LLMs and developers edit these directly. |
| `*_gsx.go` | **Forbidden to hand-edit.** These files are generated from `.gsx` sources. Any manual changes will be overwritten on the next `tui generate` run. |
| `*_test.go` | **Editable.** Test files are always hand-written. |

### "Add a New View" Checklist

When adding a new view (e.g., a `RoomsScreen`) that renders via `.gsx`:

1. **Create the `.gsx` source file** — e.g., `internal/interfaces/tui/rooms_screen.gsx`.
   Define the component struct and its `Render()` template method following the
   go-tui component pattern (see `research-go-tui.md` section (b)).

2. **Run the regeneration command** — execute `tui generate ./internal/interfaces/tui/...`
   to produce `internal/interfaces/tui/rooms_screen_gsx.go`.

3. **Write hand-written companion files** — create `rooms_screen_state.go` for
   state logic, `rooms_screen_adapter.go` for the API adapter, etc.

4. **Write tests** — create `rooms_screen_test.go` with MockTerminal-based unit
   tests covering focus, navigation, empty, loading, and error states.

5. **Register the new screen** — wire the new component into the cockpit's
   runtime registry (Decision (b)) and add a navigation path from the
   information-architecture keybinding table.

6. **Commit both `.gsx` and `*_gsx.go` in the same commit** — VAL-CROSS-004
   requires that generated artifacts are always committed alongside their
   source.

### Generated Artifact Paths

These files are generated from `.gsx` sources and must never be hand-edited:

| Generated File | Source |
|---------------|--------|
| `internal/interfaces/tui/shell_gsx.go` | `internal/interfaces/tui/shell.gsx` |

When new `.gsx` files are added, the corresponding `*_gsx.go` file joins this
list.

---

## Traceability Table

This table maps audit findings from [audit-current-tui.md](./audit-current-tui.md)
section (h) to the architectural decisions above (or marks them deferred with
rationale). It satisfies VAL-DOCS-012.

| # | Audit Finding | Source (path:line) | Decision | Resolution |
|---|--------------|-------------------|----------|------------|
| 1 | Chained `NewShell*` constructors | `shell.gsx:38–95` | Decision (b): Runtime registry | The registry replaces the constructor chain with a single `RegisterRuntime()` call. New runtimes no longer require modifying four constructors. |
| 2 | Three near-duplicate runtime goroutines | `console_stream_pump.go:63`, `console_ask_runtime.go:109`, `console_cancel_runtime.go:85` | Decision (c): Shared generic bounded-runtime | Two runtimes (`ConsoleAskRuntime`, `ConsoleCancelRuntime`) delegate to `Bounded[Req, Upd]`, eliminating ~80% of their duplicated boilerplate. `ConsoleStreamPump` retains an independent goroutine lifecycle because its source-driven SSE callback model does not fit `Bounded`'s request-driven handler pattern (see Decision (c) §1 for rationale). |
| 3 | String-keyed transcript kinds | `models.go:75`, `event_stream.go:254` | Decision (e): Typed EntryKind enum | `EntryKind int` with 18 named constants replaces string keys. `ParseEntryKind()` maps legacy strings. |
| 4 | Ambient `ShellState` with no reducer boundary | `shell_state.go:8`, `shell.gsx:338–356` | Decision (d): Typed entity model + reducer | New cockpit uses `CockpitState` with a single `Reduce()` function. Legacy `ShellState` preserved for coexist. |
| 5 | Synchronous boot blocks UI thread | `live_state.go:12` | Decision (f): Async boot with loading state | `NewCockpit()` returns immediately; background fetch; loading/error/ready phases. |
| 6 | Undocumented `.gsx` toolchain | No documentation existed | Decision (g) + [`.gsx Toolchain`](#gsx-toolchain) section | Full toolchain documentation with regeneration command, editable/forbidden globs, and add-a-view checklist. |

---

## Surface Ownership Table

This table maps the new cockpit surfaces to the five DESIGN.md owner surfaces
(Runtime, Companion, Rooms, Orchestration, Events) and flags duplications
with stated resolutions. It satisfies VAL-DOCS-014.

| Cockpit Surface | DESIGN.md Owner | Description | Duplication Resolution |
|----------------|-----------------|-------------|----------------------|
| Agent Inventory (Main lane) | **Runtime** | Live agent list, status, role, parent link, last activity. Primary triage surface. | No duplication — this is the canonical Runtime surface in the TUI. |
| Agent Detail (Detail lane) | **Runtime** + **Companion** | Selected agent's runtime snapshot, hierarchy, recent transcript preview. | Shares transcript preview with Companion. Resolution: transcript preview in Detail lane is summary-only (last N entries). Full conversation is in the Companion composer flow. |
| Composer (Ask/Chat) | **Companion** | Text input for streaming ask/chat with the selected agent. Full conversation transcript. | No duplication — this is the canonical Companion surface in the TUI. |
| Evidence Drawer | **Events** | Raw payloads for transcript rows, tool calls, errors. Forensic inspection. | No duplication — this is the canonical Events surface in the TUI. Drawer opens on demand and does not compete with runtime or companion for layout space. |
| Rooms List (secondary flow) | **Rooms** | Room directory, membership, latest messages. | No duplication in M3 — Rooms is a secondary flow not implemented in the walking skeleton. When implemented, it will be a separate screen reachable via keybinding, not a competing panel in the primary layout. |
| Status Footer | **Runtime** | Connection health, active entity label, keybinding hints. | Summary-only — does not duplicate Runtime's agent inventory or Companion's transcript. Footer is informational, not interactive. |
| Memory / Continuity Rail | **Companion** | Per-agent memory context, ACA/continuity state. | Preserved from the legacy shell for the Companion flow. In the new cockpit, this is accessible from the Detail lane when a companion session is active. Not a separate top-level surface. |
| Orchestration Board | **Orchestration** | Issue flow, board state, execution grouping. | Deferred to follow-up mission. The TUI has no Orchestration surface in M3. When implemented, it will be a separate screen (like Rooms) with its own keybinding. |

---

## Reconciliation

This section reconciles the new three-lane cockpit model with the prior
four-region shell plan in [docs/plans/go-tui-agent-shell.md](../go-tui-agent-shell.md)
and maps DESIGN.md principles to concrete decisions. It satisfies VAL-DOCS-009.

### Relationship to the Four-Region Shell

The canonical plan at [docs/plans/go-tui-agent-shell.md](../go-tui-agent-shell.md)
specifies a four-region layout: TopBar, Center (transcript), Bottom (composer),
and Right Rail (tabbed: Memory, Continuity, Workers, Task). The new three-lane
model **layers on top of** this plan rather than replacing it:

- **TopBar** is preserved as-is (workspace, epic, assistant, mode, in-flight).
  It becomes the header strip above the three lanes.

- **Center** (transcript) is split into two surfaces: the **Main lane** now
  shows the agent inventory (the primary operational view per DESIGN.md's
  "Runtime First" principle), and the **Companion** sub-flow within the Main
  lane shows the transcript when a conversation is active. The transcript is
  no longer the default screen — it is reached by selecting an agent and
  entering the ask/chat flow.

- **Bottom** (composer) is preserved as part of the Companion sub-flow. It is
  not a permanent fixture of the three-lane layout — it appears when the user
  is in an active conversation.

- **Right Rail** is replaced by the **Detail lane** (center) and **Evidence
  lane** (right, drawer). The rail's four tabs (Memory, Continuity, Workers,
  Task) are redistributed: Memory and Continuity become sections within the
  Detail lane when a companion session is active; Workers becomes the agent
  inventory in the Main lane (with hierarchy, per DESIGN.md's "Multi-Agent
  Work Is Coordinated, Not Collapsed" principle); Task is deferred to the
  Orchestration surface (follow-up mission).

The four-region shell remains valid for the legacy companion-only mode (the
coexist path in Decision (a)). The three-lane layout is the new default for
the operator cockpit.

### DESIGN.md Principle Mapping

The following DESIGN.md principles (defined at [DESIGN.md](../../../DESIGN.md))
map directly to concrete decisions in this document:

1. **"Runtime First"** (DESIGN.md §1) → Decision (f): Async boot ensures the
   terminal is painted immediately with live loading state, not mock data. The
   default screen shows the agent inventory, not a plan preview. Decision (h):
   empty/loading/error conventions ensure every widget surface is honest about
   its data state.

2. **"Main Lane, Detail Lane, Evidence Lane"** (DESIGN.md §2) → Adopted
   directly as the three-lane layout. The Main lane is the operational surface
   (agent inventory), the Detail lane is the selected-entity inspector, and the
   Evidence lane is the raw-payload drawer. No flattening into peer top-level
   surfaces.

3. **"Summary First, Raw Second"** (DESIGN.md §4) → Decision (d): the reducer
   pattern separates summary state (rendered in Main and Detail lanes) from raw
   payloads (stored in the event log, exposed via the Evidence drawer on
   demand). Decision (h): transcript rows show summaries by default; the user
   opens the Evidence drawer to see raw SSE payloads.

4. **"Honest Surfaces"** (DESIGN.md §6) → Decision (f): the loading state
   explicitly names the target URL and shows a spinner — never mock data.
   Decision (h): error states name the failed endpoint and show retry guidance.
   The default plan preview with fabricated entries ("Codex", "gpt-5.4") is
   eliminated in the new cockpit.

5. **"Multi-Agent Work Is Coordinated, Not Collapsed"** (DESIGN.md §5) →
   Decision (c): the bounded runtime generic enables adding new runtimes
   (events subscription, rooms subscription) without boilerplate proliferation.
   The agent inventory in the Main lane shows hierarchy (parent/child) rather
   than a flat list.

---

## Cross-References

- [audit-current-tui.md](./audit-current-tui.md) — audit findings cited in the
  Traceability Table.
- [research-go-tui.md](./research-go-tui.md) — go-tui v0.11.0 API reference.
- [information-architecture.md](./information-architecture.md) — three-lane
  layout, keybinding table, progressive reveal, screen inventory.
- [component-spec.md](./component-spec.md) — per-widget contract for M2.
- [integration-map.md](./integration-map.md) — API/stream adapter table.
- [docs/plans/go-tui-agent-shell.md](../go-tui-agent-shell.md) — prior
  four-region shell plan (reconciled above).
- [DESIGN.md](../../../DESIGN.md) — product shape and UX principles.
- [AGENTS.md](../../../AGENTS.md) — engineering and runtime rules.
