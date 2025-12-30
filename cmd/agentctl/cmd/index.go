package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/storage/dbdriver"
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

All Voyage models use 1024 dimensions.

Remote sync:
  Use 'index sync push' to push local embeddings to remote Turso for
  cross-workspace knowledge sharing.`,
	}
	cmd.AddCommand(
		newIndexInitCommand(),
		newIndexStatusCommand(),
		newIndexSyncCommand(),
		newIndexGitDiffCommand(),
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
  agentctl index sync push

  # Push only memory scope
  agentctl index sync push --scope memory

  # Push with explicit connection
  agentctl index sync push --remote-url libsql://db.turso.io --remote-token xxx

  # Dry run to see what would be pushed
  agentctl index sync push --dry-run`,
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
  agentctl index sync query --query "authentication middleware" --global

  # Query specific workspaces
  agentctl index sync query --query "API error handling" --workspaces project-a,project-b

  # Limit results
  agentctl index sync query --query "database migrations" --global --limit 5`,
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
	cfg, err := config.Load(ctx)
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
				fmt.Fprintf(os.Stderr, "  [%s] Error: %s\n", scope, errMsg)
			} else {
				fmt.Fprintf(os.Stderr, "  [%s] Pushed %d items\n", scope, result["count"])
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

	// Check for Voyage API key for query embedding
	voyageKey := os.Getenv("VOYAGE_API_KEY")
	if voyageKey == "" {
		return fmt.Errorf("VOYAGE_API_KEY required for generating query embedding")
	}

	// Load config for dimensions
	cfg, err := config.Load(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Generate query embedding
	provider, err := semantic.NewVoyageProvider(semantic.VoyageConfig{
		APIKey:        voyageKey,
		Model:         modelMemory, // Use memory model for text queries
		RateLimitWait: boolPtr(true),
	})
	if err != nil {
		return fmt.Errorf("create voyage provider: %w", err)
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
	storageDir := filepath.Join(cfg.Home, "storage")
	casDir := cfg.Paths.CAS
	if casDir == "" {
		casDir = filepath.Join(cfg.Home, "cas")
	}

	// Open local SQLite store
	localStore, err := memory.Open(ctx, storageDir, casDir)
	if err != nil {
		return 0, fmt.Errorf("open local store: %w", err)
	}
	defer localStore.Close()

	// List memories with embeddings
	memories, err := localStore.List(ctx, workspace, 10000) // Get all
	if err != nil {
		return 0, fmt.Errorf("list memories: %w", err)
	}

	if dryRun {
		// Count memories with embeddings
		count := 0
		for _, mem := range memories {
			emb, err := localStore.GetEmbedding(ctx, mem.Name, workspace)
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
	if meta, _ := localStore.GetEmbeddingMetadata(ctx, workspace); meta != nil && meta.Model != "" {
		model = meta.Model
	}

	// Push each memory with embedding
	count := 0
	for _, mem := range memories {
		emb, err := localStore.GetEmbedding(ctx, mem.Name, workspace)
		if err != nil || len(emb) == 0 {
			continue // Skip memories without embeddings
		}

		_, err = remoteStore.SaveWithEmbedding(ctx, mem, emb, model)
		if err != nil {
			fmt.Fprintf(os.Stderr, "    [warn] skip %s: %v\n", mem.Name, err)
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
			fmt.Fprintf(os.Stderr, "    [warn] skip session %s: %v\n", sess.ID, err)
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

The default range is ORIG_HEAD..HEAD which captures changes from the most
recent pull/merge. This can be run from a git post-merge hook for automatic
incremental indexing.

Git hook setup (optional):
  echo '#!/bin/sh
  agentctl index git-diff' > .git/hooks/post-merge
  chmod +x .git/hooks/post-merge`,
		Example: `  # Index files changed by last pull/merge
  agentctl index git-diff

  # Index files changed in last 3 commits
  agentctl index git-diff --base HEAD~3

  # Index specific commit range
  agentctl index git-diff --base abc123 --head def456

  # Dry run to see what would be indexed
  agentctl index git-diff --dry-run`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runIndexGitDiff(cmd, workspace, base, head, dryRun, embed)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root directory")
	cmd.Flags().StringVar(&base, "base", "ORIG_HEAD", "Base commit (default: ORIG_HEAD for post-pull)")
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

	// Check for VOYAGE_API_KEY if embedding is requested
	voyageKey := os.Getenv("VOYAGE_API_KEY")
	if embed && voyageKey == "" && !dryRun {
		return fmt.Errorf("VOYAGE_API_KEY required when --embed is set")
	}

	// Get changed files from git
	changedFiles, err := getGitDiffFiles(ctx, absWorkspace, base, head)
	if err != nil {
		return fmt.Errorf("get git diff: %w", err)
	}

	// Filter to supported file types
	supportedExts := map[string]bool{
		".go": true, ".py": true, ".js": true, ".jsx": true, ".ts": true, ".tsx": true,
	}
	var filesToIndex []string
	for _, f := range changedFiles {
		ext := strings.ToLower(filepath.Ext(f))
		if supportedExts[ext] {
			filesToIndex = append(filesToIndex, f)
		}
	}

	if dryRun {
		data := map[string]any{
			"dry_run":        true,
			"workspace":      absWorkspace,
			"base":           base,
			"head":           head,
			"changed_files":  len(changedFiles),
			"files_to_index": len(filesToIndex),
			"files":          filesToIndex,
		}
		env := protocol.OK("index.git-diff", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		return protocol.Write(cmd.OutOrStdout(), env)
	}

	if len(filesToIndex) == 0 {
		data := map[string]any{
			"workspace":     absWorkspace,
			"base":          base,
			"head":          head,
			"changed_files": len(changedFiles),
			"indexed":       0,
			"message":       "no supported files to index",
			"duration_ms":   time.Since(start).Milliseconds(),
		}
		env := protocol.OK("index.git-diff", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		return protocol.Write(cmd.OutOrStdout(), env)
	}

	// Load config
	cfg, err := config.Load(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Index changed files using semantic indexer
	storageDir := filepath.Join(cfg.Home, "storage")
	casDir := cfg.Paths.CAS
	if casDir == "" {
		casDir = filepath.Join(cfg.Home, "cas")
	}

	store, err := memory.Open(ctx, storageDir, casDir)
	if err != nil {
		return fmt.Errorf("open memory store: %w", err)
	}
	defer store.Close()

	// Create provider if embedding is enabled
	var provider *semantic.VoyageProvider
	if embed && voyageKey != "" {
		provider, err = semantic.NewVoyageProvider(semantic.VoyageConfig{
			APIKey:        voyageKey,
			Model:         modelSymbols,
			RateLimitWait: boolPtr(true),
		})
		if err != nil {
			return fmt.Errorf("create voyage provider: %w", err)
		}
	}

	// Use indexer for file processing
	indexerCfg := semantic.Config{Enabled: true}
	logger := zerolog.New(os.Stderr).With().Timestamp().Logger()

	var indexedCount int
	var queuedCount int
	var errorCount int
	results := make([]map[string]any, 0) // Initialize as empty slice for stable JSON

	// Create indexer once outside the loop
	var indexer *semantic.Indexer
	if provider != nil {
		indexer = semantic.NewIndexer(indexerCfg, store, provider, absWorkspace, logger)
	}

	for _, relPath := range filesToIndex {
		absPath := filepath.Join(absWorkspace, relPath)

		// Check if file exists (might have been deleted)
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			results = append(results, map[string]any{
				"file":   relPath,
				"status": "skipped",
				"reason": "file deleted",
			})
			continue
		}

		// Use semantic indexer for this file
		if indexer != nil {
			args := semantic.JobArgs{
				WorkspaceID: absWorkspace,
				Reason:      semantic.ReasonGitPull,
				Files:       []semantic.JobFileInput{{Path: relPath}}, // Use relative path
			}

			result, err := indexer.RunInitFilesJob(ctx, args)
			if err != nil {
				results = append(results, map[string]any{
					"file":   relPath,
					"status": "error",
					"error":  err.Error(),
				})
				errorCount++
				continue
			}

			results = append(results, map[string]any{
				"file":           relPath,
				"status":         "indexed",
				"files_indexed":  result.Summary.FilesIndexed,
				"chunks_indexed": result.Summary.ChunksIndexed,
			})
			indexedCount++
			fmt.Fprintf(os.Stderr, "  [indexed] %s\n", relPath)
		} else {
			// Without provider, just report the file as queued (not indexed)
			results = append(results, map[string]any{
				"file":   relPath,
				"status": "queued",
				"note":   "no embedding provider (add --embed with VOYAGE_API_KEY)",
			})
			queuedCount++
			fmt.Fprintf(os.Stderr, "  [queued] %s\n", relPath)
		}
	}

	data := map[string]any{
		"workspace":     absWorkspace,
		"base":          base,
		"head":          head,
		"changed_files": len(changedFiles),
		"indexed":       indexedCount,
		"queued":        queuedCount,
		"errors":        errorCount,
		"embed":         embed,
		"results":       results,
		"duration_ms":   time.Since(start).Milliseconds(),
	}

	env := protocol.OK("index.git-diff", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	return protocol.Write(cmd.OutOrStdout(), env)
}

// getGitDiffFiles returns the list of files changed between base and head commits.
func getGitDiffFiles(ctx context.Context, repoPath, base, head string) ([]string, error) {
	// First check if ORIG_HEAD exists (it won't if no merge/pull has happened)
	if base == "ORIG_HEAD" {
		checkCmd := exec.CommandContext(ctx, "git", "-C", repoPath, "rev-parse", "--verify", "ORIG_HEAD")
		if err := checkCmd.Run(); err != nil {
			// ORIG_HEAD doesn't exist, fall back to HEAD~1
			base = "HEAD~1"
		}
	}

	// Get list of changed files
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "diff", "--name-only", base+".."+head)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// If the range doesn't work, try without the range (unstaged changes)
		cmd2 := exec.CommandContext(ctx, "git", "-C", repoPath, "diff", "--name-only")
		var stdout2 bytes.Buffer
		cmd2.Stdout = &stdout2
		if err2 := cmd2.Run(); err2 != nil {
			return nil, fmt.Errorf("git diff failed: %w (stderr: %s)", err, stderr.String())
		}
		stdout = stdout2
	}

	// Parse output
	var files []string
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}

	return files, nil
}

func init() {
	rootCmd.AddCommand(newIndexCommand())
}
