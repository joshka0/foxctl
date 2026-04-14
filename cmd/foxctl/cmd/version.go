package cmd

import (
	"github.com/joshka0/foxctl/internal/platform/buildinfo"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/spf13/cobra"
)

func newVersionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print build metadata",
		RunE: func(cmd *cobra.Command, _ []string) error {
			info := buildinfo.Current()
			return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.version", info, protocol.WithSource("run"))
		},
	}

	return cmd
}

func init() {
	rootCmd.AddCommand(newVersionCommand())
}
