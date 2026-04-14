// Package memvid provides integration with the memvid MV2 file format.
// Memvid is a single-file memory format with embedded search (Tantivy + HNSW).
// This package wraps the memvid CLI for session export functionality.
package memvid

import (
	"time"
)

// Frame represents a single unit of content in an MV2 file.
// Maps to the MV2 spec frame structure.
type Frame struct {
	// URI is the unique identifier for this frame (e.g., "mv2://session/turn/123")
	URI string `json:"uri"`

	// Title is an optional display name
	Title string `json:"title,omitempty"`

	// Content is the raw text content
	Content string `json:"content"`

	// CreatedAt is when this frame was created
	CreatedAt time.Time `json:"created_at"`

	// Tags are user-defined key-value pairs
	Tags map[string]string `json:"tags,omitempty"`
}

// SearchResult represents a single search hit from memvid.
type SearchResult struct {
	// FrameID is the internal frame identifier
	FrameID uint64 `json:"frame_id"`

	// URI of the matching frame
	URI string `json:"uri"`

	// Title of the matching frame
	Title string `json:"title,omitempty"`

	// Score is the relevance score (higher = more relevant)
	Score float64 `json:"score"`

	// Snippet is the matching text excerpt
	Snippet string `json:"snippet,omitempty"`

	// Highlights are the matched terms/phrases
	Highlights []string `json:"highlights,omitempty"`
}

// SearchMode specifies the type of search to perform.
type SearchMode string

const (
	// SearchModeLexical uses BM25 full-text search (Tantivy)
	SearchModeLexical SearchMode = "lex"

	// SearchModeSemantic uses vector similarity search (HNSW)
	SearchModeSemantic SearchMode = "sem"

	// SearchModeHybrid combines lexical and semantic (default)
	SearchModeHybrid SearchMode = "hybrid"
)

// SearchOptions configures a search query.
type SearchOptions struct {
	// Query is the search text
	Query string

	// Mode specifies lexical, semantic, or hybrid search
	Mode SearchMode

	// TopK limits the number of results (default: 10)
	TopK int

	// MinScore filters results below this threshold
	MinScore float64

	// TimeRange filters by creation time
	TimeStart *time.Time
	TimeEnd   *time.Time

	// Tags filters by tag key-value pairs
	Tags map[string]string
}

// Stats contains statistics about an MV2 file.
type Stats struct {
	// FrameCount is the total number of frames
	FrameCount int64 `json:"frame_count"`

	// FileSize is the total file size in bytes
	FileSize int64 `json:"file_size"`

	// CreatedAt is when the file was created
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the file was last modified
	UpdatedAt time.Time `json:"updated_at"`

	// VectorDimensions is the embedding dimension (typically 384)
	VectorDimensions int `json:"vector_dimensions"`

	// CompressionRatio is the data compression ratio
	CompressionRatio float64 `json:"compression_ratio,omitempty"`
}

// SessionExportOptions configures session export to MV2.
type SessionExportOptions struct {
	// SessionID is the foxctl session to export
	SessionID string

	// OutputPath is the destination .mv2 file path
	OutputPath string

	// IncludeTurns exports individual turns as frames
	IncludeTurns bool

	// IncludeChunks exports session chunks as frames
	IncludeChunks bool

	// IncludeSummaries exports L1/L2 summaries as frames
	IncludeSummaries bool

	// GenerateEmbeddings creates vector embeddings (requires API key)
	GenerateEmbeddings bool

	// EmbeddingModel specifies the model for embeddings
	// Default: "all-MiniLM-L6-v2" (384 dimensions, matches MV2 spec)
	EmbeddingModel string
}
