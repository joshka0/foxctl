# Milestone 5: Progressive Memory (L0/L1/L2)

**Status:** ~60% Complete

## Overview

Implement progressive memory for long-running actors. L0 is raw turns, L1 is summarized batches, L2 is distilled high-level context. Context stays within budget without losing important history.

---

## PR 5.1 — Actor Memory Cursors + Artifacts

**Status:** ✅ Complete

### Implementation

| Component | Location | Status |
|-----------|----------|--------|
| Short-term memory | `internal/runtime/actor/memory/shortterm.go` | ✅ Done |
| Token counting | `internal/runtime/actor/memory/tokens.go` | ✅ Done |
| Redactor | `internal/runtime/actor/memory/redactor.go` | ✅ Done |

### Memory State Fields

```go
type MemoryState struct {
    NextTurnToSummarize   int
    NextSummaryToDistill  int
    L1ArtifactID          string  // CAS reference
    L2ArtifactID          string  // CAS reference
}
```

### actor_summaries Table

```sql
CREATE TABLE actor_summaries (
    actor_id TEXT,
    summary_index INTEGER,
    level TEXT,  -- 'L1' or 'L2'
    turn_start INTEGER,
    turn_end INTEGER,
    text TEXT,
    created_at TEXT,
    PRIMARY KEY (actor_id, summary_index)
);
```

---

## PR 5.2 — Background Summarizer Worker

**Status:** ⚠️ Partial

### Implementation

| Component | Location | Status |
|-----------|----------|--------|
| Summarizer | `internal/runtime/actor/memory/summarizer.go` | ✅ Done |
| Worker loop | Supervisor-managed | ⚠️ Needs implementation |

### Worker Behavior

1. Read turns beyond cursor
2. Summarize batches into L1
3. Distill L1 → L2 when thresholds hit
4. Advance cursor only after successful transaction

### Crash Safety

- Killing process mid-summarize does not corrupt state
- Resumes cleanly from last committed cursor

### Remaining Work

- [ ] Create `internal/runtime/actor/memory/worker.go`
- [ ] Add worker to supervisor startup
- [ ] Implement crash-safe cursor advancement
- [ ] Add L1 → L2 distillation logic

---

## PR 5.3 — Prompt Builder with Token Budgets

**Status:** ✅ Complete

### Implementation

| Component | Location | Status |
|-----------|----------|--------|
| Token budgets | `internal/runtime/actor/memory/shortterm.go` | ✅ Done |

### Budget Allocation (Hard)

| Section | Budget |
|---------|--------|
| System + policies | 2k |
| Injected inbox context | 6k |
| Retrieved (semantic/task/graph) | 8k |
| L2 summaries | 6k |
| L1 summaries | 8k |
| L0 raw turns | up to 20k |
| Current message + scratch | remaining |

### Test Coverage

```go
// From shortterm_test.go
if cfg.L1TokenBudget != 6000 {
    t.Errorf("L1TokenBudget = %d, want 6000", cfg.L1TokenBudget)
}
if cfg.L2TokenBudget != 4000 {
    t.Errorf("L2TokenBudget = %d, want 4000", cfg.L2TokenBudget)
}
```

---

## PR 5.4 — ContextBudgetExceeded Hook (Pruning)

**Status:** ❌ Not Implemented

### Required Skill

`hooks_context_pruner` - Smart context filtering when budget exceeded.

### Trigger

`ContextBudgetExceeded` event

### Pruning Rules (Deterministic)

1. Drop lowest-ranked injected items (by task PageRank + recency)
2. Reduce L0 turn window first
3. **Always keep:** decisions, errors, todo state

### Remaining Work

- [ ] Create `skills/hooks_context_pruner/` skill
- [ ] Implement ranking-based pruning
- [ ] Wire to ContextBudgetExceeded event
- [ ] Test deterministic pruning behavior

---

## Acceptance Criteria

- [x] Produce and store L1 summaries without affecting runtime
- [ ] Background summarization is crash-safe and resumes cleanly
- [x] Long-running actors stay stable without manual compaction
- [ ] Context overflow triggers pruning deterministically
