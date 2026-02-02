// Package symbol implements the code symbol index as a post-review indexer.
// It stores symbol definitions, call relationships, and embeddings per
// code_symbol_index_and_swe_grep.md spec.
//
// # Named Memory Type Mapping
//
// This package maps conceptual tables from the spec to named memory entry types:
//
//   - [SymbolType] ("code_symbol") → conceptual `symbols` rows (spec §3.1)
//   - [CallEdgeType] ("code_symbol_call") → conceptual `calls` rows (spec §3.2)
//   - [FileMetaType] ("code_symbol_file_meta") → conceptual `file_meta` rows (spec §3.3)
//
// Symbols are stored as named memory entries with type="code_symbol", where:
//   - Entry.Name follows the [EntryName] format: "symbol://<workspace>/<file_path>:<symbol_name>"
//   - Entry.Result contains a JSON-serialized [Result] struct with the [Symbol] and provenance
//   - Entry.Embedding (when vector support is enabled) holds the symbol embedding
//
// Call edges are stored with type="code_symbol_call", keyed by source and target symbol IDs.
// File metadata is stored with type="code_symbol_file_meta" to track freshness for incremental updates.
package symbol
