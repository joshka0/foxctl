# agentctl as AI Agent Harness - Demo Workflow

This document demonstrates how agentctl serves as a coordination harness for Claude and Gemini.

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                     AGENTCTL HARNESS                         │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌─────────────┐    Mailbox    ┌─────────────┐             │
│  │ Claude Code │◄────────────►│  Gemini CLI │             │
│  │  (primary)  │    (async)    │  (advisor)  │             │
│  └──────┬──────┘               └──────┬──────┘             │
│         │                             │                     │
│         ▼                             ▼                     │
│  ┌──────────────────────────────────────────┐              │
│  │          agentctl CLI                     │              │
│  │  • Skills (35+): code analysis, fs, test │              │
│  │  • Todo: task tracking with traceability │              │
│  │  • Mailbox: inter-agent messaging        │              │
│  │  • Memory: auto-cache + named memories   │              │
│  └──────────────────────────────────────────┘              │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

## Demo: Code Review Workflow

### Step 1: Create a Task

```bash
# Claude creates a task to track the work
agentctl todo add \
  --title "Review daemon complexity" \
  --description "Analyze daemon.go for refactoring opportunities"
```

Output:
```json
{
  "task": {
    "id": "01KD2S51X9...",
    "title": "Review daemon complexity",
    "status": "pending"
  }
}
```

### Step 2: Gather Structured Context (agentctl)

```bash
# Get complexity hotspots (fast: ~50ms)
agentctl run code/complexity --input '{
  "path": "internal/agent/daemon",
  "threshold": 10
}'
```

Output:
```json
{
  "results": [
    {
      "function": "Run",
      "file": "internal/agent/daemon/daemon.go",
      "cyclomatic_complexity": 43,
      "risk_level": "high",
      "recommendations": [
        "Consider breaking this function into smaller, focused functions",
        "Function is 229 lines long - consider splitting"
      ]
    }
  ]
}
```

### Step 3: Log Query to Mailbox (for traceability)

```bash
# Log the question we're about to ask
agentctl mailbox send gemini-queries \
  --from claude-agent \
  --type agent.ask \
  --payload '{
    "query": "How should I refactor daemon.Run?",
    "context": {
      "file": "internal/agent/daemon/daemon.go",
      "complexity": 43,
      "lines": 229
    }
  }'
```

### Step 4: Get Second Opinion (Gemini)

```bash
# Send structured context to Gemini
echo "Code complexity analysis:
- daemon.Run: cyclomatic 43, cognitive 79, 229 lines (HIGH RISK)
- createAgent: cyclomatic 10, 58 lines (MEDIUM)
- handleAsk: cyclomatic 10, 73 lines (MEDIUM)

Recommendations from static analysis:
1. Break Run into smaller functions
2. Extract complex conditions into helpers
3. Flatten nesting structure" | gemini -p "What refactoring approach would you recommend?"
```

### Step 5: Log Gemini's Response

```bash
agentctl mailbox send claude-queries \
  --from gemini-agent \
  --type agent.reply \
  --payload '{"response": "..."}'
```

### Step 6: Complete Task

```bash
agentctl todo complete \
  --id 01KD2S51X9... \
  --notes "Identified daemon.Run as primary refactoring target. Gemini suggests state machine pattern."
```

## Key Integration Points

### 1. Skills for Structured Analysis (Fast Path)

| Skill | Purpose | Latency |
|-------|---------|---------|
| `code/complexity` | Find complexity hotspots | ~50ms |
| `code/symbols` | Extract code structure | ~100ms |
| `code/snippet_extract` | Smart code search | ~200ms |
| `test/run` | Run tests with coverage | varies |

Usage from Claude Code:
```bash
# Invoke via Skill tool
/agentctl-complexity

# Or directly via Bash
agentctl run code/complexity --input '{"path": "."}'
```

### 2. Mailbox for Communication Logging

| Command | Purpose |
|---------|---------|
| `mailbox send <ns>` | Send message to agent |
| `mailbox poll <ns>` | Poll for messages (blocking) |
| `mailbox list <ns>` | List messages |
| `mailbox ack <id>` | Acknowledge/delete message |

Example: Query tracing
```bash
# Before asking Gemini
agentctl mailbox send gemini-queries --from claude --payload '{"query": "..."}'

# After getting response
agentctl mailbox send claude-queries --from gemini --payload '{"response": "..."}'
```

### 3. Task Tracking for Traceability

| Command | Purpose |
|---------|---------|
| `todo add --title "..."` | Create task |
| `todo list` | Show all tasks |
| `todo active` | Get current task |
| `todo complete --id <id>` | Mark done |

### 4. Memory for Context Persistence

| Command | Purpose |
|---------|---------|
| `memory put <name>` | Save named memory |
| `memory get <name>` | Retrieve memory |
| `memory search <query>` | Semantic search |

## Integration Patterns

### Pattern A: Direct Skill Invocation (Fastest)
```
Claude → agentctl skill → structured result
```
- Use for: Code analysis, file operations, tests
- Latency: 50-500ms
- Best for: Frequent operations during development

### Pattern B: Gemini Consultation (Async)
```
Claude → agentctl context → Gemini → response
```
- Use for: Second opinions, alternative perspectives
- Latency: 60-120 seconds (Gemini MCP startup)
- Best for: Architecture decisions, complex refactoring

### Pattern C: Multi-Agent Coordination (Future)
```
Claude → mailbox → Daemon Agent → mailbox → Claude
```
- Use for: Long-running background tasks
- Latency: Varies by task
- Best for: CI monitoring, automated reviews

## Best Practices

1. **Always create a task first** - Enables traceability via hooks
2. **Use agentctl skills for structured data** - Faster than Gemini's file reads
3. **Log important queries to mailbox** - Creates audit trail
4. **Cache expensive operations** - agentctl auto-caches by default
5. **Complete tasks with notes** - Captures learnings

## Current Limitations

1. **Gemini startup time**: ~60s for MCP initialization
2. **No real-time streaming**: Mailbox is poll-based, not push
3. **Single-machine**: No distributed agent coordination yet

## Verification

Test that the integration works:

```bash
# 1. Create task
agentctl todo add --title "Integration test"

# 2. Run skill
agentctl run code/complexity --input '{"path": ".", "threshold": 20}'

# 3. Check mailbox
agentctl mailbox list test-agent

# 4. Complete task
agentctl todo list
```

All commands should return valid JSON envelopes with `"status": "ok"`.
