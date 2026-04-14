package expander

import (
	"sync"

	"github.com/joshka0/foxctl/internal/intelligence/codecontext/files"
)

// BlockExpander finds code block boundaries for a specific language.
type BlockExpander interface {
	// FindBlock finds the enclosing code block for a given line.
	// Returns the start line, end line (1-indexed, inclusive), and the
	// detected symbol name (if any). Returns an error if the line is
	// out of range or no block can be determined.
	FindBlock(content *files.FileContent, line int) (start, end int, symbol string, err error)

	// ExpandToSymbol finds the block boundaries for a named symbol.
	// Searches for the symbol definition and returns its full extent.
	// Returns an error if the symbol is not found.
	ExpandToSymbol(content *files.FileContent, symbolName string) (start, end int, err error)

	// Language returns the language this expander handles.
	Language() string
}

// BlockResult contains the result of a block expansion.
type BlockResult struct {
	// StartLine is the first line of the block (1-indexed).
	StartLine int `json:"start_line"`

	// EndLine is the last line of the block (1-indexed, inclusive).
	EndLine int `json:"end_line"`

	// SymbolName is the detected symbol name (function, class, etc.).
	SymbolName string `json:"symbol_name,omitempty"`

	// SymbolKind is the type of symbol (func, method, class, type, etc.).
	SymbolKind string `json:"symbol_kind,omitempty"`

	// Language is the detected or specified language.
	Language string `json:"language,omitempty"`
}

// registry holds all registered expanders.
var (
	registry   = make(map[string]BlockExpander)
	registryMu sync.RWMutex
)

// Register adds a BlockExpander to the registry.
// Panics if an expander for the same language is already registered.
func Register(e BlockExpander) {
	registryMu.Lock()
	defer registryMu.Unlock()

	lang := e.Language()
	if _, exists := registry[lang]; exists {
		panic("expander: duplicate registration for " + lang)
	}
	registry[lang] = e
}

// Get returns the BlockExpander for a language.
// Returns nil if no expander is registered for the language.
// Use GetOrGeneric if you want a fallback.
func Get(language string) BlockExpander {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return registry[language]
}

// GetOrGeneric returns the expander for a language, falling back to
// the generic expander if no specific one is registered.
func GetOrGeneric(language string) BlockExpander {
	registryMu.RLock()
	defer registryMu.RUnlock()

	if e, ok := registry[language]; ok {
		return e
	}
	// Fall back to generic
	if e, ok := registry["generic"]; ok {
		return e
	}
	return nil
}

// Languages returns a list of all registered language names.
func Languages() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	langs := make([]string, 0, len(registry))
	for lang := range registry {
		langs = append(langs, lang)
	}
	return langs
}

// ExpanderError represents an error during block expansion.
type ExpanderError struct {
	Language string
	Message  string
	Line     int
	Symbol   string
}

func (e *ExpanderError) Error() string {
	if e.Symbol != "" {
		return "expander [" + e.Language + "]: " + e.Message + " (symbol: " + e.Symbol + ")"
	}
	return "expander [" + e.Language + "]: " + e.Message
}

// ErrLineOutOfRange indicates the requested line is outside file bounds.
var ErrLineOutOfRange = &ExpanderError{Message: "line out of range"}

// ErrSymbolNotFound indicates the requested symbol was not found.
var ErrSymbolNotFound = &ExpanderError{Message: "symbol not found"}

// ErrNoBlock indicates no enclosing block could be determined.
var ErrNoBlock = &ExpanderError{Message: "no enclosing block found"}
