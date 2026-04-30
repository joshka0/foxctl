package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
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
	var model string
	var provider string

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
			"  # Dry run to see what would be indexed\n" +
			"  foxctl semantic-index init --workspace . --glob '**/*.go' --dry-run",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSemanticIndexInit(cmd, workspace, glob, exclude, dryRun, taskID, chunkBytes, chunkOverlap, model, provider)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root directory")
	cmd.Flags().StringVar(&glob, "glob", "**/*.go", "Glob pattern to match files")
	cmd.Flags().StringSliceVar(&exclude, "exclude", nil, "Glob patterns to exclude (comma-separated, e.g., '*_test.go,vendor/**')")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be indexed without making changes")
	cmd.Flags().StringVar(&taskID, "task-id", "", "Task ID for provenance tracking")
	cmd.Flags().IntVar(&chunkBytes, "chunk-bytes", 0, "Chunk size in bytes (0 = no chunking)")
	cmd.Flags().IntVar(&chunkOverlap, "chunk-overlap", 0, "Chunk overlap in bytes")
	cmd.Flags().StringVar(&model, "model", "", "Embedding model name (default: voyage-code-3 or gemini-embedding-001)")
	cmd.Flags().StringVar(&provider, "provider", "", "Embedding provider: voyage (default), gemini, lmstudio/openai_compat, or noop")

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
	var provider string

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update semantic index for changed files",
		Long:  "Update the semantic file index for specific files that have changed.",
		Example: "  # Update specific files\n" +
			"  foxctl semantic-index update --workspace . --files main.go,utils.go\n\n" +
			"  # Mark files as deleted\n" +
			"  foxctl semantic-index update --workspace . --deleted old_file.go\n\n" +
			"  # Update with provenance\n" +
			"  foxctl semantic-index update --workspace . --files main.go --task-id task-123 --review-id review-456",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSemanticIndexUpdate(cmd, workspace, files, deleted, dryRun, taskID, reviewID, chunkBytes, chunkOverlap, model, provider)
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
	cmd.Flags().StringVar(&model, "model", "", "Embedding model name (default: voyage-code-3 or gemini-embedding-001)")
	cmd.Flags().StringVar(&provider, "provider", "", "Embedding provider: voyage (default), gemini, lmstudio/openai_compat, or noop")

	return cmd
}

func runSemanticIndexInit(cmd *cobra.Command, workspace, glob string, exclude []string, dryRun bool, taskID string, chunkBytes, chunkOverlap int, model, providerName string) error {
	start := time.Now()
	ctx := cmd.Context()

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

	// Dry run: just list files
	if dryRun {
		return writeSemanticDryRun(cmd, semantic.JobTypeInitFiles, files, absWorkspace)
	}

	// Build job args
	workspaceID := workspaceutil.ID(absWorkspace)
	args := semantic.JobArgs{
		WorkspaceID: workspaceID,
		Reason:      semantic.ReasonInitialIndex,
		TaskID:      taskID,
	}
	for _, f := range files {
		args.Files = append(args.Files, semantic.JobFileInput{Path: f})
	}

	// Create indexer and run
	indexer, cleanup, err := createSemanticIndexer(ctx, absWorkspace, chunkBytes, chunkOverlap, model, providerName)
	if err != nil {
		return writeSemanticError(cmd, semantic.ErrCodeProviderConfigInvalid, err.Error())
	}
	defer cleanup()

	result, err := indexer.RunInitFilesJob(ctx, args)
	if err != nil {
		return writeSemanticError(cmd, semantic.ErrCodeSemanticIndexNotFound, err.Error())
	}

	return writeSemanticResult(cmd, semantic.JobTypeInitFiles, result, absWorkspace, start)
}

func runSemanticIndexUpdate(cmd *cobra.Command, workspace string, files, deleted []string, dryRun bool, taskID, reviewID string, chunkBytes, chunkOverlap int, model, providerName string) error {
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

	// Dry run: just list files
	if dryRun {
		allFiles := append(files, deleted...)
		return writeSemanticDryRun(cmd, semantic.JobTypeUpdateFiles, allFiles, absWorkspace)
	}

	// Build job args
	workspaceID := workspaceutil.ID(absWorkspace)
	args := semantic.JobArgs{
		WorkspaceID: workspaceID,
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
	indexer, cleanup, err := createSemanticIndexer(ctx, absWorkspace, chunkBytes, chunkOverlap, model, providerName)
	if err != nil {
		return writeSemanticError(cmd, semantic.ErrCodeProviderConfigInvalid, err.Error())
	}
	defer cleanup()

	result, err := indexer.RunUpdateFilesJob(ctx, args)
	if err != nil {
		return writeSemanticError(cmd, semantic.ErrCodeSemanticIndexNotFound, err.Error())
	}

	return writeSemanticResult(cmd, semantic.JobTypeUpdateFiles, result, absWorkspace, start)
}

func createSemanticIndexer(ctx context.Context, workspace string, chunkBytes, chunkOverlap int, model, providerName string) (*semantic.Indexer, func(), error) {
	// Load config
	cfg, err := loadConfig(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}

	// Open memory store
	storageDir := cfg.Storage.Root
	if storageDir == "" {
		storageDir = filepath.Join(cfg.Home, "storage")
	}
	casDir := cfg.Paths.CAS
	if casDir == "" {
		casDir = filepath.Join(cfg.Home, "cas")
	}
	store, err := memory.Open(ctx, storageDir, casDir)
	if err != nil {
		return nil, nil, fmt.Errorf("open memory store: %w", err)
	}

	cleanup := func() {
		// Cleanup; error is not actionable.
		_ = store.Close() //nolint:errcheck
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
			cleanup()
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
			cleanup()
			return nil, nil, fmt.Errorf("create voyage provider: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Using Voyage AI %s (1024 dims)\n", model)

	case "gemini":
		if geminiKey == "" {
			cleanup()
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
			cleanup()
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
			cleanup()
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
		cleanup()
		return nil, nil, fmt.Errorf("unknown provider %q: use voyage, gemini, lmstudio/openai_compat, or noop", providerName)
	}

	// Build indexer config
	indexerCfg := semantic.Config{
		Enabled:           true,
		ChunkBytes:        chunkBytes,
		ChunkOverlapBytes: chunkOverlap,
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
