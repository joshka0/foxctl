// Package retrieval holds the remaining non-v2 retrieval helpers that are still
// in use during the migration:
//
//   - tree building (`tree.go`)
//   - symbol summary generation (`file_summary.go`)
//
// New code-search entrypoints should use `internal/intelligence/retrieval/v2`.
package retrieval
