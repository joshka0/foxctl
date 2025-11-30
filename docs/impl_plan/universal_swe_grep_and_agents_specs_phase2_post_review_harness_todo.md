# Phase 2 – Post-Review Pipeline Harness Todo Spec

This spec breaks down Phase 2 of `universal_swe_grep_and_agents` into concrete
steps focused on the **post-review harness** that fans out to semantic and
symbol indexers.

- Phase 1 established the review gate and review artifacts.
- Phase 2 consumes those artifacts when a review becomes `ok` and a diff is
  applied, and emits a single internal event that downstream indexers
  subscribe to.

> Cross-refs:
> - Impl plan: `docs/impl_plan/universal_swe_grep_and_agents.md` (Phase 2)
> - Testing plan: `docs/impl_plan/universal_swe_grep_and_agents_testing.md` (Phase 2)
> - Specs: `docs/spec/review_gate.md`, `docs/spec/semantic_file_index.md`,
>   `docs/spec/code_symbol_index_and_swe_grep.md`,
>   `docs/spec/review_semantic_trajectory_specs.md`
> - Core profile: `docs/spec/core_profile_v1.md`

---

## A. Event Model & Contracts

Goal: make the **post-review event** shape explicit and stable so indexers and
other consumers can rely on it.

### A1. Define post-review event schema

- [ ] Document the canonical post-review event shape (in Go types and docs):
  - `workspace_id`
  - `task_id`
  - `review_id`
  - `files[{path, digest, change_kind}]`
  - Optional metadata needed by semantic/symbol indexers (e.g. branch, commit).
- [ ] Ensure this shape matches `review_gate.md` and `semantic_file_index.md`
  where they talk about post-review triggers.
- [ ] Add a small internal package (e.g. `internal/indexing/postreview`) that
  owns this event type and any helpers.

### A2. Wire review artifacts → post-review events

- [ ] Decide how post-review events are produced when a review goes `ok` and a
  diff is applied:
  - Source of truth for file list and digests (e.g. diff application layer).
  - How to correlate `ReviewArtifact` → `post_review_event`.
- [ ] Define clear invariants:
  - Events are **idempotent** for the same `(task_id, review_id)` pair.
  - Events are only emitted after the corresponding changes are persisted.

---

## B. Overseer Post-Review Handler

Goal: implement a concrete handler that listens for `ok` reviews and fans out a
single internal event to configured indexers.

### B1. Implement handler entrypoint

- [ ] Add a post-review handler in the overseer (see impl plan Phase 2):
  - Triggered when a `ReviewArtifact` transitions to `ok` and the diff is
    applied.
  - Collects `(workspace_id, files[{path, digest, change_kind}], task_id,
    review_id)` into the event type from A1.
- [ ] Decide whether this handler runs inline with review completion or via the
  job system; document the choice and trade-offs.

### B2. Emit a single internal command/event

- [ ] Implement a single internal command/event type (e.g.
  `PostReviewIndexEvent`) that downstream indexers consume.
- [ ] Ensure emission is:
  - Logged with enough context for debugging.
  - Observable in tests via a test hook or fake sink.
- [ ] Add basic unit tests around the handler that:
  - Simulate a review going `ok`.
  - Assert exactly one event is emitted with the expected contents.

---

## C. Indexer Configuration & Fanout

Goal: configure which indexers run post-review and how they subscribe to events.

### C1. Configuration surface

- [ ] Implement configuration for `indexing.post_review.indexers` as in
  `semantic_file_index.md` §8.2:
  - At minimum, support enabling/disabling semantic and symbol indexers.
  - Allow for future indexers without changing the event contract.
- [ ] Decide where this configuration lives (e.g. `config.Config`, a dedicated
  indexing settings struct, or both).
- [ ] Add validation to reject unknown indexer names with actionable errors.

### C2. Indexer subscribers

- [ ] Define a small interface that indexers implement to consume
  post-review events (e.g. `HandlePostReviewEvent(ctx, event)`).
- [ ] Implement basic fanout over the configured indexers:
  - Sequential or bounded-concurrency fanout.
  - Clear logging and error propagation semantics.
- [ ] Ensure that failure in one indexer does not corrupt others; document the
  retry/backoff story or explicit non-goals for v1.

---

## D. Tests, Golden Envelopes, and Observability

Goal: make the harness observable and testable without needing the full
semantic/symbol indexers implemented.

### D1. Unit and integration tests

- [ ] Add unit tests around the overseer handler and event emission:
  - Happy-path `ok` review producing one event.
  - No event on non-`ok` statuses.
- [ ] Add integration-style tests that:
  - Wire a fake indexer subscriber.
  - Assert that it receives the expected events for a synthetic review flow.

### D2. Golden envelopes / fixtures

- [ ] If post-review events cross a process boundary, add golden JSON fixtures
  under `test/golden/` (or appropriate `testdata/`) to lock in the shape.
- [ ] Ensure these fixtures stay in sync with `core_profile_v1.md` and the
  relevant specs.

### D3. Logging and metrics

- [ ] Decide on minimal logging fields for post-review processing
  (workspace/task/review IDs, number of files, indexers invoked).
- [ ] Add basic metrics (counters, durations, failures) if appropriate, or
  explicitly document that they are deferred to a later phase.

---

## Open Questions / To Discuss

- How much of post-review processing should run inline vs through the job
  system in Phase 2?
- Do we need retries/backoff for failed indexers now, or can that wait for a
  later phase?
- Should post-review events also be recorded in the trajectory system in
  Phase 2, or only once Phase 7 is in place?
