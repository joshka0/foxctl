# Phase 9 – End-to-End Flows & CI Hardening Todo Spec

This spec breaks down Phase 9 of `universal_swe_grep_and_agents` into concrete
steps focused on **end-to-end flows, examples, and CI hardening**.

- Earlier phases build the review gate, indexing, SWE Grep, tools, trajectories,
  and teams.
- Phase 9 makes this pipeline **easy to adopt, observable, and hard to regress**
  via examples, E2E tests, and CI wiring.

> **Cross-refs**
> - Impl plan: `docs/impl_plan/universal_swe_grep_and_agents.md` (Phase 9)
> - Testing plan: `docs/impl_plan/universal_swe_grep_and_agents_testing.md` (Phase 9)
> - Specs:
>   - `docs/spec/review_gate.md`
>   - `docs/spec/semantic_file_index.md`
>   - `docs/spec/code_symbol_index_and_swe_grep.md`
>   - `docs/spec/dspy_go_agents.md`
>   - `docs/spec/dspy_trajectory_capture.md`
>   - `docs/spec/skills_spec/README.md`
> - Codemaps (from universal codemap Phase 9):
>   - CM3 – agentctl Envelope Protocol & CLI Pipeline.
>   - CM13 – Core Profile v1: End-to-End Envelope, Jobs & CAS Flow.
>   - CM14 – Test Infrastructure: Test-Watch, Feedback Hooks, CI Targets.
>   - CM17 – Test Infrastructure & CI Pipeline: Test Watcher, Makefile Targets, Coverage.

---

## A. Examples & Developer Workflows

Goal: add **clear, documented examples** that demonstrate the full pipeline on a
small workspace.

### A1. Example repos and fixtures

- [ ] Select or create one or two small fixture workspaces under `tests/fixtures/`
  or `docs/examples/` that exercise:
  - Tasks and review gate behavior.
  - Post-review indexing (semantic + symbol).
  - SWE Grep and tools (`code.symbol_search`, `code.swe_grep`).
  - Optional teams and trajectories (if enabled).
- [ ] Document constraints for these fixtures:
  - Small, deterministic, self-contained (no network).
  - Stable paths for use across tests and examples.

### A2. End-to-end example docs

- [ ] Add example workflows under `docs/examples/` (per impl plan Phase 9):
  - **Workflow 1:** Task → edits → review → post-review indexing → agent
    retrieval (semantic + symbol + SWE Grep) → completion.
  - **Workflow 2 (optional):** Agent-driven change using dspy-go tools + SWE
    Grep, followed by review and indexing.
- [ ] For each example, include:
  - CLI commands (`agentctl todo`, `agentctl run`, `agentctl dspy-agent`)
    showing the path end-to-end.
  - Pointers to relevant specs and Phase docs.
  - Notes on where envelopes/jobs/CAS artifacts end up on disk.

---

## B. End-to-End Flows & Invariants

Goal: implement **E2E tests** that traverse the full pipeline and assert key
invariants from previous phases.

### B1. Full pipeline tests (per testing plan Phase 9)

- [ ] Add one or more E2E tests that, on a small fixture workspace, exercise:
  - Task creation.
  - Code edits under `task_guard` / `file_guard`.
  - Review request + review gate behavior.
  - Post-review event → semantic + symbol index jobs.
  - Agent retrieval (semantic index, symbol index, SWE Grep tools).
  - Task completion.
- [ ] Capture and assert key invariants along the way:
  - Post-review triggers both semantic and symbol indexers once per review.
  - Semantic + symbol index entries exist and are consistent with diffs.
  - Retrieval events (`code.symbol_search`, `code.swe_grep`) show up in
    trajectories with correct `meta.trace_id` and `task_id`.

### B2. Golden envelopes and artifacts

- [ ] Extend golden coverage for envelopes and CAS artifacts (CM3, CM13):
  - Ensure `test/golden/envelopes/` contains canonical envelopes for new flows
    (if not already covered by earlier phases).
  - Ensure CAS-backed artifacts (e.g. SWE Grep NDJSON, trajectory exports) have
    example goldens where shapes are important.
- [ ] Add or refine tests that:
  - Validate golden envelopes with `protocol.Validate`.
  - Check `meta.cas_digest == data.artifact` where applicable.
  - Fail fast when shapes drift unexpectedly.

---

## C. CI Hardening & Test Infrastructure

Goal: ensure **CI consistently exercises the pipeline**, with clear targets and
reasonable performance, using existing infra (CM14, CM17).

### C1. Make targets and test suites

- [ ] Review and refine `Makefile` targets to cover:
  - `make fmt` / `make lint` / `go test ./...` (including race where feasible).
  - Targeted integration tests (e.g. `make test-e2e` or similar) for heavier
    flows.
  - Golden validation (e.g. `make test-golden` or a flag-driven subset).
- [ ] Ensure these targets are wired into CI (GitHub Actions) appropriately:
  - Fast, always-on suite for PRs.
  - Optional, slower E2E suite gated behind specific jobs or schedules.

### C2. Golden drift and regeneration

- [ ] Document and enforce a process for updating goldens:
  - When is it acceptable to re-record golden envelopes / artifacts.
  - How to review diffs for protocol/shape changes.
- [ ] If not already present, add a simple check that:
  - Fails when goldens change without being committed.
  - Encourages reviewers to inspect golden diffs.

### C3. Test watcher and feedback hooks

- [ ] Ensure existing `test_watch` / `hooks/test_feedback` integrations (CM14)
  remain compatible with new tests:
  - Confirm they run the relevant subsets for changed packages.
  - Optionally extend them to highlight retrieval/indexing/trajectory failures
    clearly.

---

## D. Top-Level Docs & Adoption

Goal: update **top-level docs** so maintainers and users can understand the
pipeline and adopt it safely.

### D1. Top-level docs refresh

- [ ] Update `README.md` to:
  - Summarize the new pipeline (review gate → indexing → agents → trajectories).
  - Link to key specs and impl plan phases.
- [ ] Update `AGENTS.md` to:
  - Reflect the current dspy-go agent roles, tools (`code.symbol_search`,
    `code.swe_grep`), and how they use the new retrieval primitives.
- [ ] Update `ARCHITECTURE.md` to:
  - Show where post-review, indexing, SWE Grep, trajectories, and teams live in
    the overall architecture.

### D2. Phase docs cross-linking

- [ ] Ensure each phase-specific todo spec and the main impl/testing plans link
  to each other where appropriate:
  - From `universal_swe_grep_and_agents.md` Phase 1–9 to their corresponding
    `*_todo.md` specs.
  - From testing plan sections back to both specs and impl plan.
- [ ] Optionally add a short `docs/start/` entry summarizing the phases and
  where to start when hacking on this pipeline.

---

## Open Questions / To Discuss

- What is the minimal, stable set of E2E scenarios we want to lock in as
  blocking tests vs optional/nightly ones?
- How aggressively should CI enforce golden stability (e.g. all goldens vs
  only envelope/trajectory-related ones)?
- Are there additional high-value examples we should include beyond the ones
  described in the impl plan (e.g. failure-mode walkthroughs, demo scripts)?
