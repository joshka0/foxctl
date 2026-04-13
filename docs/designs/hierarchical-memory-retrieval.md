# Hierarchical Memory Retrieval for RLM Agent Loop

## Status: Historical Draft (Superseded by v2 Hybrid Runtime)
## Date: 2026-02-07
## Branch: feat/companion-dynamic-compress

## Scope Note (V2-first)

This design targets the v2 runtime context-builder/event-pipeline path.
Phase 1/2 items that touch current `internal/engine/rlm_tools.go` are
transitional options; the canonical target lives under `internal/v2/runtime/*`.

Historical note: sections below reference earlier transitional storage terms
(`companion_day_summaries`, `companion_history`, `companion_memory_state`).
Current runtime behavior is hybrid-only and documented in
`docs/general/companion-memory.md`.

---

## Problem

The companion RLM agent loop has two retrieval paths, both suboptimal:

1. **`GetContext()` blob injection** - Always injects a pre-formatted ~40K token markdown blob (L0+L1+L2) into the system prompt. No selectivity, no relevance filtering. The model sees everything whether it needs it or not.

2. **`executeSemanticQuery`** (via `rlm_context_query` with `semantic_query`) - Flat search across all memory types using `SearchSimilar()`, then post-filters by SessionID in Go. No layer awareness, no query enrichment, no type-scoped search.

### What's Missing

- **No hierarchical retrieval**: L2 (stable prefs/relationship) and L1 (day summaries) are searched the same way with the same ranking
- **No query enrichment**: L2 context doesn't inform L1 search queries
- **No expand/drill-down**: Can't go from "Feb 1 summary looks relevant" → "show me the actual turns from Feb 1"
- **`SearchSimilarByType` unused**: Exists on the interface but RLM always uses `SearchSimilar` (all types), wasting vector search budget
- **Atomic text fields unused**: SimpleMem processing stores `atomic_text`, `entities`, `keywords` in `named_memory` but RLM search ignores them
- **No token budget on tool output**: `executeSemanticQuery` returns unlimited results
- **Autonomous instructions are vague**: Don't guide the model toward hierarchical retrieval strategy
- **No temporal pyramid views**: Can't request coarse-grained context (`hours` → `days` → `weeks` → `months`) before drilling down

---

## Existing Primitives Inventory

### Storage Methods Available (MemoryStore interface)

| Method | Currently Used by RLM | Useful for Hierarchical | Notes |
|--------|:---:|:---:|-------|
| `Get(name, workspace)` | No | **Yes** | Direct lookup for known L2 name pattern |
| `Search(workspace, query, limit)` | Yes (fallback) | Yes | BM25 text search |
| `SearchSimilar(workspace, embedding, limit)` | Yes (primary) | No | All-type vector search, wasteful |
| `SearchSimilarByType(workspace, type, embedding, limit)` | **No** | **Yes** | Type-scoped vector search |
| `ListFiltered(workspace, filter, limit, offset)` | Yes (last resort) | Yes | Filter by Types[] + SessionID |
| `Relevant(workspace, limit)` | No | Maybe | Recency+frequency scoring |
| `SaveResult(ctx, opts)` | N/A | N/A | Write path |
| `UpdateAtomic(...)` | No | Future | SimpleMem enrichment |

### Named Memory Entry Patterns

```
companion:history:<conversationID>         → type: companion_history (L2, one per conversation)
companion:summary:<conversationID>:<date>  → type: companion_summary (L1, one per day)
```

### Database Tables (Direct SQL Access in companion/)

| Table | Layer | Query Patterns |
|-------|-------|---------------|
| `companion_turns` | L0 | `WHERE conversation_id = ? AND created_at >= ?` |
| `companion_day_summaries` | L1 | `WHERE conversation_id = ? AND date = ?` |
| `companion_history` | L2 | `WHERE conversation_id = ?` |
| `companion_memory_state` | Meta | Compression cursor tracking |

### Retention Decision: Keep Everything (L0/L1/L2)

We will keep all conversation memory artifacts indefinitely:
- **L0**: Keep all raw turns in `companion_turns` (no retention/pruning).
- **L1**: Keep all day summaries in `companion_day_summaries` (do not delete after distillation).
- **Named memory**: Keep all `named_memory` entries for `companion_summary` and `companion_history`.

Implication:
- "Expand" can always drill down to raw turns for any date that has turns.
- Distillation (L2) is an additional compressed layer, not a replacement for L1/L0.

### Package Dependency Constraint

```
companion/ → engine/    (companion imports engine)
engine/    ↛ companion/ (would create cycle)
```

Engine can access companion data only through interfaces injected by companion.

### Time Semantics Decision: UTC Day Buckets

All date bucketing and day-boundary logic uses **UTC**:
- Day summary keys (`YYYY-MM-DD`) are derived in UTC.
- Timestamps should be stored in UTC to keep summaries stable when moving DBs across machines/timezones.

---

## Design

### Phase 1: Fix `executeSemanticQuery` (Quick Win)

**Files:** `internal/engine/rlm_tools.go`

Tighten the existing semantic query path without adding new tools:

1. **Use `SearchSimilarByType`** instead of `SearchSimilar`:
   - Prefer `companion_history` (L2) first (target 1 entry)
   - Then fill the remaining slots with `companion_summary` (L1)
   - Over-fetch (up to ~1000) to compensate for post-filtering by `SessionID` on shared workspaces
   - Fall back to `ListFiltered(workspace, {Types:[...], SessionID:convID})` to fill any remaining slots

2. **Add `layer` field** to output entries:
   ```json
   {"name": "...", "type": "companion_history", "layer": "L2", "score": 0.92}
   ```

3. **Add `max_chars` cap** on total output size (default 6000 chars), plus per-entry summary truncation.

4. **Use `Get()` fast path** for L2 when conversation ID is known:
   ```go
   l2Name := fmt.Sprintf("companion:history:%s", e.conversationID)
   entry, err := e.memoryStore.Get(ctx, l2Name, e.workspace)
   ```

5. **Add minimal, stable output metadata**:
   - `stats.method`, `stats.truncated`, `stats.max_chars`
   - `date` field for L1 summaries when `name` matches `companion:summary:<conversationID>:<YYYY-MM-DD>`

**Risk:** Low. Same tool, better implementation. No schema changes.

### Phase 2: Add `rlm_memory_retrieve` Tool

**Files:** `internal/engine/rlm_tools.go`

New purpose-built tool for hierarchical retrieval:

#### Input Schema

```go
type MemoryRetrieveInput struct {
    Query    string   `json:"query"`              // Natural language query
    Layers   []string `json:"layers,omitempty"`   // Default: ["L2","L1"]
    LimitL2  int      `json:"limit_l2,omitempty"` // Default: 1
    LimitL1  int      `json:"limit_l1,omitempty"` // Default: 5
    MaxChars int      `json:"max_chars,omitempty"`// Default: 6000
}
```

#### Output Schema

```json
{
  "found": true,
  "context_markdown": "## Retrieved Memory\n...",
  "sources": [
    {"layer": "L2", "name": "companion:history:abc", "type": "companion_history", "score": 1.0, "date": null},
    {"layer": "L1", "name": "companion:summary:abc:2026-02-01", "type": "companion_summary", "score": 0.82, "date": "2026-02-01"}
  ],
  "stats": {"method": "vector", "l2_hits": 1, "l1_hits": 3, "chars": 4200},
  "expandable_dates": ["2026-02-01", "2026-02-05"],
  "expandable_refs": ["day:2026-02-01", "turn:abc123#msg:m9:0-600"]
}
```

Key:
- `expandable_dates` tells the model which dates have underlying L0 turns available for drill-down.
- `expandable_refs` exposes stable references for partial-turn expansion.

#### Retrieval Strategy

```
1. L2 Fast Path
   ├─ Get("companion:history:<convID>", workspace)  → direct lookup
   └─ If miss: SearchSimilarByType("companion_history", vec, 2) → filter by SessionID

2. Query Enrichment (from L2 Result JSON)
   ├─ Parse L2.Result for recurring_topics, user_preferences, shared_memories
   └─ Append to query: "query\n\nContext: topic1, topic2, pref1"

3. L1 Targeted Search
   ├─ Embed enriched query
   ├─ SearchSimilarByType("companion_summary", enriched_vec, limit_l1 * 3)
   ├─ Filter by SessionID
   └─ Fallback: Search(workspace, enriched_query, limit_l1 * 3)

4. Format Output
   ├─ Build context_markdown (token-budgeted)
   ├─ Collect expandable_dates from L1 hit names
   └─ Cap total output at max_chars
```

#### Tool Definition

```go
ToolDef{
    Name: "rlm_memory_retrieve",
    Description: "Retrieve relevant conversation memory hierarchically. " +
        "Searches L2 (distilled history: preferences, relationship, recurring topics) first, " +
        "then L1 (day summaries) enriched by L2 context. " +
        "Use before answering questions that depend on prior conversations. " +
        "Returns context_markdown for direct use and expandable_dates for drill-down.",
    // ... schema ...
}
```

**Risk:** Medium. New tool, but no schema changes. Additive only.

### Phase 3: Add `rlm_memory_expand` Tool (L1→L0 Drill-Down)

**Files:**
- `internal/engine/rlm_tools.go` - tool implementation
- `internal/engine/interfaces.go` (new) - `ConversationTurnReader` interface
- `internal/context/companion/memory_reader.go` (new) - adapter implementation
- `internal/context/companion/service.go` - wire adapter into RLMToolExecutor

#### Interface (in engine/, cycle-safe)

```go
// ConversationTurnReader provides read-only access to conversation turns.
// Defined in engine to avoid companion→engine import cycles.
type ConversationTurnReader interface {
    // TurnsForDate returns conversation turns for a specific date (YYYY-MM-DD).
    // Returns an empty slice when no turns exist for that date (either none were recorded,
    // or they were pruned by retention policy).
    TurnsForDate(ctx context.Context, conversationID, date string, limit int) ([]ConversationTurn, error)

    // DateHasTurns reports whether a date still has underlying turns after any retention/pruning.
    DateHasTurns(ctx context.Context, conversationID, date string) (bool, error)
}

type ConversationTurn struct {
    Role      string    `json:"role"`
    Content   string    `json:"content"`
    CreatedAt time.Time `json:"created_at"`
}
```

#### Input/Output

```go
type MemoryExpandInput struct {
    Date     string `json:"date"`               // YYYY-MM-DD
    Limit    int    `json:"limit,omitempty"`     // Default: 30
    MaxChars int    `json:"max_chars,omitempty"` // Default: 8000
}
```

```json
{
  "found": true,
  "date": "2026-02-01",
  "turns": [
    {"role": "user", "content": "...", "created_at": "2026-02-01T14:30:00Z"},
    {"role": "assistant", "content": "...", "created_at": "2026-02-01T14:31:00Z"}
  ],
  "turn_count": 12,
  "truncated": false,
  "message": ""
}
```

If no L0 turns remain (for example, pruned by retention policy):
```json
{
  "found": false,
  "date": "2026-01-15",
  "turns": [],
  "turn_count": 0,
  "message": "No turns are available for this date. Use rlm_memory_retrieve with layers=[\"L2\"] for distilled context."
}
```

#### Wiring (cycle-safe)

```go
// In companion/service.go, within Chat():
adapter := NewConversationTurnReaderAdapter(s.memory.DB())
rlmExecutor.SetTurnReader(adapter)
```

```go
// In engine/rlm_tools.go:
type RLMToolExecutor struct {
    // ... existing fields ...
    turnReader ConversationTurnReader // Optional: for L0 expand
}

func (e *RLMToolExecutor) SetTurnReader(reader ConversationTurnReader) {
    e.turnReader = reader
}
```

**Risk:** Medium. New interface + adapter, but small surface area. SQL queries are straightforward date-filtered selects.

### Phase 4: Strengthen Autonomous Instructions

**File:** `internal/context/companion/service.go` (`addAutonomousInstructions`)

Replace the generic "Use available tools" with specific guidance:

```go
case agent.ModeAutonomous:
    instructions = `

## Autonomous Mode Instructions

Before responding to the user, you should gather relevant context:

1. **Memory Retrieval**: Call rlm_memory_retrieve with the user's question as query.
   This searches your distilled history (L2) for stable preferences and relationship context,
   then day summaries (L1) for relevant past conversations.

2. **Drill Down** (optional): If a specific date in the results looks highly relevant
   and appears in expandable_dates, call rlm_memory_expand with that date to see
   the actual conversation turns.

3. **Context Variables**: Use rlm_context_query for structured data (user preferences,
   ongoing topics, session state).

4. **Then Respond**: With full context gathered, provide your response.

Take your time to think and gather information. The user expects a thoughtful, well-informed response.`
```

#### Optional: Reduce GetContext() Blob

When `rlm_memory_retrieve` is available, consider:
- **Keep L0 (vivid turns)** in system prompt - these are today's conversation, always relevant
- **Remove L1+L2 from system prompt** - let the model pull them on demand via tools
- **Savings:** ~16K tokens per turn (L1: 10K + L2: 6K)

This is a tradeoff: less automatic context vs. more targeted retrieval. Start by keeping both and measure whether the model actually uses the tools before removing the blob.

---

### Phase 5: Event-Driven Temporal Pyramid (PR-10)

Add an event pipeline that turns raw turns into cheap temporal views and
referenceable artifacts.

#### Event Schema

```json
{
  "event_type": "turn.recorded",
  "trace_id": "trace_123",
  "correlation_id": "req_456",
  "causation_id": "ask_789",
  "payload": {
    "conversation_id": "conv_1",
    "turn_id": "turn_42",
    "timestamp": "2026-02-18T14:23:11Z",
    "token_count": 1830
  }
}
```

Additional events:
- `bucket.updated`
- `bucket.closed`
- `artifact.created`
- `artifact.failed`

#### Artifact Schema

```json
{
  "artifact_id": "art_001",
  "subject_kind": "bucket",
  "subject_id": "day:2026-02-18",
  "artifact_kind": "summary",
  "artifact_version": "v1",
  "content_ref": "cas:sha256:...",
  "metadata": {"top_topics": ["auth", "runtime"], "decision_count": 2}
}
```

#### Context-Builder API Surface

```go
type ContextRequest struct {
    Query        string
    BudgetTokens int
    Mode         string // chat|recap|planning|search
    Views        []string // hours|days|weeks|months
}

type ContextBundle struct {
    Markdown   string
    Refs       []string
    UsedTokens int
}
```

#### Temporal Pyramid

| View | Primary Source | Drill-down Target |
|------|----------------|-------------------|
| `months` | L2 history artifacts | `weeks` |
| `weeks` | grouped L1 summaries + L2 | `days` |
| `days` | L1 summaries | `hours` / L0 turns |
| `hours` | L0 turns | turn slices |

Rules:
1. Turn write path must not block on enrichment.
2. Enrichment idempotency key: `(turn_id, artifact_kind, artifact_version)`.
3. Context builder always returns drill-down refs (`expandable_dates`, `expandable_refs`) when available.

---

## Implementation Order

Phases 1-4 are transitional and can be used to improve current retrieval
behavior incrementally. Phase 5 is the canonical v2 target architecture.

```
Phase 1 ─── Fix executeSemanticQuery ──── (small, same-file change, ship immediately)
    │
Phase 2 ─── Add rlm_memory_retrieve ──── (new tool, same file, additive)
    │
Phase 3 ─── Add rlm_memory_expand ─────── (new interface + adapter, 3 files)
    │
Phase 4 ─── Strengthen instructions ───── (prompt engineering, test empirically)
    │
Phase 5 ─── Event pipeline + temporal pyramid (non-blocking enrichers, refs)
```

Each phase is independently shippable. Phase 1 is a clear improvement with no downside.

---

## What This Design Intentionally Excludes

1. **RRF/hybrid fusion in the tool** - The `retrieval.Generator` and `SearchableStore` patterns are designed for code search with thousands of candidates across multiple sources. Companion memory has at most ~50 entries per conversation. Simple L2→L1 cascade with type-scoped search is sufficient. Over-engineering here adds complexity for no measurable gain.

2. **Atomic text search** - While `atomic_text`, `entities`, and `keywords` fields exist in `named_memory`, they're populated asynchronously by the SimpleMem processor. Not all companion memories will have them. Worth revisiting when atomic processing coverage is higher, but not a dependency for hierarchical retrieval.

3. **Cross-conversation search** - This design is scoped to a single conversation (filtered by SessionID). Cross-conversation memory is a different feature (e.g., "what did we discuss in any conversation about X?").

4. **Proactive injection** - Some systems pre-fetch likely-relevant memories before the model even asks. This design keeps retrieval model-driven (the model calls tools). Proactive injection could be added as a hook later.

---

## Testing Strategy

### Unit Tests

- `TestRLMToolExecutor_SemanticQuery_VectorByTypeFiltersSessionAndAddsLayer` - Type-scoped vector search with SessionID filtering (Phase 1)
- `TestRLMToolExecutor_SemanticQuery_TruncatesLongSummaries` - Output stays bounded under max_chars (Phase 1)
- `TestMemoryRetrieve_L2FastPath` - Direct name lookup succeeds
- `TestMemoryRetrieve_L2FallbackVector` - Vector search when direct lookup misses
- `TestMemoryRetrieve_QueryEnrichment` - L2 topics appended to L1 query
- `TestMemoryRetrieve_SessionFilter` - Only returns entries matching conversationID
- `TestMemoryRetrieve_MaxCharsEnforced` - Output truncated at budget
- `TestMemoryExpand_DateFound` - Returns turns for valid date
- `TestMemoryExpand_NoTurns` - Returns helpful message when older turns were pruned/are missing
- `TestMemoryExpand_EmptyDate` - Handles dates with no turns

### Integration Tests

- Full Chat() flow with autonomous mode → model calls `rlm_memory_retrieve` → responds with context
- Compression → retrieve → expand roundtrip (store turns, compress to L1, retrieve L1, expand back to L0)
- Event pipeline roundtrip (`turn.recorded` → bucket/artifact events) with non-blocking turn completion
- Temporal pyramid retrieval (`months` → `weeks` → `days` → `hours`) with deterministic refs

---

## File Change Summary

| File | Phase | Change |
|------|-------|--------|
| `internal/engine/rlm_tools.go` | 1,2,3 | Fix semantic query; add 2 new tools + execute handlers |
| `internal/engine/interfaces.go` | 3 | New: `ConversationTurnReader` interface + `ConversationTurn` struct |
| `internal/context/companion/memory_reader.go` | 3 | New: adapter implementing `ConversationTurnReader` via SQL |
| `internal/context/companion/service.go` | 3,4 | Wire turn reader; update autonomous instructions |
| `internal/engine/rlm_tools_test.go` | 1,2,3 | Tests for all phases |
| `internal/v2/runtime/enrichers/*` | 5 | Event-driven enrichment workers (bucket/artifact producers) |
| `internal/v2/runtime/contextbuilder/*` | 5 | Context builder + temporal view selection + refs |
