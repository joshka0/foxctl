package embeddingtext

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// SymbolTextOptions controls how symbol embedding text is built.
//
// Index:
//
//	Purpose: Configuration for symbol embedding text generation
//	Keywords: embedding text, symbol text
//	Related: BuildSymbolEmbeddingText, SymbolInfo
//	Flow: caller sets flags → BuildSymbolEmbeddingText reads them
//	Resources: symbol metadata
//	Events: none
//	OutputFields: none
//
// [[domain:symbol-embedding-text-options]]
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
//	Keywords: symbol embedding, default options
//	Related: SymbolTextOptions, BuildSymbolEmbeddingText
//	Flow: called by BuildSymbolEmbeddingText
//	Resources: none
//	Events: none
//	OutputFields: SymbolTextOptions
//
// [[domain:symbol-embedding-defaults]]
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
//	Keywords: symbol info, embedding input
//	Related: BuildSymbolEmbeddingText, symbol.Symbol
//	Flow: caller populates → BuildSymbolEmbeddingText consumes
//	Resources: symbol extraction results
//	Events: none
//	OutputFields: none
//
// [[domain:symbol-embedding-input]]
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

	// Aliases are normalized or canonical alternate forms that help semantic retrieval.
	Aliases []string
}

// BuildSymbolEmbeddingText creates the text to embed for a symbol.
// The output combines documentation, signature, and relationship hints
// into a format optimized for semantic search.
//
// Index:
//
//	Purpose: Generate embedding-optimized text from symbol metadata
//	Keywords: symbol embedding, doc enriched, semantic text
//	Related: SymbolInfo, SymbolTextOptions, NormalizeDoc
//	Flow: build header → add signature → add doc → add relationships → add code
//	Resources: symbol metadata
//	Events: none
//	OutputFields: embedding text string
//
// [[protocol:symbol-embedding-text-build]]
// [[domain:embedding-text-format]]
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

	if len(info.Aliases) > 0 {
		aliases := truncateList(sortDedup(info.Aliases), opts.MaxRelationships)
		if len(aliases) > 0 {
			parts = append(parts, "Aliases: "+strings.Join(aliases, ", "))
		}
	}

	// Optional: Include code
	if opts.IncludeCode && info.Code != "" {
		code := truncateCode(info.Code, opts.MaxCodeLines)
		parts = append(parts, "Source:\n"+code)
	}

	return strings.Join(parts, "\n")
}

// BuildSymbolAliases derives normalized alternate forms for semantic retrieval.
func BuildSymbolAliases(info SymbolInfo) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 8)
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}

	for _, part := range splitSymbolParts(info.Name) {
		snake := toEmbeddingSnake(part)
		add(strings.ReplaceAll(snake, "_", " "))
		add(snake)
	}
	if len(splitSymbolParts(info.Name)) > 1 {
		add(strings.Join(splitSymbolParts(info.Name), " "))
	}

	base := strings.TrimSuffix(filepath.Base(info.FilePath), filepath.Ext(info.FilePath))
	if base != "" {
		snake := toEmbeddingSnake(base)
		add(strings.ReplaceAll(snake, "_", " "))
		add(snake)
	}
	dir := strings.TrimSpace(filepath.ToSlash(filepath.Dir(info.FilePath)))
	if dir != "" && dir != "." {
		parts := strings.Split(dir, "/")
		if len(parts) > 0 {
			last := toEmbeddingSnake(parts[len(parts)-1])
			if last != "" && base != "" {
				baseSnake := toEmbeddingSnake(base)
				add(strings.TrimSpace(strings.ReplaceAll(last+" "+baseSnake, "_", " ")))
				add(strings.TrimSpace(last + " " + baseSnake))
			}
		}
	}

	return sortDedup(out)
}

func splitSymbolParts(value string) []string {
	fields := strings.FieldsFunc(strings.TrimSpace(value), func(r rune) bool {
		return r == '.' || r == '/' || r == ':' || r == '\\'
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		out = append(out, toEmbeddingWords(field))
	}
	return out
}

func toEmbeddingWords(value string) string {
	snake := toEmbeddingSnake(value)
	return strings.ReplaceAll(snake, "_", " ")
}

func toEmbeddingSnake(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var out []rune
	var prev rune
	for i, r := range value {
		switch {
		case r >= 'A' && r <= 'Z':
			if i > 0 && ((prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9')) {
				out = append(out, '_')
			}
			out = append(out, r+('a'-'A'))
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			out = append(out, r)
		default:
			if len(out) > 0 && out[len(out)-1] != '_' {
				out = append(out, '_')
			}
		}
		prev = r
	}
	return strings.Trim(strings.ReplaceAll(string(out), "__", "_"), "_")
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
