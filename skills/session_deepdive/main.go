// Package main implements the session/deep-dive skill for retrieving raw content from archives.
package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/sessionkit"
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
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	if in.SessionID == "" {
		return skillerr.Arg("session_id is required")
	}

	if in.Limit <= 0 {
		in.Limit = defaultLimit
	}

	// Open sessions store
	sessionStore, cleanup, err := sessionkit.OpenSessions(ctx, rc.Config)
	if err != nil {
		return skillerr.IO("open sessions store", skillerr.WithCause(err))
	}
	defer cleanup()

	// Get archive path
	archivePath, err := sessionStore.GetArchivePath(ctx, in.SessionID)
	if err != nil {
		return skillerr.Arg("session not found",
			skillerr.WithCause(err),
			skillerr.WithHint("Check session_id is correct and session exists"))
	}
	if archivePath == "" {
		return skillerr.Arg("session has not been archived",
			skillerr.WithHint("Run session/archive skill first to create archive"))
	}

	// Determine which chunks to retrieve
	var targetIndices []int

	if in.ChunkIndex > 0 {
		targetIndices = []int{in.ChunkIndex}
	} else if len(in.ChunkIndices) > 0 {
		targetIndices = in.ChunkIndices
	} else {
		// Get chunks from database
		chunks, err := sessionStore.GetChunks(ctx, in.SessionID, in.Limit)
		if err != nil {
			return skillerr.IO("get chunks", skillerr.WithCause(err))
		}
		for _, c := range chunks {
			targetIndices = append(targetIndices, c.ChunkIndex)
		}
	}

	if len(targetIndices) == 0 {
		output := Output{
			SessionID:   in.SessionID,
			ArchivePath: archivePath,
			Chunks:      []ChunkDetail{},
			TotalFound:  0,
			Status:      "no_chunks",
			Message:     "No chunks found for this session",
		}
		return skillout.Emit(rc, command, output)
	}

	// Get chunk metadata from database
	chunkMap := make(map[int]sessions.SessionChunk)
	for _, idx := range targetIndices {
		chunk, err := sessionStore.GetChunk(ctx, in.SessionID, idx)
		if err == nil {
			chunkMap[idx] = chunk
		}
	}

	// Read raw content from archive
	rawContents, err := readChunksFromArchive(archivePath, targetIndices)
	if err != nil {
		return skillerr.IO("read archive", skillerr.WithCause(err),
			skillerr.WithHint("Failed to read archive file; check file exists and is accessible"))
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
		SessionID:   in.SessionID,
		ArchivePath: archivePath,
		Chunks:      details,
		TotalFound:  len(details),
		Status:      "ok",
		Message:     fmt.Sprintf("Retrieved %d chunks from archive", len(details)),
	}

	return skillout.Emit(rc, command, output)
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
