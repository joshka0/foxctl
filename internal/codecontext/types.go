package codecontext

// Candidate represents a file or symbol location from upstream retrieval.
// It is the input to the evidence collection phase.
type Candidate struct {
	// Path is the file path (relative or absolute).
	Path string `json:"path"`

	// SymbolID identifies a specific symbol within the file (optional).
	// Format: "file.go:FunctionName" or just symbol name.
	SymbolID string `json:"symbol_id,omitempty"`

	// LineHint suggests a line number of interest (optional).
	// Used when SymbolID is not available.
	LineHint int `json:"line,omitempty"`

	// Priority ranks this candidate relative to others (0.0-1.0).
	// Higher priority candidates are processed first.
	Priority float64 `json:"priority,omitempty"`
}

// Snippet represents an extracted code region from evidence collection.
type Snippet struct {
	// File is the path to the source file.
	File string `json:"file"`

	// SymbolID identifies the symbol this snippet belongs to (optional).
	SymbolID string `json:"symbol_id,omitempty"`

	// StartLine is the 1-indexed first line of the snippet.
	StartLine int `json:"start_line"`

	// EndLine is the 1-indexed last line of the snippet.
	EndLine int `json:"end_line"`

	// Text is the actual code content.
	Text string `json:"text"`

	// Reason explains why this snippet was selected (optional).
	Reason string `json:"reason,omitempty"`

	// Priority inherited from the source candidate.
	Priority float64 `json:"priority,omitempty"`

	// Language detected for the file.
	Language string `json:"language,omitempty"`
}

// Evidence is the output of the collection phase.
// It contains snippets and metadata about the extraction process.
type Evidence struct {
	// Snippets are the extracted code regions.
	Snippets []Snippet `json:"snippets"`

	// Stats contains metrics about the collection process.
	Stats EvidenceStats `json:"stats"`

	// Truncated indicates whether results were limited due to constraints.
	Truncated bool `json:"truncated"`

	// Query is the original question/query used for extraction.
	Query string `json:"query,omitempty"`
}

// EvidenceStats contains metrics about the evidence collection process.
type EvidenceStats struct {
	// FilesProcessed is the number of files successfully read.
	FilesProcessed int `json:"files_processed"`

	// FilesSkipped is the number of files that couldn't be read.
	FilesSkipped int `json:"files_skipped"`

	// SnippetsExtracted is the total number of snippets found.
	SnippetsExtracted int `json:"snippets_extracted"`

	// TotalBytes is the total bytes read across all files.
	TotalBytes int64 `json:"total_bytes"`

	// FileErrors contains per-file error information.
	FileErrors []FileError `json:"file_errors,omitempty"`
}

// FileError records why a file couldn't be processed.
type FileError struct {
	// Path is the file that had an error.
	Path string `json:"path"`

	// Code is the error classification (EPOLICY, ENOTFOUND, EIO, etc.).
	Code string `json:"code"`

	// Message describes the error.
	Message string `json:"message"`
}

// RenderMode controls how evidence is formatted for output.
type RenderMode string

const (
	// ModeSnippets renders evidence as disjoint code regions (default).
	// Each snippet is a separate block with file path and line numbers.
	ModeSnippets RenderMode = "snippets"

	// ModeMasked renders full file content with irrelevant sections redacted.
	// Useful for showing file structure while highlighting relevant parts.
	ModeMasked RenderMode = "masked"

	// ModeStructure renders only signatures, imports, and type definitions.
	// Useful for understanding API shape without implementation details.
	ModeStructure RenderMode = "structure"

	// ModeFlow renders control-flow oriented excerpts.
	// Follows function calls and includes related code paths.
	ModeFlow RenderMode = "flow"
)

// Default limits when not specified.
const (
	DefaultMaxFiles        = 50
	DefaultMaxSnippets     = 100
	DefaultMaxBytesPerFile = 64 * 1024 // 64 KB
	DefaultContextLines    = 3         // Lines before/after match
)
