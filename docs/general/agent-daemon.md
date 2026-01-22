# Agent Daemon Architecture

> Status: Fully implemented (LLMChatEngine + DSPy routing)

## Overview

The agent daemon runs companion and autonomous agents via the mailbox polling loop (L1 loop). It routes agents to different execution engines based on their `exec_mode`.

## Engine Selection

```
┌────────────────────────────────────────────────────────────────┐
│                      Agent Daemon                               │
├────────────────────────────────────────────────────────────────┤
│                                                                 │
│   exec_mode = "reactive" (default)                             │
│   ┌─────────────────────────────────────────────────────────┐  │
│   │  LLMChatEngine via companion.Service                    │  │
│   │  - Simple request/response                              │  │
│   │  - Conversation memory (L0/L1/L2)                       │  │
│   │  - RLM context tools (optional)                         │  │
│   │  - Default provider: cerebras                           │  │
│   └─────────────────────────────────────────────────────────┘  │
│                                                                 │
│   exec_mode = "autonomous" | "proactive"                       │
│   ┌─────────────────────────────────────────────────────────┐  │
│   │  DSPy ReAct Agent                                       │  │
│   │  - Tool calling loop                                    │  │
│   │  - Multi-step reasoning                                 │  │
│   │  - Requires: gemini, openai, anthropic, groq, openrouter│  │
│   └─────────────────────────────────────────────────────────┘  │
│                                                                 │
└────────────────────────────────────────────────────────────────┘
```

## Execution Modes

| Mode | Engine | Use Case | Provider Support |
|------|--------|----------|------------------|
| `reactive` | LLMChatEngine | Chat companions, simple Q&A | Any (cerebras default) |
| `autonomous` | DSPy ReAct | Tool-using agents, multi-step tasks | gemini, openai, anthropic, groq, openrouter |
| `proactive` | DSPy ReAct | Background workers, scheduled tasks | gemini, openai, anthropic, groq, openrouter |

## Default Configuration

For reactive agents (companions), the daemon uses these defaults:

```go
// Provider resolution order:
1. agentRecord.LLMProvider  // Agent-specific override
2. opts.LLMProvider         // Daemon startup flag
3. "cerebras"               // Default fallback

// Model resolution for cerebras:
1. agentRecord.LLMModel     // Agent-specific model
2. opts.LLMModel            // Daemon startup flag
3. "llama-4-scout-17b-16e-instruct"  // Default cerebras model
```

## Spawning Agents

### Chat Companion (Reactive)

```bash
# Spawn a reactive companion using LLMChatEngine
agentctl agent spawn \
  --name "Luna" \
  --role companion \
  --exec-mode reactive \
  --llm-provider cerebras \
  --llm-model "llama-3.3-70b" \
  --system-prompt @prompt.txt

# Run the daemon
agentctl agent run <agent-id>
```

### Autonomous Agent (DSPy)

```bash
# Spawn an autonomous agent using DSPy ReAct
agentctl agent spawn \
  --name "Coder" \
  --role coder \
  --exec-mode autonomous \
  --llm-provider openrouter \
  --llm-model "anthropic/claude-sonnet-4-20250514"

# Run the daemon
agentctl agent run <agent-id>
```

## Message Flow

### Reactive Agent (LLMChatEngine)

```
Mailbox Message
    │
    ▼
┌─────────────────────────────────────────┐
│ handleAsk() / handleConsoleAsk()        │
├─────────────────────────────────────────┤
│ 1. Resolve conversation ID              │
│    - Explicit: --conversation-id        │
│    - Fallback: agent-id:caller-ns       │
│                                         │
│ 2. companion.Service.Chat()             │
│    - Injects conversation memory        │
│    - Executes via LLMChatEngine         │
│    - Stores turns on success            │
│                                         │
│ 3. Reply via mailbox                    │
└─────────────────────────────────────────┘
```

### Autonomous Agent (DSPy)

```
Mailbox Message
    │
    ▼
┌─────────────────────────────────────────┐
│ handleAsk() / handleConsoleAsk()        │
├─────────────────────────────────────────┤
│ 1. Build DSPy task context              │
│                                         │
│ 2. dspyAgent.Execute()                  │
│    - ReAct reasoning loop               │
│    - Tool execution via registry        │
│    - Multi-step until completion        │
│                                         │
│ 3. Reply via mailbox                    │
└─────────────────────────────────────────┘
```

## Provider Configuration

### Cerebras (Default for Reactive)

```bash
# Set in .env
CEREBRAS_API_KEY=csk-xxx

# Or pass via daemon options
agentctl agent run <id> --llm-api-key $CEREBRAS_API_KEY
```

### OpenRouter (Common for Autonomous)

```bash
# Set in .env
OPENROUTER_API_KEY=sk-or-xxx

# Agent uses openrouter provider
agentctl agent spawn --llm-provider openrouter --llm-model "anthropic/claude-sonnet-4-20250514"
```

## Companion Service Features

When using LLMChatEngine via companion.Service:

| Feature | Status | Description |
|---------|--------|-------------|
| Conversation Memory | Yes | L0/L1/L2 progressive decay |
| Context Variables | Yes | RLM context store |
| Tool Calling | Optional | Via RLM tools (not DSPy) |
| Compression Daemon | Yes | Background L0→L1→L2 summarization |

## Code References

| Component | Location |
|-----------|----------|
| Engine routing | `internal/agent/daemon/daemon.go:237` |
| Companion service creation | `internal/agent/daemon/daemon.go:259-301` |
| DSPy agent creation | `internal/agent/daemon/daemon.go:239-258` |
| Message handlers | `internal/agent/daemon/handlers.go` |
| Companion service | `internal/companion/service.go` |
| LLMChatEngine | `internal/engine/llmchat_engine.go` |

## Troubleshooting

### "unsupported LLM provider: cerebras" with DSPy

**Cause:** Agent has `exec_mode: autonomous` or `exec_mode: proactive` but uses cerebras.

**Fix:** Either:
1. Change to `exec_mode: reactive` for cerebras
2. Or use a DSPy-supported provider: gemini, openai, anthropic, groq, openrouter

```bash
# Check agent exec_mode
sqlite3 ~/.agentctl/storage/agents.db "SELECT exec_mode FROM agents WHERE id='$AGENT_ID'"

# Update to reactive
sqlite3 ~/.agentctl/storage/agents.db "UPDATE agents SET exec_mode='reactive' WHERE id='$AGENT_ID'"
```

### Companion memory not injected

**Cause:** Memory DB not provided to companion service.

**Check logs for:**
```
{"level":"debug","memory_enabled":false,"message":"Service created"}
```

**Fix:** Ensure daemon has `--enable-companion-memory` or agent role triggers it.

## Related

- [Companion Memory](./companion-memory.md) - L0/L1/L2 conversation memory
- [RLM Context](./rlm-context.md) - Context variable system
- [Architecture](./architecture.md) - Overall system design
