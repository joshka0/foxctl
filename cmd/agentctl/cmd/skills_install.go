package cmd

import (
	"path/filepath"

	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/spf13/cobra"
)

func newSkillsInstallCommand() *cobra.Command {
	var manifestPath string
	var binaryPath string
	var modulePath string
	var force bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install a skill manifest and binary",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := config.MustFromContext(cmd.Context())

			installer := skill.NewInstaller(cfg.Paths.Skills)
			handle, err := installer.Install(cmd.Context(), skill.InstallOptions{
				ManifestPath: manifestPath,
				BinaryPath:   binaryPath,
				ModulePath:   modulePath,
				Force:        force,
			})
			if err != nil {
				return err
			}

			// Load manifest to get version for output
			manifest, err := skill.LoadManifest(handle.ManifestPath)
			if err != nil {
				return err
			}

			result := map[string]any{
				"name":    handle.Name,
				"version": manifest.Metadata.Version,
				"path":    filepath.Dir(handle.ManifestPath),
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.skills.install", result, protocol.WithSource("run"))
		},
	}

	cmd.Flags().StringVar(&manifestPath, "manifest", "", "Path to skill.yaml")
	cmd.Flags().StringVar(&binaryPath, "binary", "", "Path to skill binary")
	cmd.Flags().StringVar(&modulePath, "module", "", "Path to WASM module (for wasi skills)")
	cmd.Flags().BoolVar(&force, "force", false, "Force reinstall if skill already exists")
	if err := cmd.MarkFlagRequired("manifest"); err != nil {
		panic(err)
	}
	return cmd
}
