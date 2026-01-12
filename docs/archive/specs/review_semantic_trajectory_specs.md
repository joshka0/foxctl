# Review, Semantic Index, and Trajectory Specs

This file groups the related specs that define the review gate, semantic file
indexing, dspy-go agents, and trajectory capture for agentctl. It is an
orientation hub only; the authoritative normative details live in the linked
specs.

## Related Specs

- `review_gate.md` – Task-level review lifecycle, review artifacts, and
  integration with hooks, mailbox, and overseer.
- `semantic_file_index.md` – Single-embedding-per-file semantic index, lifecycle
  triggers (especially post-review), and integration with vector search.
- `code_symbol_index_and_swe_grep.md` – Code symbol index, SWE Grep skill, and
  funnel-style retrieval architecture for agents.
- `dspy_go_agents.md` – dspy-go agent runtime, canonical Coding/Planning/Review
  signatures, and overseer/ mailbox integration.
- `dspy_trajectory_capture.md` – Trajectory index, user request capture,
  trajectory events, dspy-friendly episode schema, and export operations.

## Conceptual Flow (Non-Normative)

```text
User request / tasks
        │
        ▼
  dspy-go agents (coder / planner / reviewer)
        │
        ▼
  Code changes + tests + reviews
        │
        ├─ Review artifacts (review_gate.md)
        │
        ├─ Semantic file index updates (semantic_file_index.md)
        │
        ├─ Symbol index + SWE Grep (code_symbol_index_and_swe_grep.md)
        │
        ▼
  Trajectories + episodes (dspy_trajectory_capture.md)
        │
        ▼
  Offline optimizers / evaluation (non-normative)
```

This diagram is intentionally high level. Each linked spec remains the single
source of truth for its respective behavior and data shapes.
