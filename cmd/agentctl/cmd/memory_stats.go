package cmd

import (
	"github.com/jkatigb/agentctl/internal/cache"
	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/envelope"
	memstore "github.com/jkatigb/agentctl/internal/memory"
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

			var cacheStats cache.Stats
			if err := withCacheStore(cmd.Context(), cfg, func(store *cache.Store) error {
				stats, err := store.Stats(cmd.Context())
				if err != nil {
					return err
				}
				cacheStats = stats
				return nil
			}); err != nil {
				return err
			}

			var memoryStats memstore.Stats
			if err := withMemoryStore(cmd.Context(), cfg, func(store *memstore.Store) error {
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
