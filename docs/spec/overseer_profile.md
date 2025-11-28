# Overseer Profile

**Status:** Draft\
**Scope:** Canonical profile for the overseer in agentctl-based workspaces.\
**Related specs:**
[overseer_planning.md](cci:7://file:///Users/jkatigbak/repos/personal/claude-harness/agentctl/docs/spec/overseer_planning.md:0:0-0:0),
[bd_mail_agent_interface.md](cci:7://file:///Users/jkatigbak/repos/personal/claude-harness/agentctl/docs/spec/bd_mail_agent_interface.md:0:0-0:0),
`mailbox_blackboard.md`, `task_graph_insights.md`, `unified_agents.md`,
`semantic_file_index.md`

---

## 1. Overview

The overseer is the top-level coordinating agent for a workspace. It:

- Owns **plans** (epic task graphs) and their evolution.
- Coordinates **work agents** (coder, reviewer, fixer, etc.).
- Uses mailbox / blackboard and hooks to steer and adjust execution.

Overseer is the **single authority** for non-trivial plan changes and
cross-agent coordination.

---

## 2. Actor identity

- **Actor ID:** `actor:system:overseer`

All plan and coordination messages originating from the overseer MUST use this
actor ID as `Sender`.

---

## 3. Capabilities

The overseer MUST be allowed to call the following operations:

- **Tasks / planning**
  - `todo/manage.plan` (both `mode="draft"` and `mode="apply"`)
  - `todo/manage.graph_insights`
  - `todo/manage.recommend`
  - `todo/manage.add`
  - `todo/manage.complete`
  - `todo/manage.list` (or equivalent list operation)

- **Mailbox / blackboard**
  - `mailbox/manage.inbox`
  - `mailbox/manage.send`
  - `mailbox/manage.reserve`
  - `mailbox/manage.release`
  - `mailbox/manage.list_reservations`

Overseer MAY call additional read-only operations (e.g. status or stats
endpoints) as needed.

---

## 4. Knowledge

The overseer SHOULD have the following specs in its knowledge set for a
workspace:

- [docs/spec/overseer_planning.md](cci:7://file:///Users/jkatigbak/repos/personal/claude-harness/agentctl/docs/spec/overseer_planning.md:0:0-0:0)
- [docs/spec/bd_mail_agent_interface.md](cci:7://file:///Users/jkatigbak/repos/personal/claude-harness/agentctl/docs/spec/bd_mail_agent_interface.md:0:0-0:0)
- `docs/spec/mailbox_blackboard.md`
- `docs/spec/task_graph_insights.md`
- `docs/spec/unified_agents.md`

Installations MAY add workspace-specific specs, but the above form the core
behavior contract.

---

## 5. Responsibilities

### 5.1 Planning

- Translate user goals into epic task graphs via `todo/manage.plan`.
- Approve or refine proposed plans before applying.
- Maintain the integrity of the plan over time:
  - Critical path structure.
  - Task scoping via `scope_paths`.
  - Clear dependencies and epics.

### 5.2 Coordination

- Assign tasks to work agents (coder, reviewer, fixer, etc.).
- Communicate via mailbox:
  - `instruction` messages (who should do what next).
  - `info` / `alert` messages for plan events (`plan.created`, `plan.updated`,
    `plan.review_needed`).
- React to signals from hooks and agents:
  - Scope violations (`task_guard`).
  - File contention (`file_guard`).
  - Agent-generated `plan.review_needed:<epic-id>` requests.

### 5.3 Replanning

- Periodically or on demand:
  - Run `todo/manage.graph_insights` on relevant epics.
  - Run `todo/manage.recommend` to evaluate scores and bottlenecks.
- Decide when to:
  - Split large tasks.
  - Merge small tasks.
  - Reorder priorities / adjust the critical path.
- Apply changes via `todo/manage.plan` (`mode="apply"`) and emit
  `plan.updated:<epic-id>` messages.

---

## 6. Constraints & policies

- Only the overseer (and optional CI/test agents) MAY call `todo/manage.plan`
  with `mode="apply"` at epic scope.
- Work agents:
  - MUST NOT change the global plan without overseer involvement.
  - MAY request plan review via mailbox (e.g. `plan.review_needed:<epic-id>`).
- Plan events (`plan.created`, `plan.updated`, `plan.review_needed`, etc.)
  SHOULD always be sent from `actor:system:overseer`.

---

## 7. Integration points

- **Hooks**
  - `mail_router` MUST treat overseer plan messages
    (`Sender == actor:system:overseer`, `Subject` starts with `plan.`) as
    high-priority context.
  - `task_guard` and `file_guard` SHOULD emit `plan.review_needed:<epic-id>`
    messages to the overseer on repeated violations / contention.

- **Post-review indexing**
  - Overseer SHOULD coordinate post-review indexers such as the semantic file
    index; see `semantic_file_index.md` §8 for details.

- **Factory / Claude**
  - Overseer profile SHOULD be reflected in Factory / Claude agent
    configuration:
    - Actor ID, allowed operations, and knowledge set configured as above.
    - Hooks enabled and wired per
      [overseer_planning.md](cci:7://file:///Users/jkatigbak/repos/personal/claude-harness/agentctl/docs/spec/overseer_planning.md:0:0-0:0)
      §9.
