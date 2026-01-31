# Phase -1: Evaluation Harness

> **Goal:** Make embedding/retrieval changes measurable with a small, repeatable eval suite.

## Overview

This phase adds a lightweight evaluation tool so each PR can prove search quality improvements without guessing. It runs curated queries against multiple retrieval paths and reports hit-rate and ranking metrics.

**Dependencies:** None
**Estimated PRs:** 1

---

## PR -1.1: Retrieval Evaluation CLI

### Summary

Add an `agentctl eval retrieval` command that runs a curated query set and reports simple, comparable metrics across index types.

### Deliverables

- CLI: `agentctl eval retrieval`
- Query set file: `docs/plans/embedding-quality-roadmap/eval-queries.yaml`
- Output formats: JSON (machine), Markdown (human)

### Query Set Format

```yaml
- id: auth-handler
  query: "authentication handler"
  scope: symbols
  expected_any_of:
    # Memory entry format (available today)
    - symbol://<workspace>/internal/auth/handler.go:HandleRequest
    # Repoindex format (Phase 4)
    - <repo_key>::sym:internal/auth::internal/auth/handler.go::HandleRequest
  notes: "Should surface top-level auth request handler"

- id: semantic-search
  query: "hybrid search scoring"
  scope: files
  expected_any_of:
    - file://<workspace>/internal/retrieval/semantic_search.go
    - <repo_key>::file:internal/retrieval/semantic_search.go
  notes: "Should land in retrieval path"
```

Expected target formats:
- Memory entry names (symbol://, file://) for current storage
- Repoindex node IDs (<repo_key>::kind:...) after Phase 4
- Optional patterns if IDs include signature hashes

### Placeholder Resolution

- `<workspace>` resolves to the eval run's workspace path.
- `<repo_key>` resolves using the same workspace normalization as storage (memory.db).
- Expected targets may be exact matches or regex/prefix patterns when sigHash is present.

### Metrics

- **hit_rate@k** (k=5,10)
- **first_correct_rank**
- **top_k_snapshot** (for manual review)

### Acceptance Criteria

- [ ] `agentctl eval retrieval` runs end-to-end on a workspace
- [ ] Reports hit_rate@k and first_correct_rank per query
- [ ] Supports multiple sources (symbols, file summaries, semantic index)
- [ ] Query set lives in versioned YAML under roadmap docs
