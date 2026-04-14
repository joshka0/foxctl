package cmd

import (
	"fmt"
	"os"

	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/spf13/cobra"
)

func newSkillsUninstallCommand() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "uninstall <skill-name>",
		Short: "Uninstall a skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.MustFromContext(cmd.Context())
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

			if dryRun {
				result := map[string]any{
					"name":    args[0],
					"path":    skillPath,
					"dry_run": true,
					"message": "Would uninstall skill",
				}
				return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.skills.uninstall", result, protocol.WithSource("run"))
			}

			if err := os.RemoveAll(skillPath); err != nil {
				return fmt.Errorf("failed to uninstall skill: %w", err)
			}
			result := map[string]any{
				"name": args[0],
				"path": skillPath,
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.skills.uninstall", result, protocol.WithSource("run"))
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be uninstalled without making changes")
	return cmd
}
