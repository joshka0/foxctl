# Agent Daemon Architecture

> Status: Implemented, with mixed legacy and v2-backed command paths

## Overview

The foreground daemon loop is still the mailbox-driven agent runner launched by:

```bash
foxctl agent run <agent-id>
```

That path reads mailbox messages for an agent namespace, executes turns, and
writes replies/events back to storage. Around that loop, some newer command
surfaces now use v2 services and projections, especially for `ask` and
orchestration state.

## Execution Modes

| Mode | Behavior | Typical Use |
|------|----------|-------------|
| `reactive` (default) | Waits for mailbox asks/commands and responds turn-by-turn | Standard assistants |
| `autonomous` | Runs autonomous continuation turns, then exits | Bounded research/batch runs |
| `proactive` | Runs autonomous turns and stays alive with periodic think cycles + mailbox polling | Long-lived workers/research agents |
| `tick` | Stays alive and runs one scheduled interval-driven cycle each tick | Simulations, scheduled workers, bridge-driven BEAM agents |

## Lifecycle Commands

```bash
# Create an agent
foxctl agent spawn \
  --role researcher \
  --prompt "Research the storage architecture" \
  --exec-mode proactive \
  --max-auto-turns 3 \
  --max-iterations 20

# Create a tick-driven simulation agent that can run indefinitely
foxctl agent spawn \
  --role researcher \
  --prompt "Advance the simulation one step per tick" \
  --exec-mode tick \
  --think-interval 5 \
  --timeout 0

# Run daemon loop for that agent
foxctl agent run <agent-id>

# Ask a question and wait for reply
foxctl agent ask <agent-id> --question "What did you find?" --wait

# Inspect status
foxctl agent info <agent-id>

# Watch live events
foxctl agent watch <agent-id>

# Stop the agent
foxctl agent kill <agent-id>
```

## Current Routing Status

| Surface | Current path | Notes |
|--------|--------------|-------|
| `agent run` | `internal/agent/daemon.Run` | Foreground mailbox-driven runtime |
| `agent ask` | v2 `AskService` | Dispatcher mode can be mailbox or Jido-backed |
| `agent ask-status` | v2 projections + event store | Reads run state and terminal callback data |
| `agent spawn` | Daemon first, legacy fallback when daemon is absent | CLI is not fully hard-cut to v2 |
| `agent list` | Local agent store path in CLI | Not v2-service-only today |
| `agent kill` | Mixed local/daemon management path in CLI | v2 kill service exists elsewhere but is not the only live path |

## Message Flow

### Mailbox ask flow

```text
CLI (agent ask, mailbox dispatcher)
  -> mailbox.Send(ask)
  -> daemon poll loop receives message
  -> agent runtime executes turn(s)
  -> mailbox.Send(reply)
  -> CLI polls caller namespace and returns reply
```

### Jido-backed ask flow

```text
CLI (agent ask, jido dispatcher)
  -> v2 AskService
  -> Jido runtime bridge dispatch
  -> v2 events/projections update run state
  -> CLI wait/status reads projection + terminal callback
```

## Conversation Memory

Conversation history is retained on session state and reused across daemon turns,
bounded by context/token limits. Companion memory and layered context assembly
now also intersect with v2 context-building code.

## Provider/Model Selection

Provider/model are configured at spawn time (for example `--llm-provider` and
`--llm-model`) and/or via environment configuration. Keep API keys in env vars
or secure secret mounts, not inline prompt text.

### Tick Mode Safety Contract

`tick` mode is intentionally cost-constrained:

- it is forced onto `lmstudio` at execution time
- `think_interval` controls the cadence between ticks
- `timeout=0` means "no outer session deadline"
- individual LLM/tool turns remain bounded even when the outer session is infinite

This is intended for long-lived simulations and scheduled workers where cost
must stay local and predictable.

### Ending a Tick Agent

Tick agents can terminate themselves by calling the `end_tick` tool.

That cleanly ends the long-running tick loop instead of leaving the agent alive
and idling forever.

## Jido Notes

- `agent ask` can use a Jido-backed dispatcher.
- Overseer orchestration uses a Jido runtime bridge plus v2 projections.
- Companion context can optionally fetch layered context from Jido when
  `AGENTCTL_COMPANION_CONTEXT_PROVIDER=jido`.
- Jido-backed agents now receive deterministic `task_continuity` at spawn and
  ask time, and runtime-state inspection can refresh that continuity when the
  Jido state includes `workspace_root`.
- The Jido bridge now supports tick-driven bridge agents:
  - bridge children can start with `exec_mode=tick` and `think_interval`
  - the Jido side schedules recurring `foxctl.tick` signals
  - each tick drives a persistent backing `foxctl` worker through daemon RPC

Task continuity surfaces:

- structured command for agents/scripts:
  - `foxctl context task-history-summary`
- hook wrapper for prompt injection:
  - `configs/hooks/task-continuity-summary.sh`

## Code References

| Component | Location |
|-----------|----------|
| Foreground daemon loop | `internal/agent/daemon/daemon.go` |
| Mailbox handlers | `internal/agent/daemon/handlers.go` |
| CLI agent command routing | `cmd/foxctl/cmd/agent.go` |
| V2 command services | `internal/v2/services/` |
| Jido bridge | `internal/v2/adapters/jido/` |
| Companion memory service | `internal/context/companion/` |

## Related Docs

- [Runtime orchestration](./runtime-orchestration.md)
- [Agent hierarchy spec](../spec/agent_hierarchy.md)
- [Overseer profile](../spec/overseer_profile.md)
- [Architecture](./architecture.md)
