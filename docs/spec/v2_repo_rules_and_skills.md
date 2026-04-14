---
vault_refs:
  - notes/repo/foxctl/skills-runtime-wiring.md
  - notes/repo/foxctl/platform-and-web.md
  - notes/repo/foxctl/semantic-and-memory.md
  - notes/repo/foxctl/packages/internal-adapters-skillslib-skillerr.md
  - notes/repo/foxctl/packages/internal-adapters-skillslib-skillmain.md
---
# Agentctl V2 Repo Rules and Core Skills

Status: In Progress  
Owner: Solo maintainer  
Last Updated: 2026-03-12

## Purpose

Define the minimum rules and skill set needed to keep v2 consistent, maintainable, and low-friction as a one-person codebase.

## Version Boundary

This document governs `internal/v2/*` work only.
Supported agent command surfaces are hard-cut to v2 paths; this document does
not define v1 fallback routing controls.

## Related Docs

- `docs/spec/v2_greenfield_bootstrap.md`
- `docs/plans/v2-greenfield-bootstrap.md`
- `docs/plans/features/v2-skills-parity-plan.md`
- `docs/general/runtime-orchestration.md`
- `docs/general/agent-daemon.md`
- `docs/general/memory.md`
- `docs/general/companion-memory.md`
- `docs/general/context-and-observability.md`
- `docs/architecture/context-architecture.md`

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

### `context/*` (ACA control plane, read-only)

1. `context/show`
2. `context/retrieve`

Rules:

- read-only access to top-of-mind and blended ACA retrieval state
- safe for overseer/companion-style orientation flows
- must not become a second orchestration path for task mutation

### `obsidian/*` (knowledge plane, read-only)

1. `obsidian/index_search`
2. `obsidian/read`
3. `obsidian/related`

Rules:

- read-only vault access by default in v2 profiles
- retrieval over the knowledge layer should remain bounded and deterministic
- write-side Obsidian flows stay out of the v2 MVP until dry-run/plan-apply semantics are defined cleanly

### Plugin / extension tool families

These are not part of the minimal portable v2 core set. They should be treated as plugin-style or repo-local extension surfaces that are registered intentionally by a project:

1. `heartwood/state`
2. `heartwood/action`

Rule: extension tools should be documented as project-specific and only added to profile allowlists through an explicit project overlay or plugin registration step.

## Current Parity Gap

The classic agent runtime currently exposes a broader practical tool surface than the v2 runtime governance docs imply.

Today this repo has real, production-facing read-only surfaces for:

- ACA control-plane retrieval
- Obsidian knowledge-layer retrieval
- repo-index-era search and DAG traversal
- project-local Heartwood tooling

So v2 work should track two separate parity concerns:

1. **profile/docs parity**
   The v2 profile allowlists and skill-governance docs should reflect the read-only ACA/Obsidian surfaces that already exist.
2. **tool-definition/executor parity**
   The actual v2 tool catalog still needs a concrete non-test source of `ToolDef` values and delegate wiring for those newer tools.

The first is a low-risk docs/allowlist exercise.
The second is the real runtime implementation task.

That implementation task now has a canonical assembly path:

- `internal/v2/runtime/tools/default_defs.go` provides the non-test default catalog input
- `internal/v2/adapters/toolbridge/bridge.go` provides the default delegate/executor bridge
- `internal/v2/services/default_runtime.go` provides the canonical service-facing builder for the default catalog + delegate + runner stack

The remaining gap is broader adoption of that builder in live v2 runtime entrypoints, not the absence of a production assembly path.

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

1. `overseer`: `agent/*`, `mail/*`, `code/*`, repo-index search, ACA read-only `context/*`, read-only `obsidian/*`, read-only `fs/*`
2. `worker`: `fs/*`, `code/*`, `edit/*`, `test/*`, `mail/*`
3. `companion`: `mail/*`, limited `memory/*`, read-only ACA `context/*`, read-only `obsidian/*`, optional read-only `code/*`

Rule: profiles own defaults; per-agent overrides can only narrow, never broaden.

Project-local extensions such as `heartwood/*` should not be assumed by the portable defaults above. They belong in repo-specific overlays.

## PR Checklist (Required for V2 Changes)

1. Does this add a second way to do an existing operation?
2. Does this move logic into a transport layer?
3. Are specs updated for contract changes?
4. Are stage-level tests included?
5. Are event outputs deterministic and redacted?
6. Is docs link integrity still passing?
7. Is there a subagent review note recorded before marking completion?
   Include reviewer id/scope and findings summary (`none` is acceptable if explicitly stated).

## Subagent Review Protocol (System Prompt + DoD)

Use a second-pass subagent review before marking any v2 slice complete.

### Recommended Subagent System Prompt

```text
You are a strict v2 reviewer for foxctl.

Priorities (in order):
1) Design correctness and architecture boundaries
2) Consistency with v2 contracts and naming
3) Simplicity and maintainability
4) Elegance (minimal, coherent structure)

Rules:
- Review only against v2 docs/spec intent and changed files.
- Flag violations of: single orchestration path, no core->adapter/port imports,
  deterministic tests/outputs, non-blocking turn path guarantees.
- Prefer concrete findings with file+line references.
- If no issues, explicitly say "no findings" and list residual risks briefly.
```

### Review DoD

A slice is complete only when:

1. reviewer id (or handle) is recorded
2. reviewed scope/files are recorded
3. findings are recorded (`none` explicitly allowed)
4. required fixes are applied or consciously deferred with rationale
5. final status is recorded as `approved` or `approved-with-known-risks`

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
