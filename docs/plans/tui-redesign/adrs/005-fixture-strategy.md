# ADR 005: Two-Tier Fixture Strategy for Testing

| Field    | Value        |
|----------|--------------|
| Date     | 2026-04-18   |
| Status   | accepted     |

## Context

The existing TUI test suite uses `httptest.Server`-based fakes for adapter-level unit tests (e.g., `api_client_test.go`, `agent_adapter_test.go`, `console_adapter_test.go` at `internal/interfaces/tui/`). These tests are fast (milliseconds) and cover adapter serialization, error handling, and edge cases without requiring a live daemon.

However, the current tests do **not** cover end-to-end TUI flows. The smoke modes (`smoke_agent.go` at `internal/interfaces/tui/smoke_agent.go`, `smoke_console.go` at `internal/interfaces/tui/smoke_console.go`) validate ask/stream/cancel lifecycles against a live daemon, but they are non-interactive — they do not exercise the terminal UI, widget rendering, keyboard input, or screen transitions.

The redesign introduces three levels of testing that need different fixture strategies:

1. **Unit tests** (M2 widgets) — `MockTerminal`-based tests for isolated widget render and input handling. No HTTP, no daemon.
2. **Integration tests** (adapters, runtimes) — `httptest.Server` fakes for adapter serialization and runtime lifecycle. Fast, no daemon.
3. **End-to-end flow tests** (M3 walking skeleton) — tuistory-driven TUI interaction against a live daemon with real agent state.

The M3 walking skeleton needs agents seeded deterministically (known IDs, roles, status), a reachable API, and a clean teardown. The mission boundary prohibits touching port 8090 (the user's running daemon). The per-test daemon fixture must use OS-chosen ports (`-p 0`).

The synchronous boot at `internal/interfaces/tui/live_state.go:12` means the existing `LoadInitialShellState()` function blocks until HTTP calls complete. For testing async boot (loading → ready/error transitions), the fixture needs to control daemon availability timing.

## Decision

**Use a two-tier fixture strategy:**

### Tier 1: In-process fake API (`httptest.Server`)

For adapter-level unit tests and runtime lifecycle tests. Each test creates an `httptest.Server` that returns deterministic responses without booting the full daemon. This is the fast path (milliseconds per test) and covers:

- Adapter serialization (request/response mapping)
- Error handling (network errors, malformed JSON, non-2xx status codes)
- Runtime lifecycle (`Enqueue`, `Updates`, `Stop`, `Close`, context cancellation)
- SSE parsing (malformed frames, unknown event types)

This tier already exists in the codebase. No changes needed.

### Tier 2: Per-test isolated daemon

For integration and end-to-end TUI flow tests. An exported Go function:

```go
func BootDaemon(ctx context.Context, t *testing.TB, opts SeedOpts) *DaemonFixture
```

This function:

1. Creates a temp `FOXCTL_STORAGE_ROOT` directory.
2. Boots `foxctl web serve -p 0` as a subprocess.
3. Parses the OS-chosen port from stderr.
4. Seeds N agents deterministically (returns IDs to the caller).
5. Registers `t.Cleanup` for teardown (stops daemon, removes temp dir).
6. Handles `t.Fatal` paths — cleanup still runs.

The fixture is used by all M3 end-to-end tests. It ensures:

- No port collision with 8090 (always `-p 0`).
- No leaked processes (post-test `pgrep -f 'foxctl web serve'` yields nothing for the test's PID tree).
- No leaked temp directories (removed on cleanup).
- Deterministic agent state (IDs, roles, status are known to the test).

`MockTerminal`-based widget tests use neither fixture — they test isolated widget render and input handling against typed state, with no HTTP at all.

## Alternatives Considered

### Daemon-only testing

Run all tests against the live daemon. Rejected because: booting the daemon takes ~1–2 seconds per test, making the unit test suite unacceptably slow for TDD cycles. With ~50+ adapter and runtime tests, this adds minutes to every `go test` run.

### Record/replay fixtures

Capture HTTP responses from the daemon and replay them in tests. Rejected because: (1) the daemon API is not stable enough for recorded fixtures to remain valid across development iterations; (2) record/replay does not exercise the full SSE lifecycle (connection, streaming, cancellation) — the connection management and backpressure behavior differ between a real SSE stream and a replayed HTTP response; (3) maintaining recorded fixtures adds maintenance burden that a per-test daemon avoids.

### Shared daemon for all tests

Boot one daemon at test-suite start and share it across all tests. Rejected because: (1) shared mutable state between tests leads to non-deterministic failures; (2) the mission boundary prohibits using port 8090, and a shared daemon on an OS-chosen port requires careful coordination; (3) test isolation is compromised — one test's agent spawn/delete affects another test's assertions.

## Consequences

- **Positive:** Unit tests remain fast (milliseconds) — no daemon needed for adapter or widget tests.
- **Positive:** End-to-end tests exercise the real daemon with real agent state, catching integration bugs that fake servers cannot.
- **Positive:** The fixture's `t.Cleanup` pattern ensures no leaked processes or temp directories, even on `t.Fatal`.
- **Positive:** Deterministic seeding means tests are reproducible — the same agent IDs, roles, and status appear in every run.
- **Negative:** End-to-end tests are slower (~1–2 seconds per test for daemon boot). Mitigated by: the two-tier strategy keeps unit tests fast; only flow-level tests pay the daemon cost.
- **Negative:** The fixture requires `foxctl web serve` to be built and available in `bin/`. Tests that use the fixture must be gated behind a build tag or condition (e.g., `-tags=integration`) if CI needs to run unit tests without building the full binary.

## Status

accepted
