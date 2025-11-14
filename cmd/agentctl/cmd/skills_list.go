package cmd

import (
	"os"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/spf13/cobra"
)

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
