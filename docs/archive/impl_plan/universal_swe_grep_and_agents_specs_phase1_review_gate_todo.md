# Phase 1 – Review Gate v1 Todo Spec

This spec breaks down Phase 1 of `universal_swe_grep_and_agents` into concrete
steps, grouped as:

- **A. `todo/manage` review operations**
- **B. `todo/manage.complete` + lifecycle semantics under the review gate**
- **C. Review artifacts + CAS wiring**

It is a *todo-style* implementation guide: each bullet should map to one or
more PRs and be cross-checked against the main impl/testing plans.

> Cross-refs:
> - Impl plan: `docs/impl_plan/universal_swe_grep_and_agents.md` (Phase 1)
> - Testing plan: `docs/impl_plan/universal_swe_grep_and_agents_testing.md` (Phase 1)
> - Spec: `docs/spec/review_gate.md`
> - Core profile: `docs/spec/core_profile_v1.md`

---

## A. `todo/manage` Review Operations

### A1. Introduce review operations in `skills/todo` input schema

- [x] Extend the `input` struct in `skills/todo/main.go` with:
  - `ReviewRequest *reviewRequest    "json:\"review_request\""`
  - `ReviewStatus  *reviewStatusReq  "json:\"review_status\""`
- [x] Define `reviewRequest` and `reviewStatusReq` types that align with
  `review_gate.md`:
  - `ReviewRequest`:
    - `workspace_id` (required)
    - `task_id` (required)
    - Optional `kind` (`auto|human|mixed`, default `auto`).
  - `ReviewStatusReq`:
    - `workspace_id`
    - `task_id`.

### A2. Wire `operation="review_request"` and `"review_status"`

- [x] Extend the `switch op := strings.ToLower(in.Operation)` in `run` to
  handle:
  - `"review_request"` → `handleReviewRequest(...)`.
  - `"review_status"` → `handleReviewStatus(...)`.
- [x] Implement `handleReviewRequest` in `skills/todo/main.go`:
  - Validate required fields (workspace_id, task_id).
  - Load the task from `tasks.Store` and validate current status:
    - Allowed: `in_progress`, `ready_for_review` (per `review_gate.md`).
    - Reject: `pending`, `completed`, `blocked`, `canceled` (with clear
      error envelopes).
  - For v1, keep behavior minimal and local:
    - Set task `Status` → `ready_for_review` (if not already) and persist.
    - Set `LastReviewStatus` → `pending`, `LastReviewAt` → now.
    - Generate a synthetic `LastReviewID` (e.g. ULID) representing a logical
      "review request" record to be consumed by Phase 2+ indexers.
  - Return `data` including:
    - `task_id`, `status`, `last_review_status`, `last_review_id`.
- [x] Implement `handleReviewStatus`:
  - Lookup task by `(workspace_id, task_id)`.
  - Return only cheap, inline data:
    - `last_review_status`, `last_review_at`, `last_review_id`.
  - Do **not** call CAS or jobs here; this is a fast status probe.

### A3. Envelope & error semantics

- [x] Ensure both operations use standard envelope error handling:
  - Missing/invalid inputs → `status:"error"` envelopes with actionable
    `error.code` and `error.message` (no panics).
- [x] Add happy-path unit tests under `skills/todo/main_test.go` for:
  - Valid review_request transitions (`in_progress` → `ready_for_review`).
  - Rejection for invalid states (e.g. `completed` task).
  - Review_status returning the expected `last_review_*` fields.

---

## B. `todo/manage.complete` + Lifecycle Under Review Gate

Goal: extend `todo/manage.complete` so it respects the review gate when enabled,
without overcomplicating policy in v1.

### B1. Detect review-gate enablement (v1 minimal policy)

- [ ] Decide where the review gate is toggled for a workspace in v1:
  - Option 1: Environment flag (e.g.
    `FOXCTL_TODO_REVIEW_GATE=on` for now).
  - Option 2: Config field in `config.Config` / workspace settings.
- [ ] Implement a tiny helper `isReviewGateEnabled(workspaceID, cfg)` inside
  `skills/todo/main.go` that encapsulates this rule and can evolve later
  without touching the core logic.

### B2. Enforce review gate in `handleComplete`

- [x] In `handleComplete`, after loading the task and checking dependencies,
  branch on `isReviewGateEnabled`:
  - **If disabled:** keep existing behavior (mark completed, set notes/gotchas).
  - **If enabled:**
    - Require `task.Status == ready_for_review`.
    - Require `LastReviewStatus == ok` and `LastReviewID != ""`.
    - If missing or not `ok`, return an error envelope indicating review is
      required before completion.
- [x] Ensure the function continues to update `CompletedAt`, `Notes`, and
  `Gotchas` exactly once when all conditions are satisfied.

### B3. Lifecycle tests

- [x] Add unit tests in `skills/todo/main_test.go` to cover:
  - Happy path:
    - Task with `Status=ready_for_review`, `LastReviewStatus=ok`, and
      `LastReviewID` set → `complete` succeeds and status becomes `completed`.
  - Rejected paths:
    - Gate enabled but `LastReviewStatus` is `pending` or `stale`.
    - Gate enabled but task is `pending` or `in_progress`.
    - Gate enabled but no `LastReviewID`.
- [ ] Keep these tests strictly local (no CAS/jobs), using the existing
  `todoTestEnv` harness.

---

## C. Review Artifacts + CAS Wiring

Goal: introduce a minimal, forward-compatible representation for review
artifacts that can later be consumed by Phase 2 (post-review harness) and
indexers.

### C1. Define a lightweight review artifact model

- [x] Add a new Go type under `internal/domain/agent` or a nearby domain
  package, e.g. `ReviewArtifact`:
  - Fields (per `review_gate.md` + core profile v1):
    - `ID` (sha256 digest or ULID)
    - `WorkspaceID`
    - `TaskID`
    - `Status` (`ok|failed|pending`)
    - `Summary` (short human-readable description)
    - Optional `CASDigest` for large review payloads.
- [ ] Ensure this type does **not** change the envelope wire contract yet; it
  should be used behind the scenes.

### C2. Minimal CAS integration

- [x] Implement a helper in `internal/adapters/artifacts` or similar that:
  - Accepts a `ReviewArtifact` payload + optional large body.
  - Decides whether to:
    - Inline the review details in envelopes, or
    - Store them in CAS and set `CASDigest`.
  - Returns the persisted `ReviewArtifact` (with digest if used).
- [ ] For v1, this can be a thin wrapper around the existing CAS store, reusing
  the same `Put`/`Head` semantics without introducing new envelope fields.

### C3. Wire review_request → artifact creation stub

- [x] Extend `handleReviewRequest` (from section A) to:
  - Create a minimal `ReviewArtifact` representing the request and status
    (e.g., `Status=pending`, `Summary` referencing the task/title).
  - Persist via the helper from C2.
  - Store the resulting `ReviewArtifact.ID` into `Task.LastReviewID`.
- [ ] Do **not** yet implement full-blown review pipelines; this is just enough
  to:
  - Give the post-review handler something stable to point at.
  - Make the Phase 1/2 specs and tests line up.

### C4. Tests and golden fixtures

- [x] Add unit tests around the artifact helper (C2) to ensure:
  - CAS digests are stable for a given payload.
  - Inline vs CAS behavior respects size thresholds (reuse core-profile rules).
- [x] (Optional for v1) Add one or two golden envelopes in
  `test/golden/envelopes/` showing a review_request result with
  `last_review_status` and `last_review_id` populated.

---

## Open Questions / To Discuss

- **Policy surface for enabling the review gate**:
  - For now this spec assumes a simple env/config toggle; long-term we may want
    workspace-level policy stored in tasks DB or config files.
- **How far to push artifacts in Phase 1**:
  - This spec keeps artifacts minimal and mostly prepares for Phase 2 (post-
    review harness) without committing to a full review UI/workflow.
- **Admin overrides**:
  - `review_gate.md` allows force-complete semantics; this spec proposes we
    defer explicit `force` flags until after the basic happy-path is wired and
    tested.
