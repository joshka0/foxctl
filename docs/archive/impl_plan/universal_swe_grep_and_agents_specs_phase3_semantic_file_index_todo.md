# Phase 3 – Semantic File Index v1 Todo Spec

This spec breaks down Phase 3 of `universal_swe_grep_and_agents` into concrete
steps focused on the **semantic file index**: stable embeddings stored in named
memory, driven by post-review events and embedding jobs.

- Phase 2 established the post-review harness and canonical `PostReviewEvent`.
- Phase 3 consumes those events and job inputs to maintain the semantic index
  (`file_embedding` + `file_embedding_chunk`) with stable names and chunking.

> **Cross-refs**
>
> - Impl plan: `docs/impl_plan/universal_swe_grep_and_agents.md` (Phase 3)
> - Codemap: `docs/impl_plan/universal_swe_grep_and_agents_codemap.md` (Phase 3)
> - Testing plan: `docs/impl_plan/universal_swe_grep_and_agents_testing.md`
>   (Phase 3)
> - Specs: `docs/spec/semantic_file_index.md`,
>   `docs/spec/review_semantic_trajectory_specs.md`,
>   `docs/spec/core_profile_v1.md`
> - Related: `docs/spec/post_review_harness.md`,
>   `docs/spec/code_symbol_index_and_swe_grep.md`

---

## A. Data Model & Named Memory Schema

Goal: make the **semantic index data model** (named memory entries + config)
explicit and stable across re-runs and config changes.

### A1. File embedding & chunk schema

- [x] Define named-memory schema for:
  - `type = "file_embedding"` and `"file_embedding_chunk"`.
  - `FileEmbeddingResult` and `ChunkEmbeddingResult` structs per
    `semantic_file_index.md` data model.
  - Implemented in `internal/indexing/semantic/types.go`.
- [ ] Ensure schema is fully documented and aligned with spec:
  - Cross-check fields (`path`, `digest`, `language`, `embedding`,
    `chunk_count`, `chunking_config_hash`, `source`, etc.) against
    `docs/spec/semantic_file_index.md`.
  - Add or refine Go docstrings where needed (without inventing new wire
    contracts).
- [ ] Define and document invariants:
  - `file_embedding` entries exist for all indexed files (chunked or not).
  - `file_embedding_chunk` entries only for chunked files.
  - `EmbeddingSource` fields (`task_id`, `review_id`, `reason`) reflect
    provenance from post-review events and jobs.

### A2. Naming & chunking config stability

- [x] Implement canonical naming functions:
  - `FileEmbeddingName(workspace, path)` → `file://<workspace>/<path>`.
  - `ChunkEmbeddingName(workspace, path, chunkID, cfgHash)` →
    `file://<workspace>/<path>#chunk-<id>?cfg=<hash>`.
  - Implemented in `internal/indexing/semantic/types.go`.
- [x] Implement `ChunkingConfigHash()` on `semantic.Config` to detect when chunk
      boundaries must be recomputed.
- [ ] Document naming + hashing behavior in the spec and/or README:
  - Explain how `chunking_config_hash` participates in deduping and
    invalidation.
  - Call out compatibility expectations when config changes (e.g. new chunks use
    new hash; old entries may be left or marked deprecated).

---

## B. Embedding Jobs & Execution

Goal: define and validate the **embedding jobs** that initialize and update the
semantic index.

### B1. Job contracts (`semantic_index.*`)

- [ ] Confirm and document job entry points in `semantic_file_index.md` and code
      (e.g. `semantic_index.init_files`, `semantic_index.update_files`):
  - Inputs: workspace id, file set (paths / glob), chunking config, provider.
  - Outputs: summary of files indexed/updated, error list, CAS artifacts.
- [ ] Ensure job argument structs and result structs live in a small, focused
      package and are used consistently by CLI + jobs + tests.
- [ ] Align job naming and error codes with `code_symbol_index_and_swe_grep.md`
      so future phases see consistent behavior.

### B2. Idempotence, retries, and error handling

- [ ] Specify idempotence rules:
  - Re-running init/update jobs on the same workspace + paths must not create
    duplicate named entries.
  - Jobs should be safe to retry after transient failures.
- [ ] Define error taxonomy for semantic jobs (per spec §11):
  - Distinguish configuration errors vs. embedding provider failures vs. storage
    errors.
  - Map to canonical error codes used by the CLI + envelopes.
- [ ] Ensure job implementation wraps errors with enough context (workspace,
      path, job id) and never panics in normal operation.

---

## C. Integration: Post-Review, Factory, and CLI

Goal: make it easy to **wire semantic indexing** from both post-review events
and explicit CLI entrypoints.

### C1. Post-review integration (overseer + handler)

- [x] Integrate semantic indexer with the Phase 2 post-review handler:
  - `internal/indexing/semantic/factory.go` – `Factory` + `RegisterWithHandler`.
  - `internal/indexing/handler.go` – `PostReviewHandler` fanout to semantic
    indexer via `Indexer` interface.
  - `internal/indexing/semantic/integration_test.go` – integration test
    `TestSemanticIndexerWithPostReviewHandler`.
- [ ] Document the end-to-end flow:
  - Review becomes `ok` → overseer emits `PostReviewEvent` → post-review handler
    filters + routes files → semantic indexer updates named memory.
  - Clarify how `EmbeddingSource` is populated from `PostReviewEvent`.

### C2. Optional CLI (`agentctl semantic-index ...`)

- [x] Design and implement a CLI entrypoint for semantic indexing:
  - `agentctl semantic-index init` – run init job(s) over a workspace subset.
  - `agentctl semantic-index update` – reindex changed files.
  - Flags for chunking config, include/exclude globs, dry-run.
- [x] Ensure CLI emits envelopes aligned with `core_profile_v1` and reuses
      existing job primitives (no new wire contracts):
  - Jobs submitted via existing job store + WFQ scheduler (when applicable).
  - Envelopes validated with existing golden harness.
- [ ] Add minimal UX docs under `docs/start/` (indexing quickstart), pointing
      back to this Phase 3 spec and `semantic_file_index.md`.

---

## D. Tests, Golden Artifacts, and Observability

Goal: validate the semantic index implementation with focused tests and
fixtures, so later phases can build on a stable base.

### D1. Unit tests (naming + chunking)

- [x] Add unit tests for naming and chunk stability:
  - For a given `(workspace, path, chunk_bytes, overlap, cfg hash)`,
    `FileEmbeddingName` / `ChunkEmbeddingName` are stable across re-runs.
  - `ChunkingConfigHash` changes only when relevant config fields change.
- [x] Add unit tests for config-change behavior:
  - New `chunking_config_hash` yields new chunk entries.
  - Old entries are deprecated or cleaned up per spec (document behavior).

### D2. Job tests (init/update behavior)

- [x] For `semantic_index.init_files` and `semantic_index.update_files`, add
      small fixture-based tests that assert:
  - Correct number of `file_embedding` / `file_embedding_chunk` entries.
  - `result.digest` (if present) matches CAS contents or file snapshot.
- [x] Add update-behavior tests:
  - Editing a non-chunked file reuses the same named entry, updates digest and
    embedding.
  - Editing a chunked file reuses `chunk.id` and spans, but updates embeddings
    and config hash when chunking config changes.

### D3. Golden job results

- [x] Add golden job result envelopes for semantic index jobs:
  - Store under `test/golden/envelopes/semantic-index-*.json`.
  - Include summary + CAS artifact metadata, not full embeddings.
- [ ] Extend `test/golden/golden_test.go` (or a sibling) to validate:
  - Envelope structure against `core_profile_v1`.
  - Semantic job-specific fields against `semantic_file_index.md`.

### D4. Logging and metrics

- [ ] Decide on minimal logging fields for semantic indexing:
  - workspace / task / review ids, file path, chunking config hash, files
    indexed/failed, provider model.
- [ ] Add metrics hooks (compatible with existing metrics infra) to track:
  - Counters: `semantic_index_files_indexed_total`,
    `semantic_index_files_failed_total`.
  - Histograms: `semantic_index_job_duration_seconds`.
- [ ] Ensure logs and metrics are wired into the same observability story as the
      post-review harness so regressions are easy to spot.

---

## Open Questions / To Discuss

- How aggressively should old chunk entries be cleaned up when config changes
  (e.g. immediate deletion vs. soft-deprecation)?
- What default chunking parameters (bytes/overlap) should be used for
  medium-sized repos, and how are they surfaced in config?
- How should semantic index jobs interact with the WFQ scheduler namespaces
  (e.g. one namespace per workspace vs. per-indexer)?
