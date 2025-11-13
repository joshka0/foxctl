package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/jkatigb/agentctl/internal/cache"
	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/envelope"
	"github.com/spf13/cobra"
)

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
			return withCacheStore(cmd.Context(), cfg, func(store *cache.Store) error {
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
			})
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
			return withCacheStore(cmd.Context(), cfg, func(store *cache.Store) error {
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
			})
		},
	}
	return cmd
}

func decodeResult(result []byte) any {
	var decoded any
	if err := json.Unmarshal(result, &decoded); err == nil {
		return decoded
	}
	return string(result)
}
