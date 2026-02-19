# Companion Memory System

> Status: Implemented for v1 layered memory (L0/L1/L2 with compression daemon)

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

## Layered Budgeted Context Assembly (L2 -> L1 -> L0)

`ConversationMemory.GetContext()` assembles context in layered order with
explicit token budgets per layer:

1. `L2` distilled history (`companion_history`) for durable relationship context
2. `L1` day summaries (`companion_day_summaries`) for recent recall
3. `L0` vivid turns (`companion_turns`) for immediate conversational continuity

This keeps prompt construction deterministic and bounded while still preserving
temporal depth.

Implementation reference:
- `internal/companion/memory.go` (`GetContext`, `LayerBudget`)

## Temporal Pyramid (Cheap Derived Views)

To support dynamic context assembly without replaying all turns, companion
memory should expose progressively coarser time views:

| View | Source | Cost | Typical Use |
|------|--------|------|-------------|
| `hours` | L0 turns | Very low | "What just happened?" |
| `days` | L1 summaries | Low | Recent continuity / daily recap |
| `weeks` | L2 distilled history + grouped L1 | Low-medium | Ongoing topics and decisions |
| `months` | L2 distilled history snapshots | Low-medium | Long-term relationship memory |

This temporal pyramid is compatible with the v2 dynamic context builder:
start coarse (`months`/`weeks`), then drill down (`days`/`hours`) only when
needed.

V2 note: coarse-to-fine retrieval + drill-down refs (`expandable_dates`, turn
refs) are planned for `internal/v2/runtime/contextbuilder/*`. Current v1 path
still injects a bounded markdown context blob from `GetContext()`.

PR-17 note: v2 now has a libsql-first artifact semantic retrieval surface in
`internal/v2/adapters/libsql/turns` (`SearchArtifactsByEmbedding`) with a
deterministic cosine fallback when vector SQL is unavailable. The next step is
to blend those artifact-semantic hits into layered context assembly (alongside
temporal drill-down refs) rather than only using chronological context.

## Memory Modes

| Mode | Vivid Window | Token Budget | Use Case |
|------|--------------|--------------|----------|
| `standard` | 24h, 50 turns | 40K | Task-oriented companions |
| `roleplay` | 48h, 100 turns | 50K | Chat/roleplay companions |

## Usage

### Spawning a Chat Companion

```bash
# Quick spawn with chat defaults
agentctl agent spawn --name "Luna" --chat --prompt-file prompt.txt

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
│    - L2: Our History (weeks/months)     │
│    - L1: Recent Conversations (days)    │
│    - L0: Today's Conversation (hours)   │
│    - Apply per-layer + total budgets    │
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

### Dynamic Drill-Down (V2 Planned)

For PR-10 style dynamic context, retrieval should return drill-down metadata
(`expandable_dates`, turn refs) so the model can move from day summaries into
specific turn slices only when required, rather than injecting the full memory
blob every turn. This is roadmap behavior for v2 and is not enabled by default
in the current v1 runtime path.

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
