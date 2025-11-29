package symbol

import (
	"context"
)

// Extractor extracts symbols from source code.
// Implementations may use Tree-sitter, Go AST, or other parsers.
type Extractor interface {
	// Extract parses the given content and returns symbols.
	Extract(ctx context.Context, filePath string, content []byte) ([]Symbol, error)

	// ExtractCalls extracts call relationships from a symbol's body.
	ExtractCalls(ctx context.Context, symbol Symbol, content []byte) ([]string, error)

	// SupportedLanguages returns the list of languages this extractor supports.
	SupportedLanguages() []string
}

// ExtractorRegistry manages language-specific extractors.
type ExtractorRegistry struct {
	extractors map[string]Extractor
}

// NewExtractorRegistry creates a new extractor registry.
func NewExtractorRegistry() *ExtractorRegistry {
	return &ExtractorRegistry{
		extractors: make(map[string]Extractor),
	}
}

// Register adds an extractor for a language.
func (r *ExtractorRegistry) Register(language string, extractor Extractor) {
	r.extractors[language] = extractor
}

// Get returns the extractor for a language, or nil if not supported.
func (r *ExtractorRegistry) Get(language string) Extractor {
	return r.extractors[language]
}

// SupportedLanguages returns all registered languages.
func (r *ExtractorRegistry) SupportedLanguages() []string {
	var langs []string
	for lang := range r.extractors {
		langs = append(langs, lang)
	}
	return langs
}

// DefaultRegistry creates a registry with built-in extractors.
func DefaultRegistry() *ExtractorRegistry {
	reg := NewExtractorRegistry()
	reg.Register("go", NewGoExtractor())
	return reg
}
