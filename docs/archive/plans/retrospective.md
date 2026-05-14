# M2/M3 Retrospective — foxctl TUI Operator Cockpit Redesign

This document captures what held up and what didn't during the implementation of the M2 Component Library Seed and M3 Walking Skeleton for the foxctl TUI operator cockpit redesign. Each finding is formatted with a concrete observation and a `Recommendation:` line for future work.

---

## Finding 1: The Generic `Bounded[Req, Upd]` Runtime Eliminated ~80% of Duplicated Goroutine Scaffolding

**Observation:** The three existing runtimes (`console_stream_pump`, `console_ask_runtime`, `console_cancel_runtime`) each had ~150 lines of near-identical goroutine lifecycle code (`context.WithCancel`, `sync.Once`, `sync.WaitGroup`, `requests`/`updates` channel pairs, `run()` methods). Extracting this into `runtime.Bounded[Req, Upd]` reduced each runtime to a ~20-line handler function plus a constructor call. The generic also enforced the mission's bounded-channel invariant at the type level — buffer size is a required constructor parameter, and backpressure policy is explicit.

**What held up:** The generic abstraction was straightforward to design once the common pattern was identified in the audit. The `NewBoundedSource` variant (for source-driven runtimes like stream pumps) was a minor extension that preserved the same lifecycle guarantees.

**What didn't:** Early attempts to make `Bounded` handle both request-driven and source-driven patterns in a single constructor were confusing. Splitting into `NewBounded` and `NewBoundedSource` made the API clearer.

**Recommendation:** When extracting generic runtimes, separate the "push" (request-driven) and "pull" (source-driven) patterns into distinct constructors rather than overloading a single constructor with mode flags. This keeps the API discoverable and reduces the chance of LLM authors passing the wrong handler shape.

---

## Finding 2: Typed `EntryKind` Enum Replaced String-Keyed Sprawl but Required a Legacy-String Mapper

**Observation:** The 18 transcript/event kinds were scattered across `models.go` and `event_stream.go` as raw strings. The `EntryKind` typed enum with `ParseEntryKind()` mapper provided compile-time safety and enabled exhaustive switch checking. However, the legacy shell (`shell.gsx` / `shell_gsx.go`) still uses string kinds, so the mapper is a permanent bridge, not a temporary shim.

**What held up:** The enum design was simple (`type EntryKind int` with `iota` constants). The `String()` method and `ParseEntryKind()` mapper were easy to implement and test. All 18 kinds are covered by unit tests.

**What didn't:** The coexist strategy (Decision (a)) means the legacy shell and new cockpit share the same wire format but use different internal representations. This creates a small friction point when adding new kinds — they must be added to both the enum and the legacy string handling. One M3 test initially missed a kind because it was added to the legacy path but not the enum.

**Recommendation:** Maintain a single source-of-truth list of all kinds (e.g., a code-generated const block or a registry) that feeds both the typed enum and the legacy mapper. If the legacy shell is ever deprecated, the mapper can be removed, but the list should stay centralized.

---

## Finding 3: Theme Tokens Prevented Raw Color Literal Proliferation but Required Discipline in Code Review

**Observation:** The `theme.Colors` palette with 20+ named tokens eliminated raw color literals (`tui.Cyan`, `tui.Red`, hex strings) from all M2 widget implementations. The `Palette` map provides an enumerable view for testing and tooling. Status badge variants (ok, warn, error, pending) each use distinct ANSI colors, satisfying the "not just bold-vs-not-bold" requirement.

**What held up:** The token set was designed once and referenced consistently. The `theme_test.go` file enforces distinctness of status colors and dark-first palette properties, catching regressions automatically.

**What didn't:** Snapshot test data (`.txt` golden files) and standalone demo binaries (`cmd/detailpane/main.go`) occasionally used hardcoded example strings that referenced concepts like `"review git diff"` — not color literals, but still static content that could be mistaken for marketing copy. These were caught during VAL-CROSS-001 grep and corrected to neutral examples.

**Recommendation:** Extend the "no raw color literals" rule to a broader "no static marketing strings" rule for all new TUI code. Add a `make check-marketing-strings` target that greps for known marketing phrases (`READY`, `Codex`, `gpt-5.4`, etc.) in `internal/interfaces/tui/` and fails CI if found.

---

## Finding 4: MockTerminal-Based Unit Tests Were Sufficient for Widget Logic; Tuistory Was Needed Only for Integration Flows

**Observation:** Every M2 widget (EntityList, DetailPane, Tabs, Drawer, StreamViewer, small primitives) has comprehensive MockTerminal-based unit tests covering focus, navigation, empty/loading/error states, and Unicode width. These tests run in ~2s and provide fast feedback during TDD. Tuistory snapshots were used for M3 end-to-end flows (boot, inventory, ask-stream, cancel, evidence drawer) and for capturing visual evidence of widget states, but were not required for day-to-day widget development.

**What held up:** The MockTerminal API (`tui.NewMockTerminal`, `mt.CellAt(x, y)`, `mt.StringTrimmed()`) is powerful enough to assert on raw cell buffers, including ANSI color codes. This made it possible to verify focus indicators, selection backgrounds, and status badge colors without a PTY.

**What didn't:** Tuistory setup for M3 flows was heavier — each flow requires a compiled binary, a per-test daemon fixture, and PTY interaction. The initial `skel-fixture` feature took multiple iterations to get process teardown reliable on macOS (the `lsof` port-parsing approach had edge cases with zombie processes). The fixture now uses `t.Cleanup` and explicit `syscall.Kill` for robust teardown.

**Recommendation:** Keep the testing pyramid steep — MockTerminal tests for widget logic (fast, deterministic), httptest fakes for adapter logic (medium speed), and tuistory + per-test daemon only for end-to-end flows (slow but necessary). Document this pyramid explicitly in the component README so future LLM authors don't over-invest in tuistory for simple widget changes.

---

## Finding 5: The Three-Lane Layout Rendered Correctly but Required Careful Separator Arithmetic

**Observation:** The Main / Detail / Evidence lane layout with box-drawing separators (`│`, `┬`, `┴`) renders correctly at 80×24, 120×40, and 60×20. The `buildLanedRow` helper computes lane widths (40% / 35% / 25% at ≥80 width, 45% / 35% / 20% at minimum width) and pads content to exactly fill the terminal without gaps or overflows.

**What held up:** The layout math is deterministic and tested via `cockpit_resize_test.go`. Selection state survives resizes because `UpdateSize` only changes dimensions and recomputes the `tooSmall` flag — it does not mutate `selectedIndex` or `agents`.

**What didn't:** Early versions of `renderReady` had subtle bugs where the separator count (2 columns for 3 lanes) was not consistently subtracted from `width` before computing lane widths, causing off-by-one errors at certain terminal sizes. The `padString` and `centerInWidth` helpers also had a bug where CJK characters caused misalignment because they used byte count instead of display width — this was fixed by using `tui.RuneWidth` throughout.

**Recommendation:** Extract the lane layout arithmetic into a dedicated `layout.go` file with its own unit tests. The `buildLanedRow`, `padString`, `centerInWidth`, and `truncateLabel` functions are currently inlined in `cockpit.go` and duplicated conceptually across render paths. A tested layout engine would make the three-lane model easier to extend (e.g., adding a fourth lane for orchestration).

---

## Finding 6: Async Boot with Loading State Was the Right Call — It Eliminated the "Frozen Terminal" Problem

**Observation:** The legacy `NewApp()` blocks on `LoadInitialShellState()` synchronously, leaving the terminal unresponsive for seconds if the API is slow. The new `NewCockpitScreen()` returns immediately in `CockpitPhaseLoading`, and a background `BootManager` goroutine transitions to `Ready` or `Error`. The loading state shows a spinner and the target URL; the error state names the URL and shows retry guidance.

**What held up:** The `BootManager` pattern (start in `Init()`, stop in cleanup, communicate via `phaseChanges` channel watched by `gotui.Watch`) integrates cleanly with go-tui's watcher system. The `t.Cleanup`-based fixture makes it easy to test boot transitions without leaking goroutines.

**What didn't:** The initial boot timeout was hardcoded to 5s, which is too long for tests. A `BootConfig.Timeout` field was added to allow sub-second timeouts in tests. The `events_subscriber.go` live-refresh component also needed its own debounce logic to avoid hammering the API on every SSE event — this was added late in M3 and could have been designed earlier.

**Recommendation:** Make all timeouts and debounce intervals configurable via constructor options with sensible defaults. Never hardcode timing values that affect both production and test paths. Consider adding a `BootConfig.Debounce` field for the live-refresh subscriber as well, unifying the async timing policy.

---

## Finding 7: The Evidence Drawer Pattern Works Well but Needs a More Structured Payload Model

**Observation:** The evidence drawer opens on the currently selected stream line and shows a typed raw payload (text reply, tool call, tool result, error, malformed event). The `buildEvidenceContent` method in `cockpit.go` uses string-prefix matching (`assistant:`, `tool:`, `result:`, `you:`, `⚠ `) to determine the row type and format the drawer content.

**What held up:** The drawer renders correctly for all three required row types (text reply, tool call, error) and closes on ESC with focus restoration. The `Drawer` widget's focus-trap and scroll behavior are well-tested.

**What didn't:** The string-prefix approach is brittle — it depends on the exact format of `streamLines` entries, which are built incrementally in `ApplyAskStreamUpdate`. If the prefix convention changes (e.g., adding a new row type), `buildEvidenceContent` must be updated in sync. There is no typed payload model for stream rows; they are just strings.

**Recommendation:** Introduce a `StreamRow` struct with `Type` (enum), `Content`, `RawPayload`, and `Metadata` fields. Replace `[]string` streamLines with `[]StreamRow` in `CockpitScreen`. This would make the evidence drawer's content builder type-safe and enable richer rendering (e.g., collapsible tool arguments, syntax-highlighted JSON) without string parsing.

---

## Finding 8: The `CockpitScreen` Monolith Grew to ~1500 Lines and Should Be Split

**Observation:** `cockpit.go` contains the `CockpitScreen` struct, all state methods, the render methods for four phases (too-small, loading, error, ready), the status footer builder, the detail pane row renderer, the stream row builder, the evidence content builder, and layout helpers. At ~1500 lines, it is the largest file in the TUI package.

**What held up:** The monolith is self-contained and easy to understand as a single file. All state mutations go through `CockpitScreen` methods, preserving single-writer ownership. The `sync.Mutex` pattern is consistent throughout.

**What didn't:** The file is too large for LLM authors to extend correctly. Adding a new phase (e.g., a "rooms" screen) or a new lane would require editing a file that already handles four render paths. The `renderReady` method alone is ~300 lines. The layout helpers (`buildLanedRow`, `padString`, `centerInWidth`, `truncateLabel`, `shortID`) are general-purpose utilities that don't belong in a screen-specific file.

**Recommendation:** Split `cockpit.go` into:
- `cockpit_screen.go` — struct, state methods, keymap, watchers (the "controller")
- `cockpit_render.go` — render methods for each phase (the "view")
- `cockpit_layout.go` — lane arithmetic, padding, truncation helpers (the "layout engine")
- `cockpit_evidence.go` — evidence drawer content building (the "evidence formatter")

This aligns with the architecture doc's stated goal of "single-source renders" and makes each file small enough for LLM authors to modify without understanding the entire cockpit.

---

## Finding 9: The `.gsx` Toolchain Was Not Needed for M2/M3 but Must Be Documented for Future Work

**Observation:** No new `.gsx` files were created in M2 or M3. The walking skeleton uses hand-written Go components (`CockpitScreen`, `BootManager`, `EventsSubscriber`) rather than `.gsx` templates. The only `.gsx` file in the TUI package is the legacy `shell.gsx`, and its generated `shell_gsx.go` was not modified.

**What held up:** The architecture doc's `.gsx` toolchain section (VAL-DOCS-010) is complete and accurate. The regeneration command (`tui generate ./internal/interfaces/tui/...`) works. The editable/forbidden globs are clearly documented.

**What didn't:** Because no `.gsx` files were edited, VAL-CROSS-004 (no `.gsx` / generated drift) was trivially satisfied. However, future M4+ work that adds new screens (e.g., Rooms, Orchestration) may need `.gsx` templates. The current team has no recent practice with the toolchain, so there is a risk of drift when it is first used.

**Recommendation:** Add a small `.gsx` demo component (e.g., `components/cmd/demo_screen.gsx`) in a follow-up mission to exercise the toolchain end-to-end. This would verify that the regeneration command, commit-in-same-commit rule, and generated-file headers all work as documented before a larger `.gsx` refactor is attempted.

---

## Finding 10: Live Refresh via `/api/events` Works but Lacks Granularity

**Observation:** The `EventsSubscriber` maintains a single SSE connection to `/api/events` with a topic filter for agent events. When an external `foxctl agent spawn` or `foxctl agent kill` occurs, the subscriber receives an event and triggers a debounced re-fetch of the agent inventory. The inventory updates within 5 seconds in practice.

**What held up:** The debounce pattern (1s delay, 5s max wait) prevents API hammering. The subscriber reconnects automatically on connection drop. The `t.Cleanup`-based fixture ensures no leaked SSE connections in tests.

**What didn't:** The subscriber re-fetches the entire agent list on every event, even if only one agent changed. There is no incremental update path. The `/api/events` endpoint provides event types but not the full agent payload, so the TUI cannot update a single row without a full `GET /api/agents` call.

**Recommendation:** Consider adding an `AgentEvent` payload to the SSE stream that includes the changed agent's full state. This would allow the TUI to update a single inventory row in O(1) time rather than re-fetching the entire list. Document this as a follow-up API change, not a TUI fix — the TUI is correctly consuming what the API provides.

---

## Summary

M2 delivered a solid, well-tested component library that is easy for LLM authors to extend. M3 proved the patterns against the live daemon with end-to-end flows. The main risks for future work are:

1. **Monolith growth** — `cockpit.go` needs splitting before adding new screens.
2. **Stringly-typed stream rows** — need a typed `StreamRow` model for evidence drawer safety.
3. **No `.gsx` practice** — the toolchain is documented but untested in this mission.
4. **Full-list re-fetch on events** — API granularity could improve live-refresh efficiency.

All mission boundaries were respected: no changes to `packages/gui-agent/`, `archive/`, `internal/interfaces/web/api/`, or `server.go`. `go.mod` still pins `github.com/grindlemire/go-tui v0.11.0`. The existing shell and smoke modes remain unchanged.
