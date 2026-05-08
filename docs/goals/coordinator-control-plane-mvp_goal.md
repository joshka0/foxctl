# Goal: Coordinator Control Plane MVP

## Goal

Implement the governed coordinator-owned control loop as five integrated,
reviewable PR slices.

The delivered system should establish this contract:

```text
Signals become proposals.
Proposals require decisions.
Decisions cite evidence.
Applies are idempotent.
Tasks are created once.
Generated memory starts evidence-only.
Pi observes, escalates, and overrides.
Rooms execute and report.
```

The coordinator is the normal decision-maker. Humans are on-the-loop, not
in-the-loop: Pi should surface status, escalation, override, pause, kill, and
rare clarification, but ordinary low-risk decisions should be handled by
coordinator policy and harness evidence.

## Context

- Repo: `/Users/joshka/repos/personal/foxctl`.
- External review conclusion: the missing boundary is not another event system;
  it is a durable decision layer between observed signals and durable state
  mutation.
- Current architecture already has useful substrate:
  - hooks and hook action contracts:
    `internal/runtime/hooks/types.go`,
    `skills/hooks_dispatch/main.go`,
    `internal/runtime/hooks/executor.go`
  - contextplane memory proposal storage:
    `internal/context/contextplane/types.go`,
    `internal/context/contextplane/mutable_store.go`,
    `internal/context/contextplane/proposals.go`
  - memory vocabulary and curator reports:
    `internal/context/memorycore/record.go`,
    `internal/context/memorycore/curator.go`,
    `skills/memory_curator_report/main.go`
  - task and room coordination:
    `internal/storage/tasks/store.go`,
    `cmd/foxctl/cmd/room_tasks.go`,
    `internal/runtime/orchestration/roomruntime/send.go`
  - agent daemon durability:
    `internal/agent/daemon/daemon.go`,
    `internal/agent/daemon/dedupe_sqlite.go`,
    `internal/agent/daemon/handlers.go`
  - Pi integration:
    `integrations/pi/README.md`,
    `integrations/pi/foxctl.ts`
  - foxprox integration status:
    `docs/foxprox/ROOM-INTEGRATIONS.md`,
    `foxprox/foxprox/broker/storage/sqlite/sqlite.go`,
    `foxprox/foxprox/broker/router/router.go`,
    `foxprox/foxprox/transport/httpjson/events.go`
  - task history and evidence substrate:
    `internal/context/contextplane/taskhistory/`,
    `internal/runtime/observability/`
- Package placement:
  `docs/architecture/package-topology.md` assigns contextplane ownership for
  orientation, proposals, retrieval inspection, promotion helpers, and task
  history.
- Related plan:
  `docs/plans/features/aca-self-evolving-memory-layer.md`.

## Architecture Contract

Use these boundaries throughout all five PRs:

```text
Coordinator = policy, evidence, and authority decision-maker
Rooms       = execution and collaboration substrate
Pi          = operator console, escalation, override, and status surface
Hooks       = signal capture and immediate guardrails
Memory      = candidate, proposal, eval, and promotion system
Tasks       = durable work items created only by approved proposals
```

Do not treat Pi confirmation, room consensus, or harness success as implicit
authorization. They become authority only when persisted as, or cited by, a
typed `CoordinatorDecision`.

## Constraints

- Keep this generic across repositories and languages. Do not bake in
  foxctl-only assumptions except where wiring into foxctl itself is required.
- Prefer an additive lane in `internal/context/contextplane` for the first
  proposal and decision model.
- Do not introduce keyword or substring heuristics for behavioral routing,
  classification, promotion, or suppression. Use typed fields, explicit policy,
  structured model outputs, scored features, or schema-backed signals.
- Preserve envelope/protocol invariants and `meta.*` behavior.
- Do not add dependencies without explicit approval.
- Do not add libSQL, sqlite-vector extension loading, cgo sqlite dependencies,
  `-tags=libsqlite3`, or legacy storage compatibility paths.
- Do not let generated agent output write active memory directly.
- Do not let hook events create unbounded tasks, room jobs, or background agent
  work. Use dedupe keys, budgets, leases, idempotency keys, and visible state.
- Do not collapse foxctl rooms and foxprox rooms in the first pass. Bridge them
  with explicit source IDs and keep one canonical state owner per flow.
- Do not rely on raw PTY scrollback or plain assistant text sentinels as durable
  task, room, memory, or completion truth.
- Keep the PRs small and composable. Each PR must be independently reviewable
  and have focused tests.
- Use semantic comments only for durable retrieval boundaries; avoid broad
  mechanical `Index:` blocks.
- Run `make check-doc-links` whenever markdown changes.

## PR Plan

### PR 1: Coordinator Control Plane MVP

Add the durable control-plane records and storage helpers.

Implement in `internal/context/contextplane` unless package topology forces a
different existing family:

- `ControlProposal`
- `CoordinatorDecision`
- `ApplyResult`
- typed `ProposalKind` values, starting with:
  - `task_proposal`
  - `memory_candidate`
- typed proposal statuses:
  - `open`
  - `evaluating`
  - `needs_clarification`
  - `needs_authority`
  - `needs_harness`
  - `conflicting_evidence`
  - `unsafe_side_effects`
  - `approved`
  - `applying`
  - `applied`
  - `rejected`
  - `superseded`
  - `failed`
- typed decision values:
  - `approve`
  - `reject`
  - `defer`
  - `escalate`
  - `needs_clarification`
  - `request_harness`
- typed authority modes:
  - `coordinator_policy`
  - `room_consensus`
  - `human_approval`
  - `human_override`

The coordinator decision should cite evidence and policy. Harness results are
evidence, not an authority mode by themselves.

Done when:

- Proposal records have stable `dedupe_key` uniqueness and duplicate signals
  increment `count` without creating duplicate proposal rows.
- Decisions are append-only and do not overwrite historical decisions.
- Apply results are separate from decisions and include idempotency keys.
- The current derived proposal state can be listed for CLI/API/Pi read models.
- Existing `MemoryProposal` behavior remains intact.
- Tests cover proposal dedupe, append-only decisions, invalid transitions,
  apply idempotency, and latest-state derivation.

### PR 2: Hook Proposal Mode

Add a governed hook path that emits proposals instead of directly creating
durable tasks.

Implement:

- `FOXCTL_TASK_GUARD_MODE=proposal` for `skills/hooks_task_guard`.
- Proposal-mode behavior for write/tool events without an active task:
  - record a `task_proposal`
  - do not create a task directly
  - attach source refs and event evidence
  - use a stable dedupe key derived from workspace, session, event/tool kind,
    scope path, and normalized intent
  - increment `count` on repeated equivalent events
- Hook dispatch accounting for emitted/executed/skipped/unavailable actions.
- A clear distinction between advisory hooks, proposal hooks, guard hooks, and
  critical guard hooks where critical action failures do not silently approve.

Done when:

- Proposal mode does not create tasks directly.
- Strict mode behavior remains available.
- Existing auto mode remains compatible, but governed workflows can use
  proposal mode.
- Hook dispatch reports skipped or unavailable durable actions explicitly.
- Tests prove duplicate hook events become one proposal with incremented count.
- Tests prove missing workspace or unsafe scope cannot auto-approve.

### PR 3: Pi Cockpit Read Model

Make Pi an operator cockpit for coordinator state without making Pi the default
authority source.

Implement:

- A Pi-readable overview of:
  - open proposals
  - latest decisions
  - apply results
  - active rooms
  - active jobs or harness runs where available
  - escalation states requiring human attention
- Pi tools or slash commands to:
  - list proposal inbox
  - inspect proposal details and evidence refs
  - approve, reject, override, pause, kill, or request clarification where the
    action is supported
- Every Pi approve/reject/override action must persist a typed
  `CoordinatorDecision`; a Pi UI confirmation alone has no authority.
- Room and foxprox status shown in Pi must identify which backend/source owns
  the state being displayed.

Done when:

- Pi can show pending coordinator work without requiring raw JSON archaeology.
- A human override from Pi creates a durable decision record.
- Pi cannot apply a proposal without a decision record.
- Read-only status still works when no room is active.
- Tests or typed checks cover the Pi command/tool payloads.
- A local dogfood note records the exact Pi command/tool path used.

### PR 4: Memory Candidate Governance

Make generated memory safe by default.

Implement:

- `memory_candidate` apply path that creates only evidence-backed candidate
  memory.
- Generated memory defaults:
  - lifecycle `candidate`
  - review `unreviewed` or `needs_review`
  - `instruction_eligible=false`
  - evidence-only unless promoted later
  - source refs required
- Policy-gated handling for agent-generated memory writes. If the existing
  agent memory tool path writes generated content, route it to candidate memory
  or require a coordinator decision before any active write.
- Bridge existing memory proposal and curator outputs into the unified proposal
  inbox where action is required, without rewriting curator internals.

Done when:

- Generated memory cannot silently inherit active named-memory defaults.
- Candidate memory is retrievable as evidence but not as instruction policy.
- Promotion remains separate through curator/eval/governed decision paths.
- Existing memory proposal and curator tests still pass.
- Tests prove generated memory starts evidence-only and
  `instruction_eligible=false`.
- Tests prove direct active memory writes remain available only for explicit,
  trusted/manual paths if those paths already existed.

### PR 5: Room Job and Harness Integration

Connect coordinator decisions to bounded execution and evidence loops.

Implement:

- A coordinator one-shot processor first, not a broad autonomous daemon:

  ```bash
  foxctl coordinator process --workspace . --limit 20
  ```

- MVP policy:
  - auto-approve only low-risk `task_proposal` records with workspace present,
    scope inside workspace, explicit evidence, no unsafe side effects, and no
    conflicting evidence
  - route high-risk or unclear proposals to `needs_authority` or
    `needs_clarification`
  - request harness evidence when policy requires proof before apply
- Apply approved task proposals into exactly one durable task.
- Create or update room messages/jobs for approved work where room context
  exists.
- Attach harness/test evidence back to decisions or apply results.
- Add a regression scenario proving the full loop:

  ```text
  hook signal -> task proposal -> coordinator decision -> durable task
  -> room/Pi-readable status -> harness evidence -> idempotent replay
  ```

Done when:

- Re-running the coordinator process does not create duplicate tasks.
- Apply without an approving decision fails.
- Rejected proposals cannot apply.
- High-blast-radius proposals do not auto-approve.
- Room loop downtime leaves durable state visible rather than losing work.
- Harness evidence is first-class in the decision/apply record.
- The full dogfood scenario is documented in the final report.

## Branching and PR Boundaries

Treat this as one integrated `/goal` objective with five review slices.

Recommended branches:

1. `feat/coordinator-control-plane-mvp`
2. `feat/hook-proposal-mode`
3. `feat/pi-coordinator-cockpit`
4. `feat/memory-candidate-governance`
5. `feat/room-harness-coordinator`

If using stacked branches, each branch should build on the previous one. If the
repo workflow prefers one feature branch, keep commits grouped so each PR slice
can be extracted cleanly.

After each PR milestone:

- run the narrow tests listed for that PR
- commit the milestone as a coherent change set
- record changed files, verification, and residual risks
- do not continue into the next PR if the current milestone has failing tests
  that are not understood and documented

## Verification

Run focused tests after each PR. Adjust package lists only when file placement
changes, and record any unrelated existing failures exactly.

### PR 1 checks

```bash
go test ./internal/context/contextplane
git diff --check
```

### PR 2 checks

```bash
go test ./skills/hooks_task_guard ./skills/hooks_dispatch ./internal/runtime/hooks
go test ./internal/context/contextplane
git diff --check
```

### PR 3 checks

```bash
go test ./internal/interfaces/web/... ./internal/context/contextplane
if [ -f integrations/pi/package.json ]; then npm --prefix integrations/pi test; fi
git diff --check
```

If the Pi package has no test script, run the repo's existing Pi typecheck or
document why none exists.

### PR 4 checks

```bash
go test ./internal/context/memorycore ./internal/context/contextengine ./internal/context/contextplane ./internal/storage/memory
go test ./skills/memory_curator_report
git diff --check
```

### PR 5 checks

```bash
go test ./internal/context/contextplane ./internal/storage/tasks ./internal/runtime/orchestration/roomruntime
go test ./cmd/foxctl/cmd -run 'Test.*Coordinator|Test.*Proposal|Test.*Task|Test.*Room|Test.*Harness'
bash tests/regression/run.sh
git diff --check
```

### Final checks

```bash
make build
go test ./internal/context/... ./internal/runtime/hooks ./internal/runtime/orchestration/roomruntime ./internal/storage/tasks ./internal/storage/memory
go test ./skills/hooks_task_guard ./skills/hooks_dispatch ./skills/memory_curator_report
make check-doc-links
./bin/foxctl index anchors lint --workspace . --summary
git diff --check
```

If a full check is too broad because of unrelated existing failures, record the
exact failure and run the narrowest relevant passing checks that prove the
changed behavior.

## Dogfood Scenario

Before final completion, run or document an end-to-end local dogfood:

1. Start the foxctl daemon/web service required for Pi and rooms.
2. Load the Pi extension with explicit workspace and tool allowlist.
3. Bind Pi to a room.
4. Trigger a write/tool event with no active task.
5. Confirm the hook records one `task_proposal`.
6. Run `foxctl coordinator process --workspace . --limit 20`.
7. Confirm one `CoordinatorDecision`, one `ApplyResult`, and one durable task.
8. Confirm the room timeline reports the task/proposal/decision.
9. Run the relevant harness/regression command and attach evidence.
10. Confirm Pi can show proposal, decision, task, harness result, and room
    status.
11. Trigger or seed a high-blast-radius proposal.
12. Confirm it enters `needs_authority` and Pi can persist a human override or
    rejection as a decision record.

## Stop Conditions

- Stop after 3 failed attempts at the same failing check and summarize the
  blocker.
- Stop before adding dependencies.
- Stop before changing public protocol or envelope fields not listed in this
  goal.
- Stop before broad schema rewrites outside the explicit control-plane tables.
- Stop before merging foxctl rooms and foxprox rooms into one store.
- Stop before introducing model-driven auto-apply without explicit schema,
  policy, evidence, and tests.
- Stop before allowing generated memory to become active or instruction-eligible
  without a governed promotion path.
- Stop if a proposed change would make human approval the default bottleneck
  for low-risk work.
- Stop if a proposed coordinator loop can run without visible budget, lease,
  dedupe, idempotency, pause, or kill controls.

## Final Self-Review

Before marking the goal complete, write a short review note covering:

- Which five PR slices were completed and what each changed.
- Which decision/authority records now gate state mutation.
- Which tests prove proposal dedupe and apply idempotency.
- Which tests prove generated memory remains evidence-only.
- Which dogfood steps were run through Pi/rooms/harnesses.
- Any remaining fragmented surfaces, especially existing `MemoryProposal`,
  curator proposals, room jobs, hook actions, foxprox rooms, and task history
  packets.
- Residual risks and confidence score.
