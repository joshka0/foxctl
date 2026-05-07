package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing"
	embedstore "github.com/joshka0/foxctl/internal/intelligence/indexing/embedding"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/filesummary"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/fsutil"
	"github.com/joshka0/foxctl/internal/platform/symbolutil"
	workspaceutil "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/protocol"
	llmproviders "github.com/joshka0/foxctl/internal/providers/llm"
	"github.com/joshka0/foxctl/internal/runtime/observability"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/dbdriver"
	"github.com/joshka0/foxctl/internal/storage/memory"
	"github.com/joshka0/foxctl/internal/storage/sessions"
	"github.com/joshka0/foxctl/internal/storage/tasks"
	"github.com/joshka0/foxctl/internal/storage/vector"
	"github.com/oklog/ulid/v2"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

// modelForScope returns the recommended model for a scope.
// Delegates to semantic.ScopeModelRecommendation to avoid duplication.
func modelForScope(scope string) string {
	model, _ := semantic.ScopeModelRecommendation(embeddingScopeForIndexScope(scope))
	return model
}

func modelForScopeConfig(scope string, cfg config.Config) string {
	return semantic.ResolveModelForScope(embeddingScopeForIndexScope(scope), cfg)
}

func embeddingScopeForIndexScope(scope string) semantic.EmbeddingScope {
	switch scope {
	case "symbols":
		return semantic.ScopeSymbols
	case "memory":
		return semantic.ScopeMemory
	case "tasks":
		return semantic.ScopeTasks
	case "sessions":
		return semantic.ScopeSessions
	case "file_summaries":
		return semantic.ScopeFileSummaries
	default:
		return semantic.ScopeDefault
	}
}

func indexEmbeddingProviderHint(cfg config.Config) string {
	provider := strings.ToLower(strings.TrimSpace(cfg.Embedding.Provider))
	switch provider {
	case "openai_compat", "openai-compatible", "lmstudio":
		return "set FOXCTL_EMBEDDING_PROVIDER=openai_compat with FOXCTL_EMBEDDING_MODEL and FOXCTL_EMBEDDING_BASE_URL for embeddings"
	default:
		return "set FOXCTL_EMBEDDING_PROVIDER=openai_compat or VOYAGE_API_KEY / GEMINI_API_KEY for embeddings"
	}
}

func createIndexEmbeddingProviderForScope(cfg config.Config, scope string) (semantic.EmbeddingProvider, error) {
	provider, err := semantic.NewProviderForScope(
		embeddingScopeForIndexScope(scope),
		cfg,
		semantic.WithVoyageKey(os.Getenv("VOYAGE_API_KEY")),
		semantic.WithGeminiKey(os.Getenv("GEMINI_API_KEY")),
	)
	if err != nil {
		return nil, fmt.Errorf("create embedding provider for %s: %w (%s)", scope, err, indexEmbeddingProviderHint(cfg))
	}
	return provider, nil
}

func maybeIndexEmbeddingProviderForScope(cfg config.Config, scope string) semantic.EmbeddingProvider {
	provider, err := createIndexEmbeddingProviderForScope(cfg, scope)
	if err != nil {
		return nil
	}
	return provider
}

// formatSessionEmbeddingText enriches session summary with date and activity type.
// Format: [Jan 2, 2006] [feature] Summary\nAccomplished: ...\nDecisions: ...
func formatSessionEmbeddingText(sess sessions.Session) string {
	var parts []string

	// Date prefix from session start time
	dateStr := sess.StartedAt.Format("Jan 2, 2006")

	// Activity type inferred from tags
	activity := inferSessionActivityType(sess.Tags)

	// Header with date and activity
	parts = append(parts, fmt.Sprintf("[%s] [%s]", dateStr, activity))

	if sess.Summary != "" {
		parts = append(parts, sess.Summary)
	}
	if len(sess.Accomplished) > 0 {
		parts = append(parts, "Accomplished: "+strings.Join(sess.Accomplished, "; "))
	}
	if len(sess.Decisions) > 0 {
		parts = append(parts, "Decisions: "+strings.Join(sess.Decisions, "; "))
	}
	if len(sess.Gotchas) > 0 {
		parts = append(parts, "Gotchas: "+strings.Join(sess.Gotchas, "; "))
	}
	if len(sess.KeyFiles) > 0 {
		parts = append(parts, "Files: "+strings.Join(sess.KeyFiles, ", "))
	}
	if len(sess.Tags) > 0 {
		parts = append(parts, "Topics: "+strings.Join(sess.Tags, ", "))
	}

	return strings.Join(parts, "\n")
}

// inferSessionActivityType derives activity type from session tags.
func inferSessionActivityType(tags []string) string {
	for _, tag := range tags {
		lower := strings.ToLower(tag)
		switch {
		case strings.Contains(lower, "bug") || strings.Contains(lower, "fix"):
			return "bugfix"
		case strings.Contains(lower, "feature") || strings.Contains(lower, "feat"):
			return "feature"
		case strings.Contains(lower, "refactor"):
			return "refactor"
		case strings.Contains(lower, "doc"):
			return "docs"
		case strings.Contains(lower, "test"):
			return "testing"
		case strings.Contains(lower, "perf"):
			return "performance"
		case strings.Contains(lower, "config") || strings.Contains(lower, "setup"):
			return "config"
		}
	}
	return "development"
}

// formatTaskEmbeddingText enriches task with date and status.
// Format: [Jan 2026] [completed]\nTask: title\nDescription: ...
func formatTaskEmbeddingText(t tasks.Task) string {
	var parts []string

	// Date prefix (use completed_at if done, else created_at)
	var dateStr string
	if t.CompletedAt != nil {
		dateStr = t.CompletedAt.Format("Jan 2006")
	} else {
		dateStr = t.CreatedAt.Format("Jan 2006")
	}

	// Status prefix
	status := t.Status
	if status == "" {
		status = "pending"
	}
	parts = append(parts, fmt.Sprintf("[%s] [%s]", dateStr, status))

	// Title is always included
	if t.Title != "" {
		parts = append(parts, "Task: "+t.Title)
	}

	// Description provides context
	if t.Description != "" {
		parts = append(parts, "Description: "+t.Description)
	}

	// Dependencies count
	if len(t.DependsOn) > 0 {
		parts = append(parts, fmt.Sprintf("Dependencies: %d tasks", len(t.DependsOn)))
	}

	// Epic association
	if t.EpicID != "" {
		parts = append(parts, "Epic: "+t.EpicID)
	}

	// Notes capture implementation details
	if t.Notes != "" {
		parts = append(parts, "Notes: "+t.Notes)
	}

	// Gotchas are valuable for future reference
	if t.Gotchas != "" {
		parts = append(parts, "Gotchas: "+t.Gotchas)
	}

	return strings.Join(parts, "\n")
}

func newIndexCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Manage embeddings for all scopes (symbols, sessions, memories, tasks)",
		Long: `Manage the embedding index across all knowledge scopes.

This is a simplified interface for full-repo embedding management.
For fine-grained control, use 'foxctl semantic-index'.

Scopes (model selection via semantic.ScopeModelRecommendation):
  symbols   - Code files (voyage-code-3)
  memory    - Gotchas/notes (voyage-3.5)
  tasks     - Task descriptions (voyage-3.5)
  sessions  - Session context (voyage-3.5)

Override with FOXCTL_EMBEDDING_MODEL_<SCOPE> or _CODE/_TEXT env vars.
Provider selection follows the configured embedding provider and supports
OpenAI-compatible endpoints as well as Voyage/Gemini.

Remote sync:
  Use 'index sync push' to push local embeddings to remote Turso for
  cross-workspace knowledge sharing.`,
	}
	cmd.AddCommand(
		newIndexInitCommand(),
		newIndexStatusCommand(),
		newIndexAnchorsCommand(),
		newIndexRepoCommand(),
		newIndexSyncCommand(),
		newIndexGitDiffCommand(),
		newIndexFileSummariesCommand(),
		newIndexSymbolIndexCommand(),
		newIndexSymbolSummariesCommand(),
	)
	return cmd
}

func newIndexInitCommand() *cobra.Command {
	var workspace string
	var scopes []string
	var glob string
	var exclude []string
	var dryRun bool
	var parallel bool
	var force bool
	var memoryEnqueue bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize embeddings for all scopes",
		Long: `Initialize embeddings for the entire repository across all scopes.

This command performs one-time full embedding generation using the
recommended models for each scope (see semantic.ScopeModelRecommendation):

  symbols   → voyage-code-3   (code files)
  memory    → voyage-3.5      (gotchas, notes)
  tasks     → voyage-3.5      (task descriptions)
  sessions  → voyage-3.5      (session summaries)

Provider selection follows the configured embedding provider and supports
OpenAI-compatible endpoints as well as Voyage/Gemini.
Override with FOXCTL_EMBEDDING_MODEL_<SCOPE> or _CODE/_TEXT env vars.`,
		Example: `  # Index everything (all scopes)
  foxctl index init

  # Index only symbols (code)
  foxctl index init --scope symbols

  # Index symbols and memories
  foxctl index init --scope symbols,memory

  # Custom glob pattern for symbols
  foxctl index init --glob '**/*.go' --exclude '*_test.go,vendor/**'

  # Dry run to see what would be indexed
  foxctl index init --dry-run

  # Queue missing memory embeddings for paced background worker drain
  foxctl index init --scope memory --memory-enqueue`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runIndexInit(cmd, workspace, scopes, glob, exclude, dryRun, parallel, force, memoryEnqueue)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root directory")
	cmd.Flags().StringSliceVar(&scopes, "scope", []string{"symbols", "memory", "tasks", "sessions"}, "Scopes to index (symbols, memory, tasks, sessions)")
	cmd.Flags().StringVar(&glob, "glob", "**/*.go", "Glob pattern for symbol files")
	cmd.Flags().StringSliceVar(&exclude, "exclude", []string{"*_test.go", "vendor/**"}, "Glob patterns to exclude for symbols")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be indexed without making changes")
	cmd.Flags().BoolVar(&parallel, "parallel", false, "Index scopes in parallel (faster but uses more API quota)")
	cmd.Flags().BoolVar(&force, "force", false, "Rebuild existing memory embeddings instead of only filling missing embeddings")
	cmd.Flags().BoolVar(&memoryEnqueue, "memory-enqueue", false, "Queue memory embedding jobs instead of embedding memory inline")

	return cmd
}

func newIndexSymbolIndexCommand() *cobra.Command {
	var workspace string
	var glob string
	var exclude []string
	var dryRun bool
	var force bool
	var maxFileKB int
	var maxFileLOC int
	var languages []string
	var embedding bool
	var embeddingModel string
	var embeddingStoreRoot string
	var embeddingTextMode string

	cmd := &cobra.Command{
		Use:   "symbol-index",
		Short: "Run the symbol indexer on a file set",
		Long: `Run the symbol indexer over a selected file set. This uses the doc-aware
embedding text pipeline and updates named-memory symbol entries.`,
		Example: `  # Index symbols for a single file
  foxctl index symbol-index --glob "internal/intelligence/indexing/repoindex/comment_edges.go"

  # Index symbols for Go and TS
  foxctl index symbol-index --glob "**/*.{go,ts,tsx}" --exclude "*_test.go,vendor/**"

  # Dry run to see matched files
  foxctl index symbol-index --dry-run`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runIndexSymbolIndex(cmd, workspace, glob, exclude, dryRun, force, maxFileKB, maxFileLOC, languages, embedding, embeddingModel, embeddingStoreRoot, embeddingTextMode)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root directory")
	cmd.Flags().StringVar(&glob, "glob", "**/*.{go,ts,tsx,js,jsx,py,ex,exs}", "Glob pattern for symbol files")
	cmd.Flags().StringSliceVar(&exclude, "exclude", []string{"*_test.go", "vendor/**"}, "Glob patterns to exclude for symbols")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be indexed without making changes")
	cmd.Flags().BoolVar(&force, "force", false, "Force re-index even if content/digests appear unchanged")
	cmd.Flags().IntVar(&maxFileKB, "max-file-kb", 512, "Skip files larger than this size in KB")
	cmd.Flags().IntVar(&maxFileLOC, "max-file-loc", 500, "Skip files with more lines than this limit")
	cmd.Flags().StringSliceVar(&languages, "language", nil, "Optional language filter (e.g. go,ts,tsx,js,py,ex,exs)")
	cmd.Flags().BoolVar(&embedding, "embedding", true, "Enable symbol embeddings")
	cmd.Flags().StringVar(&embeddingModel, "embedding-model", "", "Override embedding model (defaults to scope recommendation)")
	cmd.Flags().StringVar(&embeddingStoreRoot, "embedding-store-root", "", "Override embedding store root (defaults to storage root)")
	cmd.Flags().StringVar(&embeddingTextMode, "embedding-text-mode", "doc_enriched", "Embedding text mode (doc_enriched, summary_only)")

	return cmd
}

func newIndexStatusCommand() *cobra.Command {
	var workspace string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show embedding status for all scopes",
		Long:  "Display the current embedding status including counts and providers for each scope.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runIndexStatus(cmd, workspace)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root directory")

	return cmd
}

type scopeResult struct {
	Scope    string `json:"scope"`
	Model    string `json:"model"`
	Mode     string `json:"mode,omitempty"`
	Count    int    `json:"count"`
	Duration int64  `json:"duration_ms"`
	Error    string `json:"error,omitempty"`
}

func runIndexInit(cmd *cobra.Command, workspace string, scopes []string, glob string, exclude []string, dryRun, parallel, force, memoryEnqueue bool) error {
	start := time.Now()
	ctx := cmd.Context()

	// Resolve workspace
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return fmt.Errorf("resolve workspace: %w", err)
	}

	// Load config
	cfg, err := loadConfig(ctx, config.WithWorkspacePath(absWorkspace))
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Validate scopes
	validScopes := map[string]bool{"symbols": true, "memory": true, "tasks": true, "sessions": true}
	for _, s := range scopes {
		if !validScopes[s] {
			return fmt.Errorf("invalid scope %q: must be one of symbols, memory, tasks, sessions", s)
		}
	}

	if dryRun {
		return runIndexDryRun(cmd, cfg, absWorkspace, scopes, glob, exclude)
	}

	results := make([]scopeResult, 0, len(scopes))
	var mu sync.Mutex

	indexScope := func(scope string) scopeResult {
		scopeStart := time.Now()
		result := scopeResult{Scope: scope}

		switch scope {
		case "symbols":
			result.Model = modelForScopeConfig("symbols", cfg)
			count, err := indexSymbols(ctx, cfg, absWorkspace, glob, exclude)
			result.Count = count
			if err != nil {
				result.Error = err.Error()
			}

		case "memory":
			result.Model = modelForScopeConfig("memory", cfg)
			count, err := reembedMemories(ctx, cfg, absWorkspace, force, memoryEnqueue)
			if memoryEnqueue {
				result.Mode = "queued"
			}
			result.Count = count
			if err != nil {
				result.Error = err.Error()
			}

		case "tasks":
			result.Model = modelForScopeConfig("tasks", cfg)
			count, err := reembedTasks(ctx, cfg, absWorkspace)
			result.Count = count
			if err != nil {
				result.Error = err.Error()
			}

		case "sessions":
			result.Model = modelForScopeConfig("sessions", cfg)
			count, err := reembedSessions(ctx, cfg)
			result.Count = count
			if err != nil {
				result.Error = err.Error()
			}
		}

		result.Duration = time.Since(scopeStart).Milliseconds()
		return result
	}

	if parallel {
		var wg sync.WaitGroup
		for _, scope := range scopes {
			wg.Add(1)
			go func(s string) {
				defer wg.Done()
				result := indexScope(s)
				mu.Lock()
				results = append(results, result)
				mu.Unlock()
			}(scope)
		}
		wg.Wait()
	} else {
		for _, scope := range scopes {
			fmt.Fprintf(os.Stderr, "Indexing %s with %s...\n", scope, modelForScopeConfig(scope, cfg)) //nolint:forbidigo // CLI progress output
			result := indexScope(scope)
			results = append(results, result)
			if result.Error != "" {
				fmt.Fprintf(os.Stderr, "  Error: %s\n", result.Error) //nolint:forbidigo // CLI progress output
			} else {
				fmt.Fprintf(os.Stderr, "  Done: %d items in %dms\n", result.Count, result.Duration) //nolint:forbidigo // CLI progress output
			}
		}
	}

	// Build output
	data := map[string]any{
		"workspace": absWorkspace,
		"scopes":    results,
		"total_ms":  time.Since(start).Milliseconds(),
	}

	env := protocol.OK("index.init", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	return protocol.Write(cmd.OutOrStdout(), env)
}

func runIndexDryRun(cmd *cobra.Command, cfg config.Config, workspace string, scopes []string, glob string, exclude []string) error {
	data := map[string]any{
		"dry_run":   true,
		"workspace": workspace,
		"scopes":    []map[string]any{},
	}

	scopeInfo := make([]map[string]any, 0, len(scopes))
	for _, scope := range scopes {
		info := map[string]any{
			"scope": scope,
			"model": modelForScopeConfig(scope, cfg),
		}

		if scope == "symbols" {
			files, err := fsutil.FindFilesMatchingGlob(workspace, glob, exclude)
			if err != nil {
				info["error"] = err.Error()
			} else {
				info["files_count"] = len(files)
			}
		}

		scopeInfo = append(scopeInfo, info)
	}
	data["scopes"] = scopeInfo

	env := protocol.OK("index.init", data, protocol.WithSource("cli"))
	return protocol.Write(cmd.OutOrStdout(), env)
}

func runIndexStatus(cmd *cobra.Command, workspace string) error {
	ctx := cmd.Context()

	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return fmt.Errorf("resolve workspace: %w", err)
	}

	cfg, err := loadConfig(ctx, config.WithWorkspacePath(absWorkspace))
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	storageDir := filepath.Join(cfg.Home, "storage")

	// Get counts from each store
	status := map[string]any{
		"workspace": absWorkspace,
		"scopes":    []map[string]any{},
	}

	scopes := []map[string]any{}

	// Symbols - check memory store stats
	memStore, err := memory.OpenWithConfig(ctx, cfg)
	if err == nil {
		defer memStore.Close()
		stats, _ := memStore.Stats(ctx)
		scopes = append(scopes, map[string]any{
			"scope":             "symbols",
			"recommended_model": modelForScopeConfig("symbols", cfg),
			"total_memories":    stats.Named,
		})
	}

	// Memory count
	scopes = append(scopes, map[string]any{
		"scope":             "memory",
		"recommended_model": modelForScopeConfig("memory", cfg),
		"status":            "use 'foxctl memory stats' for details",
	})

	// Tasks
	taskStore, err := tasks.Open(ctx, storageDir)
	if err == nil {
		defer taskStore.Close()
		all, _ := taskStore.ListAll(ctx, 1000)
		scopes = append(scopes, map[string]any{
			"scope":             "tasks",
			"recommended_model": modelForScopeConfig("tasks", cfg),
			"total_count":       len(all),
		})
	}

	// Sessions
	sessStore, err := sessions.Open(ctx, storageDir)
	if err == nil {
		defer sessStore.Close()
		opts := sessions.ListOptions{Limit: 1000}
		recent, _ := sessStore.List(ctx, opts)
		scopes = append(scopes, map[string]any{
			"scope":             "sessions",
			"recommended_model": modelForScopeConfig("sessions", cfg),
			"recent_count":      len(recent),
		})
	}

	status["scopes"] = scopes

	env := protocol.OK("index.status", status, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	return protocol.Write(cmd.OutOrStdout(), env)
}

func indexSymbols(ctx context.Context, cfg config.Config, workspace, glob string, exclude []string) (int, error) {
	store, err := memory.OpenWithConfig(ctx, cfg)
	if err != nil {
		return 0, fmt.Errorf("open memory store: %w", err)
	}
	defer store.Close()

	provider, err := createIndexEmbeddingProviderForScope(cfg, "symbols")
	if err != nil {
		return 0, err
	}

	files, err := fsutil.FindFilesRespectingGitignore(workspace, glob, exclude)
	if err != nil {
		return 0, fmt.Errorf("find files: %w", err)
	}

	if len(files) == 0 {
		return 0, nil
	}

	indexerCfg := semantic.Config{Enabled: true}
	// TODO: Migrate semantic indexer to use observability instead of zerolog
	logger := zerolog.New(os.Stderr).With().Timestamp().Logger() //nolint:forbidigo // semantic indexer requires zerolog
	indexer := semantic.NewIndexer(indexerCfg, store, provider, workspace, logger)

	workspaceID := workspaceutil.ID(workspace)
	args := semantic.JobArgs{
		WorkspaceID: workspaceID,
		Reason:      semantic.ReasonInitialIndex,
	}
	for _, f := range files {
		args.Files = append(args.Files, semantic.JobFileInput{Path: f})
	}

	result, err := indexer.RunInitFilesJob(ctx, args)
	if err != nil {
		return 0, err
	}

	return result.Summary.FilesIndexed, nil
}

func runIndexSymbolIndex(cmd *cobra.Command, workspace, glob string, exclude []string, dryRun, force bool, maxFileKB, maxFileLOC int, languages []string, embedding bool, embeddingModel, embeddingStoreRoot, embeddingTextMode string) error {
	ctx := cmd.Context()
	start := time.Now()

	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return fmt.Errorf("resolve workspace: %w", err)
	}

	files, err := fsutil.FindFilesRespectingGitignore(absWorkspace, glob, exclude)
	if err != nil {
		return fmt.Errorf("find files: %w", err)
	}

	if len(files) == 0 {
		data := map[string]any{
			"workspace":    absWorkspace,
			"workspace_id": workspaceutil.ID(absWorkspace),
			"file_count":   0,
			"glob":         glob,
			"exclude":      exclude,
			"dry_run":      dryRun,
		}
		env := protocol.OK("index.symbol_index", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		return protocol.Write(cmd.OutOrStdout(), env)
	}

	if dryRun {
		data := map[string]any{
			"workspace":    absWorkspace,
			"workspace_id": workspaceutil.ID(absWorkspace),
			"file_count":   len(files),
			"glob":         glob,
			"exclude":      exclude,
			"files":        files,
			"dry_run":      true,
		}
		env := protocol.OK("index.symbol_index", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		return protocol.Write(cmd.OutOrStdout(), env)
	}

	cfg, err := loadConfig(ctx, config.WithWorkspacePath(absWorkspace))
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	store, err := memory.OpenWithConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("open memory store: %w", err)
	}
	defer store.Close()

	symCfg := symbol.DefaultConfig()
	symCfg.Enabled = true
	symCfg.Force = force
	symCfg.MaxFileKB = maxFileKB
	symCfg.MaxFileLOC = maxFileLOC
	symCfg.Languages = languages
	symCfg.EmbeddingEnabled = embedding
	if strings.TrimSpace(embeddingModel) != "" {
		symCfg.EmbeddingModel = strings.TrimSpace(embeddingModel)
	}
	if strings.TrimSpace(embeddingStoreRoot) != "" {
		symCfg.EmbeddingStoreRoot = strings.TrimSpace(embeddingStoreRoot)
	}
	if strings.TrimSpace(embeddingTextMode) != "" {
		symCfg.EmbeddingTextMode = config.ResolveEmbedSymbolTextMode(config.EmbedSymbolTextMode(embeddingTextMode))
	}

	logger := zerolog.New(os.Stderr).With().Timestamp().Logger() //nolint:forbidigo // symbol indexer requires zerolog
	indexer := symbol.NewIndexer(symCfg, store, nil, absWorkspace, logger)

	now := time.Now().UTC()
	filesChanged := make([]indexing.FileChange, 0, len(files))
	for _, file := range files {
		filesChanged = append(filesChanged, indexing.FileChange{
			Path:       file,
			ChangeKind: indexing.ChangeKindModified,
		})
	}

	event := indexing.PostReviewEvent{
		ID:            ulid.Make().String(),
		WorkspaceID:   workspaceutil.ID(absWorkspace),
		ReviewKind:    "manual",
		ReviewStatus:  "ok",
		DiffAppliedAt: now,
		Files:         filesChanged,
		Source:        "cli",
		CreatedAt:     now,
		Reason:        "manual",
	}

	result, err := indexer.Index(ctx, event)
	if err != nil {
		return err
	}

	data := map[string]any{
		"workspace":    absWorkspace,
		"workspace_id": event.WorkspaceID,
		"file_count":   len(files),
		"indexed":      result.FilesIndexed,
		"skipped":      result.FilesSkipped,
		"failed":       result.FilesFailed,
		"failures":     result.Failures,
		"duration_ms":  time.Since(start).Milliseconds(),
	}

	env := protocol.OK("index.symbol_index", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	return protocol.Write(cmd.OutOrStdout(), env)
}

func reembedMemories(ctx context.Context, cfg config.Config, workspace string, force, enqueue bool) (int, error) {
	store, err := memory.OpenWithConfig(ctx, cfg)
	if err != nil {
		return 0, fmt.Errorf("open memory store: %w", err)
	}
	defer store.Close()

	if enqueue {
		return enqueueMemoryEmbeddingJobs(ctx, cfg, store, workspace, force)
	}

	provider, err := createIndexEmbeddingProviderForScope(cfg, "memory")
	if err != nil {
		return 0, err
	}

	if force {
		memories, err := listMemoriesForReembedding(ctx, store, workspace, 1000)
		if err != nil {
			return 0, err
		}
		return embedMemoryEntries(ctx, cfg, store, provider, workspace, memories)
	}

	totalCount := 0
	batchSize := 1000
	for {
		memories, err := store.ListWithoutEmbedding(ctx, workspace, batchSize)
		if err != nil {
			return totalCount, fmt.Errorf("list memories without embeddings: %w", err)
		}
		if len(memories) == 0 {
			break
		}
		batchCount, err := embedMemoryEntries(ctx, cfg, store, provider, workspace, memories)
		totalCount += batchCount
		if err != nil {
			return totalCount, err
		}
		if batchCount == 0 {
			break
		}
	}

	return totalCount, nil
}

func enqueueMemoryEmbeddingJobs(ctx context.Context, cfg config.Config, store storage.MemoryStore, workspace string, force bool) (int, error) {
	queueStore, err := embedstore.OpenStore(ctx, cfg.Paths.Cache)
	if err != nil {
		return 0, fmt.Errorf("open embedding queue: %w", err)
	}
	defer queueStore.Close() //nolint:errcheck

	workspaceID := workspaceutil.CanonicalID(workspace)
	model := modelForScopeConfig("memory", cfg)
	batchSize := 1000
	offset := 0
	totalQueued := 0
	for {
		var memories []storage.NamedEntry
		if force {
			entries, _, err := store.ListFiltered(ctx, workspaceID, storage.MemoryListFilter{}, batchSize, offset)
			if err != nil {
				return totalQueued, fmt.Errorf("list memories: %w", err)
			}
			memories = entries
		} else {
			entries, err := store.ListWithoutEmbeddingPage(ctx, workspaceID, batchSize, offset)
			if err != nil {
				return totalQueued, fmt.Errorf("list memories without embeddings: %w", err)
			}
			memories = entries
		}
		if len(memories) == 0 {
			break
		}

		result, err := queueStore.EnqueueMemories(ctx, embedstore.MemoryEnqueueRequest{
			WorkspaceID: workspaceID,
			Memories:    memoryEmbeddingInputs(memories),
			Priority:    embedstore.PriorityNormal,
			Model:       model,
		})
		if err != nil {
			return totalQueued, fmt.Errorf("enqueue memory embeddings: %w", err)
		}
		totalQueued += result.Queued
		offset += len(memories)
	}
	return totalQueued, nil
}

func memoryEmbeddingInputs(entries []storage.NamedEntry) []embedstore.MemoryInput {
	inputs := make([]embedstore.MemoryInput, 0, len(entries))
	for _, entry := range entries {
		inputs = append(inputs, embedstore.MemoryInput{
			Name:    entry.Name,
			Type:    entry.Type,
			Content: formatMemoryEmbeddingContent(entry),
		})
	}
	return inputs
}

func formatMemoryEmbeddingContent(entry storage.NamedEntry) string {
	dateStr := entry.CreatedAt.Format("Jan 2006")
	typeStr := strings.TrimSpace(entry.Type)
	if typeStr == "" {
		typeStr = "note"
	}
	content := strings.TrimSpace(entry.Summary)
	if content == "" {
		content = strings.TrimSpace(entry.Name)
	}
	return fmt.Sprintf("[%s] [%s] %s", dateStr, typeStr, content)
}

func embedMemoryEntries(ctx context.Context, cfg config.Config, store storage.MemoryStore, provider semantic.EmbeddingProvider, workspace string, memories []storage.NamedEntry) (int, error) {
	totalCount := 0
	batchSize := 1000
	for start := 0; start < len(memories); start += batchSize {
		end := start + batchSize
		if end > len(memories) {
			end = len(memories)
		}
		batchCount := 0
		for _, mem := range memories[start:end] {
			embedding, err := provider.Embed(ctx, strings.TrimSpace(mem.Summary))
			if err != nil {
				observability.Emit(ctx, observability.NewEvent("index.memory_embed").
					WithComponent(observability.ComponentCLI).
					WithData("memory", mem.Name).
					Error(err, 0))
				continue // Skip on error, don't fail entire batch
			}
			if len(embedding) == 0 {
				continue
			}
			if _, ok := store.(*memory.TursoStore); ok {
				if err := store.ValidateEmbeddingDimensions(ctx, workspace, len(embedding)); err != nil {
					return totalCount, err
				}
			}
			if err := store.SetEmbeddingMetadata(ctx, storage.EmbeddingMetadata{
				Workspace:  workspace,
				Provider:   semantic.DetectProviderForConfig(cfg, os.Getenv("VOYAGE_API_KEY"), os.Getenv("GEMINI_API_KEY")),
				Model:      provider.Model(),
				Dimensions: len(embedding),
				CreatedAt:  mem.CreatedAt,
				UpdatedAt:  mem.UpdatedAt,
			}); err != nil {
				return totalCount, fmt.Errorf("set memory embedding metadata: %w", err)
			}

			if err := store.UpdateEmbedding(ctx, mem.Name, workspace, embedding); err != nil {
				observability.Emit(ctx, observability.NewEvent("index.memory_embed").
					WithComponent(observability.ComponentCLI).
					WithData("memory", mem.Name).
					WithData("phase", "update").
					Error(err, 0))
				continue
			}
			observability.Emit(ctx, observability.NewEvent("index.memory_embed").
				WithComponent(observability.ComponentCLI).
				WithData("memory", mem.Name).
				Success(0))
			batchCount++
		}
		totalCount += batchCount
		observability.Emit(ctx, observability.NewEvent("index.memory_batch_complete").
			WithComponent(observability.ComponentCLI).
			WithData("batch_count", batchCount).
			Success(0))
	}
	return totalCount, nil
}

func listMemoriesForReembedding(ctx context.Context, store storage.MemoryStore, workspace string, batchSize int) ([]storage.NamedEntry, error) {
	if batchSize <= 0 {
		batchSize = 1000
	}
	var result []storage.NamedEntry
	offset := 0
	for {
		entries, total, err := store.ListFiltered(ctx, workspace, storage.MemoryListFilter{}, batchSize, offset)
		if err != nil {
			return nil, fmt.Errorf("list memories: %w", err)
		}
		for _, entry := range entries {
			if strings.TrimSpace(entry.Summary) != "" {
				result = append(result, entry)
			}
		}
		if len(entries) == 0 || offset+len(entries) >= total {
			break
		}
		offset += len(entries)
	}
	return result, nil
}

func reembedTasks(ctx context.Context, cfg config.Config, defaultWorkspace string) (int, error) {
	storageDir := filepath.Join(cfg.Home, "storage")
	store, err := tasks.Open(ctx, storageDir)
	if err != nil {
		return 0, fmt.Errorf("open tasks store: %w", err)
	}
	defer store.Close()

	// Also open memory store to store task embeddings for semantic search
	memStore, err := memory.OpenWithConfig(ctx, cfg)
	if err != nil {
		return 0, fmt.Errorf("open memory store: %w", err)
	}
	defer memStore.Close()

	provider, err := createIndexEmbeddingProviderForScope(cfg, "tasks")
	if err != nil {
		return 0, err
	}

	allTasks, err := store.ListAll(ctx, 1000)
	if err != nil {
		return 0, fmt.Errorf("list tasks: %w", err)
	}

	count := 0
	for _, task := range allTasks {
		// Use enriched text with date prefix and status
		text := formatTaskEmbeddingText(task)
		if text == "" {
			continue
		}

		embedding, err := provider.Embed(ctx, text)
		if err != nil {
			observability.Emit(ctx, observability.NewEvent("index.task_embed").
				WithComponent(observability.ComponentCLI).
				WithData("task_id", task.ID).
				Error(err, 0))
			continue
		}

		// Convert []float32 to []byte (binary format for vector search)
		embeddingBytes := vector.SerializeF32(embedding)

		// Store embedding in tasks.db
		if err := store.SetEmbedding(ctx, task.ID, embeddingBytes, modelForScope("tasks")); err != nil {
			continue
		}

		// Also store in memory.db for semantic search skill compatibility
		// Use name format: task://<task_id> with type: task_embedding
		workspacePath := task.WorkspaceID
		if workspacePath == "" {
			workspacePath = defaultWorkspace // fallback to current workspace
		}
		workspace := workspacePath
		if strings.ContainsAny(workspacePath, "/\\") {
			if workspaceID := workspaceutil.ID(workspacePath); workspaceID != "" {
				workspace = workspaceID
			}
		}
		entryName := "task://" + task.ID

		// Create task metadata as result JSON
		taskResult, _ := json.Marshal(map[string]string{
			"task_id": task.ID,
			"status":  task.Status,
		})

		if _, err := memStore.SaveResult(ctx, memory.SaveOptions{
			Name:      entryName,
			Type:      "task_embedding",
			Workspace: workspace,
			Summary:   text,
			Result:    taskResult,
		}); err != nil {
			observability.Emit(ctx, observability.NewEvent("index.task_memory_save_warning").
				WithComponent(observability.ComponentCLI).
				WithData("task_id", task.ID).
				WithData("reason", "save to memory").
				Error(err, 0))
			// Continue anyway - tasks.db already has the embedding
		}

		// Update embedding in memory.db
		if err := memStore.UpdateEmbedding(ctx, entryName, workspace, embedding); err != nil {
			observability.Emit(ctx, observability.NewEvent("index.task_embedding_update_warning").
				WithComponent(observability.ComponentCLI).
				WithData("task_id", task.ID).
				WithData("reason", "update embedding in memory").
				Error(err, 0))
		}

		observability.Emit(ctx, observability.NewEvent("index.task_embed").
			WithComponent(observability.ComponentCLI).
			WithData("task_id", task.ID).
			Success(0))
		count++
	}

	return count, nil
}

func reembedSessions(ctx context.Context, cfg config.Config) (int, error) {
	storageDir := filepath.Join(cfg.Home, "storage")

	store, err := sessions.Open(ctx, storageDir)
	if err != nil {
		return 0, fmt.Errorf("open sessions store: %w", err)
	}
	defer store.Close()

	provider, err := createIndexEmbeddingProviderForScope(cfg, "sessions")
	if err != nil {
		return 0, err
	}

	opts := sessions.ListOptions{Limit: 1000}
	recentSessions, err := store.List(ctx, opts)
	if err != nil {
		return 0, fmt.Errorf("list sessions: %w", err)
	}

	count := 0
	for _, sess := range recentSessions {
		// Use enriched text with date prefix and metadata
		text := formatSessionEmbeddingText(sess)
		if text == "" {
			continue
		}

		embedding, err := provider.Embed(ctx, text)
		if err != nil {
			observability.Emit(ctx, observability.NewEvent("index.session_embed").
				WithComponent(observability.ComponentCLI).
				WithData("session_id", sess.ID).
				Error(err, 0))
			continue
		}

		// Convert []float32 to []byte (binary format for vector search)
		embeddingBytes := vector.SerializeF32(embedding)

		if err := store.SetEmbedding(ctx, sess.ID, embeddingBytes, modelForScope("sessions")); err != nil {
			observability.Emit(ctx, observability.NewEvent("index.session_embed").
				WithComponent(observability.ComponentCLI).
				WithData("session_id", sess.ID).
				WithData("phase", "store").
				Error(err, 0))
			continue
		}
		observability.Emit(ctx, observability.NewEvent("index.session_embed").
			WithComponent(observability.ComponentCLI).
			WithData("session_id", sess.ID).
			Success(0))
		count++
	}

	return count, nil
}

func newIndexSyncCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync embeddings with remote Turso database",
		Long: `Sync local embeddings with a remote Turso database for cross-workspace
knowledge sharing.

This enables:
  - Pushing local embeddings to a central Turso database
  - Querying across embeddings from multiple workspaces
  - Sharing knowledge between different development environments

Requires TURSO_DATABASE_URL and TURSO_AUTH_TOKEN environment variables,
or use --remote-url and --remote-token flags.`,
	}
	cmd.AddCommand(
		newIndexSyncPushCommand(),
		newIndexSyncQueryCommand(),
	)
	return cmd
}

func newIndexSyncPushCommand() *cobra.Command {
	var workspace string
	var remoteURL string
	var remoteToken string
	var scopes []string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "push",
		Short: "Push local embeddings to remote Turso",
		Long: `Push local embeddings from SQLite to a remote Turso database.

This copies all embeddings from the local memory store to the remote Turso
database, enabling cross-workspace similarity search.

Environment variables:
  TURSO_DATABASE_URL   - Remote Turso database URL
  TURSO_AUTH_TOKEN     - Turso authentication token`,
		Example: `  # Push all scopes to remote Turso (using env vars)
  export TURSO_DATABASE_URL=libsql://your-db.turso.io
  export TURSO_AUTH_TOKEN=your-token
  foxctl index sync push

  # Push only memory scope
  foxctl index sync push --scope memory

  # Push with explicit connection
  foxctl index sync push --remote-url libsql://db.turso.io --remote-token xxx

  # Dry run to see what would be pushed
  foxctl index sync push --dry-run`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runIndexSyncPush(cmd, workspace, remoteURL, remoteToken, scopes, dryRun)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Local workspace root directory")
	cmd.Flags().StringVar(&remoteURL, "remote-url", "", "Remote Turso database URL (or TURSO_DATABASE_URL)")
	cmd.Flags().StringVar(&remoteToken, "remote-token", "", "Remote Turso auth token (or TURSO_AUTH_TOKEN)")
	cmd.Flags().StringSliceVar(&scopes, "scope", []string{"memory"}, "Scopes to push (memory, sessions)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be pushed without making changes")

	return cmd
}

func newIndexSyncQueryCommand() *cobra.Command {
	var remoteURL string
	var remoteToken string
	var query string
	var workspaces []string
	var limit int
	var global bool

	cmd := &cobra.Command{
		Use:   "query",
		Short: "Query embeddings from remote Turso",
		Long: `Query embeddings from a remote Turso database using semantic search.

This enables cross-workspace knowledge search by querying a central
Turso database that contains embeddings from multiple workspaces.`,
		Example: `  # Query across all workspaces (global search)
  foxctl index sync query --query "authentication middleware" --global

  # Query specific workspaces
  foxctl index sync query --query "API error handling" --workspaces project-a,project-b

  # Limit results
  foxctl index sync query --query "database migrations" --global --limit 5`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runIndexSyncQuery(cmd, remoteURL, remoteToken, query, workspaces, limit, global)
		},
	}

	cmd.Flags().StringVar(&remoteURL, "remote-url", "", "Remote Turso database URL (or TURSO_DATABASE_URL)")
	cmd.Flags().StringVar(&remoteToken, "remote-token", "", "Remote Turso auth token (or TURSO_AUTH_TOKEN)")
	cmd.Flags().StringVar(&query, "query", "", "Search query text")
	cmd.Flags().StringSliceVar(&workspaces, "workspaces", nil, "Filter to specific workspaces (comma-separated)")
	cmd.Flags().IntVar(&limit, "limit", 10, "Maximum results to return")
	cmd.Flags().BoolVar(&global, "global", false, "Search across all workspaces")
	_ = cmd.MarkFlagRequired("query")

	return cmd
}

func runIndexSyncPush(cmd *cobra.Command, workspace, remoteURL, remoteToken string, scopes []string, dryRun bool) error {
	ctx := cmd.Context()
	start := time.Now()

	// Get remote connection info
	if remoteURL == "" {
		remoteURL = os.Getenv("TURSO_DATABASE_URL")
	}
	if remoteToken == "" {
		remoteToken = os.Getenv("TURSO_AUTH_TOKEN")
	}
	if remoteURL == "" {
		return fmt.Errorf("remote Turso URL required: set TURSO_DATABASE_URL or use --remote-url")
	}
	if remoteToken == "" {
		return fmt.Errorf("remote Turso token required: set TURSO_AUTH_TOKEN or use --remote-token")
	}

	// Resolve workspace
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return fmt.Errorf("resolve workspace: %w", err)
	}

	// Load config
	cfg, err := loadConfig(ctx, config.WithWorkspacePath(absWorkspace))
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Validate scopes
	validScopes := map[string]bool{"memory": true, "sessions": true}
	for _, s := range scopes {
		if !validScopes[s] {
			return fmt.Errorf("invalid scope %q: must be one of memory, sessions", s)
		}
	}

	results := map[string]any{
		"workspace": absWorkspace,
		"remote":    remoteURL,
		"scopes":    []map[string]any{},
	}

	scopeResults := []map[string]any{}

	for _, scope := range scopes {
		scopeStart := time.Now()
		result := map[string]any{
			"scope": scope,
		}

		switch scope {
		case "memory":
			count, err := syncMemoryToTurso(ctx, cfg, absWorkspace, remoteURL, remoteToken, dryRun)
			result["count"] = count
			if err != nil {
				result["error"] = err.Error()
			}
		case "sessions":
			count, err := syncSessionsToTurso(ctx, cfg, remoteURL, remoteToken, dryRun)
			result["count"] = count
			if err != nil {
				result["error"] = err.Error()
			}
		}

		result["duration_ms"] = time.Since(scopeStart).Milliseconds()
		scopeResults = append(scopeResults, result)

		if !dryRun {
			if errMsg, ok := result["error"].(string); ok && errMsg != "" {
				observability.Emit(ctx, observability.NewEvent("index.push_scope_error").
					WithComponent(observability.ComponentCLI).
					WithData("scope", scope).
					WithData("error", errMsg).
					Error(errors.New(errMsg), 0))
			} else {
				observability.Emit(ctx, observability.NewEvent("index.push_scope_complete").
					WithComponent(observability.ComponentCLI).
					WithData("scope", scope).
					WithData("count", result["count"]).
					Success(0))
			}
		}
	}

	results["scopes"] = scopeResults
	results["dry_run"] = dryRun
	results["total_ms"] = time.Since(start).Milliseconds()

	env := protocol.OK("index.sync.push", results, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	return protocol.Write(cmd.OutOrStdout(), env)
}

func runIndexSyncQuery(cmd *cobra.Command, remoteURL, remoteToken, query string, workspaces []string, limit int, global bool) error {
	ctx := cmd.Context()
	start := time.Now()

	// Get remote connection info
	if remoteURL == "" {
		remoteURL = os.Getenv("TURSO_DATABASE_URL")
	}
	if remoteToken == "" {
		remoteToken = os.Getenv("TURSO_AUTH_TOKEN")
	}
	if remoteURL == "" {
		return fmt.Errorf("remote Turso URL required: set TURSO_DATABASE_URL or use --remote-url")
	}
	if remoteToken == "" {
		return fmt.Errorf("remote Turso token required: set TURSO_AUTH_TOKEN or use --remote-token")
	}

	if !global && len(workspaces) == 0 {
		return fmt.Errorf("either --global or --workspaces is required")
	}

	// Load config for dimensions
	loadOpts := []config.Option{}
	if !global && len(workspaces) == 1 {
		loadOpts = append(loadOpts, config.WithWorkspacePath(workspaces[0]))
	}
	cfg, err := loadConfig(ctx, loadOpts...)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Generate query embedding
	provider, err := createIndexEmbeddingProviderForScope(cfg, "memory")
	if err != nil {
		return err
	}

	embedding, err := provider.Embed(ctx, query)
	if err != nil {
		return fmt.Errorf("generate query embedding: %w", err)
	}

	// Open remote Turso store (OpenTurso handles zero dimensions via GetDefaultVectorDimensions)
	store, err := memory.OpenTurso(ctx, dbdriver.TursoConfig{
		URL:                remoteURL,
		AuthToken:          remoteToken,
		EnableVectorSearch: true,
		VectorDimensions:   cfg.Embedding.Dimensions,
	})
	if err != nil {
		return fmt.Errorf("connect to remote Turso: %w", err)
	}
	defer store.Close()

	// Query based on scope
	var results []memory.ScoredEntry
	if global {
		results, err = store.SearchSimilarGlobal(ctx, embedding, limit)
	} else {
		results, err = store.SearchSimilarMultiWorkspace(ctx, workspaces, embedding, limit)
	}
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}

	// Format results
	formattedResults := make([]map[string]any, 0, len(results))
	for _, r := range results {
		formattedResults = append(formattedResults, map[string]any{
			"name":      r.Entry.Name,
			"workspace": r.Entry.Workspace,
			"type":      r.Entry.Type,
			"summary":   r.Entry.Summary,
			"score":     r.Score,
		})
	}

	data := map[string]any{
		"query":       query,
		"global":      global,
		"workspaces":  workspaces,
		"results":     formattedResults,
		"count":       len(results),
		"duration_ms": time.Since(start).Milliseconds(),
	}

	env := protocol.OK("index.sync.query", data, protocol.WithSource("cli"))
	return protocol.Write(cmd.OutOrStdout(), env)
}

func syncMemoryToTurso(ctx context.Context, cfg config.Config, workspace, remoteURL, remoteToken string, dryRun bool) (int, error) {
	// Open local configured store
	localStore, err := memory.OpenWithConfig(ctx, cfg)
	if err != nil {
		return 0, fmt.Errorf("open local store: %w", err)
	}
	defer localStore.Close()

	type embeddingStore interface {
		storage.MemoryStore
		GetEmbedding(context.Context, string, string) ([]float32, error)
		GetEmbeddingMetadata(context.Context, string) (*memory.EmbeddingMetadata, error)
	}
	embedStore, ok := localStore.(embeddingStore)
	if !ok {
		return 0, fmt.Errorf("configured memory store does not support embedding export")
	}

	// List memories with embeddings
	memories, err := localStore.List(ctx, workspace, 10000) // Get all
	if err != nil {
		return 0, fmt.Errorf("list memories: %w", err)
	}

	if dryRun {
		// Count memories with embeddings
		count := 0
		for _, mem := range memories {
			emb, err := embedStore.GetEmbedding(ctx, mem.Name, workspace)
			if err == nil && len(emb) > 0 {
				count++
			}
		}
		return count, nil
	}

	// Open remote Turso store (OpenTurso handles zero dimensions via GetDefaultVectorDimensions)
	remoteStore, err := memory.OpenTurso(ctx, dbdriver.TursoConfig{
		URL:                remoteURL,
		AuthToken:          remoteToken,
		EnableVectorSearch: true,
		VectorDimensions:   cfg.Embedding.Dimensions,
	})
	if err != nil {
		return 0, fmt.Errorf("open remote store: %w", err)
	}
	defer remoteStore.Close()

	// Get model from metadata
	model := "voyage-code-3" // default
	if meta, _ := embedStore.GetEmbeddingMetadata(ctx, workspace); meta != nil && meta.Model != "" {
		model = meta.Model
	}

	// Push each memory with embedding
	count := 0
	for _, mem := range memories {
		emb, err := embedStore.GetEmbedding(ctx, mem.Name, workspace)
		if err != nil || len(emb) == 0 {
			continue // Skip memories without embeddings
		}

		_, err = remoteStore.SaveWithEmbedding(ctx, mem, emb, model)
		if err != nil {
			observability.Emit(ctx, observability.NewEvent("index.turso_memory_skip").
				WithComponent(observability.ComponentCLI).
				WithData("memory", mem.Name).
				Error(err, 0))
			continue
		}
		count++
	}

	return count, nil
}

func syncSessionsToTurso(ctx context.Context, cfg config.Config, remoteURL, remoteToken string, dryRun bool) (int, error) {
	storageDir := filepath.Join(cfg.Home, "storage")

	// Open local sessions store
	localStore, err := sessions.Open(ctx, storageDir)
	if err != nil {
		return 0, fmt.Errorf("open local sessions store: %w", err)
	}
	defer localStore.Close()

	// List sessions
	opts := sessions.ListOptions{Limit: 10000}
	allSessions, err := localStore.List(ctx, opts)
	if err != nil {
		return 0, fmt.Errorf("list sessions: %w", err)
	}

	if dryRun {
		// Count sessions with embeddings
		count := 0
		for _, sess := range allSessions {
			if len(sess.Embedding) > 0 {
				count++
			}
		}
		return count, nil
	}

	// Open remote Turso sessions store (OpenTurso handles zero dimensions via GetDefaultVectorDimensions)
	remoteStore, err := sessions.OpenTurso(ctx, dbdriver.TursoConfig{
		URL:                remoteURL,
		AuthToken:          remoteToken,
		EnableVectorSearch: true,
		VectorDimensions:   cfg.Embedding.Dimensions,
	})
	if err != nil {
		return 0, fmt.Errorf("open remote sessions store: %w", err)
	}
	defer remoteStore.Close()

	// Push sessions with embeddings
	count := 0
	for _, sess := range allSessions {
		if len(sess.Embedding) == 0 {
			continue
		}

		// Save session to remote (includes embedding via Session struct)
		if _, err := remoteStore.Save(ctx, sess); err != nil {
			observability.Emit(ctx, observability.NewEvent("index.turso_session_skip").
				WithComponent(observability.ComponentCLI).
				WithData("session_id", sess.ID).
				Error(err, 0))
			continue
		}
		count++
	}

	return count, nil
}

func newIndexGitDiffCommand() *cobra.Command {
	var workspace string
	var base string
	var head string
	var dryRun bool
	var embed bool

	cmd := &cobra.Command{
		Use:   "git-diff",
		Short: "Index files changed between git commits (e.g., after git pull)",
		Long: `Index files that changed between two git commits.

This is designed to run after git operations like pull, merge, or rebase
to incrementally update the symbol index with only the changed files.

The default base is "auto" which prefers the last successfully indexed HEAD SHA,
then falls back to ORIG_HEAD (post-merge/pull), then HEAD~1.
This can be run from a git post-merge hook for automatic incremental indexing.

Git hook setup (optional):
  echo '#!/bin/sh
  foxctl index git-diff' > .git/hooks/post-merge
  chmod +x .git/hooks/post-merge`,
		Example: `  # Index files changed by last pull/merge
  foxctl index git-diff

  # Index files changed in last 3 commits
  foxctl index git-diff --base HEAD~3

  # Index specific commit range
  foxctl index git-diff --base abc123 --head def456

  # Dry run to see what would be indexed
  foxctl index git-diff --dry-run`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runIndexGitDiff(cmd, workspace, base, head, dryRun, embed)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root directory")
	cmd.Flags().StringVar(&base, "base", "auto", "Base commit (auto: last indexed HEAD, else ORIG_HEAD, else HEAD~1)")
	cmd.Flags().StringVar(&head, "head", "HEAD", "Head commit")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be indexed without making changes")
	cmd.Flags().BoolVar(&embed, "embed", false, "Also queue embeddings for changed files")

	return cmd
}

func runIndexGitDiff(cmd *cobra.Command, workspace, base, head string, dryRun, embed bool) error {
	ctx := cmd.Context()
	start := time.Now()

	// Resolve workspace
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return fmt.Errorf("resolve workspace: %w", err)
	}

	workspaceID := workspaceutil.ID(absWorkspace)

	// Load config (needed to access the local index state store).
	cfg, err := loadConfig(ctx, config.WithWorkspacePath(absWorkspace))
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	store, err := memory.OpenWithConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("open memory store: %w", err)
	}
	defer store.Close()

	type indexerStateStore interface {
		storage.MemoryStore
		GetIndexerState(context.Context, string, string) (memory.IndexerState, bool, error)
		SetLastIndexedHeadSHA(context.Context, string, string, string) (memory.IndexerState, error)
	}
	indexStateStore, ok := store.(indexerStateStore)
	if !ok {
		return fmt.Errorf("configured memory store does not support indexer state")
	}

	// Resolve base commit:
	// - "auto" prefers the last successfully indexed HEAD SHA (per workspace+indexer)
	// - then falls back to ORIG_HEAD (post-merge/pull)
	// - then HEAD~1 (handled in getGitDiffFileChanges)
	baseInput := base
	baseResolved := base
	baseSource := "input"

	lastIndexedHeadSHA := ""
	if st, ok, stateErr := indexStateStore.GetIndexerState(ctx, workspaceID, symbol.IndexerID); stateErr == nil && ok {
		lastIndexedHeadSHA = strings.TrimSpace(st.LastIndexedHeadSHA)
	}

	if isGitDiffAutoBase(baseResolved) {
		if lastIndexedHeadSHA != "" && gitCommitExists(ctx, absWorkspace, lastIndexedHeadSHA) {
			baseResolved = lastIndexedHeadSHA
			baseSource = "last_indexed_head_sha"
		} else {
			baseResolved = "ORIG_HEAD"
			baseSource = "ORIG_HEAD"
		}
	}

	// Match getGitDiffFileChanges behavior so the resolved base is visible in outputs.
	if baseResolved == "ORIG_HEAD" && !gitCommitExists(ctx, absWorkspace, "ORIG_HEAD") {
		baseResolved = "HEAD~1"
		baseSource = "HEAD~1"
	}

	headSHA, headSHAErr := gitRevParse(ctx, absWorkspace, head)
	if headSHAErr != nil {
		// Best-effort: indexing will fail if we're not in a git repo.
		// We include headSHAErr in the output when possible.
		headSHA = ""
	}

	// Get changed files from git.
	changedFiles, err := getGitDiffFileChanges(ctx, absWorkspace, baseResolved, head)
	if err != nil {
		return fmt.Errorf("get git diff: %w", err)
	}

	// Filter to supported file types
	supportedExts := map[string]bool{
		".go": true, ".py": true, ".js": true, ".jsx": true, ".ts": true, ".tsx": true,
		".ex": true, ".exs": true,
	}
	var filesToIndex []indexing.FileChange
	for _, f := range changedFiles {
		ext := strings.ToLower(filepath.Ext(f.Path))
		if supportedExts[ext] {
			filesToIndex = append(filesToIndex, f)
		}
	}

	if dryRun {
		data := map[string]any{
			"dry_run":               true,
			"workspace":             absWorkspace,
			"workspace_id":          workspaceID,
			"base":                  baseResolved,
			"base_input":            baseInput,
			"base_source":           baseSource,
			"head":                  head,
			"head_sha":              headSHA,
			"head_sha_error":        headSHAErrString(headSHAErr),
			"indexer_id":            symbol.IndexerID,
			"last_indexed_head_sha": lastIndexedHeadSHA,
			"changed_files":         len(changedFiles),
			"files_to_index":        len(filesToIndex),
			"embed":                 embed,
			"files":                 filesToIndex,
		}
		env := protocol.OK("index.git-diff", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		return protocol.Write(cmd.OutOrStdout(), env)
	}

	if len(filesToIndex) == 0 {
		stateUpdated := false
		storedLastIndexedHeadSHA := ""
		stateErr := ""
		if headSHA != "" {
			state, err := indexStateStore.SetLastIndexedHeadSHA(ctx, workspaceID, symbol.IndexerID, headSHA)
			if err != nil {
				stateErr = err.Error()
			} else {
				stateUpdated = true
				storedLastIndexedHeadSHA = state.LastIndexedHeadSHA
			}
		}
		data := map[string]any{
			"workspace":                    absWorkspace,
			"workspace_id":                 workspaceID,
			"base":                         baseResolved,
			"base_input":                   baseInput,
			"base_source":                  baseSource,
			"head":                         head,
			"head_sha":                     headSHA,
			"head_sha_error":               headSHAErrString(headSHAErr),
			"indexer_id":                   symbol.IndexerID,
			"last_indexed_head_sha":        lastIndexedHeadSHA,
			"changed_files":                len(changedFiles),
			"files_to_index":               0,
			"indexed":                      0,
			"message":                      "no supported files to index",
			"state_updated":                stateUpdated,
			"state_error":                  stateErr,
			"stored_last_indexed_head_sha": storedLastIndexedHeadSHA,
			"duration_ms":                  time.Since(start).Milliseconds(),
		}
		env := protocol.OK("index.git-diff", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		return protocol.Write(cmd.OutOrStdout(), env)
	}

	symCfg := symbol.DefaultConfig()
	symCfg.Enabled = true
	symCfg.EmbeddingEnabled = embed
	if embed {
		symCfg.EmbeddingTextMode = config.EmbedSymbolTextModeDocEnriched
	}

	// TODO: Migrate indexers to use observability instead of zerolog.
	logger := zerolog.New(os.Stderr).With().Timestamp().Logger() //nolint:forbidigo // indexers require zerolog
	indexer := symbol.NewIndexer(symCfg, store, nil, absWorkspace, logger)

	now := time.Now().UTC()
	event := indexing.PostReviewEvent{
		ID:            ulid.Make().String(),
		WorkspaceID:   workspaceID,
		ReviewKind:    "manual",
		ReviewStatus:  "ok",
		DiffAppliedAt: now,
		Files:         filesToIndex,
		Source:        "cli",
		Metadata: map[string]any{
			"base":        baseResolved,
			"base_input":  baseInput,
			"base_source": baseSource,
			"head":        head,
		},
		CreatedAt: now,
		Reason:    "git-diff",
	}

	res, err := indexer.Index(ctx, event)
	if err != nil {
		return err
	}

	stateUpdated := false
	storedLastIndexedHeadSHA := ""
	stateErr := ""
	if res.FilesFailed == 0 && headSHA != "" {
		state, err := indexStateStore.SetLastIndexedHeadSHA(ctx, workspaceID, symbol.IndexerID, headSHA)
		if err != nil {
			stateErr = err.Error()
		} else {
			stateUpdated = true
			storedLastIndexedHeadSHA = state.LastIndexedHeadSHA
		}
	}

	data := map[string]any{
		"workspace":                    absWorkspace,
		"workspace_id":                 event.WorkspaceID,
		"base":                         baseResolved,
		"base_input":                   baseInput,
		"base_source":                  baseSource,
		"head":                         head,
		"head_sha":                     headSHA,
		"head_sha_error":               headSHAErrString(headSHAErr),
		"indexer_id":                   symbol.IndexerID,
		"last_indexed_head_sha":        lastIndexedHeadSHA,
		"changed_files":                len(changedFiles),
		"files_to_index":               len(filesToIndex),
		"indexed":                      res.FilesIndexed,
		"skipped":                      res.FilesSkipped,
		"failed":                       res.FilesFailed,
		"failures":                     res.Failures,
		"embed":                        embed,
		"state_updated":                stateUpdated,
		"state_error":                  stateErr,
		"stored_last_indexed_head_sha": storedLastIndexedHeadSHA,
		"duration_ms":                  time.Since(start).Milliseconds(),
	}

	env := protocol.OK("index.git-diff", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	return protocol.Write(cmd.OutOrStdout(), env)
}

// getGitDiffFileChanges returns the list of files changed between base and head commits.
//
// Index:
//
//	Purpose: Translate a git diff range into (path, change_kind) entries for post-review indexers
//	Keywords: git-diff, name-status, ORIG_HEAD, head, base, rename, deleted, added, modified
//	Related: runIndexGitDiff, indexing.FileChange, indexing.ChangeKind
//	Flow: resolve ORIG_HEAD fallback -> run git diff --name-status -> parse statuses -> expand renames -> return file changes
//	Resources: git subprocess
//	Events: git-diff-name-status
//	OutputFields: path, change_kind
//
// [[invariant:diff-range-must-resolve-to-file-changes]]
// [[risk:stale-orig-head-fallback]]
func getGitDiffFileChanges(ctx context.Context, repoPath, base, head string) ([]indexing.FileChange, error) {
	// First check if ORIG_HEAD exists (it won't if no merge/pull has happened)
	if base == "ORIG_HEAD" {
		checkCmd := exec.CommandContext(ctx, "git", "-C", repoPath, "rev-parse", "--verify", "ORIG_HEAD")
		if err := checkCmd.Run(); err != nil {
			// ORIG_HEAD doesn't exist, fall back to HEAD~1
			base = "HEAD~1"
		}
	}

	// Get list of changed files
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "diff", "--name-status", base+".."+head)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// If the range doesn't work, try without the range (unstaged changes)
		cmd2 := exec.CommandContext(ctx, "git", "-C", repoPath, "diff", "--name-status")
		var stdout2 bytes.Buffer
		cmd2.Stdout = &stdout2
		if err2 := cmd2.Run(); err2 != nil {
			return nil, fmt.Errorf("git diff failed: %w (stderr: %s)", err, stderr.String())
		}
		stdout = stdout2
	}

	out := make([]indexing.FileChange, 0, 64)
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}

		status := strings.TrimSpace(parts[0])
		switch {
		case strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C"):
			// rename/copy: status \t old \t new
			if len(parts) < 3 {
				continue
			}
			oldPath := strings.TrimSpace(parts[1])
			newPath := strings.TrimSpace(parts[2])
			if oldPath != "" {
				out = append(out, indexing.FileChange{Path: oldPath, ChangeKind: indexing.ChangeKindDeleted})
			}
			if newPath != "" {
				out = append(out, indexing.FileChange{Path: newPath, ChangeKind: indexing.ChangeKindAdded})
			}
		case status == "A":
			out = append(out, indexing.FileChange{Path: strings.TrimSpace(parts[1]), ChangeKind: indexing.ChangeKindAdded})
		case status == "D":
			out = append(out, indexing.FileChange{Path: strings.TrimSpace(parts[1]), ChangeKind: indexing.ChangeKindDeleted})
		default:
			// Treat everything else as modified (M, T, etc).
			out = append(out, indexing.FileChange{Path: strings.TrimSpace(parts[1]), ChangeKind: indexing.ChangeKindModified})
		}
	}
	return out, nil
}

// isGitDiffAutoBase reports whether the base argument should be auto-resolved.
//
// Index:
//
//	Purpose: Allow `index git-diff` to default to "diff since last successful indexing" without requiring explicit SHAs
//	Keywords: git-diff, base, auto, last-indexed, last_indexed
//	Related: runIndexGitDiff, memory.Store.GetIndexerState
//	Flow: normalize token -> compare against known auto markers
//
// [[domain:git-diff-auto-base]]
func isGitDiffAutoBase(base string) bool {
	base = strings.TrimSpace(strings.ToLower(base))
	switch base {
	case "", "auto", "last-indexed", "last_indexed", "lastindexed":
		return true
	default:
		return false
	}
}

// gitCommitExists reports whether rev resolves to a commit object.
//
// Index:
//
//	Purpose: Avoid using stale "last indexed" SHAs that aren't present in the current clone (shallow clones, rewritten history)
//	Keywords: git, commit, cat-file, verify, rev, exists
//	Related: runIndexGitDiff, gitRevParse
//	Flow: normalize rev -> run `git cat-file -e <rev>^{commit}` -> return status
//	Resources: git subprocess
//
// [[invariant:commit-existence-must-be-verified-before-indexing]]
// [[risk:shallow-clone-missing-sha]]
func gitCommitExists(ctx context.Context, repoPath, rev string) bool {
	rev = strings.TrimSpace(rev)
	if rev == "" {
		return false
	}
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "cat-file", "-e", rev+"^{commit}")
	return cmd.Run() == nil
}

// gitRevParse resolves a git revision (HEAD, ref name, or SHA) to a full commit SHA.
//
// Index:
//
//	Purpose: Persist a stable last-indexed anchor even when `--head` is a ref name like HEAD or main
//	Keywords: git, rev-parse, sha, head, refs
//	Related: runIndexGitDiff, gitCommitExists
//	Flow: normalize rev -> run `git rev-parse <rev>` -> trim output
//	Resources: git subprocess
//	OutputFields: full_commit_sha
//
// [[invariant:last-indexed-anchor-must-be-full-sha]]
// [[risk:ref-name-instability]]
func gitRevParse(ctx context.Context, repoPath, rev string) (string, error) {
	rev = strings.TrimSpace(rev)
	if rev == "" {
		return "", fmt.Errorf("git rev-parse: rev is required")
	}
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "rev-parse", rev)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git rev-parse %q failed: %w (stderr: %s)", rev, err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

func headSHAErrString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func newIndexFileSummariesCommand() *cobra.Command {
	var workspace string
	var force bool
	var dryRun bool
	var batchSize int
	var glob string
	var exclude []string

	cmd := &cobra.Command{
		Use:   "file-summaries",
		Short: "Generate LLM-powered file summaries for semantic search",
		Long: `Generate short, search-friendly file summaries using an LLM.

These summaries describe what each source file does and are used by
semantic search to provide context about files in the codebase.
Summaries are cached by symbol hash and only regenerated when exported
symbols change (not comments or implementation details).

Automatically respects .gitignore - files in node_modules, vendor,
.git, and other ignored directories are skipped.

Requires an LLM provider (Devstral via OpenRouter, Cerebras, or Groq).
Set OPENROUTER_API_KEY, CEREBRAS_API_KEY, or GROQ_API_KEY.

Provider priority:
  1. Devstral via OpenRouter (best for code summaries)
  2. Cerebras (fast)
  3. Groq (fast)`,
		Example: `  # Generate file summaries for files missing them
  foxctl index file-summaries

  # Force regenerate all file summaries
  foxctl index file-summaries --force

  # Process specific file types
  foxctl index file-summaries --glob '**/*.go'

  # Dry run to see what would be processed
  foxctl index file-summaries --dry-run`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runIndexSummaries(cmd, workspace, force, dryRun, batchSize, glob, exclude)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root directory")
	cmd.Flags().BoolVar(&force, "force", false, "Force regenerate all summaries (ignore cache)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be processed without making changes")
	cmd.Flags().IntVar(&batchSize, "batch-size", 50, "Number of files to process per batch")
	cmd.Flags().StringVar(&glob, "glob", "**/*.{go,ts,tsx,js,jsx,py,rs,java}", "Glob pattern for files to process")
	cmd.Flags().StringSliceVar(&exclude, "exclude", []string{"*_test.go"}, "Glob patterns to exclude (gitignore is respected automatically)")

	return cmd
}

func newIndexSymbolSummariesCommand() *cobra.Command {
	var workspace string
	var force bool
	var dryRun bool
	var useLLM bool
	var batchSize int

	cmd := &cobra.Command{
		Use:   "symbol-summaries",
		Short: "Generate symbol summaries for repo navigation",
		Long: `Generate short, search-friendly symbol summaries for repoindex.

These summaries describe what each indexed symbol does and are used by
repoindex to annotate symbol nodes for faster navigation. Summaries are
cached by symbol digest and only regenerated when symbol metadata changes.

By default, summaries are deterministic (doc comment + signature).
Use --llm to enable LLM-generated summaries.

Requires symbol index entries (run "foxctl index init --scope symbols" first if missing).

If --llm is set, requires an LLM provider (Devstral via OpenRouter, Cerebras, or Groq).
Set OPENROUTER_API_KEY, CEREBRAS_API_KEY, or GROQ_API_KEY.

Provider priority:
  1. Devstral via OpenRouter (best for code summaries)
  2. Cerebras (fast)
  3. Groq (fast)`,
		Example: `  # Generate deterministic symbol summaries
  foxctl index symbol-summaries

  # Force regenerate all symbol summaries
  foxctl index symbol-summaries --force

  # Enable LLM summaries
  foxctl index symbol-summaries --llm

  # Dry run to see what would be processed
  foxctl index symbol-summaries --dry-run`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runIndexSymbolSummaries(cmd, workspace, force, dryRun, useLLM, batchSize)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root directory")
	cmd.Flags().BoolVar(&force, "force", false, "Force regenerate all summaries (ignore cache)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be processed without making changes")
	cmd.Flags().BoolVar(&useLLM, "llm", false, "Use LLM for summaries (default: deterministic)")
	cmd.Flags().IntVar(&batchSize, "batch-size", 200, "Number of symbols to process per batch")

	return cmd
}

func runIndexSummaries(cmd *cobra.Command, workspace string, force, dryRun bool, batchSize int, glob string, exclude []string) (err error) {
	ctx := cmd.Context()
	start := time.Now()
	var summaryEvent *observability.EventBuilder
	var filesFound int
	var filesSkipped int
	var filesToProcessCount int
	var processedCount int
	var errorsCount int
	var providerName string
	var modelName string

	defer func() {
		if summaryEvent == nil {
			return
		}
		summaryEvent.WithDataMap(map[string]any{
			"files_found":      filesFound,
			"files_to_process": filesToProcessCount,
			"files_skipped":    filesSkipped,
			"processed":        processedCount,
			"errors":           errorsCount,
			"force":            force,
			"dry_run":          dryRun,
			"batch_size":       batchSize,
			"glob":             glob,
			"provider":         providerName,
			"model":            modelName,
		})
		if err != nil {
			if emitErr := observability.EmitSync(ctx, summaryEvent.Error(err, time.Since(start))); emitErr != nil {
				fmt.Fprintf(os.Stderr, "observability emit failed: %v\n", emitErr) //nolint:forbidigo // fallback for obs emit failures
			}
			return
		}
		if emitErr := observability.EmitSync(ctx, summaryEvent.Success(time.Since(start))); emitErr != nil {
			fmt.Fprintf(os.Stderr, "observability emit failed: %v\n", emitErr) //nolint:forbidigo // fallback for obs emit failures
		}
	}()

	// Validate batchSize to prevent infinite loop
	if batchSize <= 0 {
		return fmt.Errorf("batch-size must be positive, got %d", batchSize)
	}

	// Resolve workspace
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return fmt.Errorf("resolve workspace: %w", err)
	}

	summaryEvent = observability.NewEvent("index.file_summaries").
		WithComponent(observability.ComponentCLI).
		WithCommand("index.file-summaries").
		WithWorkspace(absWorkspace).
		EnrichFromEnv().
		EnrichFromContext(ctx)

	// Load config
	cfg, err := loadConfig(ctx, config.WithWorkspacePath(absWorkspace))
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Find files to process (respects .gitignore)
	files, err := fsutil.FindFilesRespectingGitignore(absWorkspace, glob, exclude)
	if err != nil {
		return fmt.Errorf("find files: %w", err)
	}

	filesFound = len(files)

	if len(files) == 0 {
		data := map[string]any{
			"workspace":   absWorkspace,
			"files_found": 0,
			"message":     "no files matched the glob pattern",
		}
		env := protocol.OK("index.file-summaries", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		return protocol.Write(cmd.OutOrStdout(), env)
	}

	// Open memory store
	store, err := memory.OpenWithConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("open memory store: %w", err)
	}
	defer store.Close()

	// Filter to files needing summaries (unless force)
	var filesToProcess []string
	var skippedCount int

	for _, f := range files {
		if !force {
			// Check if summary exists and is current
			entryName := symbol.FileSummaryEntryName(absWorkspace, f)
			entry, err := store.Get(ctx, entryName, absWorkspace)
			if err == nil {
				// Summary exists - check if digest matches
				var result symbol.FileSummaryResult
				if err := json.Unmarshal(entry.Result, &result); err == nil {
					// Build input to compute current digest
					input, inputErr := buildFileSummaryInput(absWorkspace, f)
					if inputErr == nil {
						currentDigest := symbol.ComputeFileSummaryDigest(input)
						if result.Digest == currentDigest {
							skippedCount++
							continue // Summary is current
						}
					}
					// If input error, fall through to process the file (will error during processing)
				}
			}
		}
		filesToProcess = append(filesToProcess, f)
	}

	filesSkipped = skippedCount
	filesToProcessCount = len(filesToProcess)

	if dryRun {
		data := map[string]any{
			"dry_run":          true,
			"workspace":        absWorkspace,
			"files_found":      len(files),
			"files_to_process": len(filesToProcess),
			"files_skipped":    skippedCount,
			"force":            force,
			"files":            filesToProcess,
		}
		env := protocol.OK("index.file-summaries", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		return protocol.Write(cmd.OutOrStdout(), env)
	}

	if len(filesToProcess) == 0 {
		data := map[string]any{
			"workspace":     absWorkspace,
			"files_found":   len(files),
			"files_skipped": skippedCount,
			"processed":     0,
			"message":       "all files already have current summaries",
			"duration_ms":   time.Since(start).Milliseconds(),
		}
		env := protocol.OK("index.file-summaries", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		return protocol.Write(cmd.OutOrStdout(), env)
	}

	// Get LLM provider
	providers := llmproviders.FileSummaryProviders()
	if len(providers) == 0 {
		return fmt.Errorf("no LLM providers available - set OPENROUTER_API_KEY, CEREBRAS_API_KEY, or GROQ_API_KEY")
	}

	providerName = providers[0].Name
	modelName = providers[0].Model

	llm := llmproviders.NewSummaryLLM(providers[0])

	observability.Emit(ctx, observability.NewEvent("index.summary_llm_selected").
		WithComponent(observability.ComponentCLI).
		WithData("provider", providers[0].Name).
		WithData("model", providers[0].Model).
		Success(0))
	observability.Emit(ctx, observability.NewEvent("index.summary_files_processing").
		WithComponent(observability.ComponentCLI).
		WithData("files_to_process", len(filesToProcess)).
		WithData("files_skipped", skippedCount).
		Success(0))

	// Get optional embedding provider
	embedProvider := maybeIndexEmbeddingProviderForScope(cfg, "file_summaries")

	// Create summary generator
	generator := filesummary.NewFileSummaryGenerator(store, llm, embedProvider, absWorkspace)

	// Process files in batches
	var processed, errors int
	results := make([]map[string]any, 0)

	for i := 0; i < len(filesToProcess); i += batchSize {
		end := i + batchSize
		if end > len(filesToProcess) {
			end = len(filesToProcess)
		}
		batch := filesToProcess[i:end]

		observability.Emit(ctx, observability.NewEvent("index.summary_batch_start").
			WithComponent(observability.ComponentCLI).
			WithData("batch_start", i+1).
			WithData("batch_end", end).
			WithData("total_files", len(filesToProcess)).
			Success(0))

		for _, f := range batch {
			fileStart := time.Now()
			fileEvent := observability.NewEvent("index.file_summaries").
				WithComponent(observability.ComponentCLI).
				WithCommand("index.file-summaries").
				WithSubtype("file").
				WithWorkspace(absWorkspace).
				WithData("path", f).
				EnrichFromEnv().
				EnrichFromContext(ctx)

			input, inputErr := buildFileSummaryInput(absWorkspace, f)
			if inputErr != nil {
				results = append(results, map[string]any{
					"file":   f,
					"status": "error",
					"error":  inputErr.Error(),
				})
				errors++
				fileEvent.WithData("status", "error")
				observability.Emit(ctx, fileEvent.Error(inputErr, time.Since(fileStart)))
				continue
			}

			summary, cached, err := generator.GetOrCreateSummary(ctx, input)
			if err != nil {
				results = append(results, map[string]any{
					"file":   f,
					"status": "error",
					"error":  err.Error(),
				})
				errors++
				fileEvent.WithData("status", "error")
				observability.Emit(ctx, fileEvent.Error(err, time.Since(fileStart)))
				continue
			}

			if !cached {
				results = append(results, map[string]any{
					"file":    f,
					"status":  "generated",
					"summary": summary,
				})
				processed++
				fileEvent.WithData("status", "generated").WithData("cached", false)
				observability.Emit(ctx, fileEvent.Success(time.Since(fileStart)))
			} else {
				results = append(results, map[string]any{
					"file":   f,
					"status": "cached",
				})
				fileEvent.WithData("status", "cached").WithData("cached", true)
				observability.Emit(ctx, fileEvent.Success(time.Since(fileStart)))
			}
		}
	}

	processedCount = processed
	errorsCount = errors

	data := map[string]any{
		"workspace":     absWorkspace,
		"provider":      providers[0].Name,
		"model":         providers[0].Model,
		"files_found":   len(files),
		"files_skipped": skippedCount,
		"processed":     processed,
		"errors":        errors,
		"force":         force,
		"results":       results,
		"duration_ms":   time.Since(start).Milliseconds(),
	}

	env := protocol.OK("index.file-summaries", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	return protocol.Write(cmd.OutOrStdout(), env)
}

func runIndexSymbolSummaries(cmd *cobra.Command, workspace string, force, dryRun, useLLM bool, batchSize int) (err error) {
	ctx := cmd.Context()
	start := time.Now()
	var summaryEvent *observability.EventBuilder
	var symbolsFound int
	var symbolsSkipped int
	var symbolsToProcessCount int
	var processedCount int
	var errorsCount int
	var providerName string
	var modelName string
	var llm filesummary.SummaryLLM

	defer func() {
		if summaryEvent == nil {
			return
		}
		summaryEvent.WithDataMap(map[string]any{
			"symbols_found":      symbolsFound,
			"symbols_to_process": symbolsToProcessCount,
			"symbols_skipped":    symbolsSkipped,
			"processed":          processedCount,
			"errors":             errorsCount,
			"force":              force,
			"dry_run":            dryRun,
			"batch_size":         batchSize,
			"llm_enabled":        useLLM,
			"provider":           providerName,
			"model":              modelName,
		})
		if err != nil {
			if emitErr := observability.EmitSync(ctx, summaryEvent.Error(err, time.Since(start))); emitErr != nil {
				fmt.Fprintf(os.Stderr, "observability emit failed: %v\n", emitErr) //nolint:forbidigo // fallback for obs emit failures
			}
			return
		}
		if emitErr := observability.EmitSync(ctx, summaryEvent.Success(time.Since(start))); emitErr != nil {
			fmt.Fprintf(os.Stderr, "observability emit failed: %v\n", emitErr) //nolint:forbidigo // fallback for obs emit failures
		}
	}()

	if batchSize <= 0 {
		return fmt.Errorf("batch-size must be positive, got %d", batchSize)
	}

	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return fmt.Errorf("resolve workspace: %w", err)
	}

	summaryEvent = observability.NewEvent("index.symbol_summaries").
		WithComponent(observability.ComponentCLI).
		WithCommand("index.symbol-summaries").
		WithWorkspace(absWorkspace).
		EnrichFromEnv().
		EnrichFromContext(ctx)

	cfg, err := loadConfig(ctx, config.WithWorkspacePath(absWorkspace))
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	store, err := memory.OpenWithConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("open memory store: %w", err)
	}
	defer store.Close()

	filter := memory.ListFilter{Types: []string{symbol.SymbolType}}
	var inputs []symbol.SymbolSummaryInput
	var skippedCount int
	results := make([]map[string]any, 0)

	offset := 0
	total := 0
	for {
		entries, totalCount, listErr := store.ListFiltered(ctx, absWorkspace, filter, batchSize, offset)
		if listErr != nil {
			return fmt.Errorf("list symbols: %w", listErr)
		}
		if total == 0 {
			total = totalCount
			symbolsFound = totalCount
		}
		if len(entries) == 0 {
			break
		}
		for _, entry := range entries {
			result, parseErr := symbol.UnmarshalResult(entry.Result)
			if parseErr != nil {
				errorsCount++
				results = append(results, map[string]any{
					"symbol": entry.Name,
					"status": "error",
					"error":  parseErr.Error(),
				})
				continue
			}
			sym := result.Symbol
			if sym.Kind == symbol.KindFileSummary {
				skippedCount++
				continue
			}
			input, inputErr := buildSymbolSummaryInput(sym)
			if inputErr != nil {
				errorsCount++
				results = append(results, map[string]any{
					"symbol_id": sym.ID,
					"status":    "error",
					"error":     inputErr.Error(),
				})
				continue
			}

			if !force {
				entryFound := false
				if sym.Key != "" {
					pkg := symbolutil.DeriveSymbolPackage(sym.FilePath, sym.Language)
					keyEntryName := symbol.SymbolSummaryKeyEntryName(absWorkspace, pkg, sym.Key.String())
					if entry, getErr := store.Get(ctx, keyEntryName, absWorkspace); getErr == nil {
						var cached symbol.SymbolSummaryResult
						if err := json.Unmarshal(entry.Result, &cached); err == nil {
							currentDigest := symbol.ComputeSymbolSummaryDigest(input)
							if cached.Digest == currentDigest {
								skippedCount++
								continue
							}
						}
						entryFound = true
					}
				}
				if !entryFound {
					entryName := symbol.SymbolSummaryEntryName(absWorkspace, sym.ID)
					entry, getErr := store.Get(ctx, entryName, absWorkspace)
					if getErr == nil {
						var cached symbol.SymbolSummaryResult
						if err := json.Unmarshal(entry.Result, &cached); err == nil {
							currentDigest := symbol.ComputeSymbolSummaryDigest(input)
							if cached.Digest == currentDigest {
								skippedCount++
								continue
							}
						}
					}
				}
			}

			inputs = append(inputs, input)
		}
		offset += len(entries)
		if total > 0 && offset >= total {
			break
		}
	}

	symbolsSkipped = skippedCount
	symbolsToProcessCount = len(inputs)

	if symbolsFound == 0 {
		data := map[string]any{
			"workspace":     absWorkspace,
			"symbols_found": 0,
			"message":       "no symbols found - run symbol indexing first",
		}
		env := protocol.OK("index.symbol-summaries", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		return protocol.Write(cmd.OutOrStdout(), env)
	}

	if dryRun {
		items := make([]map[string]string, 0, len(inputs))
		for _, input := range inputs {
			items = append(items, map[string]string{
				"symbol_id": input.SymbolID,
				"name":      input.Name,
				"file":      input.FilePath,
			})
		}
		data := map[string]any{
			"dry_run":            true,
			"workspace":          absWorkspace,
			"symbols_found":      symbolsFound,
			"symbols_to_process": len(inputs),
			"symbols_skipped":    skippedCount,
			"force":              force,
			"symbols":            items,
		}
		env := protocol.OK("index.symbol-summaries", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		return protocol.Write(cmd.OutOrStdout(), env)
	}

	if len(inputs) == 0 {
		data := map[string]any{
			"workspace":       absWorkspace,
			"symbols_found":   symbolsFound,
			"symbols_skipped": skippedCount,
			"processed":       0,
			"message":         "all symbols already have current summaries",
			"duration_ms":     time.Since(start).Milliseconds(),
		}
		env := protocol.OK("index.symbol-summaries", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		return protocol.Write(cmd.OutOrStdout(), env)
	}

	if useLLM {
		providers := llmproviders.FileSummaryProviders()
		if len(providers) == 0 {
			return fmt.Errorf("no LLM providers available - set OPENROUTER_API_KEY, CEREBRAS_API_KEY, or GROQ_API_KEY")
		}

		providerName = providers[0].Name
		modelName = providers[0].Model

		llm = llmproviders.NewSummaryLLM(providers[0])

		observability.Emit(ctx, observability.NewEvent("index.symbol_llm_selected").
			WithComponent(observability.ComponentCLI).
			WithData("provider", providerName).
			WithData("model", modelName).
			Success(0))
	} else {
		providerName = "deterministic"
		modelName = "fallback"
		observability.Emit(ctx, observability.NewEvent("index.symbol_deterministic_mode").
			WithComponent(observability.ComponentCLI).
			Success(0))
	}

	observability.Emit(ctx, observability.NewEvent("index.symbol_processing_start").
		WithComponent(observability.ComponentCLI).
		WithData("symbols_to_process", len(inputs)).
		WithData("symbols_skipped", skippedCount).
		Success(0))

	embedProvider := maybeIndexEmbeddingProviderForScope(cfg, "symbols")

	generator := symbol.NewSymbolSummaryGenerator(store, llm, embedProvider, absWorkspace)

	var processed, errors int
	for i := 0; i < len(inputs); i += batchSize {
		end := i + batchSize
		if end > len(inputs) {
			end = len(inputs)
		}
		batch := inputs[i:end]

		observability.Emit(ctx, observability.NewEvent("index.symbol_batch_start").
			WithComponent(observability.ComponentCLI).
			WithData("batch_start", i+1).
			WithData("batch_end", end).
			WithData("total_symbols", len(inputs)).
			Success(0))

		for _, input := range batch {
			symbolStart := time.Now()
			symbolEvent := observability.NewEvent("index.symbol_summaries").
				WithComponent(observability.ComponentCLI).
				WithCommand("index.symbol-summaries").
				WithSubtype("symbol").
				WithWorkspace(absWorkspace).
				WithData("symbol_id", input.SymbolID).
				WithData("file", input.FilePath).
				EnrichFromEnv().
				EnrichFromContext(ctx)

			summary, cached, genErr := generator.GetOrCreateSummary(ctx, input)
			if genErr != nil {
				results = append(results, map[string]any{
					"symbol_id": input.SymbolID,
					"status":    "error",
					"error":     genErr.Error(),
				})
				errors++
				symbolEvent.WithData("status", "error")
				observability.Emit(ctx, symbolEvent.Error(genErr, time.Since(symbolStart)))
				continue
			}

			if !cached {
				results = append(results, map[string]any{
					"symbol_id": input.SymbolID,
					"status":    "generated",
					"summary":   summary,
				})
				processed++
				symbolEvent.WithData("status", "generated").WithData("cached", false)
				observability.Emit(ctx, symbolEvent.Success(time.Since(symbolStart)))
			} else {
				results = append(results, map[string]any{
					"symbol_id": input.SymbolID,
					"status":    "cached",
				})
				symbolEvent.WithData("status", "cached").WithData("cached", true)
				observability.Emit(ctx, symbolEvent.Success(time.Since(symbolStart)))
			}
		}
	}

	processedCount = processed
	errorsCount += errors

	data := map[string]any{
		"workspace":       absWorkspace,
		"provider":        providerName,
		"model":           modelName,
		"llm_enabled":     useLLM,
		"symbols_found":   symbolsFound,
		"symbols_skipped": skippedCount,
		"processed":       processed,
		"errors":          errorsCount,
		"force":           force,
		"results":         results,
		"duration_ms":     time.Since(start).Milliseconds(),
	}

	env := protocol.OK("index.symbol-summaries", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	return protocol.Write(cmd.OutOrStdout(), env)
}

// buildFileSummaryInput builds a FileSummaryInput from a file path.
func buildFileSummaryInput(workspace, relPath string) (symbol.FileSummaryInput, error) {
	fullPath := filepath.Join(workspace, relPath)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return symbol.FileSummaryInput{}, fmt.Errorf("read file %s: %w", relPath, err)
	}

	input := symbol.FileSummaryInput{
		FilePath:    relPath,
		SymbolsHash: symbol.ComputeSymbolsHash(content, relPath), // Hash symbols for cache invalidation
	}

	// Extract package name for Go files
	if strings.HasSuffix(relPath, ".go") {
		lines := strings.Split(string(content), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "package ") {
				input.Package = strings.TrimPrefix(line, "package ")
				break
			}
		}
	}

	// Extract first comment block (simplified)
	input.FirstComment = extractFirstCommentForSummary(string(content))

	// Extract top symbols (simplified)
	input.TopSymbols = extractTopSymbolsForSummary(string(content), relPath)

	return symbol.NormalizeFileSummaryInput(input), nil
}

func buildSymbolSummaryInput(sym symbol.Symbol) (symbol.SymbolSummaryInput, error) {
	if sym.ID == "" {
		return symbol.SymbolSummaryInput{}, fmt.Errorf("symbol ID missing")
	}
	if sym.Name == "" {
		return symbol.SymbolSummaryInput{}, fmt.Errorf("symbol name missing for %s", sym.ID)
	}
	input := symbol.SymbolSummaryInput{
		SymbolID:      sym.ID,
		SymbolKey:     sym.Key.String(),
		FilePath:      sym.FilePath,
		Name:          sym.Name,
		Kind:          sym.Kind,
		Signature:     sym.Signature,
		Documentation: sym.Documentation,
		BodyDigest:    sym.BodyDigest,
		Language:      sym.Language,
		Pkg:           symbolutil.DeriveSymbolPackage(sym.FilePath, sym.Language),
	}

	return input, nil
}

// extractFirstCommentForSummary extracts the first comment block.
func extractFirstCommentForSummary(content string) string {
	lines := strings.Split(content, "\n")
	var comment strings.Builder
	inBlock := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "" && comment.Len() == 0 {
			continue
		}

		if strings.HasPrefix(trimmed, "/*") {
			inBlock = true
			trimmed = strings.TrimPrefix(trimmed, "/*")
		}
		if inBlock {
			if idx := strings.Index(trimmed, "*/"); idx >= 0 {
				comment.WriteString(strings.TrimSpace(trimmed[:idx]))
				break
			}
			comment.WriteString(strings.TrimSpace(trimmed))
			comment.WriteString(" ")
			continue
		}

		if strings.HasPrefix(trimmed, "//") {
			comment.WriteString(strings.TrimSpace(strings.TrimPrefix(trimmed, "//")))
			comment.WriteString(" ")
			continue
		}

		if comment.Len() > 0 || !strings.HasPrefix(trimmed, "package") {
			break
		}
	}

	result := strings.TrimSpace(comment.String())
	if len(result) > 200 {
		result = result[:200]
	}
	return result
}

// extractTopSymbolsForSummary extracts top-level symbol names.
func extractTopSymbolsForSummary(content string, path string) []string {
	var symbols []string
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Go functions/types
		if strings.HasPrefix(trimmed, "func ") {
			if name := extractGoFuncNameForSummary(trimmed); name != "" && name[0] >= 'A' && name[0] <= 'Z' {
				symbols = append(symbols, name)
			}
		} else if strings.HasPrefix(trimmed, "type ") {
			parts := strings.Fields(strings.TrimPrefix(trimmed, "type "))
			if len(parts) > 0 && len(parts[0]) > 0 && parts[0][0] >= 'A' && parts[0][0] <= 'Z' {
				symbols = append(symbols, parts[0])
			}
		}

		// TypeScript/JavaScript exports
		if (strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".tsx") ||
			strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".jsx")) &&
			strings.HasPrefix(trimmed, "export ") {
			if name := extractTSExportNameForSummary(trimmed); name != "" {
				symbols = append(symbols, name)
			}
		}

		if len(symbols) >= 10 {
			break
		}
	}

	return symbols
}

func extractGoFuncNameForSummary(line string) string {
	line = strings.TrimPrefix(line, "func ")
	if strings.HasPrefix(line, "(") {
		idx := strings.Index(line, ")")
		if idx >= 0 {
			line = strings.TrimSpace(line[idx+1:])
		}
	}
	idx := strings.Index(line, "(")
	if idx > 0 {
		return line[:idx]
	}
	return ""
}

func extractTSExportNameForSummary(line string) string {
	line = strings.TrimPrefix(line, "export ")
	line = strings.TrimPrefix(line, "default ")

	if strings.HasPrefix(line, "function ") {
		line = strings.TrimPrefix(line, "function ")
		idx := strings.Index(line, "(")
		if idx > 0 {
			return line[:idx]
		}
	}
	if strings.HasPrefix(line, "const ") || strings.HasPrefix(line, "let ") || strings.HasPrefix(line, "var ") {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			return strings.TrimSuffix(parts[1], ":")
		}
	}
	if strings.HasPrefix(line, "class ") || strings.HasPrefix(line, "interface ") || strings.HasPrefix(line, "type ") {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			return strings.TrimSuffix(parts[1], "{")
		}
	}
	return ""
}

func init() {
	rootCmd.AddCommand(newIndexCommand())
}
