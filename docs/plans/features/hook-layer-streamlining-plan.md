# Hook Layer Streamlining Plan

Status: active plan

Owner: agentctl

Last updated: 2026-03-11

## Goal

Reduce hook complexity by moving lifecycle orchestration out of shell scripts and
into typed Go components, while keeping shell wrappers as thin provider adapters.

This is not a manifest-version migration. `agentctl/v1` is still the current
skill manifest contract. The real cleanup target is the split between:

- a reasonably coherent Go hook engine under `internal/hooks/`
- a large shell-hook estate under `configs/hooks/`

## Current State

Stable now:

- hook configuration and merge semantics in `internal/hooks/config.go`,
  `internal/hooks/types.go`, `internal/hooks/dispatcher.go`, and
  `internal/hooks/merge.go`
- hook execution adapters in:
  - `internal/hooks/shell_runner.go`
  - `internal/hooks/skill_runner.go`
  - `internal/hooks/executor.go`
- ACA/session lifecycle behavior in shell scripts under `configs/hooks/`

Pain points now:

- lifecycle orchestration logic is encoded in bash rather than typed Go
- env/path/session handling is repeated across scripts
- async capture and restore behavior is hard to test deterministically
- ACA logic exists in both Go and shell glue, which increases drift risk

## Target Shape

Keep this split:

- shell wrappers: provider-specific glue only
- Go lifecycle layer: hook behavior, orchestration, state mutation, reporting

Desired shell responsibilities:

- read hook stdin/env from provider
- resolve workspace/binary
- invoke a single `agentctl hooks ...` command
- emit provider-compatible JSON

Desired Go responsibilities:

- orientation and ACA state refresh
- daemon warmup / ensure-running logic
- session identity persistence
- session restore / anchor composition
- session capture / summarize follow-up
- ACA infer / promote / maintenance actions
- deterministic report and action surfaces

## Scope

In scope:

- lifecycle hooks:
  - `SessionStart`
  - `SessionEnd`
  - `SubagentStop`
- shared ACA/session helper consolidation
- new CLI/runtime entrypoints under `agentctl hooks ...`
- tests for lifecycle behavior without shelling through bash

Out of scope for this pass:

- rewriting every advisory shell hook
- removing shell-hook support entirely
- changing hook config schema
- changing the `agentctl/v1` skill manifest contract

## Phases

## Phase 1: SessionStart

Objective:

Move the current `configs/hooks/session-init.sh` orchestration into Go.

Deliverables:

- `agentctl hooks session-start`
- typed response contract for provider shell wrappers
- wrapper script becomes a thin adapter

Behavior to preserve:

- detect workspace
- best-effort daemon warm/ensure-running
- compute ACA orientation
- persist/read session identity
- restore context on `resume|compact`
- include session anchor when present

Primary files:

- `cmd/agentctl/cmd/hooks_runtime.go`
- `internal/hooks/lifecycle/*` or equivalent command helpers
- `configs/hooks/session-init.sh`

Verification:

- unit tests for response composition
- shell wrapper smoke test

## Phase 2: SessionEnd

Objective:

Move `configs/hooks/session-end.sh` orchestration into Go.

Deliverables:

- `agentctl hooks session-end`
- Go-owned async capture/summarize/ACA-infer flow
- shell wrapper reduced to payload pass-through

Behavior to preserve:

- session capture
- optional summarization
- ACA handoff capture
- inference and auto-promotion hooks
- append-only user prefs / gotchas updates

Verification:

- tests for capture request composition
- tests for append-only note updates

## Phase 3: SubagentStop

Objective:

Move `configs/hooks/subagent-stop.sh` into Go.

Deliverables:

- `agentctl hooks subagent-stop`
- bounded subagent ACA capture
- shared lifecycle helper reuse from Phases 1-2

Verification:

- tests for summary extraction fallback
- tests for ACA capture request composition

## Phase 4: Shared Lifecycle Core

Objective:

Extract shared lifecycle orchestration from commands into reusable Go helpers.

Deliverables:

- session identity persistence helper
- hook response encoder
- daemon warm/ensure helper
- ACA lifecycle helper package

Verification:

- table-driven unit tests for helper behavior
- no business logic remaining in shell scripts beyond argument/env bridging

## Design Rules

- shell wrappers should not contain orchestration decisions
- hook lifecycle logic should be deterministic and unit-testable
- background follow-up work must remain fail-open where current behavior is fail-open
- ACA behavior should be implemented once in Go, not copied in shell
- provider-specific JSON adaptation belongs at the boundary only

## Success Criteria

- `session-init.sh`, `session-end.sh`, and `subagent-stop.sh` become thin wrappers
- lifecycle logic is testable from Go without invoking bash
- ACA and session behavior stop drifting between shell and Go paths
- future hook changes mostly touch Go packages, not shell scripts

## First Slice

Start with `SessionStart`.

Reason:

- it has the clearest input/output shape
- it is central to ACA orientation
- it contains enough orchestration to prove the pattern
- it reduces repeated session/daemon/workspace logic immediately
