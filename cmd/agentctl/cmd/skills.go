package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/envelope"
	execrunner "github.com/jkatigb/agentctl/internal/runner/exec"
	"github.com/jkatigb/agentctl/internal/skill"
	"github.com/spf13/cobra"
)

func newSkillsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Manage local skills",
	}
	cmd.AddCommand(
		newSkillsRunCommand(),
		newSkillsInstallCommand(),
		newSkillsListCommand(),
	)
	return cmd
}

func newSkillsRunCommand() *cobra.Command {
	var input string
	var inputFile string
	cmd := &cobra.Command{
		Use:   "run <skill-name>",
		Short: "Run a local skill binary with JSON input",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cmd.Context())
			if err != nil {
				return err
			}
			data, err := loadSkillInput(cmd, input, inputFile)
			if err != nil {
				return err
			}
			manifest, bin, err := findSkill(cfg, args[0])
			if err != nil {
				return err
			}
			runner := execrunner.Runner{Manifest: manifest, Binary: bin}
			stdout, stderr, err := runner.Run(cmd.Context(), data)
			if len(stderr) > 0 {
				if _, werr := cmd.ErrOrStderr().Write(stderr); werr != nil {
					return werr
				}
			}
			if err != nil {
				return err
			}
			return writeEnvelope(cmd.OutOrStdout(), stdout)
		},
	}
	cmd.Flags().StringVar(&input, "input", "", "Inline JSON input (default: {})")
	cmd.Flags().StringVar(&inputFile, "input-file", "", "Path to JSON input file ('-' for stdin)")
	return cmd
}

func init() {
	rootCmd.AddCommand(newSkillsCommand())
}

func newSkillsInstallCommand() *cobra.Command {
	var manifestPath string
	var binaryPath string
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install a skill manifest and binary",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(cmd.Context())
			if err != nil {
				return err
			}
			manifest, err := skill.LoadManifest(manifestPath)
			if err != nil {
				return err
			}
			dest := filepath.Join(cfg.Paths.Skills, manifest.Metadata.Name)
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
			if err := copyFile(manifestPath, filepath.Join(dest, "skill.yaml")); err != nil {
				return err
			}
			if err := copyFile(binaryPath, filepath.Join(dest, "bin")); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "installed %s\n", manifest.Metadata.Name)
			return nil
		},
	}
	cmd.Flags().StringVar(&manifestPath, "manifest", "", "Path to skill.yaml")
	cmd.Flags().StringVar(&binaryPath, "binary", "", "Path to skill binary")
	_ = cmd.MarkFlagRequired("manifest")
	_ = cmd.MarkFlagRequired("binary")
	return cmd
}

func newSkillsListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installed skills",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(cmd.Context())
			if err != nil {
				return err
			}
			manifests, err := skill.Discover(cfg.Paths.Skills)
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("agentctl.skills.list", map[string]any{"skills": summarizeSkills(manifests)}))
		},
	}
	return cmd
}

func summarizeSkills(manifests []skill.Manifest) []map[string]string {
	var out []map[string]string
	for _, m := range manifests {
		out = append(out, map[string]string{
			"name":        m.Metadata.Name,
			"version":     m.Metadata.Version,
			"description": m.Metadata.Description,
		})
	}
	return out
}

func findSkill(cfg config.Config, name string) (skill.Manifest, string, error) {
	installPath := filepath.Join(cfg.Paths.Skills, name, "skill.yaml")
	if manifest, err := skill.LoadManifest(installPath); err == nil {
		return manifest, filepath.Join(cfg.Paths.Skills, name, "bin"), nil
	}
	short := strings.ReplaceAll(strings.ReplaceAll(name, "/", "_"), "-", "_")
	devManifest := filepath.Join("skills", short, "skill.yaml")
	if manifest, err := skill.LoadManifest(devManifest); err == nil {
		bin := filepath.Join("dist", "skills", short, short)
		return manifest, bin, nil
	}
	return skill.Manifest{}, "", fmt.Errorf("skill %s not found (run make skills-build and install)", name)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	return os.Chmod(dst, info.Mode())
}
