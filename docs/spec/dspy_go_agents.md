# dspy-go Agent Runtime for agentctl

**Status:** Draft\
**Scope:** Integrate dspy-go agents as first-class workers in agentctl
workspaces.\
**Related specs:** `overseer_profile.md`, `overseer_planning.md`,
`mailbox_blackboard.md`, `task_graph_insights.md`, `test_watch_feedback.md`,
`unified_agents.md`

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

This data MAY live in a separate teams store or as configuration in the
workspace; exact storage is left to implementation.

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

### 5.2 Editing & Refactoring

All writing tools MUST go through `agentctl` skills so `task_guard` and
`file_guard` can enforce reservations and scope rules.

- **`edit.apply_patch`**
  - Purpose: Apply a minimal patch to one file.
  - Inputs:
    - `workspace_id`
    - `path`
    - `patch` (structured diff representation, not raw shell patch)
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
