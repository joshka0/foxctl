---
name: agentctl Multi-Agent
description: Spawn and coordinate subagents with depth limits, roles, and hierarchy tracking. Manage agent sessions.
---

# Multi-Agent Coordination with agentctl

Hierarchical agent spawning with depth limits and role-based coordination.

## Agent Hierarchy

```
Depth 0: Overseer (actor:system:overseer)
  ├── Depth 1: Planner Agent
  │    └── Depth 2: Coder Subagent
  └── Depth 1: Coder Agent
       └── Depth 2: Reviewer Subagent
```

## Depth Limits

Three depth controls prevent runaway spawning:

- **Depth** - Current level (0 = root overseer)
- **MaxDepth** - Global cap for entire tree (default: 3)
- **LocalMaxDepth** - Subtree cap (can tighten, never loosen)

## Agent Roles

- `planner` - Task decomposition and planning
- `coder` - Code implementation
- `reviewer` - Code review
- `researcher` - Information gathering

## Spawn Request (via agent.spawn tool)

When an agent needs subagents:

```json
{
	"tool": "agent.spawn",
	"args": {
		"epic_id": "epic-123",
		"requested_subagents": [
			{
				"role": "coder",
				"reason": "Implement auth module",
				"local_max_depth": 2
			}
		]
	}
}
```

## Spawn Validation

Overseer validates each spawn request:

1. **Depth check** - `caller_depth < max_depth`
2. **Local limit check** - `caller_depth < local_max_depth`
3. **Role filter** - Allowed roles only
4. **Concurrency limit** - Max concurrent agents (default: 10)

## Denial Reasons

- `depth_limit_exceeded` - Global max depth reached
- `local_limit_exceeded` - Subtree limit reached
- `resource_exhausted` - Too many concurrent agents
- `policy_violation` - Custom policy denied spawn

## Session Management

Each agent runs in a session with:

- Unique session ID (ULID)
- Actor ID (`actor:<role>:<id>`)
- Parent-child tracking
- Status tracking (running, completed, failed)

## Hierarchy Queries

Get agent tree structure:

```bash
# List all active sessions
agentctl agent list

# Get hierarchy from session
agentctl agent hierarchy --session <session-id>
```

## Concurrency Controls

- **MaxConcurrentAgents** - Limits total active sessions
- **Atomic enforcement** - TOCTOU-safe concurrency checks
- **Advisory pre-check** - Early rejection before spawn attempt

## Actor ID Convention

```
actor:<role>:<identifier>

Examples:
- actor:system:overseer
- actor:system:admin
- actor:coder:agent1
- actor:planner:epic-123
```

## Integration Points

- **Mailbox** - Inter-agent messaging
- **Blackboard** - Shared state coordination
- **File Reservations** - Prevent edit conflicts
- **Task Store** - Work item tracking
