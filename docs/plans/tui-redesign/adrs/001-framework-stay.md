# ADR 001: Stay on grindlemire/go-tui v0.11.0

| Field    | Value        |
|----------|--------------|
| Date     | 2026-04-18   |
| Status   | accepted     |

## Context

The foxctl TUI is built on [github.com/grindlemire/go-tui](https://github.com/grindlemire/go-tui) v0.11.0, pinned in `go.mod` at line 30. The framework provides a component model with `.gsx` templating, state management via `tui.State[T]`, focus/keymap routing, a `MockTerminal` for testing, and an event loop driven by `App.Run()`. The existing TUI shell (`internal/interfaces/tui/shell.gsx`, ~706 lines of `.gsx` source) and its generated artifact (`shell_gsx.go`, ~1629 lines) depend deeply on these APIs.

The audit ([audit-current-tui.md](../audit-current-tui.md)) documents the current codebase at ~7,800 lines of Go. The existing `.gsx` toolchain produces view structs that implement the go-tui `Component` interface (`BindApp`, `UnbindApp`, `GetRoot`, `GetWatchers`, `Render`, `UpdateProps`). The `Shell.Watchers()` method at `internal/interfaces/tui/shell.gsx:122` bridges runtime update channels to go-tui's `tui.Watch()` mechanism. The `Shell.Render()` method at `internal/interfaces/tui/shell_gsx.go:1555` produces the terminal layout. The three bounded runtimes (`ConsoleStreamPump` at `internal/interfaces/tui/console_stream_pump.go:63`, `ConsoleAskRuntime` at `internal/interfaces/tui/console_ask_runtime.go:109`, `ConsoleCancelRuntime` at `internal/interfaces/tui/console_cancel_runtime.go:85`) use `context.WithCancel`, `sync.Once`, and `sync.WaitGroup` — standard Go concurrency patterns that are framework-agnostic.

The go-tui framework has gaps for operator-cockpit needs (no built-in entity list, no drawer, no streaming viewer), but these can be filled with custom widgets built on the `Component` interface. The mission boundary in `mission.md` explicitly states: "Framework stays on `github.com/grindlemire/go-tui`. No framework swap in this mission."

## Decision

**Stay on `github.com/grindlemire/go-tui` v0.11.0.** No framework upgrade, no framework swap. The new cockpit screens build custom widgets on top of go-tui's `Component` interface, using the existing `.gsx` code generation toolchain for view templates and hand-written Go for state, adapters, and runtimes.

## Alternatives Considered

### bubbletea (Charm)

The most popular Go TUI framework. Rejected because: (1) the existing ~7,800 lines of TUI code are deeply integrated with go-tui's `Component`/`State`/`Watch` model — a migration would be a full rewrite, not a refactor; (2) go-tui's `.gsx` code generation provides a structured authoring model that bubbletea lacks (bubbletea uses free-form `tea.Model` structs); (3) the mission scope explicitly forbids a framework swap; (4) the existing test suite (12 test files, ~2,500 lines) depends on go-tui's `MockTerminal` which has no bubbletea equivalent.

### tcell/tview

A lower-level terminal library (tcell) with a widget toolkit (tview). Rejected because: tview's widget model is less composable than go-tui's `Component` interface, and tcell does not provide a testing harness comparable to `MockTerminal`. Also blocked by the mission boundary.

### Upgrade to a newer go-tui version

If a newer version of go-tui existed with relevant improvements. Rejected because: v0.11.0 is the current latest release, and the mission boundary prohibits upgrades regardless.

## Consequences

- **Positive:** The existing shell, smoke modes (`-smoke-agent`, `-smoke-console`), and ~2,500 lines of tests remain valid without modification. The `.gsx` toolchain continues to generate view structs. The `MockTerminal` testing infrastructure is available for new widgets.
- **Positive:** No migration risk. The coexist strategy (ADR 002) can reuse adapters, runtimes, and event parsing from the existing package.
- **Negative:** Custom widgets must be built from scratch — go-tui does not provide built-in entity lists, drawers, streaming viewers, or status badges. This is the primary M2 scope.
- **Negative:** go-tui's `.gsx` toolchain must be understood by LLM authors. The audit identified this as an undocumented hazard (audit section (h), pain point #5: the relationship between `shell.gsx` and `shell_gsx.go` was not documented). The architecture doc's [.gsx Toolchain](../architecture.md#gsx-toolchain) section now addresses this.
- **Risk:** If go-tui becomes unmaintained, future missions may need to revisit this decision. Acceptable because: the mission scope is a spike, not a long-term framework commitment, and the coexist strategy means the new cockpit code is isolated enough to migrate later if needed.

## Status

accepted
