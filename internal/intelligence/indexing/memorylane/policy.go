// Package memorylane defines candidate policy for the general memory embedding lane.
package memorylane

import (
	"strings"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"
)

var codeOwnedTypes = map[string]struct{}{
	semantic.FileEmbeddingType:      {},
	semantic.FileEmbeddingChunkType: {},
	symbol.SymbolType:               {},
	symbol.CallEdgeType:             {},
	symbol.FileMetaType:             {},
	symbol.FileSummaryType:          {},
	symbol.SymbolSummaryType:        {},
	"edit":                          {},
	"symbol":                        {},
}

// EligibleType reports whether a named-memory type belongs in the general
// memory embedding lane. Code-owned memory records have separate embedding lanes.
func EligibleType(memoryType string) bool {
	return !IsCodeOwnedType(memoryType)
}

// IsCodeOwnedType reports whether a named-memory type is owned by code indexing
// or file embedding pipelines rather than the general memory lane.
func IsCodeOwnedType(memoryType string) bool {
	_, ok := codeOwnedTypes[strings.ToLower(strings.TrimSpace(memoryType))]
	return ok
}
