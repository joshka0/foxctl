---
title: Overseer and orchestration
description: Coordinate subagents through the overseer hierarchy, spawn protocol, depth constraints, and result aggregation.
---

foxctl's multi-agent model is overseer-first. The overseer owns plan changes and cross-agent coordination. Child agents request work through a documented spawn and mail protocol rather than creating sessions directly. This page describes the hierarchy model, depth constraints, spawn flow, rejection paths, and the overseer's responsibilities.

## Overseer identity

The overseer uses the fixed actor ID `actor:system:overseer`. All plan and coordination messages originating from the overseer must use this actor ID as `Sender`.

## Hierarchy model

### Tree fields

Every `AgentConfig` and `AgentSession` includes hierarchy fields that define the agent's position in the tree:

| Field | Type | Description |
|-------|------|-------------|
| `RootActorID` | `string` | The tree root, usually `actor:system:overseer` |
| `ParentActorID` | `string` | Immediate parent; empty for the overseer |
| `Depth` | `int` | 0 for overseer, increments per level |
| `MaxDepth` | `int` | Global cap for the entire tree, set by the overseer |
| `LocalMaxDepth` | `int` | Subtree cap; can be tightened but never loosened |

### Invariants

```
Depth <= MaxDepth
Depth <= LocalMaxDepth
child.Depth = parent.Depth + 1
child.MaxDepth = parent.MaxDepth              // inherited from root
child.LocalMaxDepth <= parent.LocalMaxDepth   // can only tighten
```

### Example tree

```
overseer (Depth=0, MaxDepth=3, LocalMaxDepth=3)
├── coder-backend (Depth=1, LocalMaxDepth=2)
│   └── reviewer-api (Depth=2, LocalMaxDepth=1)
│       └── [BLOCKED – Depth would be 3 but LocalMaxDepth=1]
└── coder-frontend (Depth=1, LocalMaxDepth=3)
    ├── helper-css (Depth=2, LocalMaxDepth=2)
    └── helper-a11y (Depth=2, LocalMaxDepth=2)
```

## Spawn protocol

Agents request subagents by calling the `agent.spawn` tool. This tool does **not** directly create sessions — it sends a mail message to the overseer.

### Spawn request

When `agent.spawn` is called, the tool constructs a mail message:

```json
{
  "to": "actor:system:overseer",
  "sender": "<calling-agent-actor-id>",
  "subject": "spawn.request:<epic-id>:<parent-plan-node-id>",
  "body": {
    "spawn_reason": "Frontend and backend work can proceed in parallel",
    "caller_depth": 1,
    "caller_max_depth": 3,
    "caller_local_max_depth": 2,
    "requested_subagents": [
      {
        "role": "coder",
        "task": "Implement the API endpoint for user preferences",
        "suggested_actor_id": "team:backend",
        "local_max_depth": 1
      }
    ]
  },
  "kind": "instruction",
  "ack_required": true
}
```

### Overseer processing

The overseer:

1. Validates depth constraints against the tree invariants.
2. Creates plan nodes (emits `plan.updated`).
3. Spawns approved sessions.
4. Sends a response mail.

### Accepted response

```json
{
  "subject": "spawn.accepted:<epic-id>:<request-id>",
  "body": {
    "spawned_agents": [
      {
        "actor_id": "actor:team:backend:coder-1",
        "session_id": "01KB...",
        "depth": 2,
        "plan_node_id": "node-123"
      }
    ],
    "denied_agents": [
      {
        "role": "reviewer",
        "reason": "depth_limit_exceeded"
      }
    ]
  }
}
```

### Denied response

```json
{
  "subject": "spawn.denied:<epic-id>:<request-id>",
  "body": {
    "reason": "all_requests_exceed_depth_limit",
    "suggestion": "Complete current task or escalate to overseer for plan revision"
  }
}
```

## Depth limit rules

### How depth is computed

When spawning children, the overseer computes:

```
child.Depth         = parent.Depth + 1
child.MaxDepth      = root.MaxDepth              // always inherited
child.LocalMaxDepth = min(parent.LocalMaxDepth, requested_local_max_depth)
```

### Denial conditions

| Condition | Result |
|-----------|--------|
| `parent.Depth >= parent.MaxDepth` | Deny: global limit reached |
| `parent.Depth >= parent.LocalMaxDepth` | Deny: subtree limit reached |
| `requested_local_max_depth > parent.LocalMaxDepth` | Clamp to parent's limit (not deny) |

### Escalation on denial

If an agent receives `spawn.denied`, it should:

1. Attempt to complete the task itself.
2. Or send a `plan.review_needed` mail to the overseer requesting plan revision.

## Decision heuristics

### When to split

- Task is **parallelizable** (frontend + backend, multiple services).
- Task requires **different expertise** (security review, data engineering).
- Current subtask list exceeds complexity threshold.
- Remaining depth budget allows (`Depth < LocalMaxDepth`).

### When not to split

- Near depth limit (`Depth >= LocalMaxDepth - 1`).
- Coordination overhead exceeds benefit (trivial tasks).
- Task is inherently sequential.
- Already blocked waiting on another subagent.

## Overseer responsibilities

### Planning

- Translate user goals into epic task graphs via `todo/manage.plan`.
- Approve or refine proposed plans before applying.
- Maintain plan integrity: critical path structure, task scoping via `scope_paths`, clear dependencies and epics.

### Coordination

- Assign tasks to work agents (coder, reviewer, fixer).
- Communicate via mailbox: `instruction` messages for who should do what next, `info`/`alert` messages for plan events.
- React to signals from hooks and agents: scope violations (`task_guard`), file contention (`file_guard`), agent-generated `plan.review_needed` requests.

### Replanning

- Periodically or on demand, run `todo/manage.graph_insights` on relevant epics.
- Run `todo/manage.recommend` to evaluate scores and bottlenecks.
- Decide when to split large tasks, merge small tasks, or reorder priorities.
- Apply changes via `todo/manage.plan` (`mode="apply"`) and emit `plan.updated:<epic-id>` messages.

## Overseer capabilities

The overseer must be allowed to call these operations:

**Tasks/planning:**
- `todo/manage.plan` (both `mode="draft"` and `mode="apply"`)
- `todo/manage.graph_insights`
- `todo/manage.recommend`
- `todo/manage.add`
- `todo/manage.complete`
- `todo/manage.list`

**Mailbox/blackboard:**
- `mailbox/manage.inbox`
- `mailbox/manage.send`
- `mailbox/manage.reserve`
- `mailbox/manage.release`
- `mailbox/manage.list_reservations`

## Plan event wire contract

Plan events are carried as mailbox messages or job progress/result envelopes. The key `meta` fields for plan events:

| Field | Required | Meaning |
|-------|----------|---------|
| `epic_id` | yes | Epic identifier |
| `plan_id` | yes | Logical plan identifier (ULID per epic) |
| `version` | yes | Monotonically increasing plan version per `(workspace_id, epic_id)` |
| `source` | yes | Originator: `overseer`, `agent`, `human` |
| `reason` | yes | Machine-readable reason: `initial_create`, `replan`, `guard_violation` |
| `trace_id` | optional | Protocol v1 trace id linking envelopes |
| `workspace_id` | optional | Logical workspace identifier |
| `priority` | optional | Advisory priority: `low`, `normal`, `high` |

`version` must be incremented on every `plan.updated` event. `plan.created` starts at `version = 1`.

### Plan event types

| Event | Meaning |
|-------|---------|
| `plan.created` | Initial plan created for an epic |
| `plan.updated` | Plan modified (replan, guard violation, task completion) |
| `plan.review_needed` | Overseer or agent requesting human review |

### Error codes for plan operations

| Code | Meaning |
|------|---------|
| `E_UNAUTHORIZED` | Non-overseer attempted a plan apply or privileged action |
| `E_INVALID_PLAN` | Plan payload structurally invalid or violates invariants |
| `E_CONFLICT` | Concurrent plan edits or stale `version` detected |
| `E_NOT_FOUND` | Referenced epic/plan/task does not exist |

## Mail subject conventions

| Subject pattern | Sender | Description |
|----------------|--------|-------------|
| `spawn.request:<epic>:<node>` | Any agent | Request to spawn subagents |
| `spawn.accepted:<epic>:<req>` | Overseer | Spawn request approved |
| `spawn.denied:<epic>:<req>` | Overseer | Spawn request denied |
| `agent.completed:<session>` | System | Agent session finished |
| `result.ready:<task>` | Child agent | Results available for parent |

## Session lifecycle with hierarchy

### Parent-child relationships

- Parent can query child status via the `agent.status` tool.
- Parent receives `agent.completed:<child-session-id>` mail when a child finishes.
- If a parent dies or cancels, the overseer may cancel its children.

### Result aggregation

- Children send results to parent via mail (`result.ready:<task-id>`).
- Parent aggregates and either continues with its own work or forwards the aggregated result up the tree.

## Constraints and policies

- Only the overseer (and optional CI/test agents) may call `todo/manage.plan` with `mode="apply"` at epic scope.
- Work agents must not change the global plan without overseer involvement.
- Work agents may request plan review via mailbox (`plan.review_needed:<epic-id>`).
- Plan events should always be sent from `actor:system:overseer`.

## Integration points

- **Runtime**: `Runtime.Spawn()` accepts hierarchy fields in `AgentConfig`, validates depth invariants, and tracks parent-child relationships.
- **Tools registry**: `agent.spawn` tool is registered for all agents; the implementation sends mail and optionally awaits the overseer response.
- **Overseer listener**: Polls mailbox for `spawn.request` messages, processes requests, updates plan, spawns sessions, and sends responses.
- **Hooks**: `mail_router` treats overseer plan messages as high-priority context. `task_guard` and `file_guard` emit `plan.review_needed` messages on repeated violations or contention.
