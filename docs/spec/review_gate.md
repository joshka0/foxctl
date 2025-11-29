# Review Gate Spec (Draft)

## Overview

The review gate introduces an optional but recommended step between "I think
this feature is done" and marking a task as **completed**. It defines:

- A canonical task lifecycle with a **reviewable** state.
- A **review pipeline** that can run automated checks (lint, tests, etc.) and
  record results as review artifacts.
- Integration points with hooks, mailbox/blackboard, overseer, and dspy-go
  agents so that reviews become first-class, observable events.

This spec is primarily about **behavior and data shapes**, not concrete SQL or
CLI layouts. It assumes the existing task storage, hooks, and mailbox/task graph
specs.

## Goals

- Make it easy to require a review step before tasks are marked completed.
- Provide clear task states so agents and humans know whether work is:
  - Not started
  - In progress
  - Waiting for review
  - Reviewed and done
- Define a minimal **review pipeline** that can run automated checks via
  existing skills and jobs.
- Attach review results to tasks as durable artifacts (CAS + memory), so future
  sessions can see what was validated.
- Integrate with mailbox/overseer and task graph insights for prioritizing
  review work.

## Non-Goals

- Define a full permission model for who may review or override.
- Mandate specific linters, test commands, or quality bars.
- Implement a full CI system inside agentctl; the pipeline may call out to
  existing tools.
- Replace human code review in Git; this spec focuses on **task-level** review
  within a workspace.

## Task Lifecycle & States

This spec refines the conceptual task lifecycle defined in the tasks storage and
`todo/manage` skill.

### Status values

At a high level, tasks can be in one of several states:

- `pending` – Task exists but no substantial work has started.
- `in_progress` – Someone (agent or human) is actively working on this task.
- `ready_for_review` – Implementation is believed complete and is waiting on the
  review pipeline and/or human/overseer signoff.
- `completed` – Task has passed review and is considered done.
- `blocked` – Task is currently blocked (e.g., on dependencies or other issues).
  Optional in this spec but commonly used.
- `canceled` – Task has been abandoned or superseded.

Implementations MAY support additional states, but the semantics of
`ready_for_review` and `completed` are normative for this spec.

### Allowed transitions (conceptual)

For the purposes of the review gate, the following transitions are expected:

- `pending` → `in_progress` – work starts.
- `in_progress` → `ready_for_review` – worker believes implementation is
  complete and requests review.
- `ready_for_review` → `completed` – review pipeline has passed and, if
  required, a human/overseer has acknowledged.
- `ready_for_review` → `in_progress` – review failed or requested changes;
  worker returns to implement fixes.
- Any non-terminal state → `blocked`/`canceled` as appropriate.

Direct transition `in_progress` → `completed` SHOULD be discouraged when the
review gate is enabled for a workspace. Implementations MAY still expose an
explicit override path for emergencies (e.g. `--force`), but this is
non-normative.

## Data Model

This section describes logical structures used by the review gate. Actual
storage could be additional columns on the task table or separate tables.

### Tasks (extended semantics)

Tasks continue to carry at least:

- `id` (string)
- `workspace_id` (string)
- `title` (string)
- `status` (string)
- `depends_on` (list of task IDs)
- Optional hierarchy/metadata (`parent_id`, `scope_path`, etc.)

For the review gate, tasks conceptually also have:

- `last_review_status` (string, optional)
  - `ok`, `failed`, or `pending`.
- `last_review_at` (timestamp, optional).
- `last_review_id` (string, optional) – link to most recent review artifact.

These may be stored explicitly or derivable by querying recent review artifacts
for the task.

### Review artifacts

A review artifact represents one execution of the review pipeline for a task.

Logical fields:

- `id` (string) – Unique review identifier.
- `workspace_id` (string).
- `task_id` (string).
- `kind` (string) – `auto`, `human`, or `mixed`.
- `status` (string) – `ok` or `failed`.
- `inputs` (object, optional) – describes what was actually reviewed:
  - `files` (list of objects):
    - `path` (string) – file path relative to workspace root.
    - `digest` (string) – CAS digest of the file contents at review time.
    - `changed_since_last_review` (bool, optional) – whether this file changed
      since the previous review for this task.
  - `diff_digest` (string, optional) – CAS digest of a diff/patch blob that was
    presented to reviewers (AI or human).
- `labels` (list of strings, optional) – high-level tags such as `"holistic"`,
  `"security"`, `"performance"`.
- `checks` (list of objects):
  - `name` (string) – e.g. `lint`, `tests-short`, `tests-race`, `ai_review`,
    `security_review`.
  - `status` (string) – `ok` or `failed`.
  - `details` (string, optional) – human-readable summary.
  - `artifact_digest` (string, optional) – CAS digest for large logs/output or
    structured payloads (such as AI review findings).
  - `reviewer_role` (string, optional) – logical role used by the reviewer, e.g.
    `holistic_reviewer`, `security_analyst`, `perf_analyst`.
  - `tags` (list of strings, optional) – additional tags such as `"holistic"`,
    `"security"`, or `"performance"`.
- `created_at` (timestamp).
- `created_by` (string) – actor ID (agent or human) that initiated the review
  (often `actor:system:overseer` for automated reviews).

Implementations SHOULD:

- Persist review artifacts in SQLite (review table) **and/or**
  - Use CAS for large logs and attach summaries via the memory subsystem
    (`task_hooks_memory.md`).

## Review Pipeline

The review pipeline is the series of automated steps run when a review is
requested or when completion is attempted with the gate enabled.

### Triggers

A review MAY be triggered by any of the following actions:

- Explicit **review request**:
  - User or agent runs `agentctl todo review <task-id>` or equivalent.
  - `todo/manage` operation `review_request` (see below).
- Implicit on **completion attempt**:
  - When `agentctl todo complete <task-id>` is invoked and the task is not
    already in `ready_for_review` with a passing review, the pipeline MAY be run
    before transitioning to `completed`.

Workspace configuration MAY control whether review is required before
completion, but when the gate is enabled, the following behavior SHOULD hold.

### Minimal pipeline

At minimum, a review pipeline SHOULD be able to:

1. Run static checks / lint, including workspace-defined quality gates such as
   complexity/duplication thresholds and max file/function lengths where
   configured.
2. Run fast tests (e.g. `-short`).
3. Optionally run additional checks (formatting, security, etc.).

These checks are expected to be implemented via existing skills and jobs, for
example:

- `tests.run` or a similar skill for invoking `go test` / `make test-short`.
- Other existing `ci_*` or `code_*` skills as configured.

### Execution & recording

Conceptual execution steps:

1. **Prepare plan**
   - Resolve `workspace_id` and `task_id`.
   - Determine which checks to run for this workspace (e.g. from
     `~/.agentctl/config.yaml` or workspace-local config).

2. **Submit jobs**
   - For each check, submit a job to the jobs subsystem, capturing:
     - Command/skill invoked.
     - Arguments, environment, and workspace context.

3. **Wait for results**
   - Collect job results; treat non-zero exit or error envelopes as failures.

4. **Persist review artifact**
   - For each check, store short summary and CAS digest for detailed logs.
   - Create a review artifact with aggregate `status=ok` only if all checks
     passed.
   - Write a memory entry for `(workspace_id, task_id)` with type `review`
     containing:
     - Short summary of checks and result.
     - Links to CAS artifacts.

5. **Update task state**
   - If `status=ok`:
     - Set task `status` to `ready_for_review` or `completed` depending on
       trigger and policy.
     - Update `last_review_status=ok`, `last_review_at`, `last_review_id`.
   - If `status=failed`:
     - Leave or set task `status` to `in_progress` (review failed; more work
       needed).
     - Update `last_review_status=failed` fields.

Exact transition behavior is controlled by workspace policy (see below), but
review artifacts MUST be recorded regardless.

## AI / agent reviewers

AI- or agent-based reviewers are modeled as specific **checks** within a review
artifact. They typically produce structured findings instead of, or in addition
to, freeform text.

- Common check names include:
  - `ai_review` – general/holistic review of changes.
  - `security_review` – security-focused review.
  - `perf_review` – performance-focused review.
- For these checks, `artifact_digest` SHOULD point to a CAS blob containing a
  structured JSON payload such as:

  ```jsonc
  {
    "summary": "High-level verdict, e.g. 'OK with nits' or 'Refactor recommended'",
    "findings": [
      {
        "file": "agentctl/internal/agent/runtime/overseer.go",
        "range": { "start_line": 77, "end_line": 93 },
        "severity": "warn", // info | warn | error
        "category": "code_smell", // bug | smell | style | test | docs | perf | security
        "message": "Concurrent agent limit check is TOCTOU; rely on atomic runtime check instead.",
        "suggested_fix": "Use Runtime.Spawn() limit enforcement and treat overseer check as advisory."
      }
    ]
  }
  ```

Overseer behavior for AI/agent reviewers:

- The overseer is the **orchestrator** for review checks. It:
  - Consumes `review_request` events (often via mailbox messages).
  - Decides which checks to run based on workspace configuration
    (`review.checks`).
  - Owns the top-level review artifact (`created_by` typically set to
    `actor:system:overseer`).
- Implementations MAY have the overseer:
  - Perform a holistic `ai_review` itself as part of its own dspy-go program, or
  - Spawn dedicated reviewer subagents with specific roles (e.g.
    `security_analyst`, `perf_analyst`) that produce their own findings. The
    overseer then aggregates or references those findings in the main review
    artifact using `reviewer_role` and `tags`.

Downstream agents can use the structured findings and tags to:

- Identify which parts of the code need changes.
- Filter by concern (e.g. show only `security` or `performance` findings).
- Decide whether it is safe to advance a task to `completed`.

## Review Quality Metrics (Conceptual)

Review artifacts and trajectories SHOULD carry a small set of derived metrics to
support analysis and offline optimizers. Non-normative examples include:

- `loc_added`, `loc_deleted`, `files_touched` – basic change size signals.
- `review_iterations` – number of review cycles for a task before acceptance.
- `review_latency_ms` – wall-clock latency between first review request and
  final acceptance or failure.
- `tests_run` / `tests_failed` – number of test runs and failures associated
  with this review episode.
- `maintainability_style_score` – a coarse score (e.g. 0.0–1.0) reflecting
  adherence to workspace best practices and coding style, primarily derived from
  objective signals such as lint findings, complexity/duplication changes, and
  optional reviewer tags.

These metrics are derived from existing artifacts (diffs, test logs, lint
results, and review findings) and SHOULD be exposed in trajectory exports
(`dspy_trajectory_capture.md`) rather than as new wire-level fields.

## APIs / Skills

The review gate extends the `todo/manage` skill with additional operations and
semantics.

### `todo/manage.review_request`

- **Command**: `todo/manage` with `operation = "review_request"`.
- **Inputs** (`data`):
  - `workspace_id` (required).
  - `task_id` (required).
  - `kind` (optional) – `auto`, `human`, or `mixed` (default `auto`).
  - Optional hints (non-normative): which checks to run, priority, etc.
- **Behavior** (conceptual):
  - Validates that the task exists and is in a state that can be reviewed
    (`in_progress` or `ready_for_review`).
  - Initiates the review pipeline as described above.
  - Optionally sets task `status` to `ready_for_review` immediately, or only
    after checks pass, depending on policy.
- **Outputs** (`data`):
  - `task_id`.
  - `status` (current task status after triggering).
  - `review_id` (if created synchronously) or `job_id` (if review runs
    asynchronously).
  - Optional summary of which checks were scheduled.

### `todo/manage.complete` (revised semantics)

Existing `complete` semantics are extended to respect the review gate when
enabled for a workspace.

- **Command**: `todo/manage` with `operation = "complete"`.
- **Inputs** (`data`):
  - `workspace_id`.
  - `task_id`.
  - Optional flags (non-normative): `force` / `no_review`.
- **Behavior** (when review gate enabled):
  - If the task has a recent **passing** review artifact and is in
    `ready_for_review`, transition to `completed`.
  - Otherwise, either:
    - Trigger `review_request` implicitly and wait for result, or
    - Return an error / warning envelope indicating that review is required
      before completion.
  - Implementations MAY allow explicit overrides (e.g. admin forcing
    completion), but this is left to policy.

- **Outputs** (`data`):
  - Final `status` for the task.
  - Optional `review_id` or `last_review_status` fields for clarity.

### `todo/manage.review_status` (optional)

An optional helper operation for tooling and UIs.

- **Inputs**:
  - `workspace_id`.
  - `task_id`.
- **Outputs**:
  - `last_review_status`.
  - `last_review_at`.
  - `last_review_id`.
  - A short summary of the most recent review.

Implementations MAY fold this data into other operations instead of defining a
separate endpoint.

## CLI UX (Non-Normative)

CLI commands are thin wrappers over `todo/manage`.

Examples:

- `agentctl todo review <task-id>`
  - Calls `todo/manage.review_request`.
  - Streams or prints a short summary when review finishes.

- `agentctl todo complete <task-id>`
  - Uses `todo/manage.complete` semantics.
  - When review is required and not yet passing, prints a clear message
    describing what is missing (e.g. checks that failed).

- `agentctl todo status <task-id>`
  - MAY include review-related fields (e.g. last review result and time).

Exact flag names and output formatting are implementation details.

## Hook Integration

Hooks provide the glue between user/agent behavior, task state, and review
artifacts.

### Task guard (`hooks/task_guard`)

- Ensures write operations are associated with an active `task_id`.
- When a task is in `ready_for_review` or `completed` and new writes occur under
  its scope, `hooks/task_guard` MUST:
  - Automatically move the task status back to `in_progress`.
  - Treat any previously `ok` review for that task as **stale** so that a new
    review is required before the task can be considered `completed` again.

When `hooks/task_guard` is configured in strict mode and blocks a write, callers
that surface this as a top-level tool error SHOULD map the block to
`E_GUARD_VIOLATION` (see `dspy_go_agents.md` §11.3) and treat it as a
non-retryable, scope/guard violation rather than a transient runtime error.

Dirtying policy is **kernel-owned**: agents and reviewers do not make this
decision. Implementations MAY vary in how they present stale reviews in UIs, but
MUST preserve the semantics that new writes after a successful review invalidate
that review for the relevant task scope.

### Test watcher & feedback (`hooks_test_feedback`)

The existing PostToolUse test feedback hook already surfaces failing tests.

- Review pipelines SHOULD reuse the same test parsing and storage mechanisms
  where possible so that:
  - Failing tests captured by the watcher are visible in review artifacts.
  - Developers and agents see a consistent view of test failures.

### Context injection (`hooks/context_inject`, future)

- When a task is in `ready_for_review` or has a failed review, the context
  injection hook MAY:
  - Surface a short summary of the last review (what failed, where logs are).
  - Link to CAS artifacts for detailed inspection.

## Mailbox & Overseer Integration

The review gate builds on the mailbox/blackboard and task graph specs.

### Review requests as mailbox messages

When a review is requested (explicitly or implicitly), implementations MAY:

- Create a `mailbox/send` message with:
  - `kind = "review_request"`.
  - `task_id` set to the relevant task.
  - `recipient` set to a reviewer or `admin`.
  - `ack_required = true` if human signoff is desired.

The `mail_router` hook will surface these messages in the reviewer/admin
context, and the agent viewer can show outstanding review requests.

### Review results as mailbox messages

When a review finishes:

- On success:
  - Optionally send a `kind = "review_result"` message to the original requester
    and/or admin summarizing checks.
- On failure:
  - Send a `review_result` message with `status = "failed"` and links to CAS
    artifacts.

These messages give humans and overseer agents a compact view of which tasks
need attention.

### Overseer behavior

The overseer (as defined in `overseer_profile.md` and `task_graph_insights.md`)
can:

- Use `todo/manage.graph_insights` to find high-impact tasks.
- Filter for tasks that are stuck in `ready_for_review` or have
  `last_review_status = failed`.
- Send nudges or assignments via mailbox messages (e.g. "please review T-123" or
  "fix failing tests for T-456").

In addition to coordinating review work, the overseer MAY trigger post-review
indexers such as the semantic file index described in `semantic_file_index.md`
and the code symbol index described in `code_symbol_index_and_swe_grep.md` to
keep semantic search and related code indexes aligned with accepted changes.

## dspy-go Agents & Trajectories

For dspy-go agents (`dspy_go_agents.md`, `agent_hierarchy.md`):

- Coder agents that call `todo/manage.complete` must be prepared for completion
  to be blocked pending review.
- Planner/overseer agents can:
  - Decide when to trigger `review_request` based on task graph metrics and
    mailbox state.
  - Use review artifacts as part of ExecutionTrace when exporting training data
    (e.g. "what checks were run before completion?").

Trajectories can later include:

- User request → tasks created → code changes → review pipeline runs → outcome
  (ok/failed) → final completion.

## Configuration & Policy

Workspaces MAY configure review behavior via config files (exact keys
implementation-defined). Examples of policy knobs:

- `review.require_for_completion` (bool; default true for new workspaces).
- `review.checks` (list): which named checks to run and, optionally, which
  reviewer roles/tags to use. For example:

  ```yaml
  review:
    require_for_completion: true
    checks:
      - name: lint
      - name: tests-short
      - name: ai_review
        role: holistic_reviewer
        tags: ["holistic"]
      - name: security_review
        role: security_analyst
        tags: ["security"]
        optional: true
  ```

  The overseer uses this configuration to decide which checks to run and which
  dspy-go roles (if any) to spawn for AI-based reviewers.

- `review.auto_on_complete` (bool): whether `todo complete` auto-triggers a
  review.
- `review.human_ack_required` (bool): whether human/admin `ack` is required
  after an `ok` review before final completion.

This spec does not mandate the existence or names of these keys, but any
configuration MUST preserve the core semantics:

- When the gate is enabled, `completed` SHOULD mean "has passed at least one
  `ok` review since the last relevant code changes".

## Phasing

Suggested implementation phases:

1. **Core semantics**
   - Introduce `ready_for_review` status and review artifacts.
   - Implement `todo/manage.review_request` and updated `complete` semantics.

2. **Hook & mailbox integration**
   - Wire review requests/results into mailbox messages.
   - Add context injection for review summaries.

3. **Overseer & UI**
   - Extend overseer behavior and viewer dashboards to highlight tasks waiting
     for review and failed reviews.

Later revisions MAY refine status names, add richer review types, or tighten
requirements based on practical usage.
