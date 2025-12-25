// Package main implements the embedding queue skill for background symbol embeddings.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/indexing/embedding"
)

// skillError is a typed error that carries an error code and hint.
type skillError struct {
	code    string
	message string
	hint    string
}

func (e *skillError) Error() string {
	return e.message
}

func newSkillError(code, message, hint string) error {
	return &skillError{code: code, message: message, hint: hint}
}

const (
	// DefaultRoot is the default storage root.
	DefaultRoot = "~/.agentctl/storage"
)

// Input is the skill input schema.
type Input struct {
	// Operation is the action to perform.
	Operation string `json:"operation"`

	// WorkspaceID identifies the workspace.
	WorkspaceID string `json:"workspace_id,omitempty"`

	// Symbols is the list of symbols to enqueue (for "enqueue" operation).
	Symbols []SymbolInput `json:"symbols,omitempty"`

	// Priority sets the job priority (for "enqueue" operation).
	Priority int `json:"priority,omitempty"`

	// Deduplicate skips symbols with unchanged content (for "enqueue" operation).
	Deduplicate bool `json:"deduplicate,omitempty"`

	// SymbolID is used for "get" operation.
	SymbolID string `json:"symbol_id,omitempty"`

	// FilePath is used for "get_by_file" operation.
	FilePath string `json:"file_path,omitempty"`

	// JobID is used for "job_status" operation.
	JobID string `json:"job_id,omitempty"`

	// OlderThan is the duration for cleanup (for "cleanup" operation).
	OlderThanHours int `json:"older_than_hours,omitempty"`
}

// SymbolInput describes a symbol to be embedded.
type SymbolInput struct {
	SymbolID   string `json:"symbol_id"`
	FilePath   string `json:"file_path"`
	SymbolName string `json:"symbol_name"`
	Content    string `json:"content"`
}

// Output is the skill output.
type Output struct {
	Operation string `json:"operation"`

	// For enqueue
	Queued  int      `json:"queued,omitempty"`
	Skipped int      `json:"skipped,omitempty"`
	JobIDs  []string `json:"job_ids,omitempty"`

	// For stats
	Stats *embedding.QueueStats `json:"stats,omitempty"`

	// For get
	Embedding *embedding.EmbeddingResult `json:"embedding,omitempty"`

	// For get_by_file
	Embeddings []*embedding.EmbeddingResult `json:"embeddings,omitempty"`

	// For job_status
	Job *embedding.EmbeddingJob `json:"job,omitempty"`

	// For cleanup
	Deleted int64 `json:"deleted,omitempty"`

	// Message provides human-readable info.
	Message string `json:"message,omitempty"`
}

func main() {
	if err := run(context.Background(), os.Stdin, os.Stdout); err != nil {
		code := "ERUNTIME"
		var data map[string]any
		var se *skillError
		if errors.As(err, &se) {
			code = se.code
			if se.hint != "" {
				data = map[string]any{"hint": se.hint}
			}
		}
		env := envelope.Error("embedding/queue", code, err.Error(), data)
		_ = json.NewEncoder(os.Stdout).Encode(env) //nolint:errcheck // best-effort error output
		os.Exit(1)
	}
}

func run(ctx context.Context, r io.Reader, w io.Writer) error {
	var input Input
	if err := json.NewDecoder(r).Decode(&input); err != nil {
		return newSkillError("EPARSE", fmt.Sprintf("parse input: %v", err), "Ensure valid JSON on stdin")
	}

	if input.Operation == "" {
		return newSkillError("EARG", "operation is required", "Specify operation: enqueue, stats, get, get_by_file, job_status, cleanup")
	}

	// Get storage root from environment or use default (use cache dir, same as incremental_index)
	root := os.Getenv("AGENTCTL_HOME")
	if root == "" {
		home, _ := os.UserHomeDir() //nolint:errcheck // fallback to empty string is fine
		root = home + "/.agentctl/cache"
	} else {
		root = root + "/cache"
	}

	// Open store
	store, err := embedding.OpenStore(ctx, root)
	if err != nil {
		return newSkillError("EIO", fmt.Sprintf("open store: %v", err), "Check that storage directory exists and is accessible")
	}
	defer store.Close()

	output := Output{Operation: input.Operation}

	switch input.Operation {
	case "enqueue":
		if err := handleEnqueue(ctx, store, &input, &output); err != nil {
			return err
		}

	case "stats":
		if err := handleStats(ctx, store, &output); err != nil {
			return err
		}

	case "get":
		if err := handleGet(ctx, store, &input, &output); err != nil {
			return err
		}

	case "get_by_file":
		if err := handleGetByFile(ctx, store, &input, &output); err != nil {
			return err
		}

	case "job_status":
		if err := handleJobStatus(ctx, store, &input, &output); err != nil {
			return err
		}

	case "cleanup":
		if err := handleCleanup(ctx, store, &input, &output); err != nil {
			return err
		}

	default:
		return newSkillError("EARG", fmt.Sprintf("unknown operation: %s", input.Operation), "Valid operations: enqueue, stats, get, get_by_file, job_status, cleanup")
	}

	env := envelope.OK("embedding/queue", output)
	return json.NewEncoder(w).Encode(env)
}

func handleEnqueue(ctx context.Context, store *embedding.Store, input *Input, output *Output) error {
	if input.WorkspaceID == "" {
		input.WorkspaceID = "default"
	}
	if len(input.Symbols) == 0 {
		return newSkillError("EARG", "symbols is required for enqueue", "Provide symbols array with symbol_id, file_path, symbol_name, and content")
	}

	// Convert input symbols
	symbols := make([]embedding.SymbolInput, len(input.Symbols))
	for i, s := range input.Symbols {
		symbols[i] = embedding.SymbolInput{
			SymbolID:   s.SymbolID,
			FilePath:   s.FilePath,
			SymbolName: s.SymbolName,
			Content:    s.Content,
		}
	}

	priority := embedding.PriorityNormal
	if input.Priority > 0 {
		priority = embedding.JobPriority(input.Priority)
	}

	result, err := store.Enqueue(ctx, embedding.EnqueueRequest{
		WorkspaceID: input.WorkspaceID,
		Symbols:     symbols,
		Priority:    priority,
		Deduplicate: input.Deduplicate,
	})
	if err != nil {
		return fmt.Errorf("enqueue: %w", err)
	}

	output.Queued = result.Queued
	output.Skipped = result.Skipped
	output.JobIDs = result.JobIDs
	output.Message = fmt.Sprintf("Enqueued %d symbols (%d skipped)", result.Queued, result.Skipped)
	return nil
}

func handleStats(ctx context.Context, store *embedding.Store, output *Output) error {
	stats, err := store.Stats(ctx)
	if err != nil {
		return fmt.Errorf("stats: %w", err)
	}

	output.Stats = stats
	output.Message = fmt.Sprintf("Queue: %d pending, %d running, %d completed, %d failed | Embeddings: %d stored",
		stats.QueuedCount, stats.RunningCount, stats.CompletedCount, stats.FailedCount, stats.EmbeddingsCount)
	return nil
}

func handleGet(ctx context.Context, store *embedding.Store, input *Input, output *Output) error {
	if input.WorkspaceID == "" {
		input.WorkspaceID = "default"
	}
	if input.SymbolID == "" {
		return newSkillError("EARG", "symbol_id is required for get", "Provide symbol_id field")
	}

	emb, err := store.GetEmbedding(ctx, input.WorkspaceID, input.SymbolID)
	if err != nil {
		if errors.Is(err, embedding.ErrNotFound) {
			output.Message = "Embedding not found"
			return nil
		}
		return fmt.Errorf("get embedding: %w", err)
	}

	output.Embedding = emb
	output.Message = fmt.Sprintf("Found embedding for %s (%d dimensions)", input.SymbolID, emb.Dimensions)
	return nil
}

func handleGetByFile(ctx context.Context, store *embedding.Store, input *Input, output *Output) error {
	if input.WorkspaceID == "" {
		input.WorkspaceID = "default"
	}
	if input.FilePath == "" {
		return newSkillError("EARG", "file_path is required for get_by_file", "Provide file_path field")
	}

	embeddings, err := store.GetEmbeddingsByFile(ctx, input.WorkspaceID, input.FilePath)
	if err != nil {
		return fmt.Errorf("get embeddings by file: %w", err)
	}

	output.Embeddings = embeddings
	output.Message = fmt.Sprintf("Found %d embeddings for %s", len(embeddings), input.FilePath)
	return nil
}

func handleJobStatus(ctx context.Context, store *embedding.Store, input *Input, output *Output) error {
	if input.JobID == "" {
		return newSkillError("EARG", "job_id is required for job_status", "Provide job_id field from enqueue response")
	}

	job, err := store.GetJob(ctx, input.JobID)
	if err != nil {
		return fmt.Errorf("get job: %w", err)
	}

	output.Job = job
	output.Message = fmt.Sprintf("Job %s: state=%s, attempts=%d", job.ID, job.State, job.Attempts)
	return nil
}

func handleCleanup(ctx context.Context, store *embedding.Store, input *Input, output *Output) error {
	hours := input.OlderThanHours
	if hours <= 0 {
		hours = 24 // Default: 24 hours
	}

	deleted, err := store.Cleanup(ctx, time.Duration(hours)*time.Hour)
	if err != nil {
		return fmt.Errorf("cleanup: %w", err)
	}

	output.Deleted = deleted
	output.Message = fmt.Sprintf("Deleted %d completed/failed jobs older than %d hours", deleted, hours)
	return nil
}
