// Package main implements the session/archive skill for JSONL archival and chunking.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/workspaceutil"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/sessionkit"
	"github.com/jkatigb/agentctl/internal/sessionkit/archive"
	"github.com/jkatigb/agentctl/internal/sessionkit/claudejsonl"
	"github.com/jkatigb/agentctl/internal/sessionkit/codexjsonl"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
	"github.com/jkatigb/agentctl/internal/storage/vector"
)

// Input defines the skill input parameters.
type Input struct {
	SessionID    string `json:"session_id"`
	JSONLPath    string `json:"jsonl_path,omitempty"`
	Workspace    string `json:"workspace,omitempty"` // Workspace path (for auto-creating session)
	Source       string `json:"source,omitempty"`    // "claude" (default) or "codex"
	MaxChunkSize int    `json:"max_chunk_size,omitempty"`
	EmbedWindows bool   `json:"embed_windows,omitempty"` // Generate embeddings for context windows
	Force        bool   `json:"force,omitempty"`         // Force re-archive even if already done
	DryRun       bool   `json:"dry_run,omitempty"`
}

// Output defines the skill output.
type Output struct {
	SessionID       string               `json:"session_id"`
	ArchivePath     string               `json:"archive_path,omitempty"`
	OriginalSize    int64                `json:"original_size"`
	CompressedSize  int64                `json:"compressed_size,omitempty"`
	ChunkCount      int                  `json:"chunk_count"`
	WindowCount     int                  `json:"window_count"`
	EmbeddedWindows int                  `json:"embedded_windows,omitempty"`
	EmbeddingModel  string               `json:"embedding_model,omitempty"`
	Windows         []archive.WindowInfo `json:"windows,omitempty"`
	Chunks          []archive.ChunkInfo  `json:"chunks,omitempty"`
	Status          string               `json:"status"`
	Message         string               `json:"message"`
}

const command = "session/archive"

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	if in.SessionID == "" {
		return skillerr.Arg("session_id is required")
	}

	source := strings.ToLower(strings.TrimSpace(in.Source))
	if source == "" {
		source = "claude"
	}
	if source != "claude" && source != "codex" {
		return skillerr.Arg("source must be \"claude\" or \"codex\"")
	}

	paths := sessionkit.ResolvePaths(rc.Config)

	if in.MaxChunkSize <= 0 {
		if source == "codex" {
			in.MaxChunkSize = archive.DefaultCodexWindowTokens
		} else {
			in.MaxChunkSize = archive.DefaultMaxChunkSize
		}
	}

	// Open sessions store
	sessionStore, cleanup, err := sessionkit.OpenSessions(ctx, rc.Config)
	if err != nil {
		return skillerr.IO("open sessions store", skillerr.WithCause(err))
	}
	defer cleanup()

	// Get or create session
	session, err := sessionStore.Get(ctx, in.SessionID)
	if err != nil {
		// Session doesn't exist - create it if we have jsonl_path
		if in.JSONLPath == "" {
			return skillerr.Arg("session not found and no jsonl_path provided")
		}
		// Create a minimal session record
		workspace := workspaceutil.Resolve(in.Workspace, "", rc.Workspace)
		session = sessions.Session{
			ID:            in.SessionID,
			WorkspacePath: workspace,
			RawJSONLPath:  in.JSONLPath,
			Status:        "active",
			AgentType:     source,
		}
		if _, err := sessionStore.Save(ctx, session); err != nil {
			return skillerr.IO("create session", skillerr.WithCause(err))
		}
	}

	// Determine JSONL path
	jsonlPath := in.JSONLPath
	if jsonlPath == "" {
		jsonlPath = session.RawJSONLPath
	}
	if jsonlPath == "" {
		switch source {
		case "claude":
			jsonlPath = claudejsonl.LocateSessionJSONL(session.WorkspacePath, in.SessionID)
		case "codex":
			jsonlPath = codexjsonl.LocateSessionJSONL(in.SessionID)
		}
	}
	if jsonlPath == "" {
		return skillerr.Arg("no JSONL path found for session; specify jsonl_path")
	}

	// Get file info
	fileInfo, err := os.Stat(jsonlPath)
	if err != nil {
		return skillerr.IO("stat JSONL", skillerr.WithCause(err))
	}

	// Idempotent by default: skip chunks we've already archived (unless force)
	var skipToChunk int
	var startWindowIndex int
	if !in.Force {
		existingWindows, err := sessionStore.GetContextWindows(ctx, in.SessionID)
		if err == nil && len(existingWindows) > 0 {
			// Find the highest chunk_end from existing windows
			for _, w := range existingWindows {
				if w.ChunkEnd >= skipToChunk {
					skipToChunk = w.ChunkEnd + 1 // Skip to the chunk after the last archived
				}
				if w.WindowIndex >= startWindowIndex {
					startWindowIndex = w.WindowIndex + 1
				}
			}
		}
	}

	// Parse JSONL and create chunks and context windows using archive package
	var result archive.ChunkResult
	switch source {
	case "claude":
		result, err = archive.ChunkFile(jsonlPath, archive.ChunkOptions{
			SessionID:        in.SessionID,
			MaxChunkSize:     in.MaxChunkSize,
			SkipToChunk:      skipToChunk,
			StartWindowIndex: startWindowIndex,
		})
	case "codex":
		result, err = archive.ChunkCodexFile(jsonlPath, archive.ChunkOptions{
			SessionID:        in.SessionID,
			MaxChunkSize:     in.MaxChunkSize,
			SkipToChunk:      skipToChunk,
			StartWindowIndex: startWindowIndex,
		})
	}
	if err != nil {
		return skillerr.IO("parse JSONL", skillerr.WithCause(err))
	}

	output := Output{
		SessionID:    in.SessionID,
		OriginalSize: fileInfo.Size(),
		ChunkCount:   len(result.Chunks),
		WindowCount:  len(result.Windows),
		Status:       "ok",
	}

	// Build chunk info for output
	for _, c := range result.Chunks {
		output.Chunks = append(output.Chunks, archive.ChunkInfo{
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
	for _, w := range result.Windows {
		output.Windows = append(output.Windows, archive.WindowInfo{
			Index:            w.WindowIndex,
			StartedAt:        archive.FormatTimestamp(w.StartedAt),
			EndedAt:          archive.FormatTimestamp(w.EndedAt),
			PreCompactTokens: w.PreCompactTokens,
			Trigger:          w.Trigger,
			ChunkCount:       w.ChunkEnd - w.ChunkStart + 1,
		})
	}

	if in.DryRun {
		output.Message = fmt.Sprintf("Dry run: would create %d chunks in %d context windows from %d bytes",
			len(result.Chunks), len(result.Windows), fileInfo.Size())
		return skillout.Emit(rc, command, output)
	}

	// Create archives directory
	if err := os.MkdirAll(paths.ArchivesDir, 0o755); err != nil {
		return skillerr.IO("create archives dir", skillerr.WithCause(err))
	}

	// Compress and save
	archivePath := archive.ArchivePath(paths.ArchivesDir, in.SessionID)
	compressedSize, err := archive.CompressFile(jsonlPath, archivePath)
	if err != nil {
		return skillerr.IO("compress JSONL", skillerr.WithCause(err))
	}

	output.ArchivePath = archivePath
	output.CompressedSize = compressedSize

	// Save chunks to database
	if err := sessionStore.SaveChunks(ctx, result.Chunks); err != nil {
		return skillerr.IO("save chunks", skillerr.WithCause(err))
	}

	// Save context windows to database
	if err := sessionStore.SaveContextWindows(ctx, result.Windows); err != nil {
		return skillerr.IO("save context windows", skillerr.WithCause(err))
	}

	// Generate embeddings for context windows if requested
	if in.EmbedWindows && len(result.Windows) > 0 {
		// Refetch windows to get the actual IDs (handles conflict case where existing IDs are kept)
		savedWindows, err := sessionStore.GetContextWindows(ctx, in.SessionID)
		if err != nil {
			// Non-fatal - just skip embedding
			savedWindows = nil
		}
		if len(savedWindows) > 0 {
			embeddedCount, embeddingModel := embedContextWindows(ctx, sessionStore, savedWindows, result.Chunks, rc.Config)
			output.EmbeddedWindows = embeddedCount
			output.EmbeddingModel = embeddingModel
		}
	}

	// Update session with archive path
	if err := sessionStore.SetArchivePath(ctx, in.SessionID, archivePath); err != nil {
		return skillerr.IO("set archive path", skillerr.WithCause(err))
	}

	output.Message = fmt.Sprintf("Archived %d chunks in %d context windows, compressed %.1f%% (%d -> %d bytes)",
		len(result.Chunks),
		len(result.Windows),
		100.0*(1.0-float64(compressedSize)/float64(fileInfo.Size())),
		fileInfo.Size(),
		compressedSize,
	)

	return skillout.Emit(rc, command, output)
}

// embedContextWindows generates embeddings for each context window.
// Returns the count of successfully embedded windows and the model used.
func embedContextWindows(
	ctx context.Context,
	store storage.SessionStore,
	windows []storage.ContextWindow,
	chunks []storage.SessionChunk,
	cfg config.Config,
) (int, string) {
	// Create embedder for sessions scope
	embedder, err := semantic.NewEmbedderFromConfig(semantic.ScopeSessions, cfg)
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
		result, err := embedder.Embed(ctx, embeddingText)
		if err != nil {
			continue // Skip this window on error
		}

		// Serialize and save embedding only (summary unchanged)
		embeddingBytes := vector.SerializeF32(result.Vec)
		if err := store.SetContextWindowEmbedding(ctx, window.ID, embeddingBytes, result.Model); err != nil {
			continue
		}

		embeddedCount++
	}

	return embeddedCount, embedder.Model()
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
				parts = append(parts, "User: "+skillout.TruncateString(filtered, 500))
			}
		case "assistant_response":
			// Filter out noise from assistant responses
			filtered := filterEmbeddingContent(chunk.ContentPreview)
			if filtered != "" {
				parts = append(parts, "Assistant: "+skillout.TruncateString(filtered, 300))
			}
		case "error":
			// Errors are always valuable for understanding what went wrong
			if chunk.ContentPreview != "" {
				parts = append(parts, "Error: "+skillout.TruncateString(chunk.ContentPreview, 200))
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
