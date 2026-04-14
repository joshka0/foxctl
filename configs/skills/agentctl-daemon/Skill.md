---
name: agentctl Daemon
description: Agent daemon architecture, exec_mode routing, and LLM engine selection for reactive vs autonomous agents.
---

# Agent Daemon Engine Selection

The agent daemon routes to different execution engines based on `exec_mode`.

## Engine Routing

```
exec_mode = "reactive" (default)
    └── LLMChatEngine via companion.Service
        - Provider: cerebras (default)
        - Simple request/response
        - Conversation memory (L0/L1/L2)

exec_mode = "autonomous" | "proactive"
    └── LLMChatEngine via companion.Service
        - Tool calling loop
        - Multi-step reasoning
        - Same provider support as reactive
```

## Quick Reference

| Mode | Engine | Default Provider | Tool Loop |
|------|--------|------------------|-----------|
| `reactive` | LLMChatEngine | cerebras | No |
| `autonomous` | LLMChatEngine | cerebras | Yes |
| `proactive` | LLMChatEngine | cerebras | Yes |

## Autonomous vs Proactive: When to Use

### Autonomous Mode

**Trigger:** Responds to incoming messages (mailbox polling).

**Use when:**
- Agent should wait for tasks/questions
- Work is request-driven (human or other agent sends work)
- Research agents that answer queries
- Coder agents that implement assigned tasks

```bash
# Autonomous agent waits for work
agentctl agent spawn --name "Researcher" --exec-mode autonomous
agentctl agent run <agent-id>  # Starts polling mailbox

# Send work to agent
agentctl agent ask <agent-id> --question "Research the auth flow" --wait
```

### Proactive Mode

**Trigger:** Self-initiates work based on schedule or conditions.

**Use when:**
- Agent should work without being asked
- Scheduled/periodic tasks (health checks, summaries)
- Background workers (indexing, cleanup)
- Monitoring agents (watch for conditions, alert)

```bash
# Proactive agent self-starts work
agentctl agent spawn --name "Indexer" --exec-mode proactive
agentctl agent run <agent-id>  # Immediately starts working

# Agent doesn't wait for messages - it has its own task loop
```

### Decision Matrix

| Scenario | Mode | Reason |
|----------|------|--------|
| "Answer questions about codebase" | autonomous | Wait for questions |
| "Index all files every hour" | proactive | Self-scheduled work |
| "Implement task when assigned" | autonomous | Wait for assignment |
| "Monitor logs for errors" | proactive | Continuous watching |
| "Review PR when requested" | autonomous | Wait for request |
| "Generate daily summaries" | proactive | Self-scheduled |

## Spawning Agents

### Chat Companion (Reactive)

```bash
# Uses LLMChatEngine with cerebras
agentctl agent spawn \
  --name "Luna" \
  --role companion \
  --exec-mode reactive \
  --llm-provider cerebras \
  --llm-model "llama-3.3-70b" \
  --system-prompt @prompt.txt

# Start the daemon
agentctl agent run <agent-id>

# Send message (uses conversation memory)
agentctl agent ask <agent-id> \
  --question "Hello!" \
  --conversation-id "user-session-1" \
  --wait
```

### Autonomous Agent

```bash
agentctl agent spawn \
  --name "Coder" \
  --role coder \
  --exec-mode autonomous \
  --llm-provider openrouter \
  --llm-model "anthropic/claude-sonnet-4-20250514"

# Start the daemon
agentctl agent run <agent-id>
```

## Context Budget Control

Prevent runaway context accumulation with `--max-context-tokens`:

```bash
# Stop if context exceeds 30K tokens
agentctl agent spawn \
  --role researcher \
  --prompt "Analyze the entire codebase" \
  --exec-mode autonomous \
  --max-iterations 25 \
  --max-context-tokens 30000
```

### Context Tracking

The engine logs per-iteration context to stderr:

```
[CONTEXT] iter=5 msgs=13 prompt_tokens=12820 completion_tokens=1676 total=14496 finish=stop
[CONTEXT] iter=6 msgs=15 prompt_tokens=31040 ...
[CONTEXT] budget exceeded: 31040 > 30000 limit, stopping
```

When budget is exceeded:
- Session stops with `StopReasonContextBudget`
- Session status set to `error`
- Any partial assistant response is captured

## Session Continuation

Resume previous agent sessions with follow-up prompts:

```bash
# Continue a session
agentctl agent resume <session-id> --prompt "Based on your findings, tell me more about X"
```

### How It Works

1. **Turn Persistence**: Each user/assistant turn is saved to `session_turns` table during execution
2. **Resume Loading**: Previous turns are loaded and included as "PREVIOUS CONVERSATION:" context
3. **Session Linking**: New session linked to original via `session_edges` (edge_type: "continues")

### Turn Persistence

Agent sessions automatically persist turns:

```sql
-- Each turn stores:
session_id, turn_index, role, content_preview, tool_calls, tokens_used, timestamp
```

### Use Cases

- Continue research after reviewing initial findings
- Ask follow-up questions about agent's analysis
- Refine tasks based on agent's output
- Chain multiple research phases

## Provider Defaults

For reactive agents, resolution order:

1. `agentRecord.LLMProvider` - Agent-specific
2. `opts.LLMProvider` - Daemon flag
3. `"cerebras"` - Fallback default

For cerebras model:

1. `agentRecord.LLMModel` - Agent-specific
2. `opts.LLMModel` - Daemon flag
3. `"llama-4-scout-17b-16e-instruct"` - Default

## Environment Variables

```bash
# Cerebras (reactive agents)
CEREBRAS_API_KEY=csk-xxx

# OpenRouter (autonomous agents)
OPENROUTER_API_KEY=sk-or-xxx

# Alternative providers
ANTHROPIC_API_KEY=sk-ant-xxx
OPENAI_API_KEY=sk-xxx
GEMINI_API_KEY=xxx
GROQ_API_KEY=gsk-xxx
```

## Companion Memory (Reactive Only)

When `exec_mode: reactive`, companion memory is available:

| Layer | Window | Content |
|-------|--------|---------|
| L0 (Vivid) | 24-48h | Full turns |
| L1 (Recent) | 7-14 days | Day summaries |
| L2 (History) | Permanent | Distilled context |

Enable via `--enable-companion-memory` or auto-enabled for `role: companion`.

## Common Issues

### "unsupported LLM provider" errors

The configured LLM provider is not supported or missing its API key.

**Fix:**
```bash
# Option 1: Change to reactive mode
sqlite3 ~/.agentctl/storage/agents.db \
  "UPDATE agents SET exec_mode='reactive' WHERE id='$AGENT_ID'"

# Option 2: Use a supported OpenAI-compatible provider
agentctl agent spawn --exec-mode autonomous --llm-provider openrouter --llm-model "anthropic/claude-sonnet-4-20250514"
```

### Memory not injected for companion

Check daemon logs for `memory_enabled: false`.

**Fix:** Run with `--enable-companion-memory` or ensure role is `companion`.

## Code Locations

| Component | File |
|-----------|------|
| Engine routing | `internal/agent/daemon/daemon.go:237` |
| Companion service | `internal/context/companion/service.go` |
| LLMChatEngine | `internal/runtime/engine/llmchat_engine.go` |
| Message handlers | `internal/agent/daemon/handlers.go` |

## Related Skills

- `agentctl-agents` - Multi-agent coordination
- `agentctl-memory` - Memory system (named_memory, not companion)
