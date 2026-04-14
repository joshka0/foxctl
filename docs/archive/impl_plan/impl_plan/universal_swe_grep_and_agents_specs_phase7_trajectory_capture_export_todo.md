# Phase 7 – Trajectory Capture & Export Todo Spec

This spec breaks down Phase 7 of `universal_swe_grep_and_agents` into concrete
steps focused on **capturing retrieval + review behavior as trajectories** and
exporting dspy-ready episodes via jobs, envelopes, and CAS.

- Earlier phases establish review gate, indexing, SWE Grep, and tools.
- Phase 7 builds a derived view over these systems using existing
  envelope/jobs/CAS infrastructure.

> **Cross-refs**
>
> - Impl plan: `docs/impl_plan/universal_swe_grep_and_agents.md` (Phase 7)
> - Testing plan: `docs/impl_plan/universal_swe_grep_and_agents_testing.md`
>   (Phase 7)
> - Specs:
>   - `docs/spec/dspy_trajectory_capture.md`
>   - `docs/spec/code_symbol_index_and_swe_grep.md` (§7 trajectory integration)
>   - `docs/spec/dspy_go_agents.md`
>   - `docs/spec/core_profile_v1.md`
> - Skills/Jobs spec: `docs/spec/skills_spec/README.md` (§5.1
>   `trajectory.export`)
> - Codemaps for this phase (from `universal_swe_grep_and_agents_codemap.md`):
>   - CM3 – foxctl Envelope Protocol & CLI Pipeline.
>   - CM5 / CM12 – CAS storage, integrity, and integration.
>   - CM7 / CM8 – Job System: types, storage, submission, WFQ scheduler.
>   - CM10 – Knowledge System & Factory Droids.
>   - CM13 – Core Profile v1: end-to-end envelope, jobs & CAS flow.
>   - CM14 – Test infrastructure & CI.

---

## A. Data Model & Capture Points

Goal: define a **concrete, implementation-backed mapping** from existing systems
(envelopes, jobs, reviews, tasks) into `Trajectory`, `UserRequestCapture`, and
`TrajectoryEvent` concepts.

### A1. Trajectory index representation

- [ ] Decide on storage representation for trajectory index records:
  - Option A: SQLite tables `trajectories`, `user_requests`,
    `trajectory_events`.
  - Option B: named memory entries with types `"trajectory"`, `"user_request"`,
    `"trajectory_event"` plus minimal SQLite indices.
  - The choice MUST preserve:
    - Queryability by `workspace_id`, time range.
    - Joinability via `trajectory_id`, `job_id`, `trace_id`.
- [ ] Define Go types mirroring the conceptual fields in
      `dspy_trajectory_capture.md` §3:
  - `Trajectory` (id, workspace_id, root_request_id, task_ids, epic_id,
    agent_role, job_id, trace_id, status, summary, artifact_digest, timestamps).
  - `UserRequestCapture` (id, actor, source, text, command_context, task_hints).
  - `TrajectoryEvent` (id, trajectory_id, ts, kind, actor, command, status,
    data_inline, data_artifact, meta fields).
- [ ] Document how these records relate to existing data sources:
  - Jobs (job id, input.json, result.json).
  - Envelopes (tool/skill calls, status, meta fields).
  - Review artifacts and tasks (per `review_gate.md`, todo/agents specs).

### A2. Capture points and event mapping

- [ ] Identify and enumerate capture points per `dspy_trajectory_capture.md` §4:
  - CLI user requests (e.g. `foxctl dspy-agent spawn`, `todo.add`).
  - dspy-go agent runs (runtime spawn + tool calls).
  - Review outcomes (review gate artifacts).
  - Task state transitions.
- [ ] For each capture point, define the mapping to `TrajectoryEvent`:
  - `kind` values (e.g. `"user_request"`, `"tool_call"`, `"tool_result"`,
    `"review_result"`, `"task_transition"`, `"graph_search"`, `"swe_grep"`).
  - Which fields are derived from envelopes vs jobs vs tasks.
- [ ] Ensure **meta correlation fields** are consistently populated:
  - `meta.correlation_id` present and stable for all trajectory-related
    envelopes.
  - `meta.task_id`, `meta.epic_id`, `meta.job_id` attached where applicable.
  - No changes to the wire contract; only stronger conventions for how meta is
    used and interpreted.

### A3. Retrieval-specific events

- [ ] Map `code.symbol_search` and `code.swe_grep` tool calls to
      `TrajectoryEvent` kinds per impl plan Phase 7 +
      `code_symbol_index_and_swe_grep.md` §7:
  - Tool call envelopes → `TrajectoryEvent(kind="tool_call")` with optional
    subkind `"graph_search"` / `"swe_grep"` in `meta` or `data_inline`.
  - Tool results → `TrajectoryEvent(kind="tool_result")` referencing CAS
    artifacts where appropriate.
- [ ] Confirm that retrieval events include enough context for downstream
      analysis:
  - Inputs: question, candidate files/symbols (summarized, not full text).
  - Outputs: snippet counts, CAS digests, basic metrics.

---

## B. Jobs, Export Operations, and CAS

Goal: implement the **`trajectory.export` job** and any supporting helpers using
the existing job system, CAS, and envelope pipeline.

### B1. Internal exporter library

- [ ] Implement an internal Go package that:
  - Reads trajectory index records (trajectories, user requests, events) for a
    given workspace and filter set.
  - Joins in review artifacts, job metadata, and tool results as needed.
  - Produces in-memory `TrajectoryEpisode` objects per
    `dspy_trajectory_capture.md` §6.
- [ ] Ensure the exporter controls inline vs CAS payload size:
  - Respect `trajectory_capture.max_inline_bytes` and/or `inline_output_kb` from
    Core Profile v1.
  - If episodes would exceed the inline threshold, write NDJSON episodes to CAS
    and return digests instead.

### B2. `trajectory.export` job/skill

- [ ] Implement a `trajectory.export` operation per skills spec README §5.1 and
      `dspy_trajectory_capture.md` §7:
  - Inputs (conceptual): `workspace_id`, filters (`task_id`, `epic_id`, time
    range, `agent_role`, `status`), `format` (`"ndjson"`), `include_raw_traces`.
  - Behavior:
    - Select trajectories.
    - Build episodes via the exporter library.
    - Stream as NDJSON envelopes or write NDJSON to CAS and return a digest.
- [ ] Decide implementation shape for v1:
  - Option A: internal job only (no public skill); invoked via CLI command
    `foxctl trajectory export` that streams or returns CAS digest.
  - Option B: job backed by a dedicated skill manifest, following existing
    `jobs + exec` patterns.
- [ ] Integrate with the job system per CM7/CM8:
  - Use existing job store (`queued → running → ok/error/canceled`).
  - Ensure long-running exports run via jobs, not blocking CLI directly.
  - Respect WFQ scheduler semantics when export jobs are added.

### B3. Envelope, CAS, and Core Profile invariants

- [ ] Reuse the Core Profile v1 envelope rules for `trajectory.export` outputs:
  - All outputs as Protocol v1 envelopes
    (`status: "ok" | "error" | "progress"`).
  - `meta.cas_digest` is optional; if set it MUST match `data.artifact`.
  - Validate envelopes via `protocol.Validate` and existing golden tests.
- [ ] Apply CAS integration per CM5/CM12:
  - Large exports → NDJSON in CAS referenced via `data.artifact`.
  - Pin exported artifacts when appropriate to prevent GC during training and
    export workflows.
- [ ] Align error codes with `dspy_trajectory_capture.md` §5.3.2:
  - `trajectory.capture.invalid_request`, `trajectory.capture.storage_error`,
    `trajectory.export.access_denied`, `trajectory.invalid_schema`,
    `trajectory_event.missing_trace_id`.

---

## C. Knowledge, Privacy, and Configuration

Goal: ensure **knowledge-aware, privacy-preserving trajectory capture** that can
be safely exported and used for training.

### C1. Knowledge-aware capture

- [ ] Decide how (or whether) to relate trajectories to the Knowledge system
      (CM10):
  - Optionally link trajectories to knowledge items or factory droids via
    metadata (e.g. which droids/knowledge packs were active).
  - Keep this linkage internal; no new wire fields.
- [ ] If implemented, document how knowledge usage (e.g. recommendations from
      the knowledge router) is reflected in `TrajectoryEvent` records.

### C2. Configuration flags

- [ ] Implement configuration keys consistent with `dspy_trajectory_capture.md`
      §8:
  - `trajectory_capture.enabled` (bool).
  - `trajectory_capture.capture_user_requests` / `capture_agent_runs` /
    `capture_reviews` (bools).
  - `trajectory_capture.max_inline_bytes`.
  - `trajectory_capture.redact_patterns` (additional redaction rules).
  - `trajectory_export.enabled` (bool).
- [ ] Surface these via existing config mechanisms (workspace config, YAML),
      without changing the config wire contract.

### C3. Privacy, safety, and access control

- [ ] Enforce privacy requirements from `dspy_trajectory_capture.md` §9:
  - No secrets or raw environment variables in `data`, `meta`, or CAS payloads.
  - Apply redaction filters before persisting or exporting trajectories.
- [ ] Gate export operations:
  - Workspace-level opt-in for `trajectory_export.enabled`.
  - Optional role/actor checks so only admins can run full exports.

---

## D. Tests, Golden Fixtures, and CI Integration

Goal: validate that **events are captured correctly, episodes are exported per
spec, and everything obeys envelope/Jobs/CAS invariants**.

### D1. Mapping tests (envelopes → TrajectoryEvents)

- [ ] Add unit tests for mapping envelopes to `TrajectoryEvent` per
      `universal_swe_grep_and_agents_testing.md` (Phase 7):
  - Ensure tool calls for `code.symbol_search` / `code.swe_grep` are correctly
    labeled and correlated via `meta.correlation_id`, `meta.task_id`.
  - Cover review results, task transitions, and agent runs.
- [ ] Add tests that validate required meta fields (e.g. correlation_id
      presence) for trajectory-related envelopes.

### D2. Export tests (`trajectory.export`)

- [ ] Implement tests for the `trajectory.export` operation:
  - Seed a small set of trajectories + events (fixtures or in-memory).
  - Run the export logic and produce NDJSON episodes.
  - Validate episode shapes against `dspy_trajectory_capture.md` §6.
  - Verify filters (`task_id`, time range, `agent_role`, `status`) behave as
    expected.

### D3. Golden episodes

- [ ] Create a `test/golden/trajectories/` directory with anonymized episodes:
  - Small curated set of `TrajectoryEpisode` NDJSON fixtures.
  - CI test that `trajectory.export` over the fixture data matches these goldens
    (modulo any explicitly documented evolution).

### D4. Job system and CLI integration tests

- [ ] Add tests ensuring that **long-running exports use the job system**
      (CM7/CM8):
  - `foxctl trajectory export` (or equivalent) creates jobs with expected
    states (`queued → running → ok/error`).
  - WFQ scheduling honors existing policies when exports are queued.
- [ ] Add CLI-level tests exercising the full pipeline (CM3, CM13):
  - CLI → job preparation → runner dispatch → skill/job execution → envelope
    parsing → CAS artifact handling → result persistence.

### D5. Test infra and CI hooks

- [ ] Integrate Phase 7 tests with existing test infra (CM14):
  - Ensure `make test` / CI targets run trajectory tests by default or under a
    specific tag.
  - Provide guidance for anonymizing and updating golden episodes when schemas
    evolve.

---

## Open Questions / To Discuss

- Should the trajectory index be implemented via dedicated SQLite tables, named
  memory types, or a hybrid? What migration story is acceptable for v1?
- Which trajectories are in-scope for v1 export (only Coding/Planning/Review
  agents, or also other tools/jobs)?
- How aggressive should default redaction be, and should there be presets for
  "internal only" vs "export for training"?
