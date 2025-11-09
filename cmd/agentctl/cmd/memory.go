package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/cache"
	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/envelope"
	memstore "github.com/jkatigb/agentctl/internal/memory"
	"github.com/jkatigb/agentctl/internal/workspace"
	"github.com/spf13/cobra"
)

func newMemoryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Inspect and manage cached or named memories",
	}
	cmd.AddCommand(
		newMemoryRecentCommand(),
		newMemoryCacheCommand(),
		newMemoryListCommand(),
		newMemorySearchCommand(),
		newMemoryGetCommand(),
		newMemoryPutCommand(),
		newMemorySaveCommand(),
		newMemoryUpdateCommand(),
		newMemoryDeleteCommand(),
		newMemoryRelevantCommand(),
	)
	return cmd
}

func newMemoryRecentCommand() *cobra.Command {
	var workspaceFlag string
	var limit int
	cmd := &cobra.Command{
		Use:   "recent",
		Short: "Show recent auto-cache entries",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(cmd.Context())
			if err != nil {
				return err
			}
			ws := resolveWorkspace(cfg, workspaceFlag)
			store, err := cache.Open(cmd.Context(), cfg.Paths.Cache, cache.Options{
				AutoTTL: cfg.Memory.AutoCacheTTL,
				CASPath: cfg.Paths.CAS,
			})
			if err != nil {
				return err
			}
			defer func() { _ = store.Close() }()

			entries, err := store.Recent(cmd.Context(), ws, limit)
			if err != nil {
				return err
			}
			var payload []map[string]any
			for _, e := range entries {
				payload = append(payload, map[string]any{
					"cache_key":     e.CacheKey,
					"skill":         e.SkillName,
					"version":       e.SkillVersion,
					"workspace":     e.Workspace,
					"created_at":    e.CreatedAt,
					"expires_at":    e.ExpiresAt,
					"hit_count":     e.HitCount,
					"last_accessed": e.LastAccessed,
				})
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("agentctl.memory.recent", map[string]any{
				"entries":   payload,
				"workspace": ws,
			}))
		},
	}
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Workspace path (default: auto-detect)")
	cmd.Flags().IntVar(&limit, "limit", 10, "Maximum entries to return")
	return cmd
}

func newMemoryCacheCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache <cache-key>",
		Short: "Fetch a cached result by cache key",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cmd.Context())
			if err != nil {
				return err
			}
			store, err := cache.Open(cmd.Context(), cfg.Paths.Cache, cache.Options{
				AutoTTL: cfg.Memory.AutoCacheTTL,
				CASPath: cfg.Paths.CAS,
			})
			if err != nil {
				return err
			}
			defer func() { _ = store.Close() }()

			entry, ok, err := store.Get(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("cache entry %s not found", args[0])
			}
			decoded := decodeResult(entry.Result)
			data := map[string]any{
				"cache_key": entry.CacheKey,
				"skill":     entry.SkillName,
				"version":   entry.SkillVersion,
				"workspace": entry.Workspace,
				"result":    decoded,
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("agentctl.memory.cache", data))
		},
	}
	return cmd
}

func newMemoryListCommand() *cobra.Command {
	var workspaceFlag string
	var limit int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List named memories",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(cmd.Context())
			if err != nil {
				return err
			}
			ws := resolveWorkspace(cfg, workspaceFlag)
			store, err := memstore.Open(cmd.Context(), cfg.Paths.Cache, cfg.Paths.CAS)
			if err != nil {
				return err
			}
			defer func() { _ = store.Close() }()

			entries, err := store.List(cmd.Context(), ws, limit)
			if err != nil {
				return err
			}
			var payload []map[string]any
			for _, e := range entries {
				payload = append(payload, map[string]any{
					"name":         e.Name,
					"type":         e.Type,
					"workspace":    e.Workspace,
					"summary":      e.Summary,
					"created_at":   e.CreatedAt,
					"updated_at":   e.UpdatedAt,
					"access_count": e.AccessCount,
				})
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("agentctl.memory.list", map[string]any{
				"entries":   payload,
				"workspace": ws,
			}))
		},
	}
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Workspace path (default: auto-detect)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum entries to return")
	return cmd
}

func newMemorySearchCommand() *cobra.Command {
	var workspaceFlag string
	var query string
	var limit int
	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search named memories",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(query) == "" {
				return fmt.Errorf("--query is required")
			}
			cfg, err := config.Load(cmd.Context())
			if err != nil {
				return err
			}
			ws := resolveWorkspace(cfg, workspaceFlag)
			store, err := memstore.Open(cmd.Context(), cfg.Paths.Cache, cfg.Paths.CAS)
			if err != nil {
				return err
			}
			defer store.Close()

			entries, err := store.Search(cmd.Context(), ws, query, limit)
			if err != nil {
				return err
			}
			var payload []map[string]any
			for _, e := range entries {
				payload = append(payload, map[string]any{
					"name":       e.Name,
					"type":       e.Type,
					"workspace":  e.Workspace,
					"summary":    e.Summary,
					"updated_at": e.UpdatedAt,
				})
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("agentctl.memory.search", map[string]any{
				"entries":   payload,
				"workspace": ws,
			}))
		},
	}
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Workspace path (default: auto-detect)")
	cmd.Flags().StringVar(&query, "query", "", "Search text")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum entries to return")
	return cmd
}

func newMemoryGetCommand() *cobra.Command {
	var workspaceFlag string
	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Retrieve a named memory envelope",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cmd.Context())
			if err != nil {
				return err
			}
			ws := resolveWorkspace(cfg, workspaceFlag)
			store, err := memstore.Open(cmd.Context(), cfg.Paths.Cache, cfg.Paths.CAS)
			if err != nil {
				return err
			}
			defer store.Close()

			entry, err := store.Get(cmd.Context(), args[0], ws)
			if err != nil {
				return err
			}
			modified, err := cache.AnnotateMemory(entry.Result, envelope.MemoryRef{
				Name:      entry.Name,
				Type:      entry.Type,
				Workspace: entry.Workspace,
			})
			if err != nil {
				return err
			}
			return writeEnvelope(cmd.OutOrStdout(), modified)
		},
	}
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Workspace path (default: auto-detect)")
	return cmd
}

func newMemoryPutCommand() *cobra.Command {
	var name string
	var typ string
	var workspaceFlag string
	var summary string
	var file string
	var data string
	cmd := &cobra.Command{
		Use:   "put",
		Short: "Store a JSON envelope as memory",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if name == "" {
				return fmt.Errorf("--name is required")
			}
			cfg, err := config.Load(cmd.Context())
			if err != nil {
				return err
			}
			ws := resolveWorkspace(cfg, workspaceFlag)
			payload, err := readMemoryPayload(cmd, file, data)
			if err != nil {
				return err
			}
			if !json.Valid(payload) {
				return fmt.Errorf("payload must be valid JSON envelope")
			}
			if summary == "" {
				summary = summarizeResult(payload)
			}
			store, err := memstore.Open(cmd.Context(), cfg.Paths.Cache, cfg.Paths.CAS)
			if err != nil {
				return err
			}
			defer store.Close()
			entry, err := store.SaveFromResult(cmd.Context(), name, typ, ws, summary, payload)
			if err != nil {
				return err
			}
			resp := map[string]any{
				"name":      entry.Name,
				"type":      entry.Type,
				"workspace": entry.Workspace,
				"summary":   entry.Summary,
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("agentctl.memory.put", resp))
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Memory name")
	cmd.Flags().StringVar(&typ, "type", "result", "Memory type label")
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Workspace path (default: auto-detect)")
	cmd.Flags().StringVar(&summary, "summary", "", "Summary metadata")
	cmd.Flags().StringVar(&file, "file", "", "Path to JSON envelope ('-' for stdin)")
	cmd.Flags().StringVar(&data, "data", "", "Inline JSON envelope")
	return cmd
}

func newMemorySaveCommand() *cobra.Command {
	var name string
	var typ string
	var workspaceFlag string
	var summary string
	cmd := &cobra.Command{
		Use:   "save <job-id>",
		Short: "Persist a job result as named memory",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return fmt.Errorf("--as is required")
			}
			cfg, err := config.Load(cmd.Context())
			if err != nil {
				return err
			}
			ws := resolveWorkspace(cfg, workspaceFlag)
			store, cleanup, err := openJobStore(cmd.Context())
			if err != nil {
				return err
			}
			defer cleanup()
			result, err := store.Result(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if summary == "" {
				summary = summarizeResult(result)
			}
			mem, err := memstore.Open(cmd.Context(), cfg.Paths.Cache, cfg.Paths.CAS)
			if err != nil {
				return err
			}
			defer func() { _ = mem.Close() }()
			entry, err := mem.SaveFromResult(cmd.Context(), name, typ, ws, summary, result)
			if err != nil {
				return err
			}
			payload := map[string]any{
				"name":      entry.Name,
				"type":      entry.Type,
				"workspace": entry.Workspace,
				"summary":   entry.Summary,
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("agentctl.memory.save", payload))
		},
	}
	cmd.Flags().StringVar(&name, "as", "", "Name for the memory entry")
	cmd.Flags().StringVar(&typ, "type", "result", "Memory type label")
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Workspace path (default: auto-detect)")
	cmd.Flags().StringVar(&summary, "summary", "", "Summary metadata")
	_ = cmd.MarkFlagRequired("as")
	return cmd
}

func newMemoryUpdateCommand() *cobra.Command {
	var workspaceFlag string
	var summary string
	var typ string
	cmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Update named memory metadata",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if summary == "" && typ == "" {
				return fmt.Errorf("at least one of --summary or --type must be set")
			}
			cfg, err := config.Load(cmd.Context())
			if err != nil {
				return err
			}
			ws := resolveWorkspace(cfg, workspaceFlag)
			store, err := memstore.Open(cmd.Context(), cfg.Paths.Cache, cfg.Paths.CAS)
			if err != nil {
				return err
			}
			defer store.Close()
			var summaryPtr *string
			var typePtr *string
			if summary != "" {
				summaryPtr = &summary
			}
			if typ != "" {
				typePtr = &typ
			}
			entry, err := store.Update(cmd.Context(), args[0], ws, summaryPtr, typePtr)
			if err != nil {
				return err
			}
			payload := map[string]any{
				"name":       entry.Name,
				"type":       entry.Type,
				"workspace":  entry.Workspace,
				"summary":    entry.Summary,
				"updated_at": entry.UpdatedAt,
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("agentctl.memory.update", payload))
		},
	}
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Workspace path (default: auto-detect)")
	cmd.Flags().StringVar(&summary, "summary", "", "New summary text")
	cmd.Flags().StringVar(&typ, "type", "", "New type label")
	return cmd
}

func newMemoryDeleteCommand() *cobra.Command {
	var workspaceFlag string
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a named memory entry",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cmd.Context())
			if err != nil {
				return err
			}
			ws := resolveWorkspace(cfg, workspaceFlag)
			store, err := memstore.Open(cmd.Context(), cfg.Paths.Cache, cfg.Paths.CAS)
			if err != nil {
				return err
			}
			defer store.Close()
			if err := store.Delete(cmd.Context(), args[0], ws); err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("agentctl.memory.delete", map[string]any{
				"name":      args[0],
				"workspace": ws,
			}))
		},
	}
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Workspace path (default: auto-detect)")
	return cmd
}

func newMemoryRelevantCommand() *cobra.Command {
	var workspaceFlag string
	var limit int
	cmd := &cobra.Command{
		Use:   "relevant",
		Short: "Rank memories by recency and usage",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(cmd.Context())
			if err != nil {
				return err
			}
			ws := resolveWorkspace(cfg, workspaceFlag)
			store, err := memstore.Open(cmd.Context(), cfg.Paths.Cache, cfg.Paths.CAS)
			if err != nil {
				return err
			}
			defer store.Close()
			entries, err := store.Relevant(cmd.Context(), ws, limit)
			if err != nil {
				return err
			}
			var payload []map[string]any
			for _, e := range entries {
				payload = append(payload, map[string]any{
					"name":          e.Entry.Name,
					"type":          e.Entry.Type,
					"workspace":     e.Entry.Workspace,
					"summary":       e.Entry.Summary,
					"score":         e.Score,
					"access_count":  e.Entry.AccessCount,
					"last_accessed": e.Entry.LastAccess,
				})
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("agentctl.memory.relevant", map[string]any{
				"entries":   payload,
				"workspace": ws,
			}))
		},
	}
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Workspace path (default: auto-detect)")
	cmd.Flags().IntVar(&limit, "limit", 10, "Maximum entries to return")
	return cmd
}

func resolveWorkspace(cfg config.Config, override string) string {
	if override != "" {
		return workspace.Normalize(override)
	}
	if !cfg.Memory.AutoLoadWorkspace {
		if wd, err := os.Getwd(); err == nil {
			return workspace.Normalize(wd)
		}
		return ""
	}
	return workspace.Detect("")
}

func readMemoryPayload(cmd *cobra.Command, file, data string) ([]byte, error) {
	switch {
	case file == "-":
		return io.ReadAll(cmd.InOrStdin())
	case file != "":
		return os.ReadFile(file)
	case data != "":
		return []byte(data), nil
	default:
		return io.ReadAll(cmd.InOrStdin())
	}
}

func decodeResult(result []byte) any {
	var decoded any
	if err := json.Unmarshal(result, &decoded); err == nil {
		return decoded
	}
	return string(result)
}

func summarizeResult(result []byte) string {
	var env envelope.Envelope
	if err := json.Unmarshal(result, &env); err == nil {
		if env.Meta.Workspace != "" {
			return fmt.Sprintf("%s (%s)", env.Command, filepath.Base(env.Meta.Workspace))
		}
		return env.Command
	}
	return ""
}

func init() {
	rootCmd.AddCommand(newMemoryCommand())
}
