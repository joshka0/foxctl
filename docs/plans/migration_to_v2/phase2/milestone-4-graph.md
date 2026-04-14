# Milestone 4: Unified Graph + PageRank

**Status:** ~90% Complete

## Overview

Tasks, sessions, symbols, and memories become graph nodes. Edges are added via hooks. PageRank helps both scheduling and context selection - enabling the "shared brain" architecture.

---

## PR 4.1 — Graph Tables + API

**Status:** ✅ Complete

### Implementation

| Component | Location | Status |
|-----------|----------|--------|
| Graph manage skill | `skills/graph/main.go` | ✅ Done |
| Graph storage | `internal/storage/` | ✅ Done |

### Skills Available

```bash
# Add edge
foxctl run graph/manage --input '{"op": "add_edge", "from": "task:123", "to": "symbol:foo", "edge_type": "modified"}'

# Query neighbors
foxctl run graph/manage --input '{"op": "query_neighbors", "node_id": "task:123"}'

# Get stats
foxctl run graph/manage --input '{"op": "stats"}'
```

---

## PR 4.2 — Edge Ingestion Hooks

**Status:** ⚠️ Partial

### Hook → Edge Mappings

| Trigger | Edge Created | Status |
|---------|--------------|--------|
| `PostToolUse(Edit/Write)` | `task → symbol (modified)` | ⚠️ Needs hook |
| `PostAgentTurn` | `task → actor (worked_by)` | ⚠️ Needs hook |
| `PostAgentTurn` | `task → bb_post (discussed)` | ⚠️ Needs hook |
| `bb.posted` | `task → memory (mentions)` | ⚠️ Needs hook |
| `task.depends_on` | `task → task (depends_on)` | ✅ Done (todo/manage) |

### Remaining Work

- [ ] Create `hooks_graph_ingest` skill
- [ ] Wire to PostToolUse for symbol edges
- [ ] Wire to PostAgentTurn for actor/discussion edges
- [ ] Wire to bb.posted for mention edges

---

## PR 4.3 — PageRank Job + Persistence

**Status:** ✅ Complete

### Implementation

| Component | Location | Status |
|-----------|----------|--------|
| PageRank skill | `skills/graph_pagerank/main.go` | ✅ Done |
| Tasks graph | `internal/intelligence/analysis/tasksgraph/graph.go` | ✅ Done |

### Stored Metrics

- `pagerank` score
- `in_degree`
- `out_degree`

### Trigger Rules

- Run after every N new edges (N=200)
- Run once on supervisor startup

### CLI

```bash
# View top tasks by PageRank
foxctl run graph/manage --input '{"op": "top", "node_type": "task", "limit": 20}'
```

---

## PR 4.4 — Overseer Scheduling Uses PageRank

**Status:** ✅ Complete

### Implementation

| Component | Location | Status |
|-----------|----------|--------|
| Overseer scorer | `internal/intelligence/analysis/overseer/scorer.go` | ✅ Done |

### Scoring Formula (Hard)

```
score = 0.60 * pagerank + 0.25 * explicit_priority + 0.15 * recency
```

Current weights from `scorer.go`:
- `WeightPageRank = 0.20`
- `WeightCriticalPath` (additional factor)

### Behavior

Overseer consistently pushes high-importance tasks earlier and explains why in `bb.assignment`.

---

## PR 4.5 — Context Injection Uses Graph Neighbors

**Status:** ❌ Not Implemented

### Required Behavior

At `agent.turn.start`, router injects:

1. Active task summary (from tasks db)
2. Top 3 neighbor tasks by PageRank (dependencies + dependents)
3. For each injected task: 10-turn compact dialogue slice via `task.dialogue`
4. Any new bb posts for those tasks since last turn

### Implementation Location

This belongs in `hooks_context_router` (Milestone 3.3).

### Remaining Work

- [ ] Implement graph neighbor lookup in context router
- [ ] Add dialogue slice injection
- [ ] Add bb post injection for related tasks
- [ ] Test "agents notice each other" behavior

---

## Acceptance Criteria

- [x] Add edges and query neighbors deterministically
- [x] Graph populated automatically from normal work
- [x] `foxctl graph top --type task --limit 20` is stable and reproducible
- [x] Overseer uses graph importance for scheduling
- [ ] Agents working on related tasks "notice" each other automatically
