# Phase 2 Implementation: Post-Review Pipeline Harness

**Date:** 2025-01-20

## Summary

Implemented Phase 2 of the Universal SWE Grep & Agentic RAG integration plan.
This establishes the generalized post-review indexer harness that will be used
by semantic file index, code symbol index, and future indexers.

## Changes

### 2a. Defined PostReviewEvent struct and Indexer interface

New package: `internal/intelligence/indexing/`

**Types:**

- `PostReviewEvent` – emitted when a review passes, contains workspace_id,
  files[], task_id, review_id
- `FileChange` – represents a single affected file (path, digest, size_bytes,
  language, change_kind)
- `ChangeKind` – enum: added, modified, deleted, renamed
- `Indexer` – interface that indexers implement with `ID()` and
  `Index(ctx, event)` methods
- `IndexerResult` – contains files_indexed, files_skipped, files_failed,
  failures[]
- `IndexerFailure` – per-file error with error_code and error_message
- `IndexerConfig` – per-indexer configuration (id, kind, enabled,
  include/exclude globs, max_file_kb)
- `PostReviewConfig` – top-level config (enabled, async, indexers[])

### 2b. Implemented PostReviewHandler

The `PostReviewHandler` coordinates post-review indexers:

- Receives `PostReviewEvent` when a review passes
- Filters files for each indexer based on include/exclude globs and max file
  size
- Runs indexers synchronously or asynchronously based on configuration
- Collects and aggregates results from all indexers
- Handles per-indexer errors gracefully without failing the entire pipeline

Key methods:

- `NewPostReviewHandler(cfg, logger)` – creates handler
- `RegisterIndexer(indexer)` – adds an indexer implementation
- `Handle(ctx, event)` – processes a post-review event

### 2c. Added configuration for indexing.post_review.indexers

Extended `internal/platform/config/config.go`:

- Added `IndexingSettings` struct with `PostReview` field
- Added `PostReviewSettings` with enabled, async, and indexers[] fields
- Added `IndexerSettings` for per-indexer configuration
- Set defaults: `indexing.post_review.enabled=false`, `async=true`

Example configuration (in config.yaml):

```yaml
indexing:
    post_review:
        enabled: true
        async: true
        indexers:
            - id: semantic_embed
              kind: semantic_file_index
              enabled: true
              include_globs: ["**/*.go", "**/*.md"]
              exclude_globs: ["vendor/**", "dist/**"]
              max_file_kb: 256
            - id: code_symbol_dag
              kind: code_symbol_dag
              enabled: true
              include_globs: ["**/*.go"]
```

### 2d. Added comprehensive tests

Tests in `internal/intelligence/indexing/handler_test.go`:

- `TestPostReviewHandler_Handle_Disabled` – skips when disabled
- `TestPostReviewHandler_Handle_NoFiles` – skips when no files
- `TestPostReviewHandler_Handle_NoActiveIndexers` – skips when no indexers
  configured
- `TestPostReviewHandler_Handle_SingleIndexer` – basic indexer invocation
- `TestPostReviewHandler_Handle_IndexerError` – graceful error handling
- `TestPostReviewHandler_Handle_IncludeGlobs` – file filtering by extension
- `TestPostReviewHandler_Handle_ExcludeGlobs` – file exclusion patterns
- `TestPostReviewHandler_Handle_MaxFileKB` – file size filtering
- `TestPostReviewHandler_Handle_MultipleIndexers` – concurrent indexer support
- `TestPostReviewHandler_RegisterIndexer_Duplicate` – prevents duplicate IDs
- `TestPostReviewResult_TotalFilesIndexed` – result aggregation

## Files Added

- `internal/intelligence/indexing/types.go` – PostReviewEvent, Indexer interface, config
  types
- `internal/intelligence/indexing/handler.go` – PostReviewHandler implementation
- `internal/intelligence/indexing/handler_test.go` – comprehensive tests

## Files Modified

- `internal/platform/config/config.go` – added IndexingSettings,
  PostReviewSettings, IndexerSettings

## Spec Alignment

This implementation aligns with:

- `docs/spec/semantic_file_index.md` §8.2 (Post-Review Indexer Abstraction)
- `docs/impl_plan/universal_swe_grep_and_agents.md` (Phase 2)

## Next Steps

- Phase 3: Semantic File Index v1 (implements Indexer interface)
- Phase 4: Code Symbol Index v1 (implements Indexer interface)
- Phase 5: SWE Grep Skill
