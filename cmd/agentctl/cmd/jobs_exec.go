package cmd

import (
	"github.com/jkatigb/agentctl/internal/config"
	"github.com/spf13/cobra"
)

func newJobsExecSkillCommand() *cobra.Command {
	var jobID string
	var binPath string
	cmd := &cobra.Command{
		Use:    "exec-skill",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(cmd.Context())
			if err != nil {
				return err
			}
			store, cleanup, err := openJobStore(cmd.Context())
			if err != nil {
				return err
			}
			defer cleanup()
			result, err := store.ExecutePreparedSkill(cmd.Context(), jobID, binPath)
			if err != nil {
				return err
			}
			return handleArtifacts(cmd.Context(), cfg, jobID, result)
		},
	}
	cmd.Flags().StringVar(&jobID, "job-id", "", "Job identifier")
	cmd.Flags().StringVar(&binPath, "bin", "", "Skill binary path")
	_ = cmd.MarkFlagRequired("job-id")
	_ = cmd.MarkFlagRequired("bin")
	return cmd
}
