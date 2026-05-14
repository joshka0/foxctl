# Audit: Current TUI (`internal/interfaces/tui/`)

This document is a structured audit of the existing Go TUI implementation at
`internal/interfaces/tui/` on branch `feat/tui-go` (as of commit `d13c8640`).
It is an M1 deliverable for the TUI operator cockpit redesign and satisfies
VAL-DOCS-002.

---

## (a) File Map

| File | Lines | Purpose |
|------|-------|---------|
| `app.go` | ~180 | Entry point: `Run()`, `NewApp()`, `newShellRuntime()` wiring |
| `shell.gsx` | ~706 | `.gsx` source: Shell component constructors, keymap, event handlers, state mutations |
| `shell_gsx.go` | ~1629 | **Generated** from `shell.gsx`: view structs (TopBar, TranscriptPane, ComposerPane, RailTabs, MemoryRail, ContinuityRail, WorkersRail, TaskRail, RightRail, Footer), `Shell.Render()` |
| `shell_state.go` | ~170 | `ShellState` struct, `DefaultShellState()`, `ApplyConsoleStreamEvent[s]()`, `AttachAskCorrelation()`, transcript capping |
| `live_state.go` | ~304 | `LoadInitialShellState()`: synchronous HTTP boot, agent/console enrichment, worker mapping |
| `models.go` | ~90 | `Options`, `FocusPane`, `RailTab`, `ShellState` fields, `TranscriptEntry`, `WorkerSummary`, `MemorySummary`, `ContinuitySummary` |
| `keys.go` | ~35 | `focusPaneForIndex()`, `nextRail()`, `stopBindings()` (ESC, q, Ctrl+C) |
| `stream_config.go` | ~15 | `shouldAttachConsoleStream()`, `shouldAttachAgentCompanion()` |
| `api_client.go` | ~180 | `APIClient`: generic HTTP client with `RequestJSON()`, URL normalization, error types |
| `agent_adapter.go` | ~130 | `AgentAdapter`: typed calls for `GET /api/agents`, `GET /api/agents/{id}`, `POST /api/agents/{id}/ask` |
| `console_adapter.go` | ~200 | `ConsoleAdapter`: typed calls for `/api/console/sessions/*` surfaces (list, get, create, ask, cancel) |
| `epic_adapter.go` | ~447 | `LoadShellState()`: loads epic from `.foxctl/epics/{id}/meta.json` when `--epic-id` is set |
| `console_stream_pump.go` | ~140 | `ConsoleStreamPump`: bounded goroutine reading SSE stream, emitting `ConsoleStreamUpdate` |
| `console_ask_runtime.go` | ~260 | `ConsoleAskRuntime`: bounded goroutine submitting ask requests, emitting `ConsoleAskUpdate` |
| `console_cancel_runtime.go` | ~230 | `ConsoleCancelRuntime`: bounded goroutine submitting cancel requests, emitting `ConsoleCancelUpdate` |
| `event_stream.go` | ~560 | SSE parser (`ParseConsoleEventStream`), event normalization, `MapConsoleStreamEventToTranscriptEntry` |
| `event_stream_reader.go` | ~60 | `ReadConsoleEventStream()`: HTTP SSE connection + streaming parse |
| `smoke_agent.go` | ~140 | Non-interactive agent smoke: enqueues one ask, reports outcome |
| `smoke_console.go` | ~300 | Non-interactive console smoke: stream + ask + cancel lifecycle |
| `*_test.go` (12 files) | ~2,500 | Unit tests for all adapters, runtimes, state, shell, event parsing |
| `shell_footer_test.go` | ~55 | Footer rendering test |
| `shell_state_stream_test.go` | ~260 | Stream→transcript state update tests |
| `shell_state_test.go` | ~430 | Core ShellState mutation tests |
| `app_runtime_test.go` | ~130 | `newShellRuntime` wiring tests |
| `live_state_test.go` | ~340 | `LoadInitialShellState` integration tests |

**Total:** ~7,800 lines of Go (including tests), plus the `.gsx` source.

---

## (b) Boot + Runtime Flow Narrative

The TUI boots through a synchronous, blocking sequence:

1. **`Run(ctx, opts)`** in `internal/interfaces/tui/app.go:11` creates the app via `NewApp()`.
2. **`NewApp(ctx, opts)`** in `internal/interfaces/tui/app.go:34` calls `LoadInitialShellState()` **synchronously** — this blocks the UI thread until all HTTP calls complete.
3. **`LoadInitialShellState()`** in `internal/interfaces/tui/live_state.go:12` creates an `APIClient`, an `AgentAdapter`, calls `ListAgents()`, optionally calls `GetAgent()` or `GetSession()`, and maps results into `ShellState`. This is a **synchronous HTTP boot** — the terminal is not painted until all these calls return.
4. **`newShellRuntime()`** in `internal/interfaces/tui/app.go:54` wires the shell with runtimes. It checks `shouldAttachConsoleStream()` (`internal/interfaces/tui/stream_config.go:5`) and `shouldAttachAgentCompanion()` (`internal/interfaces/tui/stream_config.go:9`) to decide which runtimes to start.
5. **`NewConsoleStreamPump()`** at `internal/interfaces/tui/console_stream_pump.go:73` starts a goroutine that reads the SSE stream.
6. **`NewConsoleAskRuntime()`** at `internal/interfaces/tui/console_ask_runtime.go:122` starts a goroutine that submits enqueued ask requests.
7. **`NewConsoleCancelRuntime()`** at `internal/interfaces/tui/console_cancel_runtime.go:117` starts a goroutine that submits enqueued cancel requests.
8. **`NewShellWithRuntimes()`** in `internal/interfaces/tui/shell.gsx:72` receives all channels and callbacks, stores them in the `Shell` struct.
9. **`gotui.NewApp()`** at `internal/interfaces/tui/app.go:41` creates the go-tui app with the Shell as root component.
10. **`app.Run()`** at `internal/interfaces/tui/app.go:22` starts the terminal event loop, which calls `Shell.Render()` at `internal/interfaces/tui/shell_gsx.go:1555`.

Each runtime goroutine follows the same pattern: `ctx` + `cancel` via `context.WithCancel`, `stopOnce` via `sync.Once`, `waitGroup` via `sync.WaitGroup`, and a `run()` method that reads from a `requests` channel and writes to an `updates` channel.

**Key observation:** The three runtime goroutines (`ConsoleStreamPump.run()` at `internal/interfaces/tui/console_stream_pump.go:104`, `ConsoleAskRuntime.run()` at `internal/interfaces/tui/console_ask_runtime.go:154`, and `ConsoleCancelRuntime.run()` at `internal/interfaces/tui/console_cancel_runtime.go:150`) are near-identical in structure. Each has its own `sendUpdate()` method, its own buffer-size constants, its own `Enqueue()`/`Updates()`/`Stop()`/`Close()` surface, and its own `requests`/`updates` channel pair. This duplication is a primary target for the generic `Bounded[Req, Upd]` runtime in M2.

---

## (c) Adapter Inventory

The TUI has five adapter-shaped files:

| Adapter | File | API Surface | Key Methods |
|---------|------|-------------|-------------|
| `APIClient` | `api_client.go` | Generic HTTP | `RequestJSON()` at `api_client.go:63`, `endpointURL()` at `api_client.go:130` |
| `ConsoleAdapter` | `console_adapter.go` | `/api/console/sessions/*` | `ListSessions()` at `console_adapter.go:113`, `GetSession()` at `console_adapter.go:127`, `AskSession()` at `console_adapter.go:143`, `CancelSession()` at `console_adapter.go:157` |
| `AgentAdapter` | `agent_adapter.go` | `/api/agents/*` | `ListAgents()` at `agent_adapter.go:69`, `GetAgent()` at `agent_adapter.go:96`, `AskAgent()` at `agent_adapter.go:79` |
| `EpicAdapter` (implicit) | `epic_adapter.go` | Local filesystem `.foxctl/epics/` | `LoadShellState()` at `epic_adapter.go:105`, `mapEpicToShellState()` at `epic_adapter.go:180` |
| `EventStream` | `event_stream.go` + `event_stream_reader.go` | `/api/console/sessions/{id}/events` (SSE) | `ParseConsoleEventStream()` at `event_stream.go:73`, `ReadConsoleEventStream()` at `event_stream_reader.go:25` |

**Gaps in the adapter layer:**

1. **No `/api/events` hub subscription.** The TUI only subscribes to console session event streams (`internal/interfaces/tui/event_stream_reader.go:25`). There is no adapter for the global `/api/events` SSE endpoint. Live agent inventory refresh requires polling `GET /api/agents` — no push-based update path exists in the current TUI.
2. **No agent hierarchy adapter.** Agent hierarchy (parent/child relationships) is only available via JSON-RPC, which the TUI does not call. The `AgentRecord.ParentID` field exists (`internal/interfaces/tui/agent_adapter.go:22`) but no code traverses the hierarchy.
3. **No rooms adapter.** There is no `/api/rooms` integration. The TUI has no awareness of rooms, room membership, or room messages.
4. **No streaming ask adapter.** The `AgentAdapter.AskAgent()` at `internal/interfaces/tui/agent_adapter.go:79` uses `RequestJSON` — a synchronous request/response call. It does not consume the `POST /api/agents/{id}/ask-stream` SSE endpoint. Agent replies come back as a single response string, not streamed tokens.

---

## (d) Current Information Architecture

The current IA is a **four-region shell** (matching `docs/plans/go-tui-agent-shell.md`):

```
┌──────────────────────────────────────────────────────────────┐
│ TopBar: workspace | epic | assistant | mode | in-flight      │
├──────────────────────────────────────┬───────────────────────┤
│                                      │ Right Rail (tabbed):  │
│  TranscriptPane                      │  • Memory             │
│  (scrollable transcript)             │  • Continuity         │
│                                      │  • Workers            │
│                                      │  • Task               │
├──────────────────────────────────────┤                       │
│  ComposerPane (input)                │                       │
├──────────────────────────────────────┴───────────────────────┤
│ Footer: keybindings hint                                      │
└──────────────────────────────────────────────────────────────┘
```

**Focus model** (`internal/interfaces/tui/models.go:26–32`): Four `FocusPane` values cycle via Tab: `FocusTranscript` → `FocusComposer` → `FocusRail` → `FocusWorkers`.

**Rail tabs** (`internal/interfaces/tui/models.go:35–43`): `RailMemory` (0), `RailContinuity` (1), `RailWorkers` (2), `RailTask` (3). Cycled via Ctrl+M/Y/W/B or ←/→ arrow keys.

**Default state** (`internal/interfaces/tui/shell_state.go:28–82`): Without `--api-base-url`, the shell renders a **plan preview** with mock data: epic title "Go TUI Agent Shell", assistant "Codex" / "gpt-5.4", placeholder transcript entries, and static memory items. This is the state users see when running `make go-tui-agent` without a live daemon.

**Navigation keys** (`internal/interfaces/tui/shell.gsx:279–314` and `internal/interfaces/tui/keys.go:17–23`):
- Tab cycles focus panes
- Ctrl+M/Y/W/B switch rail tabs
- ←/→ cycle rail tabs
- Enter submits composer (when focused)
- Ctrl+C cancels in-flight request
- ESC/q/Ctrl+C stop the app

---

## (e) DESIGN.md Gap Analysis

The current TUI falls short of several DESIGN.md principles:

### 1. Runtime First

> DESIGN.md: "The default experience should prioritize live operations over passive browsing."

**Gap:** The default state at `internal/interfaces/tui/shell_state.go:28–82` is a **plan preview** with static marketing content: epic title "Go TUI Agent Shell", assistant "Codex" with model "gpt-5.4", and placeholder entries like `"Plan preview loaded. No companion agent is attached yet"`. This is the opposite of runtime-first — the user sees mock data, not live agent state. A user launching the TUI without arguments sees no operational information about the running system.

### 2. Main Lane, Detail Lane, Evidence Lane

> DESIGN.md: "Use a three-lane mental model: Main lane, Detail lane, Evidence lane."

**Gap:** The current layout is a **two-column** layout (transcript + rail). There is no Detail lane (no selected-entity inspection panel) and no Evidence lane (no raw-payload drawer). The `RightRail` at `internal/interfaces/tui/shell_gsx.go:1349` renders static context tabs — not entity details or evidence. The user cannot select a transcript row and drill into raw payloads, tool call details, or error traces.

### 3. Multi-Agent Work Is Coordinated, Not Collapsed

> DESIGN.md: "The UI should present a multi-agent system as a graph of cooperating actors."

**Gap:** Workers are rendered as a flat list in the `WorkersRail` view at `internal/interfaces/tui/shell_gsx.go:1164`. There is no hierarchy visualization, no parent/child relationship display, and no role-specific distinction beyond name + status + task string. The `AgentRecord.ParentID` field exists at `internal/interfaces/tui/agent_adapter.go:22` but is never displayed or used for hierarchy rendering.

### 4. Honest Surfaces

> DESIGN.md: "If a surface is projection-based or heuristic, the UI must say so through structure and copy."

**Gap:** The default plan preview at `internal/interfaces/tui/shell_state.go:28–82` contains hardcoded entries like `"EpicStatus": "READY"`, `"Name": "Codex"`, `"Model": "gpt-5.4"`, and `"Examples: 'review git diff'"`. These are presented as if they are real data, but they are entirely fabricated. The footer prompt at `internal/interfaces/tui/shell.gsx:486` shows `"Read-only plan preview"` only when the composer is empty — when the user starts typing, the hint disappears and the mock state is indistinguishable from live data.

### 5. Summary First, Raw Second

> DESIGN.md: "Show summaries first; make raw payloads explicit and opt-in."

**Gap:** There is no mechanism for progressive reveal. Transcript entries at `internal/interfaces/tui/models.go:73–78` contain only `Speaker`, `Kind`, `Text`, and `CorrelationID`. The raw SSE event data is discarded during mapping at `internal/interfaces/tui/event_stream.go:254` (`MapConsoleStreamEventToTranscriptEntry`). There is no way to access the original payload after it has been normalized into a transcript row.

### 6. Surface Ownership

> DESIGN.md: "Canonical home for: live agent inventory, state transitions, direct lifecycle actions."

**Gap:** The TUI has no dedicated Runtime surface. Agent inventory is flattened into a worker list in the rail (`WorkersRail` at `internal/interfaces/tui/shell_gsx.go:1164`). There is no Rooms surface, no Orchestration surface, and no Events surface. The entire TUI is oriented around a single companion agent or console session.

---

## (f) Component Inventory

### View components (generated from `.gsx`)

Each view struct follows the go-tui `Component` interface (`BindApp`, `UnbindApp`, `GetRoot`, `GetWatchers`, `Render`, `UpdateProps`):

| Component | Constructor (in `shell_gsx.go`) | Lines | Purpose |
|-----------|------|-------|---------|
| `TopBarView` | `TopBar()` at line 609 | ~75 | Workspace, epic, assistant, mode, in-flight indicator |
| `TranscriptPaneView` | `TranscriptPane()` at line 721 | ~70 | Scrollable list of transcript entries |
| `ComposerPaneView` | `ComposerPane()` at line 828 | ~50 | Text input for ask/compose |
| `RailTabsView` | `RailTabs()` at line 912 | ~40 | Tab header for rail sections |
| `MemoryRailView` | `MemoryRail()` at line 986 | ~50 | Memory summary cards |
| `ContinuityRailView` | `ContinuityRail()` at line 1071 | ~60 | Epic/continuity context |
| `WorkersRailView` | `WorkersRail()` at line 1164 | ~60 | Worker list (name, status, task) |
| `TaskRailView` | `TaskRail()` at line 1260 | ~55 | Task/planning placeholder |
| `RightRailView` | `RightRail()` at line 1349 | ~130 | Tabbed rail container |
| `FooterView` | `Footer()` at line 1512 | ~45 | Keybinding hints, cancel status |

### Runtime components (hand-written)

| Component | File | Goroutine | API |
|-----------|------|-----------|-----|
| `ConsoleStreamPump` | `console_stream_pump.go` | Yes | `Enqueue()` (none — source-driven), `Updates()`, `Stop()`, `Close()` |
| `ConsoleAskRuntime` | `console_ask_runtime.go` | Yes | `Enqueue(ctx, req)`, `Updates()`, `Stop()`, `Close()` |
| `ConsoleCancelRuntime` | `console_cancel_runtime.go` | Yes | `Enqueue(ctx, req)`, `Updates()`, `Stop()`, `Close()` |

### Adapter components (hand-written)

| Component | File | External dependency |
|-----------|------|-------------------|
| `APIClient` | `api_client.go` | `http.Client` |
| `ConsoleAdapter` | `console_adapter.go` | `APIClient` |
| `AgentAdapter` | `agent_adapter.go` | `APIClient` |
| `EpicAdapter` (implicit) | `epic_adapter.go` | Local filesystem |

### Test helpers

| Component | File | Purpose |
|-----------|------|---------|
| `SmokeAgent` | `smoke_agent.go` | Non-interactive agent ask validation |
| `SmokeConsole` | `smoke_console.go` | Non-interactive console lifecycle validation |

---

## (g) Integration Gaps

### Gaps between the TUI and the daemon API

1. **No `/api/events` subscription.** The TUI does not subscribe to the global event hub. Agent spawn/delete events are not reflected without manual refresh. The `event_stream.go` file at `internal/interfaces/tui/event_stream.go:73` only parses console session SSE, not the `/api/events` SSE endpoint. This means the TUI cannot show live inventory updates.

2. **No streaming ask.** `AgentAdapter.AskAgent()` at `internal/interfaces/tui/agent_adapter.go:79` uses a synchronous `RequestJSON` POST. The daemon's `POST /api/agents/{id}/ask-stream` endpoint returns SSE tokens, but the TUI discards this capability. Agent replies appear atomically rather than streaming character-by-character.

3. **No rooms integration.** There is no adapter for `/api/rooms` or `/api/rooms/{id}/events`. The TUI has zero awareness of room coordination, room membership, or room messages. The `TaskRail` at `internal/interfaces/tui/shell_gsx.go:1260` is a placeholder that renders static text.

4. **No agent hierarchy.** The `AgentRecord.ParentID` field at `internal/interfaces/tui/agent_adapter.go:22` is fetched from the API but never rendered as a tree. There is no JSON-RPC adapter for the hierarchy endpoint mentioned in the mission architecture.

5. **No structured error surface.** Error states in transcript entries at `internal/interfaces/tui/models.go:73–78` are plain text strings. There is no structured error display with error code, retry hint, or expandable stack trace. The `handleConsoleAskUpdate()` error path at `internal/interfaces/tui/shell.gsx:196` appends a `TranscriptEntry{Kind: "error", Text: "ask failed: ..."}` but provides no UI affordance beyond the text.

6. **No backpressure policy documentation.** The runtime goroutines use bounded channels (default 16) but the backpressure behavior is implicit. `ConsoleAskRuntime.Enqueue()` at `internal/interfaces/tui/console_ask_runtime.go:140` blocks when the channel is full — there is no documented drop/block/error policy per DESIGN.md's bounded-concurrency requirement.

---

## (h) Authoring Pain Points

These are the patterns that make it difficult for Codex/Claude to extend the TUI correctly:

1. **Chained `NewShell*` constructors.** The `Shell` component is constructed through a chain of four constructors: `NewShell()` → `NewShellWithStream()` → `NewShellWithRuntime()` → `NewShellWithRuntimes()`, each adding parameters (`internal/interfaces/tui/shell.gsx:38–95`). An LLM adding a new channel (e.g., an events subscription) must modify all four constructors and understand which parameters propagate. This is fragile and error-prone.

2. **Three near-identical runtime goroutines.** `ConsoleStreamPump` (`internal/interfaces/tui/console_stream_pump.go:73–140`), `ConsoleAskRuntime` (`internal/interfaces/tui/console_ask_runtime.go:122–195`), and `ConsoleCancelRuntime` (`internal/interfaces/tui/console_cancel_runtime.go:117–190`) share ~80% structural similarity: `context.WithCancel`, `sync.Once` for stop, `sync.WaitGroup` for goroutine lifecycle, `requests`/`updates` channels, `Enqueue()`/`Updates()`/`Stop()`/`Close()` methods, and a `run()` loop with a `sendUpdate()` helper. Adding a fourth runtime (e.g., an events watcher) requires copying ~150 lines of boilerplate.

3. **String-keyed transcript kinds.** `TranscriptEntry.Kind` at `internal/interfaces/tui/models.go:75` is a `string`. The kind values are scattered across the codebase: `"pending"`, `"ask"`, `"reply"`, `"event"`, `"cmd"`, `"draft"`, `"status"`, `"error"`, `"tool"`, `"counts"`, `"next"`, `"brief"`, `"epic"`, `"plan"`, `"inflight"`, `"agent"`, `"console"`, `"connected"`, `"heartbeat"`. There is no typed enum, no exhaustive switch, and no compile-time guarantee that a new kind is handled in all mapping functions.

4. **Ambient `ShellState` with no reducer boundary.** `ShellState` at `internal/interfaces/tui/shell_state.go:8` is a mutable value type with methods like `ApplyConsoleStreamEvent()` at `internal/interfaces/tui/shell_state.go:55` and `AttachAskCorrelation()` at `internal/interfaces/tui/shell_state.go:74` that return new states. But the `Shell` component also mutates state directly through `s.state.Update()` closures scattered across `shell.gsx` (e.g., `submitComposer()` at `shell.gsx:356`, `updateComposer()` at `shell.gsx:338`, `backspaceComposer()` at `shell.gsx:345`). There is no single reducer function; state mutations are spread across the component.

5. **Undocumented `.gsx` toolchain.** The relationship between `shell.gsx` and `shell_gsx.go` is not documented anywhere in the TUI package. There is no README explaining that `shell_gsx.go` is generated, what command regenerates it, or which files an LLM should edit vs. leave alone. An LLM that edits `shell_gsx.go` directly will have its changes overwritten on the next `go generate`.

6. **Synchronous boot blocks UI thread.** `LoadInitialShellState()` at `internal/interfaces/tui/live_state.go:12` performs HTTP calls synchronously during `NewApp()`. If the API is slow or unreachable, the terminal hangs with no loading indicator. There is no mechanism for showing a loading state while waiting for the first data to arrive.

7. **No empty/loading/error state system.** The current TUI has no widget-level conventions for empty, loading, or error states. When no agents exist, the `WorkersRail` at `internal/interfaces/tui/shell_gsx.go:1164` shows an empty box. When the API is unreachable, the TUI fails to start entirely. There are no per-component loading spinners, error banners, or empty-state CTAs.

---

## (i) Strengths to Preserve

1. **Clean adapter pattern.** The `APIClient` → `ConsoleAdapter`/`AgentAdapter` separation at `api_client.go:63` and `console_adapter.go:95`/`agent_adapter.go:56` is well-structured. Each adapter wraps the generic HTTP client with typed request/response structs. Tests use `httptest.Server` fakes effectively (`api_client_test.go`, `agent_adapter_test.go`, `console_adapter_test.go`). This pattern should be preserved and extended for new adapters (rooms, events hub, hierarchy).

2. **Bounded runtime goroutines with leak-free shutdown.** The three runtimes (`ConsoleStreamPump`, `ConsoleAskRuntime`, `ConsoleCancelRuntime`) each use `context.WithCancel` + `sync.Once` + `sync.WaitGroup` for clean shutdown. `Stop()` waits for the goroutine to exit. Tests verify cleanup. This bounded-goroutine pattern is correct and should be preserved in the generic `Bounded[Req, Upd]` abstraction.

3. **Comprehensive test coverage.** The package has ~2,500 lines of tests covering adapters, runtimes, state mutations, event parsing, and smoke modes. Tests use table-driven patterns (`console_stream_pump_test.go`, `console_ask_runtime_test.go`), `httptest.Server` fakes, and edge-case coverage (empty inputs, nil receivers, context cancellation). This testing discipline should be maintained.

4. **Deterministic smoke modes.** `smoke_agent.go` and `smoke_console.go` provide non-interactive validation paths that report structured summaries (`SmokeAgentSummary`, `SmokeConsoleSummary`). These smoke modes are CI-friendly and do not require a terminal. They should be preserved and extended for new surfaces.

5. **go-tui watcher integration.** The `Shell.Watchers()` method at `internal/interfaces/tui/shell.gsx:122` correctly bridges runtime update channels to go-tui's `tui.Watch()` mechanism, causing re-renders when new data arrives. This reactive update pattern works well and should be the basis for future watchers.

6. **Transcript cap with configurable limit.** `capTranscriptEntries()` at `internal/interfaces/tui/shell_state.go:145` prevents unbounded transcript growth. The `transcriptLimit` is configurable via `Options`. This is a good memory-safety pattern that should be preserved.

7. **Separation of `.gsx` view templates from hand-written logic.** Despite the `.gsx` toolchain being undocumented, the architectural split between view templates (`.gsx` → generated `*_gsx.go`) and hand-written logic (state, adapters, runtimes) is sound. The redesign should maintain this separation while documenting the workflow.

8. **Structured SSE parsing.** `ParseConsoleEventStream()` at `internal/interfaces/tui/event_stream.go:73` handles the SSE protocol correctly with buffered scanning, line limits, and multi-line data assembly. The event normalization pipeline (`decodeWrappedStreamPayload` → `decodeConsolePayload` → `MapConsoleStreamEventToTranscriptEntry`) handles multiple wire formats. This parser should be reused or extended for new SSE endpoints.
