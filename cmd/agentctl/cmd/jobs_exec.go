package cmd

import (
	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/skill"
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
			cfg, err := config.Load(cmd.Context())
			if err != nil {
				return err
			}
			store, cleanup, err := openJobStore(cmd.Context())
			if err != nil {
				return err
			}
			defer cleanup()
			manifest, err := skill.LoadManifest(manifestPath)
			if err != nil {
				return err
			}
			result, err := store.ExecutePreparedSkill(cmd.Context(), jobID, manifest, artifactPath)
			if err != nil {
				return err
			}
			return handleArtifacts(cmd.Context(), cfg, jobID, result)
		},
	}
	cmd.Flags().StringVar(&jobID, "job-id", "", "Job identifier")
	cmd.Flags().StringVar(&manifestPath, "manifest", "", "Path to skill manifest")
	cmd.Flags().StringVar(&artifactPath, "artifact", "", "Path to skill binary/module")
	_ = cmd.MarkFlagRequired("job-id")
	_ = cmd.MarkFlagRequired("manifest")
	_ = cmd.MarkFlagRequired("artifact")
	return cmd
}
