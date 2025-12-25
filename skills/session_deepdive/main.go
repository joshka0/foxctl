// Package main implements the session/deep-dive skill for retrieving raw content from archives.
package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
)

// Input defines the skill input parameters.
type Input struct {
	SessionID    string `json:"session_id"`
	ChunkIndex   int    `json:"chunk_index,omitempty"`
	ChunkIndices []int  `json:"chunk_indices,omitempty"`
	Query        string `json:"query,omitempty"`
	Limit        int    `json:"limit,omitempty"`
}

// Output defines the skill output.
type Output struct {
	SessionID   string        `json:"session_id"`
	ArchivePath string        `json:"archive_path,omitempty"`
	Chunks      []ChunkDetail `json:"chunks"`
	TotalFound  int           `json:"total_found"`
	Status      string        `json:"status"`
	Message     string        `json:"message"`
}

// ChunkDetail provides full details about a chunk.
type ChunkDetail struct {
	Index          int             `json:"index"`
	Type           string          `json:"type"`
	ContentPreview string          `json:"content_preview,omitempty"`
	RawContent     json.RawMessage `json:"raw_content,omitempty"`
	ToolsUsed      []string        `json:"tools_used,omitempty"`
	FilesTouched   []string        `json:"files_touched,omitempty"`
	HasError       bool            `json:"has_error"`
	ErrorType      string          `json:"error_type,omitempty"`
	ByteOffset     int64           `json:"byte_offset"`
	ByteLength     int64           `json:"byte_length"`
}

const (
	command      = "session/deep-dive"
	defaultLimit = 10
)

func main() {
	ctx := context.Background()

	// Read input from stdin
	var input Input
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		fail("EPARSE", fmt.Errorf("decode input: %w", err), "Ensure valid JSON on stdin")
	}

	if input.SessionID == "" {
		fail("EARG", fmt.Errorf("session_id is required"), "Provide session_id in input JSON")
	}

	if input.Limit <= 0 {
		input.Limit = defaultLimit
	}

	// Get agentctl home
	agentctlHome := os.Getenv("AGENTCTL_HOME")
	if agentctlHome == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			// Fall back to temp dir if home dir is unavailable
			homeDir = os.TempDir()
		}
		agentctlHome = filepath.Join(homeDir, ".agentctl")
	}

	// Open sessions store
	storageRoot := filepath.Join(agentctlHome, "storage")
	sessionStore, err := sessions.Open(ctx, storageRoot)
	if err != nil {
		fail("EIO", fmt.Errorf("open sessions store: %w", err), "Check that storage directory exists and is accessible")
	}
	defer func() { errs.Ignore(sessionStore.Close(), "close sessions store") }()

	// Get archive path
	archivePath, err := sessionStore.GetArchivePath(ctx, input.SessionID)
	if err != nil {
		fail("ENOTFOUND", fmt.Errorf("session not found: %w", err), "Check session_id is correct and session exists")
	}
	if archivePath == "" {
		fail("ENOTFOUND", fmt.Errorf("session has not been archived; run session/archive first"), "Run session/archive skill first to create archive")
	}

	// Determine which chunks to retrieve
	var targetIndices []int

	if input.ChunkIndex > 0 {
		targetIndices = []int{input.ChunkIndex}
	} else if len(input.ChunkIndices) > 0 {
		targetIndices = input.ChunkIndices
	} else {
		// Get chunks from database
		chunks, err := sessionStore.GetChunks(ctx, input.SessionID, input.Limit)
		if err != nil {
			fail("EIO", fmt.Errorf("get chunks: %w", err), "Database query failed; check storage is accessible")
		}
		for _, c := range chunks {
			targetIndices = append(targetIndices, c.ChunkIndex)
		}
	}

	if len(targetIndices) == 0 {
		output := Output{
			SessionID:   input.SessionID,
			ArchivePath: archivePath,
			Chunks:      []ChunkDetail{},
			TotalFound:  0,
			Status:      "no_chunks",
			Message:     "No chunks found for this session",
		}
		env := envelope.OK(command, output)
		errs.Ignore(envelope.Write(os.Stdout, env), "emit session/deep-dive result")
		return
	}

	// Get chunk metadata from database
	chunkMap := make(map[int]sessions.SessionChunk)
	for _, idx := range targetIndices {
		chunk, err := sessionStore.GetChunk(ctx, input.SessionID, idx)
		if err == nil {
			chunkMap[idx] = chunk
		}
	}

	// Read raw content from archive
	rawContents, err := readChunksFromArchive(archivePath, targetIndices)
	if err != nil {
		fail("EIO", fmt.Errorf("read archive: %w", err), "Failed to read archive file; check file exists and is accessible")
	}

	// Build output
	details := []ChunkDetail{}
	for _, idx := range targetIndices {
		detail := ChunkDetail{
			Index: idx,
		}

		if chunk, ok := chunkMap[idx]; ok {
			detail.Type = chunk.ChunkType
			detail.ContentPreview = chunk.ContentPreview
			detail.ToolsUsed = chunk.ToolsUsed
			detail.FilesTouched = chunk.FilesTouched
			detail.HasError = chunk.HasError
			detail.ErrorType = chunk.ErrorType
			detail.ByteOffset = chunk.ByteOffset
			detail.ByteLength = chunk.ByteLength
		}

		if raw, ok := rawContents[idx]; ok {
			detail.RawContent = raw
		}

		details = append(details, detail)
	}

	output := Output{
		SessionID:   input.SessionID,
		ArchivePath: archivePath,
		Chunks:      details,
		TotalFound:  len(details),
		Status:      "ok",
		Message:     fmt.Sprintf("Retrieved %d chunks from archive", len(details)),
	}

	env := envelope.OK(command, output)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit session/deep-dive result")
}

// readChunksFromArchive reads specific lines from a gzipped JSONL file.
func readChunksFromArchive(archivePath string, indices []int) (map[int]json.RawMessage, error) {
	// Create a set of target indices for fast lookup
	targetSet := make(map[int]bool)
	for _, idx := range indices {
		targetSet[idx] = true
	}

	// Open gzipped file
	file, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("create gzip reader: %w", err)
	}
	defer gzReader.Close()

	// Read line by line
	results := make(map[int]json.RawMessage)
	scanner := bufio.NewScanner(gzReader)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024) // 10MB max line

	lineNum := 0
	for scanner.Scan() {
		if targetSet[lineNum] {
			// Make a copy of the line data
			line := make([]byte, len(scanner.Bytes()))
			copy(line, scanner.Bytes())
			results[lineNum] = json.RawMessage(line)

			// Check if we have all we need
			if len(results) == len(targetSet) {
				break
			}
		}
		lineNum++
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan archive: %w", err)
	}

	return results, nil
}

func fail(code string, err error, hint string) {
	var data map[string]any
	if hint != "" {
		data = map[string]any{"hint": hint}
	}
	env := envelope.Error(command, code, err.Error(), data)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit session/deep-dive failure")
	os.Exit(1)
}
