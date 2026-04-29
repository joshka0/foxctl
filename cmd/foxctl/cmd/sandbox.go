package cmd

import (
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/spf13/cobra"
)

func newSandboxCommand() *cobra.Command {
	sandboxCmd := &cobra.Command{
		Use:   "sandbox",
		Short: "Sandbox integration commands",
		Long:  "Sandbox planning and integration commands.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return writeErrorEnvelope(cmd, "sandbox", string(protocol.ErrorCodeEARG), "sandbox subcommand is required")
		},
	}

	sandboxCmd.AddCommand(newSandboxSmolVMCommand())
	sandboxCmd.AddCommand(newSandboxOpenSandboxCommand())

	return sandboxCmd
}

func newSandboxOpenSandboxCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "opensandbox",
		Short: "OpenSandbox integration (disabled)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return writeErrorEnvelope(cmd, "sandbox/opensandbox", string(protocol.ErrorCodeERuntime), openSandboxDisabledMessage)
		},
	}
}

func init() {
	rootCmd.AddCommand(newSandboxCommand())
}
