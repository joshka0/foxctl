# Implementation Plan: LLM-Based Episode Summarization for Hybrid Memory Pipeline

## Problem Statement
The hybrid memory daemon already detects sealed episodes (`needs_summary = 1`) and invokes a `SummarizeEpisode` helper via `summarizerA` in `daemon.go`, but `ConversationMemory` still does not implement this method.

Without it:
- Sealed episodes accumulate with empty summaries
- The deterministic Tier 1 extractors miss natural language signals (e.g., "I'm EchoNova", implicit preferences)
- The context materializer skips episodes with `needs_summary=1` — no episode history reaches the system prompt

## Architecture Decision
Keep the interface contract unchanged and implement:
- `func (m *ConversationMemory) SummarizeEpisode(ctx context.Context, conversationID string, episodeID, startEventID, endEventID int64) (string, int, error)`

All new code lives in a dedicated file: `internal/context/companion/hybrid_summarizer.go`

Decision points:
- **LLM credentials**: Read from `*LLMSummarizer` fields via type assertion because implementation lives in the same `companion` package, where unexported field access is legal. This avoids additional config plumbing.
- **Persistence split**: Load episodes + run LLM outside DB tx; then persist extracted hard state/evidence with a short-lived local transaction.
- **Evidence insertion**: Use `InsertEvidenceSnippet(ctx, nil, …)` — helper uses `queryRowWithTx` fallback to `m.db` when `tx == nil`.

## Design Patterns
- **Single-responsibility file separation**: new `hybrid_summarizer.go` only owns episode summarization + prompt + extraction mapping.
- **Boundary-safe async flow**: daemon writes `needs_summary=0` only after method returns successfully.
- **Best-effort extraction persistence**: a malformed or partial extraction item does not abort the episode summary flow.
- **Strict JSON contract**: prompt instructs exact schema; parser handles fallback to raw summary text.

## File Changes

### 1) `internal/context/companion/hybrid_summarizer.go` (new)

**Purpose**: Episode-level LLM summarization and structured extraction.

#### A) Response type

```go
type episodeSummaryLLMResponse struct {
    Summary             string `json:"summary"`
    IdentityClaims      []string `json:"identity_claims"`
    Preferences         []string `json:"preferences"`
    Decisions           []string `json:"decisions"`
    RelationshipDynamic string   `json:"relationship_dynamics"`
    TechnicalContext    []string `json:"technical_context"`
    HardStateEntries    []struct {
        EntryType     string  `json:"entry_type"`
        Value         string  `json:"value"`
        SourceEventID int64   `json:"source_event_id,omitempty"`
        Confidence    float64 `json:"confidence,omitempty"`
        Metadata      any     `json:"metadata,omitempty"`
    } `json:"hard_state_entries"`
    Evidence []struct {
        SourceEventID int64   `json:"source_event_id"`
        EventType     string  `json:"event_type"`
        FactText      string  `json:"fact_text"`
        Confidence    float64 `json:"confidence,omitempty"`
        Bucket        string  `json:"bucket,omitempty"`
        TTLDays       *int    `json:"ttl_days,omitempty"`
    } `json:"evidence_snippets"`
}
```

#### B) Core method

```go
func (m *ConversationMemory) SummarizeEpisode(
    ctx context.Context,
    conversationID string,
    episodeID, startEventID, endEventID int64,
) (string, int, error)
```

Steps:
1. Validate args
2. Resolve LLM credentials via `m.summarizer.(*LLMSummarizer)` (same package — unexported field access is legal)
3. Load episode events via SQL
4. Build transcript and prompt
5. Call LLM
6. Parse JSON response (raw text fallback on parse failure)
7. Persist extractions in short-lived tx
8. Return `(summary, tokenCount, nil)`

#### C) LLM configuration

```go
engineCfg := engine.LLMChatConfig{
    Provider:      summarizer.provider,
    APIKey:        summarizer.apiKey,
    Model:         summarizer.model,
    MaxIterations: 1,
    Temperature:   0.2,
    MaxTokens:     1000,
}
```

#### D) Full LLM prompt

**System prompt:**
```
You are a memory synthesis assistant for a companion conversation. Convert one sealed episode into:
1) a concise narrative summary,
2) structured hard-state extractions,
3) evidence snippets.

Rules:
- Use only information present in the transcript.
- Never invent facts.
- Keep the summary to exactly 2-3 sentences.
- Prefer concrete claims with sourceable events.
- Return strict JSON only.
```

**User prompt template:**
```
Episode for conversation %s
Episode ID: %d
Start Event ID: %d
End Event ID: %d

Transcript:
%s

Produce JSON with this exact schema:
{
  "summary": "2-3 sentence narrative summary with what happened, decisions, and emotional tone",
  "identity_claims": [
    "explicit first-person identity statements (e.g., 'I'm EchoNova', 'call me Nova', 'my name is...')"
  ],
  "preferences": [
    "stated user preferences or constraints not captured by simple signal phrases"
  ],
  "decisions": [
    "explicit agreements, commitments, or resolved choices"
  ],
  "relationship_dynamics": "brief note about rapport/style (warm, formal, playful, conflictual, directive, etc.)",
  "technical_context": [
    "topic/tool mentions, technical constraints, bug reports, environment/tooling details"
  ],
  "hard_state_entries": [
    {
      "entry_type": "identity|preference|decision|open_question|goal|policy|glossary|relationship_dynamic|technical_context",
      "value": "normalized extracted value",
      "source_event_id": 1234,
      "confidence": 0.93,
      "metadata": {"rationale":"optional"}
    }
  ],
  "evidence_snippets": [
    {
      "source_event_id": 1234,
      "event_type": "assistant_message|user_message|tool_result",
      "fact_text": "short factual snippet suitable for grounding",
      "confidence": 0.8,
      "bucket": "identity|decision|facts|technical|prefs",
      "ttl_days": 30
    }
  ]
}

Output must be valid JSON only.

Example output:
{
  "summary": "The user clarified they prefer concise responses and asked the assistant to use the alias EchoNova. They confirmed the project uses Docker locally, then decided to postpone deployment until Friday. The tone was focused, slightly anxious about production impact, and appreciative of direct checklists.",
  "identity_claims": ["user prefers to be called EchoNova"],
  "preferences": ["prefers concise responses", "prefers checklists for operational tasks"],
  "decisions": ["postpone deployment to Friday", "continue using local Docker setup for now"],
  "relationship_dynamics": "professional, time-sensitive, trust-seeking",
  "technical_context": ["tool used: rlm_context_query", "environment: Docker compose local"],
  "hard_state_entries": [
    {"entry_type": "preference", "value": "concise responses", "source_event_id": 118, "confidence": 0.92, "metadata": {"note": "normalized via LLM extraction"}},
    {"entry_type": "identity", "value": "EchoNova", "source_event_id": 118, "confidence": 0.95}
  ],
  "evidence_snippets": [
    {"source_event_id": 118, "event_type": "user_message", "fact_text": "User asked to be called EchoNova.", "confidence": 0.97, "bucket": "identity", "ttl_days": 30},
    {"source_event_id": 124, "event_type": "assistant_message", "fact_text": "Deployment postponed to Friday.", "confidence": 0.9, "bucket": "decision", "ttl_days": 30}
  ]
}
```

#### E) Episode event loading

```sql
SELECT e.id, t.content, e.event_type, e.payload_json
FROM companion_events e
LEFT JOIN companion_turns t ON t.id = e.turn_id
WHERE e.conversation_id = $1
  AND e.id >= $2
  AND e.id <= $3
ORDER BY e.id
```

#### F) Transcript formatter

`formatEpisodeTranscript(events)` produces stable lines:
- `[1234] user: ...` for `user_message`
- `[1235] assistant: ...` for `assistant_message`
- `[1236] tool_call: <tool_name>` for `tool_call`
- `[1237] tool_result: <payload excerpt>` for `tool_result`
- Trim long tool payloads to ~500 chars.

#### G) Extraction persistence with explicit tx

```go
tx, err := m.db.BeginTx(ctx, nil)
if err != nil {
    // Log warning but still return the summary
    return summary, tokenCount, nil
}

for _, h := range parsed.HardStateEntries {
    entry := ExtractedEntry{
        EntryType:  h.EntryType,
        RawText:    h.Value,
        Value:      h.Value,
        Confidence: h.Confidence,
    }
    if err := m.persistDeterministicExtraction(ctx, tx, conversationID, sid, entry); err != nil {
        zerolog.Ctx(ctx).Warn().Err(err).Msg("episode extraction persistence failed")
        continue
    }
}

// Evidence uses nil tx (documented: falls back to m.db via queryRowWithTx)
for _, e := range parsed.Evidence {
    _, err := m.InsertEvidenceSnippet(ctx, nil, &EvidenceSnippet{...})
    if err != nil {
        zerolog.Ctx(ctx).Warn().Err(err).Msg("episode evidence persistence failed")
        continue
    }
}

if err := tx.Commit(); err != nil {
    return "", 0, fmt.Errorf("commit episode extractions: %w", err)
}

return summary, tokenCount, nil
```

### 2) `internal/context/companion/hybrid_types.go` (modified)

Add new entry type constants for LLM-extracted types:

```go
const (
    EntryTypeIdentity         = "identity"
    EntryTypeRelationship     = "relationship_dynamic"
    EntryTypeTechnicalContext = "technical_context"
)
```

## Testing Strategy

### Unit Tests
- Transcript formatting: user/assistant/tool events map to correct line format
- JSON parse: fenced + normal JSON both parse to `episodeSummaryLLMResponse`
- Fallback path: invalid JSON returns raw summary text and zero extraction side effects
- Confidence/default filling and key synthesis determinism
- Entry-type normalization for new types

### Integration Tests (in-process sqlite)
- Episode range query returns correct event set with mixed event types
- End-to-end: returns summary + token count, inserts hard state and evidence
- Parse failure: summary still returned, no panic

### Edge Cases
- `m.summarizer` absent or not `*LLMSummarizer` → returns error, daemon retries
- LLM stop reason `StopReasonError` → error
- `json.Unmarshal` failure → raw summary fallback, no side-effect inserts
- Duplicate hard-state keys → per-entry warning, no abort
- Evidence redaction drops content → no error, snippet silently skipped
- Empty episode (no events in range) → return error

## Error Handling
- **LLM init/run error**: return error to trigger daemon retry
- **JSON parse error**: warning log, return raw summary with `EstimateTokens`, skip extractions
- **Per-item persistence error**: warning per item, continue
- **TX commit failure**: return error so daemon retries
- **Input validation failure**: return error

## Migration Notes
No DB schema migration needed. Existing hybrid tables already support all required writes.

## Dependencies
- `internal/runtime/engine` for `LLMChatConfig` and `NewLLMChatEngine`
- `internal/runtime/actor/memory` for `EstimateTokens`
- No new external dependencies

## Implementation Order
1. Create `internal/context/companion/hybrid_summarizer.go` with method skeleton
2. Add episode-event query helper and transcript formatter
3. Add full prompt template + LLM call + JSON parse with fallback
4. Add extraction persistence with short-lived tx
5. Add entry-type constants to `hybrid_types.go`
6. Add tests covering parse fallback, tx persistence, transcript mapping
7. Verify interface compatibility — `go vet ./internal/context/companion/...`

## Open Questions
1. For unknown `entry_type` from LLM, should we normalize into dedicated constants immediately, or append as generic keys? **Recommendation**: Add constants now (`identity`, `relationship_dynamic`, `technical_context`) and route LLM output through normalization.
2. On tx commit failure after partial writes, should method return hard error or suppress? **Recommendation**: Return error (strict) to preserve daemon retry semantics.
