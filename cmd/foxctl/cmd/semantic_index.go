package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/fsutil"
	workspaceutil "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/storage/memory"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

func newSemanticIndexCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "semantic-index",
		Short: "Manage semantic file index for workspace files",
		Long: "Manage the semantic file index, which generates embeddings for workspace files.\n\n" +
			"The semantic index enables similarity search across code and documentation.\n\n" +
			"Common workflows:\n" +
			"  foxctl semantic-index init --workspace <path> [--glob '**/*.go']\n" +
			"  foxctl semantic-index update --workspace <path> --files <file1,file2>\n\n" +
			"See docs/spec/semantic_file_index.md for detailed specification.",
	}
	cmd.AddCommand(
		newSemanticIndexInitCommand(),
		newSemanticIndexUpdateCommand(),
		newSemanticIndexStatsCommand(),
		newSemanticIndexDrainCommand(),
	)
	return cmd
}

func newSemanticIndexInitCommand() *cobra.Command {
	var workspace string
	var glob string
	var exclude []string
	var dryRun bool
	var taskID string
	var chunkBytes int
	var chunkOverlap int
	var chunkDelay time.Duration
	var model string
	var provider string
	var batchSize int
	var batchDelay time.Duration
	var maxFileBytes int64
	var enqueue bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize semantic index for workspace files",
		Long:  "Initialize the semantic file index by generating embeddings for all matching files in the workspace.",
		Example: "  # Index all Go files with Voyage (default, 200M free tokens)\n" +
			"  foxctl semantic-index init --workspace . --glob '**/*.go'\n\n" +
			"  # Index Go source files, excluding tests\n" +
			"  foxctl semantic-index init --workspace . --glob '**/*.go' --exclude '*_test.go'\n\n" +
			"  # Use Gemini instead of Voyage\n" +
			"  foxctl semantic-index init --workspace . --provider gemini\n\n" +
			"  # Use local LM Studio/OpenAI-compatible embeddings\n" +
			"  FOXCTL_EMBEDDING_BASE_URL=http://127.0.0.1:1234/v1 foxctl semantic-index init --workspace . --provider lmstudio --model text-embedding-nomic-embed-text-v1.5\n\n" +
			"  # Slice and throttle local GPU-backed embedding\n" +
			"  foxctl semantic-index init --workspace . --provider lmstudio --batch-size 1 --batch-delay 30s --chunk-bytes 32768 --chunk-overlap 1024 --chunk-delay 10s --max-file-bytes 1048576\n\n" +
			"  # Queue matching files for paced background drain\n" +
			"  foxctl semantic-index init --workspace . --provider lmstudio --model text-embedding-qwen3-embedding-8b --chunk-bytes 32768 --chunk-overlap 1024 --chunk-delay 10s --enqueue\n\n" +
			"  # Dry run to see what would be indexed\n" +
			"  foxctl semantic-index init --workspace . --glob '**/*.go' --dry-run",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSemanticIndexInit(cmd, workspace, glob, exclude, dryRun, enqueue, taskID, chunkBytes, chunkOverlap, chunkDelay, model, provider, semanticIndexBatchOptions{
				BatchSize:    batchSize,
				BatchDelay:   batchDelay,
				MaxFileBytes: maxFileBytes,
			})
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root directory")
	cmd.Flags().StringVar(&glob, "glob", "**/*.go", "Glob pattern to match files")
	cmd.Flags().StringSliceVar(&exclude, "exclude", nil, "Glob patterns to exclude (comma-separated, e.g., '*_test.go,vendor/**')")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be indexed without making changes")
	cmd.Flags().StringVar(&taskID, "task-id", "", "Task ID for provenance tracking")
	cmd.Flags().IntVar(&chunkBytes, "chunk-bytes", 0, "Chunk size in bytes (0 = no chunking)")
	cmd.Flags().IntVar(&chunkOverlap, "chunk-overlap", 0, "Chunk overlap in bytes")
	cmd.Flags().DurationVar(&chunkDelay, "chunk-delay", 0, "Delay between chunk embedding requests, useful for large files with local GPU-backed embedding")
	cmd.Flags().StringVar(&model, "model", "", "Embedding model name (default: voyage-code-3 or gemini-embedding-001)")
	cmd.Flags().StringVar(&provider, "provider", "", "Embedding provider: voyage (default), gemini, lmstudio/openai_compat, or noop")
	cmd.Flags().IntVar(&batchSize, "batch-size", 0, "Maximum files per indexing batch (0 = all files in one batch)")
	cmd.Flags().DurationVar(&batchDelay, "batch-delay", 0, "Delay between indexing batches, useful for local GPU-backed embedding")
	cmd.Flags().Int64Var(&maxFileBytes, "max-file-bytes", 0, "Hard-skip files larger than this before embedding (0 = no extra limit). Use --chunk-bytes to slice large files")
	cmd.Flags().BoolVar(&enqueue, "enqueue", false, "Queue files for paced semantic-index drain instead of embedding synchronously")

	return cmd
}

func newSemanticIndexUpdateCommand() *cobra.Command {
	var workspace string
	var files []string
	var deleted []string
	var dryRun bool
	var taskID string
	var reviewID string
	var chunkBytes int
	var chunkOverlap int
	var chunkDelay time.Duration
	var model string
	var provider string
	var batchSize int
	var batchDelay time.Duration
	var maxFileBytes int64
	var enqueue bool

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update semantic index for changed files",
		Long:  "Update the semantic file index for specific files that have changed.",
		Example: "  # Update specific files\n" +
			"  foxctl semantic-index update --workspace . --files main.go,utils.go\n\n" +
			"  # Mark files as deleted\n" +
			"  foxctl semantic-index update --workspace . --deleted old_file.go\n\n" +
			"  # Slice and throttle local GPU-backed embedding\n" +
			"  foxctl semantic-index update --workspace . --files main.go,utils.go --provider lmstudio --batch-size 1 --batch-delay 30s --chunk-bytes 32768 --chunk-overlap 1024 --chunk-delay 10s --max-file-bytes 1048576\n\n" +
			"  # Queue changed files for paced background drain\n" +
			"  foxctl semantic-index update --workspace . --files main.go,utils.go --provider lmstudio --model text-embedding-qwen3-embedding-8b --chunk-bytes 32768 --chunk-overlap 1024 --chunk-delay 10s --enqueue\n\n" +
			"  # Update with provenance\n" +
			"  foxctl semantic-index update --workspace . --files main.go --task-id task-123 --review-id review-456",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSemanticIndexUpdate(cmd, workspace, files, deleted, dryRun, enqueue, taskID, reviewID, chunkBytes, chunkOverlap, chunkDelay, model, provider, semanticIndexBatchOptions{
				BatchSize:    batchSize,
				BatchDelay:   batchDelay,
				MaxFileBytes: maxFileBytes,
			})
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root directory")
	cmd.Flags().StringSliceVar(&files, "files", nil, "Files to index or update (comma-separated)")
	cmd.Flags().StringSliceVar(&deleted, "deleted", nil, "Files to remove from index (comma-separated)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be updated without making changes")
	cmd.Flags().StringVar(&taskID, "task-id", "", "Task ID for provenance tracking")
	cmd.Flags().StringVar(&reviewID, "review-id", "", "Review ID for provenance tracking")
	cmd.Flags().IntVar(&chunkBytes, "chunk-bytes", 0, "Chunk size in bytes (0 = no chunking)")
	cmd.Flags().IntVar(&chunkOverlap, "chunk-overlap", 0, "Chunk overlap in bytes")
	cmd.Flags().DurationVar(&chunkDelay, "chunk-delay", 0, "Delay between chunk embedding requests, useful for large files with local GPU-backed embedding")
	cmd.Flags().StringVar(&model, "model", "", "Embedding model name (default: voyage-code-3 or gemini-embedding-001)")
	cmd.Flags().StringVar(&provider, "provider", "", "Embedding provider: voyage (default), gemini, lmstudio/openai_compat, or noop")
	cmd.Flags().IntVar(&batchSize, "batch-size", 0, "Maximum files per indexing batch (0 = all files in one batch)")
	cmd.Flags().DurationVar(&batchDelay, "batch-delay", 0, "Delay between indexing batches, useful for local GPU-backed embedding")
	cmd.Flags().Int64Var(&maxFileBytes, "max-file-bytes", 0, "Hard-skip files larger than this before embedding (0 = no extra limit). Use --chunk-bytes to slice large files")
	cmd.Flags().BoolVar(&enqueue, "enqueue", false, "Queue files for paced semantic-index drain instead of embedding synchronously")

	return cmd
}

func newSemanticIndexStatsCommand() *cobra.Command {
	var workspace string

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show semantic index embedding queue stats",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSemanticIndexStats(cmd, workspace)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root directory")
	return cmd
}

func newSemanticIndexDrainCommand() *cobra.Command {
	var workspace string
	var model string
	var provider string
	var batchSize int
	var maxDuration time.Duration
	var processAll bool
	var jobDelay time.Duration
	var recoverStaleAfter time.Duration
	var chunkBytes int
	var chunkOverlap int
	var chunkDelay time.Duration

	cmd := &cobra.Command{
		Use:   "drain",
		Short: "Drain queued semantic file embedding jobs",
		Long:  "Drain queued semantic file embedding jobs at a controlled pace.",
		Example: "  foxctl semantic-index drain --workspace . --provider lmstudio --model text-embedding-qwen3-embedding-8b --batch-size 1 --job-delay 30s\n\n" +
			"  foxctl semantic-index drain --workspace . --provider noop --process-all",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSemanticIndexDrain(cmd, workspace, model, provider, batchSize, maxDuration, processAll, jobDelay, recoverStaleAfter, chunkBytes, chunkOverlap, chunkDelay)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root directory")
	cmd.Flags().StringVar(&model, "model", "", "Embedding model override for drained jobs")
	cmd.Flags().StringVar(&provider, "provider", "", "Embedding provider override: voyage, gemini, lmstudio/openai_compat, or noop")
	cmd.Flags().IntVar(&batchSize, "batch-size", 1, "Maximum queued files to process in this drain run")
	cmd.Flags().DurationVar(&maxDuration, "max-duration", 5*time.Minute, "Maximum drain duration")
	cmd.Flags().BoolVar(&processAll, "process-all", false, "Process until queue is empty or max-duration is reached")
	cmd.Flags().DurationVar(&jobDelay, "job-delay", 0, "Delay between queued file jobs")
	cmd.Flags().DurationVar(&recoverStaleAfter, "recover-stale-after", 0, "Requeue running jobs older than this before draining (0 = disabled)")
	cmd.Flags().IntVar(&chunkBytes, "chunk-bytes", 0, "Override queued chunk size in bytes (0 = use queued value)")
	cmd.Flags().IntVar(&chunkOverlap, "chunk-overlap", 0, "Override queued chunk overlap in bytes (0 = use queued value)")
	cmd.Flags().DurationVar(&chunkDelay, "chunk-delay", 0, "Override queued delay between chunk embedding requests (0 = use queued value)")
	return cmd
}

type semanticIndexBatchOptions struct {
	BatchSize    int
	BatchDelay   time.Duration
	MaxFileBytes int64
}

func runSemanticIndexInit(cmd *cobra.Command, workspace, glob string, exclude []string, dryRun, enqueue bool, taskID string, chunkBytes, chunkOverlap int, chunkDelay time.Duration, model, providerName string, batchOpts semanticIndexBatchOptions) error {
	start := time.Now()
	ctx := cmd.Context()
	if err := validateSemanticIndexOptions(chunkBytes, chunkOverlap, chunkDelay, batchOpts); err != nil {
		return writeSemanticError(cmd, "EARG", err.Error())
	}

	// Resolve workspace path
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return writeSemanticError(cmd, semantic.ErrCodeCASResolveError, fmt.Sprintf("resolve workspace: %v", err))
	}
	info, err := os.Stat(absWorkspace)
	if err != nil {
		code := "ENOTFOUND"
		if os.IsPermission(err) {
			code = "EIO"
		}
		return writeSemanticError(cmd, code, fmt.Sprintf("workspace %q: %v", absWorkspace, err))
	}
	if !info.IsDir() {
		return writeSemanticError(cmd, "EARG", fmt.Sprintf("workspace %q is not a directory", absWorkspace))
	}

	// Find files matching glob, excluding specified patterns
	files, err := fsutil.FindFilesMatchingGlob(absWorkspace, glob, exclude)
	if err != nil {
		return writeSemanticError(cmd, semantic.ErrCodeCASResolveError, fmt.Sprintf("find files: %v", err))
	}

	if len(files) == 0 {
		return writeSemanticResult(cmd, semantic.JobTypeInitFiles, &semantic.JobResult{
			Summary: semantic.JobSummary{FilesSkipped: 0},
		}, absWorkspace, start)
	}

	filteredFiles, skipped, err := filterSemanticIndexFilesBySize(absWorkspace, jobFilesFromPaths(files, semantic.ChangeKindAdded), batchOpts.MaxFileBytes, cmd.ErrOrStderr())
	if err != nil {
		return writeSemanticError(cmd, semantic.ErrCodeCASResolveError, err.Error())
	}
	files = pathsFromJobFiles(filteredFiles)

	// Dry run: just list files
	if dryRun {
		return writeSemanticDryRun(cmd, semantic.JobTypeInitFiles, files, absWorkspace, skipped)
	}

	// Build job args
	workspaceID := workspaceutil.ID(absWorkspace)
	args := semantic.JobArgs{
		WorkspaceID: workspaceID,
		Reason:      semantic.ReasonInitialIndex,
		TaskID:      taskID,
	}
	args.Files = filteredFiles
	if len(args.Files) == 0 {
		return writeSemanticResult(cmd, semantic.JobTypeInitFiles, &semantic.JobResult{
			Summary: semantic.JobSummary{FilesSkipped: skipped},
		}, absWorkspace, start)
	}
	if enqueue {
		result, err := enqueueSemanticIndexFiles(ctx, absWorkspace, semantic.JobTypeInitFiles, args, chunkBytes, chunkOverlap, chunkDelay, model, providerName)
		if err != nil {
			return writeSemanticError(cmd, semantic.ErrCodeCASResolveError, err.Error())
		}
		return writeSemanticEnqueueResult(cmd, semantic.JobTypeInitFiles, result, absWorkspace, skipped, start)
	}

	// Create indexer and run
	indexer, cleanup, err := createSemanticIndexer(ctx, absWorkspace, chunkBytes, chunkOverlap, chunkDelay, model, providerName)
	if err != nil {
		return writeSemanticError(cmd, semantic.ErrCodeProviderConfigInvalid, err.Error())
	}
	defer cleanup()

	result, err := runSemanticIndexBatches(ctx, indexer, args, true, batchOpts, cmd.ErrOrStderr())
	if err != nil {
		return writeSemanticError(cmd, semantic.ErrCodeSemanticIndexNotFound, err.Error())
	}
	result.Summary.FilesSkipped += skipped

	return writeSemanticResult(cmd, semantic.JobTypeInitFiles, result, absWorkspace, start)
}

func runSemanticIndexUpdate(cmd *cobra.Command, workspace string, files, deleted []string, dryRun, enqueue bool, taskID, reviewID string, chunkBytes, chunkOverlap int, chunkDelay time.Duration, model, providerName string, batchOpts semanticIndexBatchOptions) error {
	start := time.Now()
	ctx := cmd.Context()
	if err := validateSemanticIndexOptions(chunkBytes, chunkOverlap, chunkDelay, batchOpts); err != nil {
		return writeSemanticError(cmd, "EARG", err.Error())
	}

	if len(files) == 0 && len(deleted) == 0 {
		return writeSemanticError(cmd, "EARG", "at least one of --files or --deleted is required")
	}

	// Resolve workspace path
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return writeSemanticError(cmd, semantic.ErrCodeCASResolveError, fmt.Sprintf("resolve workspace: %v", err))
	}
	info, err := os.Stat(absWorkspace)
	if err != nil {
		code := "ENOTFOUND"
		if os.IsPermission(err) {
			code = "EIO"
		}
		return writeSemanticError(cmd, code, fmt.Sprintf("workspace %q: %v", absWorkspace, err))
	}
	if !info.IsDir() {
		return writeSemanticError(cmd, "EARG", fmt.Sprintf("workspace %q is not a directory", absWorkspace))
	}

	allFiles := append(jobFilesFromPaths(files, semantic.ChangeKindModified), jobFilesFromPaths(deleted, semantic.ChangeKindDeleted)...)
	filteredFiles, skipped, err := filterSemanticIndexFilesBySize(absWorkspace, allFiles, batchOpts.MaxFileBytes, cmd.ErrOrStderr())
	if err != nil {
		return writeSemanticError(cmd, semantic.ErrCodeCASResolveError, err.Error())
	}

	// Dry run: just list files
	if dryRun {
		return writeSemanticDryRun(cmd, semantic.JobTypeUpdateFiles, pathsFromJobFiles(filteredFiles), absWorkspace, skipped)
	}

	// Build job args
	workspaceID := workspaceutil.ID(absWorkspace)
	args := semantic.JobArgs{
		WorkspaceID: workspaceID,
		Reason:      semantic.ReasonPostReview,
		TaskID:      taskID,
		ReviewID:    reviewID,
	}
	args.Files = filteredFiles
	if len(args.Files) == 0 {
		return writeSemanticResult(cmd, semantic.JobTypeUpdateFiles, &semantic.JobResult{
			Summary: semantic.JobSummary{FilesSkipped: skipped},
		}, absWorkspace, start)
	}
	if enqueue {
		result, err := enqueueSemanticIndexFiles(ctx, absWorkspace, semantic.JobTypeUpdateFiles, args, chunkBytes, chunkOverlap, chunkDelay, model, providerName)
		if err != nil {
			return writeSemanticError(cmd, semantic.ErrCodeCASResolveError, err.Error())
		}
		return writeSemanticEnqueueResult(cmd, semantic.JobTypeUpdateFiles, result, absWorkspace, skipped, start)
	}

	// Create indexer and run
	indexer, cleanup, err := createSemanticIndexer(ctx, absWorkspace, chunkBytes, chunkOverlap, chunkDelay, model, providerName)
	if err != nil {
		return writeSemanticError(cmd, semantic.ErrCodeProviderConfigInvalid, err.Error())
	}
	defer cleanup()

	result, err := runSemanticIndexBatches(ctx, indexer, args, false, batchOpts, cmd.ErrOrStderr())
	if err != nil {
		return writeSemanticError(cmd, semantic.ErrCodeSemanticIndexNotFound, err.Error())
	}
	result.Summary.FilesSkipped += skipped

	return writeSemanticResult(cmd, semantic.JobTypeUpdateFiles, result, absWorkspace, start)
}

func enqueueSemanticIndexFiles(ctx context.Context, workspace, jobType string, args semantic.JobArgs, chunkBytes, chunkOverlap int, chunkDelay time.Duration, model, providerName string) (*semantic.FileQueueResult, error) {
	cfg, err := loadConfig(ctx, config.WithWorkspacePath(workspace))
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	store, err := semantic.OpenQueueStore(ctx, cfg.Paths.Cache)
	if err != nil {
		return nil, fmt.Errorf("open semantic queue: %w", err)
	}
	defer func() {
		_ = store.Close() //nolint:errcheck
	}()
	return store.EnqueueFiles(ctx, semantic.FileQueueRequest{
		Workspace:    workspace,
		JobType:      jobType,
		Args:         args,
		Provider:     normalizeSemanticIndexProvider(providerName),
		Model:        strings.TrimSpace(model),
		ChunkBytes:   chunkBytes,
		ChunkOverlap: chunkOverlap,
		ChunkDelay:   chunkDelay,
	})
}

func runSemanticIndexStats(cmd *cobra.Command, workspace string) error {
	start := time.Now()
	ctx := cmd.Context()

	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return writeSemanticError(cmd, semantic.ErrCodeCASResolveError, fmt.Sprintf("resolve workspace: %v", err))
	}
	workspaceID := workspaceutil.ID(absWorkspace)
	cfg, err := loadConfig(ctx, config.WithWorkspacePath(absWorkspace))
	if err != nil {
		return writeSemanticError(cmd, semantic.ErrCodeProviderConfigInvalid, fmt.Sprintf("load config: %v", err))
	}
	store, err := semantic.OpenQueueStore(ctx, cfg.Paths.Cache)
	if err != nil {
		return writeSemanticError(cmd, semantic.ErrCodeCASResolveError, fmt.Sprintf("open semantic queue: %v", err))
	}
	defer func() {
		_ = store.Close() //nolint:errcheck
	}()
	stats, err := store.Stats(ctx, workspaceID)
	if err != nil {
		return writeSemanticError(cmd, semantic.ErrCodeCASResolveError, fmt.Sprintf("queue stats: %v", err))
	}
	data := map[string]any{
		"workspace":    absWorkspace,
		"workspace_id": workspaceID,
		"stats":        stats,
	}
	env := protocol.OK("semantic_index.queue_stats", data,
		protocol.WithSource("cli"),
		protocol.WithWorkspace(absWorkspace),
		protocol.WithDuration(time.Since(start).Milliseconds()),
	)
	return protocol.Write(cmd.OutOrStdout(), env)
}

func runSemanticIndexDrain(cmd *cobra.Command, workspace, model, providerName string, batchSize int, maxDuration time.Duration, processAll bool, jobDelay, recoverStaleAfter time.Duration, chunkBytes, chunkOverlap int, chunkDelay time.Duration) error {
	start := time.Now()
	ctx := cmd.Context()
	if maxDuration <= 0 {
		return writeSemanticError(cmd, "EARG", "max-duration must be > 0")
	}
	if batchSize < 0 {
		return writeSemanticError(cmd, "EARG", "batch-size must be >= 0")
	}
	if jobDelay < 0 {
		return writeSemanticError(cmd, "EARG", "job-delay must be >= 0")
	}
	if recoverStaleAfter < 0 {
		return writeSemanticError(cmd, "EARG", "recover-stale-after must be >= 0")
	}
	if err := validateSemanticIndexOptions(chunkBytes, chunkOverlap, chunkDelay, semanticIndexBatchOptions{}); err != nil {
		return writeSemanticError(cmd, "EARG", err.Error())
	}

	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return writeSemanticError(cmd, semantic.ErrCodeCASResolveError, fmt.Sprintf("resolve workspace: %v", err))
	}
	workspaceID := workspaceutil.ID(absWorkspace)
	cfg, err := loadConfig(ctx, config.WithWorkspacePath(absWorkspace))
	if err != nil {
		return writeSemanticError(cmd, semantic.ErrCodeProviderConfigInvalid, fmt.Sprintf("load config: %v", err))
	}
	store, err := semantic.OpenQueueStore(ctx, cfg.Paths.Cache)
	if err != nil {
		return writeSemanticError(cmd, semantic.ErrCodeCASResolveError, fmt.Sprintf("open semantic queue: %v", err))
	}
	defer func() {
		_ = store.Close() //nolint:errcheck
	}()

	recoveredStale := int64(0)
	if recoverStaleAfter > 0 {
		recoveredStale, err = store.RequeueStaleRunning(ctx, recoverStaleAfter)
		if err != nil {
			return writeSemanticError(cmd, semantic.ErrCodeCASResolveError, fmt.Sprintf("recover stale semantic queue jobs: %v", err))
		}
	}

	deadline := time.Now().Add(maxDuration)
	processed := 0
	failed := 0
	queueFailures := 0
	finalResult := &semantic.JobResult{}
	lastError := ""
	limit := batchSize
	if processAll {
		limit = 0
	}

	for limit == 0 || processed+failed+queueFailures < limit {
		if time.Now().After(deadline) {
			break
		}
		queued, err := store.ClaimNext(ctx, workspaceID)
		if err != nil {
			return writeSemanticError(cmd, semantic.ErrCodeCASResolveError, fmt.Sprintf("claim semantic queue job: %v", err))
		}
		if queued == nil {
			break
		}

		result, err := drainSemanticQueueJob(ctx, queued.Payload, model, providerName, chunkBytes, chunkOverlap, chunkDelay)
		if result != nil {
			mergeSemanticJobResult(finalResult, result)
		}
		if err != nil {
			lastError = err.Error()
			queueFailures++
			if failErr := store.Fail(ctx, queued.ID, err.Error()); failErr != nil {
				return writeSemanticError(cmd, semantic.ErrCodeCASResolveError, fmt.Sprintf("mark semantic queue job failed: %v", failErr))
			}
		} else if result != nil && result.HasFailures() {
			lastError = result.Failures[0].ErrorMessage
			failed++
			if failErr := store.Fail(ctx, queued.ID, lastError); failErr != nil {
				return writeSemanticError(cmd, semantic.ErrCodeCASResolveError, fmt.Sprintf("mark semantic queue job failed: %v", failErr))
			}
		} else {
			processed++
			if err := store.Complete(ctx, queued.ID); err != nil {
				return writeSemanticError(cmd, semantic.ErrCodeCASResolveError, fmt.Sprintf("mark semantic queue job complete: %v", err))
			}
		}

		if jobDelay > 0 && (limit == 0 || processed+failed+queueFailures < limit) {
			timer := time.NewTimer(jobDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return writeSemanticError(cmd, semantic.ErrCodeCASResolveError, ctx.Err().Error())
			case <-timer.C:
			}
		}
	}

	stats, _ := store.Stats(ctx, workspaceID)
	data := map[string]any{
		"workspace":       absWorkspace,
		"workspace_id":    workspaceID,
		"processed":       processed,
		"failed":          failed,
		"queue_failures":  queueFailures,
		"recovered_stale": recoveredStale,
		"summary":         finalResult.Summary,
		"failures":        finalResult.Failures,
		"stats":           stats,
		"duration_ms":     time.Since(start).Milliseconds(),
	}
	if lastError != "" {
		data["last_error"] = lastError
	}
	env := protocol.OK("semantic_index.drain", data,
		protocol.WithSource("cli"),
		protocol.WithWorkspace(absWorkspace),
		protocol.WithDuration(time.Since(start).Milliseconds()),
	)
	return protocol.Write(cmd.OutOrStdout(), env)
}

func drainSemanticQueueJob(ctx context.Context, payload semantic.FileQueuePayload, modelOverride, providerOverride string, chunkBytesOverride, chunkOverlapOverride int, chunkDelayOverride time.Duration) (*semantic.JobResult, error) {
	workspace := firstNonEmpty(strings.TrimSpace(payload.Workspace), ".")
	model := firstNonEmpty(strings.TrimSpace(modelOverride), strings.TrimSpace(payload.Model))
	providerName := firstNonEmpty(strings.TrimSpace(providerOverride), strings.TrimSpace(payload.Provider))
	chunkBytes := payload.ChunkBytes
	if chunkBytesOverride > 0 {
		chunkBytes = chunkBytesOverride
	}
	chunkOverlap := payload.ChunkOverlap
	if chunkOverlapOverride > 0 {
		chunkOverlap = chunkOverlapOverride
	}
	chunkDelay := payload.ChunkDelay()
	if chunkDelayOverride > 0 {
		chunkDelay = chunkDelayOverride
	}

	indexer, cleanup, err := createSemanticIndexer(ctx, workspace, chunkBytes, chunkOverlap, chunkDelay, model, providerName)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	args := payload.JobArgs()
	switch payload.JobType {
	case semantic.JobTypeInitFiles:
		return indexer.RunInitFilesJob(ctx, args)
	case semantic.JobTypeUpdateFiles:
		return indexer.RunUpdateFilesJob(ctx, args)
	default:
		return nil, fmt.Errorf("unsupported semantic queue job type %q", payload.JobType)
	}
}

func runSemanticIndexBatches(ctx context.Context, indexer *semantic.Indexer, args semantic.JobArgs, initMode bool, opts semanticIndexBatchOptions, progress io.Writer) (*semantic.JobResult, error) {
	batchSize := opts.BatchSize
	if batchSize <= 0 || batchSize > len(args.Files) {
		batchSize = len(args.Files)
	}
	if batchSize == 0 {
		return &semantic.JobResult{}, nil
	}

	ranges := semanticIndexBatchRanges(len(args.Files), batchSize)
	final := &semantic.JobResult{}
	for idx, batchRange := range ranges {
		batchArgs := args
		batchArgs.Files = args.Files[batchRange[0]:batchRange[1]]
		if progress != nil && len(ranges) > 1 {
			delayAfter := time.Duration(0)
			if idx < len(ranges)-1 {
				delayAfter = opts.BatchDelay
			}
			fmt.Fprintf(progress, "semantic-index batch: %d/%d files=%d delay_after=%s\n", idx+1, len(ranges), len(batchArgs.Files), delayAfter) //nolint:forbidigo // CLI progress output
		}

		var result *semantic.JobResult
		var err error
		if initMode {
			result, err = indexer.RunInitFilesJob(ctx, batchArgs)
		} else {
			result, err = indexer.RunUpdateFilesJob(ctx, batchArgs)
		}
		if result != nil {
			mergeSemanticJobResult(final, result)
		}
		if err != nil {
			return final, err
		}
		if idx < len(ranges)-1 && opts.BatchDelay > 0 {
			timer := time.NewTimer(opts.BatchDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return final, ctx.Err()
			case <-timer.C:
			}
		}
	}
	return final, nil
}

func validateSemanticIndexOptions(chunkBytes, chunkOverlap int, chunkDelay time.Duration, opts semanticIndexBatchOptions) error {
	if chunkBytes < 0 {
		return fmt.Errorf("chunk-bytes must be >= 0")
	}
	if chunkOverlap < 0 {
		return fmt.Errorf("chunk-overlap must be >= 0")
	}
	if chunkBytes == 0 && chunkOverlap > 0 {
		return fmt.Errorf("chunk-overlap requires chunk-bytes > 0")
	}
	if chunkBytes > 0 && chunkOverlap >= chunkBytes {
		return fmt.Errorf("chunk-overlap must be less than chunk-bytes")
	}
	if chunkDelay < 0 {
		return fmt.Errorf("chunk-delay must be >= 0")
	}
	if opts.BatchSize < 0 {
		return fmt.Errorf("batch-size must be >= 0")
	}
	if opts.BatchDelay < 0 {
		return fmt.Errorf("batch-delay must be >= 0")
	}
	if opts.MaxFileBytes < 0 {
		return fmt.Errorf("max-file-bytes must be >= 0")
	}
	return nil
}

func semanticIndexBatchRanges(total, batchSize int) [][2]int {
	if total <= 0 || batchSize <= 0 {
		return nil
	}
	ranges := make([][2]int, 0, (total+batchSize-1)/batchSize)
	for start := 0; start < total; start += batchSize {
		end := start + batchSize
		if end > total {
			end = total
		}
		ranges = append(ranges, [2]int{start, end})
	}
	return ranges
}

func mergeSemanticJobResult(dst, src *semantic.JobResult) {
	if dst == nil || src == nil {
		return
	}
	dst.Summary.FilesIndexed += src.Summary.FilesIndexed
	dst.Summary.ChunksIndexed += src.Summary.ChunksIndexed
	dst.Summary.FilesSkipped += src.Summary.FilesSkipped
	dst.Failures = append(dst.Failures, src.Failures...)
	if src.CASArtifact != nil {
		dst.CASArtifact = src.CASArtifact
	}
}

func jobFilesFromPaths(paths []string, changeKind semantic.FileChangeKind) []semantic.JobFileInput {
	files := make([]semantic.JobFileInput, 0, len(paths))
	for _, path := range paths {
		files = append(files, semantic.JobFileInput{Path: path, ChangeKind: changeKind})
	}
	return files
}

func pathsFromJobFiles(files []semantic.JobFileInput) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
	}
	return paths
}

func filterSemanticIndexFilesBySize(workspace string, files []semantic.JobFileInput, maxFileBytes int64, progress io.Writer) ([]semantic.JobFileInput, int, error) {
	if maxFileBytes == 0 {
		return files, 0, nil
	}
	filtered := make([]semantic.JobFileInput, 0, len(files))
	skipped := 0
	for _, file := range files {
		if file.ChangeKind == semantic.ChangeKindDeleted {
			filtered = append(filtered, file)
			continue
		}
		path := filepath.Clean(file.Path)
		if filepath.IsAbs(path) || strings.HasPrefix(path, "..") {
			return nil, skipped, fmt.Errorf("invalid file path %q", file.Path)
		}
		info, err := os.Stat(filepath.Join(workspace, path))
		if err != nil {
			return nil, skipped, fmt.Errorf("stat %s: %w", file.Path, err)
		}
		if info.Size() > maxFileBytes {
			skipped++
			if progress != nil {
				fmt.Fprintf(progress, "semantic-index skip: path=%s size=%d max_file_bytes=%d\n", file.Path, info.Size(), maxFileBytes) //nolint:forbidigo // CLI progress output
			}
			continue
		}
		file.SizeBytes = info.Size()
		filtered = append(filtered, file)
	}
	return filtered, skipped, nil
}

func createSemanticIndexer(ctx context.Context, workspace string, chunkBytes, chunkOverlap int, chunkDelay time.Duration, model, providerName string) (*semantic.Indexer, func(), error) {
	// Load config
	cfg, err := loadConfig(ctx, config.WithWorkspacePath(workspace))
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}

	// Create embedding provider based on preference
	// Priority: explicit --provider flag > config/env provider > VOYAGE_API_KEY > GEMINI_API_KEY > noop
	var provider semantic.EmbeddingProvider

	voyageKey := os.Getenv("VOYAGE_API_KEY")
	geminiKey := os.Getenv("GEMINI_API_KEY")

	// Determine provider to use
	if providerName == "" {
		if detected := semantic.DetectProviderForConfig(cfg, voyageKey, geminiKey); detected != "" {
			providerName = detected
		} else {
			providerName = "noop"
		}
	}
	providerName = normalizeSemanticIndexProvider(providerName)

	switch providerName {
	case "voyage":
		if voyageKey == "" {
			return nil, nil, fmt.Errorf("voyage provider requires VOYAGE_API_KEY environment variable")
		}
		if model == "" {
			model = "voyage-code-3" // Best for code (1024 dims, 200M free tokens)
		}
		provider, err = semantic.NewVoyageProvider(semantic.VoyageConfig{
			APIKey: voyageKey,
			Model:  model,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("create voyage provider: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Using Voyage AI %s (1024 dims)\n", model)

	case "gemini":
		if geminiKey == "" {
			return nil, nil, fmt.Errorf("gemini provider requires GEMINI_API_KEY environment variable")
		}
		if model == "" {
			model = "gemini-embedding-001" // 3072 dims
		}
		provider, err = semantic.NewGeminiProvider(semantic.GeminiConfig{
			APIKey: geminiKey,
			Model:  model,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("create gemini provider: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Using Gemini %s (3072 dims)\n", model)

	case "openai_compat":
		if model == "" || strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "voyage-") {
			model = strings.TrimSpace(cfg.Embedding.Model)
		}
		if model == "" || strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "voyage-") {
			model = "text-embedding-nomic-embed-text-v1.5"
		}
		dimensions := semanticIndexOpenAICompatDimensions(model, cfg.Embedding.Dimensions)
		provider, err = semantic.NewOpenAICompatProvider(semantic.OpenAICompatConfig{
			APIKey:     cfg.Embedding.APIKey,
			Model:      model,
			BaseURL:    cfg.Embedding.BaseURL,
			Dimensions: dimensions,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("create openai-compatible provider: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Using OpenAI-compatible embeddings %s (%d dims)\n", model, provider.Dimensions())

	case "noop":
		dims := 1024 // Default to Voyage dimensions
		if model != "" && (model == "gemini-embedding-001" || model == "text-embedding-004") {
			dims = 3072
		}
		provider = semantic.NewNoOpProvider(model, dims)
		fmt.Fprintf(os.Stderr, "Using no-op provider (%d dims) - embeddings will be zero vectors\n", dims)

	default:
		return nil, nil, fmt.Errorf("unknown provider %q: use voyage, gemini, lmstudio/openai_compat, or noop", providerName)
	}

	if dims := provider.Dimensions(); dims > 0 {
		cfg.Embedding.Dimensions = dims
		cfg.Database.Vector.Dimensions = dims
	}
	store, err := memory.OpenWithConfig(ctx, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("open memory store: %w", err)
	}

	cleanup := func() {
		// Cleanup; error is not actionable.
		_ = store.Close() //nolint:errcheck
	}

	// Build indexer config
	indexerCfg := semantic.Config{
		Enabled:           true,
		ChunkBytes:        chunkBytes,
		ChunkOverlapBytes: chunkOverlap,
		ChunkDelay:        chunkDelay,
		ProviderModel:     model,
	}

	// TODO: Migrate semantic indexer to use observability instead of zerolog
	logger := zerolog.New(os.Stderr).With().Timestamp().Logger() //nolint:forbidigo // indexer requires zerolog
	indexer := semantic.NewIndexer(indexerCfg, store, provider, workspace, logger)

	return indexer, cleanup, nil
}

func normalizeSemanticIndexProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "lmstudio", "openai-compatible", "openai_compat":
		return "openai_compat"
	default:
		return strings.ToLower(strings.TrimSpace(provider))
	}
}

func semanticIndexOpenAICompatDimensions(model string, configured int) int {
	modelDims := semantic.DimensionsForModel(model)
	defaultDims := semantic.DimensionsForModel("")
	if modelDims > 0 && modelDims != defaultDims {
		return modelDims
	}
	if configured > 0 {
		return configured
	}
	return modelDims
}

func writeSemanticResult(cmd *cobra.Command, command string, result *semantic.JobResult, workspace string, start time.Time) error {
	data := map[string]any{
		"summary":  result.Summary,
		"failures": result.Failures,
	}
	if result.CASArtifact != nil {
		data["cas_artifact"] = result.CASArtifact
	}

	opts := []protocol.Option{
		protocol.WithSource("cli"),
		protocol.WithWorkspace(workspace),
		protocol.WithDuration(time.Since(start).Milliseconds()),
	}

	env := protocol.OK(command, data, opts...)

	return protocol.Write(cmd.OutOrStdout(), env)
}

func writeSemanticEnqueueResult(cmd *cobra.Command, sourceCommand string, result *semantic.FileQueueResult, workspace string, filesSkipped int, start time.Time) error {
	data := map[string]any{
		"source_command": sourceCommand,
		"queued":         result.Queued,
		"skipped":        result.Skipped,
		"files_skipped":  filesSkipped,
		"job_ids":        result.JobIDs,
	}
	env := protocol.OK("semantic_index.enqueue", data,
		protocol.WithSource("cli"),
		protocol.WithWorkspace(workspace),
		protocol.WithDuration(time.Since(start).Milliseconds()),
	)
	return protocol.Write(cmd.OutOrStdout(), env)
}

func writeSemanticDryRun(cmd *cobra.Command, command string, files []string, workspace string, filesSkipped int) error {
	data := map[string]any{
		"dry_run":       true,
		"files":         files,
		"files_count":   len(files),
		"files_skipped": filesSkipped,
		"workspace_id":  workspace,
	}

	env := protocol.OK(command, data, protocol.WithSource("cli"))
	return protocol.Write(cmd.OutOrStdout(), env)
}

func writeSemanticError(cmd *cobra.Command, code, message string) error {
	env := protocol.Error("semantic_index", protocol.ErrorCode(code), message, nil, protocol.WithSource("cli"))
	if err := protocol.Write(cmd.OutOrStdout(), env); err != nil {
		return fmt.Errorf("write error envelope: %w", err)
	}
	return fmt.Errorf("%s: %s", code, message)
}

func init() {
	rootCmd.AddCommand(newSemanticIndexCommand())
}
