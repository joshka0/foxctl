# Universal SWE Grep & Agents – Phase 3–9 Cross-Ref & Open Questions

Status: Draft  \
Scope: Cross-reference and open-question index for **Phases 3–9** of
`universal_swe_grep_and_agents`.

This doc does **not** introduce new wire contracts. It only:

- Summarizes where each phase’s truth lives (impl plan, testing plan, specs,
  codemap, phase todo-spec).
- Aggregates the **"Open Questions / To Discuss"** sections for Phases 3–9 and
  groups them by theme to help drive decisions.

---

## 1. Phase Cross-Reference (3–9)

### 1.1 Phase 3 – Semantic File Index v1

- **Impl plan**  \
  - `docs/impl_plan/universal_swe_grep_and_agents.md` → _Phase 3 – Semantic File Index v1_
- **Testing plan**  \
  - `docs/impl_plan/universal_swe_grep_and_agents_testing.md` → _Phase 3 – Semantic File Index v1_
- **Phase todo-spec**  \
  - `docs/impl_plan/universal_swe_grep_and_agents_specs_phase3_semantic_file_index_todo.md`
- **Primary specs**  \
  - `docs/spec/semantic_file_index.md`  \
  - `docs/spec/review_semantic_trajectory_specs.md`  \
  - `docs/spec/core_profile_v1.md`  \
  - Related: `docs/spec/post_review_harness.md`,
    `docs/spec/code_symbol_index_and_swe_grep.md`
- **Codemap**  \
  - `docs/impl_plan/universal_swe_grep_and_agents_codemap.md` → Phase 3 row

---

### 1.2 Phase 4 – Code Symbol Index v1 (Go-First)

- **Impl plan**  \
  - `docs/impl_plan/universal_swe_grep_and_agents.md` → _Phase 4 – Code Symbol Index v1 (Go-First)_
- **Testing plan**  \
  - `docs/impl_plan/universal_swe_grep_and_agents_testing.md` → _Phase 4 – Code Symbol Index v1_
- **Phase todo-spec**  \
  - `docs/impl_plan/universal_swe_grep_and_agents_specs_phase4_code_symbol_index_todo.md`
- **Primary specs**  \
  - `docs/spec/code_symbol_index_and_swe_grep.md`  \
  - `docs/spec/semantic_file_index.md`  \
  - `docs/spec/review_gate.md`  \
  - `docs/spec/post_review_harness.md`  \
  - `docs/spec/core_profile_v1.md`
- **Codemap / infra**  \
  - `docs/impl_plan/universal_swe_grep_and_agents_codemap.md` → Phase 4 row  \
  - Job system + WFQ codemaps; skill/runtime codemaps

---

### 1.3 Phase 5 – SWE Grep Skill (`code/swe_grep`)

- **Impl plan**  \
  - `docs/impl_plan/universal_swe_grep_and_agents.md` → _Phase 5 – SWE Grep Skill (`code/swe_grep`)_
- **Testing plan**  \
  - `docs/impl_plan/universal_swe_grep_and_agents_testing.md` → _Phase 5 – SWE Grep Skill (`code/swe_grep`)_
- **Phase todo-spec**  \
  - `docs/impl_plan/universal_swe_grep_and_agents_specs_phase5_swe_grep_todo.md`
- **Primary specs**  \
  - `docs/spec/code_symbol_index_and_swe_grep.md` (§5 SWE Grep, funnel context)  \
  - `docs/spec/dspy_go_agents.md` (tool surface)  \
  - `docs/spec/core_profile_v1.md`  \
  - Related: `docs/spec/review_gate.md`, `docs/spec/semantic_file_index.md`
- **Codemap / infra**  \
  - `docs/impl_plan/universal_swe_grep_and_agents_codemap.md` → Phase 5 row  \
  - Execution runners codemap (exec skill, PathValidator)  \
  - CAS codemaps for envelopes + artifacts

---

### 1.4 Phase 6 – dspy-go Tools & Agents Wiring

- **Impl plan**  \
  - `docs/impl_plan/universal_swe_grep_and_agents.md` → _Phase 6 – dspy-go Tools & Agents Wiring_
- **Testing plan**  \
  - `docs/impl_plan/universal_swe_grep_and_agents_testing.md` → _Phase 6 – dspy-go Tools & Agents Wiring_
- **Phase todo-spec**  \
  - `docs/impl_plan/universal_swe_grep_and_agents_specs_phase6_tools_and_agents_todo.md`
- **Primary specs**  \
  - `docs/spec/code_symbol_index_and_swe_grep.md` (§6 agent tools)  \
  - `docs/spec/dspy_go_agents.md` (agent roles, tools, teams)  \
  - `docs/spec/core_profile_v1.md`
- **Codemap / infra**  \
  - Overseer & hierarchy codemap (spawn protocol, depth limits)  \
  - Planning LLM stack codemap  \
  - Knowledge system & factory droids codemap  \
  - Dspy-go agent runtime & tools integration codemap

---

### 1.5 Phase 7 – Trajectory Capture & Export

- **Impl plan**  \
  - `docs/impl_plan/universal_swe_grep_and_agents.md` → _Phase 7 – Trajectory Capture & Export_
- **Testing plan**  \
  - `docs/impl_plan/universal_swe_grep_and_agents_testing.md` → _Phase 7 – Trajectory Capture & Export_
- **Phase todo-spec**  \
  - `docs/impl_plan/universal_swe_grep_and_agents_specs_phase7_trajectory_capture_export_todo.md`
- **Primary specs**  \
  - `docs/spec/dspy_trajectory_capture.md`  \
  - `docs/spec/code_symbol_index_and_swe_grep.md` (§7 trajectory integration)  \
  - `docs/spec/dspy_go_agents.md`  \
  - `docs/spec/core_profile_v1.md`  \
  - `docs/spec/skills_spec/README.md` (§5.1 `trajectory.export`)
- **Codemap / infra**  \
  - `docs/impl_plan/universal_swe_grep_and_agents_codemap.md` → Phase 7 row

---

### 1.6 Phase 8 – Teams & Routing

- **Impl plan**  \
  - `docs/impl_plan/universal_swe_grep_and_agents.md` → _Phase 8 – Teams & Routing_
- **Testing plan**  \
  - `docs/impl_plan/universal_swe_grep_and_agents_testing.md` → _Phase 8 – Teams & Routing_
- **Phase todo-spec**  \
  - `docs/impl_plan/universal_swe_grep_and_agents_specs_phase8_teams_routing_todo.md`
- **Primary specs**  \
  - `docs/spec/dspy_go_agents.md` (§4.3 Teams and Assignments)  \
  - `docs/spec/skills_spec/README.md` (§6 Teams & Routing future skills)  \
  - `docs/spec/core_profile_v1.md`
- **Codemap / infra**  \
  - Codemap Phase 8 row: CM6 (dspy-go runtime/tools), CM9 (overseer & hierarchy),
    CM10 (knowledge system), CM13 (Core Profile v1)

---

### 1.7 Phase 9 – End-to-End Flows & CI Hardening

- **Impl plan**  \
  - `docs/impl_plan/universal_swe_grep_and_agents.md` → _Phase 9 – Polish, Examples, and CI Hardening_
- **Testing plan**  \
  - `docs/impl_plan/universal_swe_grep_and_agents_testing.md` → _Phase 9 – End-to-End & CI Hardening_
- **Phase todo-spec**  \
  - `docs/impl_plan/universal_swe_grep_and_agents_specs_phase9_end_to_end_ci_hardening_todo.md`
- **Primary specs**  \
  - `docs/spec/review_gate.md`  \
  - `docs/spec/semantic_file_index.md`  \
  - `docs/spec/code_symbol_index_and_swe_grep.md`  \
  - `docs/spec/dspy_go_agents.md`  \
  - `docs/spec/dspy_trajectory_capture.md`  \
  - `docs/spec/skills_spec/README.md`
- **Codemap / infra**  \
  - Phase 9 codemaps: CM3 (envelope/CLI), CM13 (Core Profile v1),
    CM14 (test infra), CM17 (test watcher/Makefile/coverage)

---

## 2. Open Questions by Phase (3–9)

This section mirrors the **"Open Questions / To Discuss"** blocks from each
Phase 3–9 todo-spec.

### 2.1 Phase 3 – Semantic File Index v1

- **[P3-Q1]** How aggressively should old chunk entries be cleaned up when
  config changes (e.g. immediate deletion vs. soft-deprecation)?
- **[P3-Q2]** What default chunking parameters (bytes/overlap) should be used
  for medium-sized repos, and how are they surfaced in config?
- **[P3-Q3]** How should semantic index jobs interact with WFQ scheduler
  namespaces (e.g. one namespace per workspace vs. per-indexer)?

---

### 2.2 Phase 4 – Code Symbol Index v1

- **[P4-Q1]** How aggressively should we handle symbol ID stability across
  full-file renames, especially when file paths change but contents do not?
- **[P4-Q2]** Do we need additional indexing modes (e.g., package-level
  summaries) in v1, or can they be deferred to a later phase?
- **[P4-Q3]** What default `MaxFileLOC` and `MaxFileKB` thresholds should we use
  for Go-first, and how should they be surfaced in configuration?

---

### 2.3 Phase 5 – SWE Grep Skill (`code/swe_grep`)

- **[P5-Q1]** **Resolved:** `code/swe_grep` uses cheap LLMs for snippet
  extraction only; recall and scoring are handled upstream by the semantic
  file index + symbol index/DAG.
- **[P5-Q2]** What default limits (`max_files`, `max_snippets`,
  `max_bytes_per_file`) strike the right balance between recall and speed for
  typical repos?
- **[P5-Q3]** Do we need a jobs-backed variant for very large candidate sets, or
  is synchronous exec sufficient for Phase 5?

---

### 2.4 Phase 6 – dspy-go Tools & Agents Wiring

- **[P6-Q1]** Should `edit.apply_patch` be migrated entirely to a structured
  diff format (backed by `code/diff`), or should we introduce a new tool name
  for structured diffs and keep the simple text-replace helper for small
  edits?
- **[P6-Q2]** Which agent roles (Coder, Reviewer, others) should have direct
  access to `code.symbol_search` and `code.swe_grep` in v1, and how should
  their instructions differ?
- **[P6-Q3]** Do we need any additional knowledge packs or factory droids
  specifically for explaining the retrieval funnel (`semantic_file_index` →
  symbol index → SWE Grep) to humans and/or agents?

---

### 2.5 Phase 7 – Trajectory Capture & Export

- **[P7-Q1]** Should the trajectory index be implemented via dedicated SQLite
  tables, named memory types, or a hybrid? What migration story is acceptable
  for v1?
- **[P7-Q2]** Which trajectories are in-scope for v1 export (only
  Coding/Planning/Review agents, or also other tools/jobs)?
- **[P7-Q3]** How aggressive should default redaction be, and should there be
  presets for "internal only" vs "export for training"?

---

### 2.6 Phase 8 – Teams & Routing

- **[P8-Q1]** How much of the teams functionality should Phase 8 implement vs
  defer (e.g., storage only vs full `teams/manage.*` skills + CLI/tools)?
- **[P8-Q2]** What is the minimal, stable set of team fields needed for v1,
  given future Viewer and routing plans?
- **[P8-Q3]** Should agents be able to mutate team membership directly (via
  tools), or should that remain an admin-only path in v1?

---

### 2.7 Phase 9 – End-to-End Flows & CI Hardening

- **[P9-Q1]** What is the minimal, stable set of E2E scenarios we want to lock
  in as blocking tests vs optional/nightly ones?
- **[P9-Q2]** How aggressively should CI enforce golden stability (e.g. all
  goldens vs only envelope/trajectory-related ones)?
- **[P9-Q3]** Are there additional high-value examples we should include beyond
  the ones described in the impl plan (e.g. failure-mode walkthroughs,
  demo scripts)?

---

## 3. Open Questions Grouped by Theme

This section clusters the phase-specific questions into broader themes to help
prioritize decisions.

### 3.1 Indexing Configuration & Scheduler Behavior

- **Chunking + file-size policies**  \
  - [P3-Q2] Default semantic chunking parameters (bytes/overlap) + how they are
    surfaced in config.  \
  - [P4-Q3] Default `MaxFileLOC` / `MaxFileKB` thresholds for the symbol index
    (Go-first).
- **Old entry lifecycle**  \
  - [P3-Q1] Cleanup vs. soft-deprecation of old chunk entries on config change.
- **Scheduler / WFQ interaction**  \
  - [P3-Q3] How semantic index jobs use WFQ namespaces (per-workspace vs.
    per-indexer).

### 3.2 Retrieval Behavior, Limits, and Tool Usage

- **SWE Grep behavior**  \
  - [P5-Q1] **Resolved:** `code/swe_grep` does LLM-based snippet extraction over
    candidates selected by semantic + symbol index/DAG; it does **not**
    implement its own full-repo retrieval scorer.  \
  - [P5-Q2] Default limits for files/snippets/bytes.  \
  - [P5-Q3] Need (or not) for a jobs-backed SWE Grep variant.
- **Agent tool surfaces**  \
  - [P6-Q2] Which roles can use `code.symbol_search` / `code.swe_grep` and how
    their prompts/instructions differ.  \
  - [P6-Q3] Knowledge packs / factory droids to explain the retrieval funnel.

### 3.3 Editing & Diff Pipeline

- **Structured diff adoption**  \
  - [P6-Q1] Whether to fully migrate `edit.apply_patch` to JSON diffs from
    `code/diff`, vs introducing a new tool name and keeping a simpler helper.

### 3.4 Trajectories & Privacy

- **Storage backend for trajectories**  \
  - [P7-Q1] SQLite tables vs named memory vs hybrid; migration story.
- **Scope of captured/exported trajectories**  \
  - [P7-Q2] Which agents/tools/jobs are in-scope for v1 export.
- **Redaction and export modes**  \
  - [P7-Q3] Default redaction strictness + presets for internal vs
    training-oriented exports.

### 3.5 Teams, Routing, and Governance

- **Depth of v1 teams implementation**  \
  - [P8-Q1] Storage-only vs full `teams/manage.*` skills + CLI/tools in Phase 8.
- **Minimal team schema for v1**  \
  - [P8-Q2] Required fields for teams/team_members, anticipating Viewer and
    routing.
- **Mutation rights**  \
  - [P8-Q3] Whether agents may mutate team membership vs admin-only
    operations.

### 3.6 E2E Scenarios, CI, and Goldens

- **E2E coverage level**  \
  - [P9-Q1] Which E2E scenarios are blocking vs nightly/optional.
- **Golden stability policy**  \
  - [P9-Q2] How strictly CI should treat golden drift, and which goldens are
    considered critical (envelopes, trajectories, SWE Grep, etc.).
- **Examples & demos**  \
  - [P9-Q3] Additional examples (including failure-mode walkthroughs and demo
    scripts) beyond the baseline impl plan.

---

## 4. Suggested Next Steps

- **[NS1]** For each theme in §3, decide which questions must be answered
  **before starting implementation** vs which can be resolved during the first
  PRs.  \
- **[NS2]** Capture the resulting decisions either:
  - As small updates to the relevant specs, or  \
  - As a follow-up note in this doc, linking back to the authoritative spec.
- **[NS3]** When questions lead to non-trivial behavior changes, update the
  corresponding Phase todo-spec(s) so they stay in sync with the decisions.
EOF