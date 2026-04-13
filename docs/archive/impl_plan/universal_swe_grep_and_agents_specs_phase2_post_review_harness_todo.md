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
>   `docs/spec/review_semantic_trajectory_specs.md`,
>   `docs/spec/post_review_harness.md`
> - Core profile: `docs/spec/core_profile_v1.md`

---

## A. Event Model & Contracts

Goal: make the **post-review event** shape explicit and stable so indexers and
other consumers can rely on it.

### A1. Define post-review event schema

- [x] Document the canonical post-review event shape (in Go types and docs):
  - `workspace_id`
  - `task_id`
  - `review_id`
  - `files[{path, digest, change_kind}]`
  - Optional metadata needed by semantic/symbol indexers (e.g. branch, commit).
  - Extended in `internal/indexing/types.go` with: `id`, `review_kind`,
    `review_status`, `diff_applied_at`, `source`, `metadata`, `created_at`,
    `sequence` per `docs/spec/post_review_harness.md` §4.1.
- [x] Ensure this shape matches `review_gate.md` and `semantic_file_index.md`
  where they talk about post-review triggers.
- [x] Add a small internal package (e.g. `internal/indexing/postreview`) that
  owns this event type and any helpers:
  - `internal/indexing/postreview/store.go` – idempotent SQLite store.
  - `internal/indexing/postreview/store_test.go` – unit tests for Put, Get,
    GetByReview, List, and idempotence/duplicate detection.

### A2. Wire review artifacts → post-review events

- [x] Decide how post-review events are produced when a review goes `ok` and a
  diff is applied:
  - `Producer.Produce(ctx, artifact, files)` in `internal/indexing/postreview/producer.go`.
  - `BuildPostReviewEvent(artifact, files)` constructs the event from a `ReviewArtifact`.
  - **Stub behavior:** Files field is empty until diff layer exists
    (see `docs/impl_plan/universal_swe_grep_and_agents_deferred.md` D1).
- [x] Define clear invariants:
  - Events are **idempotent** for the same `(workspace_id, task_id, review_id)` tuple
    (enforced by `postreview.Store.Put` with duplicate detection).
  - Events are only emitted after the corresponding changes are persisted
    (caller responsibility; producer assumes artifact is already `ok`).

---

## B. Overseer Post-Review Handler

Goal: implement a concrete handler that listens for `ok` reviews and fans out a
single internal event to configured indexers.

### B1. Implement handler entrypoint

- [x] Add a post-review handler in the overseer (see impl plan Phase 2):
  - `internal/intelligence/analysis/overseer/post_review.go` – `PostReviewHandler` struct.
  - `HandleReviewApproved(ctx, artifact, files)` – main entrypoint.
  - `HandleReviewApprovedStub(ctx, artifact)` – convenience for stub (no files).
  - Triggered when a `ReviewArtifact` transitions to `ok` and the diff is
    applied.
  - Collects `(workspace_id, files[{path, digest, change_kind}], task_id,
    review_id)` into the event type from A1.
- [x] Structure the handler so it always builds and persists a `PostReviewEvent`
  and then delegates to the configured fanout (see C), allowing:
  - Inline processing for dev/tests.
  - Job-backed processing for production (default mode).
  - Current: delegates to `indexing.PostReviewHandler.Handle()` for fanout.

### B2. Emit a single internal command/event

- [x] Implement a single internal command/event type (e.g.
  `PostReviewIndexEvent`) that downstream indexers consume:
  - Uses `indexing.PostReviewEvent` (extended in A1) as the canonical event.
  - Produced via `postreview.Producer.Produce()` with idempotence.
- [x] Ensure emission is:
  - Logged with enough context for debugging (workspace, task, review IDs,
    file count, event ID).
  - Observable in tests via `SetTestHook(fn)` callback.
- [x] Add basic unit tests around the handler that:
  - Simulate a review going `ok` → event produced.
  - Assert exactly one event is emitted with the expected contents.
  - Verify idempotence (same artifact → same event ID).
  - Verify rejection of non-ok status.
  - Verify files are passed through to event.
  - `internal/intelligence/analysis/overseer/post_review_test.go` – 5 tests.

---

## C. Indexer Configuration & Fanout

Goal: configure which indexers run post-review and how they subscribe to events.

### C1. Configuration surface

- [x] Implement configuration for `indexing.post_review` as in
  `semantic_file_index.md` §8.2, including:
  - `enabled` flag (default false; must be true to emit events/jobs).
  - `mode` (`inline` | `jobs`), with **production default = `jobs`**.
  - `indexers[]` entries (at minimum semantic and symbol indexers).
  - `concurrency_per_indexer` (default 3) for jobs mode.
  - Implemented in `internal/indexing/types.go` `PostReviewConfig`.
- [x] Decide where this configuration lives (e.g. `config.Config`, a dedicated
  indexing settings struct, or both):
  - Lives in `indexing.PostReviewConfig` (dedicated struct).
  - `PostReviewConfigFromSettings()` converts from platform config.
- [x] Add validation to reject unknown indexer names or invalid modes with
  actionable errors:
  - `PostReviewConfig.Validate()` checks mode, concurrency, indexer IDs.
  - `PostReviewConfig.EffectiveMode()` defaults empty to `jobs`.
  - Tests in `handler_test.go` `TestPostReviewConfig_Validate`.

### C2. Indexer subscribers

- [x] Define a small interface that indexers implement to consume
  post-review events (e.g. `HandlePostReviewEvent(ctx, event)`):
  - `Indexer.Index(ctx, event)` in `internal/indexing/types.go`.
- [x] Implement basic fanout over the configured indexers:
  - For `mode="inline"`, sequential fanout in current goroutine.
  - For `mode="jobs"`, **stub behavior**: falls back to async goroutine.
    Full WFQ scheduler integration is deferred (see `deferred.md` D3).
  - Mode-aware switch in `handler.go` `Handle()` with logging.
- [x] Ensure that failure in one indexer does not corrupt others; document the
  retry/backoff story or explicit non-goals for v1:
  - Each indexer runs independently; errors are logged and recorded in
    `IndexerResults` but do not abort other indexers.
  - Retry/backoff is a non-goal for v1 (events are idempotent; re-run safe).

---

## D. Tests, Golden Envelopes, and Observability

Goal: make the harness observable and testable without needing the full
semantic/symbol indexers implemented.

### D1. Unit and integration tests

- [x] Add unit tests around the overseer handler and event emission:
  - Happy-path `ok` review producing one event.
  - No event on non-`ok` statuses (error returned).
  - Idempotence: same artifact → same event ID.
  - Files passthrough to event.
  - `internal/intelligence/analysis/overseer/post_review_test.go` – 6 tests.
- [x] Add integration-style tests that:
  - Wire a fake indexer subscriber.
  - Assert that it receives the expected events for a synthetic review flow.
  - `TestPostReviewHandler_IntegrationWithFakeIndexer` – full flow test.

### D2. Golden envelopes / fixtures

- [x] If post-review events cross a process boundary, add golden JSON fixtures
  under `test/golden/` (or appropriate `testdata/`) to lock in the shape:
  - `test/golden/envelopes/post_review_event.json` – canonical event shape.
- [x] Ensure these fixtures stay in sync with `core_profile_v1.md` and the
  relevant specs:
  - `test/golden/golden_test.go` `TestGoldenPostReviewEvent` validates shape.

### D3. Logging and metrics

- [x] Decide on minimal logging fields for post-review processing
  (workspace/task/review IDs, number of files, indexers invoked, mode,
  indexer names):
  - Implemented in `overseer/post_review.go` and `indexing/handler.go`.
  - Fields: workspace_id, task_id, review_id, event_id, file_count,
    indexer_count, mode, indexer_id, files_indexed.
- [ ] Add metrics as per `docs/spec/post_review_harness.md` §10:
  - Counters: `post_review_events_total{indexer}`,
    `post_review_events_failures_total{reason}`.
  - Histograms: `post_review_index_duration_seconds{indexer}`.
  - Gauges: backlog size per indexer job queue.
  - **Deferred:** Metrics implementation is out of scope for Phase 2.

---

## Open Questions / To Discuss

- See `docs/spec/post_review_harness.md` §14 for canonical open questions.
- This Phase 2 todo spec assumes:
  - Production uses **jobs** mode with a concurrency cap of 3 per indexer.
  - Inline mode is only used in dev/test as needed.
  - Trajectory capture integration is deferred to later phases, but the event
    schema and metadata are designed to feed trajectories later.
