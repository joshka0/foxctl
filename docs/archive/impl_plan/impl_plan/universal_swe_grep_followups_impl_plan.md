### Universal SWE Grep Follow-ups – Implementation Plan

This plan assumes the follow-up spec in
`docs/specs/2025-12-13_universal_swe_grep_followups.md` is the source of truth
for scope, non-goals, and acceptance criteria.

Focus: align implementation with existing canonical specs without changing the
Core Profile v1 envelope shape.

---

## 0. Scope and Non-goals (contract guardrails)

This implementation plan covers follow-ups A–E from the follow-up spec:

- **A)** Bring `code/snippet_extract` into spec conformance.
- **B)** Implement `code.symbol_search` over the symbol index.
- **C)** Resolve symbol call graph representation (persisted call edges vs
  embedded calls).
- **D)** Populate `PostReviewEvent.Files` from the diff application layer.
- **E)** Trajectory capture: tool-call capture + `trajectory.export`.

Non-goals (repeat / enforce):

- No envelope shape changes.
- No new `meta.*` fields.
- No new dependencies.

Key invariants to preserve:

- **Artifactization policy:** `code/snippet_extract` uses **inline-threshold
  artifactization**. Thresholds are derived from existing config
  (`inline_output_kb`) and/or the skill manifest `io.inline_output_kb`.
- **Correlation naming:** correlation is sourced from envelope
  `meta.correlation_id` and stored internally as `trace_id` in trajectory
  records.

---

## 1. Canonical docs (context links)

### 1.1 Source-of-truth spec for this plan

- `docs/specs/2025-12-13_universal_swe_grep_followups.md`

### 1.2 Core specs

- `docs/spec/code_symbol_index_and_swe_grep.md`
  - SWE Grep contract (§5)
  - Symbol search contract (§6)
  - Trajectory integration (§7)
- `docs/spec/dspy_go_agents.md`
  - tool surface for `code.symbol_search` + `code.swe_grep`
- `docs/spec/dspy_trajectory_capture.md`
  - capture semantics + export contract
- `docs/spec/post_review_harness.md`
  - post-review event semantics (`PostReviewEvent.Files`)
- `docs/spec/review_gate.md`
  - review lifecycle and the “diff applied” point that triggers post-review
    events
- `docs/spec/core_profile_v1.md`
  - envelope/CAS invariants + error semantics catalog

### 1.3 Existing implementation plan docs (background)

- `docs/impl_plan/universal_swe_grep_and_agents.md` (end-to-end dependency
  order)
- `docs/impl_plan/universal_swe_grep_and_agents_deferred.md` (notably D1 diff
  layer)
- `docs/impl_plan/swe_grep_phase5_impl_plan.md` (skill work breakdown)
- `docs/impl_plan/phase6_retrieval_tools_decisions.md` (tool surface decisions)
- `docs/impl_plan/universal_swe_grep_and_agents_testing.md` (what is tested vs
  pending)

---

## 2. Current code touchpoints (for implementers)

This section anchors the plan in the current code layout.

### 2.1 `code/snippet_extract`

- Skill:
  - `skills/code_swe_grep/main.go`
  - `skills/code_swe_grep/main_test.go`
  - Manifest: `skills/code_swe_grep/skill.yaml`
- Integration tests:
  - `test/integration/swe_grep_test.go`

### 2.2 Tool layer (`code.symbol_search`, `code.swe_grep`)

- Tool registry implementation:
  - `internal/agent/tools/code_tools.go`
  - `internal/agent/tools/code_tools_test.go`

### 2.3 Symbol index (data model + persistence)

- Named memory type mapping and structs:
  - `internal/intelligence/indexing/symbol/types.go`
- Indexer implementation/tests:
  - `internal/intelligence/indexing/symbol/indexer.go`
  - `internal/intelligence/indexing/symbol/indexer_test.go`

### 2.4 Post-review event production

- Event producer (currently allows empty file list):
  - `internal/intelligence/indexing/postreview/producer.go`
- Overseer post-review handler:
  - `internal/intelligence/analysis/overseer/post_review.go`

### 2.5 Trajectory storage

- SQLite store uses `trace_id` column populated from envelope
  `meta.correlation_id`:
  - `internal/storage/trajectory/store.go`

---

## 3. Execution order (dependency-aware)

Recommended order (minimizes blast radius and keeps tests meaningful):

1. **Phase A:** `code/snippet_extract` spec conformance.
2. **Phase B:** `code.symbol_search` (mode=`search`) backed by symbol index.
3. **Phase C:** choose + align call graph representation; then enable
   callers/callees mode if desired.
4. **Phase D:** diff layer wiring to populate `PostReviewEvent.Files`.
5. **Phase E:** trajectory tool-call capture + `trajectory.export`.

---

## Phase A – Bring `code/snippet_extract` into spec conformance

**Goal:** align error codes and artifactization behavior with
`docs/spec/code_symbol_index_and_swe_grep.md` §5 without changing the envelope
shape.

### PR A1 – Error code alignment (no artifact policy change yet)

- **Scope**
  - Update `skills/code_swe_grep/main.go`:
    - Map the “no usable candidates” path to `E_SWE_GREP_NO_CANDIDATES`.
    - Map path validation failures to `E_GUARD_VIOLATION`.
    - Map missing validated files to `E_FILE_NOT_FOUND`.
    - Keep `EARG` for malformed JSON / missing required top-level keys.
  - Update `skills/code_swe_grep/main_test.go` to reflect the new error code
    expectations.

- **Validation**
  - `CGO_ENABLED=0 go test ./...`
  - Confirm error envelopes validate against Core Profile v1 helpers.

### PR A2 – Inline-threshold artifactization

- **Scope**
  - Update `skills/code_swe_grep/main.go`:
    - Only emit `data.artifact*` fields when inline output would exceed the
      configured inline threshold.
    - When artifact is present:
      - Write NDJSON to CAS.
      - `meta.cas_digest` is optional; if set it MUST match `data.artifact`.
    - When artifact is omitted:
      - Ensure `data.snippets_inline` is still present and complete.
  - Update `test/integration/swe_grep_test.go`:
    - Add a small-output case asserting artifact is omitted.
    - Ensure a large-output case asserts artifact is present.
  - Add goldens and fixtures:
    - `test/golden/envelopes/ok-code_swe_grep-inline.json`
    - `test/golden/envelopes/ok-code_swe_grep-cas.json`
    - `test/golden/swe_grep/*.ndjson`

- **Validation**
  - `CGO_ENABLED=0 go test ./...`
  - Golden tests pass (including NDJSON artifact content validation).

### PR A3 – Tool wrapper compatibility audit

- **Scope**
  - Ensure `internal/agent/tools/code_tools.go` handling for `code.swe_grep`:
    - Treats `data.artifact*` as optional.
    - Does not assume `artifact` is always present.

- **Validation**
  - `CGO_ENABLED=0 go test ./...`

---

## Phase B – Implement `code.symbol_search` over the symbol index

**Goal:** replace the stub in `internal/agent/tools/code_tools.go` with a real
implementation.

Status: Completed

### PR B1 – Implement `mode="search"` using symbol index (completed)

- **Scope**
  - Replace stub implementation in `internal/agent/tools/code_tools.go`.
  - Query named memory for entries with type `"code_symbol"` (see
    `internal/intelligence/indexing/symbol/types.go`).
  - Provide a minimal v1 ranking strategy that is deterministic and safe:
    - Prefer exact/substring matches against `symbol.name`, then
      `symbol.signature`.
    - If `symbol_hint` provided, boost matches on that hint.
    - Use stable tie-breakers (e.g., file path, symbol ID).
  - Return candidates in the shape documented by
    `docs/spec/code_symbol_index_and_swe_grep.md` §6.1.

- **Tests**
  - Update/add tests in `internal/agent/tools/code_tools_test.go` that:
    - Seed a memory store with a handful of `code_symbol` entries.
    - Assert ranking and max-results behavior.

- **Validation**
  - `CGO_ENABLED=0 go test ./...`

### PR B2 – `mode="callers"|"callees"` behavior (completed)

- **Scope**
  - Implement callers/callees using `code_symbol_call` entries _if_ Phase C
    chooses to persist call edges.
  - If call edges are not persisted, return an empty candidate list with an
    explicit message explaining why.

- **Validation**
  - `CGO_ENABLED=0 go test ./...`

---

## Phase C – Resolve symbol call graph representation

This phase is a decision point because it affects both indexing and the meaning
of `mode="callers"|"callees"`.

### Decision: Persist call edges vs. explicitly defer

- **Option 1 (preferred if callers/callees are required) (chosen):**
  - Persist `code_symbol_call` entries (type `"code_symbol_call"`) and implement
    callers/callees mode.

- **Option 2:**
  - Do not persist call edges in v1; keep best-effort `Result.Calls` embedded in
    `code_symbol` entries.
  - Update canonical docs to reflect this v1 limitation.

### PR C1 – Implement the chosen option and align docs (completed)

- **If Option 1:**
  - Update the symbol indexer to emit `code_symbol_call` entries.
  - Add indexer unit tests asserting call edges are persisted.
  - Ensure callers/callees mode in `code.symbol_search` is wired.

- **If Option 2:**
  - Update `docs/spec/code_symbol_index_and_swe_grep.md` to explicitly state
    call edges are not persisted in v1.
  - Ensure callers/callees mode returns an empty set (but succeeds) with a clear
    message.

- **Validation**
  - `CGO_ENABLED=0 go test ./...`

---

## Phase D – Populate `PostReviewEvent.Files` from the diff application layer

**Goal:** stop emitting empty file lists for real review-gate flows.

### PR D1 – Define diff application output (`[]indexing.FileChange`)

- **Scope**
  - Identify the component that applies the review diff to the workspace.
  - Ensure that component produces:
    - `path`
    - `change_kind`
    - `digest` (or equivalent)
  - Pass these file changes into:
    - `overseer.PostReviewHandler.HandleReviewApproved(..., files)`
  - Update post-review golden(s) and tests that currently assume an empty
    `Files` list:
    - `test/golden/envelopes/post_review_event.json`
    - `internal/intelligence/analysis/overseer/post_review_test.go`

- **Validation**
  - `CGO_ENABLED=0 go test ./...`

### PR D2 – Indexer fanout behavior with non-empty file lists

- **Scope**
  - Ensure semantic + symbol index post-review jobs consume `event.Files` for
    incremental work.
  - Add regression coverage that file list propagates into job inputs (where
    applicable).

- **Validation**
  - `CGO_ENABLED=0 go test ./...`

---

## Phase E – Trajectory capture and export follow-ups

### Status note: E1 doc drift

- The naming drift around correlation (`meta.correlation_id` vs `trace_id`) is
  addressed in docs. No further work is required unless the code behavior
  changes.

### PR E2 – Capture agent tool calls/results as trajectory events

- **Scope**
  - Add capture hooks in the tool execution path so that when an agent calls:
    - `code.symbol_search`
    - `code.swe_grep` the system persists `TrajectoryEvent` rows (`tool_call`,
      `tool_result`).
  - Ensure correlation uses envelope `meta.correlation_id` and persists as
    `trace_id`.

- **Tests**
  - Add regression tests that:
    - tool call + result events are persisted
    - event correlation is consistent

- **Validation**
  - `CGO_ENABLED=0 go test ./...`

### PR E3 – Implement `trajectory.export`

- **Scope**
  - Add an export path (job and/or CLI surface) that:
    - Reads stored trajectories and events.
    - Emits episode NDJSON per `docs/spec/dspy_trajectory_capture.md`.
    - Uses inline thresholding and CAS for large outputs (Core Profile v1
      rules).
  - Add golden episodes under `test/golden/trajectories/` and assert exports
    match.

- **Validation**
  - `CGO_ENABLED=0 go test ./...`

---

## 4. Verification checklist (applies to every PR)

- `make fmt`
- `make lint`
- `CGO_ENABLED=0 go test ./...`

Each PR should:

- Include tests/goldens for any new behavior.
- Keep envelope contracts stable (no new fields).
- Avoid logging secrets or large snippet bodies.
