// Package codemap provides semantic codemap generation using dspy-go agents.
package codemap

import (
	"time"
)

// Codemap is the final output stored and retrieved via semantic search.
type Codemap struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Query       string    `json:"query"`
	Workspace   string    `json:"workspace"`
	Traces      []Trace   `json:"traces"`
	CreatedAt   time.Time `json:"created_at"`

	// Metadata for search/filtering
	FileCount   int      `json:"file_count"`
	SymbolCount int      `json:"symbol_count"`
	Terms       []string `json:"terms"` // Extracted query terms
}

// Trace represents an execution path or relationship in the codemap.
type Trace struct {
	Number      int          `json:"number"`
	Title       string       `json:"title"`
	Summary     string       `json:"summary"`
	Tree        string       `json:"tree"`
	Annotations []Annotation `json:"annotations"`
}

// Annotation provides details about a specific location in the code.
type Annotation struct {
	Label       string `json:"label"`       // "1a", "1b", "2a"
	Title       string `json:"title"`       // "Skill Discovery"
	Description string `json:"description"` // Full explanation
	Path        string `json:"path"`        // "@internal/skill/discovery.go:78"
}
