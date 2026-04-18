# ADR 004: Typed Entity Model with State Reducer

| Field    | Value        |
|----------|--------------|
| Date     | 2026-04-18   |
| Status   | accepted     |

## Context

The current TUI state management has two interconnected problems:

**1. Ambient `ShellState` with no reducer boundary.**

`ShellState` at `internal/interfaces/tui/shell_state.go:8` is a mutable value type with methods like `ApplyConsoleStreamEvent()` at `internal/interfaces/tui/shell_state.go:55` and `AttachAskCorrelation()` at `internal/interfaces/tui/shell_state.go:74` that return new states. However, the `Shell` component also mutates state directly through `s.state.Update()` closures scattered across `shell.gsx` — e.g., `submitComposer()` at `internal/interfaces/tui/shell.gsx:356`, `updateComposer()` at line 338, and `backspaceComposer()` at line 345. There is no single reducer function; state mutations are spread across the component, making it impossible to reason about state transitions, reproduce bugs, or write deterministic golden tests.

**2. String-keyed transcript kinds.**

`TranscriptEntry.Kind` at `internal/interfaces/tui/models.go:75` is a `string`. The kind values are scattered across the codebase: `"pending"`, `"ask"`, `"reply"`, `"event"`, `"cmd"`, `"draft"`, `"status"`, `"error"`, `"tool"`, `"counts"`, `"next"`, `"brief"`, `"epic"`, `"plan"`, `"inflight"`, `"agent"`, `"console"`, `"connected"`, `"heartbeat"`. The `MapConsoleStreamEventToTranscriptEntry` function at `internal/interfaces/tui/event_stream.go:136` maps event types to kinds using string comparison. There is no typed enum, no exhaustive switch, and no compile-time guarantee that a new kind is handled in all mapping functions.

The audit ([audit-current-tui.md](../audit-current-tui.md) section (h), pain points #3 and #4) identifies both issues as authoring hazards. Adding a new transcript kind requires finding and updating every string comparison in the codebase.

## Decision

**Introduce a typed entities package (`internal/interfaces/tui/entities/`) with structured types and a single reducer function.**

The new cockpit uses `CockpitState` — an immutable snapshot containing typed entity slices (`Agent`, `AgentNode`, `Room`, `RoomMessage`, `EventRow`) and a typed `EntryKind` enum. All mutations go through:

```go
func Reduce(state CockpitState, event Event) CockpitState
```

This is a pure function: it accepts the current state and a typed event, returns a new state, and performs no IO or side effects. The go-tui `State[CockpitState]` is updated only by calling `state.Set(Reduce(state.Get(), event))` from watcher callbacks or key handlers. No closure may directly call `state.Update()` with arbitrary mutations.

The `EntryKind` enum replaces the 18+ string-keyed kinds:

```go
type EntryKind int

const (
    KindPending EntryKind = iota
    KindAsk
    KindReply
    KindEvent
    KindCmd
    KindDraft
    KindStatus
    KindError
    KindTool
    KindCounts
    KindNext
    KindBrief
    KindEpic
    KindInflight
    KindAgent
    KindConsole
    KindConnected
    KindHeartbeat
)
```

A helper function `ParseEntryKind(s string) EntryKind` maps legacy strings to the typed enum for backward compatibility with existing event parsing at `internal/interfaces/tui/event_stream.go:136`.

The existing `ShellState` is preserved for the legacy shell (coexist decision, ADR 002) but is not used by the new cockpit screens.

## Alternatives Considered

### Defend `ShellState` with a mutation-logging wrapper

Keep `ShellState` but add a `Mutate(fn func(*ShellState))` method that logs all mutations. Rejected because: this addresses observability but not correctness — scattered closures can still produce inconsistent intermediate states. A reducer guarantees that every state transition is atomic, reproducible, and testable.

### String constants (`const KindAsk = "ask"`)

Centralize the string values but keep the type as `string`. Rejected because: string constants do not enable exhaustive switch checking. Go's `go vet` catches missing cases in `switch` statements over typed ints when `default` is omitted, but not for string switches. The compiler enforcement is the primary benefit of the typed enum.

### Immutable event-sourced model

Store the full event log and recompute state from scratch on every render. Rejected because: the transcript can grow unbounded (capped at `transcriptLimit` per `capTranscriptEntries()` at `internal/interfaces/tui/shell_state.go:145`), and recomputing from scratch would be O(n) per render. The reducer pattern is O(1) per event while still providing reproducibility.

## Consequences

- **Positive:** Every state transition is atomic and reproducible. The reducer can be unit-tested exhaustively: given any `CockpitState` and any `Event`, the resulting state is deterministic and testable.
- **Positive:** Time-travel debugging becomes possible by recording the event stream and replaying it.
- **Positive:** Adding a new `EntryKind` triggers compile-time errors in any incomplete switch statement, preventing missed cases.
- **Positive:** The `ParseEntryKind` helper provides a clean migration path from the existing string-based event parsing at `internal/interfaces/tui/event_stream.go:136`.
- **Negative:** The legacy shell keeps `ShellState` and its scattered mutations. There are two state models in the codebase during the coexist period. Mitigated by: the legacy shell is frozen (ADR 002) — no new features are added to it.
- **Negative:** The reducer pattern requires discipline: every mutation must go through `Reduce()`. A developer could bypass the reducer by calling `state.Set()` directly. Mitigated by: code review and the `golangci-lint` rule that widget implementation files must not contain `state.Set` outside of the reducer call.

## Status

accepted
