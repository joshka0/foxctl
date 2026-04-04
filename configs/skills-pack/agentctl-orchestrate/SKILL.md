---
name: agentctl-orchestrate
description: "Orchestration: tasks/todos, sessions, mailbox/inbox, agent spawn/ask/kill, and multi-agent coordination."
---

## What I do
- Keep work tracked (tasks), durable (sessions), and collaborative (mailbox/inbox).
- Spawn, query, and manage persistent daemon agents.

## When to use me
- You're doing multi-step work and don't want to lose state.
- You want human-in-the-loop messaging ("overseer inbox").
- You need to spawn research/coder/overseer agents and query their results.
- You need a durable multi-agent room rather than only direct inbox messages.

## Tasks (todo)
```bash
agentctl run todo/manage --input '{"operation":"list"}'
agentctl run todo/manage --input '{"operation":"add","title":"<task>","description":"<details>"}'
agentctl run todo/manage --input '{"operation":"set_active","id":"<task-id>"}'
agentctl run todo/manage --input '{"operation":"complete","id":"<task-id>"}'
```

## Sessions
```bash
agentctl run session/save --input '{"name":"<short-name>"}'
agentctl run session/restore --input '{"name":"<short-name>"}'
```

## Mailbox / inbox
```bash
agentctl run mailbox/manage --input '{"operation":"inbox","inbox":{"actor_id":"overseer","limit":20}}'
agentctl run mailbox/manage --input '{"operation":"send","send":{"recipient":"overseer","subject":"<subject>","body":"<body>","priority":2}}'
```

## Durable room coordination

For shared multi-agent chat and room task broadcasts, use the dedicated room pack:

- `configs/skills-pack/agentctl-room/SKILL.md`

Typical flow:

```bash
agentctl room create alpha --title "Alpha Room"
agentctl room join alpha agent-a --role lead
agentctl room task add alpha --sender agent-a --title "Refactor retry path"
agentctl room loop alpha --backend tmux
```

## Agent orchestration

### Spawn agents
```bash
# Research agent (autonomous research, then queryable)
agentctl agent spawn --role researcher \
  --prompt "Research the hook system architecture" \
  --exec-mode autonomous_reactive \
  --llm-provider openrouter --llm-model openrouter/aurora-alpha \
  --max-auto-turns 3 --max-iterations 20

# Overseer (coordinates subagents)
agentctl agent spawn --role overseer \
  --prompt "Coordinate a code review" \
  --exec-mode autonomous --max-auto-turns 5
```

### Query agents
```bash
agentctl agent list                                    # List all agents
agentctl agent info <id>                               # Agent details
agentctl agent ask <id> --question "..." --wait --timeout 120s  # Ask and wait for reply
agentctl agent kill <id>                               # Stop agent
agentctl agent hierarchy                               # Show agent tree
agentctl agent watch <id>                              # Live activity
```

### Execution modes
| Mode | Behavior |
|------|----------|
| `reactive` | Waits for mailbox messages |
| `autonomous` | Runs turns then exits |
| `autonomous_reactive` | Autonomous turns, then stays alive for asks |
| `proactive` | Periodic think cycles + message polling |

### Roles and tools
| Role | Extra tools |
|------|-------------|
| `researcher` | context_search, smart_search, context_grep, repo_index_* |
| `coder` | fs_write_file |
| `overseer` | agent_spawn, agent_list, agent_kill, agent_hierarchy, agent_wait |

### LLM providers
Auto-detect priority: openrouter → cerebras → groq → openai. Default model: `openrouter/aurora-alpha` (free).
