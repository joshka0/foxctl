package cmd

import (
	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/envelope"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/spf13/cobra"
)

func newMemoryStatsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show cache and named memory store statistics",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(cmd.Context())
			if err != nil {
				return err
			}

			var cacheStats storage.CacheStats
			if err := withCacheStore(cmd.Context(), cfg, func(store storage.CacheStore) error {
				stats, err := store.Stats(cmd.Context())
				if err != nil {
					return err
				}
				cacheStats = stats
				return nil
			}); err != nil {
				return err
			}

			var memoryStats storage.MemoryStats
			if err := withMemoryStore(cmd.Context(), cfg, func(store storage.MemoryStore) error {
				stats, err := store.Stats(cmd.Context())
				if err != nil {
					return err
				}
				memoryStats = stats
				return nil
			}); err != nil {
				return err
			}

			data := map[string]any{
				"cache": map[string]any{
					"entries":     cacheStats.Entries,
					"ttl_seconds": cacheStats.TTL.Seconds(),
					"db_path":     cacheStats.Path,
				},
				"memory": map[string]any{
					"entries": memoryStats.Named,
					"db_path": memoryStats.Path,
				},
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("agentctl.memory.stats", data))
		},
	}
	return cmd
}
