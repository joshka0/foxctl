## Phase 7 – Trajectory Capture & Export (Summary)

Phase 7 builds a trajectory layer on top of existing envelope, Jobs, CAS,
review, and agents infrastructure. The goal is to capture retrieval + review
behavior as `Trajectory`, `UserRequestCapture`, and `TrajectoryEvent` records,
then export dspy-ready `TrajectoryEpisode` NDJSON via a `trajectory.export` job
that fully respects Core Profile v1 envelopes and CAS invariants.

No new wire contracts are added: we standardize how existing `meta.*` and data
fields are populated and interpreted, and introduce an internal trajectory index
(SQLite and/or named memory) plus an exporter library. Privacy and configuration
are first-class: capture can be disabled, redaction is configurable, and exports
are gated by workspace-level opt-in and optional role checks.

---

## A/B/C/D Todo Structure – Sanity Check vs Phase 7 Spec

- **Section A – Data model & capture points**
  - A1/A2/A3 in the Phase 7 todo spec map to:
    - Choosing a concrete storage representation for trajectory index records
      (SQLite tables vs named memory vs hybrid).
    - Defining Go types for `Trajectory`, `UserRequestCapture`, and
      `TrajectoryEvent` consistent with `docs/spec/dspy_trajectory_capture.md`.
    - Enumerating capture points (CLI, dspy-go agents, review artifacts, tasks)
      and mapping envelopes/jobs → `TrajectoryEvent` rows, with stable
      correlation metadata (`trace_id`, `job_id`, `task_id`, `epic_id`).
  - **Codemaps most relevant**:
    - **foxctl Envelope Protocol & CLI Pipeline** (where capture logic can
      hook into validated envelopes).
    - **foxctl Job System: Types, Storage, and WFQ Scheduler** / **Job System:
      Submission, SQLite Storage, State Transitions, and WFQ Scheduler**.
    - **foxctl Core Profile v1: End-to-End Envelope, Jobs & CAS Flow**.

- **Section B – Jobs, export operations, and CAS**
  - B1/B2/B3 cover:
    - An internal exporter library that turns stored trajectories/events plus
      jobs/envelopes/reviews into `TrajectoryEpisode` objects.
    - A `trajectory.export` operation (job/skill + CLI) that filters
      trajectories (by workspace, task, epic, role, time, status) and exports
      NDJSON episodes inline or via CAS.
    - Strict adherence to Core Profile v1: envelopes validated via
      `protocol.Validate`, optional `meta.cas_digest` matching `data.artifact`
      (when set), CAS pinning for exported artifacts.
  - **Codemaps**:
    - **CAS Storage & Integrity Verification in foxctl** / **foxctl CAS:
      Put, Get, Integrity, Deduplication & Integration**.
    - **foxctl Job System…** (preparation, execution, result handling).
    - **foxctl Core Profile v1: End-to-End Envelope, Jobs & CAS Flow**.

- **Section C – Knowledge, privacy, and configuration**
  - C1/C2/C3 cover:
    - Optional linkage of trajectories to Knowledge/factory droids (which
      packs/droids were active during episodes).
    - Config keys to enable/disable capture + export, and to control max inline
      bytes and redaction patterns.
    - Privacy and access control: redacting secrets before persistence/export;
      gating `trajectory.export` behind workspace config and optional actor/role
      checks.
  - **Codemaps**:
    - **foxctl Knowledge System & Factory Droids**.
    - **foxctl Core Profile v1: End-to-End Envelope, Jobs & CAS Flow**.

- **Section D – Tests, goldens, and CI integration**
  - D1–D5 cover:
    - Mapping tests for envelopes → `TrajectoryEvent`, especially
      retrieval-specific events (`code.symbol_search`, `code.swe_grep`).
    - Export tests for `trajectory.export` (filters, episode shape, CAS vs
      inline behavior).
    - Golden NDJSON fixtures under `test/golden/trajectories/`.
    - Job system + CLI integration tests for long-running exports.
    - CI integration via existing test/watch + coverage targets.
  - **Codemaps**:
    - **foxctl Test Infrastructure: Test-Watch, Feedback Hooks, and CI
      Targets**.
    - **foxctl Core Profile v1: End-to-End Envelope, Jobs & CAS Flow**.

---

## Proposed PRs for Phase 7 – Trajectory Capture & Export

### PR 1 – Trajectory Data Model & Storage (A1)

- **Scope**
  - Choose a **v1 storage representation** for the trajectory index:
    - Recommendation: dedicated SQLite tables `trajectories` and
      `trajectory_events`, with optional `user_requests` if the spec warrants a
      separate table.
  - Define Go types:
    - `Trajectory`, `UserRequestCapture`, and `TrajectoryEvent` aligning with
      `dspy_trajectory_capture.md` §3 (ids, workspace_id, job_id, trace_id,
      status, summary, artifact_digest, timestamps, etc.).
  - Implement a repository layer around the new tables:
    - `InsertTrajectory`, `InsertEvents`, `ListTrajectories`,
      `ListEventsByTrajectory`, and any minimal query helpers needed for export.
  - Document how these records relate to existing data:
    - Jobs (job id, `input.json`, `result.json`), envelopes (commands, status,
      meta fields), review artifacts, and tasks.

- **Constraints**
  - No changes to envelope or config wire contracts; schema is internal-only.
  - Migrations must be idempotent and follow existing SQLite migration patterns.

- **Validation**
  - Unit tests for migration and basic CRUD on the trajectory store.
  - Tests that serialize/deserialize Go types and enforce required fields (e.g.,
    non-empty workspace_id, timestamps).

---

### PR 2 – Capture Hooks: CLI, Jobs, and Agents → TrajectoryEvents (A2)

- **Scope**
  - Enumerate and implement capture points from `dspy_trajectory_capture.md` §4:
    - CLI user requests (e.g., `foxctl dspy-agent`, `todo.*`, SWE Grep flows).
    - dspy-go agent runs (session start/end, tool calls, significant internal
      steps).
    - Review outcomes and task state transitions.
  - At each capture point:
    - Ensure `meta.correlation_id`, `meta.job_id`, `meta.task_id`, and
      `meta.epic_id` are consistently populated using existing meta fields (no
      new meta fields added).
    - Construct and persist `Trajectory` and `TrajectoryEvent` records:
      - `kind` values such as `"user_request"`, `"tool_call"`, `"tool_result"`,
        `"review_result"`, `"task_transition"`, `"graph_search"`, `"swe_grep"`
        where appropriate.
      - Store small `data_inline` summaries; push heavy payloads to CAS and
        record digests in event fields.

- **Constraints**
  - Follow Core Profile v1 and AGENTS rules: no `meta.*` shape changes, only
    better population and consistent conventions.
  - No logging or persisting secrets; redaction must apply before anything is
    written to the trajectory index.

- **Validation**
  - Unit tests for envelope → `TrajectoryEvent` mapping (per Phase 7 testing
    plan D1).
  - Tests asserting presence of required correlation fields and well-defined
    behavior when they are missing (e.g., mapping to
    `trajectory_event.missing_trace_id` error codes where appropriate).

---

### PR 3 – Retrieval-Specific Trajectory Events (A3)

- **Scope**
  - Specialize capture for retrieval-intensive tools:
    - Ensure events derived from `code.symbol_search` and `code.swe_grep` are
      annotated so they can be queried as retrieval events (e.g., subkind or
      dedicated `kind` values).
    - Capture for each retrieval event:
      - Query text (redacted as configured).
      - Candidate selection summaries (counts of files/symbols/snippets, not raw
        content).
      - CAS digests for larger snippet/result payloads.
  - Ensure these events can be joined back to:
    - Their parent `Trajectory` and originating `Job`.
    - Any downstream review decisions or edits.

- **Constraints**
  - Respect `trajectory_capture.max_inline_bytes` and any Core Profile inline
    size limits; overflow goes to CAS.

- **Validation**
  - Focused unit tests where synthetic swe_grep/symbol_search envelopes produce
    the expected retrieval events.
  - Small anonymized golden JSON samples of retrieval trajectories to guard
    against regressions.

---

### PR 4 – Internal Exporter Library: Trajectories → TrajectoryEpisode (B1)

- **Scope**
  - Implement an internal Go package (e.g., `trajectoryexport`) that:
    - Reads trajectories/events plus related jobs/results/reviews from storage.
    - Constructs in-memory `TrajectoryEpisode` objects as per
      `dspy_trajectory_capture.md` §6.
    - Controls inline vs CAS payload:
      - Respects `trajectory_capture.max_inline_bytes` and/or `inline_output_kb`
        from Core Profile v1.
      - When episodes exceed thresholds, streams NDJSON to CAS and returns
        digests instead of large inline payloads.
  - Provide filtering APIs:
    - By workspace, `task_id`, `epic_id`, time range, `agent_role`, and
      `status`.

- **Constraints**
  - No direct ad-hoc filesystem traversal; rely on Jobs/CAS/knowledge
    abstractions for loading artifacts.

- **Validation**
  - Pure Go tests that seed the trajectory store and job results in-memory and
    assert episode structures.
  - Shape checks against the spec: required fields, forbidden fields, and
    redaction behavior.

---

### PR 5 – `trajectory.export` Job + CLI Wiring (B2/B3)

- **Scope**
  - Implement the `trajectory.export` operation per
    `docs/spec/skills_spec/README.md` §5.1 and `dspy_trajectory_capture.md` §7:
    - CLI entry point (e.g., `foxctl trajectory export`) that accepts filters,
      format (`ndjson`), and `include_raw_traces` flag.
    - Uses the exporter library to generate episodes.
    - Either:
      - Streams NDJSON envelopes directly to stdout, or
      - Writes NDJSON to CAS and returns a CAS digest in the result envelope.
  - Integrate with the job system (CM7/CM8):
    - Use existing job store and WFQ scheduler; long-running exports are queued
      jobs rather than blocking the CLI.
    - Jobs follow the usual `queued → running → ok/error/canceled` lifecycle,
      and results are written under the job directory.
  - Enforce Core Profile v1 invariants:
    - All outputs as validated envelopes (via `protocol.Validate`).
    - Proper `data.artifact` reference (and optional `meta.cas_digest` matching
      it); CAS pinning for exported artifacts where needed.
  - Align typed error codes with `dspy_trajectory_capture.md` §5.3.2:
    - Map internal failures to codes like `trajectory.capture.invalid_request`,
      `trajectory.capture.storage_error`, `trajectory.export.access_denied`,
      `trajectory.invalid_schema`, `trajectory_event.missing_trace_id`.

- **Constraints**
  - No new envelope or config fields; only command names and data conventions
    change.

- **Validation**
  - Unit tests for the CLI handler (argument parsing, job submission, error
    envelopes).
  - Integration tests that:
    - Prepare a few trajectories in storage.
    - Run `trajectory.export` with different filters.
    - Read back NDJSON or CAS artifacts and validate content and filter
      behavior.

---

### PR 6 – Knowledge Linkage, Config, and Privacy (C1–C3)

- **Scope**
  - **Knowledge linkage**:
    - Optionally associate trajectories with Knowledge/factory droids (e.g.,
      which packs were active) using internal metadata only.
    - Document how knowledge usage (recommendations, lookups) shows up in
      `TrajectoryEvent` records without adding new wire-visible fields.
  - **Configuration**:
    - Add config keys per `dspy_trajectory_capture.md` §8:
      - `trajectory_capture.enabled` plus per-surface flags such as
        `capture_user_requests`, `capture_agent_runs`, and `capture_reviews`.
      - `trajectory_capture.max_inline_bytes` and
        `trajectory_capture.redact_patterns`.
      - `trajectory_export.enabled`.
    - Wire these through existing config loading and workspace config
      mechanisms.
  - **Privacy & access control**:
    - Implement redaction filters applied before writes to trajectory storage
      and before export envelopes are emitted.
    - Gate `trajectory.export` on `trajectory_export.enabled` and optional
      actor/role checks.

- **Constraints**
  - Respect "no secrets in envelopes or CAS" rules; treat redaction failures as
    hard errors for export.

- **Validation**
  - Config-driven tests:
    - Capture disabled → no trajectories/events created.
    - Export disabled → CLI returns an appropriate error envelope.
  - Redaction tests on synthetic sensitive payloads.

---

### PR 7 – Tests, Goldens, and CI Integration (D1–D5)

- **Scope**
  - **Mapping tests (D1)**:
    - Unit tests that map envelopes from tools/agents/reviews into
      `TrajectoryEvent` correctly and enforce trace/task/epic/job correlation
      invariants.
  - **Export tests (D2)**:
    - Tests that exercise the exporter with fixtures and validate episode shape
      and filters.
  - **Golden episodes (D3)**:
    - Add `test/golden/trajectories/` with anonymized NDJSON fixtures.
    - Golden tests asserting `trajectory.export` output matches fixtures modulo
      explicitly documented evolution.
  - **Job and CLI integration (D4)**:
    - End-to-end tests: CLI → job creation → execution → result envelope and CAS
      artifacts.
  - **Test infra / CI hooks (D5)**:
    - Ensure `make test` and CI run trajectory tests; if necessary, add or
      update test-watch configuration so local dev sees trajectory failures.

- **Constraints**
  - Keep tests runnable in `-short` mode where possible; heavier golden tests
    may be flagged separately.

- **Validation**
  - All new tests pass locally and in CI; coverage on new/changed code meets
    existing thresholds.

---

## Validation Overview – How We Know Phase 7 Is "Done"

- **Trajectory capture correctness**
  - For a curated set of sessions (including retrieval and review), the
    trajectory index contains:
    - A `Trajectory` row per session.
    - Linked `TrajectoryEvent` rows for user requests, tool calls/results,
      reviews, and task transitions.
    - Stable `trace_id`/`job_id`/`task_id`/`epic_id` correlations.

- **Export fidelity**
  - `trajectory.export` produces NDJSON `TrajectoryEpisode` objects that
    validate against `dspy_trajectory_capture.md`:
    - Filters behave as expected.
    - Large exports use CAS with correct `data.artifact` (and optional
      `meta.cas_digest` matching it) and pinning.

- **Privacy & configuration**
  - Redaction removes secrets from both trajectory storage and exports.
  - Capture and export can be toggled via config and enforced per
    workspace/role.

- **Tests & CI**
  - New unit, integration, and golden tests live under `internal/*`,
    `cmd/foxctl/cmd`, and `test/golden/trajectories/`.
  - CI runs all relevant tests and coverage for Phase 7 code without
    regressions.
