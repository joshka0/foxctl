package cmd

import (
	"os"

	"github.com/joshka0/foxctl/internal/domain/skill"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/spf13/cobra"
)

func newSkillsSearchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search for skills by name, description, or tags",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.MustFromContext(cmd.Context())
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
			return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.skills.search", map[string]any{"skills": summarizeSkills(matches)}, protocol.WithSource("run"))
		},
	}
	return cmd
}
