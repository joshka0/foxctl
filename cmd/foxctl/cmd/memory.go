package cmd

import "github.com/spf13/cobra"

func newMemoryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Inspect and manage cached or named memories",
	}
	cmd.AddCommand(
		newMemoryRecentCommand(),
		newMemoryCacheCommand(),
		newMemoryStatsCommand(),
		newMemoryListCommand(),
		newMemorySearchCommand(),
		newMemoryGetCommand(),
		newMemoryPutCommand(),
		newMemorySaveCommand(),
		newMemoryUpdateCommand(),
		newMemoryDeleteCommand(),
		newMemoryRelevantCommand(),
		newMemoryMigrateWorkspaceCommand(),
	)
	return cmd
}

func init() {
	rootCmd.AddCommand(newMemoryCommand())
}
