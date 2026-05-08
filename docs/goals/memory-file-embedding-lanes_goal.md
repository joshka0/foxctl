# Goal: Harden Memory and File Embedding Lanes

## Goal

Make foxctl's memory and file embedding lanes distinct, safe to requeue, and
verifiable before any broad reembedding run.

Delivered behavior must prevent code-owned entries from being embedded through
the general memory lane, keep file-summary retrieval separate from raw
file/chunk embeddings, and give operators first-class queue/status/cleanup
commands for the file embedding lane.

## Context

- Current repo: `/Users/joshka/repos/personal/foxctl`.
- Recent local dogfood cleanup removed stale `file_embedding`,
  `file_embedding_chunk`, `file_summary`, `plan_sync_state`, `curator_report`,
  and old memory/semantic-file queue rows for workspace
  `74fe387351cc36fe653759d90570c6dd`.
- Current good state to preserve:
  - symbol queue has `0 pending`, `0 running`, `28171 completed`, `0 failed`
  - `symbol_embeddings` has `27583` Qwen 4096-dimensional embeddings
  - Turso named memory keeps `code_symbol`, `code_symbol_call`, and
    `code_symbol_file_meta`
- Primary findings from review:
  - `skills/embedding_memories/main.go` filters code-owned memory types only in
    the queue conversion path, not consistently in dry-run or inline embedding.
  - `cmd/foxctl/cmd/index.go` has memory reembedding paths that can still embed
    broad named-memory entries without the same code-owned filter.
  - `internal/intelligence/indexing/filesummary/search.go` uses broad memory
    vector search and accepts any `file://` entry, so future `file_embedding`
    and `file_embedding_chunk` rows can pollute file-summary retrieval.
  - `internal/intelligence/indexing/semantic/queue.go` stores semantic-file
    jobs in `semantic_embedding_queue_jobs`, while `embedding/queue` and
    `embedding/worker` only handle the symbol/memory queue table.
  - `internal/intelligence/indexing/semantic/indexer.go` still chunks large
    files by byte ranges and eagerly embeds all chunks before persistence.
- Related surfaces:
  - `skills/embedding_memories/main.go`
  - `skills/embedding_memories/main_test.go`
  - `skills/embedding_queue/main.go`
  - `skills/embedding_queue/main_test.go`
  - `skills/embedding_worker/main.go`
  - `skills/embedding_worker/main_test.go`
  - `cmd/foxctl/cmd/index.go`
  - `cmd/foxctl/cmd/index_test.go`
  - `cmd/foxctl/cmd/semantic_index.go`
  - `cmd/foxctl/cmd/semantic_index_test.go`
  - `internal/intelligence/indexing/embedding/`
  - `internal/intelligence/indexing/embedqueue/`
  - `internal/intelligence/indexing/filesummary/`
  - `internal/intelligence/indexing/semantic/`
  - `internal/storage/memory/`

## Constraints

- Keep symbol indexing and symbol embeddings working. Do not delete, requeue, or
  rewrite `symbol_embeddings` or `code_symbol` data except through targeted tests
  or explicit smoke fixtures.
- Do not run a full memory/file reembedding pass as part of this goal. Run only
  small, bounded smoke requeues after the hardening work is in place.
- Use Turso as the canonical SQLite-family storage path. Do not add libSQL,
  sqlite-vector extension loading, cgo sqlite dependencies, or legacy
  compatibility layers.
- Do not add dependencies without explicit approval.
- Preserve protocol envelope shape and `meta.*` fields.
- Do not use keyword heuristics for behavior routing. Candidate selection must
  use explicit types, scopes, structured payload fields, or tested policy
  functions.
- Keep changes local to embedding, memory, file-summary, semantic-index, queue,
  and directly related tests/docs.
- Preserve existing user or prior-agent work in the dirty tree. Do not revert
  unrelated files.
- Prefer small, composable functions with testable policy boundaries.
- Use semantic comments only where they improve retrieval for durable contracts;
  do not add broad mechanical `Index:` blocks.

## Milestones

### Milestone 1: Memory Candidate Policy

Done when:

- There is one shared policy for deciding whether a named-memory entry is
  eligible for the general memory embedding lane.
- The policy is allowlist-oriented or otherwise explicit enough to prevent
  `code_symbol`, `code_symbol_call`, `code_symbol_file_meta`, `file_summary`,
  `symbol_summary`, `file_embedding`, `file_embedding_chunk`, and other
  code-owned records from entering the memory lane.
- `embedding/memories` uses the same policy for dry-run, queue mode, inline
  mode, `process_all`, and non-`process_all`.
- `foxctl index` memory reembedding and memory enqueue paths use the same policy
  for normal and `force` paths.
- Tests prove code-owned entries are skipped in all memory embedding entrypoints.
- Tests prove real memory facts, decisions, gotchas, learnings, and notes can
  still be queued or embedded.

### Milestone 2: File Summary Retrieval Boundary

Done when:

- `filesummary.SearchFileSummaries` uses type-specific retrieval for
  `file_summary` entries instead of broad memory search over all `file://`
  entries.
- File-summary path extraction prefers the structured
  `symbol.FileSummaryResult.FilePath` payload and only falls back to name
  parsing when safe.
- Tests prove `file_embedding` and `file_embedding_chunk` entries never appear
  in file-summary/tree retrieval even when they have higher vector similarity.
- Tests prove `file://<workspace>/<nested/path.go>` returns the full nested
  path, not a truncated suffix.
- Tree mode in `code/semantic_search` still merges file summaries with symbol
  groups when summaries exist.

### Milestone 3: File Queue Operations Are First-Class

Done when either the unified or explicitly separate design is implemented and
documented:

- Preferred hard cut: `semantic_file` jobs are handled by the same queue
  operation surface as symbol and memory jobs, with `stats`, `recover_stale`,
  `cleanup`, and purge/delete operations filtering by `kind`.
- Acceptable narrow alternative: semantic-file queue remains a separate table,
  but has first-class CLI/skill operations for `stats`, `recover_stale`,
  `cleanup`, and purge/delete so raw SQLite is no longer needed for normal
  operations.
- `embedding/queue` and/or `semantic_index` commands clearly report which lane
  and table they are operating on.
- Tests cover queue stats and cleanup for symbol, memory, and semantic-file
  jobs independently.
- A local smoke confirms all queues can be empty without deleting existing
  symbol embeddings.

### Milestone 4: File Embedding Chunk Planner

Done when:

- Byte-range chunking is no longer the only planner for file embeddings.
- The file embedding lane has a chunk-planning boundary that can emit chunks by
  language-aware symbol/function/class spans when such structure is available.
- The planner can embed small function or method spans separately and represent
  class/type context where applicable for the primary indexed languages:
  Go, TypeScript/JavaScript, Python, Rust, Elixir, and C#.
- For unsupported languages, the planner falls back to bounded byte or line
  chunks with deterministic spans.
- Language-aware planning is not considered a Go-only feature. The first pass
  should prove adapters for other indexed languages used by foxctl workspaces,
  including TypeScript/JavaScript, Python, Rust, Elixir, and C# where parsers or
  symbol boundaries are available.
- The chunk planner boundary should stay adapter-shaped so adding another
  language does not turn the semantic indexer into a single language-specific
  file.
- Chunk metadata records chunk kind, file path, digest, span unit/start/end,
  language, size, and any symbol identifiers used to produce the chunk.
- The generated embedding text avoids synthetic noise such as "Chunk 6/15 of
  file" and does not prepend operational labels as semantic content.
- Tests cover small file, large file, Go function spans, fallback chunks,
  deleted files, max-size skips, and deterministic chunk IDs.

### Milestone 5: Observability and Bounded Smoke

Done when:

- The relevant commands emit structured observability for candidate counts,
  skipped-by-policy counts, queue counts by kind, chunk planner counts, chunk
  size distribution, provider/model/dimensions, and failures by phase.
- Operator-facing output distinguishes skipped policy decisions from failures;
  do not label expected skips as errors.
- A bounded local smoke with LM Studio/Qwen is documented in the final report:
  - enqueue a small memory fact set
  - enqueue a small file set including one large file
  - drain with low parallelism
  - verify symbol search still works
  - verify memory search returns only memory facts
  - verify file-summary/tree retrieval does not return file embedding chunks
- Do not start the full repo reembedding run until these smoke checks pass.

Future hardening note before broad non-Go reembedding:

- Before broad non-Go reembedding, review whether each language adapter should
  use parser-backed repoindex symbol boundaries instead of line-aware spans for
  better class/member coverage. Unsupported-language byte fallback is acceptable
  for safety, but it is not the desired final retrieval quality.
- Workspace-scoped smoke commands should fail clearly when skill JSON
  `workspace` conflicts with the outer `foxctl run --workspace` execution
  context; silent empty retrievals are not acceptable operator feedback.

## Verification

Run focused tests after each milestone:

```bash
go test ./skills/embedding_memories ./skills/embedding_queue ./skills/embedding_worker
go test ./internal/intelligence/indexing/filesummary ./internal/intelligence/indexing/semantic ./internal/intelligence/indexing/embedding ./internal/intelligence/indexing/embedqueue
go test ./cmd/foxctl/cmd -run 'Test.*Embedding|Test.*Semantic|Test.*FileSummar|Test.*Queue|Test.*Memory'
```

Run broader checks before final:

```bash
go test ./skills/embedding_memories ./skills/embedding_queue ./skills/embedding_worker ./skills/code_semantic_search
go test ./internal/intelligence/indexing/filesummary ./internal/intelligence/indexing/semantic ./internal/intelligence/indexing/embedding ./internal/intelligence/indexing/embedqueue ./internal/storage/memory
make build
```

Run local queue and retrieval smoke with explicit Qwen/Turso environment:

```bash
env \
  FOXCTL_MEMORY_DB_URL= \
  FOXCTL_MEMORY_DB_DRIVER=turso \
  FOXCTL_MEMORY_DB_PATH=/Users/joshka/.foxctl/storage/memory.turso-qwen4096-dogfood \
  FOXCTL_MEMORY_VECTOR_DIMS=4096 \
  FOXCTL_VECTOR_DIMS=4096 \
  FOXCTL_EMBEDDING_PROVIDER=openai_compat \
  FOXCTL_EMBEDDING_MODEL=text-embedding-qwen3-embedding-8b \
  FOXCTL_EMBEDDING_MODEL_MEMORY=text-embedding-qwen3-embedding-8b \
  FOXCTL_EMBEDDING_MODEL_SYMBOLS=text-embedding-qwen3-embedding-8b \
  FOXCTL_EMBEDDING_BASE_URL=http://127.0.0.1:1234/v1 \
  FOXCTL_EMBEDDING_API_KEY=lm-studio \
  ./bin/foxctl run embedding/queue --ephemeral --input '{"operation":"stats","workspace_id":"74fe387351cc36fe653759d90570c6dd"}'
```

If LM Studio is not running, skip live embedding smoke only after recording the
exact failure and proving the unit/integration tests cover the changed behavior.

Done when:

- All mandatory milestone tests pass.
- Queue operations can show and clean memory and file lanes without raw SQLite.
- Memory embedding candidates exclude code-owned records in tests and live dry
  runs.
- File-summary retrieval excludes `file_embedding` and `file_embedding_chunk`
  rows in tests.
- No full reembedding run has been started by this goal.
- Final self-review lists remaining risks, commands run, and confidence.

## Stop Conditions

- Stop after three failed attempts at the same compile or test failure and
  summarize the blocker with exact command output.
- Stop before adding a new dependency, changing public storage schema broadly,
  or replacing the memory store contract beyond the scoped lane hardening.
- Stop before deleting or rewriting existing symbol embeddings outside a
  disposable test fixture.
- Stop if a proposed fix requires changing protocol envelopes, `meta.*`
  semantics, or broad workspace identity rules.
- Stop if local live embedding would require secrets or network access beyond
  the configured local LM Studio OpenAI-compatible endpoint.
