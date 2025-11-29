# Phase 3 Implementation: Semantic File Index v1

**Date:** 2025-01-20

## Summary

Implemented Phase 3 of the Universal SWE Grep & Agentic RAG integration plan.
This establishes the semantic file index as a post-review indexer that stores
file embeddings in named memory entries.

## Changes

### 3a. Defined file_embedding memory schema + helper lib

New package: `internal/indexing/semantic/`

**Types (`types.go`):**

- `FileEmbeddingResult` – structured metadata for `file_embedding` entries
- `ChunkEmbeddingResult` – structured metadata for `file_embedding_chunk`
  entries
- `ChunkInfo` – chunk identification (id, index, of, span)
- `ChunkSpan` – byte or line range within a file
- `EmbeddingSource` – provenance tracking (task_id, review_id, actor, reason)
- `Config` – indexer configuration (chunk_bytes, overlap, max_file_kb, globs)

**Constants:**

- `FileEmbeddingType = "file_embedding"`
- `FileEmbeddingChunkType = "file_embedding_chunk"`

**Helpers:**

- `FileEmbeddingName(workspace, path)` → `file://<workspace>/<path>`
- `ChunkEmbeddingName(workspace, path, chunkID, configHash)` →
  `file://<workspace>/<path>#chunk-<id>?cfg=<hash>`
- `Config.ChunkingConfigHash()` – stable hash for chunk boundary detection
- `MarshalResult()`, `UnmarshalFileResult()`, `UnmarshalChunkResult()`

### 3b. Implemented SemanticFileIndexer (Indexer interface)

**Provider interface (`provider.go`):**

- `EmbeddingProvider` – interface for embedding backends
- `NoOpProvider` – stub provider for testing and when vector support is disabled

**Indexer (`indexer.go`):**

- Implements `indexing.Indexer` interface
- Handles single-file and chunked file indexing
- Supports file deletion (removes embedding entries)
- Idempotent upsert semantics via memory store
- Chunk overlap support for context preservation

### 3c. Embedding jobs integrated via Index() method

The `Index(ctx, PostReviewEvent)` method processes all files in the event:

- Reads file content from workspace
- Decides chunking based on config and file size
- Generates embeddings via provider
- Saves to memory store with full metadata

### 3d. Wired to post-review handler

**Factory (`factory.go`):**

- `Factory` – creates indexers from config
- `RegisterWithHandler()` – convenience method to wire up semantic indexer

**Integration pattern:**

```go
handler := indexing.NewPostReviewHandler(cfg, logger)
semantic.RegisterWithHandler(handler, memoryStore, workspaceRoot, indexerCfg, logger)
// handler.Handle(ctx, event) now invokes semantic indexing
```

### 3e. Comprehensive tests

**Unit tests (`indexer_test.go`):**

- `TestIndexer_ID` – indexer identification
- `TestIndexer_Index_Disabled` – respects enabled flag
- `TestIndexer_Index_SingleFile` – single file embedding
- `TestIndexer_Index_ChunkedFile` – large file with chunks
- `TestIndexer_Index_DeletedFile` – file deletion handling
- `TestIndexer_Index_MultipleFiles` – batch processing
- `TestIndexer_Index_FileNotFound` – graceful error handling
- `TestConfig_ChunkingConfigHash` – config hash stability
- `TestFileEmbeddingName`, `TestChunkEmbeddingName` – naming conventions
- `TestSplitIntoChunks` – chunk boundary calculation
- `TestNoOpProvider` – provider implementation

**Integration test (`integration_test.go`):**

- `TestSemanticIndexerWithPostReviewHandler` – full pipeline test

## Files Added

- `internal/indexing/semantic/types.go` – data types and helpers
- `internal/indexing/semantic/provider.go` – embedding provider interface
- `internal/indexing/semantic/indexer.go` – main indexer implementation
- `internal/indexing/semantic/factory.go` – factory and registration
- `internal/indexing/semantic/indexer_test.go` – unit tests
- `internal/indexing/semantic/integration_test.go` – integration test

## Spec Alignment

This implementation aligns with:

- `docs/spec/semantic_file_index.md` §3 (Data Model), §4 (Lifecycle), §5
  (Config)
- `docs/impl_plan/universal_swe_grep_and_agents.md` (Phase 3)

## Next Steps

- Phase 4: Code Symbol Index v1 (Tree-sitter based)
- Phase 5: SWE Grep Skill
- Phase 6: dspy-go Tools & Agents Wiring
