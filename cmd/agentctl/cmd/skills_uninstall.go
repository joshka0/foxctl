package cmd

import (
	"fmt"
	"os"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/spf13/cobra"
)

func newSkillsUninstallCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall <skill-name>",
		Short: "Uninstall a skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cmd.Context())
			if err != nil {
				return err
			}
			// Resolve skill directory path without requiring a valid manifest
			// This allows uninstalling corrupted or partially installed skills
			skillPath, err := skillDirPath(cfg.Paths.Skills, args[0])
			if err != nil {
				return err
			}
			if _, err := os.Stat(skillPath); os.IsNotExist(err) {
				return fmt.Errorf("skill %s is not installed", args[0])
			} else if err != nil {
				return fmt.Errorf("failed to check skill directory: %w", err)
			}
			if err := os.RemoveAll(skillPath); err != nil {
				return fmt.Errorf("failed to uninstall skill: %w", err)
			}
			result := map[string]any{
				"name": args[0],
				"path": skillPath,
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.skills.uninstall", result, protocol.WithSource("run"))
		},
	}
	return cmd
}
