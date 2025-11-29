# dspy-go Agent Runtime for agentctl

**Status:** Draft\
**Scope:** Integrate dspy-go agents as first-class workers in agentctl
workspaces.\
**Related specs:** `overseer_profile.md`, `overseer_planning.md`,
`mailbox_blackboard.md`, `task_graph_insights.md`, `test_watch_feedback.md`,
`unified_agents.md`, `dspy_trajectory_capture.md`,
`code_symbol_index_and_swe_grep.md`

---

## 1. Overview

This spec defines how **dspy-go** agents become first-class citizens in the
`agentctl` ecosystem:

- Use dspy-go (ReAct-style agents) as the **primary agent runtime**.
- Treat `agentctl` as the **kernel**: skills, jobs, CAS, tasks, mailbox, hooks.
- Ensure all agent reads/writes go through `agentctl` skills so hooks, guards,
  and telemetry continue to work.
- Make agents observable and controllable via a future **Agent Viewer** TUI/GUI.

The goal is that coding with a dspy-go agent feels like a native part of
`agentctl`, not a bolt-on: agents understand tasks and plans, respect
constraints from the overseer, and coordinate via the mailbox/blackboard.

---

## 2. Goals & Non‑Goals

### 2.1 Goals

- **First-class coding agents**
  - Implement dspy-go based agents that can: read/search/edit code, run tests,
    and update tasks using existing `agentctl` skills.
- **Task- and plan-aware execution**
  - Agents operate in the context of a workspace, epic, and task graph, taking
    constraints from the overseer via mailbox messages.
- **Hook-compatible flows**
  - All file edits and risky operations must go through `agentctl` runners so
    `task_guard`, `file_guard`, and `test_watch_feedback` hooks still fire.
- **Observable and debuggable**
  - Agent execution history (thoughts, tool calls, results) must be visible in
    jobs storage and consumable by an Agent Viewer TUI/GUI.
- **Robot protocol friendly**
  - Reuse the "robot JSON" pattern from `beads_viewer` so other tools or agents
    can ask: "what is this agent doing?", "what should I work on next?".

### 2.2 Non‑Goals

- Replace the existing CLI or skill system. dspy-go agents use skills; they do
  not bypass them.
- Hard-code a specific LLM provider. The agent runtime must work with any
  `dspy-go` `core.LLM` implementation (Gemini, OpenAI, etc.).
- Define the full TUI/GUI UX in this spec. We only define the data and
  integration points required for a viewer.

---

## 3. Architecture

### 3.1 Components

- **dspy-go Agent Runtime**
  - Implemented in Go, using `dspy-go`'s `ReActAgent` (or compatible) type.
  - Runs as a long-lived process started by `agentctl` (CLI subcommand or future
    daemon).
- **Agent Tools Layer**
  - A set of `dspy-go` tools (`core.Tool` implementations) that wrap `agentctl`
    skills and local capabilities.
  - All filesystem, test, and task operations MUST go through these tools.
- **Agentctl Kernel**
  - Existing skills (`todo/manage`, `mailbox/manage`, filesystem helpers,
    search, etc.).
  - Jobs, CAS, and memory stores.
  - Hook system (`task_guard`, `file_guard`, `mail_router`, `test_feedback`).
- **Mailbox / Blackboard**
  - SQLite-backed mailbox from `mailbox_blackboard.md`.
  - Used for plan ownership (`actor:system:overseer`), constraints, and agent
    instructions.
- **Agent Viewer (future)**
  - TUI/GUI that reads:
    - Task graph insights
    - Mailbox state
    - Agent runs and tool traces
    - Telemetry (recent reads/tests/edits)

### 3.2 Process Model

- Agents run as one or more **long-lived processes**:
  - `agentctl agent spawn` (proposed) starts a dspy-go agent with:
    - workspace_id, epic_id/task_id
    - actor_id (mailbox identity)
    - role (`coder`, `planner`, etc.)
  - Agents maintain their own internal loop using dspy-go planning/reflection
    and call tools as needed.
- Agents must be **stateless across process restarts** except for:
  - Mailbox state (persistent).
  - Tasks DB and jobs storage.
  - Optional agent-specific memory stored via `agentctl` memory or dspy-go's
    persistence, as long as it is anchored to the workspace.

---

## 4. Agent Roles & Identities

### 4.1 Actor IDs

Agent mail identities SHOULD follow the mailbox conventions:

- Worker / coding agents: `actor:agent:dspy:<slug>`
- Planner agents: `actor:agent:dspy-planner:<slug>`
- Reviewer agents (if added): `actor:agent:dspy-reviewer:<slug>`

Each agent instance MUST:

- Use a stable `actor_id` for mailbox send/inbox operations.
- Tag all tool usage / telemetry with `actor_id` when practical.

### 4.2 Agent Types

At minimum, this spec defines two logical agent types:

- **Coding Agent** (executor)
  - Focused on implementing code changes and running tests for a given task or
    small cluster of tasks.
- **Planning Agent** (planner)
  - Focused on refining or generating plans using `todo/manage.plan` and
    `todo/manage.graph_insights`, but not directly editing files.

The overseer remains the authority for epic-level plan changes per
`overseer_profile.md`. Planner agents MAY propose changes via mailbox, but MUST
NOT apply them directly without overseer involvement.

### 4.3 Teams and Assignments

Teams are logical groupings of actors (agents and humans) within a workspace,
used for assigning and routing work. They help the overseer and viewers reason
in terms of **agile teams** rather than individual agents.

#### 4.3.1 Team identity

- Teams are identified by a stable `team_id` within a workspace.\
  Example: `team:backend`, `team:frontend`, `team:infra`.
- Mailbox MAY support **team aliases** as recipients, e.g.
  `recipient =
  "team:backend"`, which SHOULD fan out to current team members
  or appear in a shared team inbox view.

Non-normative data shape for a team:

- `team_id` (string) – unique per workspace. \
- `workspace_id` (string). \
- `name` (string) – human-readable label. \
- `description` (string, optional). \
- `members` (list of):
  - `actor_id` (string) – e.g. `actor:agent:dspy:<slug>` or
    `actor:human:<name>`. \
  - `role` (string) – `coder`, `reviewer`, `planner`, `fixer`, etc. \
  - Optional metadata: skills, capacity, tags.
- `primary_epics` ([]string, optional) – epic IDs this team primarily owns. \
- `tags` ([]string, optional).

This data SHOULD live in a dedicated teams store (e.g. SQLite tables `teams` and
`team_members`) so that future `teams/manage` skills have a stable schema.
Configuration files MAY seed initial team definitions, but runtime updates and
dashboards SHOULD use the store.

#### 4.3.2 Use in planning and execution

- The overseer MAY assign **epics or tasks to teams**, not just individual
  actors.\
  Example: epic owned by `team:backend`, with specific tasks delegated to
  individual agents later.
- Planner agents SHOULD take team ownership into account when proposing plans
  (e.g. grouping tasks per team, avoiding cross-team contention where possible).
- Coding agents MAY use team information to:
  - Decide when to request help or review from a teammate. \
  - Route `review_request` or `info` mails to `team:<slug>` instead of a single
    actor.

#### 4.3.3 Viewer expectations

Agent Viewers (TUI/GUI) SHOULD provide team-centric views:

- Per-team boards showing:
  - Epics/tasks owned by the team. \
  - Active agents (coding/planning) belonging to the team. \
  - Unread admin/overseer messages for the team and its members.
- Ability for an admin/overseer to:
  - Send instructions to `team:<slug>`. \
  - Reassign epics/tasks between teams. \
  - See reservations and conflicts aggregated at team level.

---

## 5. Canonical Tools (dspy-go Tool Set)

This section defines a **minimum tool set** that dspy-go coding agents SHOULD
support. All tools are dspy-go `core.Tool` instances with schemas that map
cleanly to `agentctl` skills.

### 5.1 Filesystem & Code Inspection

These tools MUST route through `agentctl` skills or safe helpers so hooks and
policies apply.

- **`fs.list_dir`**
  - Purpose: List contents of a directory.
  - Inputs: `workspace_id`, `path` (relative to workspace), optional `depth`.
  - Backend: Filesystem helper skill or internal helper that respects
    `policy.PathValidator` and `AGENTCTL_WORKSPACE`.
  - Notes: Writes telemetry entry of kind `read`.

- **`fs.read_file`**
  - Purpose: Read text of a file (with size limits).
  - Inputs: `workspace_id`, `path`, optional `max_bytes`.
  - Backend: Filesystem helper skill (`fs/read`) or equivalent.
  - Hooks: read-only; SHOULD still be visible in telemetry.

- **`code.search`**
  - Purpose: Search for patterns in code.
  - Inputs: `workspace_id`, `query`, `globs`, optional `max_results`.
  - Backend: Search skill (e.g. ripgrep wrapper) that runs via `agentctl`.
  - Hooks: advisory only; results also recorded in telemetry.

- **`code.outline`** (optional v1)
  - Purpose: Get a structural outline of a file (symbols, functions, etc.).
  - Inputs: `workspace_id`, `path`.
  - Backend: Language-aware helper skill if/when available.

- **`code.symbol_search`** (optional v1, strongly recommended later)
  - Purpose: Retrieve relevant symbols and files using the code symbol index and
    call graph.
  - Inputs: `workspace_id`, `question`, optional `mode` (`"search"`,
    `"callers"`, `"callees"`), and filters.
  - Backend: Internal helper backed by the symbol index described in
    `code_symbol_index_and_swe_grep.md`.

- **`code.swe_grep`** (optional v1, strongly recommended later)
  - Purpose: Given a question and candidate files/symbols, extract high-signal
    code snippets via live reads and the SWE Grep skill.
  - Inputs: `workspace_id`, `question`, candidate files/symbols.
  - Backend: `code/swe_grep` exec skill as defined in
    `code_symbol_index_and_swe_grep.md`.

### 5.2 Editing & Refactoring

All writing tools MUST go through `agentctl` skills so `task_guard` and
`file_guard` can enforce reservations and scope rules.

- **`edit.apply_patch`**
  - Purpose: Apply a minimal patch to one file.
  - Inputs:
    - `workspace_id`
    - `path`
    - `patch` (JSON structured diff as emitted by the `code_diff` skill; not raw
      shell patch)
    - `reason` (human-readable justification)
  - Backend: Edit skill that ultimately uses `replace_file_content` or
    equivalent under the hood.
  - Hooks:
    - `task_guard` verifies task scope.
    - `file_guard` reserves paths and may block on conflicts.
    - `test_feedback` may run tests afterwards.

- **`edit.multi_patch`** (optional v1)
  - Purpose: Apply patches to multiple files in one atomic operation.
  - Inputs: `workspace_id`, `patches[]` (list of {path, patch, reason}).

### 5.3 Tests & Quality

- **`tests.run`**
  - Purpose: Run tests relevant to the current workspace/path/task.
  - Inputs:
    - `workspace_id`
    - Optional `pattern` / `package` / `path` filters.
  - Backend: Test CLI helper or skill that integrates with the
    `test_watch_feedback` system.
  - Outputs: summary of pass/fail counts, failing tests, and CAS digests for
    full logs if large.

- **`tests.list_failures`**
  - Purpose: Query last-known failing tests from the test-watch DB.
  - Inputs: `workspace_id`, optional `since` timestamp.

### 5.4 Tasks & Planning

- **`todo.query`**
  - Purpose: Read tasks from the store, scoped by workspace/epic/task.
  - Inputs:
    - `workspace_id`
    - Optional filters: `epic_id`, `task_id`, `status`, `tags`.
  - Backend: `todo/manage` list-like operations.

- **`todo.update`**
  - Purpose: Update task status, title, description, or metadata.
  - Inputs: `workspace_id`, `task_id`, changes.
  - Constraints:
    - Coding agents MAY mark tasks `in_progress` or add annotations.
    - Only overseer (or planner under explicit policy) MAY apply epic-level plan
      mutations (`plan.apply`).

- **`todo.graph_insights`**
  - Purpose: Get PageRank/critical-path metrics and other insights.
  - Inputs: `workspace_id`, optional `epic_id`.
  - Backend: `todo/manage.graph_insights` skill operation.

### 5.5 Mailbox / Blackboard

These tools integrate with `mailbox_blackboard.md`.

- **`mail.send`**
  - Purpose: Send messages into the blackboard.
  - Inputs:
    - `workspace_id`
    - `sender` (agent's `actor_id`)
    - `recipient` or broadcast
    - `subject`, `body`
    - Optional: `task_id`, `stream`, `kind`, `priority`, `ack_required`.
  - Backend: `mailbox/manage.send` skill.

- **`mail.inbox`**
  - Purpose: Read messages targeting the agent.
  - Inputs:
    - `workspace_id`
    - `actor_id`
    - Optional: `task_id`, `stream`, `only_unread`, `since`.
  - Backend: `mailbox/manage.inbox`.

- **`mail.ack`**
  - Purpose: Ack messages once processed.
  - Inputs: `workspace_id`, `actor_id`, `message_ids[]`.

These tools allow dspy-go agents to respect overseer plan messages
(`plan.created`, `plan.updated`, `plan.review_needed`) and new constraints
without being restarted.

### 5.6 Telemetry / Context Recents

- **`context.recents`** (optional v1, strongly recommended later)
  - Purpose: Query recent reads/searches/tests for the workspace and/or task.
  - Inputs: `workspace_id`, optional `task_id`, `limit`, `since`.
  - Backend: Telemetry store that records tool usage.

Agents SHOULD use `context.recents` to reconstruct short-term working context
instead of relying solely on their own memory.

---

## 6. Agent Signatures

dspy-go encourages explicit, typed signatures. This spec defines two canonical
signatures for agents that operate in `agentctl` workspaces.

### 6.1 Coding Agent Signature

**Input fields (conceptual):**

- `goal` (string) – One-sentence description of what to accomplish.
- `description` (string) – Richer context, possibly from user prompt or plan.
- `workspace_id` (string) – Workspace anchor.
- `epic_id` (string, optional) – Epic / plan root.
- `task_id` (string, optional) – Primary task being worked on.
- `scope_paths` ([]string) – Allowed directories/files (enforced by
  `task_guard`).
- `constraints` ([]string) – Human- or overseer-specified constraints.
- `mode` (string) – e.g. `"analyze" | "edit" | "test" | "mixed"`.

**Output fields (conceptual):**

- `status` (string) – `"ok" | "needs_review" | "blocked" | "error"`.
- `summary` (string) – Human-readable summary for overseer/human.
- `changed_files` ([]string) – Relative paths modified.
- `new_tasks` ([]TaskRef) – Tasks to create or update.
- `plan_updates` ([]PlanDelta) – Suggested changes to the plan (never applied
  directly by the coding agent).
- `mail_drafts` ([]MailDraft) – Messages the agent recommends sending (may be
  auto-sent via `mail.send` with policy).

The actual Go implementation SHOULD use `core.NewTypedSignature` with a struct
matching the above fields, then convert to a legacy signature for
`ReActAgent.Initialize`.

### 6.2 Planning Agent Signature

**Input fields:**

- `workspace_id`
- `epic_id` or `root_task_id`
- `goal`, `description`
- `scope_paths`
- Current task list and insights snapshot (optionally passed in, or fetched via
  tools).

**Output fields:**

- Proposed plan in a machine-readable form compatible with `todo/manage.plan`:
  - New tasks, dependencies, priorities.
  - Suggested changes (split/merge/reorder).
- Rationale and risk notes.
- Optional mails to overseer with subject `plan.suggest_change:<epic-id>`.

Planner agents MUST NOT call `todo/manage.plan` with `mode="apply"` directly.
Application of proposed plans is overseer-only.

### 6.3 Review Agent Signature

Review agents are specialized dspy-go agents that perform structured reviews of
code for a specific task and workspace. Their inputs and outputs are designed to
align with the review artifact structure in `review_gate.md`.

#### 6.3.1 ReviewInput (conceptual)

**Input fields:**

- `workspace_id` (string) – Workspace anchor.
- `task_id` (string) – Task being reviewed.
- `goal` (string) – One-sentence description of what the review should focus on
  (e.g. "holistic review", "security review for new auth flow").
- `description` (string) – Richer context (user request, acceptance criteria,
  links to design docs).
- `files` (list):
  - `path` (string) – relative path.
  - `content` (string) – current file contents or a truncated view (agent
    runtime MAY load this via `fs.read_file`).
  - `digest` (string, optional) – CAS digest for traceability.
- `diff` (string, optional) – Unified diff or patch representing the changes
  under review (may be reconstructed from CAS and edit history).
- `constraints` ([]string, optional) – Any additional constraints from
  overseer/admin (e.g. "do not relax validation", "avoid new dependencies").

The runtime SHOULD ensure that the `files` and `diff` presented here correspond
to what will later be recorded in the review artifact `inputs` section
(`inputs.files`, `inputs.diff_digest`).

#### 6.3.2 ReviewOutput (conceptual)

**Output fields:**

- `status` (string) – `"ok" | "failed" | "needs_changes"`.
- `summary` (string) – High-level verdict suitable for humans/overseer.
- `labels` ([]string, optional) – High-level tags such as `"holistic"`,
  `"security"`, `"performance"`.
- `findings` (list):
  - `file` (string) – relative path.
  - `range` (object, optional): `{ "start_line": int, "end_line": int }`.
  - `severity` (string) – `"info" | "warn" | "error"`.
  - `category` (string) –
    `"bug" | "smell" | "style" | "test" | "docs" | "perf" | "security"`.
  - `message` (string) – Description of the issue.
  - `suggested_fix` (string, optional) – Concrete guidance for how to address
    the issue.

This structure is intentionally compatible with the structured JSON payload
described under **AI / agent reviewers** in `review_gate.md`. The overseer (or
runtime) MAY:

- Serialize `ReviewOutput` to JSON, store it in CAS, and set the
  `artifact_digest` for a corresponding review `check` (e.g. `ai_review`,
  `security_review`).
- Populate `reviewer_role` and `tags` on the `check` from the agent's configured
  role and `labels`.

#### 6.3.3 Roles and wiring to review checks

Review agents SHOULD use explicit roles that map to review checks in
`review_gate.md`:

- `holistic_reviewer` → typically used for `ai_review` checks.
- `security_analyst` → typically used for `security_review` checks.
- `perf_analyst` → typically used for `perf_review` checks.

When the overseer processes a `review_request`:

- It consults `review.checks` configuration (see `review_gate.md`).
- For each AI-based check (e.g. `ai_review`, `security_review`), it:
  - Chooses a dspy-go role (e.g. `holistic_reviewer`, `security_analyst`).
  - Constructs a `ReviewInput` with the relevant files/diff and constraints.
  - Spawns or invokes a review agent with that signature and role.
  - Converts the resulting `ReviewOutput` into a check entry and structured
    artifact in the review record.

This keeps the overseer as the single orchestrator of reviews while allowing
specialized reviewer agents to provide focused analysis for security,
performance, or other concerns.

---

## 7. Lifecycle & Integration with Overseer / Mailbox

### 7.1 Spawn

A new dspy-go agent session is started by `agentctl` via a CLI or future
programmatic API, for example:

- `agentctl agent spawn <role> --workspace <id> --epic <epic-id> --task <task-id>`

Spawn operation MUST:

- Create a job entry representing the agent session.
- Allocate an `actor_id` and persist it.
- Optionally emit a mailbox message announcing the session (e.g.
  `system: agent:spawned`).

### 7.2 Run Loop

During its run loop, an agent MUST:

- On each iteration (or before each major tool call):
  - Poll `mail.inbox` for new messages relevant to its `actor_id`,
    `workspace_id`, and current `task_id` / `epic_id`.
  - Update its internal constraints and plan view from overseer messages.
- Use tools defined in §5 instead of invoking external commands directly.
- Respect `task_guard`/`file_guard` decisions; if a write is blocked or heavily
  warned, the agent SHOULD surface this as `status="blocked"` or
  `status="needs_review"`.

### 7.3 Completion

On completion (normal or error), the agent MUST:

- Update the job entry with terminal state, including:
  - Outcome status.
  - Summary.
  - Key metrics (iterations, tools used, duration).
- Optionally write a summary envelope to CAS and store a digest.
- Emit any final mails:
  - To overseer: `review_request` or `plan.review_needed:<epic-id>` if the plan
    seems inconsistent with reality.
  - To human actor: summary of what changed and what remains.

---

## 8. Storage, Telemetry, and Observability

### 8.1 Jobs and Execution History

- Each agent run is tracked as a job in the existing jobs store.
- Execution history from dspy-go (thoughts, actions, reflections) SHOULD be
  serialized into:
  - A compact summary for human display.
  - A full trace stored in CAS if large.

### 8.2 Telemetry

- A new telemetry store MAY record `tool_usage` events (command, args summary,
  ts, workspace_id, actor_id, kind=`read|write|test|task|mail`).
- Tools in §5 SHOULD write telemetry entries on each call.
- `context.recents` uses this store to reconstruct recent history.

### 8.3 Hooks

- `mail_router` continues to inject high-priority overseer/admin messages into
  context for any tool call triggered by an agent.
- `task_guard` enforces task-scoped editing.
- `file_guard` enforces advisory/strict reservations, emitting
  `plan.review_needed` when conflicts persist.
- `test_watch_feedback` surfaces failing tests after edits, which the agent can
  read via tools or via hook-injected context.

---

## 9. Agent Viewer Integration Points (Future)

This spec does not define a full viewer UI, but agentctl MUST expose enough
machine-readable information for a TUI/GUI to:

- List active and recent agents with:
  - `agent_id`, `actor_id`, `role`, `workspace_id`, `epic_id`, `task_id`.
  - Current status and last action.
- Show per-task views including:
  - Linked agents (by `task_id`), their status, and latest summaries.
  - Related mails (from overseer, admin, agents).
  - Graph insights from `todo/manage.graph_insights`.
- Stream or fetch execution traces for debugging.
- Present per-actor inboxes based on `mailbox_blackboard.md`.

Concrete CLI and JSON schemas for these viewer endpoints are left to a follow-up
`agent_viewer.md` spec, but MUST align with the data model defined here.

---

## 10. Open Questions / V1 Cut

Open questions to resolve during implementation:

- Exact mapping between dspy-go memory and `agentctl` memory primitives
  (auto-cache vs named memory vs agent-specific stores).
- How much of the execution trace to persist vs summarize.
- Whether agents may run multiple concurrent tasks, or one task/epic per
  process.

For a V1 implementation, the following subset is sufficient:

- One **Coding Agent** role with the signature in §6.1.
- Tool subset: `fs.list_dir`, `fs.read_file`, `code.search`, `edit.apply_patch`,
  `tests.run`, `todo.query`, `todo.graph_insights`, `mail.send`, `mail.inbox`.
- Jobs + basic telemetry recording.
- Manual spawn/kill via CLI, with logs in jobs storage.
- No viewer yet; use CLI and logs to inspect behavior.

---

## 11. Wire Contracts, Error Codes, and Envelopes

This section specializes the Core Profile v1 envelope rules (`protocol_v1.md`)
for dspy-go agents and their tools. It is **normative** for the agent/dspy
integration, but does not change the underlying Protocol v1 wire contract.

### 11.1 Agent Invocation Envelope

Agent invocations (coding, planning, review) are always carried inside Protocol
v1 envelopes with `command` in the `agent/*` or `jobs/*` namespaces.
Conceptually:

```jsonc
{
  "version": 1,
  "status": "ok|error|progress",
  "command": "agent/spawn" | "agent/send" | "jobs/submit",
  "data": {
    "kind": "coding" | "planning" | "review",
    "input": { /* Coding/Planning/ReviewInput as per §6 */ },
    "output": { /* optional ReviewOutput / status summaries on results */ }
  },
  "meta": {
    "ts": "...",              // Core v1 timestamp
    "workspace": "...",      // Workspace path or id
    "job_id": "01H...",      // Job representing the agent session (if any)
    "trace_id": "...",       // Correlates all envelopes for this run
    "source": "run|cache",   // As per Core v1
    "profiles": ["core/v1", "agent/v1"]
  },
  "error": { /* see §11.4 */ }
}
```

Implementations MAY wrap agent signatures in different specific commands
(`agent/spawn`, `jobs/submit`, etc.), but MUST preserve:

- `data.kind` and `data.input` matching the signatures in §6.
- A stable `meta.trace_id` shared across all envelopes for a given agent run.
- A `meta.workspace` and (when applicable) `meta.job_id` so trajectories and
  jobs can be joined.

### 11.2 Required Meta Fields for Tools and Telemetry

For dspy-go tool calls (the tools in §5) and their corresponding telemetry
events, the following metadata MUST be available, either via Protocol v1
`meta.*` fields or via the telemetry/event record that wraps the envelopes:

- `meta.actor_id` (string, required for agent-originated calls)
  - The mailbox identity of the agent, e.g. `actor:agent:dspy:<slug>`.
- `meta.workspace_id` (string, required)
  - Logical workspace identifier (may be derived from `meta.workspace`).
- `meta.task_id` (string, required for task-scoped calls)
  - The primary task the tool call is associated with.
- `meta.trace_id` (string, required)
  - Correlation id linking all envelopes for a given agent step/run.
- `meta.epic_id` (string, optional)
  - Epic/plan root, when available.

Additional optional fields such as `meta.run_id`, `meta.job_attempt`, and
`meta.cas_digest` follow the conventions in `dspy_trajectory_capture.md` §3.3.

### 11.3 Canonical Tool Error Codes and Shapes

Tools in §5 MUST use Core Profile v1 error envelopes (`status: "error"` with
`error.code` / `error.message` / `error.details`) and SHOULD use the following
additional codes for common agent scenarios. These codes extend the base error
catalog in `protocol_v1.md` §5.

| Code                  | Applies to                             | Meaning / Typical Cause                                   |
| --------------------- | -------------------------------------- | --------------------------------------------------------- |
| `E_GUARD_VIOLATION`   | `edit.*`, `fs.*`, `tests.run`          | Blocked by `task_guard` / `file_guard` (scope/lock issue) |
| `E_PERMISSION_DENIED` | `fs.*`, `edit.*`, `tests.run`          | OS-level or policy-level permission failure               |
| `E_FILE_NOT_FOUND`    | `fs.read_file`, `edit.apply_patch`     | Requested path does not exist                             |
| `E_PATCH_FAILED`      | `edit.apply_patch`, `edit.multi_patch` | Patch could not be applied cleanly (conflict/invalid)     |
| `E_TEST_FAILED`       | `tests.run`                            | Tests executed but one or more failed                     |
| `E_MAILBOX_NOT_FOUND` | `mail.*`                               | Target mailbox/recipient does not exist                   |

Error envelopes MUST conform to Protocol v1, with additional fields in
`error.details` scoped per tool. Examples (non-exhaustive):

- `fs.read_file`:
  - `error.details`: `{ "path": "...", "exists": false }`.
- `edit.apply_patch`:
  - `error.details`: `{ "path": "...", "reason": "conflict", "hunk": 3 }`.
- `tests.run`:
  - `error.details`: `{ "failed": ["TestFoo", "TestBar"], "total": 27 }`.
- `mail.send`:
  - `error.details`: `{ "recipient": "...", "reason": "not_found" }`.

Guard-specific errors (`E_GUARD_VIOLATION`, `E_PERMISSION_DENIED`) SHOULD also
include which guard fired:

```jsonc
"error": {
  "code": "E_GUARD_VIOLATION",
  "message": "edit blocked by task_guard",
  "details": {
    "guard": "task_guard",
    "scope_paths": ["agentctl/internal/..."],
    "reason": "outside_task_scope"
  }
}
```

### 11.4 Jobs and Error Propagation to dspy-go

Agent runs are typically represented as jobs (see `jobs/*` commands in
`protocol_v1.md` §3). For dspy-go agents:

- The **job creation envelope** (`jobs/submit`) MUST include:
  - `data.kind` = `"agent_session"` (or similar profile-specific marker).
  - `data.agent_role` = `"coder" | "planner" | "reviewer"`.
  - `data.input` = Coding/Planning/ReviewInput from §6.
  - `meta.trace_id` and `meta.workspace`.
- The **terminal job envelope** (`jobs/tail` final event or `jobs/info`):
  - Uses `status: "ok"` when the agent completes without unhandled errors.
  - Uses `status: "error"` when the session aborts due to an unhandled tool or
    runtime error.
  - MUST surface the last failing tool envelope (or a summary) in
    `data.last_error` or `error.details.last_error_digest`.

Error propagation semantics back to the dspy-go runtime:

- **Retryable errors** (MAY be retried by the agent/tool wrapper):
  - `ERUNTIME`, `ETIMEOUT`, `ERATELIMIT`, transient provider/network errors.
- **Terminal errors** (SHOULD NOT be retried automatically):
  - `E_GUARD_VIOLATION`, `E_PERMISSION_DENIED`, `E_FILE_NOT_FOUND`,
    `E_PATCH_FAILED`, `EARG`, `EINVALID_PLAN` (when applicable).
- Tool wrappers SHOULD:
  - Map retryable errors to dspy exceptions that can trigger built-in retry
    logic (e.g. with backoff).
  - Map terminal errors to structured results (`status = "blocked"` or
    `status = "error"` in Coding/Review outputs) rather than untyped
    panics/exceptions.

When mapping to HTTP/gRPC-style status codes (for external APIs or logs), the
following conventions are recommended:

- `E_GUARD_VIOLATION` → `409 Conflict`.
- `E_PERMISSION_DENIED` → `403 Forbidden`.
- `E_FILE_NOT_FOUND` → `404 Not Found`.
- `E_PATCH_FAILED` → `409 Conflict` or `422 Unprocessable Entity`.
- `E_TEST_FAILED` → `422 Unprocessable Entity`.

### 11.5 Example Envelopes (edit.apply_patch)

**Successful patch application:**

```jsonc
{
  "version": 1,
  "status": "ok",
  "command": "edit/apply_patch",
  "data": {
    "path": "agentctl/internal/foo.go",
    "summary": {
      "loc_added": 12,
      "loc_deleted": 3
    }
  },
  "meta": {
    "ts": "2025-11-15T12:34:56Z",
    "workspace": "/Users/example/repos/agentctl",
    "workspace_id": "ws-123", // conceptual field for telemetry/join
    "job_id": "01HF...",
    "trace_id": "4f8a...",
    "actor_id": "actor:agent:dspy:coder-main"
  },
  "error": {
    "code": null,
    "message": null
  }
}
```

**Guard-blocked patch (task_guard):**

```jsonc
{
  "version": 1,
  "status": "error",
  "command": "edit/apply_patch",
  "data": {
    "hint": "edit blocked by task_guard; path is outside task scope"
  },
  "meta": {
    "ts": "2025-11-15T12:35:02Z",
    "workspace": "/Users/example/repos/agentctl",
    "workspace_id": "ws-123",
    "job_id": "01HF...",
    "trace_id": "4f8a...",
    "actor_id": "actor:agent:dspy:coder-main"
  },
  "error": {
    "code": "E_GUARD_VIOLATION",
    "message": "edit blocked by task_guard",
    "details": {
      "guard": "task_guard",
      "scope_paths": ["agentctl/internal/storage"],
      "path": "agentctl/testdata/manual_hack.go",
      "reason": "outside_task_scope"
    }
  }
}
```

These examples are illustrative only; implementations MUST still conform to the
canonical envelope rules in `protocol_v1.md`.
