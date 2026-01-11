# Testing Plan: Universal SWE Grep, Symbol Index, and Agents

Status: Draft\
Scope: Testing and CI strategy for the implementation phases in
`universal_swe_grep_and_agents.md`.

This plan mirrors the implementation phases and defines **what must be tested
before moving to the next phase**. It assumes:

- Tests run via `make` targets (e.g. `make fmt`, `make lint`, `go test ./...`).
- No network access in tests by default.
- CGO disabled (`CGO_ENABLED=0`) unless explicitly required.

## Cross-cutting principles

- **No phase rolls forward without tests**
  - Each phase must ship with:
    - Unit tests for local logic.
    - Integration tests where external components interact (jobs, CAS, hooks).
    - Golden tests for envelopes / artifacts where shapes are important.
- **Stable fixtures, small repos**
  - Prefer a few small, curated fixtures over large real-world repos.
  - Keep test data in `test/golden` and `tests/fixtures`.
- **Determinism**
  - Avoid reliance on wall-clock timing, network latency, or non-deterministic
    ordering in assertions.

---

## Phase 0 – Pre-flight & Guardrails

**Focus:** verify that the existing infra can bear new work.

- **Unit/infra checks**
  - [x] Ensure `go test ./...` is green on main.
  - [x] Ensure `make lint` / `golangci-lint` pass with reasonable time bounds.
  - [x] Add a small test that exercises:
    - CAS put/get with digest verification.
    - A simple jobs submit + tail.

- **Hook sanity**
  - [x] If not already covered, add tests that:
    - `task_guard` blocks writes without a task id.
    - `file_guard` reserves a path for a task and blocks conflicting
      reservations.

Gate: no new features until this is solid.

---

## Phase 1 – Review Gate v1

**Focus:** correct task lifecycle, review artifacts, and dirtying.

- **Unit tests** (task + review logic)
  - [ ] Task lifecycle transitions:
    - `pending → in_progress → ready_for_review → completed`.
    - `ready_for_review → in_progress` on failed review.
  - [x] `todo/manage.review_request` behavior:
    - Valid/invalid task states.
    - Creation of review artifacts.
  - [x] `todo/manage.complete` semantics when gate is enabled:
    - Requires a recent `ok` review.
    - Fails or triggers review when missing.

- **Dirtying/`task_guard` tests**
  - [x] When a task is `ready_for_review` or `completed` and a write occurs
        under its scope:
    - Task status is auto-demoted to `in_progress`.
    - Any prior `ok` review is treated as stale (e.g. `last_review_status` no
      longer authorizes completion).

- **Integration / e2e**
  - [ ] End-to-end scenario with a small fixture repo:
    - Create task, edit file, request review, pass minimal checks, call
      `todo/manage.complete`, assert final state.
    - Then perform a new write in the same scope and ensure status and review
      fields reflect dirtying.

---

## Phase 2 – Post-Review Pipeline Harness

**Focus:** correct plumbing from `ok` review → post-review indexer inputs.

- **Unit tests** (handler shape)
  - [x] Given a review artifact with `inputs.files` and `inputs.diff_digest`,
        assert the handler produces the expected input payload:
    - `(workspace_id, files[{path, digest, change_kind}], task_id, review_id)`.
    - `internal/analysis/overseer/post_review_test.go` – 6 tests covering happy
      path, idempotence, non-ok rejection, files passthrough.
    - `internal/indexing/postreview/producer_test.go` – event building tests.

- **Integration tests**
  - [x] Simulate a successful review and ensure:
    - A post-review event/job is enqueued once and only once.
    - Multiple indexers can subscribe (semantic + symbol index) without
      interfering.
    - `TestPostReviewHandler_IntegrationWithFakeIndexer` – full flow with fake
      indexer subscriber.
    - `internal/indexing/handler_test.go` – multi-indexer fanout tests.

- **Golden tests**
  - [x] Capture one or two canonical post-review events as JSON in
        `test/golden/envelopes/` and assert their shape does not change
        accidentally.
    - `test/golden/envelopes/post_review_event.json` – canonical event shape.
    - `test/golden/golden_test.go` `TestGoldenPostReviewEvent` – shape
      validation.

Gate: post-review handler must be stable before semantic/symbol index jobs rely
on it.

---

## Phase 3 – Semantic File Index v1

**Focus:** stable naming, chunking, and embedding jobs.

- **Unit tests**
  - [x] Naming + chunk ID stability:
    - For a given `(workspace, path, chunk_bytes, overlap, cfg hash)`, ensure
      `file_embedding` / `file_embedding_chunk` names and spans are stable
      across re-runs.
  - [x] Behavior when config changes:
    - New `chunking_config_hash` produces a new set of chunk entries.
    - Old entries are marked deprecated or removed per spec.

- **Job tests** (`semantic_index.init_files`, `semantic_index.update_files`)
  - [x] Given small fixture files, assert:
    - Correct number of `file_embedding` / `file_embedding_chunk` rows.
    - `result.digest` matches CAS contents or file snapshot.
  - [x] Update behavior:
    - Editing a file with no chunking reuses the same `name`, updates `digest`
      and `embedding`.
    - Editing a chunked file reuses `chunk.id` and spans.

- **Golden tests**
  - [x] Store example job result envelopes (summary + CAS artifact metadata) in
        `test/golden/envelopes/semantic-index-*.json`.

- **Integration tests**
  - [x] Post-review → semantic index:
    - Simulate a review, run post-review handler, assert semantic index job runs
      and produces expected entries.

---

## Phase 4 – Code Symbol Index v1

**Focus:** per-symbol parsing, call graph, and incremental updates.

- **Unit tests**
  - [x] Go extractor extraction for Go fixtures:
    - Symbols: ids, kinds, byte ranges.
    - Calls: edges between caller and callee.
  - [x] `file_meta` behavior:
    - Unchanged file content → indexer skips work.
    - Changed file content → only affected symbols re-embedded.

- **Incrementality tests**
  - [x] Add a new function to a file:
    - Only the new symbol gets an embedding.
  - [x] Modify one function in a “God file”:
    - Unchanged symbols keep their embeddings.

- **Golden tests**
  - [x] Store canonical fixture dumps under `test/golden/symbol_index/`.

- **Integration tests**
  - [x] Post-review triggered symbol index job:
    - Given a review touching multiple files, ensure only changed files are
      parsed and updated.

---

## Phase 5 – SWE Grep Skill (`code/snippet_extract`)

**Focus:** live reads, snippet extraction, and CAS behavior.

- **Unit/behavior tests**
  - [x] Given a small fixture file and candidates, assert:
    - Correct `summary` counts.
    - Snippets cover expected lines.
  - [x] Limits behavior (`max_files`, `max_snippets`, `max_bytes_per_file`).

- **Error tests**
  - [ ] Align error codes to spec:
    - Path validation failures → `E_GUARD_VIOLATION`.
    - Missing candidate file → `E_FILE_NOT_FOUND`.
    - No usable candidates → `E_SWE_GREP_NO_CANDIDATES`.

- **Golden / CAS tests**
  - [ ] NDJSON artifact shape for multi-snippet results.
  - [ ] If `meta.cas_digest` is set, it matches the `data.artifact` digest.

- **Integration tests**
  - [x] Combine with simple candidate generator (e.g. hard-coded candidates) and
        ensure a full query → candidates → SWE Grep flow behaves as expected on
        fixtures.
  - [ ] Add an inline-threshold test case where `artifact` is omitted when the
        output fits inline limits.

---

## Phase 6 – dspy-go Tools & Agents Wiring

**Focus:** tools surface, patch format, and funnel behavior from agents.

- **Tool unit tests**
  - [x] `code.symbol_search`:
    - Given symbol index data, returns ranked candidates with expected fields.
  - [x] `code.swe_grep` tool:
    - Correctly maps inputs to `code/snippet_extract` and unwraps results.

- **Patch round-trip tests**
  - [x] `code/diff` → `edit.apply_structured_diff`:
    - Run `code_diff` on a fixture change to produce JSON diff.
    - Feed that diff into `edit.apply_structured_diff` and assert the resulting
      file matches the expected content.

- **Agent integration tests** (non-LLM or stubbed LLM)
  - [ ] A stub dspy-go Coding agent that calls:
    - `code.symbol_search` → `code.swe_grep` → `edit.apply_patch` on a toy repo.
    - Ensure hooks fire and tasks move through expected states.

---

## Phase 7 – Trajectory Capture & Export

**Focus:** correctness of recorded events and exported episodes.

- **Unit tests**
  - [x] Capture hooks for `agentctl run`:
    - Ensure `user_request` + `tool_result` events are persisted and correlated.
    - Correlation uses `meta.correlation_id` (stored as `trace_id` internally).
  - [ ] Mapping from agent tool calls to `TrajectoryEvent`:
    - Ensure tool calls for `code.symbol_search` / `code.swe_grep` are correctly
      labeled and correlated.

- **Export tests**
  - [ ] `trajectory.export` job:
    - Given a small set of stored trajectories, produce NDJSON episodes.
    - Validate episode shapes against `dspy_trajectory_capture.md`.

- **Golden tests**
  - [ ] Store a small set of anonymized episodes in `test/golden/trajectories/`
        and assert exports match.

---

## Phase 8 – Teams & Routing

**Focus:** team storage and basic skills (if implemented).

- **Unit tests**
  - [x] CRUD over `teams` and `team_members` tables.

- **Skill tests** (if `teams/manage.*` is implemented)
  - [ ] `teams/manage.list`, `.describe`, `.upsert`, `.add_member`,
        `.remove_member`.

- **Integration tests**
  - [x] Mailbox routing to `team:<slug>` recipients.
  - [ ] Viewer-level queries that join tasks/agents with teams.

---

## Phase 9 – End-to-End & CI Hardening

**Focus:** full pipeline behavior and preventing regressions.

- **E2E flows**
  - [ ] On a small fixture workspace, exercise:
    - Task creation → edits → review → post-review indexing → agent retrieval
      (semantic + symbol + SWE Grep) → completion.
  - [ ] Capture logs/envelopes and assert key invariants:
    - Post-review triggers both semantic and symbol index jobs.
    - Retrieval events are present in trajectories.

- **CI wiring**
  - [ ] Add or refine `make` targets to run:
    - Unit + integration tests.
    - Golden tests checks (no uncommitted golden drift).
  - [ ] Ensure new tests are fast enough for PR CI; if slow, gate heavier tests
        behind explicit flags or nightly runs.

This testing plan should be updated as phases are refined or split; every new
phase or major feature in `universal_swe_grep_and_agents.md` should link back to
concrete tests here.
