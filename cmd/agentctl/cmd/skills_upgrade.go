package cmd

import (
	"fmt"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/spf13/cobra"
)

func newSkillsUpgradeCommand() *cobra.Command {
	var manifestPath string
	var binaryPath string
	var modulePath string
	cmd := &cobra.Command{
		Use:   "upgrade <skill-name>",
		Short: "Upgrade an installed skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cmd.Context())
			if err != nil {
				return err
			}
			if _, err := findSkill(cfg, args[0]); err != nil {
				return err
			}
			manifest, err := loadValidatedUpgradeManifest(manifestPath, args[0])
			if err != nil {
				return err
			}
			dest, err := ensureSkillDir(cfg.Paths.Skills, manifest)
			if err != nil {
				return err
			}
			if err := writeManifest(dest, manifestPath); err != nil {
				return err
			}
			if err := writeDistributionArtifacts(dest, manifest, binaryPath, modulePath); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "upgraded %s to version %s\n", manifest.Metadata.Name, manifest.Metadata.Version); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&manifestPath, "manifest", "", "Path to new skill.yaml")
	cmd.Flags().StringVar(&binaryPath, "binary", "", "Path to new skill binary")
	cmd.Flags().StringVar(&modulePath, "module", "", "Path to new WASM module (for wasi skills)")
	if err := cmd.MarkFlagRequired("manifest"); err != nil {
		panic(err)
	}
	return cmd
}
