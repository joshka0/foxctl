package cmd

import (
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/spf13/cobra"
)

func newJobsExecSkillCommand() *cobra.Command {
	var jobID string
	var manifestPath string
	var artifactPath string
	cmd := &cobra.Command{
		Use:    "exec-skill",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := config.MustFromContext(cmd.Context())
			store, cleanup, err := openJobStore(cmd.Context())
			if err != nil {
				return err
			}
			defer cleanup()
			result, err := store.ExecutePreparedSkill(cmd.Context(), jobID, manifestPath, artifactPath)
			if err != nil {
				return err
			}
			return handleArtifacts(cmd.Context(), cfg, jobID, result)
		},
	}
	cmd.Flags().StringVar(&jobID, "job-id", "", "Job identifier")
	cmd.Flags().StringVar(&manifestPath, "manifest", "", "Path to skill manifest")
	cmd.Flags().StringVar(&artifactPath, "artifact", "", "Path to skill binary/module")
	if err := cmd.MarkFlagRequired("job-id"); err != nil {
		panic(err)
	}
	if err := cmd.MarkFlagRequired("manifest"); err != nil {
		panic(err)
	}
	if err := cmd.MarkFlagRequired("artifact"); err != nil {
		panic(err)
	}
	return cmd
}
