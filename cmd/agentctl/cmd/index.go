package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
	"github.com/jkatigb/agentctl/internal/storage/tasks"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

// Scope-based model recommendations (from semantic.ScopeModelRecommendation)
const (
	modelSymbols  = "voyage-code-3"  // Best for code
	modelMemory   = "voyage-3-large" // Best text retrieval
	modelTasks    = "voyage-3.5"     // Good quality, lower cost
	modelSessions = "voyage-3.5"     // Good quality, lower cost
)

func newIndexCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Manage embeddings for all scopes (symbols, sessions, memories, tasks)",
		Long: `Manage the embedding index across all knowledge scopes.

This is a simplified interface for full-repo embedding management.
For fine-grained control, use 'agentctl semantic-index'.

Scopes and models:
  symbols   - Code files indexed with voyage-code-3 ($0.18/1M)
  memory    - Gotchas/notes with voyage-3-large ($0.18/1M)
  tasks     - Task descriptions with voyage-3.5 ($0.06/1M)
  sessions  - Session context with voyage-3.5 ($0.06/1M)

All Voyage models use 1024 dimensions.`,
	}
	cmd.AddCommand(
		newIndexInitCommand(),
		newIndexStatusCommand(),
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

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize embeddings for all scopes",
		Long: `Initialize embeddings for the entire repository across all scopes.

This command performs one-time full embedding generation using the
recommended Voyage AI models for each scope:

  symbols   → voyage-code-3   (code files)
  memory    → voyage-3-large  (gotchas, notes)
  tasks     → voyage-3.5      (task descriptions)
  sessions  → voyage-3.5      (session summaries)

Requires VOYAGE_API_KEY environment variable.`,
		Example: `  # Index everything (all scopes)
  agentctl index init

  # Index only symbols (code)
  agentctl index init --scope symbols

  # Index symbols and memories
  agentctl index init --scope symbols,memory

  # Custom glob pattern for symbols
  agentctl index init --glob '**/*.go' --exclude '*_test.go,vendor/**'

  # Dry run to see what would be indexed
  agentctl index init --dry-run`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runIndexInit(cmd, workspace, scopes, glob, exclude, dryRun, parallel)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root directory")
	cmd.Flags().StringSliceVar(&scopes, "scope", []string{"symbols", "memory", "tasks", "sessions"}, "Scopes to index (symbols, memory, tasks, sessions)")
	cmd.Flags().StringVar(&glob, "glob", "**/*.go", "Glob pattern for symbol files")
	cmd.Flags().StringSliceVar(&exclude, "exclude", []string{"*_test.go", "vendor/**"}, "Glob patterns to exclude for symbols")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be indexed without making changes")
	cmd.Flags().BoolVar(&parallel, "parallel", false, "Index scopes in parallel (faster but uses more API quota)")

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
	Count    int    `json:"count"`
	Duration int64  `json:"duration_ms"`
	Error    string `json:"error,omitempty"`
}

func runIndexInit(cmd *cobra.Command, workspace string, scopes []string, glob string, exclude []string, dryRun, parallel bool) error {
	start := time.Now()
	ctx := cmd.Context()

	// Check for API key
	voyageKey := os.Getenv("VOYAGE_API_KEY")
	if voyageKey == "" && !dryRun {
		return fmt.Errorf("VOYAGE_API_KEY environment variable required for embedding generation")
	}

	// Resolve workspace
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return fmt.Errorf("resolve workspace: %w", err)
	}

	// Load config
	cfg, err := config.Load(ctx)
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
		return runIndexDryRun(cmd, absWorkspace, scopes, glob, exclude)
	}

	results := make([]scopeResult, 0, len(scopes))
	var mu sync.Mutex

	indexScope := func(scope string) scopeResult {
		scopeStart := time.Now()
		result := scopeResult{Scope: scope}

		switch scope {
		case "symbols":
			result.Model = modelSymbols
			count, err := indexSymbols(ctx, cfg, absWorkspace, glob, exclude, voyageKey)
			result.Count = count
			if err != nil {
				result.Error = err.Error()
			}

		case "memory":
			result.Model = modelMemory
			count, err := reembedMemories(ctx, cfg, absWorkspace, voyageKey)
			result.Count = count
			if err != nil {
				result.Error = err.Error()
			}

		case "tasks":
			result.Model = modelTasks
			count, err := reembedTasks(ctx, cfg, voyageKey)
			result.Count = count
			if err != nil {
				result.Error = err.Error()
			}

		case "sessions":
			result.Model = modelSessions
			count, err := reembedSessions(ctx, cfg, voyageKey)
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
			fmt.Fprintf(os.Stderr, "Indexing %s with %s...\n", scope, scopeModel(scope))
			result := indexScope(scope)
			results = append(results, result)
			if result.Error != "" {
				fmt.Fprintf(os.Stderr, "  Error: %s\n", result.Error)
			} else {
				fmt.Fprintf(os.Stderr, "  Done: %d items in %dms\n", result.Count, result.Duration)
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

func runIndexDryRun(cmd *cobra.Command, workspace string, scopes []string, glob string, exclude []string) error {
	data := map[string]any{
		"dry_run":   true,
		"workspace": workspace,
		"scopes":    []map[string]any{},
	}

	scopeInfo := make([]map[string]any, 0, len(scopes))
	for _, scope := range scopes {
		info := map[string]any{
			"scope": scope,
			"model": scopeModel(scope),
		}

		if scope == "symbols" {
			files, err := findFilesMatchingGlob(workspace, glob, exclude)
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

	cfg, err := config.Load(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	storageDir := filepath.Join(cfg.Home, "storage")
	casDir := cfg.Paths.CAS
	if casDir == "" {
		casDir = filepath.Join(cfg.Home, "cas")
	}

	// Get counts from each store
	status := map[string]any{
		"workspace": absWorkspace,
		"scopes":    []map[string]any{},
	}

	scopes := []map[string]any{}

	// Symbols - check memory store stats
	memStore, err := memory.Open(ctx, storageDir, casDir)
	if err == nil {
		defer memStore.Close()
		stats, _ := memStore.Stats(ctx)
		scopes = append(scopes, map[string]any{
			"scope":             "symbols",
			"recommended_model": modelSymbols,
			"total_memories":    stats.Named,
		})
	}

	// Memory count
	scopes = append(scopes, map[string]any{
		"scope":             "memory",
		"recommended_model": modelMemory,
		"status":            "use 'agentctl memory stats' for details",
	})

	// Tasks
	taskStore, err := tasks.Open(ctx, storageDir)
	if err == nil {
		defer taskStore.Close()
		all, _ := taskStore.ListAll(ctx, 1000)
		scopes = append(scopes, map[string]any{
			"scope":             "tasks",
			"recommended_model": modelTasks,
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
			"recommended_model": modelSessions,
			"recent_count":      len(recent),
		})
	}

	status["scopes"] = scopes

	env := protocol.OK("index.status", status, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	return protocol.Write(cmd.OutOrStdout(), env)
}

func scopeModel(scope string) string {
	switch scope {
	case "symbols":
		return modelSymbols
	case "memory":
		return modelMemory
	case "tasks":
		return modelTasks
	case "sessions":
		return modelSessions
	default:
		return "unknown"
	}
}

func indexSymbols(ctx context.Context, cfg config.Config, workspace, glob string, exclude []string, apiKey string) (int, error) {
	storageDir := filepath.Join(cfg.Home, "storage")
	casDir := cfg.Paths.CAS
	if casDir == "" {
		casDir = filepath.Join(cfg.Home, "cas")
	}

	store, err := memory.Open(ctx, storageDir, casDir)
	if err != nil {
		return 0, fmt.Errorf("open memory store: %w", err)
	}
	defer store.Close()

	provider, err := semantic.NewVoyageProvider(semantic.VoyageConfig{
		APIKey:        apiKey,
		Model:         modelSymbols,
		RateLimitWait: boolPtr(true),
	})
	if err != nil {
		return 0, fmt.Errorf("create voyage provider: %w", err)
	}

	files, err := findFilesMatchingGlob(workspace, glob, exclude)
	if err != nil {
		return 0, fmt.Errorf("find files: %w", err)
	}

	if len(files) == 0 {
		return 0, nil
	}

	indexerCfg := semantic.Config{Enabled: true}
	logger := zerolog.New(os.Stderr).With().Timestamp().Logger()
	indexer := semantic.NewIndexer(indexerCfg, store, provider, workspace, logger)

	args := semantic.JobArgs{
		WorkspaceID: workspace,
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

func reembedMemories(ctx context.Context, cfg config.Config, workspace, apiKey string) (int, error) {
	storageDir := filepath.Join(cfg.Home, "storage")
	casDir := cfg.Paths.CAS
	if casDir == "" {
		casDir = filepath.Join(cfg.Home, "cas")
	}

	store, err := memory.Open(ctx, storageDir, casDir)
	if err != nil {
		return 0, fmt.Errorf("open memory store: %w", err)
	}
	defer store.Close()

	provider, err := semantic.NewVoyageProvider(semantic.VoyageConfig{
		APIKey:        apiKey,
		Model:         modelMemory,
		RateLimitWait: boolPtr(true),
	})
	if err != nil {
		return 0, fmt.Errorf("create voyage provider: %w", err)
	}

	// Get all memories and re-embed them
	memories, err := store.List(ctx, workspace, 1000)
	if err != nil {
		return 0, fmt.Errorf("list memories: %w", err)
	}

	count := 0
	for _, mem := range memories {
		// Generate embedding for summary
		text := mem.Summary
		if text == "" {
			continue
		}

		fmt.Fprintf(os.Stderr, "  [memory] %s\n", mem.Name)
		embedding, err := provider.Embed(ctx, text)
		if err != nil {
			continue // Skip on error, don't fail entire batch
		}

		// Store embedding
		if err := store.UpdateEmbedding(ctx, mem.Name, workspace, embedding); err != nil {
			continue
		}
		count++
	}

	return count, nil
}

func reembedTasks(ctx context.Context, cfg config.Config, apiKey string) (int, error) {
	storageDir := filepath.Join(cfg.Home, "storage")

	store, err := tasks.Open(ctx, storageDir)
	if err != nil {
		return 0, fmt.Errorf("open tasks store: %w", err)
	}
	defer store.Close()

	provider, err := semantic.NewVoyageProvider(semantic.VoyageConfig{
		APIKey:        apiKey,
		Model:         modelTasks,
		RateLimitWait: boolPtr(true),
	})
	if err != nil {
		return 0, fmt.Errorf("create voyage provider: %w", err)
	}

	allTasks, err := store.ListAll(ctx, 1000)
	if err != nil {
		return 0, fmt.Errorf("list tasks: %w", err)
	}

	count := 0
	for _, task := range allTasks {
		text := task.Title
		if task.Description != "" {
			text += "\n" + task.Description
		}
		if text == "" {
			continue
		}

		fmt.Fprintf(os.Stderr, "  [task] %s\n", task.ID)
		embedding, err := provider.Embed(ctx, text)
		if err != nil {
			continue
		}

		// Convert []float32 to []byte (JSON encoding for storage)
		embeddingBytes, err := json.Marshal(embedding)
		if err != nil {
			continue
		}

		if err := store.SetEmbedding(ctx, task.ID, embeddingBytes, modelTasks); err != nil {
			continue
		}
		count++
	}

	return count, nil
}

func reembedSessions(ctx context.Context, cfg config.Config, apiKey string) (int, error) {
	storageDir := filepath.Join(cfg.Home, "storage")

	store, err := sessions.Open(ctx, storageDir)
	if err != nil {
		return 0, fmt.Errorf("open sessions store: %w", err)
	}
	defer store.Close()

	provider, err := semantic.NewVoyageProvider(semantic.VoyageConfig{
		APIKey:        apiKey,
		Model:         modelSessions,
		RateLimitWait: boolPtr(true),
	})
	if err != nil {
		return 0, fmt.Errorf("create voyage provider: %w", err)
	}

	opts := sessions.ListOptions{Limit: 1000}
	recentSessions, err := store.List(ctx, opts)
	if err != nil {
		return 0, fmt.Errorf("list sessions: %w", err)
	}

	count := 0
	for _, sess := range recentSessions {
		text := sess.Summary
		if text == "" {
			continue
		}

		fmt.Fprintf(os.Stderr, "  [session] %s\n", sess.ID)
		embedding, err := provider.Embed(ctx, text)
		if err != nil {
			continue
		}

		// Convert []float32 to []byte (JSON encoding for storage)
		embeddingBytes, err := json.Marshal(embedding)
		if err != nil {
			continue
		}

		if err := store.SetEmbedding(ctx, sess.ID, embeddingBytes, modelSessions); err != nil {
			continue
		}
		count++
	}

	return count, nil
}

func init() {
	rootCmd.AddCommand(newIndexCommand())
}
