package cmd

import (
	"github.com/spf13/cobra"
)

func newSandboxCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "sandbox",
		Short: "Sandbox integration commands",
		Long:  "Sandbox integrations are temporarily disabled.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return writeErrorEnvelope(cmd, "sandbox", "ERUNTIME", openSandboxDisabledMessage)
		},
	}
}

func init() {
	rootCmd.AddCommand(newSandboxCommand())
}
