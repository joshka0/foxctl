package cmd

import (
	"github.com/joshka0/foxctl/internal/platform/config"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
	runtimejobs "github.com/joshka0/foxctl/internal/runtime/jobs"
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
			store, err := runtimejobs.OpenSkillStore(
				cmd.Context(),
				cfg.Paths.Jobs,
				runtimejobs.WithSkillStoreCASPath(cfg.Paths.CAS),
			)
			if err != nil {
				return err
			}
			defer func() {
				errs.Ignore(store.Close(), "close executable job store")
			}()
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
