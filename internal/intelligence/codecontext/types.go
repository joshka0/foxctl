package codecontext

import (
	"context"

	"github.com/joshka0/foxctl/internal/intelligence/codecontext/files"
	"github.com/joshka0/foxctl/internal/intelligence/codecontext/guard"
)

// Candidate is the upstream extraction target.
// It stays backward-compatible with the existing flow while supporting richer anchors.
type Candidate struct {
	// Path is the file path (relative or absolute).
	Path string `json:"path"`

	// Legacy fields.
	SymbolID string  `json:"symbol_id,omitempty"`
	LineHint int     `json:"line,omitempty"`
	Priority float64 `json:"priority,omitempty"`

	// Optional richer context from newer retrieval pipelines.
	Summary string   `json:"summary,omitempty"`
	Anchors []Anchor `json:"anchors,omitempty"`
}

// AnchorKind tells extraction how to expand.
type AnchorKind string

const (
	AnchorSymbol AnchorKind = "symbol"
	AnchorLine   AnchorKind = "line"
	AnchorFile   AnchorKind = "file"
)

// Anchor points to a relevant region in a file.
type Anchor struct {
	Kind AnchorKind `json:"kind"`

	SymbolID   string `json:"symbol_id,omitempty"`
	SymbolName string `json:"symbol_name,omitempty"`

	Line      int `json:"line,omitempty"`
	StartLine int `json:"start_line,omitempty"`
	EndLine   int `json:"end_line,omitempty"`

	Score  float64 `json:"score,omitempty"`
	Source string  `json:"source,omitempty"`
	Reason string  `json:"reason,omitempty"`
}

// Snippet represents an extracted code region from evidence collection.
type Snippet struct {
	File      string  `json:"file"`
	SymbolID  string  `json:"symbol_id,omitempty"`
	StartLine int     `json:"start_line"`
	EndLine   int     `json:"end_line"`
	Text      string  `json:"text"`
	Reason    string  `json:"reason,omitempty"`
	Priority  float64 `json:"priority,omitempty"`
	Language  string  `json:"language,omitempty"`
}

// Evidence is the output of the collection phase.
type Evidence struct {
	Snippets  []Snippet     `json:"snippets"`
	Stats     EvidenceStats `json:"stats"`
	Truncated bool          `json:"truncated"`
	Query     string        `json:"query,omitempty"`
	Warnings  []string      `json:"warnings,omitempty"`
}

// EvidenceStats contains metrics about the evidence collection process.
type EvidenceStats struct {
	FilesProcessed    int         `json:"files_processed"`
	FilesSkipped      int         `json:"files_skipped"`
	SnippetsExtracted int         `json:"snippets_extracted"`
	TotalBytes        int64       `json:"total_bytes"`
	FileErrors        []FileError `json:"file_errors,omitempty"`
}

// FileError records why a file couldn't be processed.
type FileError struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// RenderMode controls how evidence is formatted for output.
type RenderMode string

const (
	ModeSnippets  RenderMode = "snippets"
	ModeMasked    RenderMode = "masked"
	ModeStructure RenderMode = "structure"
	ModeFlow      RenderMode = "flow"
)

// Default limits when not specified.
const (
	DefaultMaxFiles        = 50
	DefaultMaxSnippets     = 100
	DefaultMaxBytesPerFile = 64 * 1024
	DefaultContextLines    = 3
	DefaultMaxAnchorsFile  = 4
)

// CollectOpts configures the evidence collection process.
type CollectOpts struct {
	Candidates []Candidate
	Query      string

	PathValidator files.PathValidator

	MaxFiles        int
	MaxSnippets     int
	MaxBytesPerFile int
	ContextLines    int

	Mode RenderMode

	// Optional richer extraction controls.
	MaxAnchorsPerFile int
	SecretMode        guard.Mode
}

// SnippetPreview is a truncated version of Snippet for inline responses.
type SnippetPreview struct {
	File      string  `json:"file"`
	SymbolID  string  `json:"symbol_id,omitempty"`
	StartLine int     `json:"start_line"`
	EndLine   int     `json:"end_line"`
	Preview   string  `json:"preview"`
	Priority  float64 `json:"priority,omitempty"`
	Language  string  `json:"language,omitempty"`
}

// OutputPayload is the standard output structure for skills using codecontext.
type OutputPayload struct {
	Query          string           `json:"query,omitempty"`
	SnippetsInline []SnippetPreview `json:"snippets_inline,omitempty"`
	Artifact       *ArtifactRef     `json:"artifact,omitempty"`
	Stats          EvidenceStats    `json:"stats"`
	Truncated      bool             `json:"truncated,omitempty"`
	Hints          []string         `json:"hints,omitempty"`
	Warnings       []string         `json:"warnings,omitempty"`
}

// ArtifactRef references a CAS-stored artifact.
type ArtifactRef struct {
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
	Kind   string `json:"kind"`
	Count  int    `json:"count,omitempty"`
}

// ArtifactSink persists rendered evidence and returns an artifact reference.
type ArtifactSink interface {
	Persist(ctx context.Context, baseName, kind string, body []byte) (ArtifactRef, error)
}

// OutputOpts configures output rendering and persistence thresholds.
type OutputOpts struct {
	Mode             RenderMode
	MaxPreviewBytes  int
	IncludeStats     bool
	InlineBytes      int
	ArtifactKind     string
	ArtifactBaseName string
}
