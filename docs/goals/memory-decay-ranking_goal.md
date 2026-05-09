# Goal: Memory Decay Ranking

## Goal

Implement recency-aware memory ranking for foxctl named-memory retrieval.

The delivered behavior should match this contract:

```text
Memory relevance remains primary.
Recent retrieval gently boosts near-ties.
Idle memories are softly dampened, not hidden.
Old strong matches can still surface.
Search access is recorded asynchronously or in a bounded best-effort path.
The public score remains stable and bounded.
```

This is a search-time reranking feature, not a memory migration, pruning system,
or lifecycle replacement.

## Context

- Repo: `/Users/joshka/repos/personal/foxctl`.
- Motivation: Mem0-style memory decay makes long-running agents prefer what is
  currently useful without deleting older knowledge.
- Current foxctl substrate:
  - Named-memory rows already carry `last_accessed`, `access_count`, and richer
    telemetry counters in `internal/storage/interfaces.go`.
  - `Store.Get` and `TursoStore.Get` update `last_accessed` and `access_count`.
  - `Store.Relevant` and `TursoStore.Relevant` already rank by recency/access
    patterns through `scoreEntry`.
  - `memory/query` and `code/semantic_search` already widen candidate pools
    before final result truncation.
  - Vector search paths currently rank only by similarity/distance.
  - Search paths currently do not consistently record memory access for surfaced
    results.
- Related files:
  - `internal/storage/interfaces.go`
  - `internal/storage/memory/store.go`
  - `internal/storage/memory/turso_store.go`
  - `internal/storage/memory/search.go`
  - `skills/memory_query/main.go`
  - `skills/code_semantic_search/main.go`
  - `cmd/foxctl/cmd/memory_named.go`
  - `internal/context/memorycore/record.go`
  - `internal/v2/adapters/turso/turns/store.go`

## Minimal Shape

Use the smallest composable implementation:

1. Add a pure memory-decay scoring unit.
2. Apply it as a bounded multiplier over existing relevance scores.
3. Record search-result access through a narrow store method.
4. Wire the reranker into named-memory retrieval lanes.
5. Expose enough score metadata for tests and debugging.

Avoid turning this into a general event bus, curator rewrite, lifecycle rewrite,
or new memory schema platform.

## Constraints

- Keep relevance primary. Decay may reorder near-ties, but must not swamp strong
  similarity or BM25 matches.
- Use a bounded multiplier, initially `0.3x..1.5x`, unless tests show a safer
  narrower band.
- Clamp public scores to `[0, 1]` after reranking.
- Preserve base scores internally for debugging and tests.
- Decay is enabled by explicit config or input policy. Do not silently change all
  retrieval behavior without an opt-in or clearly documented default.
- No new dependencies.
- No libSQL, sqlite-vector extension loading, cgo sqlite dependencies,
  `-tags=libsqlite3`, or legacy storage compatibility paths.
- Do not add keyword or substring heuristics for behavior decisions.
- Do not change memory lifecycle semantics. Stale/candidate filtering remains a
  separate governance layer unless explicitly revised in a later goal.
- Do not make search writes unbounded. Reinforcement must be best-effort,
  batched, or otherwise bounded.
- Keep the implementation generic across workspaces and repositories.
- Add semantic comments only at durable retrieval boundaries. Avoid broad,
  mechanical `Index:` blocks.
- Run `make check-doc-links` whenever markdown changes.

## Milestones

### Milestone 1: Pure Decay Scoring

Add a small pure package or file for decay math in the existing memory/storage
family.

Implement:

- Inputs:
  - base relevance score
  - last accessed timestamp
  - updated timestamp fallback
  - access count
  - current time supplied by caller or test
  - optional config with min/max multiplier
- Outputs:
  - base score
  - decay factor
  - final score clamped to `[0, 1]`
  - reason or bucket useful for tests/observability

Done when:

- Table-driven tests prove recent memories receive a boost.
- Idle memories receive a dampening factor no lower than the minimum.
- Very old strong matches can still beat weak recent matches.
- Missing access history falls back to `updated_at`.
- Time is injected; no hidden `time.Now()` in pure scoring logic.

### Milestone 2: Store-Level Rerank Helpers

Add a narrow store-level helper that reranks `[]storage.ScoredEntry` without
changing storage retrieval APIs more than necessary.

Implement:

- A helper that widens candidate pools before decay is applied.
- Stable sorting by final score, then base score, then existing deterministic
  tie-breakers.
- Optional score metadata if an existing result type can carry it cleanly;
  otherwise keep metadata local to tests and observability.

Done when:

- SQLite text search can apply decay after initial candidate retrieval.
- SQLite vector search can apply decay after cosine scoring.
- Turso vector search can retrieve a wider candidate pool, rerank in Go, and
  truncate after decay.
- Existing callers can opt out or keep current behavior during rollout.

### Milestone 3: Retrieval-Lane Wiring

Wire decay into the user-visible memory retrieval lanes.

Touch only the narrow paths needed:

- `memory/query`
- `code/semantic_search` memory scope
- remote/cross-workspace memory search if supported by the active store
- `memory relevant` only if the new scorer replaces duplicated local scoring

Done when:

- `memory/query` returns decayed scores when decay is enabled.
- `code/semantic_search` memory results use the same decay policy.
- Candidate widening happens before final truncation.
- Threshold behavior is explicit:
  - base thresholding may happen before decay when preserving old semantics is
    required
  - final score may fall below the original threshold only if this is documented
    and tested
- Result order is deterministic.

### Milestone 4: Search Reinforcement

Record access for surfaced search results without making retrieval fragile.

Implement:

- A narrow store method such as `RecordAccessBatch(ctx, workspace, names, at)`.
- Best-effort behavior: access recording failure should not fail search unless
  an explicit strict test mode is introduced.
- Bounded writes:
  - cap number of records touched per query
  - update only returned or selected results, not every candidate
  - avoid per-candidate write loops when batch update is available
- If adding a bounded access-history table, retain at most 20 touches per memory.
  If not adding history in the first slice, document that this release uses
  `last_accessed` plus counters as the first approximation.

Done when:

- A search that returns memory `A` updates `A`'s access metadata.
- Repeating the query can change ranking for near-ties.
- Search result access does not increment use/success/failure telemetry.
- Tests cover best-effort write failure behavior.

### Milestone 5: Observability, Docs, And Final Hardening

Expose enough operational visibility to understand why results moved.

Implement:

- Observability fields for:
  - decay enabled
  - candidate count before/after rerank
  - min/max/average decay factor
  - reinforcement count
  - reinforcement failures
- CLI/API output should not become noisy by default.
- Add or update docs explaining:
  - decay is a soft rerank, not pruning
  - lifecycle filtering is separate from decay
  - old memories can still surface when relevant
  - search access is a retrieval signal, not proof the memory was used

Done when:

- Logs/events can explain whether decay ran.
- Docs clarify how this differs from `memory relevant`, lifecycle state, and
  curator promotion.
- Semantic comments identify the durable retrieval/rerank boundary.

## Verification

Run narrow checks after each milestone:

```bash
go test ./internal/storage/memory -run 'Test.*Decay|Test.*Search|Test.*Relevant|Test.*Telemetry' -count=1
go test ./skills/memory_query -run 'TestMemoryQuery_.*' -count=1
go test ./skills/code_semantic_search -run 'Test.*Memory|Test.*SemanticSearch' -count=1
```

Run final checks before completion:

```bash
make build
go test ./internal/storage/memory ./skills/memory_query ./skills/code_semantic_search -count=1
go test ./internal/context/memorycore ./internal/context/contextengine -count=1
./bin/foxctl index anchors lint --workspace . --summary
make check-doc-links
git diff --check
```

If Turso vector capability is available locally, also run the relevant Turso
memory tests. If it is unavailable, document the skipped capability and ensure
SQLite/vector fallback behavior is covered.

## Done When

- Decay scoring is pure, bounded, deterministic, and tested.
- Named-memory search can apply decay as a search-time rerank.
- Search access reinforcement updates retrieval metadata without touching
  use/success/failure telemetry.
- `memory/query` and `code/semantic_search` share the same decay semantics.
- Public scores are clamped to `[0, 1]`.
- Candidate widening happens before rerank and truncation.
- Old but strongly relevant memories still surface.
- Recent near-ties rank higher after access.
- The implementation is small, composable, and avoids new dependencies.
- Final verification commands pass or any unavailable capability is clearly
  explained with the exact skipped command.

## Stop Conditions

- Stop after 3 failed attempts at the same failing check and summarize the
  blocker.
- Stop before broad schema rewrites, new dependencies, or a memory lifecycle
  redesign.
- Stop before changing default retrieval semantics globally if there is no
  explicit rollout flag, config, or documented default policy.
- Stop before altering public envelope `meta.*` fields.
- Stop before adding async workers, queues, or background daemons unless a
  bounded in-query reinforcement path is proven insufficient.

## Final Self-Review

Before marking the goal complete, write a short self-review covering:

- What would likely fail code review.
- Whether the diff stayed small and composable.
- Which retrieval paths are covered and which are not.
- Residual risks around write amplification or stale rankings.
- Confidence score from 1 to 5.
