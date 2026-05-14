# Milestone 7: Brain-like Adaptation

**Status:** ~40% Complete

## Overview

Self-improving layer that learns from experience. Preferences persist across sessions. Pattern mining extracts "what worked." Evaluation harness proves improvements are real.

---

## PR 7.1 — Preferences as First-Class Memory

**Status:** ⚠️ Partial

### Current Implementation

| Component | Location | Status |
|-----------|----------|--------|
| Memory store | `internal/storage/memory/` | ✅ Done |
| Memory query skill | `skills/memory_query/` | ✅ Done |
| Memory types | gotcha, decision, pattern | ✅ Done |

### Gap

No `preference` type with `pref://` URI scheme.

### Required URI Scheme

```
pref://workspace/<id>/formatting/goimports
pref://user/<id>/tooling/always_run_tests
pref://workspace/<id>/testing/coverage_threshold
```

### Injection Behavior

Hook `AgentTurnStart` injects top matching preferences (bounded by token budget).

### Remaining Work

- [ ] Add `preference` memory type
- [ ] Implement `pref://` URI parsing
- [ ] Add preference injection to turn start hook
- [ ] Create CLI for preference management

---

## PR 7.2 — Pattern Miner Job

**Status:** ⚠️ Partial

### Current Implementation

| Component | Location | Status |
|-----------|----------|--------|
| optimize/patterns | `skills/optimize_patterns/` | ✅ Done |
| optimize/feedback | `skills/optimize_feedback/` | ✅ Done |
| optimize/analyze | `skills/optimize_analyze/` | ✅ Done |
| trajectory store | `internal/storage/trajectory/` | ✅ Done |

### Gap

No background job that runs automatically.

### Pattern Mining Logic

From successful runs (stop reason ok, no errors):
- Tool sequences correlating with success
- Common file clusters per task type
- Frequent "gotcha → fix" pairs

### Output

Compact "pattern memories" in memory.db:

```json
{
  "type": "pattern",
  "name": "pattern://test-first-development",
  "summary": "Running tests before edits correlates with 40% fewer iterations",
  "data": {
    "tool_sequence": ["test/run", "fs/read", "code/smart_write", "test/run"],
    "success_rate": 0.85,
    "sample_size": 127
  }
}
```

### Remaining Work

- [ ] Create `analysis.pattern_miner` background job
- [ ] Implement tool sequence extraction
- [ ] Implement file cluster detection
- [ ] Implement gotcha-fix pair extraction
- [ ] Add pattern injection to relevant tasks

---

## PR 7.3 — Automatic Context Improvements

**Status:** ❌ Not Implemented

### Goal

Improve prompt building without model fine-tuning.

### Feedback Collection

Hook `PostAgentTurn` writes:
- "What was useful context" (explicit list)
- "What was useless context" (explicit list)

### Feedback Application

Context router uses feedback to rank future injections:
- Upweight consistently useful sources
- Downweight consistently ignored sources

### Metrics

- Fewer injected tokens over time
- Same or better success rate

### Remaining Work

- [ ] Add context usefulness tracking to turn storage
- [ ] Implement feedback aggregation
- [ ] Modify context router to use feedback weights
- [ ] Add metrics collection for before/after comparison

---

## PR 7.4 — Evaluation Harness

**Status:** ❌ Not Implemented

### Required Command

```bash
foxctl eval replay --from trajectory.db
```

### Behavior

- Replays tool traces + prompt builds (no live edits)
- Deterministic, offline evaluation

### Metrics Collected

| Metric | Description |
|--------|-------------|
| avg_tool_calls_per_task | Tool efficiency |
| avg_injected_tokens | Context efficiency |
| stop_block_rate | Completion reliability |
| time_to_first_success | Speed to solution |

### Use Case

Compare baseline vs improved router/tooling policies.

### Remaining Work

- [ ] Create `cmd/foxctl/cmd/eval.go`
- [ ] Implement trajectory replay logic
- [ ] Implement prompt rebuild from historical state
- [ ] Add metrics aggregation and reporting
- [ ] Add comparison mode (baseline vs candidate)

---

## Acceptance Criteria

- [ ] Set preference once → future runs follow it automatically
- [ ] New pattern memories appear and get injected for relevant tasks
- [ ] System becomes less noisy over time (measurable)
- [ ] Can compare baseline vs improved policies with metrics
