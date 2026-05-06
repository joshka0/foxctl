# Durable Execution Recovery

Status: Proposed
Owner: Solo maintainer
Last Updated: 2026-05-06

## Goal

Make foxctl v2 recover useful work after process crashes or restarts without
pretending that non-idempotent runner stages are replay-safe.

The first implementation slice is orchestration recovery:

1. Treat `v2_orchestration_cards` as the authoritative durable retry queue.
2. Add card-first startup recovery for projected `StateRunning` cards.
3. Keep runner checkpointing out of scope until model, tool, event, and turn
   side effects have explicit idempotency or journaling.

This supersedes the checkpoint-first direction in
`/Users/joshka/.factory/specs/2026-05-05-durable-execution-plan.md`.

## Key Decision

Layer 2 is the safe first slice. Layer 1 is not implementation-ready.

The existing orchestration card projection already stores durable retry state:

- `state`
- `lane`
- `retry_due_at`
- `attempt`
- `policy_status`
- `last_outcome`

`BoardCandidateSource.ListCandidates` already dispatches due `RetryQueued`
cards before `Todo` cards. Adding a separate `v2_orchestration_retry_queue`
table would create split-brain state between the card projection and a second
queue.

Stage-level turn checkpoints are deferred because `Pipeline.RunTurn` stages
already perform durable or external side effects before a stage-complete
checkpoint could be saved. A checkpoint written after a side-effecting stage is
not atomic with the event append, turn persistence, model call, or tool
execution it is meant to protect.

## Current System Facts

Runner path:

- `RunService.Run` requires `run_id`, but not `turn_id`.
- `stageInitContext` can generate `turn_id`, `request_id`,
  `correlation_id`, and `causation_id`.
- `Pipeline.appendEvent` maintains `streamVersion` and `sequence` in
  in-memory `executionState`.
- The Turso-backed v2 event store enforces monotonic stream version and
  sequence. It is append-only, not an idempotent event sink.
- `stageModelCallLLMChat` can call the model, append tool events, execute
  tools, update model messages, and append iteration records inside one stage.
- `stagePersistTurn` calls `TurnRecorder.SaveTurn`, then appends
  `turn.recorded`.
- `stageEmitEvents` appends `run.completed`.

Orchestration path:

- `Component.Run` already starts with an immediate cycle before the ticker.
- `runCycle` currently calls `Reconciler.Reconcile`, then preflight, then
  scheduler dispatch.
- Current overseer wiring constructs `NewScheduler` without `RetryQueue` in
  both Go runtime and explicit Jido modes.
- The Go subprocess reconciler enumerates child worker records first, then
  checks projected cards.
- The Jido reconciler enumerates Jido children first, then checks projected
  cards.
- Neither reconciler starts from all projected `StateRunning` cards, so a card
  whose child/process is missing can remain orphaned.
- Go runtime is the default orchestration backend. Jido remains available only
  when `FOXCTL_V2_ORCHESTRATION_RUNTIME_BACKEND=jido` is set.

Storage naming note:

- Active v2 SQL adapters live under `internal/v2/adapters/turso/*`.
- The previous `internal/v2/adapters/libsql/*` path still exists on `main` at
  `938733293b81c9be8787e15300661cf587baa8af` for history. Do not reintroduce
  that package path.
- The `libsql://` URL scheme remains the Turso remote URL scheme.

Package placement must stay inside the v2 agent/runtime/orchestration lane. See
[Package Topology](../../architecture/package-topology.md).

## Non-Goals For The First Slice

Do not implement these in the first slice:

1. A new `v2_orchestration_retry_queue` table.
2. A durable wrapper around the in-memory `RetryQueue`.
3. Runner checkpoint resume.
4. Gob-encoded private `executionState`.
5. Exactly-once model or tool execution.
6. Multi-workspace primary-key migration for orchestration cards.
7. Event-store idempotent append semantics.
8. Atomic event-append-plus-checkpoint writes.

## Architecture

### Durable Retry Queue

`v2_orchestration_cards` is the durable retry queue.

The scheduler should continue to receive candidates from
`BoardCandidateSource`. Due retry cards are already ordered before fresh `Todo`
cards. The in-memory `RetryQueue` can remain for unit tests or ephemeral
experiments, but production recovery should not depend on it.

Dispatch failure handling should project retry state through the existing
orchestration event and card-projection path. The card projection is the source
of truth for whether a card is retryable, due, blocked, or still running.

### Startup Recovery

Startup recovery must be card-first:

1. Query projected cards where `state = Running`.
2. For each card, inspect the backend runtime by stable card metadata such as
   `agent_id`, `run_id`, workspace, issue id, worker id, or child ref.
3. If the runtime proves the child is still running, leave the card alone.
4. If the runtime proves the child is terminal, emit the same canonical
   completed or failed event used by the existing reconciler path.
5. If the runtime backend is healthy but the specific child is missing, emit a
   canonical failed event that moves the card to `RetryQueued` when retry policy
   allows it.
6. If the runtime backend is unavailable, do not mutate cards and do not
   dispatch fresh work in that cycle.

The current "call `Reconcile` once before the ticker" approach is insufficient.
`Reconcile` already runs in the immediate cycle, and both existing reconcilers
enumerate known children first. Missing children are exactly the case that a
card-first recovery pass must handle.

### Runtime Evidence Model

Use a narrow runtime-inspection abstraction rather than forcing Jido and Go
subprocess into the same state reader:

```go
type StartupRecovery interface {
	RecoverOrphanedRuns(ctx context.Context) error
}

type RunningCardReader interface {
	ListRunningCards(ctx context.Context, req ListRunningCardsRequest) ([]orchestration.Card, error)
}

type RunningCardInspector interface {
	InspectRunningCard(ctx context.Context, card orchestration.Card) (RunningCardObservation, error)
}

type RunningCardObservation struct {
	Status       RunningCardStatus
	Reason       string
	Retryable    bool
	Completed    bool
	TerminalCard orchestration.Card
}
```

The concrete names can change during implementation. The important boundary is
that card enumeration is shared, while backend evidence is backend-specific.

Go subprocess inspector:

- Use `coreworker.StateReader`.
- Treat terminal worker records as completed, failed, or cancelled.
- Treat missing worker records as orphaned only after a startup grace period.
- Treat live/running worker records as still running.

Jido inspector:

- Use the Jido client, not `coreworker.StateReader`.
- Distinguish "Jido parent/runtime unavailable" from "specific child missing."
- If the Jido socket or parent is unreachable, return a recovery error and
  skip scheduler dispatch.
- If the parent/runtime is reachable and the child is missing, recover the card
  as failed/retryable when retry policy permits.

### Component Lifecycle

`Component.Run` should run startup recovery before scheduler dispatch.

Recommended behavior:

```text
startup recovery
if recovery failed:
  OnError(err)
  skip scheduler dispatch for this cycle
else:
  normal reconcile/preflight/scheduler cycle
```

It is acceptable for normal periodic reconcile to continue running in each
cycle. Startup recovery is a separate card-first pass whose error should prevent
new dispatch for the cycle, because dispatching while the runtime state is
unknown can compound orphaned work.

## Recovery Semantics

### Runtime Unavailable

Do not mutate running cards.

Return an error and skip scheduler dispatch for the cycle. This prevents a
temporary Jido socket outage or worker-store outage from marking all running
work as failed.

### Specific Child Missing

If the backend runtime is healthy and the card's child/worker is missing, append
a canonical `run.failed` orchestration event with:

- `state = RetryQueued` when retry policy allows
- `state = Released` or blocked when max attempts are exceeded or the failure is
  non-retryable
- `last_outcome = execution_failed`
- `policy_status = ok` for retryable recovery
- `eligibility = eligible` for retryable recovery
- incremented `attempt`
- populated `retry_due_at`
- deterministic recovery `request_id`
- denial reason such as `orphaned running card: runtime child not found`

Do not write a separate retry table entry. The projected card is the queue.

### Terminal Child

Use the same projection semantics as existing reconcilers:

- completed child or worker: project completed/released/review state
- failed or cancelled child or worker: project failed/retry or blocked state

### Still Running

Leave the card in `Running`.

Do not mark very recent dispatches as orphaned. Use a startup grace period based
on the card's `LastEventAt` to avoid registration races.

## Layer 1 Checkpointing Risk Register

Runner checkpointing needs a separate design before implementation.

The unsafe crash windows include:

| Crash window | Replay risk |
| --- | --- |
| After `run.started`, before checkpoint save | duplicate event or stream-version conflict |
| After generated `turn_id`, before caller sees it | checkpoint may be unaddressable |
| During pre-hooks | duplicate hook side effects |
| After model response, before checkpoint | model called again with divergent output |
| After `tool.invoked`, before tool execution | duplicate tool intent or stream conflict |
| After tool execution, before `tool.responded` | duplicate external tool side effect |
| After `tool.responded`, before checkpoint | duplicate tool history or stream conflict |
| After `SaveTurn`, before `turn.recorded` | turn rows may be rewritten on replay |
| After `turn.recorded`, before checkpoint | stream-version conflict |
| After `run.completed`, before checkpoint/delete | stream-version conflict |
| After final checkpoint save, before delete | stale completed checkpoint can rerun work |
| Checkpoint save failure after side-effecting stage | old checkpoint can replay new side effects |

Before implementing runner checkpoint resume, define these invariants:

1. Every durable side effect is atomic with the checkpoint or replay-idempotent.
2. Durable mode has a stable caller-supplied identity, preferably
   `(run_id, request_id)`.
3. `turn_id` is generated before any side effects and stored in the checkpoint.
4. The checkpoint stores stream version and sequence as consistency cursors.
5. Loading a checkpoint compares its cursor with the actual event stream.
6. Model responses and tool results are journaled before replay can skip them.
7. Tool execution receives stable idempotency keys or is documented as
   at-least-once.
8. Checkpoint failures report out-of-band and do not emit semantic
   `stage.failed` events.

Use a versioned checkpoint DTO, not raw `executionState`. Exclude secrets,
interfaces, hooks, clocks, ID generators, event bus state, channels, mutexes,
and unbounded tool output. Store large outputs through artifact or CAS refs.

## Implementation Plan

### PR 1: Spec And Contract

1. Add this plan.
2. Add a small `StartupRecovery` contract in
   `internal/v2/runtime/orchestration`.
3. Add a card-reader contract for listing `StateRunning` cards.
4. Document that `v2_orchestration_cards` is the durable retry queue.

Verification:

- `make check-doc-links`
- targeted compile or package tests for new contract packages

### PR 2: Projection Query

1. Add a narrow store method to list running orchestration cards.
2. Keep the query deterministic and bounded.
3. Include workspace filtering if available.
4. Preserve existing `issue_id` primary-key behavior; do not change
   multi-workspace semantics in this PR.

Verification:

- store tests for empty, running, non-running, archived, and workspace-filtered
  cards

### PR 3: Component Lifecycle

1. Add `StartupRecovery` to `ComponentConfig`.
2. Run startup recovery before scheduler dispatch.
3. If recovery fails, call `OnError` and skip dispatch for that cycle.
4. Preserve existing immediate reconcile behavior.

Verification:

- component test that recovery runs before scheduler
- component test that recovery failure skips scheduler
- component test that normal reconcile still runs

### PR 4: Go Subprocess Recovery

1. Implement a Go subprocess `RunningCardInspector`.
2. Use `coreworker.StateReader` and the worker store.
3. Handle terminal, running, missing, and too-recent cards.
4. Reuse existing event/projection semantics for completed and failed outcomes.

Verification:

- terminal worker becomes released/review or retry/blocked
- missing worker becomes retry/failure after grace period
- live worker remains running
- recent card remains running
- repeated recovery is idempotent at the projection level

### PR 5: Optional Jido Recovery

1. Implement a Jido `RunningCardInspector`.
2. Use Jido parent/child APIs.
3. Treat runtime unavailable as a recovery error.
4. Treat specific child missing as orphaned only when the parent/runtime is
   reachable.
5. Wire this path only for explicit `jido` backend selection.

Verification:

- unreachable Jido runtime does not mutate cards and skips scheduler dispatch
- reachable parent with missing child recovers to retry/failure
- terminal completed child recovers to released/review
- terminal failed child recovers to retry/blocked
- live child remains running

### PR 6: Wiring And Restart Behavior

1. Wire startup recovery in `cmd/foxctl/cmd/overseer_v2_orchestration.go` for
   default Go runtime and explicit Jido modes.
2. Do not add or wire a durable retry queue table.
3. Add restart-oriented tests around due `RetryQueued` cards.

Verification:

- due `RetryQueued` card dispatches before `Todo`
- not-yet-due `RetryQueued` card is skipped
- default Go runtime backend wiring includes recovery
- `FOXCTL_V2_ORCHESTRATION_RUNTIME_BACKEND=jido` wiring includes recovery

## Acceptance Criteria

The first milestone is complete when:

1. Restarting an overseer with due `RetryQueued` cards dispatches them before
   fresh `Todo` cards.
2. Restarting with `Running` cards and reachable terminal runtime state projects
   completed or failed outcomes.
3. Restarting with `Running` cards and reachable missing child state recovers
   those cards into retry or blocked state using canonical orchestration
   events.
4. Restarting while the Go worker-state runtime or explicit Jido backend is
   unavailable does not mark all running cards failed.
5. No `v2_orchestration_retry_queue` table exists.
6. No runner checkpoint resume is claimed or shipped.

## Later Work: Runner Durable Resume

Only revisit runner checkpointing after the orchestration slice is stable.

The next design should start with:

- stable durable identity, likely `(run_id, request_id)`
- versioned checkpoint DTO
- stream cursor validation
- event append idempotency or atomic append/checkpoint writes
- model response journal
- tool call intent/result journal
- explicit at-least-once versus exactly-once tool semantics
- tests for every crash window listed in this plan
