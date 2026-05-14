# ADR 002: Coexist — New Cockpit Alongside Legacy Shell

| Field    | Value        |
|----------|--------------|
| Date     | 2026-04-18   |
| Status   | accepted     |

## Context

The current foxctl TUI shell lives in `internal/interfaces/tui/shell.gsx` (~706 lines of `.gsx` source, generating ~1629 lines in `shell_gsx.go`). It implements a four-region layout (TopBar, Transcript, Composer, Right Rail) oriented around a single companion agent or console session. The shell's entry point is `Run(ctx, opts)` at `internal/interfaces/tui/app.go:11`, which calls `NewApp()` at `internal/interfaces/tui/app.go:34` — a synchronous boot that blocks the UI thread via `LoadInitialShellState()` at `internal/interfaces/tui/live_state.go:12`.

The shell is wired through a chain of four constructors: `NewShell()` → `NewShellWithStream()` → `NewShellWithRuntime()` → `NewShellWithRuntimes()`, defined at `internal/interfaces/tui/shell.gsx:33–69`. Each constructor adds channel parameters for runtimes (stream updates, ask updates, cancel updates). The `newShellRuntime()` function at `internal/interfaces/tui/app.go:54` assembles all three bounded runtimes and passes their channels to the shell.

The redesign needs a three-lane layout (Main/Detail/Evidence per DESIGN.md §2) that shows agent inventory as the default screen — fundamentally different from the existing companion-oriented transcript view. The audit ([audit-current-tui.md](../../../archive/plans/audit-current-tui.md) section (e)) identifies that the default state at `internal/interfaces/tui/shell_state.go:28–82` contains static marketing content ("Codex", "gpt-5.4", "READY"), which violates DESIGN.md's "Runtime First" and "Honest Surfaces" principles.

The existing shell has comprehensive test coverage (~2,500 lines across 12 test files) and deterministic smoke modes (`smoke_agent.go`, `smoke_console.go`) used by CI. The coexist-vs-refactor question is: should the new three-lane cockpit replace the existing shell, or live alongside it?

## Decision

**Coexist.** The new cockpit screens live alongside the existing shell as a separate entry point on `cmd/foxctl_tui`, reachable via a documented flag or subcommand (e.g., `-screen agents`). The existing shell and its smoke modes remain unchanged. Shared code (adapters at `api_client.go`, `agent_adapter.go`, `console_adapter.go`; event parsing at `event_stream.go`; bounded runtimes) is reused via direct import from the same `internal/interfaces/tui/` package.

When the new cockpit reaches feature parity for the companion flow, a follow-up mission can deprecate the old shell.

## Alternatives Considered

### Refactor-in-place

Modify the existing `Shell` component to support a three-lane layout, agent inventory, and rooms. Rejected because: (1) the `.gsx` source at `internal/interfaces/tui/shell.gsx` is tightly coupled to the four-region layout and cannot be incrementally refactored without a full rewrite of the 706-line `.gsx` template; (2) the existing smoke modes (`-smoke-agent`, `-smoke-console`) would break during the transition, blocking CI; (3) git bisect becomes unreliable during a large in-place refactor — there would be no working intermediate state; (4) the synchronous boot at `internal/interfaces/tui/live_state.go:12` would need to be replaced concurrently, compounding risk.

### Big-bang rewrite

Delete the existing shell and start fresh. Rejected because: this loses the working companion flow, the tested adapters, and the smoke modes all at once. No rollback path.

## Consequences

- **Positive:** Zero regression risk. The existing shell, smoke modes, and all 12 test files continue to pass unchanged. CI is not disrupted.
- **Positive:** The new cockpit can reuse adapters (`AgentAdapter` at `internal/interfaces/tui/agent_adapter.go:56`, `ConsoleAdapter` at `internal/interfaces/tui/console_adapter.go:95`) and the SSE parser (`ParseConsoleEventStream` at `internal/interfaces/tui/event_stream.go:73`) without modification.
- **Positive:** A/B comparison is possible — the team can run both shells during development and validate the new layout against the old one.
- **Negative:** Temporary code duplication. The new cockpit introduces its own state model (`CockpitState`) while the legacy shell keeps `ShellState` at `internal/interfaces/tui/shell_state.go:8`. The duplication persists until the legacy shell is deprecated.
- **Negative:** The `cmd/foxctl_tui` binary gains a second entry path, requiring clear `--help` documentation and potentially a default-screen decision.
- **Risk:** If the coexist period stretches too long, maintenance burden grows. Mitigated by: the architecture doc's [Reconciliation](../architecture.md#reconciliation) section defines clear deprecation criteria (feature parity for companion flow).

## Status

accepted
