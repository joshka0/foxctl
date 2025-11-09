package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

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
		newMemoryListCommand(),
		newMemoryGetCommand(),
		newMemorySaveCommand(),
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
			defer store.Close()

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
			defer store.Close()

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
			defer mem.Close()

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
	cmd.Flags().StringVar(&summary, "summary", "", "Summary metadata for the entry")
	_ = cmd.MarkFlagRequired("as")
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
