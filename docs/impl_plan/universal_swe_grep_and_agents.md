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
- `docs/spec/dspy_go_agents.md`
- `docs/spec/dspy_trajectory_capture.md`
- `docs/spec/task_graph_insights.md`
- `docs/spec/skills_spec/README.md`

---

## Phase 0 – Pre-flight & Guardrails

**Goals**

- Confirm that existing infra (tasks DB, jobs, CAS, hooks, vector build tags) is
  ready to support new features.
- Lock in guardrails so later phases do not accidentally bypass them.

**Tasks**

- [ ] Verify current state of:
  - Jobs + CAS integration.
  - `task_guard`, `file_guard`, `test_watch_feedback` hooks.
  - Named memory + optional vector support.
- [ ] Add/confirm CI checks for:
  - Lint, vet, `go test ./...` (with `-race` when feasible).
  - No CGO dependencies for new code.
- [ ] Document in `README.md` / `ARCHITECTURE.md` where post-review and indexing
      fit in the execution picture (pointer to
      `review_semantic_trajectory_specs.md`).

Dependencies: none.

---

## Phase 1 – Review Gate v1 (Kernel-Owned Dirtying)

**Goals**

- Implement the review gate semantics from `review_gate.md`, with **kernel-owned
  dirtying** and auto-demotion to `in_progress`.

**Tasks**

- [ ] Extend `todo/manage` operations:
  - `review_request`, `complete`, `review_status` as per spec.
- [ ] Implement review artifact storage + CAS logs.
- [ ] Implement `hooks/task_guard` dirtying behavior:
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

- [ ] Implement overseer post-review handler:
  - Triggered when a review artifact transitions to `ok` and diff is applied.
  - Collects
    `(workspace_id, files[{path, digest, change_kind}], task_id, review_id)`.
- [ ] Implement configuration for `indexing.post_review.indexers` as in
      `semantic_file_index.md` §8.2.
- [ ] Emit a single internal event/command consumed by downstream indexers
      (semantic, symbol, future indexers).

Dependencies: Phase 1.

---

## Phase 3 – Semantic File Index v1

**Goals**

- Implement `semantic_file_index` with fixed, config-driven chunking and jobs
  for `semantic_index.init_files` / `semantic_index.update_files`.

**Tasks**

- [ ] Implement named-memory schema + helper lib for:
  - `type = "file_embedding"` and `"file_embedding_chunk"`.
  - Stable names and `chunking_config_hash`.
- [ ] Implement embedding jobs:
  - `semantic_index.init_files` and `semantic_index.update_files` with the
    data/behavior from §6.
- [ ] Wire to post-review handler:
  - Subscribe to Phase 2 output and reindex touched files.
- [ ] Implement optional CLI (`agentctl semantic-index ...`).
- [ ] Add tests + golden outputs for:
  - No chunking.
  - Chunking and config changes.
  - Error codes in §11.

Dependencies: Phase 2.

---

## Phase 4 – Code Symbol Index v1 (Go-First)

**Goals**

- Implement symbol index storage and a Go-only indexer using Tree-sitter, with
  per-symbol incremental updates.

**Tasks**

- [ ] Define SQLite schema (or equivalent) for `symbols`, `calls`, `file_meta`
      per §3.
- [ ] Integrate Tree-sitter for Go:
  - Parse files, extract symbols, compute `body_digest` and `content_hash`.
- [ ] Implement `code_symbol_index.update_files` job:
  - Takes the same input envelope shape as semantic index jobs.
  - Performs per-symbol incremental updates as in §4.3.
- [ ] Wire to post-review handler + optional git-commit heuristics.
- [ ] Add tests for:
  - Basic indexing.
  - Renames and large files.
  - Incremental stability when only some symbols change.

Dependencies: Phase 2 (and Phase 3 for shared patterns, but not strictly).

---

## Phase 5 – SWE Grep Skill (`code/swe_grep`)

**Goals**

- Implement the `code/swe_grep` exec skill that reads **live workspace files**
  and emits high-signal snippets.

**Tasks**

- [ ] Implement `code/swe_grep` binary/skill:
  - Input / output contracts per §5.
  - Live reads via `PathValidator` and `AGENTCTL_WORKSPACE`.
  - CAS artifact emission for large results.
- [ ] Start with a simple scoring heuristic or very small LM stub; keep the
      contract stable so we can later swap the model.
- [ ] Add tests covering:
  - Path validation and guard violations.
  - Small vs large result sets (inline vs CAS).
  - Error codes: `E_SWE_GREP_NO_CANDIDATES`, `E_FILE_NOT_FOUND`, etc.

Dependencies: Phase 4 (for best candidates) but can be prototyped earlier with
semantic index + grep candidates.

---

## Phase 6 – dspy-go Tools & Agents Wiring

**Goals**

- Expose the new retrieval primitives to agents while keeping indexing/SWE Grep
  **kernel-owned**.

**Tasks**

- [ ] Implement `code.symbol_search` tool:
  - Go helper over the symbol index per `code_symbol_index_and_swe_grep.md`
    §6.1.
- [ ] Implement `code.swe_grep` tool:
  - Thin wrapper that calls the `code/swe_grep` skill.
- [ ] Align `edit.apply_patch` with `code_diff`:
  - Ensure tool input uses the JSON diff emitted by the `code_diff` skill.
  - Add tests for round-tripping (`code_diff` → agent edit →
    `edit.apply_patch`).
- [ ] Update dspy-go agent configs to include the new tools in Coding/Review
      agent signatures.

Dependencies: Phases 3–5.

---

## Phase 7 – Trajectory Capture & Export

**Goals**

- Ensure that symbol search + SWE Grep usage is captured in trajectories and
  that exports are usable for training.

**Tasks**

- [ ] Map `code.symbol_search` and `code.swe_grep` to `TrajectoryEvent` kinds
      (`tool_call`/`tool_result` plus optional `graph_search` / `swe_grep`).
- [ ] Implement `trajectory.export` job/skill per `dspy_trajectory_capture.md`
      §7.
- [ ] Add tests to verify:
  - Correlation via `meta.trace_id`, `meta.task_id`, `meta.cas_digest`.
  - Presence of review artifacts and retrieval events in exported episodes.

Dependencies: Phases 1, 3–6.

---

## Phase 8 – Teams & Routing

**Goals**

- Back teams with a concrete store and optional skills so overseer and UIs can
  route work using `team:<slug>`.

**Tasks**

- [ ] Implement SQLite tables `teams` and `team_members` per `dspy_go_agents.md`
      §4.3.
- [ ] Optionally implement `teams/manage.*` skills from
      `docs/spec/skills_spec/README.md`:
  - `teams/manage.list`, `.describe`, `.upsert`, `.add_member`,
    `.remove_member`.
- [ ] Integrate with mailbox routing and overseer planning where helpful.

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
  - Optional integration tests for `code/swe_grep` and indexing on a small
    sample repo.
- [ ] Update top-level docs (`AGENTS.md`, `ARCHITECTURE.md`, README) to link to
      specs and this implementation plan.

Dependencies: all prior phases.
