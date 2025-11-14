package cmd

import (
	"context"

	"github.com/jkatigb/agentctl/cmd/agentctl/cmd/memorycmd"
	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/spf13/cobra"
)

func newMemoryStatsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show cache and named memory store statistics",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return memorycmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				var cacheStats storage.CacheStats
				if err := memorycmd.WithCacheStore(ctx, cfg, func(store storage.CacheStore) error {
					stats, err := store.Stats(ctx)
					if err != nil {
						return err
					}
					cacheStats = stats
					return nil
				}); err != nil {
					return err
				}

				var memoryStats storage.MemoryStats
				if err := memorycmd.WithMemoryStore(ctx, cfg, func(store storage.MemoryStore) error {
					stats, err := store.Stats(ctx)
					if err != nil {
						return err
					}
					memoryStats = stats
					return nil
				}); err != nil {
					return err
				}

				data := struct {
					Cache struct {
						Entries    int64   `json:"entries"`
						TTLSeconds float64 `json:"ttl_seconds"`
						DBPath     string  `json:"db_path"`
					} `json:"cache"`
					Memory struct {
						Entries int64  `json:"entries"`
						DBPath  string `json:"db_path"`
					} `json:"memory"`
				}{}
				data.Cache.Entries = cacheStats.Entries
				data.Cache.TTLSeconds = cacheStats.TTL.Seconds()
				data.Cache.DBPath = cacheStats.Path
				data.Memory.Entries = memoryStats.Named
				data.Memory.DBPath = memoryStats.Path

				return memorycmd.WriteOK(cmd.OutOrStdout(), "agentctl.memory.stats", data)
			})
		},
	}
	return cmd
}
