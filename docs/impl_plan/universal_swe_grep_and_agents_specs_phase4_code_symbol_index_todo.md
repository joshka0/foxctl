# Phase 4 – Code Symbol Index v1 Todo Spec

This spec breaks down Phase 4 of `universal_swe_grep_and_agents` into concrete
steps focused on the **code symbol index** (symbol DAG) for Go-first, with
per-symbol incremental updates.

- Phase 2 established the post-review harness and canonical `PostReviewEvent`.
- Phase 3 established the semantic file index over `file_embedding` entries.
- Phase 4 builds a symbol-level index (symbols + calls + file_meta) that
  refreshes off the same post-review pipeline.

> **Cross-refs**
>
> - Impl plan: `docs/impl_plan/universal_swe_grep_and_agents.md` (Phase 4)
> - Codemap: `docs/impl_plan/universal_swe_grep_and_agents_codemap.md` (Phase 4)
> - Testing plan: `docs/impl_plan/universal_swe_grep_and_agents_testing.md`
>   (Phase 4)
> - Specs: `docs/spec/code_symbol_index_and_swe_grep.md`,
>   `docs/spec/semantic_file_index.md`, `docs/spec/review_gate.md`,
>   `docs/spec/post_review_harness.md`, `docs/spec/core_profile_v1.md`
> - Job & skill infra:
>   - Job system + WFQ: see job system codemaps.
>   - Skill/runtime: see agentctl skill system codemaps.

---

## A. Data Model & Storage

Goal: define a **stable, implementation-backed data model** for symbols, calls,
and file freshness that matches the spec and can evolve without breaking
consumers.

### A1. Symbols, calls, and file_meta schema

- [x] Define in-memory schema for:
  - `symbols` → `Symbol`, `Result`, `Source` in
    `internal/indexing/symbol/types.go` (stored as named memory entries with
    `type = "code_symbol"`).
  - `calls` → `CallEdge` in `types.go` (currently conceptual; persistence via
    named memory still to be wired).
  - `file_meta` → `FileMeta` in `types.go` (`type = "code_symbol_file_meta"`).
- [x] Cross-check these structs against `code_symbol_index_and_swe_grep.md` §3
      and update docs/comments if any fields diverge (without changing
      wire-level contracts).
  - Updated Go docstrings in `internal/indexing/symbol/types.go` to reference
    spec sections and note implementation additions (`StartLine`, `EndLine`,
    `Documentation`, `symbol_count`) permitted by "Implementations MAY add
    additional columns".
  - Confirmed `embedding` field is stored via named memory's `Embedding` column,
    not in the `Symbol` struct itself.
- [x] Explicitly document how named memory entry types (`code_symbol`,
      `code_symbol_call`, `code_symbol_file_meta`) map onto the conceptual
      `symbols`, `calls`, and `file_meta` tables in the spec.
  - Added package-level doc in `types.go` with named memory type mapping.
  - Added §3.4 "Named Memory Storage Mapping" to the spec with type mapping
    table and entry format documentation.

### A2. Identifiers and entry naming

- [x] Implement stable symbol IDs and entry names:
  - `ID(file_path, symbol_name)` → `"<file_path>:<symbol_name>"`.
  - `EntryName(workspace, file_path, symbol_name)` →
    `"symbol://<workspace>/<file_path>:<symbol_name>"`.
  - `FileMetaEntryName(workspace, file_path)` →
    `"symbol-meta://<workspace>/<file_path>"`.
  - Implemented in `internal/indexing/symbol/types.go` with tests in
    `indexer_test.go`.
- [x] Write a short design note (or extend the spec) describing identifier
      stability guarantees and how renames should be handled:
  - When files move but symbols are logically the same.
  - When symbol names change vs. when they are deleted.
  - Added §3.1.1 "ID Stability and Renames (v1 Behavior)" to the spec.
  - Added detailed docstring on `ID()` function in `types.go` documenting v1
    behavior for file path changes, symbol renames, modifications, and
    deletions.

---

## B. Indexer Behavior & Incremental Updates

Goal: implement **Go-first, per-symbol incremental indexing** that matches
`code_symbol_index_and_swe_grep.md` §4 while keeping the implementation CGO-free
and testable.

### B1. Language extractors (Go-first)

- [x] Implement a Go-only extractor that does not require Tree-sitter:
  - `GoExtractor` in `internal/indexing/symbol/extractor_go.go` using `go/ast`.
  - Extracts functions, methods, types, interfaces, variables, and constants.
  - Populates `Symbol` (ids, kinds, byte ranges, doc comments, signatures,
    `BodyDigest`).
  - Tested in `TestGoExtractor_ExtractFunctions` and
    `TestGoExtractor_ExtractTypes`.
- [x] Document the Go extractor behavior as the v1 reference for "Go-first" and
      call out that Tree-sitter may be introduced later without changing the
      external data model.
  - Added comprehensive docstrings to `GoExtractor` in `extractor_go.go`
    explaining extracted symbol kinds, naming conventions, body digest
    computation, call extraction, and error handling.
- [x] Define an explicit extension path for additional languages via
      `ExtractorRegistry` (what it means to be "supported").
  - Added docstrings to `Extractor` interface and `ExtractorRegistry` in
    `extractor.go` with implementation requirements and extension path example.

### B2. Incremental updates per file and symbol

- [x] Implement file-level freshness via `FileMeta` and content hash:
  - `fileChanged()` consults stored `FileMeta` and skips unchanged files.
  - Tested in `TestIndexer_ErrUnchanged` and
    `TestIndexer_Index_IncrementalUpdate`.
- [x] Implement **per-symbol** incremental updates per §4.3 of the spec:
  - For changed files, compare new `Symbol.BodyDigest` with existing entries.
  - Reuse embeddings and metadata when `body_digest` is unchanged.
  - Only recompute and overwrite entries for symbols whose bodies changed or
    were added/removed.
  - Added `SymbolDigests` map to `FileMeta` for efficient per-symbol comparison.
  - Implemented `loadFileMeta()` and `deleteSymbol()` helpers in `indexer.go`.
  - Added `TestIndexer_PerSymbolIncrementality` and
    `TestIndexer_SymbolDeletion`.
- [x] Ensure call graph updates follow the incremental rules:
  - For changed symbols, recompute outgoing call edges.
  - Remove edges for deleted symbols and stale targets.
  - Call extraction is performed per-symbol during indexing; deleted symbols
    have their entries (including call references) removed.

### B3. Handling large files ("God files")

- [x] Use `Config.MaxFileLOC` and/or size thresholds to implement large-file
      handling per `code_symbol_index_and_swe_grep.md` §4.2:
  - Avoid treating entire large files as single symbols.
  - Extract per-symbol entries and (optionally) a `KindFileSummary` symbol.
  - Implemented `MaxFileKB` check at start of `indexFile()` — files exceeding
    the size limit are skipped before parsing.
  - Implemented `MaxFileLOC` check for files with no extractable symbols.
  - Added `countLines()` helper for LOC calculation.
- [x] Add targeted tests for large-file behavior in `indexer_test.go` (e.g. many
      small functions vs. a few large ones), ensuring performance remains
      acceptable and incremental behavior still holds.
  - Added `TestIndexer_LargeFileThreshold` for `MaxFileKB` enforcement.

---

## C. Jobs, Post-Review Integration, and Optional Triggers

Goal: ensure the symbol index is **reliably refreshed** from post-review events
and can be driven via jobs when needed, without diverging from the core
pipeline.

### C1. Post-review integration

- [x] Integrate symbol indexer with the Phase 2 post-review handler:
  - `internal/indexing/symbol/factory.go` – `Factory` + `RegisterWithHandler`.
  - `internal/indexing/indexer.go` – `PostReviewHandler` fanout to symbol
    indexer via `Indexer` interface.
  - `internal/indexing/symbol/indexer.go` – `Indexer.Index` consumes
    `PostReviewEvent` and updates named memory.
- [ ] Document the end-to-end flow:
  - Review → `ReviewArtifact` → `PostReviewEvent` → `PostReviewHandler` → symbol
    indexer.
  - How `Source` (`task_id`, `review_id`, `reason`) is filled from events.

### C2. Jobs and `code_symbol_index.update_files`

- [ ] Define the job contract for `code_symbol_index.update_files` in terms of
      existing inputs:
  - Reuse the same conceptual input as semantic index jobs (`workspace_id`,
    files `{path, digest, change_kind}`, task/review ids).
  - No new wire-level fields; stick to Core Profile v1 envelopes.
- [ ] Implement a job-backed entrypoint that:
  - Accepts a post-review-like payload.
  - Uses the job system and WFQ scheduler (see job system codemaps) to run the
    symbol indexer over changed files.
  - Persists job results and logs for observability.
- [ ] Decide how this job entrypoint interacts with the `PostReviewHandler`
      modes (`inline` vs `jobs`) so Phase 2 + 4 remain consistent.

### C3. Optional heuristic triggers (out of strict v1 scope)

- [ ] Sketch but do not implement yet: heuristic triggers based on git commits
      or large unreviewed edits, and note any additional invariants they would
      require.

---

## D. Tests, Golden Fixtures, and Observability

Goal: make the symbol index implementation **observable and regression-safe**
with focused unit, integration, and golden tests.

### D1. Unit tests (symbols, calls, file_meta)

- [x] Ensure basic unit coverage for:
  - Symbol extraction (functions, methods, types, interfaces) – existing tests
    in `internal/indexing/symbol/indexer_test.go`.
  - Naming helpers (`ID`, `EntryName`, `FileMetaEntryName`).
  - File meta and unchanged-file handling.
- [ ] Add explicit unit tests for call extraction:
  - Verify that `ExtractCalls` produces reasonable `CallEdge` candidates for
    simple Go fixtures (self-contained callers/callees).

### D2. Incrementality tests

- [x] File-level incrementality:
  - `TestIndexer_Index_IncrementalUpdate` verifies content hash changes and
    symbol counts.
- [ ] Per-symbol incrementality (after B2 is implemented):
  - Add tests that modify only one function/method in a file and assert that
    only that symbol’s embedding/digest changes.
  - Add tests that add/remove functions and ensure stale symbols are removed.

### D3. Golden symbol index dumps

- [ ] Add golden fixtures for symbol index contents:
  - Store canonical dumps (e.g. JSON) of `code_symbol` + `code_symbol_call`
    entries for small Go fixture packages under `test/golden/symbol_index/`.
- [ ] Add tests that:
  - Re-index fixtures and compare the resulting entries to goldens (allowing for
    explicitly documented, intentional changes only).
  - Validate that changes in symbol layout are caught in review.

### D4. Integration tests (post-review → symbol index)

- [ ] Add integration tests that:
  - Simulate a post-review event touching multiple files.
  - Run the post-review handler with both semantic and symbol indexers
    registered.
  - Assert that only changed files are parsed and updated in the symbol index.
  - Optionally verify basic call graph structure across files.

### D5. Logging and metrics

- [ ] Decide on minimal logging fields for symbol indexing:
  - workspace / task / review ids, file path, language, symbol counts, unchanged
    vs updated files.
- [ ] Add metrics hooks (aligned with existing metrics infra) for:
  - Counters: `symbol_index_files_indexed_total`,
    `symbol_index_files_failed_total`.
  - Histograms: `symbol_index_file_index_duration_seconds`.
- [ ] Ensure these metrics integrate cleanly with the broader
      indexing/post-review observability story started in Phase 2 and extended
      in Phase 3.

---

## E. Future: Symbol Embeddings & Semantic Symbol Search (Post-MVP)

> **Note:** The tasks in this section are explicitly **out of scope for the
> Phase 4 v1 landing**. They are recorded here to align with
> `code_symbol_index_and_swe_grep.md` §3.1.2 and the Phase 5 SWE Grep work.

- [ ] E1. Symbol embedding text and provider reuse
  - Define how to construct embedding text per symbol (signature + doc comment,
    optionally a truncated body slice) in a way that is language-agnostic and
    stable across re-indexing.
  - Ensure symbol embedding jobs reuse the existing embedding provider
    abstraction used by the semantic file index (e.g. Gemini-based
    `EmbeddingProvider`), rather than introducing a parallel provider stack.
- [ ] E2. Symbol embedding jobs (conceptual)
  - Specify optional internal job types (e.g. `symbol_index.init_symbols`,
    `symbol_index.update_symbols`) that:
    - Take
      `(workspace_id, files[{path, digest, change_kind}], task_id,
      review_id, reason)`
      as input (mirroring semantic index jobs).
    - Re-embed only symbols whose `body_digest` or provider model has changed,
      and remove embeddings for deleted symbols.
  - Keep these jobs purely internal; no new public wire contracts beyond Core
    Profile v1 job envelopes.
- [ ] E3. Semantic symbol search interface (owned by later phases)
  - Sketch a retrieval surface (e.g. `code/symbol_search` skill or an extension
    of existing search tools) that:
    - Accepts a natural-language/code query and optional filters.
    - Uses symbol embeddings (when present) plus the symbol/call graph to return
      high-quality `(symbol_id, file_path)` candidates.
    - Integrates cleanly with the Phase 5 SWE Grep funnel, where candidates are
      further narrowed to snippets.

---

## Open Questions / To Discuss

- How aggressively should we handle symbol ID stability across full-file
  renames, especially when file paths change but contents do not?
- Do we need additional indexing modes (e.g., package-level summaries) in v1, or
  can they be deferred to a later phase?
- What default `MaxFileLOC` and `MaxFileKB` thresholds should we use for
  Go-first, and how should they be surfaced in configuration?
