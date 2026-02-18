# Agent Hierarchy & Spawn Protocol

**Version:** 1.0\
**Status:** Draft\
**Related:** [overseer_profile.md](overseer_profile.md),
[../archive/specs/dspy_go_agents.md](../archive/specs/dspy_go_agents.md) (legacy),
[mailbox_blackboard.md](mailbox_blackboard.md)

---

## 1. Overview

This spec defines how agents form a hierarchy (tree) and how subagents can
request to spawn further subagents. The key principles are:

1. **Overseer owns the plan** – only the overseer (`actor:system:overseer`) can
   create/modify plan nodes.
2. **Depth limits flow down** – the root sets a global `MaxDepth`; children can
   only _tighten_ their subtree's limit.
3. **Spawn via mail** – agents request spawns by sending mail to overseer, not
   by directly creating sessions.
4. **Tree metadata** – every agent knows its position in the tree (`Depth`,
   `ParentActorID`, `RootActorID`).

---

## 2. Agent tree model

### 2.1. Hierarchy fields

Every `AgentConfig` and `AgentSession` includes:

| Field           | Type     | Description                                       |
| --------------- | -------- | ------------------------------------------------- |
| `RootActorID`   | `string` | The tree root (usually `actor:system:overseer`).  |
| `ParentActorID` | `string` | Immediate parent; empty for overseer.             |
| `Depth`         | `int`    | 0 for overseer, increments per level.             |
| `MaxDepth`      | `int`    | Global cap for the entire tree (set by overseer). |
| `LocalMaxDepth` | `int`    | Subtree cap; can be tightened but never loosened. |

### 2.2. Invariants

```
Depth <= MaxDepth
Depth <= LocalMaxDepth
child.Depth = parent.Depth + 1
child.MaxDepth = parent.MaxDepth          // inherited from root
child.LocalMaxDepth <= parent.LocalMaxDepth  // can only tighten
```

### 2.3. Example tree

```
overseer (Depth=0, MaxDepth=3, LocalMaxDepth=3)
├── coder-backend (Depth=1, LocalMaxDepth=2)
│   └── reviewer-api (Depth=2, LocalMaxDepth=1)
│       └── [BLOCKED – Depth would be 3 but LocalMaxDepth=1]
└── coder-frontend (Depth=1, LocalMaxDepth=3)
    ├── helper-css (Depth=2, LocalMaxDepth=2)
    └── helper-a11y (Depth=2, LocalMaxDepth=2)
```

---

## 3. Spawn protocol

### 3.1. `agent.spawn` tool

Any agent can call the `agent.spawn` tool to **request** subagents. This tool
does NOT directly create sessions; it sends a mail to overseer.

**Tool signature:**

```json
{
	"name": "agent.spawn",
	"description": "Request overseer to spawn subagent(s) for parallel or specialized work",
	"inputSchema": {
		"type": "object",
		"properties": {
			"epic_id": {
				"type": "string",
				"description": "Epic scope for the spawn request"
			},
			"parent_plan_node_id": {
				"type": "string",
				"description": "Plan node to attach new subtasks under"
			},
			"spawn_reason": {
				"type": "string",
				"description": "Why splitting is beneficial (parallelism, specialization, etc.)"
			},
			"requested_subagents": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"role": {
							"type": "string",
							"enum": ["coder", "planner", "reviewer", "fixer"]
						},
						"task": {
							"type": "string",
							"description": "Task description for subagent"
						},
						"suggested_actor_id": {
							"type": "string",
							"description": "Suggested actor ID (team:X)"
						},
						"local_max_depth": {
							"type": "integer",
							"description": "Requested subtree depth limit"
						}
					},
					"required": ["role", "task"]
				}
			},
			"wait_for_completion": {
				"type": "boolean",
				"description": "If true, block until subagents finish; if false, continue async",
				"default": false
			}
		},
		"required": ["epic_id", "spawn_reason", "requested_subagents"]
	}
}
```

### 3.2. Mail flow

When `agent.spawn` is called:

1. **Tool constructs mail:**
   ```json
   {
     "to": "actor:system:overseer",
     "sender": "<calling-agent-actor-id>",
     "subject": "spawn.request:<epic-id>:<parent-plan-node-id>",
     "body": {
       "spawn_reason": "...",
       "caller_depth": 1,
       "caller_max_depth": 3,
       "caller_local_max_depth": 2,
       "requested_subagents": [...]
     },
     "kind": "instruction",
     "ack_required": true
   }
   ```

2. **Overseer processes `spawn.request`:**
   - Validates depth constraints.
   - Creates plan nodes (emits `plan.updated`).
   - Spawns approved sessions.
   - Sends response mail.

3. **Overseer sends response:**
   ```json
   {
   	"to": "<caller-actor-id>",
   	"sender": "actor:system:overseer",
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

   Or if fully denied:
   ```json
   {
   	"subject": "spawn.denied:<epic-id>:<request-id>",
   	"body": {
   		"reason": "all_requests_exceed_depth_limit",
   		"suggestion": "Complete current task or escalate to overseer for plan revision"
   	}
   }
   ```

---

## 4. Depth limit rules

### 4.1. Overseer (root)

- Sets `MaxDepth` for the epic (e.g., 3).
- `Depth = 0`, `LocalMaxDepth = MaxDepth`.

### 4.2. When spawning children

Overseer or parent agent proposes `local_max_depth`. Overseer computes:

```
child.Depth         = parent.Depth + 1
child.MaxDepth      = root.MaxDepth              // always inherited
child.LocalMaxDepth = min(parent.LocalMaxDepth, requested_local_max_depth)
```

### 4.3. Spawn denial conditions

| Condition                                          | Result                             |
| -------------------------------------------------- | ---------------------------------- |
| `parent.Depth >= parent.MaxDepth`                  | Deny: global limit reached         |
| `parent.Depth >= parent.LocalMaxDepth`             | Deny: subtree limit reached        |
| `requested_local_max_depth > parent.LocalMaxDepth` | Clamp to parent's limit (not deny) |

### 4.4. Escalation on denial

If an agent receives `spawn.denied`, it should:

1. Attempt to complete the task itself.
2. Or send a `plan.review_needed` mail to overseer requesting plan revision.

---

## 5. Agent decision heuristics

Guidance for agent signatures on when to split:

### 5.1. Split when

- Task is **parallelizable** (frontend + backend, multiple services).
- Task requires **different expertise** (security review, data engineering).
- Current subtask list exceeds complexity threshold.
- Remaining depth budget allows (`Depth < LocalMaxDepth`).

### 5.2. Don't split when

- Near depth limit (`Depth >= LocalMaxDepth - 1`).
- Coordination overhead exceeds benefit (trivial tasks).
- Task is inherently sequential.
- Already blocked waiting on another subagent.

---

## 6. Session lifecycle with hierarchy

### 6.1. Parent-child relationships

- Parent can query child status via `agent.status` tool.
- Parent receives `agent.completed:<child-session-id>` mail when child finishes.
- If parent dies/cancels, overseer MAY cancel children (policy TBD).

### 6.2. Result aggregation

- Children send results to parent via mail (`result.ready:<task-id>`).
- Parent aggregates and either:
  - Continues with own work.
  - Sends aggregated result to its parent or overseer.

---

## 7. Mail subject conventions

| Subject pattern               | Sender      | Description                  |
| ----------------------------- | ----------- | ---------------------------- |
| `spawn.request:<epic>:<node>` | Any agent   | Request to spawn subagents   |
| `spawn.accepted:<epic>:<req>` | Overseer    | Spawn request approved       |
| `spawn.denied:<epic>:<req>`   | Overseer    | Spawn request denied         |
| `agent.completed:<session>`   | System      | Agent session finished       |
| `result.ready:<task>`         | Child agent | Results available for parent |

---

## 8. Integration points

### 8.1. Runtime

- `Runtime.Spawn()` accepts hierarchy fields in `AgentConfig`.
- Validates `Depth < MaxDepth` and `Depth < LocalMaxDepth`.
- Tracks parent-child relationships for status queries.

### 8.2. Tools registry

- `agent.spawn` tool registered for all agents.
- Tool implementation sends mail, awaits response if `wait_for_completion`.

### 8.3. Overseer listener

- Overseer polls mailbox for `spawn.request` messages.
- Processes requests, updates plan, spawns sessions.
- Sends `spawn.accepted` or `spawn.denied` responses.

---

## 9. Future considerations

- **Preemption**: Overseer canceling low-priority subtrees.
- **Resource budgets**: Token/cost limits per subtree.
- **Dynamic depth**: Overseer adjusting `MaxDepth` mid-execution.
- **Cross-epic spawning**: Agents spawning into different epics.
