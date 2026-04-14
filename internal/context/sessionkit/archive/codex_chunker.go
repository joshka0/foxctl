package archive

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/joshka0/foxctl/internal/context/sessionkit/codexjsonl"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/oklog/ulid/v2"
)

// ChunkCodex reads a Codex JSONL file and creates chunks and context windows.
// Codex compaction markers (if present) and size limits define window boundaries.
func ChunkCodex(reader *codexjsonl.Reader, opts ChunkOptions) (ChunkResult, error) {
	var chunks []storage.SessionChunk
	var windows []storage.ContextWindow
	chunkIndex := 0
	windowIndex := opts.StartWindowIndex
	windowStartChunk := opts.SkipToChunk
	windowTokenTarget := opts.MaxChunkSize
	if windowTokenTarget <= 0 {
		windowTokenTarget = DefaultCodexWindowTokens
	}

	var windowStartTime time.Time
	var windowMsgCount int
	var windowTokenEstimate int
	var lastMsgTime time.Time

	now := time.Now().UTC()

	for {
		rm, err := reader.Next()
		if err != nil {
			return ChunkResult{}, err
		}
		if rm == nil {
			break
		}

		msg := rm.Message
		msgTime := rm.Timestamp
		if !msgTime.IsZero() {
			lastMsgTime = msgTime
			if windowStartTime.IsZero() {
				windowStartTime = msgTime
			}
		}

		trigger, preTokens, isCompactBoundary := codexjsonl.IsCompactBoundary(msg)

		if chunkIndex < opts.SkipToChunk {
			if isCompactBoundary {
				windowStartChunk = chunkIndex + 1
				windowStartTime = time.Time{}
				windowMsgCount = 0
				windowTokenEstimate = 0
			}
			chunkIndex++
			continue
		}

		if opts.SkipToChunk > 0 && chunkIndex == opts.SkipToChunk && !msgTime.IsZero() {
			windowStartTime = msgTime
		}

		chunkType := string(codexjsonl.Classify(msg))
		contentPreview := codexjsonl.ExtractPreview(msg, DefaultMaxPreviewLen)
		toolsUsed := codexjsonl.ExtractTools(msg)
		hasError := codexjsonl.HasError(msg)
		errorType := codexjsonl.ExtractErrorType(msg)

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
		msgTokens := codexjsonl.EstimateTokens(msg)
		if msgTokens == 0 {
			msgTokens = int(float64(rm.ByteLength) * codexjsonl.DefaultTokensPerChar)
		}
		windowTokenEstimate += msgTokens

		if isCompactBoundary {
			if preTokens == 0 {
				preTokens = windowTokenEstimate
			}
			endedAt := msgTime
			if endedAt.IsZero() {
				endedAt = now
			}
			window := storage.ContextWindow{
				ID:               ulid.Make().String(),
				SessionID:        opts.SessionID,
				WindowIndex:      windowIndex,
				StartedAt:        windowStartTime,
				EndedAt:          endedAt,
				PreCompactTokens: preTokens,
				Trigger:          trigger,
				ChunkStart:       windowStartChunk,
				ChunkEnd:         chunkIndex,
				MessageCount:     windowMsgCount,
				CreatedAt:        now,
			}
			windows = append(windows, window)

			windowIndex++
			windowStartChunk = chunkIndex + 1
			windowStartTime = time.Time{}
			windowMsgCount = 0
			windowTokenEstimate = 0
		} else if windowTokenTarget > 0 && windowTokenEstimate >= windowTokenTarget {
			endedAt := msgTime
			if endedAt.IsZero() {
				endedAt = now
			}
			window := storage.ContextWindow{
				ID:               ulid.Make().String(),
				SessionID:        opts.SessionID,
				WindowIndex:      windowIndex,
				StartedAt:        windowStartTime,
				EndedAt:          endedAt,
				PreCompactTokens: windowTokenEstimate,
				Trigger:          "token_limit",
				ChunkStart:       windowStartChunk,
				ChunkEnd:         chunkIndex,
				MessageCount:     windowMsgCount,
				CreatedAt:        now,
			}
			windows = append(windows, window)

			windowIndex++
			windowStartChunk = chunkIndex + 1
			windowStartTime = time.Time{}
			windowMsgCount = 0
			windowTokenEstimate = 0
		}

		chunkIndex++
	}

	if windowMsgCount > 0 || len(windows) == 0 {
		endedAt := now
		if !lastMsgTime.IsZero() {
			endedAt = lastMsgTime
		}
		window := storage.ContextWindow{
			ID:           ulid.Make().String(),
			SessionID:    opts.SessionID,
			WindowIndex:  windowIndex,
			StartedAt:    windowStartTime,
			EndedAt:      endedAt,
			ChunkStart:   windowStartChunk,
			ChunkEnd:     chunkIndex - 1,
			MessageCount: windowMsgCount,
			CreatedAt:    now,
		}
		if chunkIndex > 0 {
			windows = append(windows, window)
		}
	}

	return ChunkResult{Chunks: chunks, Windows: windows}, nil
}

// ChunkCodexFile opens a file and chunks it.
func ChunkCodexFile(path string, opts ChunkOptions) (ChunkResult, error) {
	reader, err := codexjsonl.OpenReader(path)
	if err != nil {
		return ChunkResult{}, err
	}
	defer reader.Close()

	return ChunkCodex(reader, opts)
}

// ChunkCodexFromReader creates a reader from an io.Reader and chunks it.
func ChunkCodexFromReader(r interface{ Read([]byte) (int, error) }, opts ChunkOptions) (ChunkResult, error) {
	reader := codexjsonl.NewReader(r)
	return ChunkCodex(reader, opts)
}
