# Phase 4 Implementation: Code Symbol Index v1

**Date:** 2025-01-20

## Summary

Implemented Phase 4 of the Universal SWE Grep & Agentic RAG integration plan.
This establishes the code symbol index as a post-review indexer that extracts
symbols from source code using Go AST parsing (CGO-free).

## Changes

### 4a. Defined symbol index types and schema

New package: `internal/indexing/symbol/`

**Types (`types.go`):**

- `Symbol` – represents a code symbol (function, method, struct, etc.)
- `Kind` – enum: function, method, class, struct, interface, variable, constant,
  type, file_summary
- `CallEdge` – represents call relationships between symbols
- `FileMeta` – tracks file freshness for incremental updates
- `Result` – structured metadata stored in NamedEntry.Result
- `Source` – provenance tracking (task_id, review_id, actor, reason)
- `Config` – indexer configuration (max_file_loc, max_file_kb, languages)

**Constants:**

- `SymbolType = "code_symbol"`
- `CallEdgeType = "code_symbol_call"`
- `FileMetaType = "code_symbol_file_meta"`

**Helpers:**

- `ID(filePath, symbolName)` – generates stable symbol identifier
- `EntryName(workspace, filePath, symbolName)` – canonical memory entry name
- `FileMetaEntryName(workspace, filePath)` – file meta entry name
- `ComputeDigest(content)` – sha256 digest for change detection

### 4b. Implemented Go AST symbol extraction (CGO-free)

**Extractor interface (`extractor.go`):**

- `Extractor` – interface for symbol extractors
- `ExtractorRegistry` – manages language-specific extractors
- `DefaultRegistry()` – creates registry with Go extractor

**Go extractor (`extractor_go.go`):**

- Uses Go's standard `go/ast` and `go/parser` packages (no CGO required)
- Extracts functions, methods, structs, interfaces, types, constants, variables
- Extracts function signatures with parameter and return types
- Extracts documentation comments
- Extracts call relationships from function bodies
- Computes body digests for incremental updates

### 4c. Implemented SymbolIndexer (Indexer interface)

**Indexer (`indexer.go`):**

- Implements `indexing.Indexer` interface
- Processes post-review events and indexes symbols
- Incremental updates: skips unchanged files via digest comparison
- Supports file deletion (removes symbol entries)
- Language detection from file extension or metadata

**Key features:**

- Per-symbol digests to avoid re-indexing unchanged symbols
- File meta tracking for change detection
- Graceful error handling per file

### 4d. Wired to post-review handler

**Factory (`factory.go`):**

- `Factory` – creates indexers from config
- `RegisterWithHandler()` – convenience method to wire up symbol indexer

### 4e. Comprehensive tests

**Tests (`indexer_test.go`):**

- `TestIndexer_ID` – indexer identification
- `TestIndexer_Index_Disabled` – respects enabled flag
- `TestIndexer_Index_GoFile` – Go file symbol extraction
- `TestIndexer_Index_UnsupportedLanguage` – skips unsupported languages
- `TestIndexer_Index_DeletedFile` – file deletion handling
- `TestIndexer_Index_IncrementalUpdate` – incremental indexing
- `TestGoExtractor_ExtractFunctions` – function/method extraction
- `TestGoExtractor_ExtractTypes` – struct/interface extraction
- `TestSymbolID`, `TestEntryName`, `TestFileMetaEntryName` – naming helpers
- `TestComputeDigest` – digest computation

## Files Added

- `internal/indexing/symbol/types.go` – data types and helpers
- `internal/indexing/symbol/extractor.go` – extractor interface and registry
- `internal/indexing/symbol/extractor_go.go` – Go AST-based extractor
- `internal/indexing/symbol/indexer.go` – main indexer implementation
- `internal/indexing/symbol/factory.go` – factory and registration
- `internal/indexing/symbol/indexer_test.go` – comprehensive tests

## Technical Notes

### CGO-Free Design

The Go extractor uses the standard library's `go/ast` and `go/parser` packages,
which do not require CGO. This aligns with the project's CGO_ENABLED=0 policy.

Future extractors for other languages could use:

- Pure Go parsers (if available)
- WASI-based Tree-sitter (when available)
- External parser processes (exec-based)

### Incremental Indexing

The indexer tracks file content hashes via `FileMeta` entries:

1. Before indexing, check if file digest matches stored hash
2. If unchanged, skip the file entirely
3. If changed, re-index all symbols and update file meta

Per-symbol body digests allow future optimization to skip unchanged symbols
within a changed file.

## Spec Alignment

This implementation aligns with:

- `docs/spec/code_symbol_index_and_swe_grep.md` §3 (Data Model), §4 (Lifecycle)
- `docs/impl_plan/universal_swe_grep_and_agents.md` (Phase 4)

## Next Steps

- Phase 5: SWE Grep Skill
- Phase 6: dspy-go Tools & Agents Wiring
- Future: Add extractors for Python, TypeScript, GDScript, etc.
