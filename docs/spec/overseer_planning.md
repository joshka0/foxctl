# Overseer Planning & Coordination Spec

**Status:** Draft\
**Scope:** Agentctl overseer behavior for task planning, coordination, and
runtime plan updates.\
**Related specs:** `overseer_profile.md`, `mailbox_blackboard.md`,
`task_graph_insights.md`,
[bd_mail_agent_interface.md](cci:7://file://~/repos/personal/claude-harness/agentctl/docs/spec/bd_mail_agent_interface.md:0:0-0:0),
`unified_agents.md`

---

## 1. Overview

This spec defines how the **overseer** acts as both:

- **Planner** – turning user goals into a **task graph** optimized for
  multi-agent execution.
- **Coordinator** – steering agents, reacting to runtime signals, and evolving
  the plan.

The design leverages:

- `tasks` store + `tasksgraph` insights (PageRank, critical path, cycles).
- `mailbox/blackboard` for admin/overseer messages, reservations, and hooks.
- Existing hooks: `mail_router`, `task_guard`, `file_guard`.

**Principle:** _Overseer is the single authority for non-trivial plan changes._\
Agents and hooks can suggest or perform **local refinements**, but epic-level
structure is overseer-owned.

---

## 2. Roles & Actors

### 2.1 Overseer (planner + coordinator)

See [`overseer_profile.md`](overseer_profile.md) for the canonical overseer
profile including:

- Actor ID (`actor:system:overseer`)
- Full list of allowed operations
- Knowledge set
- Responsibilities and constraints

In summary, the overseer:

- Creates/updates **epic plans** via `todo/manage.plan`.
- Approves or rejects suggested plans.
- Assigns work to agents (which tasks, in what order).
- Re-plans when runtime signals indicate drift.

### 2.2 Work agents

- Coder, reviewer, fixer, etc.
- Actors like:
  - `actor:agent:coder1`
  - `actor:agent:reviewer`
- Responsibilities:
  - Execute tasks under the plan.
  - Perform **local** task splits/adjustments when appropriate.
  - Emit signals (mail) when they believe the plan is outdated.

### 2.3 Hooks & routers

- **`mail_router`**
  - Surfaces important overseer messages into agent prompts / IDE context.
- **`task_guard`**
  - Enforces that write operations stay within the active task’s intended scope
    and plan.
- **`file_guard`**
  - Manages file reservations and detects contention hotspots.

Hooks do **not** own the plan. They:

- Deliver overseer messages.
- Emit **signals** when behavior diverges from the plan (scope mismatch,
  contention, etc.).

---

## 3. `todo/manage.plan` Operation

`todo/manage.plan` is the entrypoint for planning and structured replanning.

### 3.1 Input

```jsonc
{
	"operation": "plan",
	"workspace_id": "workspace-123",
	"plan": {
		"goal": "Ship multi-agent E2E tests",
		"description": "E2E for task graph, mailbox, file_guard, overseer scoring",
		"scope_paths": [
			"agentctl/test/e2e",
			"agentctl/internal/analysis"
		],
		"attach_to_task_id": "optional-epic-id-or-empty",
		"mode": "draft|apply",
		"max_tasks": 20,
		"max_depth": 3,
		"strategy": "auto|epic|flat"
	}
}
```

````
**Fields:**

- `goal` – one-sentence description of desired outcome.
- `description` – longer context for human/LLM planning.
- `scope_paths` – directories/files likely to be touched; inform:
  - Task scoping.
  - Future `task_guard` and `file_guard` behavior.
- `attach_to_task_id`:
  - Empty → create a **new epic** and its subtree.
  - Non-empty → refine/replan **within that epic’s subtree**.
- `mode`:
  - `draft` – do **not** mutate tasks DB; return a proposed graph only.
  - `apply` – create/update tasks in the store, returning applied diff.
- `strategy`:
  - `flat` – epic + leaf tasks only.
  - `epic` – epics → sub-epics → leaves (multi-level).
  - `auto` – heuristic based on `max_depth`, `scope_paths`, and repo size.

### 3.2 Output

```jsonc
{
	"status": "ok",
	"data": {
		"root_task_id": "01KB3K4M...",
		"applied": true,
		"tasks": [
			{
				"id": "01KB3K4M...",
				"title": "Epic: Ship multi-agent E2E tests",
				"status": "pending",
				"depends_on": [],
				"scope_paths": ["agentctl/test/e2e"]
			},
			{
				"id": "01KB3K5N...",
				"title": "Write multiagent_workflow_test.go",
				"depends_on": ["01KB3K4M..."],
				"scope_paths": ["agentctl/test/e2e"],
				"status": "pending"
			}
		],
		"graph": {
			"nodes": [
				// node metrics summary per task (PageRank, critical path score, etc.)
			],
			"edges": [
				{ "from": "01KB3K5N...", "to": "01KB3K4M..." }
			],
			"cycles": []
		},
		"diff": {
			"added_task_ids": ["01KB3K4M...", "01KB3K5N..."],
			"updated_task_ids": [],
			"removed_task_ids": []
		}
	}
}
```

- On `mode=draft`, `applied=false`; `diff` is hypothetical only.
- On `mode=apply`, `tasks` and `diff` reflect actual DB changes.

### 3.3 Ownership rules

- **Overseer** is the _default caller_ of `plan` for:
  - Initial plan creation for a feature.
  - Epic-level replanning (changing critical path, merging/splitting epics).
- **Agents** may be allowed limited `attach_to_task_id` **local splits**
  (implementation detail):
  - Must not change other epics or overall critical path without overseer’s
    approval.
  - Any local plan change should still be visible as a `plan.updated` event.

---

## 4. Plan Events via Mailbox

Overseer publishes **plan events** to the mailbox so agents and hooks can react.

### 4.1 Event vocabulary

Using
[BoardMessage](cci:2://file://~/repos/personal/claude-harness/agentctl/internal/domain/agent/board_message.go:25:0-39:1)
`Subject` conventions:

- `plan.created:<epic-id>`
- `plan.updated:<epic-id>`
- `plan.review_needed:<epic-id>`
- (Optional) `plan.local_split:<task-id>`

**Typical message fields:**

```go
agent.BoardMessage{
  WorkspaceID: ws,
  TaskID:      epicID,                 // epic or relevant task
  Sender:      "actor:system:overseer",
  Recipient:   "actor:agent:*",        // broadcast or specific agents
  Kind:        agent.BoardMessageKindInfo | agent.BoardMessageKindAlert,
  Priority:    2 or 3,
  Subject:     "plan.updated:01KB3K4M...",
  Body:        "...human-readable diff / summary...",
}
```

### 4.2 When overseer emits plan events

- After `todo/manage.plan` **apply**:
  - If this is a **new epic**: `plan.created:<epic-id>` (usually broadcast).
  - On **substantial structural changes** (e.g. many tasks added/removed,
    critical path change): `plan.updated:<epic-id>`.
- When overseer (or hooks) detect plan drift:
  - `plan.review_needed:<epic-id>` to self (overseer) or to log/agents.

**“Substantial”** is heuristic, e.g.:

- Added/removed tasks above a threshold.
- Changes to critical path.
- Large changes in total estimated workload or scope coverage.

---

## 5. Overseer Workflow: End-to-end

### 5.1 Feature request → plan

1. **User** requests a feature via Claude / Factory.
2. **Overseer** (`actor:system:overseer`):
   - Creates or selects an epic in `tasks`.
   - Calls `todo/manage.plan` (`mode=draft`) with:
     - `goal`, `description`, `scope_paths`, `attach_to_task_id` (epic).
   - Reviews the proposed graph with the user if needed.
   - Calls `todo/manage.plan` (`mode=apply`) once satisfied.
3. `plan.apply`:
   - Writes tasks to the store.
   - Overseer sends `plan.created:<epic-id>` to:
     - `actor:agent:*` (broadcast).
     - Optionally `actor:agent:reviewer` with a richer summary.

### 5.2 Plan → implementation

4. Overseer chooses leaf tasks to start:
   - Marks them `active` in `tasks`.
   - Optionally sends per-agent `instruction` messages:
     - “coder1: start with Task X”, “reviewer: review Task Y second”.
5. Work agents:
   - Read mailbox for overseer instructions.
   - Use tools guarded by `task_guard` + `file_guard`:
     - Writes limited to task `scope_paths`.
     - Reservations aligned with tasks.

### 5.3 Runtime signals → replanning

6. **Signals that may warrant plan review:**
   - Task-level:
     - Tasks stuck in `in_progress`.
     - Tasks repeatedly failing or getting reverted.
   - Hook-level:
     - `task_guard` sees repeated **scope mismatch**.
     - `file_guard` sees recurring **reservation conflicts** on same files.
   - Agent-level:
     - Agents send mail to overseer:
       - `Subject: plan.review_needed:<epic-id>` with free-form body.

7. Overseer periodically or on-demand:
   - Runs `task_graph_insights` on the epic subtree.
   - Runs overseer
     [Recommend](cci:1://file://~/repos/personal/claude-harness/agentctl/internal/analysis/overseer/scorer.go:64:0-198:1)
     to evaluate scores.
   - Decides whether to:
     - Split tasks (finer-grained), or
     - Merge tasks, or
     - Reorder priorities/critical path.

8. If overseer decides to change the plan:
   - Calls `todo/manage.plan` with `attach_to_task_id=<epic-id>` (or a
     sub-epic).
   - Applies the new structure.
   - Sends `plan.updated:<epic-id>` (Kind `alert` or `info` depending on
     magnitude).
   - Optionally sends direct `instruction` messages to affected agents:
     - “Stop work on old Task A; continue under new Task A1/A2.”

---

## 6. Hooks & Routers Behavior

### 6.1 `mail_router`

- On each user/agent prompt (or periodic tick):

  1. Calls `mailbox/manage.inbox` for the active `actorID`.
  2. Filters for overseer messages where:
     - `Subject` starts with `plan.` **or**
     - `Kind` in `{info, alert}` and `Sender == actor:system:overseer`.

- Injects a brief summary into the agent’s context, e.g.:

  > Plan updated for epic 01KB3K4M: 3 new subtasks added, critical path now
  > A→B→C.

- May mark messages as read/acked after surfacing.

### 6.2 `task_guard`

- On **PreToolUse** (writes):

  1. Resolves the current **active task** and epic.
  2. Reads plan metadata:
     - `scope_paths`, dependencies, epic root.
  3. Validates requested file paths:
     - If outside scope, either:
       - Block with actionable error (`EPOLICY`-style), or
       - Warn and require explicit override (implementation choice).

- If it detects frequent out-of-scope attempts:
  - Emits a `plan.review_needed:<epic-id>` message to overseer.

### 6.3 `file_guard`

- On reservations:

  1. Uses task `scope_paths` to suggest reservation sets.
  2. Detects contention patterns:
     - Same file reserved by many tasks repeatedly or concurrent conflicts.

- On repeated contention:
  - Sends `plan.review_needed:<epic-id>` to overseer with details:
    - “Tasks A,B,C all want src/foundation/core.go concurrently.”

---

## 7. Local vs Global Planning

### 7.1 Global (epic-level) plan changes

- **Allowed actors:** overseer.
- **Mechanism:** `todo/manage.plan` with
  `attach_to_task_id=<epic- or sub-epic-id>`, `mode=apply`.
- **Always** produce:
  - `plan.updated:<epic-id>` mailbox events.

### 7.2 Local (per-task) refinements

- **Allowed actors:** work agents, subject to policy.
- Examples:
  - Splitting a large task into 2–3 children under the same epic.
- Mechanism:
  - Either a simpler “split task” op, or a constrained `plan` call:
    - `attach_to_task_id=<current-task-id>`, low `max_tasks/max_depth`.
- Should still:
  - Write back into `tasks` store.
  - Emit at least:
    - `plan.updated:<epic-id>` (info-level),
    - Or a `review_request` to overseer for approval.

---

## 8. Open Questions / Implementation Notes

- **Policy:** exactly which agents are allowed to run `plan` in `apply` mode?
  - Start conservative: **only overseer** can call `apply`.
- **Diff summarization:** whether `plan.updated` bodies should include a
  structured diff (JSON) or just human text.
- **Plan IDs:** optional explicit `plan_id` associated with a set of tasks to
  distinguish multiple plan versions for the same epic over time.

---

## 9. Factory / Claude integration & knobs

This section describes the configuration required for Factory / Claude
environments to make use of overseer planning, mailbox/blackboard, and task
graph insights.

### 9.1 Skills & storage

- **Mailbox / blackboard**
  - `mailbox/manage.*` skill is installed and discoverable.
  - `internal/storage/blackboard` is wired to the workspace storage root.
  - `bd_mail_*` specs are included in the overseer’s knowledge set.

- **Task graph insights**
  - `todo/manage.graph_insights` operation is available on the `todo/manage`
    skill.
  - `internal/analysis/tasksgraph` is built into the `agentctl` binary.
  - `task_graph_insights.md` is included in the overseer / Factory spec pack.

- **Overseer scoring**
  - [[overseer.Scorer](cci:2://file://~/repos/personal/claude-harness/agentctl/internal/analysis/overseer/scorer.go:51:0-54:1)](cci:2://file://~/repos/personal/claude-harness/agentctl/internal/analysis/overseer/scorer.go:51:0-54:1)
    is built and reachable from `todo/manage.recommend` (or an equivalent
    operation).
  - [[multiagent_workflow_test.go](cci:7://file://~/repos/personal/claude-harness/agentctl/test/e2e/multiagent_workflow_test.go:0:0-0:0)](cci:7://file://~/repos/personal/claude-harness/agentctl/test/e2e/multiagent_workflow_test.go:0:0-0:0)
    E2E tests are part of the default test suite for this workspace.

### 9.2 Planning operation

- **`todo/manage.plan`**
  - `plan` is added to the `todo/manage` manifest / operations list.
  - Only `actor:system:overseer` (and optionally CI/test agents) are allowed to
    call `mode="apply"`; other actors are restricted to `mode="draft"` or
    disallowed.
  - `agentctl todo plan` CLI helper is exposed for humans / overseer to drive
    planning.

### 9.3 Hooks & routers

- **`mail_router` hook**
  - Enabled for the workspace.
  - Configured to:
    - Pull messages via `mailbox/manage.inbox`.
    - Treat `Sender == actor:system:overseer` and `Subject` starting with
      `plan.` as high-priority context.
  - Optionally configured to summarize multiple `plan.*` messages into a single
    prompt note.

- **`task_guard` hook**
  - Enabled for write-capable tools.
  - Configured to:
    - Resolve the current active task/epic from `todo/manage`.
    - Respect `scope_paths` emitted by `todo/manage.plan`.
    - Emit `plan.review_needed:<epic-id>` messages to overseer on repeated scope
      violations.

- **`file_guard` hook**
  - Enabled for file-reservation-capable tools.
  - Configured to:
    - Use `scope_paths` as the default reservation set.
    - Emit `plan.review_needed:<epic-id>` to overseer on recurring contention.

### 9.4 Agent / Factory profiles

- **Overseer profile**
  - Marked as:
    - Allowed to call `todo/manage.plan` (`draft` and `apply`).
    - Allowed to call `todo/manage.graph_insights`, `todo/manage.recommend`, and
      `mailbox/manage.*`.
  - Given knowledge of:
    - [[overseer_planning.md](cci:7://file://~/repos/personal/claude-harness/agentctl/docs/spec/overseer_planning.md:0:0-0:0)](cci:7://file://~/repos/personal/claude-harness/agentctl/docs/spec/overseer_planning.md:0:0-0:0)
    - [[bd_mail_agent_interface.md](cci:7://file://~/repos/personal/claude-harness/agentctl/docs/spec/bd_mail_agent_interface.md:0:0-0:0)](cci:7://file://~/repos/personal/claude-harness/agentctl/docs/spec/bd_mail_agent_interface.md:0:0-0:0)
    - `mailbox_blackboard.md`
    - `task_graph_insights.md`
    - `unified_agents.md`

- **Work agent profiles (coder, reviewer, fixer)**
  - Allowed to:
    - Read from `mailbox/manage.inbox`.
    - Use `mailbox/manage.reserve` for `file_guard`.
  - Not allowed to:
    - Call `todo/manage.plan` with `mode="apply"` at epic scope.
    - Change the global plan without overseer involvement.

```yaml
workspace:
    name: agentctl-core
    root: /workspace/agentctl

    skills:
        - name: todo/manage
          operations:
              - add
              - complete
              - list
              - graph_insights
              - recommend
              - plan # <- new

        - name: mailbox/manage
          operations:
              - inbox
              - send
              - reserve
              - release
              - list_reservations

    storage:
        tasks_db: ~/.agentctl/storage/tasks.db
        blackboard_db: ~/.agentctl/storage/blackboard.db

    hooks:
        - name: mail_router
          enabled: true
          config:
              inbox_skill: mailbox/manage
              treat_overseer_plan_subjects_as_high_priority: true
              plan_subject_prefix: "plan."
              overseer_actor_id: "actor:system:overseer"

        - name: task_guard
          enabled: true
          config:
              todo_skill: todo/manage
              enforce_scope_paths: true
              emit_review_needed_on_repeated_scope_violations: true

        - name: file_guard
          enabled: true
          config:
              mailbox_skill: mailbox/manage
              use_scope_paths_for_default_reservations: true
              emit_review_needed_on_recurring_contention: true

agents:
    overseer:
        actor_id: "actor:system:overseer"
        allowed_operations:
            - "todo/manage.plan"
            - "todo/manage.graph_insights"
            - "todo/manage.recommend"
            - "mailbox/manage.inbox"
            - "mailbox/manage.send"
            - "mailbox/manage.reserve"
            - "mailbox/manage.release"
        knowledge:
            - "docs/spec/overseer_planning.md"
            - "docs/spec/bd_mail_agent_interface.md"
            - "docs/spec/mailbox_blackboard.md"
            - "docs/spec/task_graph_insights.md"
            - "docs/spec/unified_agents.md"

    coder:
        actor_id: "actor:agent:coder1"
        allowed_operations:
            - "mailbox/manage.inbox"
            - "mailbox/manage.send"
            - "mailbox/manage.reserve"
            - "mailbox/manage.release"
            - "todo/manage.add"
            - "todo/manage.complete"
            # no todo/manage.plan.apply

    reviewer:
        actor_id: "actor:agent:reviewer"
        allowed_operations:
            - "mailbox/manage.inbox"
            - "mailbox/manage.send"
            - "todo/manage.graph_insights" # read-only insights if desired

tests:
    - name: multi-agent-e2e
      command: "go test -v ./test/e2e/..."
      required: true
```
````
