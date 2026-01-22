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

| Role | Purpose | Special Capabilities |
|------|---------|---------------------|
| `overseer` | Coordinate multi-agent workflows | agent_spawn, agent_list, agent_kill, agent_hierarchy, agent_wait |
| `planner` | Task decomposition and planning | todo.add, todo.query, graph management |
| `coder` | Code implementation | fs_write_file, edit tools |
| `reviewer` | Code review (read-only) | Read/search tools, no write access |
| `researcher` | Information gathering | Search, memory, session recall |
| `verifier` | Validate claims and results | verification tools |
| `fixer` | Apply targeted fixes | Limited write scope |

### Overseer Role

The overseer is a special coordination agent that manages agent hierarchies:

```bash
# Spawn an overseer to coordinate a multi-agent task
agentctl agent spawn --role overseer --prompt "Coordinate a codebase analysis:
1. Spawn a researcher to find all API endpoints
2. Spawn a coder to document any undocumented endpoints
3. Wait for both to complete and summarize findings"
```

**Overseer-specific tools:**
- `agent_spawn` - Request spawning of subagents (validates depth limits)
- `agent_list` - List all active agent sessions
- `agent_status` - Get detailed status of an agent
- `agent_kill` - Terminate an agent session
- `agent_hierarchy` - View the full agent tree
- `agent_wait` - Wait for all children to complete

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

## CLI Agent Spawn

Spawn agents directly via CLI:

```bash
# Basic spawn with inline prompt
agentctl agent spawn --role researcher --prompt "Find all hook implementations"

# With context budget (stops if exceeded)
agentctl agent spawn \
  --role researcher \
  --prompt "Analyze codebase structure" \
  --exec-mode autonomous \
  --max-iterations 20 \
  --max-context-tokens 30000

# From prompt file
agentctl agent spawn --role coder --prompt-file task.txt
```

### Key Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--prompt` | - | Inline prompt text |
| `--prompt-file` | - | Path to prompt file |
| `--role` | - | Agent role (researcher, coder, planner, reviewer) |
| `--exec-mode` | reactive | Execution mode: reactive, autonomous, proactive |
| `--max-iterations` | 10 | Max tool calls per turn |
| `--max-context-tokens` | 0 | Context budget in tokens (0=no limit) |
| `--max-auto-turns` | 1 | Max autonomous continuations |
| `--llm-provider` | cerebras | LLM provider override |
| `--llm-model` | - | Model override |

### Context Budget

The `--max-context-tokens` flag prevents runaway context accumulation:

```
[CONTEXT] iter=4 msgs=11 prompt_tokens=10040 ... finish=tool_calls
[CONTEXT] budget exceeded: 10040 > 10000 limit, stopping
```

When exceeded, the session stops with `status=error` and `StopReasonContextBudget`.

## Session Management

Each agent runs in a session with:

- Unique session ID (ULID)
- Actor ID (`actor:<role>:<id>`)
- Parent-child tracking
- Status tracking (running, completed, failed, error)

### Turn Persistence

Agent sessions automatically persist turns to `session_turns` table:

```sql
-- Each turn stores:
session_id, turn_index, role, content_preview, tool_calls, tokens_used
```

### Session Continuation

Resume a previous session with additional context:

```bash
# Continue a session
agentctl agent resume <session-id> --prompt "Based on your findings, tell me more about X"
```

The resume command:
- Loads previous turns from `session_turns` table
- Builds prompt including "PREVIOUS CONVERSATION:" context
- Creates linked session via `session_edges` (edge_type: "continues")
- New session inherits workspace context from original

## Hierarchy Queries

Get agent tree structure:

```bash
# List all active sessions
agentctl agent list

# Show full agent hierarchy (all roots)
agentctl agent hierarchy

# Show hierarchy from specific session
agentctl agent hierarchy <session-id>
```

Example output:
```
● actor:system:overseer [overseer] depth=0 session=01KFGX...
  └─ ● actor:researcher:01KFGY... [researcher] depth=1 session=01KFGY...
  └─ ✓ actor:coder:01KFGZ... [coder] depth=1 session=01KFGZ...
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
