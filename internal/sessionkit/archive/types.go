// Package archive provides session archival and chunking logic.
// It handles parsing Claude Code JSONL files into chunks and context windows,
// compressing archives, and extracting metadata.
package archive

import (
	"time"

	"github.com/jkatigb/agentctl/internal/storage"
)

// ChunkResult contains chunks and context windows from parsing a JSONL file.
type ChunkResult struct {
	Chunks  []storage.SessionChunk
	Windows []storage.ContextWindow
}

// ChunkOptions configures the chunking process.
type ChunkOptions struct {
	// SessionID is the session identifier for the chunks.
	SessionID string

	// MaxChunkSize is reserved for future splitting of large messages.
	MaxChunkSize int

	// SkipToChunk skips processing until reaching this chunk index (for incremental archival).
	SkipToChunk int

	// StartWindowIndex is the starting window index (for incremental archival).
	StartWindowIndex int
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
