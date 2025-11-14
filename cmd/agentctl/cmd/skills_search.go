package cmd

import (
	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/envelope"
	"github.com/jkatigb/agentctl/internal/skill"
	"github.com/spf13/cobra"
)

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
			if err != nil {
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
