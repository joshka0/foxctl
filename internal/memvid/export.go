package memvid

import (
	"context"
	"fmt"
	"time"
)

// SessionTurn represents a single turn from an agentctl session.
// This is a simplified representation - actual implementation would
// use the session store types.
type SessionTurn struct {
	ID        string
	SessionID string
	Role      string    // "user" or "assistant"
	Content   string    // The message content
	Timestamp time.Time // When the turn occurred
	Tokens    int       // Token count estimate
}

// SessionData contains all data for a session export.
type SessionData struct {
	// Session metadata
	SessionID   string
	WorkspaceID string
	AgentID     string
	CreatedAt   time.Time
	EndedAt     *time.Time

	// Session content
	Turns    []SessionTurn
	Chunks   []SessionChunk   // Optional: chunked content
	Summary  *SessionSummary  // Optional: L1/L2 summary
	Anchor   string           // Session anchor/goal if set
}

// SessionChunk represents a chunk of session content.
type SessionChunk struct {
	ID        string
	SessionID string
	Content   string
	StartTurn int
	EndTurn   int
	Embedding []float32 // Optional: pre-computed embedding
}

// SessionSummary represents a distilled session summary (L1/L2).
type SessionSummary struct {
	Level   int    // 1 or 2
	Content string
	Tokens  int
}

// Exporter handles session export to MV2 format.
type Exporter struct {
	cli *CLI
}

// NewExporter creates a new session exporter.
func NewExporter() *Exporter {
	return &Exporter{
		cli: NewCLI(),
	}
}

// NewExporterWithCLI creates an exporter with a custom CLI wrapper.
func NewExporterWithCLI(cli *CLI) *Exporter {
	return &Exporter{cli: cli}
}

// Export exports a session to an MV2 file.
func (e *Exporter) Export(ctx context.Context, data SessionData, opts SessionExportOptions) error {
	// Check CLI availability
	if err := e.cli.Available(ctx); err != nil {
		return err
	}

	// Create the MV2 file
	if err := e.cli.Create(ctx, opts.OutputPath); err != nil {
		return fmt.Errorf("failed to create MV2 file: %w", err)
	}

	var frames []Frame

	// Export session metadata as a frame
	metaFrame := Frame{
		URI:       fmt.Sprintf("mv2://session/%s/meta", data.SessionID),
		Title:     fmt.Sprintf("Session %s", data.SessionID),
		Content:   e.formatSessionMeta(data),
		CreatedAt: data.CreatedAt,
		Tags: map[string]string{
			"type":        "session_meta",
			"session_id":  data.SessionID,
			"workspace":   data.WorkspaceID,
			"agent_id":    data.AgentID,
		},
	}
	if data.Anchor != "" {
		metaFrame.Tags["anchor"] = data.Anchor
	}
	frames = append(frames, metaFrame)

	// Export individual turns if requested
	if opts.IncludeTurns {
		for i, turn := range data.Turns {
			frame := Frame{
				URI:       fmt.Sprintf("mv2://session/%s/turn/%d", data.SessionID, i),
				Title:     fmt.Sprintf("%s turn %d", turn.Role, i),
				Content:   turn.Content,
				CreatedAt: turn.Timestamp,
				Tags: map[string]string{
					"type":       "turn",
					"session_id": data.SessionID,
					"role":       turn.Role,
					"turn_index": fmt.Sprintf("%d", i),
				},
			}
			frames = append(frames, frame)
		}
	}

	// Export chunks if requested
	if opts.IncludeChunks {
		for i, chunk := range data.Chunks {
			frame := Frame{
				URI:       fmt.Sprintf("mv2://session/%s/chunk/%d", data.SessionID, i),
				Title:     fmt.Sprintf("Chunk %d (turns %d-%d)", i, chunk.StartTurn, chunk.EndTurn),
				Content:   chunk.Content,
				CreatedAt: data.CreatedAt, // Use session creation time
				Tags: map[string]string{
					"type":        "chunk",
					"session_id":  data.SessionID,
					"chunk_index": fmt.Sprintf("%d", i),
					"start_turn":  fmt.Sprintf("%d", chunk.StartTurn),
					"end_turn":    fmt.Sprintf("%d", chunk.EndTurn),
				},
			}
			frames = append(frames, frame)
		}
	}

	// Export summary if requested and available
	if opts.IncludeSummaries && data.Summary != nil {
		frame := Frame{
			URI:       fmt.Sprintf("mv2://session/%s/summary/L%d", data.SessionID, data.Summary.Level),
			Title:     fmt.Sprintf("L%d Summary", data.Summary.Level),
			Content:   data.Summary.Content,
			CreatedAt: data.CreatedAt,
			Tags: map[string]string{
				"type":       "summary",
				"session_id": data.SessionID,
				"level":      fmt.Sprintf("%d", data.Summary.Level),
			},
		}
		frames = append(frames, frame)
	}

	// Batch insert all frames
	if err := e.cli.PutBatch(ctx, opts.OutputPath, frames); err != nil {
		return fmt.Errorf("failed to write frames: %w", err)
	}

	return nil
}

// formatSessionMeta creates a human-readable session metadata string.
func (e *Exporter) formatSessionMeta(data SessionData) string {
	var sb fmt.Stringer = &sessionMetaBuilder{data: data}
	return sb.String()
}

type sessionMetaBuilder struct {
	data SessionData
}

func (b *sessionMetaBuilder) String() string {
	meta := fmt.Sprintf(`# Session: %s

**Workspace:** %s
**Agent:** %s
**Created:** %s
`, b.data.SessionID, b.data.WorkspaceID, b.data.AgentID, b.data.CreatedAt.Format(time.RFC3339))

	if b.data.EndedAt != nil {
		meta += fmt.Sprintf("**Ended:** %s\n", b.data.EndedAt.Format(time.RFC3339))
	}

	if b.data.Anchor != "" {
		meta += fmt.Sprintf("\n## Anchor\n%s\n", b.data.Anchor)
	}

	meta += fmt.Sprintf("\n**Total Turns:** %d\n", len(b.data.Turns))

	return meta
}

// ExportResult contains the result of an export operation.
type ExportResult struct {
	// OutputPath is the path to the created MV2 file
	OutputPath string

	// FrameCount is the number of frames written
	FrameCount int

	// FileSize is the size of the created file in bytes
	FileSize int64

	// Duration is how long the export took
	Duration time.Duration
}
