# Spec: Post‑Review Pipeline Harness (Phase 2)

**Status:** Draft\
**Related impl plan:**
[docs/impl_plan/universal_swe_grep_and_agents.md](cci:7://file://docs/impl_plan/universal_swe_grep_and_agents.md:0:0-0:0)
(Phase 2)\
**Related todo specs:**

- `..._specs_phase1_review_gate_todo.md`
- `..._specs_phase2_post_review_harness_todo.md`

**Related specs:**

- `docs/spec/review_gate.md`
- `docs/archive/specs/semantic_file_index.md`
- `docs/spec/code_symbol_index_and_swe_grep.md`
- `docs/archive/specs/dspy_go_agents.md` (legacy)
- `docs/archive/specs/dspy_trajectory_capture.md` (legacy)
- `docs/spec/core_profile_v1.md`
- [docs/spec/review_semantic_trajectory_specs.md](cci:7://file://docs/spec/review_semantic_trajectory_specs.md:0:0-0:0)

## 1. Problem Statement & Goals

When a task’s review transitions to `ok` and the corresponding diff is applied,
we need a **single, canonical post‑review harness** that:

- Emits a **normalized post‑review event** capturing:
  - Workspace, task, review, and file set (paths + digests + change kinds).
- Fans this event out to **one or more indexers** (semantic, symbol, future).
- Ensures **idempotent processing** per `(workspace_id, task_id, review_id)`.
- Integrates cleanly with:
  - **Jobs system & WFQ scheduler** (for deferred/async indexers).
  - **Exec/WASI runners & Core Profile v1** (for skill-based indexers).
  - **Overseer & agent hierarchy** (for orchestration/ownership).

**Goals (Phase 2 “full” behavior):**

- Canonicalize the **post‑review event schema** and storage.
- Implement an **overseer post‑review handler** that:
  - Observes `ReviewArtifact` transitions to `ok` + applied diffs.
  - Produces and persists `PostReviewEvent`.
  - Enqueues indexing work as **jobs** where appropriate.
- Implement a **configurable fanout** to named indexers:
  - At least semantic + symbol indexers (even if symbol is stubbed).
- Provide **observability** (logs, metrics, trace hooks) so we can debug and
  tune indexing.

**Non-goals (Phase 2):**

- Full fidelity **trajectory integration** (that’s later phases).
- End‑user UI flows for managing indexers or reviewing indexing status.
- Multi‑repo/monorepo cross‑workspace indexing semantics.

---

## 2. Terminology

- **ReviewArtifact:** Domain record for a review (from Phase 1), persisted in
  CAS + DB.
- **PostReviewEvent:** Canonical event representing “review OK + diff applied”.
- **Indexer:** A consumer of `PostReviewEvent` that updates some index
  (semantic, symbol, etc.).
- **Harness:** The overseer‑owned component that produces events and fans them
  out.
- **Job:** A record in `internal/storage/jobs` representing deferred indexing
  work.

---

## 3. Architecture Overview

### 3.1 High-level Flow

```mermaid
flowchart TD
    A[Task review_request (Phase 1)] --> B[ReviewArtifact pending]
    B --> C[Human/auto review completes]
    C --> D[Diff applied to workspace]
    D --> E[PostReviewHandler (overseer)]
    E --> F[PostReviewEvent persisted]
    F --> G[indexing.post_review.fanout]
    G --> H1[Semantic Indexer (job or inline)]
    G --> H2[Symbol Indexer (job or inline)]
    G --> H3[Future indexers...]
    H1 & H2 & H3 --> I[Updated indexes & CAS artifacts]
```

### 3.2 Key Components

- `internal/domain/agent.ReviewArtifact`
- `internal/intelligence/indexing/postreview` (new):
  - `PostReviewEvent` type.
  - Storage + idempotence helpers.
- `internal/intelligence/analysis/overseer`:
  - New **post‑review handler** and wiring into overseer events.
- `internal/storage/jobs` + `internal/runtime/execution/scheduler`:
  - Optional job‑backed indexer execution.
- `skills/*` (semantic/symbol indexers) or internal Go indexers:
  - Executed via Exec/WASI runners per existing patterns.

---

## 4. Data Model

### 4.1 PostReviewEvent

New internal type (Go struct + JSON schema):

- `id: string` – ULID or `sha256:` digest; unique per event.
- `workspace_id: string` – workspace identifier (same semantics as
  tasks/skills).
- `task_id: string`
- `review_id: string` – matches `ReviewArtifact.ID`.
- `review_kind: string` – `auto|human|mixed`.
- `review_status: string` – MUST be `ok` for emitted events.
- `diff_applied_at: time` – UTC timestamp when diff was applied.
- `files: []PostReviewFile`:
  - `path: string` – workspace‑relative.
  - `digest: string` – `sha256:<hex>` of full file content after review.
  - `change_kind: string` – `added|modified|deleted|renamed`.
- `source: string` – e.g. `review_gate_v1`.
- `metadata: map[string]any` – reserved for:
  - Commit ID / branch.
  - Reviewer identity (redacted / non‑PII).
- `created_at: time`
- `sequence: int` – monotonic per `(workspace, task)` if we support multiple
  reviews.

**Invariants:**

- `(workspace_id, task_id, review_id)` is **unique** across events.
- `files[*].digest` MUST match the content that indexers will see.

### 4.2 Storage

- Events stored in **jobs DB** or a dedicated table, but with:

  - **CAS‑backed payload** for large file sets:
    - Inline JSON for small payloads.
    - For large: JSON → CAS → `data.artifact` (and optional `meta.cas_digest`
      matching it) per Core Profile.

- Index key(s):
  - `(workspace_id, task_id, review_id)` unique index.
  - Optional index on `created_at` for maintenance.

---

## 5. Triggers & Lifecycle

### 5.1 Trigger Conditions

The harness emits a `PostReviewEvent` when all hold:

- A `ReviewArtifact` transitions from `pending|failed|stale` → `ok`.
- The associated **diff is successfully applied** to the workspace.
- Task record is updated to reflect review outcome (`LastReviewStatus=ok`,
  `LastReviewID=review_id`).
- File list & digests are available from the diff application layer or job.

### 5.2 Idempotence

- For a given `(workspace_id, task_id, review_id)`:
  - At most **one** `PostReviewEvent` is stored.
  - Re‑emitting is either:
    - No‑op if identical payload, or
    - Rejected with `ErrDuplicateEvent` if payload differs.
- Indexers must treat events as **at‑least‑once**:
  - They should de‑duplicate internally based on `event.id` and/or `review_id`.

---

## 6. Harness Behavior

### 6.1 Overseer PostReviewHandler

New component in overseer:

- **Inputs:**
  - `ReviewArtifact` (now `ok`).
  - Task metadata.
  - List of affected files `{path, digest, change_kind}`.
  - Optional Git/VC metadata.

- **Steps:**
  1. **Validate**:
     - `review.Status == "ok"`.
     - `task.LastReviewID == review.ID`.
     - Files list non‑empty (unless spec explicitly allows empty).
  2. Build `PostReviewEvent` instance.
  3. Persist event (via `postreview.Store` helper).
  4. Emit to `indexing.post_review.fanout`.

### 6.2 Fanout Strategy

Two modes (configurable, but spec should define v1 default):

- **Inline mode (Phase 2 required minimum):**
  - Handler directly iterates configured indexers and calls them synchronously.
  - Failures surfaced as handler errors (and logged), but:
    - Partial failure policy is defined (e.g. soft‑fail some indexers).

- **Job mode (full spec behavior):**
  - Handler enqueues **one job per indexer**:
    - Job input includes either:
      - Inline `PostReviewEvent` JSON, or
      - CAS digest of event.
    - Queue namespace derived from indexer name/workspace for WFQ scheduling.
  - Scheduler codemap applies for fairness and parallelism.
  - Indexers read from job input and process independently.

Spec should define:

- Default: **job mode** for semantic and symbol indexers once job flow is
  stable.
- Inline mode allowed for early experiments, behind clear config.

---

## 7. Configuration

New config section (names illustrative):

```yaml
indexing:
  post_review:
    enabled: true
    # Global behavior
    mode: "jobs" # "inline" | "jobs"
    # Which indexers to invoke
    indexers:
      - name: "semantic"
        kind: "job" # "job" | "inline"
        job_queue: "semantic"
        enabled: true
      - name: "symbol"
        kind: "job"
        job_queue: "symbol"
        enabled: true
```

**Validation rules:**

- If `enabled=false`, harness MUST NOT emit events or jobs.
- Unknown indexer names or kinds → validation error on startup.
- For job indexers:
  - `job_queue` MUST be non‑empty and map to WFQ namespaces.

---

## 8. Indexer Interface

Abstract indexer interface (internal):

- `HandlePostReviewEvent(ctx, event)`:
  - Semantic indexer’s implementation: reindex touched files.
  - Symbol indexer’s implementation: update symbol graph for touched files.

For **job mode**:

- Job input is an envelope:

  - `version: 1`
  - `command: "indexing.post_review.<indexer>"`
  - `data.event`: inline event or `data.artifact` with CAS digest.
  - `meta`: includes `workspace_id`, `task_id`, `review_id`.

- Indexing skill or internal worker:
  - Reads event.
  - Applies its own logic per its spec.

---

## 9. Error Handling & Retries

Define explicit behavior:

- **Event creation failure**:
  - Overseer handler returns error; review remains `ok` but indexing is not
    triggered.
  - Error logged with `workspace_id`, `task_id`, `review_id`.
  - Optional: overseer can enqueue a “recovery” job.

- **Indexer failures (inline mode)**:
  - If any indexer fails:
    - Handler logs failure.
    - Options:
      - Fail the handler (no other indexers run).
      - Or mark failure but continue to others (spec chooses one; default:
        “best-effort with per‑indexer logging”).

- **Indexer failures (job mode)**:
  - Monitor jobs via job store & metrics.
  - Retry policy:
    - Limit `N` retries per event/indexer with backoff.
    - Persistent failures flagged in logs and optional diagnostic skill.

---

## 10. Observability

- **Logging:**
  - Structured logs from harness:
    - `event_id`, `workspace_id`, `task_id`, `review_id`, `num_files`,
      `indexers`.
  - Structured logs per indexer job:
    - `indexer_name`, `job_id`, `state`, `duration`, `error`.

- **Metrics:**
  - Counters:
    - `post_review_events_total{indexer}`.
    - `post_review_events_failures_total{reason}`.
  - Histograms:
    - `post_review_index_duration_seconds{indexer}`.
  - Gauges:
    - Backlog size per indexer job queue.

- **Tracing (optional):**
  - Reuse existing job/runner trace IDs; propagate `meta.correlation_id` from
    review.

---

## 11. Testing & Golden Files

- **Unit tests:**
  - Event builder:
    - Valid event when review is `ok` and files list is non‑empty.
    - Rejects invalid statuses or missing digests.
  - Idempotence:
    - Duplicate event insert with same payload is OK.
    - Duplicate with conflicting payload = error.

- **Integration tests:**
  - Simulate:
    - Task → review_request → review `ok` → diff apply → event emitted.
    - End‑to‑end path to a fake indexer (inline + job modes).

- **Golden fixtures:**
  - If events cross process boundaries:
    - JSON examples in `test/golden/envelopes/post_review_event_*.json`.
  - Keep aligned with `core_profile_v1.md` and codemap.

---

## 12. Rollout Plan

- **Phase A (behind config)**
  - Add harness + event type.
  - Default `indexing.post_review.enabled = false`.
  - Only enable in dev/test environments.

- **Phase B (inline indexers in dev)**
  - Switch dev configs to `enabled=true`, `mode="inline"`.
  - Use fake or semantic indexer only.

- **Phase C (jobs mode in dev/staging)**
  - Wire indexers as jobs.
  - Validate job/state transitions + WFQ scheduling.

- **Phase D (production rollout)**
  - Enable in production with:
    - Conservative indexer set (semantic only).
    - Strict monitoring of errors and queue sizes.
  - Gradually add symbol + additional indexers.

---

## 13. Rollback Plan

- Config‑only rollback:

  - Set `indexing.post_review.enabled=false` to stop emitting events and jobs.
  - Existing events remain stored but are ignored.

- Code rollback:

  - Revert harness wiring in overseer and postreview package.
  - Leave indexer implementations intact if they are used elsewhere.

- Data rollback:

  - No destructive migrations in v1; events table is append‑only.
  - Optional cleanup job to GC old post-review events if needed.

---

## 14. Open Questions

- Should we **always** use jobs mode for production, or allow inline in small
  workspaces?
- Do we need **per‑indexer concurrency caps** distinct from WFQ worker counts?
- When do we integrate **trajectory capture** directly into post‑review events
  (or is that strictly a later phase)?

---
