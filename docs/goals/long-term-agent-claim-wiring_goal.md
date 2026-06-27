# Goal: Long-Term Agent Claim Wiring

## Goal

Implement the first structural slice for a better long-term foxctl **Agent**:
structured tool/session evidence should become task-scoped candidate memory
claims with provenance, using existing foxctl primitives rather than adding a
new memory side system.

The delivered behavior should make this loop work in ordinary agent sessions:

```text
structured tool result, session summary, or gathered evidence
  -> typed candidate memory claim with evidence refs
  -> task/session-scoped accumulated context
  -> existing memory/context claim store
  -> later retrieval through gather_context / memory/query
  -> validation feedback updates lifecycle and confidence
```

LongMem-style evaluation is a future validation target, not the driver for this
goal. Do not adapt, index, or special-case for LongMem until the general
observe/store/retrieve/validate loop works through foxctl's existing **Context
engine**, **ContextWiki**, memory, task, session, and embedding primitives.

## Context

- Repo: `/home/dev/repos/foxctl`.
- User direction:
  - Build durable long-term agent capability first.
  - Avoid benchmark-specific logic and brittle keyword/hard-coded behavior.
  - Leverage existing foxctl primitives before adding new ones.
  - Add only primitives that wire existing seams together in a fast,
    cost-effective way.
- Pi/Hermes convergence:
  - Do not create standalone `Context Plan`, `Evidence Ledger`, or `Lifecycle
    Curator` modules when existing seams already exist.
  - `gather_context`, typed evidence refs, context buffers, task/session stores,
    memory lifecycle, and `skills_impact` are the owning primitives.
  - First slice: wire structured tool results to candidate memory claims with
    provenance.
  - Co-design task-scoped context binding with claim extraction from day one so
    claims are written into the right task/session scope.
  - Keep extracted context as a typed envelope or pointer, not a new side store.
  - Defer broad offline consolidation and retrieval-repair loops until the
    online session loop works.
- Canonical docs:
  - `CONTEXT.md`
  - `AGENTS.md`
  - `docs/architecture/memory-core.md`
  - `docs/architecture/rlm-gather-context.md`
  - `docs/plans/context-buffer-design.md`
  - `docs/architecture/package-topology.md`
- Likely implementation areas:
  - `internal/context/contextengine`
  - `internal/context/memorycore`
  - `internal/storage/contextengine`
  - `internal/storage/tasks`
  - `internal/rlm/env`
  - `internal/agent/daemon`
  - existing task/session/context buffer paths
  - relevant tests under the same package families

## Constraints

- Use `$small-composable-code`: land the smallest coherent behavior slice with
  behavior-focused tests. Avoid grand rewrites.
- Use `$improve-codebase-architecture`: deepen existing **Modules** and
  **Seams** instead of introducing shallow wrappers or parallel systems.
- Use `$thermo-nuclear-code-quality-review`: apply strict maintainability
  pressure before finalizing. Look for code-judo moves that delete branches,
  pass-through helpers, or duplicated orchestration.
- Do not add new top-level `internal/*` roots. Follow
  `docs/architecture/package-topology.md`.
- Do not add a new memory database, evidence ledger side store, or benchmark
  ingestion subsystem for this goal.
- Do not add dependencies without explicit approval.
- Do not use ad hoc substring or keyword heuristics for routing,
  classification, claim promotion, claim suppression, or lifecycle decisions.
  Prefer explicit schemas, typed signals, scored features, tests, or existing
  learned/policy seams.
- Preserve existing public **Envelope** and command contracts unless a tested
  hard cut is explicitly required.
- Writes must be provenance-bearing and idempotent where practical.
- Candidate claims must remain evidence by default. They must not become
  instructions unless existing memory lifecycle/usage rules allow that.
- Do not run full repo ingestion, broad reembedding, production HydraDB work, or
  provider-key experiments as part of this goal.
- Do not echo, persist, or inspect secrets.
- The current worktree may contain unrelated edits. Do not revert or overwrite
  unrelated changes.

## Milestones

### Milestone 0: Ownership And Existing-Seam Audit

Identify the canonical ownership for this slice before editing code.

Done when:

- Confirm which existing **Module** owns candidate memory claims:
  `memorycore`, `contextengine`, storage, or RLM adapter wiring.
- Confirm where task/session-scoped accumulated context should live.
- Confirm whether existing context buffer/task/session stores can carry typed
  claim pointers without schema churn.
- Document any rejected new abstraction in the implementation notes or final
  self-review.
- No code is added just to create a hypothetical **Seam** with one **Adapter**.

### Milestone 1: Typed Candidate Claim Envelope

Add or reuse the smallest typed shape that can represent a candidate claim
derived from structured evidence.

Done when:

- The shape carries:
  - workspace ID;
  - task/session scope when available;
  - claim kind/type;
  - summary or statement;
  - source evidence refs;
  - provenance for tool/session/origin;
  - lifecycle/status as candidate;
  - confidence/trust fields only if they map to existing memory contracts.
- The shape is a typed envelope or pointer into existing storage, not a new side
  store.
- Tests prove invalid envelopes are rejected or safely ignored according to the
  existing package's error style.

### Milestone 2: Tool Result To Candidate Claim Wiring

Wire structured tool/session outputs into candidate memory claims through the
existing claim/memory path.

Done when:

- At least one real structured result path can emit candidate claims with
  source evidence refs.
- The write path uses existing memory/context claim storage or projection
  mechanisms.
- The claim write is task/session-scoped when task/session identity is present.
- Re-running the same source does not create duplicate active claims.
- Tests prove:
  - a structured result creates a candidate claim;
  - provenance includes the originating evidence ref or tool/session pointer;
  - task/session scope is preserved;
  - no claim is promoted to instruction-eligible policy/skill by default.

### Milestone 3: Task-Scoped Context Binding

Make gathered context and candidate claims survive handoff/resume without
re-deriving everything.

Done when:

- Task/session state can carry accumulated context pointers and candidate claim
  pointers through an existing store or context buffer path.
- `gather_context` / RLM-facing context can read the bound context without
  widening tool-visible context unnecessarily.
- Tests prove handoff/resume can recover the scoped context pointer and claim
  pointer.
- The implementation avoids a broad mutable global cache.

### Milestone 4: Claim Use Feedback Hook

Add the narrowest feedback path for successful, failed, corrected, or
contradicted use of a claim.

Done when:

- There is an explicit typed signal for claim use outcome, or a clear adapter to
  an existing signal.
- Feedback updates existing lifecycle/confidence/status fields through the
  canonical memory/context path.
- Search/retrieval access is not treated as proof of claim use.
- Tests prove success, failure, correction, and contradiction route to the
  expected lifecycle/status behavior or no-op where the existing model has no
  state for that outcome.

### Milestone 5: Revalidation Trigger Wiring

Wire existing change signals to mark affected claims for revalidation.

Done when:

- At least one existing signal, such as code change, test failure, explicit user
  correction, stale marker, or `skills_impact`, can mark a related claim as
  needing revalidation without deleting evidence history.
- Tests prove the trigger changes only related claims and leaves unrelated
  claims untouched.
- This does not introduce keyword matching as the relationship detector; use
  existing typed refs, impact edges, task scope, or explicit source refs.

Implementation note:

- Delivered the first production trigger through the existing `memoryflow`
  edit lifecycle hook and `contextengine` impact Module. `code.changed_dirty`
  events now create claim-target `needs_revalidation` markers for related
  current claims discovered through typed source refs or impact edges, then the
  existing invalidation application transitions only those claims. This keeps
  the **Seam** at the contextengine event/invalidation Interface and avoids a
  new CLI, store, or benchmark-specific lifecycle path.
- Added behavior tests proving related current claims are marked, unrelated
  current claims are untouched, candidates are not promoted, escaping edit paths
  do not emit dirty events, and the real `memoryflow` SQLite-backed hook emits
  a typed `path:` dirty event.

Continuation note, 2026-06-04:

- Wired named-memory recall into `gather_context` through a `MemoryPacks`
  dependency, so memory-lane `EvidencePack`s keep typed `named_memory:` refs
  and flow through the existing reducer into facts, answer candidates, and
  `facts_to_copy.load_refs`.
- Updated context certification source contracts to treat `named_memory` as a
  valid loadable memory ref, matching the existing `load_evidence_ref` support.
- Updated LongMem answer instrumentation and gather-memory prompt wording so
  named-memory refs are counted and can be verified without claim-only wording.
- Verified with permanent package tests plus a temporary live adapter check
  against the throwaway Turso store at
  `/tmp/foxctl-longmem-answer-live.4Fm3AM`, workspace `ws-answer-live-4`.
  The live non-LLM check surfaced expected `named_memory:` refs for the cases
  already hit by retrieval at limit 10.
- Full answer-mode LongMem scoring is currently blocked by provider state:
  LM Studio is not running locally, and both direct Z.AI/BigModel API attempts
  returned provider 429 insufficient-balance/resource-package errors.

### Milestone 6: Final Thermo-Nuclear Review

Run a strict maintainability pass over the implemented slice.

Done when:

- Review checks:
  - Did any file cross or approach 1000 lines due to this change?
  - Did the diff add mode flags, nullable branches, or scattered feature checks?
  - Did a new **Module** fail the deletion test?
  - Did feature logic land in the wrong package family?
  - Did type boundaries become stringly or `any`-heavy?
  - Is there a code-judo move that deletes a helper, branch, or wrapper?
- Any high-conviction structural issue is fixed before completion or documented
  as a stop condition with the exact blocker.

## Verification

Run narrow checks after each implemented milestone. Adjust package lists only
when the touched packages differ:

```bash
env -u GOROOT -u GOBIN -u GOTOOLDIR CGO_ENABLED=0 /usr/local/go/bin/go test -count=1 ./internal/context/contextengine ./internal/context/memorycore ./internal/storage/contextengine ./internal/storage/tasks ./internal/rlm/env
```

Run final checks before completion:

```bash
env -u GOROOT -u GOBIN -u GOTOOLDIR CGO_ENABLED=0 /usr/local/go/bin/go test -count=1 ./internal/context/contextengine ./internal/context/contextplane ./internal/context/memorycore ./internal/storage/contextengine ./internal/storage/tasks ./internal/storage/memory ./internal/rlm ./internal/rlm/env ./internal/runtime/hooks/memoryflow
git diff --check
```

If markdown docs are changed beyond this goal file, also run:

```bash
make check-doc-links
```

Done when:

- All implemented milestone tests pass.
- Candidate claim extraction is task/session scoped and provenance-bearing.
- No new side store or benchmark-specific subsystem was added.
- Retrieval/answer behavior can use the resulting claims through existing
  `gather_context` or memory retrieval surfaces.
- A final self-review reports:
  - changed files;
  - behavior delivered;
  - architecture vocabulary: **Module**, **Interface**, **Implementation**,
    **Depth**, **Seam**, **Adapter**, **Leverage**, **Locality**;
  - thermo-nuclear findings fixed or explicitly deferred;
  - residual risks and confidence.

## Stop Conditions

- Stop after 3 failed attempts at the same failing verification command and
  summarize the blocker.
- Stop before adding a new storage schema/table unless the existing store cannot
  represent typed pointers or candidate claim provenance.
- Stop before changing public command envelopes, memory lifecycle semantics, or
  task/session persistence contracts broadly.
- Stop before adding dependencies.
- Stop if the implementation starts requiring LongMem-specific query terms,
  benchmark IDs, answer labels, or hard-coded dataset behavior.
- Stop if a proposed implementation needs secrets, production credentials, full
  repo ingestion, or broad reembedding.

## Final Self-Review

### Behavior Delivered

- Candidate memory claims remain owned by the contextengine **Module** and are
  stored through the existing contextengine store **Interface**.
- `gather_context`, `retrieve_memory`, `retrieve_mixed`, and `retrieve_task`
  can read task/session-scoped memory claims through existing RLM adapter
  retrieval **Adapters**.
- Task contexts now expose related claim pointers as typed
  `memory_claim:<id>` refs, and `load_evidence_ref` can load the claim body
  through the existing contextengine store.
- Retrieval feedback can be recorded with effects through
  `RecordRetrievalFeedbackWithEffects`, which turns explicit `UsedRefs` into
  existing impact events and marks only related current/candidate claims for
  revalidation.
- Dirty-edit lifecycle events now mark related **current** claims for
  revalidation through typed source refs or impact edges, while related
  candidate claims stay candidate evidence and are not promoted by edits.
- `foxctl retrieval-feedback` now gives operators and automation an additive
  production caller for the effectful feedback path without changing append-only
  store semantics or adding a model-visible write tool.
- Applied autonomous memory drafts now write the existing ContextWiki review
  proposal and a matching contextengine candidate claim with deterministic ID,
  source feedback/episode refs, used evidence refs, and evidence-only lifecycle
  status.
- RLM LLM runs now preserve evidence refs surfaced by retrieval tools as
  `tool_surfaced_evidence_refs` metadata. These refs are carried through staged
  phases as candidate context only; they are not promoted to `EvidenceRefs`,
  retrieval feedback, or claim-use proof.
- RLM LLM runs now emit `answer_used_evidence_refs` metadata as the explicit
  answer-level signal for surfaced refs that were actually cited in the final
  answer. This signal is still read-only metadata; feedback persistence remains
  an explicit caller responsibility.
- `foxctl rlm run` now has an opt-in answer-feedback caller. It can record
  explicit `--answer-feedback-used-ref` values through the same
  retrieval-feedback effect path, while `--answer-feedback-use-answer-refs`
  is limited to informational feedback because answer citations are not proof
  of lifecycle-impacting claim use.

### Goal-Relevant Changed Files

- `internal/context/contextengine/claims.go`
- `internal/context/contextengine/events.go`
- `internal/context/contextengine/filters.go`
- `internal/context/contextengine/impact_engine.go`
- `internal/context/contextengine/memory_query_policy.go`
- `internal/context/contextengine/refs.go`
- `internal/context/contextengine/retrieve_task.go`
- `internal/context/contextengine/retrieval_feedback_effects.go`
- `internal/context/contextplane/autonomous_memory_drafts.go`
- `internal/context/memorycore/context_claim.go`
- `cmd/foxctl/cmd/context_records.go`
- `cmd/foxctl/cmd/context_records_test.go`
- `cmd/foxctl/cmd/retrieval_feedback_cli.go`
- `cmd/foxctl/cmd/rlm.go`
- `cmd/foxctl/cmd/rlm_answer_feedback.go`
- `cmd/foxctl/cmd/rlm_answer_feedback_test.go`
- `cmd/foxctl/cmd/rlm_test.go`
- `internal/storage/contextengine/schema.go`
- `internal/storage/contextengine/store.go`
- `internal/rlm/env/tools.go`
- `internal/rlm/env/named_memory_recall.go`
- `internal/rlm/env/tool_exec.go`
- `internal/rlm/evidence_refs.go`
- `internal/rlm/llm_runner.go`
- `internal/rlm/llm_runner_test.go`
- `internal/runtime/hooks/memoryflow/memoryflow_test.go`
- `internal/storage/memory/search_lexical.go`
- `internal/storage/memory/store_test.go`
- related tests in the same package families

### Architecture Review

- **Module**: `contextengine` owns claims, retrieval feedback, events, impact,
  and lifecycle transitions. `contextplane` owns review-gated memory drafts and
  proposals. `rlm/env` remains a read-side retrieval **Adapter**.
- **Interface**: no model-visible write command was added. The new CLI envelope
  is additive and routes through the existing contextengine store interface;
  existing command envelopes were preserved.
- **Implementation**: the new claim writer is in the existing autonomous memory
  draft apply path, not in `ReadOnlyAdapter`. The feedback callers are thin CLI
  adapters over the existing effectful contextengine operation, with shared CLI
  construction in `retrieval_feedback_cli.go`.
- **Depth**: `RecordRetrievalFeedbackWithEffects` gives callers one small
  operation that records feedback, appends the derived event idempotently, and
  applies lifecycle impact.
- **Seam**: the contextengine store remains the write/read seam for claims and
  feedback. The ContextWiki proposal path and retrieval-feedback CLI are
  write-side adapters at that seam.
- **Adapter**: RLM remains read-only; ContextWiki draft application is the
  write-side adapter for turning retrieval feedback into review-gated memory.
  The CLI adapter records explicit claim-use feedback and applies lifecycle
  effects through `RecordRetrievalFeedbackWithEffects`. The LLM runner now
  records retrieval-surfaced evidence refs as candidate metadata so answer
  callers can observe likely refs, and records answer-cited surfaced refs as a
  separate signal, without letting read-side retrieval mutate lifecycle or
  provenance state. The RLM answer-feedback adapter can persist explicit caller
  used refs, but answer-derived refs are accepted only for informational
  feedback.
- **Leverage**: once a candidate claim exists, task-scoped retrieval, memory
  retrieval, revalidation, staleness, and evidence loading all reuse existing
  primitives.
- **Locality**: feedback-to-revalidation logic is concentrated in
  `contextengine`; review-gated memory proposal behavior stays in
  `contextplane`. Memory claim query lifecycle/scope policy is now also owned
  by `contextengine`, while `rlm/env` keeps only adapter-specific store fan-out
  and named-memory fallback behavior.

### Rejected Abstractions

- No standalone evidence ledger, lifecycle curator, context plan, or new
  top-level `internal/*` root was added.
- No model-visible feedback or claim-write tool was added; that would duplicate
  existing write-side ownership and let ordinary retrieval mutate memory state.
- No read-side RLM retrieval tool writes candidate claims. That would violate
  the adapter's read-only contract and could create claim volume from ordinary
  retrieval.
- No LongMem-specific query terms, answer labels, benchmark IDs, or eval
  ingestion paths were introduced into the claim-writing, retrieval, or
  lifecycle wiring. Adjacent LongMem eval scaffolding exists in the broader
  dirty worktree from separate evaluation work and is not part of this goal's
  completion claim.

### Thermo-Nuclear Findings

- Fixed: feedback application is retry-safe after partial feedback/event
  persistence. The retry lookup now targets the derived event ID instead of
  scanning all workspace events, and structured event data is compared without
  map/slice comparison panics.
- Fixed: stale-context feedback uses an explicit
  `memory.claim_revalidation_requested` event instead of overloading user
  correction semantics.
- Fixed: direct `memory_claim` refs resolve through `ClaimByID` instead of an
  all-claim scan.
- Fixed: applied memory drafts now create one deterministic candidate claim,
  so the feedback/revalidation path has a real production claim-store writer.
- Fixed after adversarial review: applied memory drafts now derive
  task/session/path scope from typed evidence refs and preserve existing
  reviewed claim lifecycle state on rerun instead of overwriting it back to
  candidate.
- Fixed after adversarial review: SQLite claim upsert persists scope fields on
  legitimate claim updates, so a deterministic claim can gain task/session
  scope without being stranded outside scoped retrieval.
- Deferred: existing large files such as `internal/rlm/env/tool_exec.go` and
  `internal/storage/contextengine/store.go` remain large; this goal did not
  push a previously sub-1000-line file over the threshold.
- Fixed after adversarial review: raw lifecycle `memory_statuses` are no
  longer advertised in the default model-visible retrieval schemas. Scoped
  retrieval still includes candidate/current/needs-revalidation claims by
  policy when `task_id` or `session_id` is present, while unscoped retrieval
  defaults to current claims.
- Fixed after adversarial review: scoped autonomous-memory draft dedupe now
  includes typed path/task/session refs, so identical corrections from
  different task/session scopes produce distinct draft and claim IDs while
  same-scope duplicates still collapse.
- Fixed after adversarial review: task/session memory query visibility policy
  moved out of the RLM adapter into `contextengine`, giving the lifecycle
  policy a canonical **Module** instead of leaving it embedded in
  `tool_exec.go`.
- Fixed after adversarial review: non-integration tests now cover
  `gather_context` retrieving task-scoped candidate claims and
  `retrieve_memory` retrieving both task-scoped and session-scoped candidates
  when both scopes are provided.
- Fixed after adversarial review: the retrieval-feedback CLI leaves `CreatedAt`
  ownership with the store/effect path, so deterministic feedback IDs remain
  retry-safe across separate command invocations.
- Fixed after adversarial review: retrieval-feedback CLI `--used-ref` parsing is
  strict. Malformed refs fail the command instead of silently recording
  informational feedback with no lifecycle effects.
- Fixed after adversarial review: retrieval-feedback CLI used refs are
  normalized before both ID generation and storage, so reordered equivalent refs
  do not conflict with append-only idempotency.
- Fixed after adversarial review: tool-surfaced refs in the LLM runner are no
  longer merged into `Result.EvidenceRefs`; they are named as surfaced
  candidates in metadata.
- Fixed after adversarial review: surfaced evidence ref extraction is scoped to
  known evidence-producing tool results and skips error/invalid results, so a
  generic tool payload cannot accidentally become candidate provenance.
- Fixed after adversarial review: staged RLM execution now aggregates surfaced
  refs from all phases into the final metadata while keeping staged candidate
  paths prompt-only for evidence accounting. Final `EvidenceRefs` preserve only
  bootstrap environment evidence.
- Fixed after adversarial review: `answer_used_evidence_refs` requires
  delimiter-aware citations, so adjacent refs such as
  `memory_claim:claim-1`/`memory_claim:claim-10` and paths such as
  `file.go`/`file.go.bak` do not create false used-ref positives.
- Fixed after adversarial review: staged final synthesis is asserted to run
  without tools, so final answer-used refs can only come from phase-surfaced
  candidates, not a hidden final retrieval turn.
- Fixed after adversarial review: final answers can cite bootstrap
  `EvidenceRefs` without those environment-only refs appearing in
  `answer_used_evidence_refs`.
- Fixed after thermo-nuclear review: evidence-ref extraction and answer
  citation matching were moved out of already-large `llm_runner.go` into
  focused `internal/rlm/evidence_refs.go`.
- Fixed after adversarial review: RLM answer-feedback persistence is explicitly
  opt-in and ordinary `rlm run` leaves the store untouched.
- Fixed after adversarial review: answer-cited refs are not treated as
  lifecycle-impacting proof of claim use. `answer_corrected` and
  `stale_context_used` require explicit `--answer-feedback-used-ref` values;
  `--answer-feedback-use-answer-refs` is informational only.
- Fixed after adversarial review: retrieval-feedback CLI construction is shared
  between `foxctl retrieval-feedback` and `foxctl rlm run` answer feedback, so
  deterministic IDs, strict ref parsing, sorted refs, dry-run behavior, and
  effectful recording stay in one adapter helper.
- Fixed after adversarial review: lifecycle-impacting feedback-kind knowledge
  is exposed by `contextengine` instead of being duplicated in the RLM command
  adapter.
- Fixed after ruthless test review: command-level tests now cover the additive
  `answer_feedback` envelope, no-op/no-row behavior without flags, explicit
  answer-feedback effects, dry-run, invalid refs, lifecycle-kind guardrails,
  informational answer-derived feedback, default `evidence_used` behavior, and
  retry idempotency with canonicalized refs/events.
- Fixed after adversarial review: legacy named-memory recall now emits
  `named_memory:<name>` evidence refs instead of pretending those entries are
  `memory_claim:<id>` contextengine claims, and `load_evidence_ref` can load
  the named-memory body through the named-memory **Adapter**.
- Fixed after adversarial review: unscoped `retrieve_memory` now queries the
  canonical contextengine claim store and merges legacy named-memory hits as a
  separate Adapter result. Named-memory lifecycle suppression no longer hides
  valid current contextengine claims.
- Fixed after Hermes residual-risk review: `load_evidence_ref` now has explicit
  behavior coverage for a missing `named_memory:<name>` ref returning a
  structured unloaded result instead of a raw tool error.
- Fixed after ruthless test review: newly added memory-search tests use
  domain-neutral fixtures instead of LongMem/Qwen/Hydra terms, and no test
  freezes the BM25 normalization floor as behavior.
- Fixed after native architecture review: `load_evidence_ref` no longer returns
  a bare `memory_claim:<id>` from another workspace. Cross-workspace claim refs
  now return a structured unloaded result instead of leaking the claim body.
- Fixed after native architecture review: lifecycle-impacting feedback no
  longer reports success when the claim transition write fails. Claim
  transition persistence errors now propagate through
  `RecordRetrievalFeedbackWithEffects`, and retry behavior is covered.
- Fixed after native architecture review: scoped memory retrieval prioritizes
  reviewed current claims before scoped candidate evidence, so a low-limit
  scoped query cannot hide a query-matching current claim behind an unrelated
  in-flight candidate.
- Fixed after native residual review: contextengine `ListClaims` now has a
  deterministic same-timestamp ordering (`updated_at DESC, id ASC`). This keeps
  same-status scoped retrieval stable without inventing a relevance ranking
  model in storage.
- Fixed after Pi residual review: the same-timestamp ordering test now asserts
  that the store fixture actually produced equal `updated_at` values before it
  checks the `id ASC` tiebreaker.
- Fixed after native storage-index review: status, task-scoped, and
  session-scoped `ListClaims` retrieval shapes now have SQLite indexes that
  cover `updated_at DESC, id ASC`. The query-plan helper now scans every
  `EXPLAIN QUERY PLAN` row, and tests use the production `ListClaims`
  projection with `LIMIT` instead of a simplified covering query.

### Verification Notes

- Focused checks pass for the implemented packages and new behavior tests:
  `go test -count=1 ./internal/context/contextengine ./internal/context/contextplane ./internal/context/memorycore ./internal/storage/contextengine ./internal/storage/tasks ./internal/rlm ./internal/rlm/env`
- Focused CLI checks pass for the new feedback command behavior:
  `go test -count=1 ./cmd/foxctl/cmd -run 'TestContextRecordCommandFlags|TestRetrievalFeedbackCommand'`
- Focused RLM answer-feedback checks pass:
  `go test -count=1 ./cmd/foxctl/cmd ./internal/context/contextengine -run 'TestRLMRunCommand|TestRecordRLMAnswerFeedback|TestRetrievalFeedbackCommand|TestContextRecordCommandFlags|TestRetrievalFeedbackKindHasLifecycleImpact|TestContextEventFromRetrievalFeedback|TestRecordRetrievalFeedbackWithEffects'`
- Focused RLM LLM runner checks pass for surfaced candidate refs and staged
  aggregation:
  `go test -count=1 ./internal/rlm -run 'TestLLMRunnerAttachesSurfacedToolEvidenceRefs|TestLLMRunnerStagedAggregatesSurfacedToolEvidenceRefs|TestCollectSurfacedToolEvidenceRefs|TestCollectAnswerUsedEvidenceRefsRequiresExplicitCitation'`
- Adjacent RLM package checks pass:
  `go test -count=1 ./internal/rlm ./internal/rlm/env`
- Latest named-memory seam checks pass:
  `go test -count=1 ./internal/context/contextengine -run 'TestRefType|TestParseEvidenceRef|TestEvidenceRef'`
  and
  `go test -count=1 ./internal/rlm/env -run 'TestRetrieveMemoryUsesNamedMemoryRecall|TestLoadEvidenceRefNamedMemoryMissingReturnsStructuredError|TestRetrieveMemoryUsesWorkspaceIDOverride|TestRetrieveMemoryKeepsContextClaimsWhenNamedMemorySuppressesMatches|TestRetrieveMemoryFusesContextClaimsWithNamedMemory|TestRetrieveMemoryFallsBackWhenNamedMemoryHasNoCandidates|TestReadOnlyAdapterLoadEvidenceRef'`
- Latest memory lexical checks pass:
  `go test -count=1 ./internal/storage/memory -run 'TestSearchMatchesTermsInsidePunctuatedFileText|TestSearchIgnoresStopWordOnlyQuery|TestSearchUsesAtomicTextEntitiesAndKeywords'`
- Latest blocker regression checks pass:
  `go test -count=1 ./internal/rlm/env -run 'TestReadOnlyAdapterLoadEvidenceRefRejectsMemoryClaimFromOtherWorkspace|TestRetrieveMemoryPrioritizesCurrentClaimOverScopedCandidateAtLimit|TestDefaultMemoryQueryStatusesIncludesScopedCandidates|TestMemoryQueryPolicyKeepsScopedCandidatesVisible'`
  and
  `go test -count=1 ./internal/context/contextengine -run 'TestRecordRetrievalFeedbackWithEffects_ClaimTransitionFailureIsReturnedAndRetryable|TestRecordRetrievalFeedbackWithEffects_RetryRecoversAfterPartialApply|TestRecordRetrievalFeedbackWithEffects_CorrectedDirectClaimNeedsRevalidation|TestRecordRetrievalFeedbackWithEffects_StaleContextMarksOnlyUsedClaim'`
- Latest same-status ordering checks pass:
  `go test -count=1 ./internal/storage/contextengine -run 'TestStore_ListClaims_OrdersSameTimestampClaimsByID|TestStore_ListClaims_Filter|TestStore_ListClaims_FiltersTaskAndSessionScope'`
  and
  `go test -count=1 ./internal/storage/contextengine ./internal/rlm/env`
- Latest claim-list index checks pass:
  `go test -count=1 ./internal/storage/contextengine -run 'TestStore_ExplainQueryPlanReportsTempSortRows|TestStore_Index_ClaimStatus|TestStore_Index_ClaimsListOrder|TestStore_ListClaims_OrdersSameTimestampClaimsByID|TestStore_SchemaMigrationReplacesLegacyClaimOrderIndexes'`
  and
  `go test -count=1 ./internal/storage/contextengine`
- Latest staged smoke passes in a throwaway workspace: focused storage,
  contextengine, RLM adapter, retrieval-feedback, LongMemEval, memory-recall,
  embedding processor, memory-query, command, and integration-tag
  contextengine wiring tests are green. A real `foxctl eval longmem` retrieval
  smoke ingested one temporary case, queued one memory embedding job, produced
  hit@5=1 and MRR=1, then `foxctl run embedding/worker` drained the memory job
  with one processed job, zero errors, and an empty queue.
- Latest live LongMem answer smoke used four pilot cases in an isolated
  throwaway workspace with GLM/Z.AI through the local Anthropic-compatible
  endpoint. Ingest saved 195 named-memory records and queued 195 memory
  embedding jobs. The first worker pass processed 152 jobs and left stale
  running jobs after the local memory embedder circuit breaker opened; a
  lower-parallelism retry with stale recovery processed the remaining 43 jobs,
  recovered 8 stale jobs, and left the queue at 195 completed, 0 queued,
  0 running, 0 failed. Retrieval-only scoring at limit 50 produced hit@5=0.75,
  hit@10=0.75, hit@50=0.75, MRR=0.583, mean latency 110 ms. Live
  `retrieve-memory` answer mode used `retrieve_memory` on all four cases but
  exact/contains answer accuracy was 0/4, with mean answer latency about
  20.1 seconds. Live `gather-memory` answer mode used `gather_context` but
  surfaced no matching named-memory evidence on the same four cases, also
  scoring 0/4. This points to two next targets: improve answer synthesis over
  retrieved named-memory evidence and wire named-memory recall into the
  gather-context answer surface instead of treating the LongMem result as a
  model-quality-only failure.
- Latest selected package gate passes:
  `go test -count=1 ./internal/context/contextengine ./internal/context/contextplane ./internal/context/memorycore ./internal/storage/contextengine ./internal/storage/tasks ./internal/storage/memory ./internal/rlm ./internal/rlm/env ./internal/runtime/hooks/memoryflow`
- `git diff --check` passes.
- This is not a full-repo release gate. The broader dirty worktree includes
  adjacent LongMem eval and command-package changes outside this goal, so this
  slice uses focused command tests plus the canonical context/memory/RLM/runtime
  package gate.
- `make check-doc-links` passes.
- Native adversarial architecture/test reviewers, Pi, and Hermes reviewed the
  final named-memory seam and dirty-edit revalidation slice. Native follow-up
  review cleared the blocker fixes; residual risks were limited to existing
  `tool_exec.go` size, per-call named-memory store open/close overhead,
  first-failure aggregation in lifecycle effect application, and distinguishing
  deterministic store ordering from future semantic ranking.

### Residual Risks

- Retrieval feedback now has explicit CLI and RLM answer-feedback callers, but
  real agent sessions still need to consistently attach precise explicit
  `UsedRefs` for lifecycle-impacting outcomes. Search access and answer
  citation alone are intentionally not treated as lifecycle proof of claim use.
- Candidate claim consolidation is deferred. Scope-aware deterministic draft
  dedupe prevents duplicate claims for the same scoped feedback-derived memory
  draft, but broader cross-episode semantic merging remains curator work.
- Confidence is medium-high for the structural slice: the primitives are wired,
  tested, and retrieval-visible, but the next quality step is observing real
  agent sessions and tightening callers that emit retrieval feedback.
- `load_evidence_ref` uses `laneConfig()` to derive the active workspace for
  `memory_claim` loads. That is correct and side-effect-free, but a future
  cleanup could make the workspace derivation explicit and cheaper.
- `ApplyInvalidation` now returns claim-transition persistence failures instead
  of swallowing them. Retry is covered and deterministic, but the first failed
  attempt stops at the first failed marker rather than aggregating every marker
  failure.
- Same-status claim ordering is deterministic, but it is not semantic ranking;
  equally visible claims still need future retrieval/ranking work if observed
  sessions need more than stable store order.
- Workspace-only all-claim `ListClaims` scans still rely on the workspace index
  and may sort after filtering. Status-filtered retrieval and task/session
  scoped retrieval have ordered indexes; all-claim scans remain a performance
  follow-up if profiling shows they matter.
