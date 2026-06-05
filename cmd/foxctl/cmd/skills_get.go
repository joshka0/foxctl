package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/internal/domain/skill"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/spf13/cobra"
)

type skillsGuide struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Guide string `json:"guide"`
}

const (
	skillsGetFormatText = "text"
	skillsGetFormatJSON = "json"
)

func newSkillsGetCommand() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "get <skill-name>",
		Short: "Show an AI-friendly skill guide",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			guide, err := buildSkillsGuide(config.MustFromContext(cmd.Context()), args[0])
			if err != nil {
				return err
			}
			switch format {
			case skillsGetFormatText:
				_, err := fmt.Fprintln(cmd.OutOrStdout(), guide.Guide)
				return err
			case skillsGetFormatJSON:
				return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.skills.get", guide, protocol.WithSource("run"))
			default:
				return fmt.Errorf("unsupported format %q (expected %s or %s)", format, skillsGetFormatText, skillsGetFormatJSON)
			}
		},
	}
	cmd.Flags().StringVar(&format, "format", skillsGetFormatText, "Output format: text or json")
	return cmd
}

func buildSkillsGuide(cfg config.Config, name string) (skillsGuide, error) {
	if strings.TrimSpace(name) == "foxctl" {
		return skillsGuide{
			Name:  "foxctl",
			Path:  absolutePath(cfg.Paths.Skills),
			Guide: builtinFoxctlSkillsGuide(),
		}, nil
	}

	handle, manifest, err := resolveSkillManifest(cfg, name)
	if err != nil {
		return skillsGuide{}, skillNotFoundError(name, err)
	}
	dir := filepath.Dir(handle.ManifestPath)
	guide, err := readSkillGuideFile(dir)
	if err != nil {
		return skillsGuide{}, err
	}
	if strings.TrimSpace(guide) == "" {
		guide = generateSkillGuide(manifest)
	}
	return skillsGuide{
		Name:  manifest.Metadata.Name,
		Path:  absolutePath(dir),
		Guide: guide,
	}, nil
}

func resolveSkillManifest(cfg config.Config, requested string) (skill.Handle, skill.Manifest, error) {
	handle, err := createSkillResolver(cfg).Resolve(requested)
	if err != nil {
		return skill.Handle{}, skill.Manifest{}, err
	}
	manifest, err := loadValidatedManifest(handle.ManifestPath)
	if err != nil {
		return skill.Handle{}, skill.Manifest{}, err
	}
	return handle, manifest, nil
}

func readSkillGuideFile(dir string) (string, error) {
	guide, _, err := readSkillGuideFileWithPath(dir)
	return guide, err
}

func readSkillGuideFileWithPath(dir string) (string, string, error) {
	for _, name := range []string{"SKILL.md", "README.md"} {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err == nil {
			return strings.TrimSpace(string(data)), path, nil
		}
		if !os.IsNotExist(err) {
			return "", "", err
		}
	}
	return "", "", nil
}

func generateSkillGuide(m skill.Manifest) string {
	var b bytes.Buffer
	title := m.Metadata.Name
	if title == "" {
		title = m.Signature.Command
	}
	fmt.Fprintf(&b, "# %s\n\n", title)
	if m.Metadata.Description != "" {
		fmt.Fprintf(&b, "%s\n\n", m.Metadata.Description)
	}
	if m.Signature.Help != nil {
		if m.Signature.Help.Short != "" {
			fmt.Fprintf(&b, "%s\n\n", m.Signature.Help.Short)
		}
		if m.Signature.Help.Long != "" {
			fmt.Fprintf(&b, "%s\n\n", strings.TrimSpace(m.Signature.Help.Long))
		}
	}
	command := m.Signature.Command
	if command == "" {
		command = m.Metadata.Name
	}
	if command != "" {
		fmt.Fprintf(&b, "Run with:\n\n```bash\nfoxctl skills run %s", command)
		for _, p := range m.Signature.Parameters {
			if p.Required {
				fmt.Fprintf(&b, " --%s <%s>", p.Name, p.Name)
			}
		}
		fmt.Fprintf(&b, "\n```\n\n")
	}
	if len(m.Signature.Parameters) > 0 {
		b.WriteString("Parameters:\n")
		for _, p := range m.Signature.Parameters {
			required := "optional"
			if p.Required {
				required = "required"
			}
			desc := p.Description
			if desc == "" {
				desc = p.Type
			}
			fmt.Fprintf(&b, "- `%s` (%s): %s\n", p.Name, required, desc)
		}
	}
	return strings.TrimSpace(b.String())
}

func builtinFoxctlSkillsGuide() string {
	return strings.TrimSpace(`# foxctl

foxctl is the local skill runner and agent tooling CLI. Use it to discover installed skills, inspect skill guides, and run skills with JSON envelopes.

Common commands:

` + "```bash" + `
foxctl skills
foxctl skills --compact
foxctl skills search "<task>" --compact
foxctl skills list
foxctl skills get <name>
foxctl skills path [name]
foxctl skills doctor [name] --strict
foxctl skills run <name> --input '{"key":"value"}'
` + "```" + `

Skill I/O uses protocol envelopes. Keep stdout reserved for envelopes when writing skills, send logs to stderr, keep WASI skills isolated with ` + "`network: \"none\"`" + `, and run ` + "`foxctl skills doctor --strict`" + ` when guide or manifest behavior changes.`)
}

func absolutePath(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return abs
}

// skillNotFoundError wraps a skill resolution error with a concise next-step hint.
func skillNotFoundError(name string, err error) error {
	return fmt.Errorf("skill %q not found: %w; run \"foxctl skills\" to list available skills", name, err)
}
