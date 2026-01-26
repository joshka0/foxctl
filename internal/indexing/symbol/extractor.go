package symbol

import (
	"context"
	"sort"
)

// Extractor extracts symbols from source code.
//
// Implementations may use Tree-sitter, Go AST, or other parsers. The spec
// (code_symbol_index_and_swe_grep.md §4.1) describes Tree-sitter as the reference
// parser; in v1, Go uses the standard library's go/ast as an "equivalent parser"
// that produces the same data model without requiring CGO.
//
// # Implementation Requirements
//
// Extractors MUST:
//   - Return stable [Symbol.ID] values for the same (file_path, symbol_name) pair
//   - Compute [Symbol.BodyDigest] using [ComputeDigest] so incremental updates work
//   - Set [Symbol.StartByte], [Symbol.EndByte] for navigation
//   - Return nil (not error) for unparseable files to avoid blocking the pipeline
//
// Extractors SHOULD:
//   - Extract documentation comments when available
//   - Extract signatures for functions/methods
//   - Support extracting call relationships via ExtractCalls
type Extractor interface {
	// Extract parses the given content and returns symbols.
	// Returns nil symbols and nil error for unparseable files.
	Extract(ctx context.Context, filePath string, content []byte) ([]Symbol, error)

	// ExtractCalls extracts call relationships from a symbol's body.
	// Returns the IDs of symbols that this symbol calls.
	// May return empty/nil if call extraction is not supported or fails.
	ExtractCalls(ctx context.Context, symbol Symbol, content []byte) ([]string, error)

	// SupportedLanguages returns the list of languages this extractor supports.
	// Language identifiers should be lowercase (e.g. "go", "python", "typescript").
	SupportedLanguages() []string
}

// ExtractorRegistry manages language-specific extractors.
//
// The registry provides an extension path for adding support for additional
// languages without modifying the core indexer. In v1, only Go is supported
// via [GoExtractor]; future versions may add Tree-sitter-based extractors for
// other languages.
//
// # Extension Path
//
// To add support for a new language:
//  1. Implement the [Extractor] interface for the language
//  2. Register it with [ExtractorRegistry.Register] before creating the [Indexer]
//  3. The language will be auto-detected from file extensions (see detectLanguage)
//
// Example:
//
//	reg := symbol.NewExtractorRegistry()
//	reg.Register("go", symbol.NewGoExtractor())
//	reg.Register("python", myPythonExtractor) // hypothetical
//	idx := symbol.NewIndexer(cfg, store, reg, workspace, logger)
type ExtractorRegistry struct {
	extractors map[string]Extractor
}

// NewExtractorRegistry creates a new empty extractor registry.
func NewExtractorRegistry() *ExtractorRegistry {
	return &ExtractorRegistry{
		extractors: make(map[string]Extractor),
	}
}

// Register adds an extractor for a language.
// The language identifier should be lowercase (e.g. "go", "python").
// Registering the same language twice replaces the previous extractor.
func (r *ExtractorRegistry) Register(language string, extractor Extractor) {
	r.extractors[language] = extractor
}

// Get returns the extractor for a language, or nil if not supported.
// This allows the indexer to gracefully skip unsupported languages.
func (r *ExtractorRegistry) Get(language string) Extractor {
	return r.extractors[language]
}

// SupportedLanguages returns all registered languages in sorted order.
func (r *ExtractorRegistry) SupportedLanguages() []string {
	var langs []string
	for lang := range r.extractors {
		langs = append(langs, lang)
	}
	sort.Strings(langs)
	return langs
}

// DefaultRegistry creates a registry with built-in extractors.
// In v1, this includes Go and TypeScript support via [GoExtractor] and [TypeScriptExtractor].
func DefaultRegistry() *ExtractorRegistry {
	reg := NewExtractorRegistry()
	reg.Register("go", NewGoExtractor())
	reg.Register("typescript", NewTypeScriptExtractor())
	return reg
}
