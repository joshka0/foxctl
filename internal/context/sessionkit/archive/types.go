package archive

import (
	"time"

	"github.com/jkatigb/agentctl/internal/storage"
)

// ChunkResult contains chunks and context windows from parsing a JSONL file.
type ChunkResult struct {
	Chunks  []storage.SessionChunk
	Windows []storage.ContextWindow

	// HasMore indicates if MaxChunks limit was reached and more chunks exist.
	// When true, call Chunk again with SkipToChunk=NextChunkIndex.
	HasMore bool

	// NextChunkIndex is the index to use for SkipToChunk in the next batch.
	// Only valid when HasMore is true.
	NextChunkIndex int

	// NextWindowIndex is the window index to use for StartWindowIndex in the next batch.
	// Only valid when HasMore is true.
	NextWindowIndex int
}

// ChunkOptions configures the chunking process.
type ChunkOptions struct {
	// SessionID is the session identifier for the chunks.
	SessionID string

	// MaxChunkSize controls Codex window sizing and is reserved for future splitting of large messages.
	MaxChunkSize int

	// SkipToChunk skips processing until reaching this chunk index (for incremental archival).
	SkipToChunk int

	// StartWindowIndex is the starting window index (for incremental archival).
	StartWindowIndex int

	// MaxChunks limits how many chunks to return per call (0 = unlimited).
	// Use with SkipToChunk for batched processing of large sessions:
	//   batch1: SkipToChunk=0, MaxChunks=1000
	//   batch2: SkipToChunk=1000, MaxChunks=1000
	// This bounds memory usage for sessions with thousands of messages.
	MaxChunks int
}

// WindowInfo provides information about a context window.
type WindowInfo struct {
	Index            int    `json:"index"`
	StartedAt        string `json:"started_at,omitempty"`
	EndedAt          string `json:"ended_at,omitempty"`
	PreCompactTokens int    `json:"pre_compact_tokens,omitempty"`
	Trigger          string `json:"trigger,omitempty"`
	ChunkCount       int    `json:"chunk_count"`
}

// ChunkInfo provides information about a created chunk.
type ChunkInfo struct {
	Index          int      `json:"index"`
	Type           string   `json:"type"`
	ByteOffset     int64    `json:"byte_offset"`
	ByteLength     int64    `json:"byte_length"`
	ContentPreview string   `json:"content_preview,omitempty"`
	ToolsUsed      []string `json:"tools_used,omitempty"`
	HasError       bool     `json:"has_error,omitempty"`
}

// FormatTimestamp formats a time as RFC3339 or returns empty string if zero.
func FormatTimestamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
