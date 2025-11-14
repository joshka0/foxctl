package cmd

import (
	"fmt"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/spf13/cobra"
)

func newSkillsInstallCommand() *cobra.Command {
	var manifestPath string
	var binaryPath string
	var modulePath string
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install a skill manifest and binary",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(cmd.Context())
			if err != nil {
				return err
			}
			manifest, err := loadValidatedManifest(manifestPath)
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
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "installed %s\n", manifest.Metadata.Name); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&manifestPath, "manifest", "", "Path to skill.yaml")
	cmd.Flags().StringVar(&binaryPath, "binary", "", "Path to skill binary")
	cmd.Flags().StringVar(&modulePath, "module", "", "Path to WASM module (for wasi skills)")
	if err := cmd.MarkFlagRequired("manifest"); err != nil {
		panic(err)
	}
	return cmd
}
