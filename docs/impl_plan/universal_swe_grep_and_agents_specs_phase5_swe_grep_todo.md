# Phase 5 – SWE Grep Skill (`code/swe_grep`) Todo Spec

This spec breaks down Phase 5 of `universal_swe_grep_and_agents` into concrete
steps focused on the **SWE Grep exec skill** that reads live workspace files and
emits high-signal snippets via Protocol v1 envelopes and CAS.

- Phase 3/4 provide semantic + symbol index candidates.
- Phase 5 consumes those candidates in a kernel-owned `code/swe_grep` skill,
  relying on semantic + symbol index scoring for **recall**, and using cheap
  LLMs for **snippet extraction** alongside PathValidator and CAS, without
  introducing new wire-level fields.

> **Cross-refs**
> - Impl plan: `docs/impl_plan/universal_swe_grep_and_agents.md` (Phase 5)
> - Codemap: `docs/impl_plan/universal_swe_grep_and_agents_codemap.md` (Phase 5)
> - Testing plan: `docs/impl_plan/universal_swe_grep_and_agents_testing.md` (Phase 5)
> - Specs:
>   - `docs/spec/code_symbol_index_and_swe_grep.md` (SWE Grep §5)
>   - `docs/spec/dspy_go_agents.md` (`code.swe_grep` tool)
>   - `docs/spec/core_profile_v1.md`
>   - `docs/spec/review_gate.md`, `docs/spec/semantic_file_index.md`,
>     `docs/spec/code_symbol_index_and_swe_grep.md` (funnel context)
> - Runners & CAS:
>   - Exec/WASI runners: see execution runner codemap.
>   - CAS & envelopes: see CAS codemaps and `core_profile_v1`.

---

## A. Skill Contract & Manifest

Goal: define a **stable kernel-owned skill** `code/swe_grep` with clear input /
output contracts, manifest, and CLI surface.

### A1. Skill manifest and wiring

- [ ] Add a new exec skill under `skills/code_swe_grep/` (or equivalent):
  - `skills/code_swe_grep/main.go` – skill implementation.
  - `skills/code_swe_grep/skill.yaml` – manifest with:
    - `metadata.name: "code/swe_grep"`.
    - `distribution.type: "exec"`.
    - `capabilities.network: "none"` (no network required).
    - `capabilities.filesystem: [ { type: "workdir" } ]`.
- [ ] Ensure manifest passes existing policy checks:
  - WASI not used (exec only); no changes to WASI rules.
  - Filesystem capabilities limited to workspace via `workdir`.
- [ ] Add basic docs snippet in `docs/start/` referencing this Phase 5 spec and
  `code_symbol_index_and_swe_grep.md` §5.

### A2. CLI/runner integration

- [ ] Ensure `agentctl run code/swe_grep` works end-to-end:
  - Discovery via existing skill resolver (`skills_run.go` + `skill_helpers.go`).
  - Execution via exec runner with `AGENTCTL_WORKSPACE` correctly set.
- [ ] Decide whether any dedicated CLI aliases (e.g. `agentctl code swe-grep`)
  are needed for developer UX; if added, they MUST be thin wrappers around the
  existing `run` path and reuse Protocol v1 envelopes.

---

## B. Input/Output Shape and Path Safety

Goal: implement the **input and output contracts** from the SWE Grep spec,
reusing existing path validation and CAS rules.

### B1. Input validation

- [ ] Implement input decoding in `main.go` consistent with
  `code_symbol_index_and_swe_grep.md` §5.2:
  - Required: `workspace_id`, `question`, `candidates[]`.
  - Each candidate: `path` (required), optional `symbol_id`, optional
    `priority`.
  - Optional `limits`: `max_files`, `max_snippets`, `max_bytes_per_file`.
- [ ] Add argument validation and error envelopes using existing helpers:
  - On empty or invalid `candidates`, return `E_SWE_GREP_NO_CANDIDATES`.
  - On malformed inputs, return `EARG` (via `ValidationError` or equivalent)
    per `core_profile_v1`.

### B2. Path validation and live reads

- [ ] Use `skillslib.RunnerContext` + `policy.PathValidator` for all filesystem
  access:
  - Derive workspace from `AGENTCTL_WORKSPACE` (runner environment).
  - Validate each candidate `path` before opening files.
- [ ] Map filesystem-related failures to the correct error codes:
  - Path escapes or blocked by `task_guard` / `file_guard` → `E_GUARD_VIOLATION`.
  - File missing after validation → `E_FILE_NOT_FOUND`.
- [ ] Ensure SWE Grep **always** reads from live workspace files (never from
  symbol/semantic index storage) to honor the freshness guarantees in §4.5.

### B3. Output envelope and CAS behavior

- [ ] Implement output shape per §5.3 of the spec:
  - `data.summary` with `files_considered`, `files_relevant`,
    `snippets_emitted`.
  - `data.snippets_inline[]` when total output is small enough.
  - Optional `data.artifact`, `data.artifact_kind`, `data.artifact_size_bytes`
    when results are large (NDJSON in CAS).
- [ ] Use `skillslib.RunnerContext.Emit()` to ensure:
  - `data.artifact` is a `sha256:<hex>` digest.
  - `meta.cas_digest` is auto-populated and matches `data.artifact`.
  - Envelopes pass `protocol.Validate` and existing golden CAS rules.
- [ ] Document NDJSON artifact format (without introducing new wire types):
  - One snippet per line with `file`, optional `symbol_id`, `start_line`,
    `end_line`, and full `text`.

---

## C. Scoring, Limits, and Model Integration

Goal: provide a **simple, deterministic initial implementation** of SWE Grep
scoring and limits that can be upgraded later without breaking contracts.

### C1. Scoring and snippet extraction

- [ ] Treat **recall / scoring** as an upstream concern:
  - Rely on semantic file index + symbol index (and their DAG/call-graph
    context) to rank and down-select candidate files/symbols.
  - Do not introduce a second, independent full-repo retrieval scorer inside
    `code/swe_grep`; respect upstream ranking and any candidate limits.
- [ ] Use a cheap **LLM-based snippet extractor** per candidate:
  - For each candidate file/symbol, call a small model (or a deterministic stub
    in tests/CI) to decide which spans are relevant to `question`.
  - Keep this LLM strictly inside the kernel-owned skill; the wire contract
    remains pure JSON envelopes and CAS artifacts.
- [ ] Extract snippets per candidate file based on the extractor output:
  - Identify relevant line ranges and build `snippets_inline` entries with
    `file`, optional `symbol_id`, `start_line`, `end_line`, and `preview`.
  - Ensure previews are safe-length and avoid leaking excessive data when
    combined with CAS artifacts.

### C2. Limits and performance

- [ ] Implement and enforce `limits` from input:
  - `max_files`: upper bound on candidate files to inspect.
  - `max_snippets`: global cap on emitted snippets.
  - `max_bytes_per_file`: cap on bytes read per file.
- [ ] Add internal defaults for limits if not supplied, documented in the spec
  or skill README.
- [ ] Ensure the skill remains fast enough for interactive use; document any
  cases where long-running behavior suggests using jobs or batched calls
  (without changing wire semantics).

---

## D. Tests, Golden Fixtures, and Observability

Goal: validate the SWE Grep skill thoroughly, including **path safety, CAS
integration, and snippet behavior**, and make it observable in logs and metrics.

### D1. Unit / behavior tests

- [ ] Add unit-style tests around `main.go` logic using small fixture files:
  - Given a small fixture and candidates, assert:
    - `summary.files_considered`, `files_relevant`, `snippets_emitted` are
      correct.
    - Snippets cover expected line ranges and paths.
  - Test limits behavior for `max_files`, `max_snippets`, `max_bytes_per_file`.

### D2. Error tests

- [ ] Add tests for failure modes and error codes per spec §5.4:
  - Path validation failures → `E_GUARD_VIOLATION`.
  - Non-existent candidate file → `E_FILE_NOT_FOUND`.
  - Empty / unusable candidates → `E_SWE_GREP_NO_CANDIDATES`.
  - Internal runtime errors (e.g. injected failure) → `ERUNTIME`.
- [ ] Ensure error envelopes:
  - Use `status: "error"`.
  - Set `error.code` to one of the allowed values.
  - Optionally include `data.hint` / `error.details` for debugging.

### D3. Golden / CAS tests

- [ ] Add golden fixtures for SWE Grep outputs:
  - Example OK envelope JSON(s) under `test/golden/envelopes/`
    (e.g. `ok-code_swe_grep-inline.json`, `ok-code_swe_grep-cas.json`).
  - Example NDJSON snippets artifact under `test/golden/swe_grep/` (or similar).
- [ ] Extend `test/golden/golden_test.go` (or a sibling) to validate:
  - Envelopes conform to `core_profile_v1`.
  - `meta.cas_digest` == `data.artifact` when present.
  - NDJSON artifact lines parse as expected snippet objects.

### D4. Integration tests (query → candidates → SWE Grep)

- [ ] Add an integration-style test that wires a simple candidate generator to
  SWE Grep:
  - Use a small fixture repo.
  - Generate candidates (e.g. via a trivial grep or hard-coded list).
  - Run `code/swe_grep` and assert the end-to-end flow behaves as expected.
- [ ] Optionally add a test that combines semantic/symbol index candidates with
  SWE Grep to verify the Phase 3/4 → 5 funnel on fixtures.

### D5. Logging and metrics

- [ ] Decide on minimal logging fields for SWE Grep:
  - workspace id, question hash (not full text), number of candidates,
    files_considered, files_relevant, snippets_emitted, CAS artifact presence.
- [ ] Add metrics hooks (aligned with existing infra) to capture:
  - Counters: `swe_grep_requests_total`, `swe_grep_errors_total`,
    `swe_grep_snippets_total`.
  - Histograms: `swe_grep_duration_seconds`, distribution of
    `files_considered` / `snippets_emitted`.
- [ ] Ensure logs do **not** leak sensitive file contents beyond what is
  necessary for debugging (snippets themselves travel via normal envelopes and
  CAS, not logs).

---

## Open Questions / To Discuss

- What initial scoring heuristic or small-model integration is appropriate for
  v1 without introducing heavy dependencies or latency?
- What default limits (`max_files`, `max_snippets`, `max_bytes_per_file`) strike
  the right balance between recall and speed for typical repos?
- Do we need a jobs-backed variant for very large candidate sets, or is
  synchronous exec sufficient for Phase 5?
