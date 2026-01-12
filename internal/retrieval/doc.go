// Package retrieval provides unified candidate generation for code search and SWE grep.
//
// This package implements the candidate generation layer that feeds into code/snippet_extract,
// combining multiple retrieval sources for comprehensive code discovery:
//
//   - Symbol Index: BM25 search over code symbols (functions, methods, types)
//   - Semantic Index: Vector similarity search using embeddings (when available)
//   - Ripgrep Fallback: Keyword-based file discovery when indexes are sparse
//
// # Architecture
//
// The Generator is the primary entry point, accepting a question and returning
// ranked file candidates suitable for SWE grep extraction:
//
//	generator := retrieval.NewGenerator(store, embedProvider, workspaceRoot, logger)
//	candidates, err := generator.Generate(ctx, workspaceID, "How does authentication work?", opts)
//
// Candidates are merged from all sources, deduplicated by file path, and ranked
// by a weighted score combining lexical and semantic relevance.
//
// # Graceful Degradation
//
// The package degrades gracefully when components are unavailable:
//
//   - No embedding provider: Skips semantic search, uses BM25 only
//   - Empty symbol index: Ripgrep fallback activates
//   - No ripgrep: Returns partial results from available sources
//
// # Integration Points
//
// This package is used by:
//
//   - code/smart_search skill: Standalone skill for auto-candidate SWE grep
//   - code.swe_grep tool: Agent tool with auto_candidates=true mode
//   - Future hooks: Could power smart search enhancements
//
// See docs/impl_plan/universal_swe_grep_live_index.md for the full design.
package retrieval
