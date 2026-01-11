// Package main implements the embedding queue skill for background symbol embeddings.
package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/indexing/embedding"
)

// Input is the skill input schema.
type Input struct {
	// Operation is the action to perform.
	Operation string `json:"operation" validate:"required,oneof=enqueue stats get get_by_file job_status cleanup"`

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
	skillmain.Main("embedding/queue", run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Open store using cache path from config
	store, err := embedding.OpenStore(ctx, rc.Config.Paths.Cache)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer store.Close()

	output := Output{Operation: in.Operation}

	switch in.Operation {
	case "enqueue":
		if err := handleEnqueue(ctx, store, &in, &output); err != nil {
			return err
		}

	case "stats":
		if err := handleStats(ctx, store, &output); err != nil {
			return err
		}

	case "get":
		if err := handleGet(ctx, store, &in, &output); err != nil {
			return err
		}

	case "get_by_file":
		if err := handleGetByFile(ctx, store, &in, &output); err != nil {
			return err
		}

	case "job_status":
		if err := handleJobStatus(ctx, store, &in, &output); err != nil {
			return err
		}

	case "cleanup":
		if err := handleCleanup(ctx, store, &in, &output); err != nil {
			return err
		}
	}

	return skillout.Emit(rc, "embedding/queue", output)
}

func handleEnqueue(ctx context.Context, store *embedding.Store, input *Input, output *Output) error {
	if input.WorkspaceID == "" {
		input.WorkspaceID = "default"
	}
	if len(input.Symbols) == 0 {
		return fmt.Errorf("symbols is required for enqueue")
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
		return fmt.Errorf("symbol_id is required for get")
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
		return fmt.Errorf("file_path is required for get_by_file")
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
		return fmt.Errorf("job_id is required for job_status")
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
