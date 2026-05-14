# Phase 2 Milestones - Implementation Status

Phase 2 builds on the actor runtime foundation (Milestone 1) to add interactive consoles, multi-agent coordination, progressive memory, smarter tools, and brain-like adaptation.

## Quick Status

| Milestone | Completion | Key Focus |
|-----------|------------|-----------|
| [M2: Console](milestone-2-console.md) | ~85% | Interactive actor sessions |
| [M3: Blackboard](milestone-3-blackboard.md) | ~70% | Multi-agent coordination |
| [M4: Graph](milestone-4-graph.md) | ~90% | PageRank + context injection |
| [M5: Memory](milestone-5-memory.md) | ~60% | L0/L1/L2 progressive memory |
| [M6: Tools](milestone-6-tools.md) | ~50% | Compound tools |
| [M7: Adaptation](milestone-7-adaptation.md) | ~40% | Self-improvement |

## Priority Implementation Order

Based on impact and dependencies:

### High Priority (Enables Core Features)

1. **M4.5 - Graph-aware context injection**
   - Enables "agents see each other"
   - Depends on: M4.1-4.4 (complete)
   - Blocked by: `hooks_context_router`

2. **M3.3 - `hooks_context_router`**
   - Central routing intelligence
   - Required by: M4.5, M3.4, M5.4
   - No blockers

3. **M5.2 - Background summarizer worker**
   - Enables long-running agents
   - Depends on: M5.1 (complete)
   - No blockers

### Medium Priority (Reduces Tool Spam)

4. **M6.2 - `code.context_bundle`**
   - Reduces 5-15 tool calls to 1
   - Depends on: existing search skills
   - No blockers

5. **M3.4 - `task.dialogue` tool**
   - Enables cross-agent dialogue viewing
   - Depends on: M3.1 (task binding)
   - No blockers

### Lower Priority (Polish & Optimization)

6. **M5.4 - `hooks_context_pruner`**
   - Smart overflow handling
   - Depends on: M4.3 (PageRank)
   - No blockers

7. **M7.4 - Evaluation harness**
   - Proves improvements work
   - No blockers but low urgency

## Key Missing Skills

| Skill | Milestone | Purpose |
|-------|-----------|---------|
| `hooks_context_router` | M3.3 | Route context to actors |
| `hooks_context_pruner` | M5.4 | Prune on budget exceeded |
| `hooks_graph_ingest` | M4.2 | Populate graph from work |
| `task_dialogue` | M3.4 | Fetch task conversation |
| `code_context_bundle` | M6.2 | Unified context gathering |
| `fs_patchset` | M6.1 | Atomic multi-file edits |
| `code_search_edit` | M6.3 | Combined search+replace |

## Architecture Notes

### Event Flow

```
User/Actor Action
       ↓
   Event Bus
       ↓
   Hook Dispatcher
       ↓
┌──────────────────────────────────┐
│  hooks_context_router            │
│  - Routes bb posts to actors     │
│  - Injects graph neighbors       │
│  - Handles interrupts            │
└──────────────────────────────────┘
       ↓
   Actor Context Inbox
       ↓
   Prompt Builder (with L0/L1/L2)
       ↓
   LLM Call
```

### Storage Dependencies

```
sessions.db
├── actor_turns (needs task_id column)
├── actor_summaries (L1/L2)
└── console_sessions

memory.db
├── graph_nodes
├── graph_edges
├── named_entries (preferences)
└── patterns

trajectory.db
└── events (console.*, task.*, bb.*)
```

## Related Documents

- [extensions.md](extensions.md) - Extension points
- [gui-style.md](gui-style.md) - GUI design guidelines
- [phase2-milestones.md](phase2-milestones.md) - Original milestone spec
