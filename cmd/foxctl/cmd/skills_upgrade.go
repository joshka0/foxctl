package cmd

import (
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/protocol"
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
			cfg := config.MustFromContext(cmd.Context())
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
			result := map[string]any{
				"name":    manifest.Metadata.Name,
				"version": manifest.Metadata.Version,
				"path":    dest,
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.skills.upgrade", result, protocol.WithSource("run"))
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
