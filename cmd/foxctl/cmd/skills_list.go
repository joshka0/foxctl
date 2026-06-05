package cmd

import (
	"fmt"
	"os"

	"github.com/joshka0/foxctl/internal/domain/skill"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/spf13/cobra"
)

func newSkillsListCommand() *cobra.Command {
	var compact bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installed skills",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSkillsListCommand(cmd, compact)
		},
	}
	cmd.Flags().BoolVar(&compact, "compact", false, "Print compact agent-readable lines instead of a JSON envelope")
	return cmd
}

func runSkillsListCommand(cmd *cobra.Command, compact bool) error {
	cfg := config.MustFromContext(cmd.Context())
	manifests, err := skill.Discover(cfg.Paths.Skills)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	summaries := summarizeSkills(manifests)
	if compact {
		for _, s := range summaries {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", s["name"], s["version"], s["description"]); err != nil {
				return err
			}
		}
		return nil
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.skills.list", map[string]any{"skills": summaries}, protocol.WithSource("run"))
}
