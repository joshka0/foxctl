# Durable Execution Layer 1 Side-Effect Safety

Status: Proposed
Owner: Solo maintainer
Last Updated: 2026-05-06

## Goal

Make runner execution retry-safe in small layers without claiming checkpoint
resume before model and tool effects are journaled.

Layer 1-A already established strict durable identity and event stream cursor
readiness. The next work should add durable request idempotency first, then
event append idempotency, then model/tool effect journaling.

## Design Principle

A runner can only replay across a boundary where every previous side effect is
either:

- durably recorded and reusable, or
- idempotently acknowledged as already done.

Stage-level checkpoints alone do not satisfy that rule. Stages can emit events,
persist turns, call models, and execute tools before a checkpoint save could be
made atomic with those effects.

## Sequence

### Layer 1-B: Turn Request Registry

Add a request-level idempotency registry before `Pipeline.RunTurn`.

Purpose:

- prevent duplicate client retries from starting a second model/tool turn
- key durable turn execution by `(run_id, request_id)`
- store caller-visible `turn_id`
- return stored terminal success/error for duplicate terminal requests
- fail duplicate in-progress requests without calling the runner again
- reclaim stale `running` requests after a conservative orphan window
- refresh `running` request activity while a live runner is still executing

Minimal shape:

```go
type TurnRequestStatus string

const (
	TurnRequestRunning   TurnRequestStatus = "running"
	TurnRequestSucceeded TurnRequestStatus = "succeeded"
	TurnRequestFailed    TurnRequestStatus = "failed"
	TurnRequestCanceled  TurnRequestStatus = "canceled"
)

type TurnRequestRecord struct {
	RunID       string
	RequestID   string
	TurnID      string
	Status      TurnRequestStatus
	OutputJSON  json.RawMessage
	ErrorJSON   json.RawMessage
	StartedAt   time.Time
	CompletedAt time.Time
	UpdatedAt   time.Time
}

type TurnRequestRegistry interface {
	BeginTurnRequest(ctx context.Context, rec TurnRequestRecord) (existing TurnRequestRecord, inserted bool, err error)
	CompleteTurnRequest(ctx context.Context, rec TurnRequestRecord) (TurnRequestRecord, error)
	GetTurnRequest(ctx context.Context, runID, requestID string) (TurnRequestRecord, error)
}

type TurnRequestToucher interface {
	TouchTurnRequest(ctx context.Context, runID, requestID, turnID string, now time.Time) (TurnRequestRecord, touched bool, err error)
}

type StaleTurnRequestRecoverer interface {
	RecoverStaleTurnRequest(ctx context.Context, rec TurnRequestRecord, staleBefore time.Time) (TurnRequestRecord, recovered bool, err error)
}
```

Suggested Turso table:

```sql
CREATE TABLE IF NOT EXISTS v2_turn_requests (
	run_id TEXT NOT NULL,
	request_id TEXT NOT NULL,
	turn_id TEXT NOT NULL,
	status TEXT NOT NULL,
	output_json TEXT NOT NULL DEFAULT '',
	error_json TEXT NOT NULL DEFAULT '',
	started_at TEXT NOT NULL,
	completed_at TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL,
	PRIMARY KEY (run_id, request_id),
	CHECK (status IN ('running', 'succeeded', 'failed', 'canceled'))
);

CREATE INDEX IF NOT EXISTS idx_v2_turn_requests_turn
	ON v2_turn_requests(run_id, turn_id);
```

Integration belongs in `RunService.Run`, not inside runner stages:

1. validate and trim `run_id`
2. when registry is configured, require `request_id` and `turn_id`
3. call `BeginTurnRequest`
4. if existing terminal row exists, return stored output or error
5. if existing fresh running row exists, return a typed non-executing conflict
6. if existing stale running row exceeds the configured window and the registry
   supports recovery, atomically reclaim it before runner entry; the default
   window is 30 minutes, and explicit zero/negative config disables recovery
7. while `runner.RunTurn` is active, periodically touch the request if the
   registry supports `TurnRequestToucher`
8. execute `runner.RunTurn`
9. store terminal output or error

This still leaves a known non-atomic gap: if terminal registry update fails
after runner side effects complete, duplicate requests may see `running` while
the event stream is terminal. Stale recovery may re-run that request after the
orphan window, so this is still at-least-once recovery. Do not call this
checkpoint resume.

### Layer 1-C: Idempotent Event Append

Add a narrow append-if-absent contract using existing `Event.ID` as the
idempotency key.

Minimal shape:

```go
type AppendResult struct {
	Event    Event
	Appended bool
}

type AppendIfAbsent interface {
	AppendIfAbsent(ctx context.Context, event Event) (AppendResult, error)
}
```

Semantics:

- if `event.ID` does not exist, enforce strict next stream version/sequence and
  insert
- if `event.ID` exists and material fields match, return the stored event with
  `Appended=false`
- if `event.ID` exists with conflicting material fields, return an idempotency
  conflict
- keep `Append` as the strict append API

Use deterministic event IDs in durable mode. Do not use `stream_version` or
`sequence` in the ID because those are append cursors, not logical identity.

Suggested ID shape:

```text
evt:run:<run_id>:turn:<turn_id>:req:<request_id>:<event_type>:<ordinal>
```

No schema migration is needed if the existing `v2_events.id` primary key is the
dedupe key. Avoid adding `idempotency_key`, payload hash columns, or append
attempt audit tables until there is a concrete need.

### Layer 1-D: Model And Tool Effect Journal

Add this only after Layer 1-B and 1-C are stable.

Journal facts, not private runner state:

- model call intent before provider execution
- model request and model response per iteration
- tool call intent before execution
- tool result after execution
- stable model iteration and tool-call idempotency keys
- explicit tool replay policy

Replay rules:

- if a model response exists, reuse it and do not call the model
- if a model intent exists without a terminal result, fail closed for operator
  recovery
- if a tool result exists, reuse it and do not execute the tool
- if a tool intent exists without a result, retry only when the tool is
  explicitly `read_only` or `idempotent`; otherwise fail closed for operator
  recovery

No exactly-once model/tool execution claim should be made before this layer.

Implementation note for the Layer 1-D slices:

- the LLM-chat runner path has an optional `EffectJournal`
- Turso stores model effects and tool effects under
  `internal/v2/adapters/turso/effects`
- default web runtime wiring opens the effect journal alongside the turn request
  registry
- model intents are saved before provider calls
- model results are marked `succeeded` or `failed` before the runner consumes
  terminal data for later stages
- tool intents are saved before `tool.invoked` and before tool execution
- tool results are saved before `tool.responded`
- replay reuses completed model responses and completed tool results
- replay fails closed when it finds a model intent without a terminal result
- replay fails closed when it finds a tool intent without a terminal result
  unless the tool definition opts into `read_only` or `idempotent` replay
- core tool definitions carry the durable replay policy in
  `ToolPolicy.EffectReplay`

Still out of scope:

- RLM REPL effect journaling
- recovering a model response if the process crashes after the provider returns
  but before `CompleteModelEffect`
- retrying side-effecting tools with recorded intent but no result
- atomic event append plus projection apply

## What To Avoid

- checkpoint table in Layer 1-B or 1-C
- gob-encoded `executionState`
- model/tool replay before journaling
- broad workflow/job framework
- projection-only idempotency for runner execution
- resurrecting `internal/v2/adapters/libsql/*`
- idempotency based only on `request_id` for events, since one request emits
  multiple events
- tests of private helpers instead of observable behavior

## Test Strategy

Layer 1-B high-value tests:

- real Turso registry insert is unique on `(run_id, request_id)`
- duplicate running request does not call the runner
- stale running request is reclaimed and calls the runner once
- default stale window is preserved, shorter configured windows recover, and
  zero/negative configured windows disable stale recovery
- terminal row is not overwritten by stale recovery
- duplicate succeeded request returns stored `TurnOutput`
- duplicate failed request returns stored terminal error
- registry configured with missing `turn_id` or `request_id` fails before
  runner side effects
- default runtime wiring keeps public request JSON shape unchanged

Layer 1-C high-value tests:

- `AppendIfAbsent` inserts a new event
- replaying the same event ID returns the stored event without stream-version
  conflict
- same event ID with different material fields returns idempotency conflict
- a different event with stale stream version still returns version conflict
- deterministic runner event IDs are stable for fixed input

Layer 1-D high-value tests:

- completed model response is reused and model is not called again
- completed tool result is reused and tool executor is not called again
- existing tool intent without result fails closed for non-idempotent tools

Prefer real Turso-backed store tests for persistence/idempotency behavior. Use
runner fakes only to count whether the model, tool executor, or runner was
called.

## Swarm Review Summary

The swarm converged on this order:

1. request registry first, because it prevents duplicate runner entry
2. event append idempotency second, because it prevents duplicate event side
   effects from causing stream conflicts
3. model/tool journal third, because that is the first layer that can make
   mid-`StageModelCall` replay safe

This sequence gives useful durable behavior early while keeping each change
small, testable, and honest about residual crash windows.
