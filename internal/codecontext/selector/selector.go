// Package selector provides pluggable strategies for selecting relevant code spans.
//
// The selector package abstracts the logic for choosing which portions of a file
// are most relevant to a given query. This allows skills to use different selection
// strategies:
//
//   - HeuristicSelector: Fast keyword matching with stop-word filtering
//   - LLMSelector: Model-assisted selection for complex queries (future)
//
// Usage:
//
//	sel := selector.NewHeuristic(selector.HeuristicOpts{ContextLines: 3})
//	spans, err := sel.Select(ctx, "authentication flow", content, hints)
package selector

import (
	"context"

	"github.com/jkatigb/agentctl/internal/codecontext/files"
)

// Selector finds relevant code spans within file content.
type Selector interface {
	// Select returns relevant spans from file content given a query.
	// The spans are sorted by priority (highest first).
	Select(ctx context.Context, query string, content *files.FileContent, hints Hints) ([]Span, error)

	// Name returns the selector implementation name for logging/debugging.
	Name() string
}

// Span represents a contiguous range of lines within a file.
type Span struct {
	// StartLine is the first line of the span (1-indexed).
	StartLine int `json:"start_line"`

	// EndLine is the last line of the span (1-indexed, inclusive).
	EndLine int `json:"end_line"`

	// Reason explains why this span was selected.
	Reason string `json:"reason,omitempty"`

	// Priority indicates relative importance (higher = more relevant).
	// Range is typically 0.0 to 1.0.
	Priority float64 `json:"priority,omitempty"`

	// MatchCount is the number of keyword/pattern matches in this span.
	MatchCount int `json:"match_count,omitempty"`

	// SymbolID identifies the symbol this span belongs to, if known.
	SymbolID string `json:"symbol_id,omitempty"`
}

// Hints provide additional context for selection.
type Hints struct {
	// SymbolID targets a specific symbol (function, class, etc.).
	SymbolID string

	// LineHint targets a specific line number.
	LineHint int

	// Keywords are pre-extracted search terms.
	// If empty, the selector will extract keywords from the query.
	Keywords []string

	// Language is the programming language of the file.
	Language string

	// MaxSpans limits the number of spans returned.
	// Default: 10
	MaxSpans int

	// MaxLinesPerSpan limits span size.
	// Default: 50
	MaxLinesPerSpan int

	// ExpandToBlock indicates whether to expand spans to full code blocks.
	// Uses the expander package for language-aware expansion.
	ExpandToBlock bool
}

// DefaultMaxSpans is the default limit on returned spans.
const DefaultMaxSpans = 10

// DefaultMaxLinesPerSpan is the default maximum lines per span.
const DefaultMaxLinesPerSpan = 50

// DefaultContextLines is the default context around matches.
const DefaultContextLines = 3

// SelectorError represents an error during selection.
type SelectorError struct {
	Selector string
	Message  string
	Cause    error
}

func (e *SelectorError) Error() string {
	if e.Cause != nil {
		return "selector [" + e.Selector + "]: " + e.Message + ": " + e.Cause.Error()
	}
	return "selector [" + e.Selector + "]: " + e.Message
}

func (e *SelectorError) Unwrap() error {
	return e.Cause
}

// ApplyDefaults fills in zero values with defaults.
func (h *Hints) ApplyDefaults() {
	if h.MaxSpans <= 0 {
		h.MaxSpans = DefaultMaxSpans
	}
	if h.MaxLinesPerSpan <= 0 {
		h.MaxLinesPerSpan = DefaultMaxLinesPerSpan
	}
}
