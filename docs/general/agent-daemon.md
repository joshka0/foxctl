# Agent Daemon Architecture

> Status: Implemented (mailbox-driven daemon + tool-loop execution)

## Overview

The agent daemon runs mailbox-driven agents in the foreground via:

```bash
agentctl agent run <agent-id>
```

It polls mailbox messages for the agent namespace, executes turns through the engine/tool loop, and writes replies/events back to mailbox storage.

## Execution Modes

Current spawn modes:

| Mode | Behavior | Typical Use |
|------|----------|-------------|
| `reactive` (default) | Waits for mailbox asks/commands and responds turn-by-turn | Standard assistants, low-background activity |
| `autonomous` | Runs autonomous continuation turns, then exits | Bounded research/batch runs |
| `proactive` | Runs autonomous turns and stays alive with periodic think cycles + mailbox polling | Long-lived workers/research agents |

## Lifecycle Commands

```bash
# Create an agent
agentctl agent spawn \
  --role researcher \
  --prompt "Research the storage architecture" \
  --exec-mode proactive \
  --max-auto-turns 3 \
  --max-iterations 20 \
  --think-interval 60

# Run daemon loop for that agent
agentctl agent run <agent-id>

# Ask a question and wait for reply
agentctl agent ask <agent-id> --question "What did you find?" --wait

# Inspect status
agentctl agent info <agent-id>

# Watch live events
agentctl agent watch <agent-id>

# Stop the agent
agentctl agent kill <agent-id>
```

## Message Flow

```text
CLI (agent ask)
  -> mailbox.Send(ask)
  -> daemon poll loop receives message
  -> engine/tool loop executes turn(s)
  -> mailbox.Send(reply)
  -> CLI polls caller namespace and returns reply
```

## Conversation Memory

Conversation history is retained on session state and reused across daemon turns, bounded by context/token limits. This allows follow-up asks to build on prior tool-driven research.

## Provider/Model Selection

Provider/model are configured at spawn time (for example `--llm-provider` and `--llm-model`) and/or via environment configuration. Keep API keys in env vars or secure secret mounts, not inline prompt text.

## Code References

| Component | Location |
|-----------|----------|
| Daemon runtime loop | `internal/agent/daemon/daemon.go` |
| Mailbox handlers | `internal/agent/daemon/handlers.go` |
| Engine/tool orchestration | `internal/engine/` |
| Companion memory service | `internal/companion/` |

## Related Docs

- [Agent hierarchy spec](../spec/agent_hierarchy.md)
- [Overseer profile](../spec/overseer_profile.md)
- [Architecture](./architecture.md)
