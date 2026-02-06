# Agent Daemon Architecture

> Status: Fully implemented (LLMChatEngine tool-loop)

## Overview

The agent daemon runs companion and autonomous agents via the mailbox polling loop (L1 loop). It routes agents to different execution engines based on their `exec_mode`, including story mode's gather + dialogue loop.

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
│   exec_mode = "story"                                         │
│   ┌─────────────────────────────────────────────────────────┐  │
│   │  LLMChatEngine via companion.Service                    │  │
│   │  - Gather loop w/ tools (RLM)                           │  │
│   │  - Dialogue generation                                  │  │
│   │  - Structured outputs (optional)                        │  │
│   └─────────────────────────────────────────────────────────┘  │
│                                                                 │
│   exec_mode = "autonomous" | "proactive"                       │
│   ┌─────────────────────────────────────────────────────────┐  │
│   │  LLMChatEngine via companion.Service                    │  │
│   │  - Tool calling loop                                    │  │
│   │  - Multi-step reasoning                                 │  │
│   │  - Same provider support as reactive                    │  │
│   └─────────────────────────────────────────────────────────┘  │
│                                                                 │
└────────────────────────────────────────────────────────────────┘
```

## Execution Modes

| Mode | Engine | Use Case | Provider Support |
|------|--------|----------|------------------|
| `reactive` | LLMChatEngine | Chat companions, simple Q&A | Any (cerebras default) |
| `story` | LLMChatEngine | Gather + dialogue companions, structured outputs | Any (tool-capable model recommended) |
| `autonomous` | LLMChatEngine | Tool-using agents, multi-step tasks | Any (tool-capable model recommended) |
| `proactive` | LLMChatEngine | Background workers, scheduled tasks | Any (tool-capable model recommended) |

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

### Story Companion (Gather + Dialogue)

```bash
# Spawn a story-mode companion using LLMChatEngine
agentctl agent spawn \
  --name "Stormscribe" \
  --slug "stormscribe" \
  --role companion \
  --exec-mode story \
  --llm-provider openrouter \
  --llm-model "z-ai/glm-4.7-flash"

# Run the daemon
agentctl agent run <agent-id>
```

### Autonomous Agent (LLMChatEngine)

```bash
# Spawn an autonomous agent using LLMChatEngine tool loop
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

### Story Agent (LLMChatEngine)

```
Mailbox Message
    │
    ▼
┌─────────────────────────────────────────┐
│ handleAsk() / handleConsoleAsk()        │
├─────────────────────────────────────────┤
│ 1. Resolve conversation ID              │
│                                         │
│ 2. companion.Service.Chat()             │
│    - Gather loop with tools             │
│    - Dialogue generation                │
│    - Stores turns on success            │
│                                         │
│ 3. Reply via mailbox                    │
└─────────────────────────────────────────┘
```

### Autonomous Agent (LLMChatEngine)

```
Mailbox Message
    │
    ▼
┌─────────────────────────────────────────┐
│ handleAsk() / handleConsoleAsk()        │
├─────────────────────────────────────────┤
│ 1. Build tool-enabled task context      │
│                                         │
│ 2. companion.Service.Chat()             │
│    - Tool-call loop                     │
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
| Tool Calling | Optional | Via tool runner and registry |
| Compression Daemon | Yes | Background L0→L1→L2 summarization |

## Code References

| Component | Location |
|-----------|----------|
| Engine routing | `internal/agent/daemon/daemon.go:237` |
| Companion service creation | `internal/agent/daemon/daemon.go:259-301` |
| Message handlers | `internal/agent/daemon/handlers.go` |
| Companion service | `internal/companion/service.go` |
| LLMChatEngine | `internal/engine/llmchat_engine.go` |

## Troubleshooting

### "unsupported LLM provider" errors

**Cause:** Missing or misconfigured provider credentials for the selected model.

**Fix:** Ensure the provider API key is set and the model name matches the provider.

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
