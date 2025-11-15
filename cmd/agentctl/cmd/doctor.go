package cmd

import (
	"fmt"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/spf13/cobra"
)

func newDoctorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Validate agentctl configuration and environment",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, ok := config.FromContext(cmd.Context())
			if !ok {
				return fmt.Errorf("configuration not loaded")
			}

			data := map[string]any{
				"config": cfg,
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.doctor", data, protocol.WithSource("run"))
		},
	}

	return cmd
}

func init() {
	rootCmd.AddCommand(newDoctorCommand())
}
