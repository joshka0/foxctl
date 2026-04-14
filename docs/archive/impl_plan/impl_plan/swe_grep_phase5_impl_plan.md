### Phase 5 SWE Grep – Short Summary (3–5 sentences)

Phase 5 implements `code/snippet_extract` as a kernel‑owned **exec** skill that takes a
`workspace_id`, natural‑language `question`, and a list of candidate
files/symbols and returns high‑signal code snippets. It **always** reads live
workspace files off disk under `PathValidator` + `AGENTCTL_WORKSPACE`, so
snippets reflect the current tree even if semantic/symbol indices are slightly
stale. Phase 3/4 are responsible for **recall and scoring**: SWE Grep simply
consumes their ranked candidate set (plus optional `priority`) and does not do
its own full‑repo retrieval. Outputs follow Core Profile v1: small results stay
inline in `data.snippets_inline`, while large results are written as NDJSON
snippets into CAS with `data.artifact` (and optional `meta.cas_digest` matching
it), without adding new wire fields or changing error semantics.

---

## A/B/C/D Todo Structure – Sanity Check + Small Gaps

- **Section A (Skill contract & manifest)**
  - Matches the spec and codemap: `code/snippet_extract` as an **exec** skill under
    `skills/code_swe_grep/`, `network:"none"`, `filesystem: [{type:"workdir"}]`,
    discovery via existing resolver + exec runner.
  - **Minor addition to keep in mind:** explicitly note that `command` in
    envelopes is `"code/snippet_extract"` and must align with `metadata.name` and the
    dspy `code.swe_grep` tool later in Phase 6.

- **Section B (Input/output + path safety)**
  - B1/B2/B3 line up cleanly with §5.2–5.4 in
    [code_symbol_index_and_swe_grep.md](cci:7://file://docs/spec/code_symbol_index_and_swe_grep.md:0:0-0:0):
    input fields, path validation rules, and output envelope + CAS behavior are
    all consistent.
  - **Nice-to-have clarifications (already implied but worth treating as
    requirements in implementation):**
    - Explicitly keep SWE Grep **index‑agnostic**: it must not peek into
      semantic/symbol storage; it only sees `candidates[]`.
    - Be explicit that NDJSON artifact lines carry full `text`, while
      `snippets_inline.preview` is truncated.

- **Section C (Scoring, limits, model integration)**
  - Correctly captures the upstream/downstream separation: semantic/symbol index
    own scoring; SWE Grep owns **snippet extraction + limits** only.
  - Open questions (C1/C2) are intentionally left for implementation choice; we
    should treat “cheap LLM vs deterministic heuristic” and default limit values
    as **configurable knobs**, but the wire contract is already fully defined.

- **Section D (Tests, goldens, observability)**
  - D1–D4 map well to the spec and the test plan: unit/behavior tests, explicit
    error‑code tests, golden envelopes + NDJSON artifacts, and at least one
    end‑to‑end “candidates → SWE Grep” integration test.
  - D5 on logging/metrics is consistent with Core Profile v1 and review_gate:
    logs must not leak snippet contents; metrics and summary counters live
    outside the wire contract.

Net: A/B/C/D are aligned with the spec + codemap. The main “missing pieces” are
**where** tests/goldens actually live (we’ll fix that per‑PR below) and making
the dspy tool wrapper clearly **Phase 6** while keeping its contract in mind as
we design Phase 5.

---

## Proposed PRs for Phase 5 SWE Grep

### 1. PR 1 – Skill Skeleton, Manifest, and CLI Wiring

- **Scope**
  - `skills/code_swe_grep/skill.yaml` – exec manifest:
    - `metadata.name: "code/snippet_extract"`, `distribution.type: "exec"`,
    - `capabilities.network: "none"`,
      `capabilities.filesystem: [{type:"workdir"}]`.
  - `skills/code_swe_grep/main.go` – `main` + minimal `run` stub using
    `skillslib.RunnerContext`.
  - A short doc snippet in `docs/start/` (e.g. a one‑liner in an existing index)
    pointing to:
    - Phase 5 todo spec and
      [code_symbol_index_and_swe_grep.md](cci:7://file://docs/spec/code_symbol_index_and_swe_grep.md:0:0-0:0)
      §5.
  - No semantic behavior yet; just argument parsing stub + a simple
    `status:"error"` envelope on empty stdin.

- **Maps to Phase 5 todo**
  - **A1** (skill manifest and wiring).
  - **A2** (CLI/runner integration: “`foxctl run code/snippet_extract` works” at a
    smoke level).

- **Validation**
  - Manifest passes policy checks:
    - Reuse existing manifest tests and `scripts/checkmanifests` to ensure
      exec + `workdir` are accepted and WASI rules unchanged.
  - A tiny unit/integration test that:
    - Invokes the binary with empty/malformed stdin and asserts a valid Protocol
      v1 **error envelope** (correct `version`, `status:"error"`,
      `command:"code/snippet_extract"`).

---

### 2. PR 2 – Input Shape & Validation (`EARG`, `E_SWE_GREP_NO_CANDIDATES`)

- **Scope**
  - `skills/code_swe_grep/main.go`:
    - Define input struct exactly per §5.2:
      - `workspace_id`, `question`,
        `candidates[] { path, symbol_id?, priority? }`, optional
        `limits { max_files, max_snippets, max_bytes_per_file }`.
    - Implement `parseInput(io.Reader) (Input, error)` and a validation layer
      that:
      - Checks required fields.
      - Normalizes `limits` (e.g. zero/negative → treated as unset).
  - Optionally, small helper subtypes in the same file (no new package yet).

- **Maps to Phase 5 todo**
  - **B1** (input validation).
  - Part of **D2** (error behaviors for malformed/empty inputs).

- **Validation**
  - Table‑driven unit tests in `skills/code_swe_grep/main_test.go`:
    - Valid examples → parsed `Input` with populated fields.
    - Empty `candidates`/only unusable entries → error envelope with
      `error.code == "E_SWE_GREP_NO_CANDIDATES"`.
    - Malformed JSON / missing required keys → error envelope with
      `error.code == "EARG"`.
  - Use the existing envelope‑validation helper (`envelope.Validate` /
    `protocol.Validate`) in tests to assert envelopes are Core Profile
    v1–conformant.

---

### 3. PR 3 – Path Validation & Live Workspace Reads

- **Scope**
  - `skills/code_swe_grep/main.go`:
    - Instantiate `skillslib.RunnerContext` and use its `PathValidator`:
      - Workspace derived from `AGENTCTL_WORKSPACE` injected by the exec runner.
    - Implement the candidate loop:
      - For each candidate `path`, call `PathValidator.ValidatePath(path)`; only
        then `os.Open`.
      - Explicitly never read from index storage (semantic/symbol DB).
    - Map FS/path errors to spec’d error codes:
      - Path escape / guard block → `E_GUARD_VIOLATION`.
      - `os.IsNotExist` after validation → `E_FILE_NOT_FOUND`.
  - No snippet extraction yet; just read/validate, maybe count
    `files_considered`.

- **Maps to Phase 5 todo**
  - **B2** (path validation and live reads).
  - Part of **B1** (candidate‑level validation).
  - Part of **D2** (path/FS error codes).

- **Validation**
  - Unit/behavior tests (can run as pure Go tests with a temp workspace):
    - Valid path inside workspace → file opened successfully.
    - Path attempting `..` / symlink escape / blocked by guards →
      `status:"error"`, `error.code:"E_GUARD_VIOLATION"`.
    - Non‑existent path after validation → `error.code:"E_FILE_NOT_FOUND"`.
  - Optional small integration test that:
    - Sets `AGENTCTL_WORKSPACE` to a fixture directory and runs the skill via
      the runner helper to ensure env wiring is correct.

---

### 4. PR 4 – Output Envelopes, CAS Artifacts, and NDJSON Format

- **Scope**
  - `skills/code_swe_grep/main.go`:
    - Implement full **success** output shape per §5.3:
      - `data.summary.{files_considered, files_relevant, snippets_emitted}`.
      - `data.snippets_inline[] { file, symbol_id?, start_line, end_line, preview }`.
      - CAS artifact path:
        - When output is “large”, stream NDJSON snippets to CAS.
        - Set `data.artifact`.
        - `meta.cas_digest` is optional; if set it MUST match `data.artifact`.
    - Define the NDJSON **line schema** (internal) matching the spec:
      `{file, symbol_id?, start_line, end_line, text}`.
  - **Tests/goldens**:
    - Add golden OK envelopes to `test/golden/envelopes/`:
      - `ok-code_swe_grep-inline.json`.
      - `ok-code_swe_grep-cas.json`.
    - Add NDJSON sample under `test/golden/swe_grep/` (or similar).

- **Maps to Phase 5 todo**
  - **B3** (output envelope and CAS behavior).
  - **D3** (golden / CAS tests).

- **Validation**
  - Golden tests (extend the existing golden harness, e.g.
    `test/golden/golden_test.go` or sibling):
    - Load the SWE Grep envelope goldens and assert:
      - Envelope passes the Core Profile v1 validator.
      - If `meta.cas_digest` is set, it matches `data.artifact`.
    - Load the NDJSON artifact golden:
      - Parse each line into a snippet struct and assert required fields are
        present and consistent.
  - Unit tests for “inline vs CAS” threshold logic:
    - Small snippet sets → `artifact` omitted; all snippets in
      `snippets_inline`.
    - Large snippet sets → truncated `snippets_inline` + CAS artifact set.

---

### 5. PR 5 – Snippet Extraction Engine (LLM Stub) + Limits

- **Scope**
  - `skills/code_swe_grep/main.go` (or a small internal helper file under
    `skills/code_swe_grep/`):
    - Implement a pluggable **snippet extractor**:
      - Deterministic heuristic or local stub that can later be replaced with a
        small LM, but **does not change the wire contract**.
      - Takes a file’s text + `question` (+ optional `symbol_id`) and returns
        line ranges.
    - Enforce `limits`:
      - `max_files`, `max_snippets`, `max_bytes_per_file` from input or
        reasonable defaults (documented in code comments + spec doc if needed).
    - Compute:
      - `files_considered`, `files_relevant`, `snippets_emitted` in a
        deterministic way.
    - Respect upstream ordering:
      - Use `candidates` ordering / `priority` instead of re‑scoring via
        embeddings inside SWE Grep.

- **Maps to Phase 5 todo**
  - **C1** (scoring + snippet extraction).
  - **C2** (limits and performance).
  - **D1** (unit/behavior tests for summaries + limits).

- **Validation**
  - Unit tests on small fixture files under a `testdata/` dir in
    `skills/code_swe_grep/`:
    - Given question + candidates → predictable snippet spans and counts.
    - Limits:
      - `max_files` clamps how many candidates are processed.
      - `max_snippets` clamps global snippet count.
      - `max_bytes_per_file` clamps bytes read, but snippets remain internally
        consistent.
    - Verify aggregator:
      - `summary.files_considered`, `files_relevant`, `snippets_emitted` match
        expectation for each scenario.

---

### 6. PR 6 – Integration Test (Candidates → SWE Grep) + Observability Hooks

- **Scope**
  - A focused integration test (likely under `test/integration/`) that:
    - Sets up a small fixture workspace (few files with obvious matches).
    - Uses a trivial candidate generator (hard‑coded list or simple grep) to
      construct `candidates`.
    - Invokes `code/snippet_extract` via the normal runner path (or `foxctl run`
      harness).
    - Asserts end‑to‑end behavior:
      - Correct summary counts.
      - Snippets point at the right files/lines.
      - Inline vs CAS behavior matches thresholds.
  - Observability:
    - Add minimal logging around summary stats (question hash, number of
      candidates, `files_considered`, `files_relevant`, `snippets_emitted`, CAS
      present).
    - Optionally wire basic metrics counters/histograms if existing infra is
      already in place; otherwise, stub hooks with TODOs tracked elsewhere.

- **Maps to Phase 5 todo**
  - **D4** (integration tests).
  - **D5** (logging/metrics), at least to a minimal “not leaking contents, basic
    stats logged” level.

- **Validation**
  - Integration test passes on CI:
    - Uses Core Profile v1 validator on the returned envelope.
    - Ensures NDJSON artifact (when present) parses correctly.
  - Log/metrics sanity:
    - Tests assert that logs/metrics include only **aggregated** information (no
      raw snippet text), in line with D5’s privacy constraint.

---

### 7. (Cross‑Phase) PR 7 – dspy‑go Tool Wrapper `code.swe_grep` (Phase 6, Dependent on Phase 5)

> This is formally Phase 6 work but tightly coupled; including it here makes the
> dependency explicit.

- **Scope**
  - `internal/agent/tools/...` (where your dspy‑go tools live):
    - Implement `code.swe_grep` tool that:
      - Accepts `workspace_id`, `question`,
        `candidate_files[] { path, symbol_id?, priority? }`, optional limits.
      - Invokes the `code/snippet_extract` skill via the skills runner and maps the
        envelope’s `data.snippets_inline` / CAS artifact into the tool’s output
        schema as in §6.2 of the spec.
  - No new wire fields; the tool is a **thin mapper** over the skill.

- **Maps to Phase 5 todo**
  - Not part of A/B/C/D directly; it’s Phase 6 in the impl plan, but must
    respect:
    - **B3** (envelope + CAS semantics).
    - **D2/D3** error/OK shapes (the tool propagates them).

- **Validation**
  - dspy‑go level unit tests (without running an actual LM):
    - Stub a `code/snippet_extract` envelope and assert the tool decodes it correctly
      into the tool’s typed output.
    - Ensure errors (`E_GUARD_VIOLATION`, `E_FILE_NOT_FOUND`,
      `E_SWE_GREP_NO_CANDIDATES`, `ERUNTIME`, `ETIMEOUT`) are mapped to
      appropriate dspy error/blocked statuses.

---

## Cross‑Cutting Constraints & Dependencies to Keep Front‑of‑Mind

- **Path validation & workspace anchoring**
  - Use `skillslib.RunnerContext` everywhere in the skill.
  - Always derive workspace from `AGENTCTL_WORKSPACE` (set by the exec runner)
    and construct `PathValidator` with that workspace and allowed roots.
  - No direct `os.Open` on arbitrary paths; every file read flows through
    `PathValidator.ValidatePath`.

- **CAS usage and `meta.cas_digest` invariants**
  - Large outputs must:
    - Stream NDJSON to CAS via the standard CAS store.
    - Set `data.artifact` to `sha256:<hex>` digest returned by CAS.
    - If set, `meta.cas_digest` MUST be **exactly** the same digest.
  - Use the existing envelope helpers (`RunnerContext.Emit`, CAS helpers) so
    behavior matches other skills and existing goldens.

- **Error codes & envelope semantics**
  - Reuse only existing codes from the spec:
    - `E_GUARD_VIOLATION`, `E_FILE_NOT_FOUND`, `E_SWE_GREP_NO_CANDIDATES`,
      `ERUNTIME`, `ETIMEOUT`, plus `EARG` for input validation.
  - Error envelopes:
    - `status:"error"`, `error.code` set, and optionally `data.hint` /
      `error.details` for debugging (e.g. offending path).
  - Never invent new top‑level fields; follow Core Profile v1 (`version`,
    `status`, `command`, `data`, `meta`, `error`).

- **Agnostic to embeddings/scoring**
  - SWE Grep:
    - Treats `candidates` as **the** ranked set.
    - May respect `priority` as an advisory ordering but never calls embedding
      providers or indexes directly.
  - All embedding/scoring complexity stays in semantic/symbol indexers and their
    jobs/tools.

---

## Validation Overview (How We Know Phase 5 is “Done”)

- **Unit / behavior tests**
  - Input parsing & validation (PR 2): valid/invalid payloads, `EARG`,
    `E_SWE_GREP_NO_CANDIDATES`.
  - Path + FS behavior (PR 3): `E_GUARD_VIOLATION`, `E_FILE_NOT_FOUND`.
  - Snippet extraction + limits (PR 5): correct summary counts, line ranges, and
    enforcement of `max_files`, `max_snippets`, `max_bytes_per_file`.

- **Golden envelopes + NDJSON artifacts**
  - OK envelopes for inline and CAS cases under `test/golden/envelopes/`,
    validated by the existing golden harness:
    - Conform to Core Profile v1.
    - If `meta.cas_digest` is set, it matches `data.artifact`.
  - NDJSON artifact under `test/golden/swe_grep/`:
    - Every line parses as `{file, symbol_id?, start_line, end_line, text}`.
    - Used in golden tests to verify artifact shape.

- **Integration tests**
  - At least one end‑to‑end test (PR 6) that:
    - Starts from a `question` + fabricated `candidates[]`.
    - Runs `code/snippet_extract` via the normal runner path.
    - Asserts both envelope correctness and snippet usefulness on a small
      fixture repo.
  - Optionally, a follow‑on integration that pipes real candidates from Phase
    3/4 indexers into SWE Grep to exercise the full funnel on a fixture
    workspace.
