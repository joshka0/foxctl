// Package main implements the session/deep-dive skill for retrieving raw content from session archives with detailed chunk analysis.
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/context/sessionkit/archive"
	"github.com/joshka0/foxctl/internal/storage/sessions"
)

// Input defines the skill input parameters for session deep-dive with flexible chunk selection and querying options.
type Input struct {
	SessionID    string `json:"session_id"`
	ChunkIndex   int    `json:"chunk_index,omitempty"`
	ChunkIndices []int  `json:"chunk_indices,omitempty"`
	Query        string `json:"query,omitempty"`
	Limit        int    `json:"limit,omitempty"`
}

// Output defines the skill output with comprehensive chunk details and archive information.
type Output struct {
	SessionID   string        `json:"session_id"`
	ArchivePath string        `json:"archive_path,omitempty"`
	Chunks      []ChunkDetail `json:"chunks"`
	TotalFound  int           `json:"total_found"`
	Status      string        `json:"status"`
	Message     string        `json:"message"`
}

// ChunkDetail provides full details about a session chunk with content, metadata, and error tracking.
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

// main is the skill entry point for session/deep-dive with comprehensive archive retrieval capabilities.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates session deep-dive operations with archive validation, chunk retrieval, and detailed metadata assembly.
//
// Index:
//
//	Purpose: Retrieve raw content and detailed metadata from session archives with flexible chunk selection
//	Keywords: session/deep-dive, archive_retrieval, chunk_analysis, session_metadata, raw_content_access
//	Related: archive.ReadChunksFromArchive, sessionkit.OpenSessions, sessions.SessionChunk
//	Flow: validate input → open sessions store → get archive path → determine target chunks → read archive → assemble details → emit results
//	Resources: session store, compressed archive files
//	Events: deep-dive retrieval events
//	OutputFields: session_id, archive_path, chunks, total_found
//
// [[domain:session-archive-deep-dive]]
// [[protocol:archive-chunk-retrieval]]
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	if in.SessionID == "" {
		return skillerr.Arg("session_id is required")
	}

	if in.Limit <= 0 {
		in.Limit = defaultLimit
	}

	// Open sessions store
	sessionStore, err := rc.Stores.Sessions(ctx)
	if err != nil {
		return skillerr.IO("open sessions store", skillerr.WithCause(err))
	}

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
	rawContents, err := archive.ReadChunksFromArchive(archivePath, targetIndices)
	if err != nil {
		return skillerr.WrapIO(
			"read archive",
			err,
			skillerr.WithHint("Failed to read archive file; check file exists and is accessible."),
		)
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
			detail.RawContent = json.RawMessage(raw)
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
