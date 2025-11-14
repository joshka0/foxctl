package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jkatigb/agentctl/internal/platform/config"
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
			handle, err := findSkill(cfg, args[0])
			if err != nil {
				return err
			}
			skillPath := filepath.Dir(handle.ManifestPath)
			if err := os.RemoveAll(skillPath); err != nil {
				return fmt.Errorf("failed to uninstall skill: %w", err)
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "uninstalled %s\n", args[0]); err != nil {
				return err
			}
			return nil
		},
	}
	return cmd
}
