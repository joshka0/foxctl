# Context Persistence Architecture

This document describes how to survive context compaction and maintain continuity across Claude Code sessions using agentctl.

## The Problem

1. **Context rot**: As conversations grow, early context gets summarized away
2. **Compaction loss**: When context is compacted, detailed implementation decisions are lost
3. **Cross-session amnesia**: Starting a new session loses all prior work context
4. **Multi-agent coordination**: Claude and Gemini need shared context

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                     CONTEXT PERSISTENCE LAYER                                │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐                     │
│  │ Claude Code │    │ Claude CLI  │    │  Gemini CLI │                     │
│  │  (Opus 4.5) │    │ (Subagents) │    │ (Deep work) │                     │
│  │  PRIMARY    │    │    FAST     │    │    ASYNC    │                     │
│  └──────┬──────┘    └──────┬──────┘    └──────┬──────┘                     │
│         │                  │                  │                             │
│         └─────────┬────────┴──────────────────┘                             │
│                   ▼                                                         │
│  ┌──────────────────────────────────────────────────────────────┐          │
│  │                    agentctl HARNESS                           │          │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐        │          │
│  │  │   Memory     │  │    Tasks     │  │   Mailbox    │        │          │
│  │  │  (persist)   │  │  (tracking)  │  │  (comms)     │        │          │
│  │  └──────────────┘  └──────────────┘  └──────────────┘        │          │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐        │          │
│  │  │  Skills      │  │  Blackboard  │  │   Daemon     │        │          │
│  │  │  (35+ tools) │  │  (shared)    │  │  (agents)    │        │          │
│  │  └──────────────┘  └──────────────┘  └──────────────┘        │          │
│  └──────────────────────────────────────────────────────────────┘          │
│                   │                                                         │
│                   ▼                                                         │
│  ┌──────────────────────────────────────────────────────────────┐          │
│  │                 PERSISTENT STORAGE                            │          │
│  │  • SQLite (tasks, agents, memory, mailbox)                   │          │
│  │  • CAS (artifacts, code snapshots, large outputs)            │          │
│  │  • ~/.agentctl/storage/                                       │          │
│  └──────────────────────────────────────────────────────────────┘          │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Solution Components

### 1. Session Memory System

**Purpose**: Persist key context that should survive compaction.

```bash
# Save important context before compaction
agentctl memory put session-context --payload '{
  "session_id": "2025-12-22-daemon-tests",
  "key_decisions": [
    "daemon.Run has complexity 43 - needs refactoring",
    "FakeLLM uses atomic.Int32 to avoid reflection races",
    "defer bug: wrap Close() in anonymous function"
  ],
  "completed_tasks": ["Milestone H verification"],
  "pending_work": ["Multi-agent integration"],
  "important_files": [
    "internal/agent/daemon/daemon.go",
    "internal/agent/daemon/fake_llm_test.go"
  ]
}'

# Retrieve at session start
agentctl memory get session-context
```

**Auto-save hook** (add to `.claude/hooks/session-save.sh`):
```bash
#!/bin/bash
# Called before compaction via UserPromptSubmit with specific triggers
# Saves current session state to agentctl memory
```

### 2. Task Persistence

**Purpose**: Track all work across sessions with agentctl todo.

```bash
# At session start, check pending tasks
agentctl todo list --status pending

# When starting work
agentctl todo add --title "Implement feature X" --description "Context..."

# During work - update progress
agentctl todo update --id <id> --notes "Progress: completed step 1"

# At completion
agentctl todo complete --id <id> --notes "Final notes..."
```

**Benefits**:
- Tasks persist across sessions
- Each task captures context in description/notes
- Provides continuity for "where were we?"

### 3. Pre-Compaction Checkpoint

**New hook**: `hooks/pre_compaction_checkpoint`

When Claude Code signals compaction is imminent:
1. Save current todo status to memory
2. Summarize key decisions made
3. List files modified in session
4. Store in named memory: `pre-compaction-<timestamp>`

### 4. Session Restore Protocol

At the start of each session:

```bash
# Check for recent session memories
agentctl memory search "session"

# Get last session context
agentctl memory get pre-compaction-latest

# Show pending tasks
agentctl todo list --status pending

# Check mailbox for any pending messages
agentctl mailbox list claude-agent
```

## LLM Configuration for Daemon

### Setup

Create `~/.agentctl/config.yaml`:

```yaml
llm:
  provider: openrouter  # or "gemini", "openai", "anthropic", "groq"
  model: anthropic/claude-haiku-4-5
  # API key via environment variable: AGENTCTL_LLM_API_KEY

storage:
  root: ~/.agentctl/storage

paths:
  cas: ~/.agentctl/cas
  jobs: ~/.agentctl/jobs
```

### Environment Variables

```bash
# Add to ~/.zshrc or session

# Option 1: OpenRouter (access to multiple models with one key)
export AGENTCTL_LLM_PROVIDER=openrouter
export AGENTCTL_LLM_MODEL=anthropic/claude-haiku-4-5
export AGENTCTL_LLM_API_KEY=<your-openrouter-key>

# Option 2: Direct Anthropic API
export AGENTCTL_LLM_PROVIDER=anthropic
export AGENTCTL_LLM_MODEL=claude-haiku-4-5
export AGENTCTL_LLM_API_KEY=<your-anthropic-key>

# Option 3: Gemini
export AGENTCTL_LLM_PROVIDER=gemini
export AGENTCTL_LLM_MODEL=gemini-2.0-flash
export AGENTCTL_LLM_API_KEY=<your-google-api-key>

# Option 4: GROQ (fast inference)
export AGENTCTL_LLM_PROVIDER=groq
export AGENTCTL_LLM_MODEL=llama-3.1-70b-versatile
export AGENTCTL_LLM_API_KEY=<your-groq-key>
```

## Multi-Model Strategy

### Model Selection by Task Type

| Task Type | Model | Reason |
|-----------|-------|--------|
| Quick analysis | agentctl skills | <100ms, structured |
| Code generation | Claude Opus 4.5 (current) | Best quality |
| Second opinion | Gemini 2.5 Pro | Different perspective |
| Background daemon | Gemini 2.5 Flash | Cost-effective |
| Deep research | Gemini 2.5 Pro | Large context |

### Integration Pattern

```bash
# Pattern 1: agentctl for structured data (fastest)
result=$(agentctl run code/complexity --input '{"path": "."}')

# Pattern 2: Gemini for async deep work
echo "Complex question..." | gemini -p "Analyze thoroughly"

# Pattern 3: Task tool for Claude subagents (via Claude Code)
# Use Task tool with subagent_type for parallel work

# Pattern 4: Daemon agents for background work
agentctl agent spawn --role analyzer --skills "code/complexity,code/symbols"
agentctl agent run <agent-id> &
```

## Pre-warm Gemini Worker

### Problem
Gemini CLI has ~60s startup time due to MCP initialization.

### Solution
Background worker that keeps Gemini warm and accepts requests via mailbox.

**Worker script** (`~/.agentctl/workers/gemini-worker.sh`):
```bash
#!/bin/bash
# Gemini pre-warm worker
# Polls mailbox and processes requests through warm Gemini instance

WORKER_NS="gemini-worker"

while true; do
  # Poll for messages (5s timeout)
  msg=$(agentctl mailbox poll $WORKER_NS --timeout 5 --max 1 2>/dev/null)

  if [[ $(echo "$msg" | jq -r '.data.count') -gt 0 ]]; then
    # Extract payload
    query=$(echo "$msg" | jq -r '.data.messages[0].payload.query')
    msg_id=$(echo "$msg" | jq -r '.data.messages[0].id')
    reply_to=$(echo "$msg" | jq -r '.data.messages[0].from_ns')

    # Process with Gemini (already warm)
    response=$(echo "$query" | gemini -p "Answer concisely:")

    # Send reply
    agentctl mailbox send "$reply_to" \
      --from $WORKER_NS \
      --type agent.reply \
      --payload "{\"response\": $(echo "$response" | jq -Rs .)}"

    # Ack original message
    agentctl mailbox ack "$msg_id"
  fi
done
```

**Start worker**:
```bash
# Start in background
nohup ~/.agentctl/workers/gemini-worker.sh &

# Or via launchd/systemd for persistence
```

**Use from Claude**:
```bash
# Send request to warm worker
agentctl mailbox send gemini-worker \
  --from claude-agent \
  --type agent.ask \
  --payload '{"query": "What refactoring approach for high complexity functions?"}'

# Poll for response (worker responds in ~2s instead of 60s)
agentctl mailbox poll claude-agent --timeout 10
```

## Compaction Survival Checklist

### Before Compaction (manual or hook-triggered)

1. **Save session summary**:
   ```bash
   agentctl memory put session-$(date +%Y%m%d) --payload '{...}'
   ```

2. **Complete or annotate tasks**:
   ```bash
   agentctl todo list | jq '.data.tasks[] | select(.status=="in_progress")'
   # Update each with current status
   ```

3. **Log key decisions to blackboard**:
   ```bash
   agentctl bb post decisions --payload '{"decision": "...", "rationale": "..."}'
   ```

### After Compaction (session start)

1. **Load session context**:
   ```bash
   agentctl memory get session-latest
   ```

2. **Review pending tasks**:
   ```bash
   agentctl todo list --status pending
   ```

3. **Check for messages**:
   ```bash
   agentctl mailbox list claude-agent
   ```

## Implementation Priority

1. **Immediate** (this session):
   - Create session-save command
   - Set up LLM API key
   - Test daemon with real LLM

2. **Near-term** (next session):
   - Implement pre-warm Gemini worker
   - Create session restore protocol
   - Add pre-compaction hook

3. **Future**:
   - Auto-summarize session on compaction signal
   - Embedding-based memory search
   - Cross-session task inheritance
