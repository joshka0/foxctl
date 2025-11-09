package cmd

import (
	"fmt"

	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/envelope"
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
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("agentctl.doctor", data))
		},
	}

	return cmd
}

func init() {
	rootCmd.AddCommand(newDoctorCommand())
}
