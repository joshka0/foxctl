// Package main implements the embedding queue skill for background symbol and memory embeddings.
package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/oputil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/workspaceutil"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedding"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedqueue"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/storage/queue"
)

var allowedOps = []string{"enqueue", "stats", "get", "get_by_file", "job_status", "cleanup", "recover_stale", "purge"}

// Input is the skill input schema for embedding/queue operations.
type Input struct {
	// Operation is the action to perform.
	Operation string `json:"operation" validate:"required"`

	// WorkspaceID identifies the workspace.
	WorkspaceID string `json:"workspace_id,omitempty"`

	// Kind restricts stats and stale recovery to one task kind (symbol, memory, or semantic_file).
	Kind string `json:"kind,omitempty"`

	// Symbols is the list of symbols to enqueue (for "enqueue" operation).
	Symbols []SymbolInput `json:"symbols,omitempty"`

	// Memories is the list of named memories to enqueue (for "enqueue" operation).
	Memories []MemoryInput `json:"memories,omitempty"`

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

	// StaleAfterSeconds is the age threshold for recovering running jobs.
	StaleAfterSeconds int `json:"stale_after_seconds,omitempty"`
}

// SymbolInput describes a symbol to be embedded.
type SymbolInput struct {
	SymbolID   string `json:"symbol_id"`
	FilePath   string `json:"file_path"`
	SymbolName string `json:"symbol_name"`
	Language   string `json:"language,omitempty"`
	PackageID  string `json:"package_id,omitempty"`
	SymbolKey  string `json:"symbol_key,omitempty"`
	MemoryName string `json:"memory_name,omitempty"`
	Content    string `json:"content"`
}

// MemoryInput describes a named memory to be embedded.
type MemoryInput struct {
	Name    string `json:"name"`
	Type    string `json:"type,omitempty"`
	Content string `json:"content"`
}

// Output is the skill output for embedding/queue operations.
type Output struct {
	Operation string `json:"operation"`
	Kind      string `json:"kind,omitempty"`

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

	// For recover_stale
	Recovered int64 `json:"recovered,omitempty"`

	// Table names the backing queue table for lane-aware operations.
	Table string `json:"table,omitempty"`

	// Message provides human-readable info.
	Message string `json:"message,omitempty"`
}

// main is the skill entry point for embedding/queue.
func main() {
	skillmain.Main("embedding/queue", run)
}

// run orchestrates embedding queue operations with multiple action support.
//
// Index:
//
//	Purpose: Manage background embedding queue operations for symbols and named memories
//	Flow: validate operation → open store → route to handler → emit operation-specific results
//	SideEffects: database operations; job state management; embedding storage; queue cleanup
//	FailureModes: invalid operations, store errors, missing required fields, job not found
//	Observability: emits operation results with statistics, job IDs, and human-readable messages
//	Related: handleEnqueue, handleStats, handleGet, handleGetByFile, handleJobStatus, handleCleanup, handleRecoverStale
//	Keywords: embedding/queue, background, jobs, symbols, named memories, batch_processing
//
// [[domain:background-embedding-queue]]
// [[protocol:background-embedding-jobs]]
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	op := oputil.Op(in.Operation)
	opHint := fmt.Sprintf("Use one of: %s.", strings.Join(allowedOps, ", "))
	if op == "" {
		return skillerr.Arg("operation is required", skillerr.WithHint(opHint))
	}
	if err := oputil.Validate(op, allowedOps...); err != nil {
		return skillerr.Arg(err.Error(), skillerr.WithHint(opHint))
	}

	// Open store using cache path from config
	store, err := embedding.OpenStore(ctx, rc.Config.Paths.Cache)
	if err != nil {
		return skillerr.WrapIO("open store", err)
	}
	defer store.Close()

	output := Output{Operation: op}

	switch op {
	case "enqueue":
		if err := requireWorkspaceID(rc, &in, op); err != nil {
			return err
		}
		symbolModel := semantic.ResolveModelForScope(semantic.ScopeSymbols, rc.Config)
		memoryModel := semantic.ResolveModelForScope(semantic.ScopeMemory, rc.Config)
		if err := handleEnqueue(ctx, store, &in, &output, symbolModel, memoryModel); err != nil {
			return err
		}

	case "stats":
		if err := handleStats(ctx, rc.Config.Paths.Cache, store, &in, &output); err != nil {
			return err
		}

	case "get":
		if err := requireWorkspaceID(rc, &in, op); err != nil {
			return err
		}
		if err := handleGet(ctx, store, &in, &output); err != nil {
			return err
		}

	case "get_by_file":
		if err := requireWorkspaceID(rc, &in, op); err != nil {
			return err
		}
		if err := handleGetByFile(ctx, store, &in, &output); err != nil {
			return err
		}

	case "job_status":
		if err := handleJobStatus(ctx, store, &in, &output); err != nil {
			return err
		}

	case "cleanup":
		if err := handleCleanup(ctx, rc.Config.Paths.Cache, store, &in, &output); err != nil {
			return err
		}

	case "recover_stale":
		if err := handleRecoverStale(ctx, rc.Config.Paths.Cache, store, &in, &output); err != nil {
			return err
		}

	case "purge":
		if err := requireWorkspaceID(rc, &in, op); err != nil {
			return err
		}
		if err := handlePurge(ctx, rc.Config.Paths.Cache, store, &in, &output); err != nil {
			return err
		}
	}

	return skillout.Emit(rc, "embedding/queue", output)
}

func requireWorkspaceID(rc *skillmain.RunContext, input *Input, op string) error {
	input.WorkspaceID = workspaceutil.Resolve(input.WorkspaceID, "", rc.Workspace)
	if strings.TrimSpace(input.WorkspaceID) == "" {
		return skillerr.Arg("workspace_id is required for "+op, skillerr.WithHint("Pass workspace_id explicitly or run from a detectable workspace."))
	}
	return nil
}

// handleEnqueue processes symbol enqueue requests with deduplication and priority support.
func handleEnqueue(ctx context.Context, store *embedding.Store, input *Input, output *Output, symbolModel, memoryModel string) error {
	if len(input.Symbols) == 0 && len(input.Memories) == 0 {
		return skillerr.Arg("symbols or memories is required for enqueue")
	}

	priority := embedding.PriorityNormal
	if input.Priority > 0 {
		priority = embedding.JobPriority(input.Priority)
	}

	if len(input.Symbols) > 0 {
		symbols := make([]embedding.SymbolInput, len(input.Symbols))
		for i, s := range input.Symbols {
			symbols[i] = embedding.SymbolInput{
				SymbolID:   s.SymbolID,
				FilePath:   s.FilePath,
				SymbolName: s.SymbolName,
				Language:   s.Language,
				PackageID:  s.PackageID,
				SymbolKey:  s.SymbolKey,
				MemoryName: s.MemoryName,
				Content:    s.Content,
			}
		}
		result, err := store.Enqueue(ctx, embedding.EnqueueRequest{
			WorkspaceID: input.WorkspaceID,
			Symbols:     symbols,
			Priority:    priority,
			Model:       symbolModel,
			Deduplicate: input.Deduplicate,
		})
		if err != nil {
			return skillerr.WrapIO("enqueue symbols", err)
		}
		output.Queued += result.Queued
		output.Skipped += result.Skipped
		output.JobIDs = append(output.JobIDs, result.JobIDs...)
	}

	if len(input.Memories) > 0 {
		memories := make([]embedding.MemoryInput, len(input.Memories))
		for i, m := range input.Memories {
			memories[i] = embedding.MemoryInput{
				Name:    m.Name,
				Type:    m.Type,
				Content: m.Content,
			}
		}
		result, err := store.EnqueueMemories(ctx, embedding.MemoryEnqueueRequest{
			WorkspaceID: input.WorkspaceID,
			Memories:    memories,
			Priority:    priority,
			Model:       memoryModel,
		})
		if err != nil {
			return skillerr.WrapIO("enqueue memories", err)
		}
		output.Queued += result.Queued
		output.Skipped += result.Skipped
		output.JobIDs = append(output.JobIDs, result.JobIDs...)
	}

	output.Message = fmt.Sprintf("Enqueued %d embedding jobs (%d skipped)", output.Queued, output.Skipped)
	return nil
}

// handleStats retrieves and formats queue statistics for monitoring.
func handleStats(ctx context.Context, cacheRoot string, store *embedding.Store, input *Input, output *Output) error {
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	kind, err := normalizeTaskKind(input.Kind)
	if err != nil {
		return err
	}
	if kind == embedqueue.TaskKindSemanticFile {
		stats, err := withSemanticQueue(ctx, cacheRoot, func(semanticStore *semantic.QueueStore) (*queue.Stats, error) {
			return semanticStore.Stats(ctx, workspaceID)
		})
		if err != nil {
			return skillerr.WrapIO("semantic file stats", err)
		}
		output.Stats = queueStatsFromSemantic(stats)
		output.Kind = string(kind)
		output.Table = semantic.SemanticEmbeddingQueueTable
		scope := describeScope(workspaceID, kind)
		output.Message = fmt.Sprintf("Queue (%s, table %s): %d pending, %d running, %d completed, %d failed | Embeddings: %d stored",
			scope, semantic.SemanticEmbeddingQueueTable, output.Stats.QueuedCount, output.Stats.RunningCount, output.Stats.CompletedCount, output.Stats.FailedCount, output.Stats.EmbeddingsCount)
		return nil
	}
	var stats *embedding.QueueStats
	switch {
	case workspaceID != "" && kind != "":
		stats, err = store.StatsInWorkspaceKind(ctx, workspaceID, kind)
	case workspaceID != "":
		stats, err = store.StatsInWorkspace(ctx, workspaceID)
	case kind != "":
		stats, err = store.StatsKind(ctx, kind)
	default:
		stats, err = store.Stats(ctx)
	}
	if err != nil {
		return skillerr.WrapIO("stats", err)
	}

	output.Stats = stats
	if kind != "" {
		output.Kind = string(kind)
	}
	scope := describeScope(workspaceID, kind)
	output.Message = fmt.Sprintf("Queue (%s): %d pending, %d running, %d completed, %d failed | Embeddings: %d stored",
		scope, stats.QueuedCount, stats.RunningCount, stats.CompletedCount, stats.FailedCount, stats.EmbeddingsCount)
	return nil
}

// handleGet retrieves a specific embedding by symbol ID.
func handleGet(ctx context.Context, store *embedding.Store, input *Input, output *Output) error {
	if input.SymbolID == "" {
		return skillerr.Arg("symbol_id is required for get")
	}

	emb, err := store.GetEmbedding(ctx, input.WorkspaceID, input.SymbolID)
	if err != nil {
		if errors.Is(err, embedding.ErrNotFound) {
			output.Message = "Embedding not found"
			return nil
		}
		return skillerr.WrapIO("get embedding", err)
	}

	output.Embedding = emb
	output.Message = fmt.Sprintf("Found embedding for %s (%d dimensions)", input.SymbolID, emb.Dimensions)
	return nil
}

// handleGetByFile retrieves all embeddings for a specific file path.
func handleGetByFile(ctx context.Context, store *embedding.Store, input *Input, output *Output) error {
	if input.FilePath == "" {
		return skillerr.Arg("file_path is required for get_by_file")
	}

	embeddings, err := store.GetEmbeddingsByFile(ctx, input.WorkspaceID, input.FilePath)
	if err != nil {
		return skillerr.WrapIO("get embeddings by file", err)
	}

	output.Embeddings = embeddings
	output.Message = fmt.Sprintf("Found %d embeddings for %s", len(embeddings), input.FilePath)
	return nil
}

// handleJobStatus retrieves the current status of a specific embedding job.
func handleJobStatus(ctx context.Context, store *embedding.Store, input *Input, output *Output) error {
	if input.JobID == "" {
		return skillerr.Arg("job_id is required for job_status")
	}

	job, err := store.GetJob(ctx, input.JobID)
	if err != nil {
		return skillerr.WrapIO("get job", err)
	}

	output.Job = job
	output.Message = fmt.Sprintf("Job %s: state=%s, attempts=%d", job.ID, job.State, job.Attempts)
	return nil
}

// handleCleanup removes old completed/failed jobs from the queue.
func handleCleanup(ctx context.Context, cacheRoot string, store *embedding.Store, input *Input, output *Output) error {
	hours := input.OlderThanHours
	if hours <= 0 {
		hours = 24 // Default: 24 hours
	}

	workspaceID := strings.TrimSpace(input.WorkspaceID)
	kind, err := normalizeTaskKind(input.Kind)
	if err != nil {
		return err
	}
	olderThan := time.Duration(hours) * time.Hour
	var deleted int64
	if kind == embedqueue.TaskKindSemanticFile {
		deleted, err = withSemanticQueue(ctx, cacheRoot, func(semanticStore *semantic.QueueStore) (int64, error) {
			return semanticStore.Cleanup(ctx, workspaceID, olderThan)
		})
		if err == nil {
			output.Table = semantic.SemanticEmbeddingQueueTable
		}
	} else {
		switch {
		case workspaceID != "" && kind != "":
			deleted, err = store.CleanupInWorkspaceKind(ctx, workspaceID, kind, olderThan)
		case workspaceID != "":
			deleted, err = store.CleanupInWorkspace(ctx, workspaceID, olderThan)
		case kind != "":
			deleted, err = store.CleanupKind(ctx, kind, olderThan)
		default:
			deleted, err = store.Cleanup(ctx, olderThan)
		}
	}
	if err != nil {
		return skillerr.WrapIO("cleanup", err)
	}

	output.Deleted = deleted
	if kind != "" {
		output.Kind = string(kind)
	}
	output.Message = fmt.Sprintf("Deleted %d completed/failed jobs older than %d hours for %s", deleted, hours, describeScope(workspaceID, kind))
	return nil
}

func handleRecoverStale(ctx context.Context, cacheRoot string, store *embedding.Store, input *Input, output *Output) error {
	if input.StaleAfterSeconds <= 0 {
		return skillerr.Arg("stale_after_seconds must be > 0 for recover_stale")
	}

	olderThan := time.Duration(input.StaleAfterSeconds) * time.Second
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	kind, err := normalizeTaskKind(input.Kind)
	if err != nil {
		return err
	}
	var recovered int64
	if kind == embedqueue.TaskKindSemanticFile {
		recovered, err = withSemanticQueue(ctx, cacheRoot, func(semanticStore *semantic.QueueStore) (int64, error) {
			return semanticStore.RequeueStaleRunningInWorkspace(ctx, workspaceID, olderThan)
		})
		if err == nil {
			output.Table = semantic.SemanticEmbeddingQueueTable
		}
	} else {
		switch {
		case workspaceID != "" && kind != "":
			recovered, err = store.RequeueStaleRunningInWorkspaceKind(ctx, workspaceID, kind, olderThan)
		case workspaceID != "":
			recovered, err = store.RequeueStaleRunningInWorkspace(ctx, workspaceID, olderThan)
		case kind != "":
			recovered, err = store.RequeueStaleRunningKind(ctx, kind, olderThan)
		default:
			recovered, err = store.RequeueStaleRunning(ctx, olderThan)
		}
	}
	if err != nil {
		return skillerr.WrapIO("recover stale jobs", err)
	}

	output.Recovered = recovered
	if kind != "" {
		output.Kind = string(kind)
	}
	scope := describeScope(workspaceID, kind)
	output.Message = fmt.Sprintf("Recovered %d stale running jobs for %s", recovered, scope)
	return nil
}

func handlePurge(ctx context.Context, cacheRoot string, store *embedding.Store, input *Input, output *Output) error {
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	kind, err := normalizeTaskKind(input.Kind)
	if err != nil {
		return err
	}
	if kind == "" {
		return skillerr.Arg("kind is required for purge", skillerr.WithHint("Use kind=symbol, kind=memory, or kind=semantic_file."))
	}
	var deleted int64
	if kind == embedqueue.TaskKindSemanticFile {
		deleted, err = withSemanticQueue(ctx, cacheRoot, func(semanticStore *semantic.QueueStore) (int64, error) {
			return semanticStore.Purge(ctx, workspaceID)
		})
		if err == nil {
			output.Table = semantic.SemanticEmbeddingQueueTable
		}
	} else {
		deleted, err = store.Purge(ctx, workspaceID, kind)
	}
	if err != nil {
		return skillerr.WrapIO("purge jobs", err)
	}

	output.Deleted = deleted
	output.Kind = string(kind)
	output.Message = fmt.Sprintf("Purged %d jobs for %s", deleted, describeScope(workspaceID, kind))
	return nil
}

func normalizeTaskKind(raw string) (embedqueue.TaskKind, error) {
	switch kind := embedqueue.TaskKind(strings.TrimSpace(raw)); kind {
	case "":
		return "", nil
	case embedqueue.TaskKindSymbol, embedqueue.TaskKindMemory, embedqueue.TaskKindSemanticFile:
		return kind, nil
	default:
		return "", skillerr.Arg("kind must be one of: symbol, memory, semantic_file")
	}
}

func describeScope(workspaceID string, kind embedqueue.TaskKind) string {
	workspaceID = strings.TrimSpace(workspaceID)
	scope := "all workspaces"
	if workspaceID != "" {
		scope = "workspace " + workspaceID
	}
	if kind != "" {
		scope += ", kind " + string(kind)
	}
	return scope
}

func withSemanticQueue[T any](ctx context.Context, cacheRoot string, fn func(*semantic.QueueStore) (T, error)) (T, error) {
	var zero T
	store, err := semantic.OpenQueueStore(ctx, cacheRoot)
	if err != nil {
		return zero, err
	}
	defer func() {
		_ = store.Close() //nolint:errcheck
	}()
	return fn(store)
}

func queueStatsFromSemantic(stats *queue.Stats) *embedding.QueueStats {
	if stats == nil {
		return &embedding.QueueStats{}
	}
	return &embedding.QueueStats{
		QueuedCount:    stats.QueuedCount,
		RunningCount:   stats.RunningCount,
		CompletedCount: stats.CompletedCount,
		FailedCount:    stats.FailedCount,
		OldestQueuedAt: stats.OldestQueuedAt,
	}
}
