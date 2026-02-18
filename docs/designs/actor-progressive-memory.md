# Actor Progressive Memory

> Design document for progressive context distillation in reactive actors

## Contracts

These invariants MUST be maintained by any implementation:

### Durability Contract

- **Raw turns are durable**: Turns are persisted to `sessions.db` before any
  processing
- **Compaction is cursor-based**: A cursor tracks `next_turn_to_summarize`;
  compaction is resumable
- **Never delete before persist**: Raw turns remain until summary is durably
  written
- **Crash-safe compaction**: Summaries are inserted in a single transaction that
  advances the cursor only after a successful insert; failures leave state
  unchanged and retries produce the same result
- **Monotonic summary indexing**: `summary_index` increases monotonically per
  actor to avoid UNIQUE conflicts and ensure deterministic distillation order
- **Snapshot summarization**: Summarization is done outside the global lock; DB
  work uses short transactions to avoid blocking reads/writes

### Secret Safety Contract

- **Redact before persistence**: Summaries, learnings, and L1/L2 artifacts are
  redacted
- **Pattern-based redaction**: API keys, tokens, authorization headers, secrets
- **Raw turns may contain secrets**: Only archive redacted versions; full turns
  in CAS with TTL

### Token Estimation Contract

- **Simple estimator for MVP**: `len(text)/4` + safety margin
- **Provider-agnostic**: No tiktoken dependency; works across Gemini, Claude,
  OpenAI
- **Target 80% of budget**: Leave headroom for estimation error

## Overview

This document describes the progressive memory system for long-running reactive
actors. The system maintains a compact, relevant context window by progressively
summarizing and distilling conversation history while preserving critical
information.

## Problem Statement

Long-running actors face a fundamental tension:

- **Need memory:** Actors must remember past interactions to be useful
- **Limited context:** LLM context windows are finite (32K-128K tokens)
- **Noise accumulates:** Raw conversation history contains redundancy and
  tangents

### Without Progressive Memory

```
Turn 1: [context: 2K]
Turn 10: [context: 15K]
Turn 30: [context: 45K] ← Context overflow!
Turn 31: ???
```

Options when context overflows:

1. **Truncate:** Lose early context entirely
2. **Naive sliding window:** Lose important decisions/context
3. **Stop:** Require human intervention

All options degrade actor performance.

## Solution: Progressive Distillation

Continuously summarize and distill conversation history, pruning noise while
preserving signal. The actor always has a compact, relevant view of history.

```
ACTOR CONTEXT WINDOW

┌────────────────────────────────────────────────────────────────┐
│ SYSTEM PROMPT + ROLE                                    (~2K)  │
├────────────────────────────────────────────────────────────────┤
│ RETRIEVED CONTEXT (semantic search)                     (~6K)  │
│  - Relevant memories from vector search                        │
│  - Related past sessions                                       │
│  - Graph neighbors (related files/tasks)                       │
├────────────────────────────────────────────────────────────────┤
│ SHORT-TERM MEMORY                                      (~18K)  │
│                                                                │
│  ┌──────────────────────────────────────────────────────────┐ │
│  │ DISTILLED SUMMARY (L2)                           (~4K)   │ │
│  │  Compressed history of older conversation                │ │
│  └──────────────────────────────────────────────────────────┘ │
│                         ▲                                      │
│                         │ distill                              │
│  ┌──────────────────────────────────────────────────────────┐ │
│  │ RECENT SUMMARY (L1)                              (~6K)   │ │
│  │  Summaries of recent turn batches                        │ │
│  └──────────────────────────────────────────────────────────┘ │
│                         ▲                                      │
│                         │ summarize                            │
│  ┌──────────────────────────────────────────────────────────┐ │
│  │ RAW TURNS (L0)                                   (~8K)   │ │
│  │  Last N turns at full fidelity                           │ │
│  └──────────────────────────────────────────────────────────┘ │
│                                                                │
├────────────────────────────────────────────────────────────────┤
│ CURRENT MESSAGE + SCRATCHPAD                            (~6K)  │
└────────────────────────────────────────────────────────────────┘
```

## Memory Levels

### L0: Raw Turns

The most recent turns at full fidelity. No summarization, no loss.

**Contents:**

- Complete user messages
- Complete assistant responses
- Full tool call details (input/output)
- Exact timestamps

**Purpose:** Immediate context for current work. The actor needs full detail for
recent interactions to maintain coherence.

**Lifecycle:** When buffer fills, oldest turns are summarized into L1.

### L1: Recent Summaries

Summarized batches of raw turns. Preserves key information, removes noise.

**Contents:**

- What was accomplished in each batch
- Key decisions and their rationale
- Important discoveries
- State transitions

**Purpose:** Bridge between raw turns and compressed history. Provides
medium-term context without full verbosity.

**Lifecycle:** When buffer fills, summaries are distilled into L2.

### L2: Distilled Summary

Highly compressed session history. The essential trajectory of the conversation.

**Contents:**

- Overall session trajectory
- Major milestones and completions
- Critical decisions affecting future work
- Accumulated learnings/gotchas
- Current state summary

**Purpose:** Long-term memory. Allows actor to understand "how we got here" even
after hundreds of turns.

**Lifecycle:** Oldest distilled entries are re-distilled or archived when L2
exceeds budget.

## Distillation Pipeline

```
Every turn:
┌─────────────────────────────────────────────────────────────┐
│                    RAW TURN BUFFER (L0)                      │
│                                                              │
│  [T1] [T2] [T3] [T4] [T5] ← newest                          │
│                                                              │
│  When buffer reaches threshold:                              │
│    1. Summarize batch → append to L1                        │
│    2. Archive raw turns to sessions.db                      │
│    3. Clear buffer                                          │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼ summarize
┌─────────────────────────────────────────────────────────────┐
│                  RECENT SUMMARY (L1)                         │
│                                                              │
│  [Summary A] [Summary B] [Summary C] ← newest               │
│                                                              │
│  When L1 reaches threshold:                                  │
│    1. Distill summaries → append to L2                      │
│    2. Keep only most recent summary in L1                   │
│    3. Clear older summaries                                 │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼ distill
┌─────────────────────────────────────────────────────────────┐
│                 DISTILLED SUMMARY (L2)                       │
│                                                              │
│  [Distilled 1] [Distilled 2] ... ← newest                   │
│                                                              │
│  When L2 exceeds budget:                                     │
│    1. Re-distill oldest entries together                    │
│    2. Archive to sessions.db                                │
│    3. Prune to fit token budget                             │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼ archive
┌─────────────────────────────────────────────────────────────┐
│                   LONG-TERM ARCHIVE                          │
│                    (sessions.db)                             │
│                                                              │
│  - Full turn history (searchable via embeddings)            │
│  - Session summaries (retrievable)                          │
│  - Available for semantic retrieval                         │
└─────────────────────────────────────────────────────────────┘
```

## Configuration

All thresholds are configurable per-actor or globally:

```go
type MemoryConfig struct {
    // L0 Configuration
    RawBufferSize     int     // Turns before L0→L1 summarization
    RawTokenBudget    int     // Max tokens for L0

    // L1 Configuration
    RecentSummarySize int     // Summaries before L1→L2 distillation
    L1TokenBudget     int     // Max tokens for L1

    // L2 Configuration
    L2TokenBudget     int     // Max tokens for L2

    // Total budget
    TotalTokenBudget  int     // Total short-term memory budget

    // Summarization
    SummarizerModel   string  // LLM for summarization (e.g., "haiku", "gemini-flash")
}
```

**Default values (tunable):**

| Parameter           | Default       | Notes                                                               |
| ------------------- | ------------- | ------------------------------------------------------------------- |
| `RawBufferSize`     | 5-10 turns    | Balance: more = better context, fewer = more frequent summarization |
| `RecentSummarySize` | 2-3 summaries | How many batch summaries before distillation                        |
| `RawTokenBudget`    | 8K tokens     | Space for raw turns                                                 |
| `L1TokenBudget`     | 6K tokens     | Space for recent summaries                                          |
| `L2TokenBudget`     | 4K tokens     | Space for distilled history                                         |
| `SummarizerModel`   | gemini-flash  | Fast, cheap model for summarization                                 |

These values should be tuned based on:

- Task complexity (complex tasks need more raw context)
- Conversation style (verbose vs concise)
- Model context window size
- Cost/latency tradeoffs

## Summarization Prompts

### L0 → L1: Summarize Turns

```
You are summarizing a batch of conversation turns for an AI agent's memory.

TASK CONTEXT: {{.TaskContext}}

TURNS TO SUMMARIZE:
{{range .Turns}}
[{{.Role}}] {{.Content}}
{{end}}

Create a concise summary that captures:
1. What was accomplished
2. Key decisions made and their rationale
3. Important information discovered
4. Current state/progress
5. Any blockers or open questions

IMPORTANT - PRUNE THE FOLLOWING:
- Off-topic tangents or distractions
- Verbose explanations that can be condensed
- Redundant information (said multiple ways)
- Failed attempts (unless the failure taught something)
- Back-and-forth clarifications (keep only the resolution)

IMPORTANT - PRESERVE THE FOLLOWING:
- Exact file paths, function names, error messages
- Technical decisions and why they were made
- Gotchas or learnings worth remembering
- Current state and next steps

Output a focused summary in 2-4 paragraphs.
```

### L1 → L2: Distill Summaries

```
You are distilling multiple conversation summaries into compressed session history.

TASK CONTEXT: {{.TaskContext}}

SUMMARIES TO DISTILL:
{{range .Summaries}}
---
[Turns {{.TurnRange.Start}}-{{.TurnRange.End}}]
{{.Content}}
{{end}}

Create a highly compressed history that captures:
1. The overall trajectory of the session
2. Major milestones and completions
3. Critical decisions that affect future work
4. Accumulated knowledge/gotchas
5. Current state summary

AGGRESSIVE PRUNING:
- Remove anything tried and abandoned (unless it taught something)
- Remove debugging back-and-forth (keep only: "fixed X by doing Y")
- Remove explanations the agent already knows
- Collapse "after N attempts, solved by X"
- Remove emotional/social content

MUST PRESERVE:
- Decisions and their rationale
- Learnings that prevent future mistakes
- Current state and blockers
- File/function/variable names that matter

Output 3-5 bullet points or 2-3 short paragraphs.
```

## Relevance Filtering

Optional step during summarization to score and filter content:

```
Given the current task, score each piece of information (0-10):

CURRENT TASK: {{.CurrentTask}}
CURRENT FOCUS: {{.CurrentFocus}}

ITEMS TO SCORE:
{{range $i, $item := .Items}}
{{$i}}. {{$item}}
{{end}}

For each item:
- Score (0-10): Relevance to current task
- Keep: true/false
- Reason: One sentence

Items scoring <5 will be pruned.
```

## Implementation

### Core Types

```go
type ShortTermMemory struct {
    config     MemoryConfig
    db         *sql.DB                  // Durable state in sessions.db
    summarizer Summarizer               // LLM for summarization
    redactor   *SecretRedactor          // Redact before persistence
    mu         sync.RWMutex
}

// MemoryState is loaded from DB, not held in memory long-term
type MemoryState struct {
    ActorID          string
    SessionID        string      // Link to session lineage
    TaskContext      string      // Current task description

    // Cursors (durable, drive compaction)
    NextTurnToSummarize  int     // Cursor for L0→L1
    NextSummaryToDistill int     // Cursor for L1→L2

    // L1/L2 artifact references (CAS)
    L1ArtifactID     string      // CAS reference to recent summaries
    L2ArtifactID     string      // CAS reference to distilled summary

    // Metadata
    TotalTurns       int
    TokenEstimate    int
    LastSummarizeAt  time.Time
    LastDistillAt    time.Time
    UpdatedAt        time.Time
}

// Schema for durable cursor storage
const actorMemoryStateSchema = `
CREATE TABLE IF NOT EXISTS actor_memory_state (
    actor_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    task_context TEXT,
    next_turn_to_summarize INTEGER DEFAULT 0,
    next_summary_to_distill INTEGER DEFAULT 0,
    l1_artifact_id TEXT,
    l2_artifact_id TEXT,
    total_turns INTEGER DEFAULT 0,
    token_estimate INTEGER DEFAULT 0,
    last_summarize_at TIMESTAMP,
    last_distill_at TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (session_id) REFERENCES sessions(id)
);
`

type Turn struct {
    Index      int
    Role       string          // user, assistant, tool
    Content    string
    ToolCalls  []ToolCall
    Timestamp  time.Time
    TokenCount int
}

type Summary struct {
    TurnRange  TurnRange       // [start, end]
    Content    string
    KeyPoints  []string
    Decisions  []string
    TokenCount int
    CreatedAt  time.Time
}

type TurnRange struct {
    Start int
    End   int
}
```

### Key Methods

```go
// AppendTurn adds a new turn and triggers distillation if needed
func (m *ShortTermMemory) AppendTurn(ctx context.Context, actorID string, turn Turn) error

// GetContext returns formatted short-term memory for LLM
func (m *ShortTermMemory) GetContext(actorID string) string

// SetTaskContext updates the current task (affects summarization relevance)
func (m *ShortTermMemory) SetTaskContext(actorID string, task string)

// Clear resets memory state (e.g., on task completion)
func (m *ShortTermMemory) Clear(actorID string)

// Export returns full memory state for debugging/inspection
func (m *ShortTermMemory) Export(actorID string) *MemoryState
```

### Summarizer Interface

```go
type Summarizer interface {
    // SummarizeTurns creates a summary from raw turns
    SummarizeTurns(ctx context.Context, task string, turns []Turn) (*Summary, error)

    // DistillSummaries compresses multiple summaries into one
    DistillSummaries(ctx context.Context, task string, summaries []Summary) (string, error)

    // FilterByRelevance scores and filters items by relevance
    FilterByRelevance(ctx context.Context, task string, items []string) ([]string, error)
}

// SecretRedactor removes sensitive information before persistence
type SecretRedactor struct {
    patterns []*regexp.Regexp
}

// Default redaction patterns (Go regex)
var defaultRedactPatterns = []string{
    `(?i)authorization:\s*\S+`,                    // Authorization headers
    `(?i)(api[_-]?key|secret|token|password)\s*[:=]\s*["']?[^\s"']+`, // Key-value pairs
    `ghp_[a-zA-Z0-9]{36}`,                         // GitHub PAT
    `github_pat_[a-zA-Z0-9_]{22,}`,                // GitHub fine-grained PAT
    `sk-[a-zA-Z0-9]{48}`,                          // OpenAI API key
    `sk-proj-[a-zA-Z0-9-_]{20,}`,                  // OpenAI project key
    `AIza[a-zA-Z0-9_-]{35}`,                       // Google API key
    `AKIA[A-Z0-9]{16}`,                            // AWS access key
    `-----BEGIN (RSA |EC |DSA |OPENSSH )?PRIVATE KEY-----`, // Private keys
    `xox[baprs]-[a-zA-Z0-9-]+`,                    // Slack tokens
}

func NewSecretRedactor() *SecretRedactor {
    patterns := make([]*regexp.Regexp, 0, len(defaultRedactPatterns))
    for _, p := range defaultRedactPatterns {
        if re, err := regexp.Compile(p); err == nil {
            patterns = append(patterns, re)
        }
    }
    return &SecretRedactor{patterns: patterns}
}

func (r *SecretRedactor) Redact(text string) string {
    result := text
    for _, re := range r.patterns {
        result = re.ReplaceAllString(result, "[REDACTED]")
    }
    return result
}

// Token estimation (simple, provider-agnostic)
func EstimateTokens(text string) int {
    // Rough estimate: 1 token ≈ 4 characters
    // Target 80% to leave headroom for estimation error
    return len(text) / 4
}

func FitsInBudget(text string, budget int) bool {
    estimated := EstimateTokens(text)
    // Use 80% of budget as safety margin
    return estimated <= int(float64(budget)*0.8)
}
```

## Token Budget Allocation

Example for 32K context window:

```
Component              Tokens    %     Notes
─────────────────────────────────────────────────────────────
System Prompt          2,000    6%    Role + capabilities
Retrieved Context      6,000   19%    Semantic search results
─────────────────────────────────────────────────────────────
Short-Term Memory     18,000   56%    ← Progressive distillation
  ├─ L2 (Distilled)    4,000          Compressed session history
  ├─ L1 (Recent)       6,000          Last 2-3 batch summaries
  └─ L0 (Raw)          8,000          Last N raw turns
─────────────────────────────────────────────────────────────
Current + Scratchpad   6,000   19%    Message + tool calls
═════════════════════════════════════════════════════════════
TOTAL                 32,000  100%
```

## Example Timeline

Assuming `RawBufferSize=5` and `RecentSummarySize=3`:

```
Turn 1-5:   [Raw in L0: T1, T2, T3, T4, T5]
Turn 6:     Summarize T1-T5 → Summary A in L1
            Archive T1-T5 to sessions.db
            L0 now: [T6]

Turn 7-10:  [Raw in L0: T6, T7, T8, T9, T10]
Turn 11:    Summarize T6-T10 → Summary B in L1
            L1 now: [Summary A, Summary B]
            L0 now: [T11]

Turn 12-15: [Raw in L0: T11, T12, T13, T14, T15]
Turn 16:    Summarize T11-T15 → Summary C in L1
            L1 now: [Summary A, Summary B, Summary C]
            Distill A+B+C → Distilled 1 in L2
            L1 now: [Summary C]  (keep most recent)
            L0 now: [T16]

...continues...
```

At turn 50, context might look like:

```
L2: Distilled history of turns 1-45 (~4K tokens)
    "Session started with auth refactor. Completed JWT validation
     and refresh token rotation. Key decision: RS256 over HS256
     for asymmetric verification. Debugged clock skew issue..."

L1: Summary of turns 46-50 (~2K tokens)
    "Working on token expiry edge case. Found race condition
     in concurrent refresh. Proposed solution: mutex + retry..."

L0: Raw turns 51-55 (~8K tokens)
    [Full verbatim conversation with all details]
```

## Integration with Actor System

```go
type DspyActor struct {
    // ...existing fields...

    shortTermMemory *ShortTermMemory
    memoryManager   *MemoryManager      // For retrieved context
}

func (a *DspyActor) OnMailReceived(ctx context.Context, msg *mailbox.Message) error {
    // 1. Build context with current memory state
    context := a.buildContext(ctx, msg)

    // 2. Process message with dspy-go
    response, err := a.agent.Run(ctx, context, msg.Payload)
    if err != nil {
        return err
    }

    // 3. Append turns to short-term memory
    a.shortTermMemory.AppendTurn(ctx, a.id, Turn{
        Role:    "user",
        Content: string(msg.Payload),
    })

    a.shortTermMemory.AppendTurn(ctx, a.id, Turn{
        Role:      "assistant",
        Content:   response.Content,
        ToolCalls: response.ToolCalls,
    })

    return nil
}

func (a *DspyActor) buildContext(ctx context.Context, msg *mailbox.Message) *LLMContext {
    return &LLMContext{
        SystemPrompt:     a.systemPrompt,
        RetrievedContext: a.memoryManager.RetrieveRelevant(ctx, msg),
        ShortTermMemory:  a.shortTermMemory.GetContext(a.id),
        CurrentMessage:   string(msg.Payload),
    }
}
```

## Storage Integration

| Store         | Role in Progressive Memory                        |
| ------------- | ------------------------------------------------- |
| `sessions.db` | Archive for raw turns after summarization         |
| `memory.db`   | Long-term learnings extracted during distillation |
| Embeddings    | Enable semantic retrieval of archived content     |

When summarizing, the system can optionally:

1. Extract "learnings" and save to `memory.db`
2. Update task graph with discovered relationships
3. Generate embeddings for archived content

## Metrics and Observability

Track these metrics for tuning:

```go
type MemoryMetrics struct {
    TotalTurns           int
    SummarizationCount   int
    DistillationCount    int
    TokensBeforeSummary  int
    TokensAfterSummary   int
    CompressionRatio     float64
    AverageLatencyMs     int64
}
```

## File Structure

```
internal/actor/memory/
├── shortterm.go        # ShortTermMemory implementation
├── summarizer.go       # LLM-based summarization
├── prompts.go          # Summarization/distillation prompts
├── tokens.go           # Token estimation (len/4)
├── redactor.go         # Secret redaction
├── config.go           # MemoryConfig defaults
└── shortterm_test.go   # Tests
```

## Design Decisions

These questions have been resolved:

### 1. Async summarization

**Decision:** Async with cursor-based checkpointing.

- Summarization runs in background goroutine
- Cursor only advances after summary is durably written
- Actor continues with stale L1/L2 during summarization (safe: L0 is current)
- If crash during summarization: cursor unchanged, retry from same point

### 2. Learning extraction

**Decision:** Manual extraction initially; auto-extraction is future work.

- MVP: Don't auto-extract to avoid noise
- Summaries persisted to CAS; humans/agents can review later
- Future: Add relevance scoring to filter learnings (requires tuning)

### 3. Task boundaries

**Decision:** Archive and summarize on task completion.

- Task completion triggers final summarization
- Session summary persisted to `sessions.db`
- Next task starts with clean L0, but can retrieve past summaries via semantic
  search
- L2 carries forward if continuing same session

### 4. Multi-model

**Decision:** Single model for MVP; configurable later.

- Use fast/cheap model (gemini-flash) for all summarization
- L1→L2 distillation is not significantly more complex
- Future: Allow per-level model configuration if quality issues arise

### 5. Tuning thresholds

**Decision:** Static defaults for MVP; DSPy optimization is future work.

- Start with conservative defaults (see Configuration section)
- Instrument with metrics to understand actual patterns
- Future: DSPy signature for "optimal buffer size given task type"

## Future Considerations

- **Adaptive thresholds**: Auto-tune buffer sizes based on task complexity
- **Semantic deduplication**: Detect and merge redundant information across
  summaries
- **Multi-modal memory**: Handle image/audio content in turns
- **Cross-session learning**: Extract patterns that apply across sessions

## Related Documents

- [Reactive Actor System](./reactive-actor-system.md) - Actor architecture
- [Unified Session Lineage](../archive/designs/unified-session-lineage.md) - Session tracking
- [Progressive Memory System](./progressive-memory-system.md) - Related design
