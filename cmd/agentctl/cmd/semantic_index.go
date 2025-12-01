package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/storage/memory"
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
			"  agentctl semantic-index init --workspace <path> [--glob '**/*.go']\n" +
			"  agentctl semantic-index update --workspace <path> --files <file1,file2>\n\n" +
			"See docs/spec/semantic_file_index.md for detailed specification.",
	}
	cmd.AddCommand(
		newSemanticIndexInitCommand(),
		newSemanticIndexUpdateCommand(),
	)
	return cmd
}

func newSemanticIndexInitCommand() *cobra.Command {
	var workspace string
	var glob string
	var dryRun bool
	var taskID string
	var chunkBytes int
	var chunkOverlap int
	var model string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize semantic index for workspace files",
		Long:  "Initialize the semantic file index by generating embeddings for all matching files in the workspace.",
		Example: "  # Index all Go files in current directory\n" +
			"  agentctl semantic-index init --workspace . --glob '**/*.go'\n\n" +
			"  # Index with custom chunking config\n" +
			"  agentctl semantic-index init --workspace . --chunk-bytes 2048 --chunk-overlap 256\n\n" +
			"  # Dry run to see what would be indexed\n" +
			"  agentctl semantic-index init --workspace . --glob '**/*.go' --dry-run",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSemanticIndexInit(cmd, workspace, glob, dryRun, taskID, chunkBytes, chunkOverlap, model)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root directory")
	cmd.Flags().StringVar(&glob, "glob", "**/*.go", "Glob pattern to match files")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be indexed without making changes")
	cmd.Flags().StringVar(&taskID, "task-id", "", "Task ID for provenance tracking")
	cmd.Flags().IntVar(&chunkBytes, "chunk-bytes", 0, "Chunk size in bytes (0 = no chunking)")
	cmd.Flags().IntVar(&chunkOverlap, "chunk-overlap", 0, "Chunk overlap in bytes")
	cmd.Flags().StringVar(&model, "model", "", "Embedding model name")

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
	var model string

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update semantic index for changed files",
		Long:  "Update the semantic file index for specific files that have changed.",
		Example: "  # Update specific files\n" +
			"  agentctl semantic-index update --workspace . --files main.go,utils.go\n\n" +
			"  # Mark files as deleted\n" +
			"  agentctl semantic-index update --workspace . --deleted old_file.go\n\n" +
			"  # Update with provenance\n" +
			"  agentctl semantic-index update --workspace . --files main.go --task-id task-123 --review-id review-456",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSemanticIndexUpdate(cmd, workspace, files, deleted, dryRun, taskID, reviewID, chunkBytes, chunkOverlap, model)
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
	cmd.Flags().StringVar(&model, "model", "", "Embedding model name")

	return cmd
}

func runSemanticIndexInit(cmd *cobra.Command, workspace, glob string, dryRun bool, taskID string, chunkBytes, chunkOverlap int, model string) error {
	start := time.Now()
	ctx := cmd.Context()

	// Resolve workspace path
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return writeSemanticError(cmd, semantic.ErrCodeCASResolveError, fmt.Sprintf("resolve workspace: %v", err))
	}

	// Find files matching glob
	files, err := findFilesMatchingGlob(absWorkspace, glob)
	if err != nil {
		return writeSemanticError(cmd, semantic.ErrCodeCASResolveError, fmt.Sprintf("find files: %v", err))
	}

	if len(files) == 0 {
		return writeSemanticResult(cmd, semantic.JobTypeInitFiles, &semantic.JobResult{
			Summary: semantic.JobSummary{FilesSkipped: 0},
		}, absWorkspace, taskID, "", start)
	}

	// Dry run: just list files
	if dryRun {
		return writeSemanticDryRun(cmd, semantic.JobTypeInitFiles, files, absWorkspace)
	}

	// Build job args
	args := semantic.JobArgs{
		WorkspaceID: absWorkspace,
		Reason:      semantic.ReasonInitialIndex,
		TaskID:      taskID,
	}
	for _, f := range files {
		args.Files = append(args.Files, semantic.JobFileInput{Path: f})
	}

	// Create indexer and run
	indexer, cleanup, err := createSemanticIndexer(ctx, absWorkspace, chunkBytes, chunkOverlap, model)
	if err != nil {
		return writeSemanticError(cmd, semantic.ErrCodeProviderConfigInvalid, err.Error())
	}
	defer cleanup()

	result, err := indexer.RunInitFilesJob(ctx, args)
	if err != nil {
		return writeSemanticError(cmd, semantic.ErrCodeSemanticIndexNotFound, err.Error())
	}

	return writeSemanticResult(cmd, semantic.JobTypeInitFiles, result, absWorkspace, taskID, "", start)
}

func runSemanticIndexUpdate(cmd *cobra.Command, workspace string, files, deleted []string, dryRun bool, taskID, reviewID string, chunkBytes, chunkOverlap int, model string) error {
	start := time.Now()
	ctx := cmd.Context()

	if len(files) == 0 && len(deleted) == 0 {
		return writeSemanticError(cmd, "EARG", "at least one of --files or --deleted is required")
	}

	// Resolve workspace path
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return writeSemanticError(cmd, semantic.ErrCodeCASResolveError, fmt.Sprintf("resolve workspace: %v", err))
	}

	// Dry run: just list files
	if dryRun {
		allFiles := append(files, deleted...)
		return writeSemanticDryRun(cmd, semantic.JobTypeUpdateFiles, allFiles, absWorkspace)
	}

	// Build job args
	args := semantic.JobArgs{
		WorkspaceID: absWorkspace,
		Reason:      semantic.ReasonPostReview,
		TaskID:      taskID,
		ReviewID:    reviewID,
	}

	for _, f := range files {
		args.Files = append(args.Files, semantic.JobFileInput{
			Path:       f,
			ChangeKind: semantic.ChangeKindModified,
		})
	}
	for _, f := range deleted {
		args.Files = append(args.Files, semantic.JobFileInput{
			Path:       f,
			ChangeKind: semantic.ChangeKindDeleted,
		})
	}

	// Create indexer and run
	indexer, cleanup, err := createSemanticIndexer(ctx, absWorkspace, chunkBytes, chunkOverlap, model)
	if err != nil {
		return writeSemanticError(cmd, semantic.ErrCodeProviderConfigInvalid, err.Error())
	}
	defer cleanup()

	result, err := indexer.RunUpdateFilesJob(ctx, args)
	if err != nil {
		return writeSemanticError(cmd, semantic.ErrCodeSemanticIndexNotFound, err.Error())
	}

	return writeSemanticResult(cmd, semantic.JobTypeUpdateFiles, result, absWorkspace, taskID, reviewID, start)
}

func createSemanticIndexer(ctx context.Context, workspace string, chunkBytes, chunkOverlap int, model string) (*semantic.Indexer, func(), error) {
	// Load config
	cfg, err := config.Load(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}

	// Open memory store
	storageDir := filepath.Join(cfg.Home, "memory")
	casDir := cfg.Paths.CAS
	if casDir == "" {
		casDir = filepath.Join(cfg.Home, "cas")
	}
	store, err := memory.Open(ctx, storageDir, casDir)
	if err != nil {
		return nil, nil, fmt.Errorf("open memory store: %w", err)
	}

	cleanup := func() { _ = store.Close() }

	// Create embedding provider
	var provider semantic.EmbeddingProvider

	// Try to create Gemini provider if API key is available
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey != "" {
		if model == "" {
			model = "text-embedding-004"
		}
		provider, err = semantic.NewGeminiProvider(semantic.GeminiConfig{
			APIKey: apiKey,
			Model:  model,
		})
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("create embedding provider: %w", err)
		}
	} else {
		// Fall back to no-op provider for testing/development
		provider = semantic.NewNoOpProvider(model, 384)
	}

	// Build indexer config
	indexerCfg := semantic.Config{
		Enabled:           true,
		ChunkBytes:        chunkBytes,
		ChunkOverlapBytes: chunkOverlap,
		ProviderModel:     model,
	}

	logger := zerolog.New(os.Stderr).With().Timestamp().Logger()
	indexer := semantic.NewIndexer(indexerCfg, store, provider, workspace, logger)

	return indexer, cleanup, nil
}

func findFilesMatchingGlob(root, pattern string) ([]string, error) {
	var files []string

	// Handle ** patterns by walking the directory
	if strings.Contains(pattern, "**") {
		// Extract the extension pattern after **
		ext := ""
		if idx := strings.LastIndex(pattern, "*."); idx >= 0 {
			ext = pattern[idx+1:]
		}

		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil // Skip errors
			}
			if info.IsDir() {
				// Skip hidden directories
				if strings.HasPrefix(info.Name(), ".") && info.Name() != "." {
					return filepath.SkipDir
				}
				return nil
			}

			// Check extension match
			if ext != "" && !strings.HasSuffix(path, ext) {
				return nil
			}

			// Get relative path
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return nil
			}

			files = append(files, rel)
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		// Simple glob
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			rel, err := filepath.Rel(root, m)
			if err != nil {
				continue
			}
			files = append(files, rel)
		}
	}

	return files, nil
}

func writeSemanticResult(cmd *cobra.Command, command string, result *semantic.JobResult, workspace, taskID, reviewID string, start time.Time) error {
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

func writeSemanticDryRun(cmd *cobra.Command, command string, files []string, workspace string) error {
	data := map[string]any{
		"dry_run":      true,
		"files":        files,
		"files_count":  len(files),
		"workspace_id": workspace,
	}

	env := protocol.OK(command, data, protocol.WithSource("cli"))
	return protocol.Write(cmd.OutOrStdout(), env)
}

func writeSemanticError(cmd *cobra.Command, code, message string) error {
	env := protocol.Error("semantic_index", protocol.ErrorCode(code), message, protocol.WithSource("cli"))
	if err := protocol.Write(cmd.OutOrStdout(), env); err != nil {
		return fmt.Errorf("write error envelope: %w", err)
	}
	return fmt.Errorf("%s: %s", code, message)
}

func init() {
	rootCmd.AddCommand(newSemanticIndexCommand())
}
