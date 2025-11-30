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

## D3. Per-Indexer Concurrency Caps in WFQ Scheduler

**Status:** Deferred to Phase 2 C2 (fanout wiring)

**What:**  
In jobs mode, each indexer should respect `ConcurrencyPerIndexer` (default 3).
This requires wiring the WFQ scheduler to enforce per-namespace caps.

**Why deferred:**  
Phase 2 A focuses on event model. B focuses on handler. C focuses on fanout.
Concurrency enforcement is part of C2.

**When to address:**  
- Phase 2 section C (Indexer Configuration & Fanout).

**Cross-refs:**
- `docs/spec/post_review_harness.md` §7 (ConcurrencyPerIndexer)
- `docs/impl_plan/universal_swe_grep_and_agents_specs_phase2_post_review_harness_todo.md` C2

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
