// Package main implements the session/archive skill for JSONL archival and chunking.
package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
	"github.com/oklog/ulid/v2"
)

// Input defines the skill input parameters.
type Input struct {
	SessionID    string `json:"session_id"`
	JSONLPath    string `json:"jsonl_path,omitempty"`
	MaxChunkSize int    `json:"max_chunk_size,omitempty"`
	EmbedWindows bool   `json:"embed_windows,omitempty"` // Generate embeddings for context windows
	DryRun       bool   `json:"dry_run,omitempty"`
}

// Output defines the skill output.
type Output struct {
	SessionID       string       `json:"session_id"`
	ArchivePath     string       `json:"archive_path,omitempty"`
	OriginalSize    int64        `json:"original_size"`
	CompressedSize  int64        `json:"compressed_size,omitempty"`
	ChunkCount      int          `json:"chunk_count"`
	WindowCount     int          `json:"window_count"`
	EmbeddedWindows int          `json:"embedded_windows,omitempty"`
	EmbeddingModel  string       `json:"embedding_model,omitempty"`
	Windows         []WindowInfo `json:"windows,omitempty"`
	Chunks          []ChunkInfo  `json:"chunks,omitempty"`
	Status          string       `json:"status"`
	Message         string       `json:"message"`
}

// WindowInfo provides info about a context window.
type WindowInfo struct {
	Index            int    `json:"index"`
	StartedAt        string `json:"started_at,omitempty"`
	EndedAt          string `json:"ended_at,omitempty"`
	PreCompactTokens int    `json:"pre_compact_tokens,omitempty"`
	Trigger          string `json:"trigger,omitempty"`
	ChunkCount       int    `json:"chunk_count"`
}

// ChunkInfo provides info about a created chunk.
type ChunkInfo struct {
	Index          int      `json:"index"`
	Type           string   `json:"type"`
	ByteOffset     int64    `json:"byte_offset"`
	ByteLength     int64    `json:"byte_length"`
	ContentPreview string   `json:"content_preview,omitempty"`
	ToolsUsed      []string `json:"tools_used,omitempty"`
	HasError       bool     `json:"has_error,omitempty"`
}

// JSONLMessage represents a message in the Claude Code JSONL format.
type JSONLMessage struct {
	Type            string           `json:"type"`
	Subtype         string           `json:"subtype,omitempty"`
	Role            string           `json:"role,omitempty"`
	Message         json.RawMessage  `json:"message,omitempty"`
	Content         json.RawMessage  `json:"content,omitempty"`
	ToolUse         *ToolUseInfo     `json:"tool_use,omitempty"`
	ToolResult      *ToolResultInfo  `json:"tool_result,omitempty"`
	Timestamp       string           `json:"timestamp,omitempty"`
	CompactMetadata *CompactMetadata `json:"compactMetadata,omitempty"`
}

// CompactMetadata contains metadata about a compaction event.
type CompactMetadata struct {
	Trigger   string `json:"trigger"`   // "auto" or "manual"
	PreTokens int    `json:"preTokens"` // tokens before compaction
}

// ToolUseInfo represents tool use in a message.
type ToolUseInfo struct {
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input,omitempty"`
}

// ToolResultInfo represents a tool result.
type ToolResultInfo struct {
	ToolUseID string `json:"tool_use_id,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
	Content   string `json:"content,omitempty"`
}

// NestedMessage represents the nested message structure in Claude JSONL.
// The .message field contains {role, content} where content is either
// a string (user messages) or an array of content blocks (assistant messages).
type NestedMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// ContentBlock represents a content block in assistant messages.
type ContentBlock struct {
	Type     string `json:"type"` // "text", "tool_use", "thinking"
	Text     string `json:"text,omitempty"`
	Thinking string `json:"thinking,omitempty"`
	Name     string `json:"name,omitempty"` // tool name for tool_use
}

// UserContentBlock represents a content block in user messages.
// User messages can contain text blocks and tool_result blocks.
type UserContentBlock struct {
	Type      string `json:"type"` // "text", "tool_result"
	Text      string `json:"text,omitempty"`
	ToolUseID string `json:"tool_use_id,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
	// Content field is intentionally NOT included - it contains file contents, command output, etc.
}

const (
	command         = "session/archive"
	defaultMaxChunk = 4000 // tokens approximation
	maxPreviewLen   = 200
)

func main() {
	ctx := context.Background()

	// Read input from stdin
	var input Input
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		fail("DECODE_ERROR", fmt.Errorf("decode input: %w", err))
	}

	if input.SessionID == "" {
		fail("INVALID_INPUT", fmt.Errorf("session_id is required"))
	}

	if input.MaxChunkSize <= 0 {
		input.MaxChunkSize = defaultMaxChunk
	}

	// Get agentctl home
	agentctlHome := os.Getenv("AGENTCTL_HOME")
	if agentctlHome == "" {
		homeDir, _ := os.UserHomeDir()
		agentctlHome = filepath.Join(homeDir, ".agentctl")
	}

	// Open sessions store
	storageRoot := filepath.Join(agentctlHome, "storage")
	sessionStore, err := sessions.Open(ctx, storageRoot)
	if err != nil {
		fail("STORE_ERROR", fmt.Errorf("open sessions store: %w", err))
	}
	defer func() { errs.Ignore(sessionStore.Close(), "close sessions store") }()

	// Get session to find JSONL path
	session, err := sessionStore.Get(ctx, input.SessionID)
	if err != nil {
		fail("NOT_FOUND", fmt.Errorf("session not found: %w", err))
	}

	// Determine JSONL path
	jsonlPath := input.JSONLPath
	if jsonlPath == "" {
		jsonlPath = session.RawJSONLPath
	}
	if jsonlPath == "" {
		// Try to find in Claude's default location
		jsonlPath = findClaudeJSONL(session.WorkspacePath, input.SessionID)
	}
	if jsonlPath == "" {
		fail("NO_JSONL", fmt.Errorf("no JSONL path found for session; specify jsonl_path"))
	}

	// Open and read JSONL
	file, err := os.Open(jsonlPath)
	if err != nil {
		fail("FILE_ERROR", fmt.Errorf("open JSONL: %w", err))
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		fail("FILE_ERROR", fmt.Errorf("stat JSONL: %w", err))
	}

	// Parse JSONL and create chunks and context windows
	result, err := parseAndChunk(file, input.SessionID, input.MaxChunkSize)
	if err != nil {
		fail("PARSE_ERROR", fmt.Errorf("parse JSONL: %w", err))
	}

	output := Output{
		SessionID:    input.SessionID,
		OriginalSize: fileInfo.Size(),
		ChunkCount:   len(result.chunks),
		WindowCount:  len(result.windows),
		Status:       "ok",
	}

	// Build chunk info for output
	for _, c := range result.chunks {
		output.Chunks = append(output.Chunks, ChunkInfo{
			Index:          c.ChunkIndex,
			Type:           c.ChunkType,
			ByteOffset:     c.ByteOffset,
			ByteLength:     c.ByteLength,
			ContentPreview: c.ContentPreview,
			ToolsUsed:      c.ToolsUsed,
			HasError:       c.HasError,
		})
	}

	// Build window info for output
	for _, w := range result.windows {
		output.Windows = append(output.Windows, WindowInfo{
			Index:            w.WindowIndex,
			StartedAt:        formatTimestamp(w.StartedAt),
			EndedAt:          formatTimestamp(w.EndedAt),
			PreCompactTokens: w.PreCompactTokens,
			Trigger:          w.Trigger,
			ChunkCount:       w.ChunkEnd - w.ChunkStart + 1,
		})
	}

	if input.DryRun {
		output.Message = fmt.Sprintf("Dry run: would create %d chunks in %d context windows from %d bytes",
			len(result.chunks), len(result.windows), fileInfo.Size())
		env := envelope.OK(command, output)
		errs.Ignore(envelope.Write(os.Stdout, env), "emit session/archive result")
		return
	}

	// Create archives directory
	archiveDir := filepath.Join(agentctlHome, "archives")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		fail("DIR_ERROR", fmt.Errorf("create archives dir: %w", err))
	}

	// Compress and save
	archivePath := filepath.Join(archiveDir, fmt.Sprintf("%s.jsonl.gz", input.SessionID))
	compressedSize, err := compressFile(jsonlPath, archivePath)
	if err != nil {
		fail("COMPRESS_ERROR", fmt.Errorf("compress JSONL: %w", err))
	}

	output.ArchivePath = archivePath
	output.CompressedSize = compressedSize

	// Save chunks to database
	if err := sessionStore.SaveChunks(ctx, result.chunks); err != nil {
		fail("SAVE_ERROR", fmt.Errorf("save chunks: %w", err))
	}

	// Save context windows to database
	if err := sessionStore.SaveContextWindows(ctx, result.windows); err != nil {
		fail("SAVE_ERROR", fmt.Errorf("save context windows: %w", err))
	}

	// Generate embeddings for context windows if requested
	if input.EmbedWindows && len(result.windows) > 0 {
		embeddedCount, embeddingModel := embedContextWindows(ctx, sessionStore, result.windows, result.chunks)
		output.EmbeddedWindows = embeddedCount
		output.EmbeddingModel = embeddingModel
	}

	// Update session with archive path
	if err := sessionStore.SetArchivePath(ctx, input.SessionID, archivePath); err != nil {
		fail("UPDATE_ERROR", fmt.Errorf("set archive path: %w", err))
	}

	output.Message = fmt.Sprintf("Archived %d chunks in %d context windows, compressed %.1f%% (%d -> %d bytes)",
		len(result.chunks),
		len(result.windows),
		100.0*(1.0-float64(compressedSize)/float64(fileInfo.Size())),
		fileInfo.Size(),
		compressedSize,
	)

	env := envelope.OK(command, output)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit session/archive result")
}

// parseResult contains chunks and context windows from parsing.
type parseResult struct {
	chunks  []storage.SessionChunk
	windows []storage.ContextWindow
}

// parseAndChunk reads the JSONL file and creates chunks and context windows.
// maxChunkSize is reserved for future splitting of large messages.
func parseAndChunk(r io.Reader, sessionID string, _ int) (parseResult, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024) // 10MB max line

	var chunks []storage.SessionChunk
	var windows []storage.ContextWindow
	var byteOffset int64
	chunkIndex := 0
	windowIndex := 0

	// Track current window state
	var windowStartChunk int
	var windowStartTime time.Time
	var windowMsgCount int
	var firstMsgTime time.Time

	now := time.Now().UTC()

	for scanner.Scan() {
		line := scanner.Bytes()
		lineLen := int64(len(line)) + 1 // +1 for newline

		var msg JSONLMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			// Skip malformed lines
			byteOffset += lineLen
			continue
		}

		// Parse timestamp if available
		var msgTime time.Time
		if msg.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339, msg.Timestamp); err == nil {
				msgTime = t
			} else if t, err := time.Parse(time.RFC3339Nano, msg.Timestamp); err == nil {
				msgTime = t
			}
		}

		// Track first message time for window
		if firstMsgTime.IsZero() && !msgTime.IsZero() {
			firstMsgTime = msgTime
			windowStartTime = msgTime
		}

		// Check for compact_boundary
		isCompactBoundary := msg.Type == "system" && msg.Subtype == "compact_boundary"

		// Determine chunk type and content
		chunkType := determineChunkType(msg)
		contentPreview := extractPreview(msg, maxPreviewLen)
		toolsUsed := extractToolsUsed(msg)
		hasError := checkHasError(msg)
		errorType := extractErrorType(msg)

		// Create content hash
		hash := sha256.Sum256(line)
		contentHash := hex.EncodeToString(hash[:])

		chunk := storage.SessionChunk{
			ID:                 ulid.Make().String(),
			SessionID:          sessionID,
			ChunkIndex:         chunkIndex,
			ChunkType:          chunkType,
			ContentHash:        contentHash,
			ContentPreview:     contentPreview,
			ByteOffset:         byteOffset,
			ByteLength:         lineLen,
			ToolsUsed:          toolsUsed,
			HasError:           hasError,
			ErrorType:          errorType,
			ContextWindowIndex: windowIndex,
			CreatedAt:          now,
		}

		chunks = append(chunks, chunk)
		windowMsgCount++

		// If this is a compact_boundary, close the current window and start a new one
		if isCompactBoundary {
			// Extract compaction metadata
			var preTokens int
			var trigger string
			if msg.CompactMetadata != nil {
				preTokens = msg.CompactMetadata.PreTokens
				trigger = msg.CompactMetadata.Trigger
			}

			// Create the window that just ended
			window := storage.ContextWindow{
				ID:               ulid.Make().String(),
				SessionID:        sessionID,
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
			windowStartTime = msgTime
			windowMsgCount = 0
		}

		chunkIndex++
		byteOffset += lineLen
	}

	if err := scanner.Err(); err != nil {
		return parseResult{}, fmt.Errorf("scan JSONL: %w", err)
	}

	// Create final window for remaining chunks (if any messages after last boundary or no boundaries)
	if windowMsgCount > 0 || len(windows) == 0 {
		window := storage.ContextWindow{
			ID:           ulid.Make().String(),
			SessionID:    sessionID,
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

	return parseResult{chunks: chunks, windows: windows}, nil
}

// determineChunkType determines the type of chunk from the message.
func determineChunkType(msg JSONLMessage) string {
	switch {
	case msg.Type == "system" && msg.Subtype == "compact_boundary":
		return "compact_boundary"
	case msg.Type == "user" || msg.Role == "user":
		// Check if this is a tool_result response (appears as "user" message with array content)
		if len(msg.Message) > 0 {
			var nested NestedMessage
			if err := json.Unmarshal(msg.Message, &nested); err == nil && len(nested.Content) > 0 {
				// If content is an array, check if first block is tool_result
				var blocks []map[string]any
				if json.Unmarshal(nested.Content, &blocks) == nil && len(blocks) > 0 {
					if blockType, ok := blocks[0]["type"].(string); ok && blockType == "tool_result" {
						// Check for errors in tool results
						for _, block := range blocks {
							if isErr, ok := block["is_error"].(bool); ok && isErr {
								return "error"
							}
						}
						return "tool_output"
					}
				}
			}
		}
		return "user_request"
	case msg.Type == "assistant" || msg.Role == "assistant":
		// Check content blocks for tool_use vs text
		if len(msg.Message) > 0 {
			var nested NestedMessage
			if err := json.Unmarshal(msg.Message, &nested); err == nil && len(nested.Content) > 0 {
				var blocks []ContentBlock
				if json.Unmarshal(nested.Content, &blocks) == nil {
					hasToolUse := false
					hasText := false
					for _, block := range blocks {
						if block.Type == "tool_use" {
							hasToolUse = true
						}
						if block.Type == "text" && block.Text != "" {
							hasText = true
						}
					}
					// If only tool_use (no text), classify as tool_use
					if hasToolUse && !hasText {
						return "tool_use"
					}
				}
			}
		}
		if msg.ToolUse != nil {
			return "tool_use"
		}
		return "assistant_response"
	case msg.Type == "tool_result" || msg.ToolResult != nil:
		if msg.ToolResult != nil && msg.ToolResult.IsError {
			return "error"
		}
		return "tool_output"
	default:
		return "other"
	}
}

// extractPreview extracts a content preview from the message.
// Follows session_summarize filtering approach:
// - User messages: only string content or text blocks (skip tool_result content)
// - Assistant messages: only text blocks (skip thinking blocks)
// - Tool results: intentionally NOT included (contains file contents, command output)
func extractPreview(msg JSONLMessage, maxLen int) string {
	var content string

	// Claude Code JSONL: content is inside .message.content
	if len(msg.Message) > 0 {
		var nested NestedMessage
		if err := json.Unmarshal(msg.Message, &nested); err == nil && len(nested.Content) > 0 {
			// Try as string first (user direct input)
			var textContent string
			if err := json.Unmarshal(nested.Content, &textContent); err == nil {
				content = textContent
			} else if nested.Role == "user" {
				// User message with array content - extract only text blocks
				var userBlocks []UserContentBlock
				if json.Unmarshal(nested.Content, &userBlocks) == nil {
					var textParts []string
					for _, block := range userBlocks {
						if block.Type == "text" && block.Text != "" {
							textParts = append(textParts, block.Text)
						}
						// Skip tool_result blocks - they contain file contents, command output
					}
					content = strings.Join(textParts, "\n")
				}
			} else {
				// Assistant message - extract only text blocks
				var blocks []ContentBlock
				if json.Unmarshal(nested.Content, &blocks) == nil {
					var textParts []string
					for _, block := range blocks {
						if block.Type == "text" && block.Text != "" {
							textParts = append(textParts, block.Text)
						}
						// Skip thinking blocks and tool_use (tools are tracked separately)
					}
					content = strings.Join(textParts, "\n")
				}
			}
		}
	}

	// Fallback: try top-level content field (for other message formats)
	if content == "" && len(msg.Content) > 0 {
		var text string
		if err := json.Unmarshal(msg.Content, &text); err == nil {
			content = text
		}
		// Don't try array fallback - we want to avoid tool_result content
	}

	// Note: intentionally NOT including msg.ToolResult.Content
	// Tool results contain file contents, command output - not useful for embeddings

	// Truncate
	if len(content) > maxLen {
		content = content[:maxLen] + "..."
	}

	return strings.TrimSpace(content)
}

// extractToolsUsed extracts tool names from the message.
func extractToolsUsed(msg JSONLMessage) []string {
	// Check top-level tool use
	if msg.ToolUse != nil && msg.ToolUse.Name != "" {
		return []string{msg.ToolUse.Name}
	}

	// Check nested content blocks for tool_use
	if len(msg.Message) > 0 {
		var nested NestedMessage
		if err := json.Unmarshal(msg.Message, &nested); err == nil && len(nested.Content) > 0 {
			var blocks []ContentBlock
			if err := json.Unmarshal(nested.Content, &blocks); err == nil {
				var tools []string
				for _, block := range blocks {
					if block.Type == "tool_use" && block.Name != "" {
						tools = append(tools, block.Name)
					}
				}
				if len(tools) > 0 {
					return tools
				}
			}
		}
	}

	return nil
}

// checkHasError checks if the message contains an error.
func checkHasError(msg JSONLMessage) bool {
	if msg.ToolResult != nil && msg.ToolResult.IsError {
		return true
	}
	return false
}

// extractErrorType extracts the error type if present.
func extractErrorType(msg JSONLMessage) string {
	if msg.ToolResult != nil && msg.ToolResult.IsError {
		content := msg.ToolResult.Content
		if strings.Contains(content, "TypeError") {
			return "TypeError"
		}
		if strings.Contains(content, "SyntaxError") {
			return "SyntaxError"
		}
		if strings.Contains(content, "compile") || strings.Contains(content, "build") {
			return "CompileError"
		}
		return "ToolError"
	}
	return ""
}

// compressFile compresses a file using gzip.
func compressFile(src, dst string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer out.Close()

	gw := gzip.NewWriter(out)
	gw.Name = filepath.Base(src)
	gw.ModTime = time.Now()

	if _, err := io.Copy(gw, in); err != nil {
		return 0, err
	}

	if err := gw.Close(); err != nil {
		return 0, err
	}

	info, err := out.Stat()
	if err != nil {
		return 0, err
	}

	return info.Size(), nil
}

// findClaudeJSONL tries to find the JSONL file in Claude's default locations.
func findClaudeJSONL(workspacePath, sessionID string) string {
	homeDir, _ := os.UserHomeDir()

	// Try various possible locations
	patterns := []string{}

	// Try workspace-specific path first if provided
	if workspacePath != "" {
		patterns = append(patterns,
			filepath.Join(workspacePath, ".claude", "sessions", sessionID+".jsonl"),
		)
	}

	// Then try global Claude locations
	patterns = append(patterns,
		filepath.Join(homeDir, ".claude", "projects", "*", "sessions", sessionID+".jsonl"),
		filepath.Join(homeDir, ".claude", "projects", "*", sessionID+".jsonl"),
	)

	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err == nil && len(matches) > 0 {
			return matches[0]
		}
	}

	return ""
}

func fail(code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit session/archive failure")
	os.Exit(1)
}

// formatTimestamp formats a time as RFC3339 or returns empty string if zero.
func formatTimestamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// embedContextWindows generates embeddings for each context window.
// Returns the count of successfully embedded windows and the model used.
func embedContextWindows(
	ctx context.Context,
	store storage.SessionStore,
	windows []storage.ContextWindow,
	chunks []storage.SessionChunk,
) (int, string) {
	voyageKey := os.Getenv("VOYAGE_API_KEY")
	if voyageKey == "" {
		return 0, ""
	}

	// Create Voyage provider with rate limiting
	vp, err := semantic.NewVoyageProvider(semantic.VoyageConfig{
		APIKey:        voyageKey,
		Model:         "voyage-3.5", // Good balance of quality and cost for summaries
		RateLimitWait: boolPtr(true),
	})
	if err != nil {
		return 0, ""
	}

	// Build a map of chunk index to chunk for efficient lookup
	chunkMap := make(map[int]storage.SessionChunk)
	for _, c := range chunks {
		chunkMap[c.ChunkIndex] = c
	}

	embeddedCount := 0
	for _, window := range windows {
		// Build embedding text from chunk previews in this window
		embeddingText := buildWindowEmbeddingText(window, chunkMap)
		if embeddingText == "" {
			continue
		}

		// Generate embedding
		embedding, err := vp.Embed(ctx, embeddingText)
		if err != nil {
			continue // Skip this window on error
		}

		// Serialize and save embedding
		embeddingBytes := serializeEmbedding(embedding)
		if err := store.UpdateWindowSummary(ctx, window.ID, "", embeddingBytes, vp.Model()); err != nil {
			continue
		}

		embeddedCount++
	}

	return embeddedCount, vp.Model()
}

// buildWindowEmbeddingText creates text to embed from a window's chunks.
// Applies filtering similar to session_summarize to focus on high-signal content:
// - User requests (what was asked)
// - Assistant responses (what was answered/decided)
// - Errors (what went wrong)
// - Tool patterns (which tools were used)
// Excludes: file contents, command output, raw code, tool inputs/outputs.
func buildWindowEmbeddingText(window storage.ContextWindow, chunkMap map[int]storage.SessionChunk) string {
	var parts []string

	// Add window metadata
	if window.Trigger != "" {
		parts = append(parts, fmt.Sprintf("Context window %d (trigger: %s)", window.WindowIndex, window.Trigger))
	}

	// Track unique tools used in this window
	toolsSeen := make(map[string]bool)

	// Collect content from chunks in this window
	for i := window.ChunkStart; i <= window.ChunkEnd; i++ {
		chunk, ok := chunkMap[i]
		if !ok {
			continue
		}

		// Track tool usage
		for _, tool := range chunk.ToolsUsed {
			toolsSeen[tool] = true
		}

		// Only include high-signal chunk types with filtered content
		switch chunk.ChunkType {
		case "user_request":
			// Filter out noise from user requests
			filtered := filterEmbeddingContent(chunk.ContentPreview)
			if filtered != "" {
				parts = append(parts, "User: "+truncateText(filtered, 500))
			}
		case "assistant_response":
			// Filter out noise from assistant responses
			filtered := filterEmbeddingContent(chunk.ContentPreview)
			if filtered != "" {
				parts = append(parts, "Assistant: "+truncateText(filtered, 300))
			}
		case "error":
			// Errors are always valuable for understanding what went wrong
			if chunk.ContentPreview != "" {
				parts = append(parts, "Error: "+truncateText(chunk.ContentPreview, 200))
			}
		}
		// Skip: tool_use, tool_output, compact_boundary, other
	}

	// Add tool pattern summary at the end
	if len(toolsSeen) > 0 {
		tools := make([]string, 0, len(toolsSeen))
		for tool := range toolsSeen {
			tools = append(tools, tool)
		}
		parts = append(parts, "Tools used: "+strings.Join(tools, ", "))
	}

	// Limit total text to avoid embedding API limits
	result := strings.Join(parts, "\n")
	if len(result) > 8000 {
		result = result[:8000]
	}

	return result
}

// filterEmbeddingContent removes low-signal content from text before embedding.
// Filters out: file contents, code blocks, command output, paths, raw data.
func filterEmbeddingContent(content string) string {
	if content == "" {
		return ""
	}

	// Skip if it looks like file contents or code (heuristics)
	// These patterns indicate raw data rather than semantic content
	noisePatterns := []string{
		"package ", "import (", "func ", "type ", "const ", "var ", // Go code
		"function ", "class ", "export ", "import ", "require(", // JS/TS code
		"def ", "class ", "import ", "from ", // Python code
		"<html", "<div", "<span", "<!DOCTYPE", // HTML
		"{\"", "[{", ":{", // JSON-like
		"/Users/", "/home/", "/var/", "/tmp/", "C:\\", // Paths
		"```", "~~~", // Code blocks
		"error:", "Error:", "warning:", "Warning:", // Already captured in error chunks
	}

	for _, pattern := range noisePatterns {
		if strings.Contains(content, pattern) {
			// If it's mostly code/data, skip entirely
			if strings.Count(content, pattern) > 1 || len(content) > 500 {
				return ""
			}
		}
	}

	// Skip if content is mostly non-alphabetic (likely data, not natural language)
	alphaCount := 0
	for _, r := range content {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			alphaCount++
		}
	}
	if len(content) > 50 && float64(alphaCount)/float64(len(content)) < 0.4 {
		return ""
	}

	return strings.TrimSpace(content)
}

// truncateText truncates text to maxLen with ellipsis.
func truncateText(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// serializeEmbedding converts a float32 slice to binary bytes.
func serializeEmbedding(embedding []float32) []byte {
	buf := make([]byte, len(embedding)*4)
	for i, v := range embedding {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

// boolPtr returns a pointer to a bool value.
func boolPtr(b bool) *bool {
	return &b
}
