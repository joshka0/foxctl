# Implementation Plan: Universal SWE Grep, Symbol Index, and Agents

Status: Draft\
Scope: From review gate and post-review pipeline through semantic index, symbol
index, SWE Grep, dspy-go tools, trajectories, and teams.

This plan orders work **by dependency** so pieces can ship incrementally without
spec drift.

Related specs:

- `docs/spec/review_gate.md`
- `docs/spec/semantic_file_index.md`
- `docs/spec/code_symbol_index_and_swe_grep.md`
- `docs/spec/post_review_harness.md`
- `docs/spec/dspy_go_agents.md`
- `docs/spec/dspy_trajectory_capture.md`
- `docs/spec/task_graph_insights.md`
- `docs/spec/skills_spec/README.md`
- `docs/specs/2025-12-13_universal_swe_grep_followups.md`

Related impl plan docs:

- `docs/impl_plan/universal_swe_grep_and_agents_specs_phase1_review_gate_todo.md`
- `docs/impl_plan/universal_swe_grep_and_agents_specs_phase2_post_review_harness_todo.md`
- `docs/impl_plan/universal_swe_grep_and_agents_deferred.md` – deferred work
  tracker
- `docs/impl_plan/universal_swe_grep_and_agents_codemap.md`

---

## Phase 0 – Pre-flight & Guardrails

**Goals**

- Confirm that existing infra (tasks DB, jobs, CAS, hooks, vector build tags) is
  ready to support new features.
- Lock in guardrails so later phases do not accidentally bypass them.

**Tasks**

- [x] Verify current state of:
  - Jobs + CAS integration.
  - `task_guard`, `file_guard`, `test_watch_feedback` hooks.
  - Named memory + optional vector support.
- [x] Add/confirm CI checks for:
  - Lint, vet, `go test ./...` (with `-race` when feasible).
  - No CGO dependencies for new code.
- [x] Document in `README.md` / `ARCHITECTURE.md` where post-review and indexing
      fit in the execution picture (pointer to
      `review_semantic_trajectory_specs.md`).

Dependencies: none.

---

## Phase 1 – Review Gate v1 (Kernel-Owned Dirtying)

**Goals**

- Implement the review gate semantics from `review_gate.md`, with **kernel-owned
  dirtying** and auto-demotion to `in_progress`.

**Tasks**

- [x] Extend `todo/manage` operations:
  - `review_request`, `complete`, `review_status` as per spec.
- [x] Implement review artifact storage + CAS logs.
- [x] Implement `hooks/task_guard` dirtying behavior:
  - When a task is `ready_for_review` or `completed` and a write occurs under
    its scope:
    - Auto-demote status to `in_progress`.
    - Mark previous `ok` review as stale in task fields / derived views.
- [ ] Wire minimal static pipeline:
  - Lint, fast tests, and optional CI-style checks.
  - Plug into existing `tests.run` / CI helpers.

Dependencies: Phase 0.

---

## Phase 2 – Post-Review Pipeline Harness

**Goals**

- Create a generalized **post-review indexer harness** used by semantic + symbol
  indexers.

**Tasks**

- [x] Implement overseer post-review handler:
  - Triggered when a review artifact transitions to `ok` and diff is applied.
  - Collects
    `(workspace_id, task_id, review_id, files[{path, digest, change_kind}])`.
    - Note: `files` is currently empty until the diff application layer is wired
      (see `universal_swe_grep_and_agents_deferred.md` D1).
- [x] Implement configuration for `indexing.post_review.indexers` as in
      `semantic_file_index.md` §8.2.
- [x] Emit a single internal event/command consumed by downstream indexers
      (semantic, symbol, future indexers).

Dependencies: Phase 1.

---

## Phase 3 – Semantic File Index v1

**Goals**

- Implement `semantic_file_index` with fixed, config-driven chunking and jobs
  for `semantic_index.init_files` / `semantic_index.update_files`.

**Tasks**

- [x] Implement named-memory schema + helper lib for:
  - `type = "file_embedding"` and `"file_embedding_chunk"`.
  - Stable names and `chunking_config_hash`.
- [x] Implement embedding jobs:
  - `semantic_index.init_files` and `semantic_index.update_files` with the
    data/behavior from §6.
- [x] Wire to post-review handler:
  - Subscribe to Phase 2 output and reindex touched files.
- [x] Implement optional CLI (`foxctl semantic-index ...`).
- [x] Add tests + golden outputs for:
  - No chunking.
  - Chunking and config changes.
  - Error codes in §11.

Dependencies: Phase 2.

---

## Phase 4 – Code Symbol Index v1 (Go-First)

**Goals**

- Implement symbol index storage and a Go-only indexer (Go AST), with per-symbol
  incremental updates.

**Tasks**

- [x] Implement named-memory-backed storage for `symbols` and `file_meta` per
      §3.
- [x] Implement a Go-first extractor using the standard library (`go/ast`) and
      compute `body_digest` / `content_hash`.
- [x] Implement `code_symbol_index.init_files` /
      `code_symbol_index.update_files` job entrypoints and per-symbol
      incremental updates per §4.3.
- [x] Wire to post-review handler.
- [x] Add tests for basic indexing, large-file handling, and incremental
      stability when only some symbols change.

Dependencies: Phase 2 (and Phase 3 for shared patterns, but not strictly).

---

## Phase 5 – SWE Grep Skill (`code/snippet_extract`)

**Goals**

- Implement the `code/snippet_extract` exec skill that reads **live workspace files**
  and emits high-signal snippets.

**Tasks**

- [x] Implement `code/snippet_extract` binary/skill:
  - Input / output contracts per §5.
  - Live reads via `PathValidator` and `AGENTCTL_WORKSPACE`.
  - CAS artifact emission for large results.
- [x] Start with a simple scoring heuristic or very small LM stub; keep the
      contract stable so we can later swap the model.
- [ ] Follow-ups (spec/impl drift):
  - Align error codes (`E_SWE_GREP_NO_CANDIDATES`, `E_FILE_NOT_FOUND`,
    `E_GUARD_VIOLATION`) and artifactization policy (inline thresholding).
  - Add goldens that cover inline vs CAS threshold behavior.

Dependencies: Phase 4 (for best candidates) but can be prototyped earlier with
semantic index + grep candidates.

---

## Phase 6 – dspy-go Tools & Agents Wiring

**Goals**

- Expose the new retrieval primitives to agents while keeping indexing/SWE Grep
  **kernel-owned**.

**Tasks**

- [x] Implement `code.symbol_search` tool:
  - Go helper over the symbol index per `code_symbol_index_and_swe_grep.md`
    §6.1.
- [x] Implement `code.swe_grep` tool:
  - Thin wrapper that calls the `code/snippet_extract` skill.
- [x] Align edit tools with `code/diff`:
  - Use `edit.apply_structured_diff` to consume the structured JSON diff from
    the `code/diff` skill.
  - Keep `edit.apply_patch` as a simple text-replacement helper.
- [x] Update dspy-go agent configs to include the new tools in Coding/Review
      agent signatures.

Dependencies: Phases 3–5.

---

## Phase 7 – Trajectory Capture & Export

**Goals**

- Ensure that symbol search + SWE Grep usage is captured in trajectories and
  that exports are usable for training.

**Tasks**

- [x] Implement initial trajectory store + capture hooks for `foxctl run`:
  - Capture `user_request` and `tool_result` events.
  - Use `meta.correlation_id` as the correlation source for capture (stored as
    `trace_id` in trajectory records).
- [ ] Map agent tool calls (including `code.symbol_search` and `code.swe_grep`)
      to `TrajectoryEvent` kinds (`tool_call`/`tool_result` plus optional
      `graph_search` / `swe_grep`).
- [ ] Implement `trajectory.export` job/skill per `dspy_trajectory_capture.md`
      §7.
- [ ] Add tests to verify:
  - Correlation via correlation id / trace id, `task_id`, `cas_digest`.
  - Presence of review artifacts and retrieval events in exported episodes.

Dependencies: Phases 1, 3–6.

---

## Phase 8 – Teams & Routing

**Goals**

- Back teams with a concrete store and optional skills so overseer and UIs can
  route work using `team:<slug>`.

**Tasks**

- [x] Implement SQLite tables `teams` and `team_members` per `dspy_go_agents.md`
      §4.3.
- [ ] Optionally implement `teams/manage.*` skills from
      `docs/spec/skills_spec/README.md`:
  - `teams/manage.list`, `.describe`, `.upsert`, `.add_member`,
    `.remove_member`.
- [x] Integrate mailbox routing for `team:<slug>` recipients.
- [ ] Integrate teams into overseer planning where helpful.

Dependencies: Phases 0–2; independent of retrieval, but more useful after agents
and graph insights exist.

---

## Phase 9 – Polish, Examples, and CI Hardening

**Goals**

- Make the new pipeline easy to adopt and safe to evolve.

**Tasks**

- [ ] Add example workflows under `docs/examples/` showing:
  - End-to-end: task → code changes → review → post-review indexing → agent use
    of SWE Grep and symbol search.
- [ ] Tighten CI:
  - Golden tests for envelopes, CAS artifacts, and retrieval behavior.
  - Optional integration tests for `code/snippet_extract` and indexing on a small
    sample repo.
- [ ] Update top-level docs (`AGENTS.md`, `ARCHITECTURE.md`, README) to link to
      specs and this implementation plan.

Dependencies: all prior phases.
