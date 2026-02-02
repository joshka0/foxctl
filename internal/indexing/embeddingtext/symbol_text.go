package embeddingtext

import (
	"fmt"
	"sort"
	"strings"
)

// SymbolTextOptions controls how symbol embedding text is built.
//
// Index:
//
//	Purpose: Configuration for symbol embedding text generation
//	Related: BuildSymbolEmbeddingText, SymbolInfo
//	Keywords: embedding text, symbol text
type SymbolTextOptions struct {
	// IncludeCode includes the full source code (if available).
	// When false, only doc + signature + hints are included.
	IncludeCode bool

	// IncludeRelationships adds "Calls:" and "CalledBy:" hints.
	IncludeRelationships bool

	// MaxCodeLines limits code inclusion to prevent oversized text.
	MaxCodeLines int

	// MaxRelationships limits the number of related symbols shown.
	MaxRelationships int
}

// DefaultSymbolTextOptions returns sensible defaults for symbol embedding.
//
// Index:
//
//	Purpose: Provide default configuration for symbol embedding
//	Related: SymbolTextOptions, BuildSymbolEmbeddingText
func DefaultSymbolTextOptions() SymbolTextOptions {
	return DefaultSymbolTextOptionsDocEnriched()
}

// DefaultSymbolTextOptionsDocEnriched includes doc + signature + capped code.
func DefaultSymbolTextOptionsDocEnriched() SymbolTextOptions {
	return SymbolTextOptions{
		IncludeCode:          true,
		IncludeRelationships: true,
		MaxCodeLines:         50,
		MaxRelationships:     10,
	}
}

// DefaultSymbolTextOptionsSummaryOnly uses doc + signature only.
func DefaultSymbolTextOptionsSummaryOnly() SymbolTextOptions {
	return SymbolTextOptions{
		IncludeCode:          false,
		IncludeRelationships: false,
		MaxCodeLines:         0,
		MaxRelationships:     0,
	}
}

// SymbolInfo contains the information needed to build embedding text.
//
// Index:
//
//	Purpose: Input data for symbol embedding text generation
//	Related: BuildSymbolEmbeddingText, symbol.Symbol
//	Keywords: symbol info, embedding input
type SymbolInfo struct {
	// Name is the symbol name (e.g., "SearchHybrid")
	Name string

	// Kind is the symbol kind (e.g., "function", "type", "interface")
	Kind string

	// Package is the package path (e.g., "internal/storage/memory")
	Package string

	// FilePath is the file where the symbol is defined
	FilePath string

	// Signature is the type signature (e.g., "func(ctx context.Context, query string) ([]SearchResult, error)")
	Signature string

	// Doc is the documentation comment (GoDoc)
	Doc string

	// Code is the full source code (optional)
	Code string

	// Calls lists symbols this symbol calls
	Calls []string

	// CalledBy lists symbols that call this symbol
	CalledBy []string

	// Implements lists interfaces this type implements
	Implements []string

	// ImplementedBy lists types that implement this interface
	ImplementedBy []string
}

// BuildSymbolEmbeddingText creates the text to embed for a symbol.
// The output combines documentation, signature, and relationship hints
// into a format optimized for semantic search.
//
// Index:
//
//	Purpose: Generate embedding-optimized text from symbol metadata
//	Related: SymbolInfo, SymbolTextOptions, NormalizeDoc
//	Keywords: symbol embedding, doc enriched, semantic text
func BuildSymbolEmbeddingText(info SymbolInfo, opts SymbolTextOptions) string {
	var parts []string

	// Header: Kind + Name + Package
	header := fmt.Sprintf("[%s] %s", info.Kind, info.Name)
	if info.Package != "" {
		header += fmt.Sprintf(" (package: %s)", info.Package)
	}
	parts = append(parts, header)

	// Signature
	if info.Signature != "" {
		parts = append(parts, "Signature: "+info.Signature)
	}

	// Documentation (normalized)
	if info.Doc != "" {
		doc := NormalizeDoc(info.Doc)
		if doc != "" {
			parts = append(parts, "Documentation: "+doc)
		}
	}

	// Relationship hints (sorted + deduped for stability)
	if opts.IncludeRelationships {
		if len(info.Calls) > 0 {
			calls := truncateList(sortDedup(info.Calls), opts.MaxRelationships)
			parts = append(parts, "Calls: "+strings.Join(calls, ", "))
		}
		if len(info.CalledBy) > 0 {
			calledBy := truncateList(sortDedup(info.CalledBy), opts.MaxRelationships)
			parts = append(parts, "Called by: "+strings.Join(calledBy, ", "))
		}
		if len(info.Implements) > 0 {
			impl := truncateList(sortDedup(info.Implements), opts.MaxRelationships)
			parts = append(parts, "Implements: "+strings.Join(impl, ", "))
		}
		if len(info.ImplementedBy) > 0 {
			implBy := truncateList(sortDedup(info.ImplementedBy), opts.MaxRelationships)
			parts = append(parts, "Implemented by: "+strings.Join(implBy, ", "))
		}
	}

	// Optional: Include code
	if opts.IncludeCode && info.Code != "" {
		code := truncateCode(info.Code, opts.MaxCodeLines)
		parts = append(parts, "Source:\n"+code)
	}

	return strings.Join(parts, "\n")
}

// sortDedup sorts and deduplicates a list for deterministic output.
func sortDedup(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, it := range items {
		if it == "" {
			continue
		}
		if _, ok := seen[it]; ok {
			continue
		}
		seen[it] = struct{}{}
		out = append(out, it)
	}
	sort.Strings(out)
	return out
}

// truncateList limits the number of items in a list.
func truncateList(items []string, max int) []string {
	if max <= 0 || len(items) <= max {
		return items
	}
	result := make([]string, max)
	copy(result, items[:max])
	return result
}

// truncateCode limits code to maxLines.
func truncateCode(code string, maxLines int) string {
	if maxLines <= 0 {
		return code
	}

	lines := strings.Split(code, "\n")
	if len(lines) <= maxLines {
		return code
	}

	return strings.Join(lines[:maxLines], "\n") + "\n// ... [truncated]"
}
