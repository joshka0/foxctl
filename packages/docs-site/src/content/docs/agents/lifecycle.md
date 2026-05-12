---
title: Agent lifecycle
description: Spawn, inspect, ask, watch, and resume foxctl agents using the daemon runtime.
---

The agent daemon persists agents, mailbox messages, and conversation state across engine runs. This page covers every command in the agent lifecycle, the flags that control behavior, execution modes, and the tools available to each role.

## Execution modes

Every agent runs in one of four execution modes, set at spawn time via `--exec-mode`:

| Mode | Behavior | Typical use |
|------|----------|-------------|
| `reactive` (default) | Waits for mailbox asks/commands and responds turn-by-turn | Standard assistants, interactive Q&A |
| `autonomous` | Runs the initial prompt plus bounded continuation turns, then exits | Bounded research tasks, batch runs |
| `proactive` | Runs autonomous turns and stays alive with periodic think cycles and mailbox polling | Long-lived workers, research agents that should remain available for follow-up |
| `tick` | Stays alive and runs one scheduled interval-driven cycle each tick | Simulations, scheduled workers, bridge-driven BEAM agents |

### Tick mode safety contract

Tick mode is intentionally cost-constrained:

- Execution is forced onto `lmstudio` at runtime to keep costs local and predictable.
- `--think-interval` controls the cadence between ticks (in seconds).
- `--timeout 0` means "no outer session deadline" — the agent can run indefinitely.
- Individual LLM/tool turns remain bounded even when the outer session is infinite.

Tick agents can terminate themselves by calling the `end_tick` tool, which cleanly ends the long-running tick loop instead of leaving the agent alive and idling.

## Spawn an agent

```bash
foxctl agent spawn \
  --role researcher \
  --prompt "Research the storage architecture" \
  --exec-mode proactive \
  --max-auto-turns 3 \
  --max-iterations 20
```

### Spawn a tick-driven simulation agent

```bash
foxctl agent spawn \
  --role researcher \
  --prompt "Advance the simulation one step per tick" \
  --exec-mode tick \
  --think-interval 5 \
  --timeout 0
```

### Spawn flags

| Flag | Default | Description |
|------|---------|-------------|
| `--role` | — | Agent role: `overseer`, `researcher`, `coder`, `planner`, `reviewer` |
| `--prompt` | — | Initial instructions for the agent |
| `--exec-mode` | `reactive` | One of `reactive`, `autonomous`, `proactive`, `tick` |
| `--llm-provider` | auto-detect | Provider: `openrouter`, `cerebras`, `groq`, `openai`, `anthropic` |
| `--llm-model` | provider default | Model name (e.g., `openrouter/aurora-alpha`) |
| `--max-iterations` | `10` | Maximum tool calls per engine run |
| `--max-auto-turns` | `1` | Maximum autonomous continuation turns |
| `--max-context-tokens` | `0` | Context budget (0 means no limit) |
| `--think-interval` | — | Seconds between think cycles (for `proactive` and `tick` modes) |
| `--timeout` | — | Session timeout in seconds (`0` = no deadline) |

## Run the daemon loop

After spawning, the agent runs via the foreground daemon loop:

```bash
foxctl agent run <agent-id>
```

This reads mailbox messages for the agent's namespace, executes turns, and writes replies and events back to storage.

## Inspect and watch

```bash
# View agent metadata and current status
foxctl agent info <agent-id>

# Stream live events from the agent
foxctl agent watch <agent-id>
```

`agent info` returns the agent's role, execution mode, current state, and session metadata. `agent watch` provides a live NDJSON event stream including agent starts, job submissions, mailbox messages, and termination.

## Ask a question

```bash
# Send a question and wait for the reply
foxctl agent ask <agent-id> --question "What did you find?" --wait
```

The ask pipeline sends a `MessageTypeAsk` to the agent's mailbox. After the engine processes it, a `MessageTypeReply` is sent back with correlation headers. The CLI polls its own namespace for the matching reply:

```text
CLI (agent ask)
  → mailbox.Send(ask)
  → daemon poll loop receives message
  → agent runtime executes turn(s)
  → mailbox.Send(reply)
  → CLI polls caller namespace and returns reply
```

### Dispatcher paths

`agent ask` supports two dispatcher paths:

- **Mailbox dispatcher** (default): Routes through the daemon's mailbox system.
- **Jido dispatcher**: Routes through the v2 `AskService` with Jido runtime bridge dispatch.

To use the Jido-backed path:

```bash
foxctl agent ask <agent-id> --question "..." --dispatcher jido
```

Be explicit about which path you are exercising — findings from the classic `agent run` path are not evidence about the Jido bridge unless that dispatcher was actually used.

## Resume a session

```bash
foxctl agent resume <session-id> --prompt "Continue from the prior summary"
```

Resume continues a previous session with full context from the earlier conversation history. The agent retains its accumulated `ConversationHistory` across runs.

## Kill an agent

```bash
foxctl agent kill <agent-id>
```

Stops the agent's daemon loop. The agent's conversation history and mailbox state are preserved in storage unless explicitly cleaned.

## View agent hierarchy

```bash
# Show the agent tree for a session
foxctl agent hierarchy <session-id>

# List all agents
foxctl agent list
```

## Conversation memory

Conversation history is retained on the session state and reused across daemon turns, bounded by context and token limits. The `--max-context-tokens` flag prevents unbounded growth.

### Task continuity

Task continuity surfaces provide structured summaries for cross-session context:

```bash
# Structured command for agents and scripts
foxctl context task-history-summary
```

For hook injection, use the wrapper script:

```bash
configs/hooks/task-continuity-summary.sh
```

## Role-specific tools

Each role has a defined set of tools it can access:

| Tool | All roles | researcher | coder | overseer |
|------|-----------|------------|-------|----------|
| `fs_read_file` | ✅ | | | |
| `fs_list_dir` | ✅ | | | |
| `code_search` | ✅ | | | |
| `think` | ✅ | | | |
| `context_search` | | ✅ | | ✅ |
| `smart_search` | | ✅ | | ✅ |
| `context_grep` | | ✅ | | ✅ |
| `repo_index_search` | | ✅ | | |
| `repo_index_expand` | | ✅ | | |
| `repo_index_open` | | ✅ | | |
| `repo_index_dag_grep` | | ✅ | | |
| `memory_query` | | ✅ | | |
| `session_recall` | | ✅ | | |
| `session_timeline` | | ✅ | | ✅ |
| `fs_write_file` | | | ✅ | |
| `agent_spawn` | | | | ✅ |
| `agent_list` | | | | ✅ |
| `agent_status` | | | | ✅ |
| `agent_kill` | | | | ✅ |
| `agent_hierarchy` | | | | ✅ |
| `agent_wait` | | | | ✅ |

## Provider and model selection

Provider and model are configured at spawn time via `--llm-provider` and `--llm-model`, or through environment configuration. The auto-detection priority (first available key wins) is: `openrouter` → `cerebras` → `groq` → `openai`.

Default models by provider:

| Provider | Default model | Notes |
|----------|--------------|-------|
| `openrouter` | `openrouter/aurora-alpha` | Free tier, good tradeoff |
| `cerebras` | Provider default | Fast inference |
| `groq` | Provider default | Fast inference |
| `openai` | Provider default | |
| `anthropic` | Provider default | |

Keep API keys in environment variables or secure secret mounts — never in inline prompt text.

## Researcher workflow

The researcher role combines semantic search tools with file access for deep codebase investigation. The recommended spawn command:

```bash
foxctl agent spawn --role researcher \
  --prompt "Research <topic>. Read the actual source files and include code snippets." \
  --exec-mode autonomous \
  --llm-provider openrouter --llm-model openrouter/aurora-alpha \
  --max-auto-turns 3 --max-iterations 20
```

The researcher's built-in strategy follows five phases:

1. **DISCOVER** — Use `context_search` or `smart_search` to find relevant files
2. **READ** — Use `fs_read_file` to read key source files (at least 3–5)
3. **GREP** — Use `code_search` and `context_grep` for exact patterns
4. **DEEPEN** — Use `memory_query` for past gotchas, `session_recall` for prior sessions
5. **GRAPH** — Use `repo_index_dag_grep` for call and reference relationships

### Model benchmarks

| Model | Time | Output | Notes |
|-------|------|--------|-------|
| `openrouter/aurora-alpha` | ~20s | ~12,890 chars | Best tradeoff — free, fast, deep |
| `minimax/minimax-m2.5` | ~120s | ~8,761 chars | Slower and shallower |
| Claude Code reviewer (Opus) | ~173s | ~24,901 chars | Deepest but ~$15/run |

With `--max-auto-turns 3 --max-iterations 20`, aurora-alpha produces approximately 52% of Opus depth at zero cost and 10× speed.

## Current routing status

Different agent commands exercise different runtime paths. This table shows the current routing:

| Surface | Current path | Notes |
|--------|--------------|-------|
| `agent run` | `internal/agent/daemon.Run` | Foreground mailbox-driven runtime |
| `agent ask` | v2 `AskService` | Dispatcher can be mailbox or Jido-backed |
| `agent ask-status` | v2 projections + event store | Reads run state and terminal callback data |
| `agent spawn` | Daemon first, legacy fallback | CLI is not fully hard-cut to v2 |
| `agent list` | Local agent store path in CLI | Not v2-service-only today |
| `agent kill` | Mixed local/daemon management | v2 kill service exists but is not the only live path |

When debugging nested tool execution, record which binary path invoked `foxctl` and which dispatcher/runtime path was active.
