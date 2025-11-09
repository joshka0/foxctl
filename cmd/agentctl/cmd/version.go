package cmd

import (
	"github.com/jkatigb/agentctl/internal/buildinfo"
	"github.com/jkatigb/agentctl/internal/envelope"
	"github.com/spf13/cobra"
)

func newVersionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print build metadata",
		RunE: func(cmd *cobra.Command, _ []string) error {
			info := buildinfo.Current()
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("agentctl.version", info))
		},
	}

	return cmd
}

func init() {
	rootCmd.AddCommand(newVersionCommand())
}
