package cmd

import (
	"github.com/jkatigb/agentctl/internal/platform/buildinfo"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/spf13/cobra"
)

func newVersionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print build metadata",
		RunE: func(cmd *cobra.Command, _ []string) error {
			info := buildinfo.Current()
			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.version", info, protocol.WithSource("run"))
		},
	}

	return cmd
}

func init() {
	rootCmd.AddCommand(newVersionCommand())
}
