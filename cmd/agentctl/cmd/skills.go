package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/envelope"
	"github.com/jkatigb/agentctl/internal/policy"
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
		newSkillsDescribeCommand(),
		newSkillsSearchCommand(),
		newSkillsUninstallCommand(),
		newSkillsUpgradeCommand(),
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
			handle, err := findSkill(cfg, args[0])
			if err != nil {
				return err
			}
			stdout, stderr, err := executeSkill(cmd.Context(), handle.Manifest, handle.ArtifactPath, data)
			if len(stderr) > 0 {
				if _, werr := cmd.ErrOrStderr().Write(append(stderr, '\n')); werr != nil {
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
	var modulePath string
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
			// Validate WASI network policy at install time
			if err := policy.ValidateWASIPolicy(manifest); err != nil {
				return err
			}
			dest := filepath.Join(cfg.Paths.Skills, manifest.Metadata.Name)
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
			if err := copyFile(manifestPath, filepath.Join(dest, "skill.yaml")); err != nil {
				return err
			}
			switch manifest.Distribution.Type {
			case "exec":
				if binaryPath == "" {
					return fmt.Errorf("--binary is required for exec skills")
				}
				if err := copyFile(binaryPath, filepath.Join(dest, "bin")); err != nil {
					return err
				}
			case "wasi":
				if modulePath == "" {
					return fmt.Errorf("--module is required for wasi skills")
				}
				if err := copyFile(modulePath, filepath.Join(dest, "module.wasm")); err != nil {
					return err
				}
			default:
				return fmt.Errorf("unsupported distribution: %s", manifest.Distribution.Type)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "installed %s\n", manifest.Metadata.Name)
			return nil
		},
	}
	cmd.Flags().StringVar(&manifestPath, "manifest", "", "Path to skill.yaml")
	cmd.Flags().StringVar(&binaryPath, "binary", "", "Path to skill binary")
	cmd.Flags().StringVar(&modulePath, "module", "", "Path to WASM module (for wasi skills)")
	_ = cmd.MarkFlagRequired("manifest")
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

func newSkillsDescribeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "describe <skill-name>",
		Short: "Show detailed information about a skill",
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
			m := handle.Manifest
			details := map[string]any{
				"name":         m.Metadata.Name,
				"version":      m.Metadata.Version,
				"description":  m.Metadata.Description,
				"tags":         m.Metadata.Tags,
				"distribution": m.Distribution.Type,
				"command":      m.Signature.Command,
				"parameters":   m.Signature.Parameters,
				"returns":      m.Signature.Returns,
				"capabilities": map[string]any{
					"network":     m.Capabilities.Network,
					"egressAllow": m.Capabilities.EgressAllow,
					"filesystem":  m.Capabilities.Filesystem,
					"pure":        m.Capabilities.Pure,
				},
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("agentctl.skills.describe", details))
		},
	}
	return cmd
}

func newSkillsSearchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search for skills by name, description, or tags",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cmd.Context())
			if err != nil {
				return err
			}
			query := args[0]
			manifests, err := skill.Discover(cfg.Paths.Skills)
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			var matches []skill.Manifest
			for _, m := range manifests {
				if matchesQuery(m, query) {
					matches = append(matches, m)
				}
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("agentctl.skills.search", map[string]any{"skills": summarizeSkills(matches)}))
		},
	}
	return cmd
}

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
			fmt.Fprintf(cmd.OutOrStdout(), "uninstalled %s\n", args[0])
			return nil
		},
	}
	return cmd
}

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
			// Verify skill exists
			_, err = findSkill(cfg, args[0])
			if err != nil {
				return err
			}
			// Load new manifest
			manifest, err := skill.LoadManifest(manifestPath)
			if err != nil {
				return err
			}
			// Verify name matches
			if manifest.Metadata.Name != args[0] {
				return fmt.Errorf("manifest name %q does not match skill name %q", manifest.Metadata.Name, args[0])
			}
			// Validate WASI network policy
			if err := policy.ValidateWASIPolicy(manifest); err != nil {
				return err
			}
			dest := filepath.Join(cfg.Paths.Skills, manifest.Metadata.Name)
			// Copy new manifest
			if err := copyFile(manifestPath, filepath.Join(dest, "skill.yaml")); err != nil {
				return err
			}
			// Copy new artifact
			switch manifest.Distribution.Type {
			case "exec":
				if binaryPath == "" {
					return fmt.Errorf("--binary is required for exec skills")
				}
				if err := copyFile(binaryPath, filepath.Join(dest, "bin")); err != nil {
					return err
				}
			case "wasi":
				if modulePath == "" {
					return fmt.Errorf("--module is required for wasi skills")
				}
				if err := copyFile(modulePath, filepath.Join(dest, "module.wasm")); err != nil {
					return err
				}
			default:
				return fmt.Errorf("unsupported distribution: %s", manifest.Distribution.Type)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "upgraded %s to version %s\n", manifest.Metadata.Name, manifest.Metadata.Version)
			return nil
		},
	}
	cmd.Flags().StringVar(&manifestPath, "manifest", "", "Path to new skill.yaml")
	cmd.Flags().StringVar(&binaryPath, "binary", "", "Path to new skill binary")
	cmd.Flags().StringVar(&modulePath, "module", "", "Path to new WASM module (for wasi skills)")
	_ = cmd.MarkFlagRequired("manifest")
	return cmd
}

func matchesQuery(m skill.Manifest, query string) bool {
	// Simple case-insensitive substring matching
	q := filepath.Base(query)
	if filepath.Base(m.Metadata.Name) == q {
		return true
	}
	if m.Metadata.Description != "" && contains(m.Metadata.Description, query) {
		return true
	}
	for _, tag := range m.Metadata.Tags {
		if contains(tag, query) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		len(s) > len(substr) && anySubstring(s, substr))
}

func anySubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if matchesAt(s, substr, i) {
			return true
		}
	}
	return false
}

func matchesAt(s, substr string, offset int) bool {
	for i := 0; i < len(substr); i++ {
		if toLower(s[offset+i]) != toLower(substr[i]) {
			return false
		}
	}
	return true
}

func toLower(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}
