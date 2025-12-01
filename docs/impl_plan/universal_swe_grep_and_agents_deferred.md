# Universal SWE Grep & Agents — Deferred Work

This document tracks work that is explicitly **deferred** to later phases or PRs.
Each entry includes:

- What is deferred.
- Why it's deferred.
- When it should be addressed.
- Cross-refs to specs/impl plans.

---

## D1. Diff Application Layer → File List in PostReviewEvent

**Status:** Deferred to Phase 2+ (post-harness foundation)

**What:**  
The `PostReviewEvent.Files` field should contain the actual list of files
changed by the reviewed diff, with their digests and change kinds. Currently,
the stub producer creates events with an empty/minimal file list.

**Why deferred:**  
Phase 1/early Phase 2 focuses on:
- Review gate semantics (request/status/complete).
- PostReviewEvent schema and idempotent storage.
- Handler/fanout infrastructure.

The diff application layer (which tracks which files changed and computes
digests) doesn't exist yet. Wiring the file list requires that layer.

**When to address:**  
- When implementing the diff application skill or hook.
- Or when integrating with git commit tracking.

**Cross-refs:**
- `docs/spec/post_review_harness.md` §4.1 (files field)
- `docs/spec/post_review_harness.md` §5.1 (trigger conditions)
- `docs/impl_plan/universal_swe_grep_and_agents_specs_phase2_post_review_harness_todo.md` A2

**Stub behavior:**  
`BuildPostReviewEvent` in `internal/indexing/postreview/producer.go` currently
sets `Files: nil`. Indexers should gracefully handle empty file lists (skip or
no-op) until this is wired.

---

## D2. Trajectory Capture Integration

**Status:** Deferred to Phase 7

**What:**  
Post-review events should be recorded in the trajectory system so they can be
used for agent training and evaluation.

**Why deferred:**  
Trajectory capture is Phase 7 in the impl plan. The event schema is designed
to support this (includes `metadata` for trace IDs, etc.), but wiring is
deferred.

**When to address:**  
- Phase 7 (`dspy_trajectory_capture.md`).

**Cross-refs:**
- `docs/spec/post_review_harness.md` §14 (open questions)
- `docs/spec/dspy_trajectory_capture.md`
- `docs/impl_plan/universal_swe_grep_and_agents.md` Phase 7

---

## D3. Jobs Mode WFQ Scheduler Integration

**Status:** Deferred (stub in place)

**What:**  
In `mode="jobs"`, the handler should enqueue one job per indexer via the WFQ
scheduler with per-namespace concurrency caps (`ConcurrencyPerIndexer`, default
3). Currently, `handler.go` falls back to an async goroutine when mode is jobs.

**Why deferred:**  
- Phase 2 C2 established the Mode-aware fanout structure.
- Actual job submission requires wiring `internal/storage/jobs` and the WFQ
  scheduler, which is a larger integration piece.
- The current async goroutine behavior is acceptable for initial testing.

**When to address:**  
- When jobs system is being exercised in production scenarios.
- Or when indexer load requires fair scheduling across workspaces.

**Stub behavior:**  
`handler.go` line ~100: jobs mode falls back to `go h.runIndexers(...)` with
`context.WithoutCancel`. This is fire-and-forget without queue or concurrency
enforcement.

**Cross-refs:**
- `docs/spec/post_review_harness.md` §7 (ConcurrencyPerIndexer)
- `internal/indexing/handler.go` `Handle()` switch on mode
- `internal/storage/jobs` (target for integration)
- `docs/impl_plan/universal_swe_grep_and_agents_specs_phase2_post_review_harness_todo.md` C2

---

## D4. Post-Review Metrics (Counters/Histograms/Gauges)

**Status:** Deferred (out of Phase 2 scope)

**What:**  
Implement the metrics described in `docs/spec/post_review_harness.md` §10 for the post-review harness and indexers:

- Counters: `post_review_events_total{indexer}`, `post_review_events_failures_total{reason}`.
- Histograms: `post_review_index_duration_seconds{indexer}`.
- Gauges: backlog size per indexer job queue.

**Why deferred:**  
Phase 2 focuses on the core event model, handler, and fanout infrastructure. Wiring full metrics (including label design, registration, and CI expectations) is additional work better done once the harness and initial indexers are stable.

**When to address:**  
- After Phase 2 lands and the semantic/symbol indexers are exercised in real workflows.  
- Either as part of Phase 3 (Semantic File Index observability) or a dedicated observability/metrics milestone.

**Cross-refs:**
- `docs/spec/post_review_harness.md` §10 (Metrics)
- `docs/impl_plan/universal_swe_grep_and_agents_specs_phase2_post_review_harness_todo.md` D3
- `docs/impl_plan/universal_swe_grep_and_agents.md` Phase 2 / Phase 3 overview

**Stub behavior (if any):**  
Current implementation logs key fields (workspace/task/review IDs, event ID, indexer IDs, mode, file and index counts) but does **not** emit Prometheus metrics. Any dashboards or alerting on post-review behavior must currently be derived from logs only.

---

## Template for New Entries

```markdown
## D<N>. <Short Title>

**Status:** Deferred to <Phase/PR>

**What:**  
<Brief description of what is deferred.>

**Why deferred:**  
<Reason for deferral.>

**When to address:**  
<Conditions or phase when this should be picked up.>

**Cross-refs:**
- <spec or impl plan links>

**Stub behavior (if any):**  
<What the current code does as a placeholder.>
```
