# Companion Memory System

> Status: Fully implemented (L0/L1/L2 with compression daemon)

## Overview

The companion memory system provides progressive context decay for long-form conversational agents. Unlike code-focused short-term memory, it's optimized for:

- Short conversational turns (50-200 tokens typical)
- Time-based decay (today vivid, yesterday summarized, last week distilled)
- Relationship context (tone, topics, emotional continuity)

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Conversation Memory                       │
├─────────────────────────────────────────────────────────────┤
│  L0 (Vivid)     │ Today's turns, full content              │
│  24-48h window  │ Up to 100 turns, 8-12K tokens            │
├─────────────────────────────────────────────────────────────┤
│  L1 (Recent)    │ Day summaries, compressed                │
│  7-14 days      │ ~200 tokens/day summary                  │
├─────────────────────────────────────────────────────────────┤
│  L2 (History)   │ Distilled relationship context           │
│  Permanent      │ Topics, preferences, key moments         │
└─────────────────────────────────────────────────────────────┘
```

## Memory Modes

| Mode | Vivid Window | Token Budget | Use Case |
|------|--------------|--------------|----------|
| `standard` | 24h, 50 turns | 40K | Task-oriented companions |
| `roleplay` | 48h, 100 turns | 50K | Chat/roleplay companions |

## Usage

### Spawning a Chat Companion

```bash
# Quick spawn with chat defaults
agentctl agent spawn --name "Luna" --chat --system-prompt @prompt.txt

# Explicit configuration
agentctl agent spawn \
  --name "Luna" \
  --role companion \
  --exec-mode reactive \
  --max-iterations 3
```

### Running with Memory

```bash
# Auto-detects companion role, enables roleplay memory
agentctl agent run <agent-id>

# Explicit mode selection
agentctl agent run <agent-id> --companion-mode roleplay
```

### Conversation Continuity

```bash
# Use consistent conversation ID for memory continuity
agentctl agent ask <agent-id> \
  --question "Hi, my name is Sarah" \
  --conversation-id "sarah-session-1" \
  --wait

# Same conversation ID recalls previous context
agentctl agent ask <agent-id> \
  --question "What's my name?" \
  --conversation-id "sarah-session-1" \
  --wait
```

## Implementation Status

### ✅ Fully Implemented

- [x] L0 vivid turn storage and retrieval
- [x] Memory context injection into prompts
- [x] User/assistant turn storage
- [x] Standard vs roleplay memory configs
- [x] CLI flags (`--chat`, `--companion-mode`)
- [x] Conversation ID management
- [x] L1 day summaries with LLM summarizer
- [x] L2 distilled history with LLM summarizer
- [x] Background compression daemon (daily L0→L1, weekly L1→L2)
- [x] Token budget configuration per layer

### ⚠️ Future Enhancements

- [ ] Real token counting (tiktoken integration)
- [ ] Automatic user preference extraction from turns
- [ ] Semantic search across companion memories
- [ ] Memory export/import for backup

## Data Flow

```
User Message
    │
    ▼
┌─────────────────────────────────────────┐
│ handleAsk() / handleConsoleAsk()        │
├─────────────────────────────────────────┤
│ 1. Derive conversationID                │
│    - Explicit: --conversation-id        │
│    - Fallback: agentID:callerNS         │
│                                         │
│ 2. GetContext(conversationID)           │
│    - L2: Our History (if exists)        │
│    - L1: Recent Conversations (if any)  │
│    - L0: Today's Conversation           │
│                                         │
│ 3. Store user turn                      │
│                                         │
│ 4. Build prompt with memory             │
│    "## Conversation Memory              │
│     {context}                           │
│     ---                                 │
│     Question: {question}"               │
│                                         │
│ 5. LLMChatEngine Execute (reactive)     │
│    - Or LLMChatEngine tool loop (autonomous) │
│                                         │
│ 6. Store assistant turn (on success)    │
└─────────────────────────────────────────┘
```

## Context Format

The memory context is formatted as markdown sections:

```markdown
## Our History
We've been chatting for 3 weeks. User prefers concise answers.
We often discuss: hiking, travel, fitness
They prefer: short responses; direct answers

---

## Recent Conversations
**Monday, Jan 15** (12 messages)
Discussed hiking trails in the Pacific Northwest.
Topics: hiking, outdoor gear
Mood: enthusiastic

---

## Today's Conversation
[10:32 AM] Human: Good morning!
[10:32 AM] You: Good morning! How can I help?
[10:45 AM] Human: What trails did we discuss?
```

## Storage

| Table | Purpose |
|-------|---------|
| `companion_turns` | L0 raw turns with timestamps |
| `companion_day_summaries` | L1 compressed daily summaries |
| `companion_history` | L2 distilled long-term context |
| `companion_memory_state` | Compression cursor tracking |

Location: `~/.agentctl/storage/companion.db`

## Compression Schedule

The `CompressionDaemon` runs automatically when companion memory is enabled and an LLM summarizer is configured:

| Task | Interval | What it does |
|------|----------|--------------|
| Daily compression | Every 1 hour (checks) | Summarizes yesterday's L0 turns → L1 day summary |
| Weekly distillation | Every 6 hours (checks) | Distills old L1 summaries → L2 history |

**Throttling:**
- Daily compression: Max once per 12 hours
- Weekly distillation: Max once per 24 hours

## Future Enhancements

### Phase 1: Token Accuracy
- Integrate tiktoken for accurate token counting
- Per-provider token estimation

### Phase 2: Smart Extraction
- Automatic user preference extraction from conversation content
- Emotion/sentiment tracking over time

### Phase 3: Search & Integration
- Semantic search across companion memories
- Memory export/import for backup
- Cross-conversation insights

## Related

- [Memory System](./memory.md) - General agentctl memory
- [Sessions](./sessions.md) - Session tracking and learnings
- [Architecture](./architecture.md) - Overall system design
