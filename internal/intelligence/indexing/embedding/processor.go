// Package embedding: the canonical named-memory embedding job processor.
//
// MemoryJobProcessor encapsulates one pipeline shared by the embedding/worker
// skill and any daemon or runtime worker that drains memory queue jobs:
//  1. Validate the job identity (workspace + memory name).
//  2. In dry-run, mark the queue job complete without embedding.
//  3. Embed the job content via the configured MemoryEmbedder.
//  4. Validate dimensions against the expected count and the memory store.
//  5. Persist workspace embedding metadata.
//  6. Update the named-memory row.
//  7. Mark the queue job complete.
//
// On any error the caller is expected to call store.Fail(jobID, err) so the
// queue's existing retry semantics are preserved.
package embedding

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedqueue"
	"github.com/joshka0/foxctl/internal/storage"
)

// MemoryEmbedder is the small contract a memory embedding job processor
// requires. Callers (the skill and any daemon worker) supply the embedder
// implementation — typically a wrapper around *semantic.Embedder.
type MemoryEmbedder interface {
	// Embed generates a single embedding vector for the given text.
	Embed(ctx context.Context, text string) (MemoryEmbedding, error)
	// Provider returns the embedder provider name (e.g. "openai_compat").
	Provider() string
	// Model returns the model identifier used by this embedder.
	Model() string
	// Dimensions returns the embedding vector dimension.
	Dimensions() int
}

// MemoryEmbedding is the canonical output of a single embed call.
type MemoryEmbedding struct {
	// Vec is the embedding vector.
	Vec []float32
	// Model is the model identifier used to generate the vector.
	Model string
}

// MemoryJobProcessor is the canonical named-memory embedding job processor.
// embedding/worker and runtime hook flows use this processor through the skill
// adapter; it owns no IO beyond the deps injected at construction time.
type MemoryJobProcessor struct {
	// Store is the embedding queue store used to mark jobs complete.
	Store *Store
	// Memory is the named-memory backend updated with new vectors.
	Memory storage.MemoryStore
	// Embedder produces the embedding vectors.
	Embedder MemoryEmbedder
	// ExpectedDimensions is the configured expected dimension. When > 0,
	// Process rejects vectors whose length does not match.
	ExpectedDimensions int
	// DryRun skips the embed call and just marks the job complete.
	DryRun bool
	// Now returns the current time; injected for deterministic tests.
	// Defaults to time.Now when nil.
	Now func() time.Time
}

// ErrUnsupportedMemoryJobKind is returned when a non-memory job is handed
// to Process.
var ErrUnsupportedMemoryJobKind = errors.New("memory job processor received unsupported job kind")

// Process runs the full memory job pipeline for a single job.
//
// The caller is responsible for claiming the job from the queue and for
// invoking Store.Fail on a non-nil error to preserve queue retry semantics.
// The processor owns the embed → metadata → update → complete sequence.
func (p *MemoryJobProcessor) Process(ctx context.Context, job *EmbeddingJob) error {
	if p == nil {
		return errors.New("nil processor")
	}
	if job == nil {
		return errors.New("nil job")
	}
	if job.Kind != embedqueue.TaskKindMemory {
		return fmt.Errorf("%w: %q", ErrUnsupportedMemoryJobKind, job.Kind)
	}
	workspaceID := strings.TrimSpace(job.WorkspaceID)
	memoryName := strings.TrimSpace(job.MemoryName)
	if workspaceID == "" {
		return errors.New("workspace_id is required")
	}
	if memoryName == "" {
		return errors.New("memory_name is required")
	}
	if p.Store == nil {
		return errors.New("embedding store is required")
	}

	if p.DryRun {
		return p.Store.CompleteJob(ctx, job.ID)
	}
	if p.Memory == nil {
		return errors.New("memory store unavailable")
	}
	if p.Embedder == nil {
		return errors.New("memory embedding provider not available")
	}

	result, err := p.Embedder.Embed(ctx, job.Content)
	if err != nil {
		return fmt.Errorf("embed memory: %w", err)
	}
	if expected := p.ExpectedDimensions; expected > 0 && len(result.Vec) != expected {
		return fmt.Errorf("memory dimension mismatch: got %d, expected %d", len(result.Vec), expected)
	}
	if err := p.Memory.ValidateEmbeddingDimensions(ctx, workspaceID, len(result.Vec)); err != nil {
		return err
	}

	now := p.now().UTC()
	createdAt := job.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	if err := p.Memory.SetEmbeddingMetadata(ctx, storage.EmbeddingMetadata{
		Workspace:  workspaceID,
		Provider:   p.Embedder.Provider(),
		Model:      result.Model,
		Dimensions: len(result.Vec),
		CreatedAt:  createdAt,
		UpdatedAt:  now,
	}); err != nil {
		return fmt.Errorf("set memory embedding metadata: %w", err)
	}
	if err := p.Memory.UpdateEmbedding(ctx, memoryName, workspaceID, result.Vec); err != nil {
		return fmt.Errorf("update memory embedding: %w", err)
	}
	if err := p.Store.CompleteJob(ctx, job.ID); err != nil {
		return fmt.Errorf("complete memory job: %w", err)
	}
	return nil
}

func (p *MemoryJobProcessor) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}
