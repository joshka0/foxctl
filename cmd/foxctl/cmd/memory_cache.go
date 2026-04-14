package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/joshka0/foxctl/cmd/foxctl/cmd/memorycmd"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/spf13/cobra"
)

func newMemoryRecentCommand() *cobra.Command {
	var workspaceFlag string
	var limit int
	cmd := &cobra.Command{
		Use:   "recent",
		Short: "Show recent auto-cache entries",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return memorycmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				workspaceID := resolveWorkspaceID(cfg, workspaceFlag)
				return memorycmd.WithCacheStore(ctx, cfg, func(store storage.CacheStore) error {
					entries, err := store.Recent(ctx, workspaceID, limit)
					if err != nil {
						return err
					}
					payload := struct {
						Entries   []map[string]any `json:"entries"`
						Workspace string           `json:"workspace"`
					}{Workspace: workspaceID}
					for _, e := range entries {
						payload.Entries = append(payload.Entries, map[string]any{
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
					return memorycmd.WriteOK(cmd.OutOrStdout(), "foxctl.memory.recent", payload)
				})
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
			return memorycmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				return memorycmd.WithCacheStore(ctx, cfg, func(store storage.CacheStore) error {
					entry, ok, err := store.Get(ctx, args[0])
					if err != nil {
						return err
					}
					if !ok {
						return fmt.Errorf("cache entry %s not found", args[0])
					}
					decoded := decodeResult(entry.Result)
					data := struct {
						CacheKey  string `json:"cache_key"`
						Skill     string `json:"skill"`
						Version   string `json:"version"`
						Workspace string `json:"workspace"`
						Result    any    `json:"result"`
					}{
						CacheKey:  entry.CacheKey,
						Skill:     entry.SkillName,
						Version:   entry.SkillVersion,
						Workspace: entry.Workspace,
						Result:    decoded,
					}
					return memorycmd.WriteOK(cmd.OutOrStdout(), "foxctl.memory.cache", data)
				})
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
