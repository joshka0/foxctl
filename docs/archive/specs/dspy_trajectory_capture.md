# dspy Trajectory Capture Spec (Draft)

## 1. Overview

This spec defines how foxctl **captures user requests and agent runs as
trajectories** that can be used for:

- Training and evaluating dspy-go agents.
- Debugging and introspecting agent behavior.
- Long-term memory of "what happened" for a given task or epic.

The goal is to build on existing infrastructure:

- **Protocol v1 envelopes** (`protocol_v1.md`) for all tool/skill calls.
- **Jobs + CAS** for durable storage of large traces.
- **Review artifacts** (`review_gate.md`) for outcomes and quality signals.
- **dspy-go agent runtime** (`dspy_go_agents.md`) for agent roles and
  signatures.

Trajectories are **derived views** over existing data, not a new wire contract.
They are constructed from envelopes, jobs, mailbox messages, and review
artifacts.

---

## 2. Goals and Non-Goals

### 2.1 Goals

- **Capture user intent**
  - Persist user-initiated requests (CLI commands, prompts, mailbox messages) in
    a normalized form.
- **Link to tasks and reviews**
  - Connect user requests to tasks, epics, reviews, and code changes.
- **Trajectory-friendly schema**
  - Define a conceptual schema for dspy-ready episodes derived from foxctl
    data.
- **Reuse Protocol v1**
  - Never introduce a parallel logging format; trajectories are built from
    Protocol v1 envelopes and CAS artifacts.
- **Configurable export**
  - Allow workspaces to export trajectories selectively for training and
    analysis.

### 2.2 Non-Goals (v1)

- Implementing the full model-training pipeline.
- Cross-workspace or cross-user aggregation; this spec focuses on a single
  workspace.
- Defining new envelope fields or changing the Protocol v1 wire contract.

---

## 3. Core Concepts & Data Model

### 3.1 Trajectory (Conceptual)

A **Trajectory** is a logical record representing a coherent run or episode,
usually anchored to a single user request and task.

Fields (conceptual):

- `id` (string) – ULID for the trajectory.
- `workspace_id` (string).
- `root_request_id` (string) – ID of the captured user request that started the
  trajectory.
- `task_ids` ([]string) – tasks touched.
- `epic_id` (string, optional).
- `agent_role` (string, optional) – `coder`, `planner`, `reviewer`, etc.
- `job_id` (string, optional) – jobs entry for the main agent run.
- `trace_id` (string, optional) – Protocol v1 `meta.correlation_id` linking all
  envelopes (stored as `trace_id` in trajectory records).
- `status` (string) – `"ok" | "error" | "aborted" | "partial"`.
- `summary` (string) – human-readable description.
- `created_at` / `updated_at` (timestamps).
- `artifact_digest` (string, optional) – CAS digest for the full trajectory
  payload if large.

Trajectories themselves are small index records. Full traces live in CAS.

### 3.2 UserRequestCapture

A **UserRequestCapture** normalizes a user-initiated intent.

Fields (conceptual):

- `id` (string).
- `workspace_id` (string).
- `actor` (string) – `actor:human:<id>` or `actor:admin:<id>`.
- `source` (string) – `"cli" | "mailbox" | "api" | "viewer"`.
- `ts` (timestamp).
- `text` (string) – raw user request or prompt.
- `command_context` (object, optional):
  - `cli_command` (string, optional) – e.g. `"foxctl agent spawn ..."`.
  - `protocol_command` (string, optional) – e.g. `"agent/spawn"` or
    `"todo/manage"`.
  - `job_id` (string, optional).
  - `trace_id` (string, optional) – from `meta.correlation_id`.
- `task_hints` (object, optional):
  - `task_id` (string, optional).
  - `epic_id` (string, optional).
  - `scope_paths` ([]string, optional).

UserRequestCaptures can be stored as **named memory** entries, a dedicated
`user_requests` table, or both, but MUST be addressable by `id` and
`workspace_id`.

### 3.3 TrajectoryEvent

A **TrajectoryEvent** is a normalized view over envelopes, mailbox messages, and
review artifacts.

Fields (conceptual):

- `id` (string).
- `trajectory_id` (string).
- `ts` (timestamp).
- `kind` (string):
  - `"user_request"`, `"agent_thought"`, `"tool_call"`, `"tool_result"`,
    `"review_request"`, `"review_result"`, `"task_transition"`,
    `"graph_search"`, `"swe_grep"`, `"hook_call"`, `"hook_result"`, etc.
- `actor` (string) – e.g. `actor:agent:dspy:<slug>`, `actor:system:overseer`.
- `command` (string, optional) – Protocol v1 `command` when applicable.
- `status` (string, optional) – derived from `env.status` or review status.
- `data_inline` (object, optional) – small preview (e.g. truncated message).
- `data_artifact` (string, optional) – CAS digest for full details.
- `meta` (object, optional) – see **Meta Field Reference** below.

#### 3.3.1 Meta Field Reference (Conceptual)

Trajectory events persist a `meta` object that mirrors selected Protocol v1
`meta.*` fields for correlation and joins. In v1, the correlation value is
sourced from envelope `meta.correlation_id` and stored as `trace_id` in
trajectory records/events. The following keys are recognized for trajectory-
related envelopes and index rows:

| Field                     | Type   | Required | Scope / Uniqueness                                                             | Usage Notes                                                                                                   |
| ------------------------- | ------ | -------- | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------- |
| `trace_id`                | string | yes      | Unique per trajectory (UUID/ULID/hex); reused by all envelopes in the same run | Primary correlation id; sourced from `meta.correlation_id` and stored as `trace_id`.                          |
| `job_id`                  | string | optional | ULID; unique per job in a workspace                                            | Set when events originate from a `jobs/*` command.                                                            |
| `task_id`                 | string | optional | Task identifier; unique per tasks store                                        | Set for task-scoped events.                                                                                   |
| `epic_id`                 | string | optional | Epic identifier                                                                | Set when an event relates to an epic-level plan.                                                              |
| `review_id`               | string | optional | Review artifact identifier                                                     | Set on `review_result` events and related envelopes.                                                          |
| `review_request_id`       | string | optional | ID of review request message or job                                            | Use when linking events back to a specific review request.                                                    |
| `review_result_id`        | string | optional | ID of review result message or job                                             | Use when capturing async review completions.                                                                  |
| `actor_id` / `agent_id`   | string | optional | Mailbox/agent actor id (e.g. `actor:agent:dspy:<slug>`)                        | Set for agent-initiated events and tool calls.                                                                |
| `task_run_id` / `run_id`  | string | optional | Unique id per execution attempt of a given task                                | Useful for distinguishing retries of the same task.                                                           |
| `trace_parent`            | string | optional | Parent trace id                                                                | Set when a trajectory is spawned from another trajectory/trace.                                               |
| `job_attempt` / `attempt` | int    | optional | Non-negative integer; monotonic per `job_id`                                   | Incremented on each retry of the same job.                                                                    |
| `created_by`              | string | optional | Service or user identifier                                                     | E.g. `foxctl`, `actor:system:overseer`, `actor:human:<id>`.                                                 |
| `cas_digest`              | string | optional | `sha256:<hex>`; MUST match any referenced CAS artifact                         | MAY be set when `data_artifact` / `data.artifact` is present; MUST be omitted when no artifact is referenced. |

Unless otherwise specified, optional fields are omitted when not applicable;
they have no implicit default meaning.

Events SHOULD be derived from:

- Protocol v1 envelopes (tool calls, agent messages).
- Review artifacts (`review_gate.md`).
- Mailbox messages (`mailbox_blackboard.md`).
- Task status updates.

---

## 4. Capture Points

### 4.1 User Requests

User requests SHOULD be captured at:

- **CLI invocations** where the user expresses a goal:
  - E.g. `foxctl agent spawn --role coder --goal "Refactor X" ...`.
  - E.g. `foxctl todo add --title "..." --description "..."`.
- **Mailbox messages** authored by humans/admins:
  - `kind = "instruction" | "request"` messages with natural language bodies.
- **Viewer / API UIs** that collect freeform requests.

Implementations SHOULD:

1. Create a `UserRequestCapture` at the time of the request.
2. Attach `job_id` / `trace_id` if the request immediately triggers a job.
3. Use `task_hints` to pre-link to relevant tasks/epics where available.

### 4.2 Agent Runs (dspy-go)

When a dspy-go agent is spawned (see `dspy_go_agents.md`):

- Capture its **input signature** (Coding, Planning, or Review) as part of the
  trajectory:
  - `goal`, `description`, `workspace_id`, `epic_id`, `task_id`, `constraints`.
- Link the agent run to:
  - `root_request_id` (if started from a UserRequestCapture).
  - `job_id` and `trace_id` (from the job and Protocol v1 meta).

The agent runtime SHOULD:

- Serialize a full execution trace (thoughts, tool calls, reflections) to CAS.
- Emit a **trajectory index** entry (Trajectory + key events) referencing:
  - `job_id`, `trace_id`, `artifact_digest` for the full trace.

### 4.3 Review Outcomes

When the review gate produces a review artifact (`review_gate.md`):

- A `TrajectoryEvent` of kind `"review_result"` SHOULD be appended to any
  trajectory touching the same `(workspace_id, task_id)` and `trace_id`.
- The event SHOULD include:
  - `status` (`ok` / `failed`).
  - `checks` summary (names and statuses).
  - `artifact_digest` referencing structured AI review findings (if present).

Review outcomes provide labels for training (e.g. "this sequence led to a
successful review" vs "this sequence failed review").

### 4.4 Task Transitions

Trajectory events SHOULD also be created when tasks change state:

- `pending` → `in_progress`.
- `in_progress` → `ready_for_review`.
- `ready_for_review` → `completed`.

These events give downstream systems a way to understand when an episode
started, when it was ready for review, and when it ended.

---

## 5. Storage & Artifactization

### 5.1 Relationship to Protocol v1

All underlying execution is already represented as Protocol v1 envelopes
(`protocol_v1.md`). This spec does **not** define a new wire format.

Instead, it defines:

- **Index records** (Trajectory, UserRequestCapture, TrajectoryEvent) stored in
  SQLite and/or named memory.
- **Large payloads** (full traces, detailed tool results) stored in CAS and
  referenced by `artifact_digest`.

Implementations MUST follow Protocol v1 rules:

- `meta.cas_digest` is optional; if set it MUST match `data.artifact` and MUST
  be omitted when `data.artifact` is absent.
- Keep inline previews (`data.summary` / `data_inline`) ≤ configured size.
- Never store secrets in trajectories; secret redaction rules still apply.

### 5.3 Wire Contract & Error Codes

Trajectory capture and export reuse the Protocol v1 envelope. This section
specializes a few rules for trajectories.

#### 5.3.1 Wire Contract Rules

- **Inline vs CAS thresholds**
  - Respect `trajectory_capture.max_inline_bytes` (see §8) and/or the global
    Protocol v1 `inline_output_kb` value.
  - If a serialized trajectory payload (full trace, large tool logs) would
    exceed this threshold, it MUST be stored in CAS and referenced via
    `data.artifact` (and optionally `meta.cas_digest`).
  - `data_inline` / `data.summary` MUST remain small (≤ configured preview
    size); previews SHOULD be deterministic and secret-free.
- **Redaction**
  - All Protocol v1 redaction rules apply: no secrets or raw environment
    variables may be persisted in `data`, `meta`, or CAS artifacts.
  - Export jobs MAY apply additional redaction filters via
    `trajectory_capture.redact_patterns`.
- **Uniqueness & indexing constraints**
  - `Trajectory.id` MUST be unique per `(workspace_id, id)` pair.
  - `UserRequestCapture.id` MUST be unique per `(workspace_id, id)` pair.
  - `TrajectoryEvent.id` SHOULD be globally unique (ULID) or, at minimum, unique
    per `trajectory_id`.
  - `meta.correlation_id` MUST be present on all trajectory-related envelopes
    and SHOULD uniquely identify a single logical run within a workspace.
- **Ordering guarantees**
  - Within a given `trajectory_id`, events MUST be ordered by `ts` when
    presented to callers.
  - Implementations SHOULD ensure `ts` is monotonic for events emitted from the
    same process; cross-process skew is tolerated but SHOULD be bounded.

#### 5.3.2 Error Codes

Trajectory-related commands (capture/export jobs, indexers) SHOULD use the
following error codes in addition to the base catalog in `protocol_v1.md` §5:

| Code                                 | Recommended HTTP status | Meaning                                                                            |
| ------------------------------------ | ----------------------- | ---------------------------------------------------------------------------------- |
| `trajectory.capture.invalid_request` | 400                     | Malformed or missing required fields when capturing a trajectory or event          |
| `trajectory.capture.storage_error`   | 500                     | Failure persisting trajectory/index rows or CAS artifacts                          |
| `trajectory.export.access_denied`    | 403                     | Export attempted without sufficient permissions or workspace-level opt-in          |
| `trajectory.invalid_schema`          | 400                     | Envelope or episode shape does not conform to this spec                            |
| `trajectory_event.missing_trace_id`  | 400                     | Event missing `trace_id` (sourced from `meta.correlation_id`) where it is required |

These codes MUST appear in `error.code` when `status: "error"` and MAY be
augmented with additional context in `error.details` (e.g., offending
`trajectory_id`, `event_id`, or field names).

### 5.2 Suggested Tables / Memory Types (Non-Normative)

Example table-like concepts (implementation-defined):

- `trajectories` – stores Trajectory index rows.
- `user_requests` – stores UserRequestCapture rows.
- `trajectory_events` – stores TrajectoryEvent rows.

Alternatively or additionally:

- Named memory entries with types:
  - `"trajectory"`, `"user_request"`, `"trajectory_event"`.

The spec only requires that these items be:

- Queryable by `workspace_id` and time range.
- Joinable via `trajectory_id`, `job_id`, and `trace_id`.

---

## 6. dspy-Friendly Episode Schema

### 6.1 Episode Shape

For training/evaluation, trajectories are exported as **episodes** suitable for
dspy-go consumption.

Conceptual `TrajectoryEpisode` shape:

- `episode_id` (string) – usually the Trajectory `id` or a derived value.
- `workspace_id` (string).
- `task_id` (string, optional).
- `epic_id` (string, optional).
- `agent_role` (string) – `coder | planner | reviewer | overseer`.

- `input` (object):
  - `user_request` (string) – normalized user intent text.
  - `goal` (string) – from agent signature.
  - `description` (string) – extended context.
  - `constraints` ([]string).
  - `context_summary` (string, optional) – small summary of workspace/task
    context at start.

- `output` (object):
  - `status` (string) – `"ok" | "failed" | "review_failed" | "aborted"`.
  - `plan_summary` (string, optional) – description of steps taken.
  - `changes_summary` (string, optional) – high-level description of code
    changes.
  - `review_outcome` (object, optional):
    - `status` (string) – `ok` / `failed`.
    - `checks` (list of `{name, status}`).
  - `metrics` (object, optional):
    - `tool_calls` (int), `duration_ms` (int), etc.
    - `loc_added`, `loc_deleted`, `files_touched` (ints) – derived from diffs.
    - `review_iterations` (int), `review_latency_ms` (int) – number of review
      cycles and total wall-clock latency before acceptance or failure.
    - `tests_run` (int), `tests_failed` (int) – tests associated with the
      episode.
    - `maintainability_style_score` (float, 0.0–1.0) – coarse score reflecting
      adherence to workspace best practices and coding style, primarily derived
      from objective signals such as lint findings, complexity/duplication
      changes, and optional reviewer tags.

- `meta` (object):
  - `job_id`, `trace_id`, `review_id`, `agent_actor_id`, `llm_model`, etc.
  - `artifacts` (list of CAS digests) for full trace, diffs, logs.

This schema is **derived**, not stored as-is; it is generated by a trajectory
exporter based on index + CAS content.

### 6.2 Relation to dspy-go Signatures

- For **Coding Agents**, `input` aligns with the Coding Agent Signature in
  `dspy_go_agents.md` §6.1.
- For **Planning Agents**, `input` aligns with the Planning Agent Signature in
  `dspy_go_agents.md` §6.2.
- For **Review Agents**, `input` aligns with `ReviewInput` and `ReviewOutput`
  from `dspy_go_agents.md` §6.3.

The exporter SHOULD:

- Use agent input signatures as `input` for the episode.
- Use final agent outputs + review artifacts as `output` and `metrics`.

### 6.3 Signature-to-Metrics Mapping (Conceptual)

The following table summarizes how existing dspy-go agent signatures map onto
trajectory episode fields, metrics, and underlying artifacts:

| Role           | Primary signature inputs                                                                                                                               | Episode input / meta fields                                                                                                                                                    | Episode metrics & review signals                                                                                                                                                                             | Primary artifacts / sources                                                                                         |
| -------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------- |
| Coding Agent   | `goal`, `description`, `workspace_id`, `epic_id`, `task_id`, `scope_paths`, `constraints`, `mode` (see `dspy_go_agents.md` §6.1)                       | `input.user_request`, `input.goal`, `input.description`, `input.constraints`, `input.context_summary`, plus `workspace_id` / `epic_id` / `task_id` and `scope_paths` in `meta` | `metrics.tool_calls`, `metrics.duration_ms`, `metrics.loc_added`, `metrics.loc_deleted`, `metrics.files_touched`, `metrics.tests_run`, `metrics.tests_failed`                                                | CAS diffs and changed files, tool/job envelopes, telemetry events, test logs                                        |
| Planning Agent | `workspace_id`, `epic_id`/`root_task_id`, `goal`, `description`, `scope_paths`, current task list and graph insights (see §6.2 in `dspy_go_agents.md`) | `input.user_request`, `input.goal`, `input.description`, `input.context_summary` (often a summary of task graph state), plus `workspace_id` / `epic_id` in `meta`              | `metrics.tool_calls`, `metrics.duration_ms` and optional plan-size or graph metrics (non-normative)                                                                                                          | Plan deltas, mailbox messages, task/graph snapshots, CAS traces                                                     |
| Review Agent   | `ReviewInput` (workspace/task, goal/description, files/diff, constraints; see `dspy_go_agents.md` §6.3)                                                | `input.user_request` (e.g. "please review T-123"), `input.goal`, `input.description`, `input.context_summary` (short diff summary and scope), plus `task_id` in `meta`         | `output.review_outcome.status`, `output.review_outcome.checks`, `metrics.review_iterations`, `metrics.review_latency_ms`, `metrics.tests_run`, `metrics.tests_failed`, `metrics.maintainability_style_score` | Review artifacts from `review_gate.md` (structured findings, labels, checks), CAS diffs and logs, lint/test outputs |

This mapping is intended as a reference when adding new agent roles or
fine-tuning which fields and metrics to include in exported episodes.

### 6.4 Optimization Objectives and Weighting (Non-Normative)

Offline optimizers (GEPA, MIPRO, SIMBA-style) operating over trajectories MAY
combine episode metrics into a multi-objective score vector. A typical set of
objectives could include:

- **Success / correctness** – prefer episodes where `output.status = "ok"` and
  any associated reviews pass.
- **Spec and prompt adherence** – derived from review findings that flag
  requirement violations or missing behaviors.
- **Safety and robustness** – emphasized when security/performance findings are
  present or when follow-up bug-fix episodes appear soon after completion.
- **Maintainability and style** – using `metrics.maintainability_style_score`
  and related lint/complexity signals as a lower-weight but still meaningful
  objective.
- **Efficiency** – prefer lower `metrics.review_iterations`, lower
  `metrics.review_latency_ms`, and reasonable `metrics.tool_calls` / token
  usage, without sacrificing the higher-priority objectives above.

Exact weights and aggregation formulas are left to implementations. This spec
only standardizes the **inputs** (episode fields and metrics) so that workspaces
can experiment with different optimizers without changing the core trajectory
schema.

### 6.4.1 Visual Sketches

High-level flow from signatures to episodes to optimizers:

```text
Agent Signatures ──┐
                   │
                   ▼
Trajectory Episodes
                   │
                   ▼
  GEPA / MIPRO / SIMBA-style optimizers
       (multi-objective tuning)
```

Role-specific view (conceptual only):

```text
Coding Agent  : Signature → edits/tests → metrics (loc, tests, duration)
Planning Agent: Signature → plans/graph ops → metrics (tool_calls, duration)
Review Agent  : ReviewInput/Output → review artifacts → metrics
                               (iterations, latency, tests, maintainability_style_score)
```

### 6.5 Visual Sketches (Non-Normative)

High-level flow from user intent and signatures to optimizers:

```text
UserRequestCapture ──┐
                     │
Coder / Planner / Reviewer signatures
  (see dspy_go_agents.md §6.x)
                     │
                     ▼
      Trajectory + TrajectoryEvents
                     │
     (jobs + envelopes + CAS)
                     │
                     ▼
          TrajectoryEpisode
     (input, output, metrics, meta)
                     │
                     ▼
  GEPA / MIPRO / SIMBA-style optimizers
       (multi-objective tuning)
```

Role-specific view (conceptual only):

```text
Coding Agent  : Signature → edits/tests → metrics (loc, tests, duration)
Planning Agent: Signature → plans/graph ops → metrics (tool_calls, duration)
Review Agent  : ReviewInput/Output → review artifacts → metrics
                               (iterations, latency, tests, maintainability_style_score)
```

These sketches are intended as an orientation aid when wiring new agent roles or
metrics into the trajectory exporter and downstream optimizers.

---

## 7. Export Operations (Conceptual)

### 7.1 Trajectory Export Job

A conceptual job `trajectory.export` is responsible for building dspy-ready
episodes from stored data.

**Inputs (conceptual):**

- `workspace_id` (string)
- Filters:
  - `task_id` / `epic_id` (optional)
  - `since` / `until` timestamps (optional)
  - `agent_role` filter (optional)
  - `status` filter (optional)
- `format` (string) – e.g. `"ndjson" | "jsonl"`.
- `include_raw_traces` (bool, default `false`) – whether to include CAS digests
  for full traces.

**Behavior:**

1. Select trajectories matching filters.
2. For each trajectory:
   - Resolve UserRequestCapture, events, review artifacts, and job metadata.
   - Construct a `TrajectoryEpisode` object.
3. Stream episodes as NDJSON (Protocol v1 `status: "progress"` envelopes) or
   write them to CAS and return a digest.

**Output (conceptual):**

- For inline export:
  - Stream of envelopes with `data.episode` objects.
- For CAS export:
  - Single envelope with `data.summary` and `data.artifact` digest pointing to a
    NDJSON file of episodes.

### 7.2 Access Control

- Export SHOULD be opt-in per workspace (e.g. `trajectory_export.enabled`).
- Sensitive workspaces may restrict export to admin actors only.

---

## 8. Configuration

Conceptual config keys:

- `trajectory_capture.enabled` (bool, default `false`).
- `trajectory_capture.capture_user_requests` (bool, default `true`).
- `trajectory_capture.capture_agent_runs` (bool, default `true`).
- `trajectory_capture.capture_reviews` (bool, default `true`).
- `trajectory_capture.max_inline_bytes` (int, default from Protocol v1
  `inline_output_kb`).
- `trajectory_capture.redact_patterns` ([]string, optional) – additional
  redaction rules.
- `trajectory_export.enabled` (bool, default `false`).

Implementations MAY expose these via `~/.foxctl/config.yaml` or
workspace-local config.

---

## 9. Privacy, Safety, and Compliance

- Trajectories MUST respect the same redaction rules as Protocol v1 envelopes:
  never store secrets in cleartext.
- When exporting trajectories for training:
  - Workspaces SHOULD be able to filter by task/epic, time range, and actors.
  - Sensitive tasks SHOULD be excluded by default where practical.
- Trajectory exporters MUST ensure:
  - No raw environment variables or secrets are included.
  - Logs are filtered to remove or redact sensitive values.

---

## 10. Future Enhancements

Potential follow-ons:

- Richer episode segmentation (multiple episodes per trajectory).
- Online metrics dashboards over trajectories (success rates, review failure
  rates per task type).
- Fine-grained labeling / annotation tools integrated into the Agent Viewer.
- Cross-workspace and cross-project trajectory analysis under explicit user
  control.
