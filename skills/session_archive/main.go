// Package main implements the session/archive skill for JSONL archival and chunking.
package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
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
	DryRun       bool   `json:"dry_run,omitempty"`
}

// Output defines the skill output.
type Output struct {
	SessionID     string       `json:"session_id"`
	ArchivePath   string       `json:"archive_path,omitempty"`
	OriginalSize  int64        `json:"original_size"`
	CompressedSize int64       `json:"compressed_size,omitempty"`
	ChunkCount    int          `json:"chunk_count"`
	Chunks        []ChunkInfo  `json:"chunks,omitempty"`
	Status        string       `json:"status"`
	Message       string       `json:"message"`
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
	Type         string          `json:"type"`
	Role         string          `json:"role,omitempty"`
	Message      json.RawMessage `json:"message,omitempty"`
	Content      json.RawMessage `json:"content,omitempty"`
	ToolUse      *ToolUseInfo    `json:"tool_use,omitempty"`
	ToolResult   *ToolResultInfo `json:"tool_result,omitempty"`
	Timestamp    string          `json:"timestamp,omitempty"`
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

const (
	command            = "session/archive"
	defaultMaxChunk    = 4000 // tokens approximation
	maxPreviewLen      = 200
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

	// Parse JSONL and create chunks
	chunks, err := parseAndChunk(file, input.SessionID, input.MaxChunkSize)
	if err != nil {
		fail("PARSE_ERROR", fmt.Errorf("parse JSONL: %w", err))
	}

	output := Output{
		SessionID:    input.SessionID,
		OriginalSize: fileInfo.Size(),
		ChunkCount:   len(chunks),
		Status:       "ok",
	}

	// Build chunk info for output
	for _, c := range chunks {
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

	if input.DryRun {
		output.Message = fmt.Sprintf("Dry run: would create %d chunks from %d bytes", len(chunks), fileInfo.Size())
		env := envelope.OK(command, output)
		errs.Ignore(envelope.Write(os.Stdout, env), "emit session/archive result")
		return
	}

	// Create archives directory
	archiveDir := filepath.Join(agentctlHome, "archives")
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
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
	if err := sessionStore.SaveChunks(ctx, chunks); err != nil {
		fail("SAVE_ERROR", fmt.Errorf("save chunks: %w", err))
	}

	// Update session with archive path
	if err := sessionStore.SetArchivePath(ctx, input.SessionID, archivePath); err != nil {
		fail("UPDATE_ERROR", fmt.Errorf("set archive path: %w", err))
	}

	output.Message = fmt.Sprintf("Archived %d chunks, compressed %.1f%% (%d -> %d bytes)",
		len(chunks),
		100.0*(1.0-float64(compressedSize)/float64(fileInfo.Size())),
		fileInfo.Size(),
		compressedSize,
	)

	env := envelope.OK(command, output)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit session/archive result")
}

// parseAndChunk reads the JSONL file and creates chunks.
// maxChunkSize is reserved for future splitting of large messages.
func parseAndChunk(r io.Reader, sessionID string, _ int) ([]storage.SessionChunk, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024) // 10MB max line

	var chunks []storage.SessionChunk
	var byteOffset int64
	chunkIndex := 0

	for scanner.Scan() {
		line := scanner.Bytes()
		lineLen := int64(len(line)) + 1 // +1 for newline

		var msg JSONLMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			// Skip malformed lines
			byteOffset += lineLen
			continue
		}

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
			ID:             ulid.Make().String(),
			SessionID:      sessionID,
			ChunkIndex:     chunkIndex,
			ChunkType:      chunkType,
			ContentHash:    contentHash,
			ContentPreview: contentPreview,
			ByteOffset:     byteOffset,
			ByteLength:     lineLen,
			ToolsUsed:      toolsUsed,
			HasError:       hasError,
			ErrorType:      errorType,
			CreatedAt:      time.Now().UTC(),
		}

		chunks = append(chunks, chunk)
		chunkIndex++
		byteOffset += lineLen
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan JSONL: %w", err)
	}

	return chunks, nil
}

// determineChunkType determines the type of chunk from the message.
func determineChunkType(msg JSONLMessage) string {
	switch {
	case msg.Type == "user" || msg.Role == "user":
		return "user_request"
	case msg.Type == "assistant" || msg.Role == "assistant":
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
func extractPreview(msg JSONLMessage, maxLen int) string {
	var content string

	// Try different content fields
	if len(msg.Content) > 0 {
		var text string
		if err := json.Unmarshal(msg.Content, &text); err == nil {
			content = text
		} else {
			// Might be an array
			var parts []map[string]any
			if err := json.Unmarshal(msg.Content, &parts); err == nil {
				for _, p := range parts {
					if t, ok := p["text"].(string); ok {
						content = t
						break
					}
				}
			}
		}
	}

	if content == "" && len(msg.Message) > 0 {
		var text string
		if err := json.Unmarshal(msg.Message, &text); err == nil {
			content = text
		}
	}

	if content == "" && msg.ToolResult != nil {
		content = msg.ToolResult.Content
	}

	// Truncate
	if len(content) > maxLen {
		content = content[:maxLen] + "..."
	}

	return strings.TrimSpace(content)
}

// extractToolsUsed extracts tool names from the message.
func extractToolsUsed(msg JSONLMessage) []string {
	if msg.ToolUse != nil && msg.ToolUse.Name != "" {
		return []string{msg.ToolUse.Name}
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
