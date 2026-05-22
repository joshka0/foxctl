# Goal: Finish Outstanding Code Quality Cleanup

## Goal

Complete the outstanding items from
`docs/goals/code-quality-cleanup-results.md` without widening the cleanup into a
general rewrite. Work in small, reviewable slices that delete legacy
compatibility, strengthen type contracts, improve module depth, and keep tests
focused on real behavior.

The intended end state is a zero-tech-debt cleanup: one canonical contract for
each product/runtime concept, explicit type boundaries, no stale fallback paths,
no pass-through compatibility facades, no hidden failure swallowing, and no
tests that only preserve accidental implementation detail.

## Context

- Current tracker: `docs/goals/code-quality-cleanup-results.md`
- Audit backlog: `docs/goals/code-quality-cleanup-audit.md`
- Current cleanup branch: `feat/code-quality-cleanup`
- Merge request: `!43`
- Repo guidance: `AGENTS.md`, `CONTEXT.md`, `docs/glossary.md`,
  `docs/architecture/package-topology.md`
- Current `main` was merged into this branch on 2026-05-23. Full verification
  has been rerun after the merge.

Remaining areas from the results doc after the completed slices:

- Compatibility layers:
  - Keep similar GUI/data-client wrapper names separate unless the shared client
    owns the same route, params, response shape, and auth semantics.
- Dead frontend code:
  - Completed current zero-caller deletion pass for GUI API wrappers and tiny
    dead `@foxctl/data` orchestration exports.
  - Completed: added a real report-only dead-export/dependency graph pass with
    `bun run dead:frontend`.
  - Completed: hard-cut the broad `@foxctl/data/client` helper surface to
    active UI callers.
- Structural consolidation:
  - Completed: consolidate duplicated skill `inline_mode` parsing.
  - Completed: consolidate repeated repo-index skill workspace resolution
    helpers.
  - Completed: consolidate OpenAPI plugin stdio/envelope harness code.
  - Completed: consolidate duplicated chat adapter SSE activity decoding.
  - Deferred: keep chat command parsing adapter-local until a shared text
    command contract is explicit; do not merge Discord typed slash-command
    option handling with Teams/Telegram free-text parsing.
  - Completed: extract runtime-neutral Jido/goruntime reconciliation helpers
    for append/project and retry delay.
  - Deferred: keep payload builders and terminal event identity helpers
    adapter-local until their contracts are intentionally aligned.
- Tooling:
  - Completed: dependable unused-code and TypeScript frontend verification
    commands are wired locally and in GitLab CI:
    `bun run check:frontend` and `bun run unused:frontend`.
  - Completed: `gui-agent` ESLint React hook findings are clean, and package
    ESLint is mandatory in the root frontend gate.

## Quality Bar

Use these standards throughout:

- `code-quality` mode: `refactor`, `review-sprawl`, and `test-strategy`.
- `improve-codebase-architecture`: evaluate each candidate as a Module with an
  Interface and Implementation; prefer deeper Modules that improve Leverage and
  Locality. Use existing foxctl domain vocabulary from `CONTEXT.md`.
- `small-composable-code`: smallest coherent behavior-preserving slice; no
  speculative abstractions; side effects at the edge; explicit inputs and
  outputs.
- `zero-tech-debt`: search real callers before preserving compatibility; delete
  dead aliases/fallbacks instead of improving them.
- `thermo-nuclear-code-quality-review`: treat spaghetti condition growth,
  pass-through wrappers, weak type boundaries, broad optionality, and file
  sprawl as blockers unless justified.
- `ruthless-test-strategy`: tests must protect real behavior, contracts, state
  transitions, failure modes, or regression risks. Delete or rewrite tests that
  only assert implementation chatter.

## Constraints

- Preserve observable behavior unless the slice explicitly deletes a proven
  unused legacy path.
- Do not add dependencies without explicit approval.
- Do not introduce broad compatibility layers, fallback aliases, mode flags,
  catch-all error handling, or stringly payloads.
- Do not use keyword heuristics for routing, classification, promotion, or
  suppression behavior. Use typed signals, explicit fields, scored features, or
  tests.
- Before changing an affected area, use repo search and, where useful,
  `foxctl run code/dag_grep` or `./bin/foxctl run code/dag_grep` to inspect
  related callers and explanation subgraphs.
- Keep each slice reviewable. Prefer one coherent contract cleanup per commit.
- If a file approaches or crosses 1000 lines, pause and consider a deeper
  Module or better-owned seam before adding more code.
- If a suspected Module is shallow, apply the deletion test: if deleting it
  removes complexity rather than concentrating it behind a better seam, delete
  it.
- Keep public interfaces narrow. One Adapter means a hypothetical seam; two
  Adapters means a real seam.
- Do not rebase, force-push, or rewrite branch history unless explicitly
  directed. If `main` integration is required, do a normal non-destructive
  merge/rebase only with user approval or when the goal runner is explicitly
  assigned that integration step.
- Ignore unrelated untracked files and user changes.

## Milestones

### 1. Frontend DTO Facade Removal (Complete)

Remove the remaining `gui-agent` `@/api/types` compatibility facade.

Done when:

- Shared DTO imports point directly at `@foxctl/data/types`.
- GUI-only view models live in GUI-local modules with product/domain names.
- `packages/gui-agent/src/api/types.ts` no longer acts as a pass-through facade.
- `participantTransportKind` and similar helpers either live with GUI room UI
  logic or are replaced by explicit typed transport data.
- Verification passes:
  - `bun run --cwd packages/data typecheck`
  - `bun run --cwd packages/gui-agent build`

### 2. Orchestration Board Payload Contract (Complete)

Replace board/artifact shape sniffing with a typed
`OrchestrationBoardPayload` union and guards.

Done when:

- `packages/data` owns the union and guard functions.
- GUI and foxterm callers consume the union through explicit cases.
- Double casts around board/artifact payloads are removed.
- Tests or typechecks prove both board and artifact cases.
- Verification passes:
  - `bun run --cwd packages/data typecheck`
  - `bun run --cwd packages/gui-agent build`
  - `bun run --cwd packages/foxterm typecheck`

### 3. Frontend Typed Event Boundaries (Complete)

Remove frontend double-casts around Flow details, room timeline events, and
agent chat SSE.

Done when:

- Flow details use an explicit `FlowDetail` contract.
- Room timeline event input has a typed boundary or guard.
- Agent chat SSE uses an `SSEEnvelope<T>` style contract or equivalent typed
  stream guard.
- Tests or typechecks cover malformed/unknown boundary data where relevant.
- Verification passes:
  - `bun run --cwd packages/gui-agent build`

### 4. Backend Response Canonicalization (Complete)

Consolidate duplicated `AgentResponse` conversion and replace map-shaped daemon
fallback responses with typed structs.

Done when:

- Agent response construction has one canonical Module or helper at the
  appropriate interface seam.
- Patch/list/get paths do not hand-roll divergent response shapes.
- Daemon fallback responses use typed response structs rather than broad maps.
- Tests cover fields that previously drifted.
- Verification passes:
  - `go test ./internal/interfaces/web/api`

### 5. Room Transport Canonicalization (Mostly Complete)

Choose the final room transport contract and remove or isolate legacy room
transport compatibility.

Done when:

- `delivery_binding` plus explicit transport fields are the canonical contract,
  or a documented exception explains why not.
- Internal pane transport registration uses the canonical binding update helper;
  the legacy transport-only storage helper is deleted.
- Legacy `backend/session/pane_id` handling is deleted when caller evidence
  proves it is dead, or isolated at one boundary if still needed.
- GUI/foxterm callers use the canonical contract.
- Verification passes:
  - `go test ./internal/interfaces/web/api`
  - `bun run --cwd packages/gui-agent build`
  - `bun run --cwd packages/foxterm typecheck`

### 6. Hidden Fallback Honesty (Complete)

Make OpenCode hook and Hermes memory fallback failures explicit.

Done when:

- OpenCode hook skill failures preserve structured error context instead of
  presenting empty results.
- Hermes memory write/query fallback reports failure when both CLI and HTTP
  paths fail.
- Expected fallback cases remain intentional and tested.
- Verification passes:
  - focused tests for `configs/opencode-hooks/index.ts` if available, or a
    documented command/manual check if no test harness exists
  - focused tests or smoke checks for `integrations/hermes/client.py`

### 7. Dead Frontend Code And Unused Tooling (Mostly Complete)

Delete confirmed unused frontend code and add dependable unused-code workflow
coverage.

Done when:

- Each deletion has caller evidence from repo search, TypeScript diagnostics,
  dependency tooling, or import graph checks.
- Dead slices listed in `code-quality-cleanup-results.md` are deleted or marked
  as live with evidence.
- Completed in current slices: the unused GUI-local
  `getOrchestrationBoardCard` wrapper was deleted, and the orchestration board
  store moved to canonical `@foxctl/data/client` wrappers after adding
  `archived_only` support there.
- Completed in current slices: confirmed zero-caller GUI API wrappers were
  deleted for orchestration cleanup, mailbox, blackboard, room mutation,
  console-session settings/session/message, persisted-session get, health, and
  flow status/log calls. Tiny dead `@foxctl/data` orchestration exports were
  also removed.
- Completed in current slices: `bun run dead:frontend` now provides a
  TypeScript-graph report over active frontend packages without new
  dependencies; `bun run unused:frontend` runs it after compiler/linter gates.
- Completed in current slices: unused GUI-local API response/helper types were
  made package-private, internal orchestration board guards were unexported, and
  the obsolete `APIResponse<T>` type was deleted.
- Unused-code tooling is documented and wired so future cleanup can be repeated
  with `bun run unused:frontend`.
- Completed in current slices: re-ran the completion audit after the current
  main-branch sync; no targeted frontend dead-export findings remain.
- Verification passes:
  - `bun run --cwd packages/gui-agent build`
  - `bun run --cwd packages/foxterm typecheck`
  - `bun run dead:frontend`
  - `bun run unused:frontend`

### 8. Structural Consolidation Follow-Ups

Deepen repeated helper and orchestration Modules without creating generic
frameworks.

Done when:

- Completed: duplicated skill `inline_mode` parsing and repo-index workspace
  resolution helpers are consolidated only where the same rule is duplicated.
- Completed: OpenAPI plugin stdio/envelope harness code is consolidated
  without changing protocol validation behavior or hiding command-specific
  logic.
- Completed: chat adapter SSE activity decoding shares one canonical helper,
  with adapter-local error handling preserved.
- Deferred: chat command parsing remains adapter-local unless a shared MVP text
  command contract is made explicit.
- Completed: Jido/goruntime append/project and retry delay helpers are
  extracted into the runtime/orchestration Module before any broad architecture
  move.
- Deferred: payload builders and terminal event identity helpers remain
  adapter-local because their current contracts differ.
- Verification passes for each affected package.

### 9. Residual Typed Wrappers And Weak Casts (Complete)

Clean up remaining fixed response maps and frontend casts that were outside the
completed DTO/SSE slices.

Done when:

- Room-control fixed response wrappers use named DTOs where the JSON shape is
  fixed. Completed for status, control-snapshot, inbox, tasks, loop,
  coordinator handoff, message action, and task action responses. The compact
  inbox room helper now returns a typed summary instead of a map-shaped JSON
  blob.
- Verification passed:
  - `go test ./internal/domain/agent`
  - `go test ./internal/interfaces/web/api -run 'TestRoomDetailHandler_(GetStatusReturnsCoordinatorSummary|GetStatusReturnsPersistedLoopState|GetInboxReturnsActorScopedEntries|CoordinatorSetTransfersRole|GetLoopReturnsPersistedState|PatchLoopRequiresCoordinator|PatchLoopAllowsLocalDevSuperuser|PatchLoopPersistsPolicy|PatchLoopPublishesRoomLoopUpdatedEvent|MessageAckUpdatesStatus|GetControlSnapshotIncludesLoopHealthAndLinkedCards|GetTasksReturnsRoomLinkedTasks|TaskClaimActionUpdatesTask|PostTaskUsesExplicitMilestoneSelection|PostTaskRejectsUnknownMilestoneSelection)$'`
  - `go test ./internal/interfaces/web/api`

## Verification

Run narrow verification after each milestone, then run the broad set before the
final response or before asking to merge.

Narrow verification examples:

- `go test ./internal/interfaces/web/api`
- `go test ./internal/rlm/...`
- `bun run --cwd packages/data typecheck`
- `bun run --cwd packages/gui-agent build`
- `bun run --cwd packages/foxterm typecheck`
- `make check-doc-links` when markdown changes
- `git diff --check`

Full verification before final merge:

- `make test`
- `make lint`
- `make check`
- `bun run --cwd packages/data typecheck`
- `bun run --cwd packages/gui-agent build`
- `bun run --cwd packages/foxterm typecheck`
- `make check-doc-links`
- Any documented unused-code tooling added by this goal

If full verification is too expensive or blocked, run all available targeted
checks and report the exact skipped command, blocker, and residual risk.

## Done When

- Every milestone is complete, or any intentionally deferred item is recorded in
  `docs/goals/code-quality-cleanup-results.md` with a concrete reason.
- Remaining compatibility layers are deleted or isolated at one explicit
  interface seam with caller evidence.
- Weak type boundaries listed above are replaced by explicit contracts.
- Hidden fallback paths either return/report useful errors or have documented,
  tested recovery semantics.
- No new broad wrappers, stale comments, "temporary" branches, fake
  completeness, or compatibility shims were introduced.
- All changed code passes the relevant narrow checks.
- Full verification has run, or any skipped command is documented with a
  blocker and risk.
- Final self-review lists:
  - changed files grouped by milestone
  - verification results
  - deleted code and why it was safe
  - residual risks
  - confidence score

## Stop Conditions

- Stop after 3 failed attempts at the same verification failure and summarize
  the blocker.
- Stop before adding dependencies.
- Stop before changing schemas, public API routes, protocol/envelope shape, or
  branch history unless the change is explicitly part of a milestone and has
  verification.
- Stop if caller evidence contradicts a planned deletion.
- Stop if a milestone wants to expand into unrelated cleanup.
- Stop if `main` integration creates conflicts that require product or
  architecture decisions beyond this goal.

## Start Command

```text
/goal docs/goals/code-quality-cleanup-outstanding_goal.md
```
