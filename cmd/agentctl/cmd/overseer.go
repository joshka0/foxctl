package cmd

import (
	"time"

	"github.com/jkatigb/agentctl/internal/agent/runtime"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/spf13/cobra"
)

var overseerCmd = &cobra.Command{
	Use:   "overseer",
	Short: "Manage the overseer",
}

var overseerDryRun bool

var overseerRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the overseer daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		cfg := config.MustFromContext(ctx)
		return runtime.RunOverseer(ctx, runtime.OverseerDaemonOptions{
			StorageRoot:  cfg.Storage.Root,
			PollInterval: 500 * time.Millisecond,
			DryRun:       overseerDryRun,
		})
	},
}

func init() {
	rootCmd.AddCommand(overseerCmd)
	overseerCmd.AddCommand(overseerRunCmd)

	overseerRunCmd.Flags().BoolVar(&overseerDryRun, "dry-run", false, "Validate decisions without executing them")
}
