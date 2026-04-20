---
name: tui-engineer-worker
description: Implements M2 (component library seed) and M3 (walking skeleton) for the foxctl TUI operator cockpit. TDD-first; MockTerminal unit tests then tuistory end-to-end.
---

# TUI Engineer Worker

NOTE: Startup and cleanup are handled by `worker-base`. This skill defines the WORK PROCEDURE.

## When to Use This Skill

Use this worker for all milestone `components` and milestone `skeleton`
features. These are code-bearing features under `internal/interfaces/tui/`
and `cmd/foxctl_tui/`.

Never use this worker for M1 docs features. Those go to `tui-docs-worker`.

## Required Skills

- **`tuistory`** (REQUIRED for all features that demand snapshot evidence) —
  Factory skill that drives the TUI binary via PTY, captures frames, and
  asserts on content. Invoke via the Skill tool. Use `tuistory` for:
  - Widget-level snapshots in M2 (VAL-CMP-004..009, -012).
  - End-to-end flows in M3 (VAL-SKEL-001..012, -015..018).
  - Cross-area verification (VAL-CROSS-001, -008).

For every `tuistory` invocation, note in the handoff which snapshot files it
produced so the user-testing validator can re-run the same flows.

## Work Procedure

### 1. Orient yourself

1. Read `missionDir/mission.md`, `missionDir/validation-contract.md`, and
   `missionDir/AGENTS.md`.
2. Read `.factory/library/architecture.md`,
   `.factory/library/user-testing.md`, and
   `.factory/library/environment.md`.
3. Read `AGENTS.md` and `DESIGN.md` at the repo root.
4. Read the M1 docs under `docs/plans/tui-redesign/` (especially
   `architecture.md`, `information-architecture.md`, `component-spec.md`,
   `integration-map.md`). Your code implements these specs.
5. Read the feature description carefully. It lists the assertion IDs you are
   responsible for completing (in the `fulfills` field).

### 2. Test-Driven Development is mandatory

For every feature in M2 or M3:

1. **Red first.** Write failing tests that encode the assertion's Evidence
   requirements. Commit (or at minimum apply) these tests before any
   implementation code. Run them to confirm they fail.
2. **Green second.** Implement just enough to make the tests pass.
3. **Refactor.** Only after tests are green.

MockTerminal-based unit tests come first. `tuistory` snapshots come after
unit coverage is solid. Do **not** skip unit tests and rely on tuistory
alone.

### 3. Respect the engineering principles

From `AGENTS.md` and this mission's `architecture.md`:

- **Single-writer state ownership.** Each piece of mutable state has one
  owner goroutine.
- **Bounded queues.** All async queues are bounded with explicit
  backpressure policy.
- **Context threading.** Every long-lived operation accepts
  `context.Context`. No goroutine survives `Stop()` longer than 100ms.
- **Leak-free.** Every runtime test checks
  `goleak.VerifyNone` or `runtime.NumGoroutine()` delta.
- **Snapshot reads.** Hot render paths use `atomic.Value`/`atomic.Pointer`.
- **Determinism.** No `time.Now()`, `rand.*`, or `os.Getenv` in pure render
  or state-reducer functions. Inject deps instead.

VAL-CROSS-006 asserts all of the above via grep on M2 widget files. Violations
fail scrutiny review.

### 4. Widget implementation checklist (M2)

For each widget feature (EntityList, DetailPane, Tabs, Drawer, StreamViewer,
and friends):

1. Confirm the contract in `docs/plans/tui-redesign/component-spec.md` matches
   what the assertion demands. If the spec is silent on a detail the
   assertion requires, stop and return to the orchestrator.
2. Write MockTerminal tests covering every documented prop, state, and
   interaction. Document wrap-around / empty / loading / error behavior.
3. Implement the widget. No raw color literals — reference theme tokens
   only. No ambient state; single render function.
4. Write tuistory snapshot tests for the variants named in the assertion.
5. Confirm `go test -race -count=1 ./internal/interfaces/tui/...` passes.
6. Confirm `golangci-lint run` on the touched package passes.

### 5. Walking-skeleton implementation checklist (M3)

For each M3 feature:

1. Confirm the per-test-daemon fixture exists (feature `skel-fixture` lands
   first and is a precondition for all other M3 features).
2. Write tuistory flow tests using the fixture. Each test:
   - Boots an isolated `foxctl web serve -p 0` with temp
     `FOXCTL_STORAGE_ROOT`.
   - Seeds exactly the agents/data the flow needs.
   - Registers `t.Cleanup` for teardown.
3. Implement the UI glue consuming the typed APIs from M2.
4. Exercise every branch the assertion demands — resize, cancel, malformed
   SSE, double-submit, min terminal size, etc.
5. Confirm `go test -race -count=1 ./internal/interfaces/tui/...` passes.
6. For changes to `.gsx` files: re-run the documented generator (per
   `architecture.md` .gsx toolchain section) and commit the regenerated
   `*_gsx.go` in the same commit (VAL-CROSS-004).

### 6. Mission-boundary checks before handoff

Before writing the handoff, confirm:

- `go.mod` still has exactly `github.com/grindlemire/go-tui v0.11.0`.
- No files under `archive/`, `packages/gui-agent/`, or
  `internal/interfaces/web/api/` were modified.
- No new daemon routes or handlers were added.
- `git status --porcelain` is clean relative to your feature's intended
  scope (no stray edits in unrelated files).

### 7. Handoff

The handoff JSON must include:

- Every test file added with named cases.
- Every `go test` command run, its exit code, and a one-line observation.
- Every `tuistory` flow exercised with a pointer to the snapshot file(s)
  produced.
- Every assertion in your `fulfills` mapped to the specific test /
  snapshot / code that satisfies it (in `salientSummary` or
  `whatWasImplemented`).

## Example Handoff

```json
{
  "salientSummary": "Implemented runtime.Bounded[Req, Upd] satisfying VAL-CMP-001 and VAL-CMP-002. Generic bounded runtime at internal/interfaces/tui/runtime/bounded.go replaces the duplicated scaffolding; existing runtimes (console_stream_pump, console_ask_runtime, console_cancel_runtime) now delegate to it without behavior change. Added 9 test cases covering bounded buffer, Stop() safety, Enqueue on stopped, Updates() close-once, ctx cancellation, goroutine leak check, and 8×concurrent enqueue + concurrent stop. All green under -race -count=50.",
  "whatWasImplemented": "internal/interfaces/tui/runtime/bounded.go (generic Bounded[Req, Upd] with Enqueue/Updates/Stop/Close; ErrStopped sentinel); internal/interfaces/tui/runtime/bounded_test.go (9 table-driven test cases + goleak assertion); delegation changes in console_stream_pump.go, console_ask_runtime.go, console_cancel_runtime.go (no behavior change; existing tests unchanged).",
  "whatWasLeftUndone": "",
  "verification": {
    "commandsRun": [
      { "command": "go test -race -count=50 ./internal/interfaces/tui/runtime/...", "exitCode": 0, "observation": "All 9 cases pass; -race clean; leak check passes." },
      { "command": "go test -race -count=1 ./internal/interfaces/tui/...", "exitCode": 0, "observation": "All existing runtime tests (console_stream_pump, console_ask_runtime, console_cancel_runtime) still pass unchanged — VAL-CMP-002 satisfied." },
      { "command": "git diff --stat -- internal/interfaces/tui/console_stream_pump_test.go internal/interfaces/tui/console_ask_runtime_test.go internal/interfaces/tui/console_cancel_runtime_test.go", "exitCode": 0, "observation": "Zero line changes in those three test files — expected outcomes unchanged." },
      { "command": "golangci-lint run ./internal/interfaces/tui/runtime/...", "exitCode": 0, "observation": "Clean." }
    ],
    "interactiveChecks": []
  },
  "tests": {
    "added": [
      {
        "file": "internal/interfaces/tui/runtime/bounded_test.go",
        "cases": [
          { "name": "TestBounded_ConfigurableBufferSize", "verifies": "VAL-CMP-001 (i) — buffer size from constructor" },
          { "name": "TestBounded_StopIsIdempotent", "verifies": "VAL-CMP-001 (ii) — double Stop() safe, completes <100ms" },
          { "name": "TestBounded_EnqueueOnStoppedReturnsErrStopped", "verifies": "VAL-CMP-001 (iii)" },
          { "name": "TestBounded_UpdatesClosedOnce", "verifies": "VAL-CMP-001 (iv)" },
          { "name": "TestBounded_ContextCancelDuringEnqueue", "verifies": "VAL-CMP-001 (v)" },
          { "name": "TestBounded_NoGoroutineLeak", "verifies": "VAL-CMP-001 (vi) — goleak.VerifyNone" },
          { "name": "TestBounded_ConcurrentEnqueueWithStop", "verifies": "VAL-CMP-001 (vii) — 8 enqueuers + 1 stopper, -count=50 deterministic" },
          { "name": "TestBounded_EnqueueBlocksWhenFullUntilDrained", "verifies": "bounded backpressure invariant" },
          { "name": "TestBounded_CloseAfterStopIsNoop", "verifies": "Close() idempotency" }
        ]
      }
    ]
  },
  "discoveredIssues": []
}
```

## When to Return to Orchestrator

Standard return conditions apply. Engineering-specific returns:

- **M1 docs contradict the assertion.** If `component-spec.md` says the
  widget should behave one way but the contract asserts another, stop and
  return — do not pick a side.
- **Missing M2 primitive.** If your M3 feature needs a widget or runtime
  that a prior M2 feature should have delivered but did not, return so
  the orchestrator can fix M2 first.
- **Framework limitation.** If `grindlemire/go-tui` fundamentally cannot
  satisfy an assertion (e.g., it does not expose the PTY event needed),
  return with a concrete reference to the framework source. Do **not**
  upgrade the framework or add a dependency.
- **Pre-existing failing tests unrelated to your feature.** Document and
  return. Do not fix.
- **Daemon behavior mismatch.** If the live daemon returns a payload that
  does not match `integration-map.md`, return with the captured response.
  Do not modify daemon code.
