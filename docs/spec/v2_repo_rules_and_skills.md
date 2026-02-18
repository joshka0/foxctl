# Agentctl V2 Repo Rules and Core Skills

Status: Draft  
Owner: Solo maintainer  
Last Updated: 2026-02-17

## Purpose

Define the minimum rules and skill set needed to keep v2 consistent, maintainable, and low-friction as a one-person codebase.

## Non-Negotiable Repo Rules

### Architecture Rules

1. One orchestration path per operation (`spawn`, `ask`, `run`, `kill`).
2. No domain orchestration in transport handlers.
3. Core packages must not import storage/network adapters.
4. No second tool execution path.
5. Tool exposure is profile-based, never ad hoc.

### Runtime Rules

1. Every turn executes the same pipeline stages.
2. Every stage emits typed events.
3. Cancellation and timeout are first-class, tested behaviors.
4. No hidden global mutable state.
5. Request/turn execution must not block on maintenance jobs.
6. Async queues are bounded and have explicit overflow handling.
7. Shared read state is exposed as immutable snapshots.

### Data Rules

1. Event append is authoritative write path.
2. Read models are projections only.
3. IDs, timestamps, and ordering are deterministic in tests.
4. Secrets and sensitive args are redacted in events/logs.

### Contract Rules

1. Tool input/output schemas are explicit and versioned.
2. Envelope fields are stable unless version increment is documented.
3. Breaking contract changes require spec updates in `docs/spec/`.

### Quality Rules

1. New behavior requires tests before merge.
2. Golden tests for protocol/event outputs must be deterministic.
3. Keep functions short and stage-specific where possible.
4. Prefer pure core logic and thin imperative shells.

### Go Design Pattern Rules

1. Long-lived runtime services implement `Run(ctx context.Context) error`.
2. One host/supervisor owns component lifecycle via `errgroup`.
3. Async boundaries use bounded channels with explicit drop/backpressure behavior.
4. Shared mutable runtime state has a single goroutine owner.
5. High-frequency reads use immutable snapshots (`atomic.Pointer`/`atomic.Value`), not coarse locks.
6. Service interfaces stay small and capability-scoped.

## Core V2 Skill Set (MVP)

Use category/skill naming to keep discovery and allowlists consistent.

### `fs/*`

1. `fs/read_file`
2. `fs/list_dir`
3. `fs/write_file`

Rules:

- all paths validated through one path validator
- workspace-scoped by default

### `code/*`

1. `code/search`
2. `code/symbols`

Rules:

- bounded results
- deterministic ordering for stable tests

### `edit/*`

1. `edit/apply_patch`

Rules:

- explicit preview metadata when available
- structured diff metadata in result

### `test/*`

1. `test/run`

Rules:

- result includes exit code, summary, and artifact references when large

### `mail/*`

1. `mail/inbox`
2. `mail/send`
3. `mail/ack`

Rules:

- correlation id required for request/reply workflows
- idempotent ack behavior

### `agent/*`

1. `agent/spawn`
2. `agent/ask`
3. `agent/status`
4. `agent/wait`

Rules:

- spawn always routes through `SpawnService`
- depth/policy checks centralized

### `memory/*` (minimal)

1. `memory/query`
2. `memory/put`

Rules:

- workspace scoping required
- summary+artifact pattern for large payloads

### `observe/*` (control-plane)

1. `observe/events`
2. `observe/health`
3. `observe/snapshot`

Rules:

- read-only from projection/snapshot surfaces
- must not block turn execution path

## Tool Contract Requirements

Every tool definition must include:

1. canonical name
2. description
3. JSON schema input
4. policy flags
5. deterministic output envelope

Required policy flags:

1. `requires_confirmation`
2. `external_execution`
3. `stop_after_tool_call`

## Process Profiles and Skill Allowlists

MVP profile mapping:

1. `overseer`: `agent/*`, `mail/*`, `code/*`, read-only `fs/*`
2. `worker`: `fs/*`, `code/*`, `edit/*`, `test/*`, `mail/*`
3. `companion`: `mail/*`, limited `memory/*`, optional read-only `code/*`

Rule: profiles own defaults; per-agent overrides can only narrow, never broaden.

## PR Checklist (Required for V2 Changes)

1. Does this add a second way to do an existing operation?
2. Does this move logic into a transport layer?
3. Are specs updated for contract changes?
4. Are stage-level tests included?
5. Are event outputs deterministic and redacted?
6. Is docs link integrity still passing?

## Suggested CI Gates for V2 Folder

1. `go test ./internal/v2/...`
2. deterministic golden check for `internal/v2/core/events`
3. lint rule to prevent `internal/v2/core` importing adapter packages
4. docs link check on `docs/spec/*.md`

## Skills Governance (Solo Mode)

1. Keep core skill count small; add only when a real repeated workflow appears.
2. New skill must define owner (you), profile, and policy flags.
3. Any new skill requires:

- schema tests
- redaction test
- at least one integration test in runner context

4. Review skill sprawl monthly and merge/delete low-value skills.

## Naming and Conventions

1. Use `category/skill` canonical names in docs and manifests.
2. Map legacy aliases at boundaries only.
3. Internal code should use canonical names only.

## Deprecation Policy

1. Mark deprecated tools in spec first.
2. Keep alias compatibility for one minor cycle.
3. Remove only after migration tests and docs updates.
