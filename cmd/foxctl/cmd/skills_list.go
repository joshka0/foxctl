package cmd

import (
	"os"

	"github.com/joshka0/foxctl/internal/domain/skill"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/spf13/cobra"
)

func newSkillsListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installed skills",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := config.MustFromContext(cmd.Context())
			manifests, err := skill.Discover(cfg.Paths.Skills)
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.skills.list", map[string]any{"skills": summarizeSkills(manifests)}, protocol.WithSource("run"))
		},
	}
	return cmd
}
