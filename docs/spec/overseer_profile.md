# Overseer Profile

**Status:** Draft\
**Scope:** Canonical profile for the overseer in agentctl-based workspaces.\
**Related specs:** [overseer_planning.md](./overseer_planning.md),
[../archive/specs/bd_mail_agent_interface.md](../archive/specs/bd_mail_agent_interface.md),
[mailbox_blackboard.md](./mailbox_blackboard.md),
[../archive/specs/task_graph_insights.md](../archive/specs/task_graph_insights.md),
[../archive/specs/unified_agents.md](../archive/specs/unified_agents.md),
[semantic_file_index.md](./semantic_file_index.md)

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

- [overseer_planning.md](./overseer_planning.md)
- [../archive/specs/bd_mail_agent_interface.md](../archive/specs/bd_mail_agent_interface.md)
- [mailbox_blackboard.md](./mailbox_blackboard.md)
- [../archive/specs/task_graph_insights.md](../archive/specs/task_graph_insights.md)
- [../archive/specs/unified_agents.md](../archive/specs/unified_agents.md)

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
    - Hooks enabled and wired per [overseer_planning.md](./overseer_planning.md)
      §9.

---

## 8. Wire Contract & Error Codes

This section describes the canonical message envelope for **plan events**
emitted by the overseer over the mailbox/blackboard and via jobs. It builds on
Protocol v1 envelopes (`protocol_v1.md`) but focuses on the `data` and `meta.*`
fields specific to planning.

### 8.1 Plan Event Envelope (Conceptual)

Plan events are typically carried as mailbox messages (`mailbox/manage.send`) or
job progress/result envelopes. Conceptually, a plan event looks like:

```jsonc
{
  "version": 1,
  "status": "ok|error",
  "command": "mailbox/manage.send", // or jobs/submit, jobs/tail
  "data": {
    "MessageType": "plan.created" | "plan.updated" | "plan.review_needed",
    "Subject": "plan.created:<epic-id>",
    "Sender": "actor:system:overseer",
    "Recipients": ["actor:agent:planner:main"],
    "Timestamp": "2025-11-15T12:34:56Z",
    "Body": {
      /* Plan payload; see overseer_planning.md for exact shape */
    }
  },
  "meta": {
    "epic_id": "epic-123",
    "plan_id": "plan-01H...",
    "version": 3,
    "source": "overseer",            // e.g. overseer|agent|human
    "reason": "initial_create|replan|guard_violation",
    "trace_id": "...",              // Protocol v1 trace id
    "workspace_id": "ws-123"        // conceptual workspace identifier
  },
  "error": {
    /* present only for status:"error", see §8.3 */
  }
}
```

### 8.2 Meta Fields for Plan Events

The following `meta.*` keys are defined for overseer plan events:

| Field          | Type   | Required | Meaning                                                                                  |
| -------------- | ------ | -------- | ---------------------------------------------------------------------------------------- |
| `epic_id`      | string | yes      | Epic identifier this plan event relates to.                                              |
| `plan_id`      | string | yes      | Logical plan identifier (e.g. ULID per epic).                                            |
| `version`      | int    | yes      | Monotonically increasing plan version per `(workspace_id, epic_id)`.                     |
| `source`       | string | yes      | Originator: `"overseer"                                                                  |
| `reason`       | string | yes      | Short machine-readable reason, e.g. `"initial_create"`, `"replan"`, `"guard_violation"`. |
| `trace_id`     | string | optional | Protocol v1 trace id linking envelopes.                                                  |
| `workspace_id` | string | optional | Logical workspace identifier.                                                            |
| `reviewer`     | string | optional | Actor id of human/agent reviewer when applicable.                                        |
| `priority`     | string | optional | Advisory priority (`"low"                                                                |

`version` MUST be incremented on every `plan.updated` event for a given
`(workspace_id, epic_id)` pair. `plan.created` MUST start at `version = 1`.

### 8.3 Error Codes and Status Mapping

When plan operations fail (e.g. invalid apply attempt, conflicts), the overseer
and underlying skills MUST use Protocol v1 error envelopes and SHOULD use the
following codes in addition to the global catalog:

| Code             | HTTP/RPC Status  | Meaning                                                                                    |
| ---------------- | ---------------- | ------------------------------------------------------------------------------------------ |
| `E_UNAUTHORIZED` | 401 Unauthorized | Non-overseer or unauthorized actor attempted a plan apply or privileged action.            |
| `E_INVALID_PLAN` | 400 Bad Request  | Plan payload is structurally invalid or violates invariants (cycles, missing epics, etc.). |
| `E_CONFLICT`     | 409 Conflict     | Concurrent plan edits or stale `version` detected.                                         |
| `E_NOT_FOUND`    | 404 Not Found    | Referenced epic/plan/task does not exist.                                                  |

These codes appear in `error.code` with a human-readable `error.message` and
machine hints in `error.details` (e.g. offending `epic_id`, expected vs actual
`version`).

### 8.4 Example Plan Event Envelopes

**plan.created**

```jsonc
{
	"version": 1,
	"status": "ok",
	"command": "mailbox/manage.send",
	"data": {
		"MessageType": "plan.created",
		"Subject": "plan.created:epic-123",
		"Sender": "actor:system:overseer",
		"Recipients": ["team:backend"],
		"Timestamp": "2025-11-15T12:00:00Z",
		"Body": {
			"epic_id": "epic-123",
			"tasks": [/* see overseer_planning.md */]
		}
	},
	"meta": {
		"epic_id": "epic-123",
		"plan_id": "plan-01HF...",
		"version": 1,
		"source": "overseer",
		"reason": "initial_create"
	},
	"error": { "code": null, "message": null }
}
```

**plan.updated**

```jsonc
{
	"version": 1,
	"status": "ok",
	"command": "mailbox/manage.send",
	"data": {
		"MessageType": "plan.updated",
		"Subject": "plan.updated:epic-123",
		"Sender": "actor:system:overseer",
		"Recipients": ["team:backend"],
		"Timestamp": "2025-11-15T13:00:00Z",
		"Body": {
			"epic_id": "epic-123",
			"changes": [/* diff-style summary */]
		}
	},
	"meta": {
		"epic_id": "epic-123",
		"plan_id": "plan-01HF...",
		"version": 2,
		"source": "overseer",
		"reason": "replan"
	},
	"error": { "code": null, "message": null }
}
```

**plan.review_needed**

```jsonc
{
	"version": 1,
	"status": "ok",
	"command": "mailbox/manage.send",
	"data": {
		"MessageType": "plan.review_needed",
		"Subject": "plan.review_needed:epic-123",
		"Sender": "actor:system:overseer",
		"Recipients": ["actor:human:jane"],
		"Timestamp": "2025-11-15T13:30:00Z",
		"Body": {
			"epic_id": "epic-123",
			"reason": "file_guard_conflicts"
		}
	},
	"meta": {
		"epic_id": "epic-123",
		"plan_id": "plan-01HF...",
		"version": 2,
		"source": "overseer",
		"reason": "guard_violation",
		"priority": "high"
	},
	"error": { "code": null, "message": null }
}
```

**Example error response (version conflict)**

```jsonc
{
	"version": 1,
	"status": "error",
	"command": "todo/manage.plan",
	"data": {
		"hint": "plan version conflict; reload plan and retry with latest version"
	},
	"meta": {
		"epic_id": "epic-123",
		"plan_id": "plan-01HF...",
		"version": 2,
		"source": "overseer",
		"reason": "apply",
		"trace_id": "..."
	},
	"error": {
		"code": "E_CONFLICT",
		"message": "plan version 3 expected, but request used version 2",
		"details": {
			"expected_version": 3,
			"actual_version": 2
		}
	}
}
```

These examples are illustrative and must still conform to the canonical envelope
invariants in `protocol_v1.md`.
