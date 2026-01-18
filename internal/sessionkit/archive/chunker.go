package archive

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/jkatigb/agentctl/internal/sessionkit/claudejsonl"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/oklog/ulid/v2"
)

const (
	// DefaultMaxChunkSize is the default max chunk size (tokens approximation).
	DefaultMaxChunkSize = 4000
	// DefaultCodexWindowTokens is the default token estimate per Codex window.
	DefaultCodexWindowTokens = 30000
	// DefaultMaxPreviewLen is the default max preview length.
	DefaultMaxPreviewLen = 200
)

// Chunk reads a JSONL file and creates chunks and context windows.
// This function handles incremental archival by allowing skipping already processed chunks.
func Chunk(reader *claudejsonl.Reader, opts ChunkOptions) (ChunkResult, error) {
	var chunks []storage.SessionChunk
	var windows []storage.ContextWindow
	chunkIndex := 0
	windowIndex := opts.StartWindowIndex

	// Track current window state
	var windowStartChunk int
	var windowStartTime time.Time
	var windowMsgCount int
	var firstMsgTime time.Time

	now := time.Now().UTC()

	for {
		rm, err := reader.Next()
		if err != nil {
			return ChunkResult{}, err
		}
		if rm == nil {
			break // EOF
		}

		msg := rm.Message
		msgTime := rm.Timestamp

		// Track first message time for window
		if firstMsgTime.IsZero() && !msgTime.IsZero() {
			firstMsgTime = msgTime
			windowStartTime = msgTime
		}

		// Check for compact_boundary
		_, _, isCompactBoundary := claudejsonl.IsCompactBoundary(msg)

		// Check for compact summary message (comes after compact_boundary, summarizes previous window)
		if msg.IsCompactSummary && len(msg.Message) > 0 && len(windows) > 0 {
			var payload claudejsonl.MessagePayload
			if err := json.Unmarshal(msg.Message, &payload); err == nil && payload.Content != "" {
				// Update the most recent window with the summary
				windows[len(windows)-1].Summary = claudejsonl.ExtractSummaryText(payload.Content)
			}
		}

		// Skip chunks we've already processed (incremental mode)
		if chunkIndex < opts.SkipToChunk {
			// Still need to handle compact boundaries for window tracking
			if isCompactBoundary {
				windowStartChunk = chunkIndex + 1
				windowStartTime = time.Time{} // Reset for next window
				windowMsgCount = 0
			}
			chunkIndex++
			continue
		}

		// First chunk after skip - update windowStartChunk if this is start of incremental
		if opts.SkipToChunk > 0 && chunkIndex == opts.SkipToChunk {
			windowStartChunk = chunkIndex
			windowStartTime = msgTime
		}

		// Determine chunk type and content
		chunkType := string(claudejsonl.Classify(msg))
		contentPreview := claudejsonl.ExtractPreview(msg, DefaultMaxPreviewLen)
		toolsUsed := claudejsonl.ExtractTools(msg)
		hasError := claudejsonl.HasError(msg)
		errorType := claudejsonl.ExtractErrorType(msg)

		// Create content hash from raw message bytes
		// We need to re-serialize since we don't have the original line
		lineBytes, _ := json.Marshal(msg)
		hash := sha256.Sum256(lineBytes)
		contentHash := hex.EncodeToString(hash[:])

		chunk := storage.SessionChunk{
			ID:                 ulid.Make().String(),
			SessionID:          opts.SessionID,
			ChunkIndex:         chunkIndex,
			ChunkType:          chunkType,
			ContentHash:        contentHash,
			ContentPreview:     contentPreview,
			ByteOffset:         rm.ByteOffset,
			ByteLength:         rm.ByteLength,
			ToolsUsed:          toolsUsed,
			HasError:           hasError,
			ErrorType:          errorType,
			ContextWindowIndex: windowIndex,
			CreatedAt:          now,
		}

		chunks = append(chunks, chunk)
		windowMsgCount++

		// Check MaxChunks limit (0 = unlimited)
		if opts.MaxChunks > 0 && len(chunks) >= opts.MaxChunks {
			// Return early with continuation info
			return ChunkResult{
				Chunks:          chunks,
				Windows:         windows,
				HasMore:         true,
				NextChunkIndex:  chunkIndex + 1,
				NextWindowIndex: windowIndex,
			}, nil
		}

		// If this is a compact_boundary, close the current window and start a new one
		if isCompactBoundary {
			// Extract compaction metadata
			trigger, preTokens, _ := claudejsonl.IsCompactBoundary(msg)

			// Create the window that just ended
			window := storage.ContextWindow{
				ID:               ulid.Make().String(),
				SessionID:        opts.SessionID,
				WindowIndex:      windowIndex,
				StartedAt:        windowStartTime,
				EndedAt:          msgTime,
				PreCompactTokens: preTokens,
				Trigger:          trigger,
				ChunkStart:       windowStartChunk,
				ChunkEnd:         chunkIndex,
				MessageCount:     windowMsgCount,
				CreatedAt:        now,
			}
			windows = append(windows, window)

			// Start a new window
			windowIndex++
			windowStartChunk = chunkIndex + 1
			windowStartTime = time.Time{} // Reset for next window
			windowMsgCount = 0
		}

		chunkIndex++
	}

	// Create final window for remaining chunks (if any messages after last boundary or no boundaries)
	if windowMsgCount > 0 || len(windows) == 0 {
		window := storage.ContextWindow{
			ID:           ulid.Make().String(),
			SessionID:    opts.SessionID,
			WindowIndex:  windowIndex,
			StartedAt:    windowStartTime,
			EndedAt:      now, // Use current time as end since session may still be active
			ChunkStart:   windowStartChunk,
			ChunkEnd:     chunkIndex - 1,
			MessageCount: windowMsgCount,
			CreatedAt:    now,
		}
		// Only add if we have chunks (handles empty JSONL)
		if chunkIndex > 0 {
			windows = append(windows, window)
		}
	}

	return ChunkResult{Chunks: chunks, Windows: windows}, nil
}

// ChunkFile opens a file and chunks it.
func ChunkFile(path string, opts ChunkOptions) (ChunkResult, error) {
	reader, err := claudejsonl.OpenReader(path)
	if err != nil {
		return ChunkResult{}, err
	}
	defer reader.Close()

	return Chunk(reader, opts)
}

// ChunkFromReader creates a reader from an io.Reader and chunks it.
// Useful for streaming or in-memory sources.
func ChunkFromReader(r interface{ Read([]byte) (int, error) }, opts ChunkOptions) (ChunkResult, error) {
	reader := claudejsonl.NewReader(r)
	return Chunk(reader, opts)
}
