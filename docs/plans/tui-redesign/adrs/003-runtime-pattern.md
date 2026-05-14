# ADR 003: Shared Generic Bounded-Runtime (`Bounded[Req, Upd]`)

| Field    | Value        |
|----------|--------------|
| Date     | 2026-04-18   |
| Status   | accepted     |

## Context

The current TUI has three bounded runtime goroutines that share ~80% structural similarity:

1. **`ConsoleStreamPump`** at `internal/interfaces/tui/console_stream_pump.go:63` — source-driven runtime that reads an SSE stream. Its `run()` loop is at `internal/interfaces/tui/console_stream_pump.go:116`. Uses `context.WithCancel`, `sync.Once` for stop, `sync.WaitGroup` for goroutine lifecycle, and an `updates` channel.

2. **`ConsoleAskRuntime`** at `internal/interfaces/tui/console_ask_runtime.go:109` — request-driven runtime that submits enqueued ask requests. Its `run()` loop is at `internal/interfaces/tui/console_ask_runtime.go:200`. Uses `context.WithCancel`, `sync.Once`, `sync.WaitGroup`, a `requests` channel (buffer size 16 at line 14), and an `updates` channel (buffer size 16 at line 15). Exposes `Enqueue(ctx, req)`, `Updates()`, `Stop()`, `Close()`.

3. **`ConsoleCancelRuntime`** at `internal/interfaces/tui/console_cancel_runtime.go:85` — request-driven runtime that submits enqueued cancel requests. Its `run()` loop is at `internal/interfaces/tui/console_cancel_runtime.go:173`. Uses the identical pattern: `context.WithCancel`, `sync.Once`, `sync.WaitGroup`, `requests`/`updates` channels (buffer sizes 16 at lines 9–10). Exposes `Enqueue(ctx, req)`, `Updates()`, `Stop()`, `Close()`.

The audit ([audit-current-tui.md](../../../archive/plans/audit-current-tui.md) section (h), pain point #2) identifies these three runtimes as near-identical boilerplate. Each has its own `sendUpdate()` helper, its own buffer-size constants, its own `Enqueue()`/`Updates()`/`Stop()`/`Close()` surface. Adding a fourth runtime (e.g., an events watcher or rooms subscription) would require copying ~150 lines of boilerplate.

The mission's [AGENTS.md](../../../../AGENTS.md) engineering principles require "bounded channels with explicit backpressure policy" and "leak-free shutdown verified by `goleak` or `runtime.NumGoroutine()` delta." Each existing runtime individually satisfies these, but there is no shared enforcement.

## Decision

**Introduce `runtime.Bounded[Req, Upd]`** — a generic bounded-queue runtime that encapsulates the goroutine lifecycle pattern shared by all three current runtimes. The API surface is:

```go
type Bounded[Req, Upd any] struct { /* ... */ }

func New[Req, Upd](handle func(ctx context.Context, req Req) Upd, opts ...Option) *Bounded[Req, Upd]
func (b *Bounded[Req, Upd]) Enqueue(ctx context.Context, req Req) error
func (b *Bounded[Req, Upd]) Updates() <-chan Upd
func (b *Bounded[Req, Upd]) Stop()
func (b *Bounded[Req, Upd]) Close()
```

Constructor options include: buffer size, backpressure policy (block, drop-oldest, error), and a `StartSource` variant for source-driven runtimes like `ConsoleStreamPump`.

The existing runtimes delegate to `Bounded` internally while preserving their current public API:

- **`ConsoleStreamPump`** uses `Bounded`'s `StartSource` mode (no `Enqueue` — reads from SSE connection).
- **`ConsoleAskRuntime`** wraps `Bounded[ConsoleAskRequest, ConsoleAskUpdate]` with a `handle` function that calls `SubmitAsk`.
- **`ConsoleCancelRuntime`** wraps `Bounded[ConsoleCancelRequest, ConsoleCancelUpdate]` with a `handle` function that calls `SubmitCancel`.

All three retain their existing `Enqueue()`, `Updates()`, `Stop()`, `Close()` methods as thin wrappers. Existing tests pass unchanged (VAL-CMP-002).

## Alternatives Considered

### Keep three independent runtimes

Preserve the existing code as-is and copy the pattern for new runtimes. Rejected because: copying ~150 lines of boilerplate per runtime is the exact pain point identified in the audit. It makes the testing burden quadratic (each new runtime needs its own lifecycle tests for `Stop()` safety, concurrent `Enqueue`, context cancellation, and goroutine-leak checks).

### Interface-based runtime without generics

Define a `Runtime` interface with `Enqueue(any)` and `Updates() <-chan any`, losing type safety. Rejected because: Go generics (available since 1.18, required by the project's Go 1.25+ minimum per [AGENTS.md](../../../../AGENTS.md)) provide compile-time type safety for the request/update channel pair without runtime cost. Using `any` would reintroduce the type-assertion fragility that the typed-enum decision (ADR 004) aims to eliminate.

### Channel-based coordination without a struct

Use bare channels and `sync.WaitGroup` without a `Bounded` struct. Rejected because: this is what the current code does, and it does not enforce the bounded-channel invariant at the type level. A struct with a required buffer-size constructor parameter makes the bound explicit and testable.

## Consequences

- **Positive:** Adding a new runtime (events watcher, rooms subscription) goes from ~150 lines of boilerplate to ~20 lines of handler function plus a `Bounded` constructor call.
- **Positive:** Lifecycle correctness (safe double-`Stop()`, closed-channel drain, context cancellation) is tested once in `Bounded` rather than duplicated per runtime.
- **Positive:** The bounded-channel invariant is enforced at construction time. The backpressure policy (block, drop-oldest, error) is explicit per-runtime rather than implicit.
- **Positive:** The existing runtimes' public APIs are preserved. Their existing tests pass unchanged — the delegation is an internal refactoring only.
- **Negative:** One more abstraction layer. Runtime-specific behavior (e.g., the stream pump's source-driven mode vs. the ask runtime's request-driven mode) requires `Bounded` to support both modes, adding API surface.
- **Risk:** If the generic does not handle an edge case (e.g., the stream pump's `onEvent` callback model), it may need extensions. Mitigated by: the `StartSource` option is designed for exactly this case, and the existing `ConsoleStreamPump` serves as the reference implementation.

## Status

accepted
