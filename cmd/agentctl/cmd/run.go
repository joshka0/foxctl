package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/jkatigb/agentctl/internal/config"
	"github.com/spf13/cobra"
)

func newRunCommand() *cobra.Command {
	var input string
	var inputFile string
	var async bool
	var dedupe bool
	cmd := &cobra.Command{
		Use:   "run <skill-name>",
		Short: "Run a skill and record the result as a job",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cmd.Context())
			if err != nil {
				return err
			}
			data, err := loadSkillInput(cmd, input, inputFile)
			if err != nil {
				return err
			}
			manifest, bin, err := findSkill(cfg, args[0])
			if err != nil {
				return err
			}
			skillName := manifest.Metadata.Name

			store, cleanup, err := openJobStore(cmd.Context())
			if err != nil {
				return err
			}
			defer cleanup()

			// Check for duplicate job if --dedupe is enabled
			if dedupe {
				job, err := store.PrepareSkillJob(cmd.Context(), skillName, data)
				if err != nil {
					return err
				}
				existing, dupErr := store.FindDuplicateJob(cmd.Context(), job.ArgsHash)
				if dupErr == nil {
					// Found duplicate, use existing job
					fmt.Fprintf(cmd.ErrOrStderr(), "using existing job %s (deduplicated)\n", existing.ID)
					if async {
						fmt.Fprintf(cmd.OutOrStdout(), "job %s (existing)\n", existing.ID)
						return nil
					}
					// For sync mode, return existing result if available
					if existing.ResultPath != "" {
						result, err := store.Result(cmd.Context(), existing.ID)
						if err != nil {
							return err
						}
						return writeEnvelope(cmd.OutOrStdout(), result)
					}
					// Wait for existing job to complete
					existing, err = store.WaitForCompletion(cmd.Context(), existing.ID, 0)
					if err != nil {
						return err
					}
					result, err := store.Result(cmd.Context(), existing.ID)
					if err != nil {
						return err
					}
					return writeEnvelope(cmd.OutOrStdout(), result)
				}
				// No duplicate found, continue with prepared job
				if async {
					worker := exec.CommandContext(cmd.Context(), os.Args[0], "jobs", "exec-skill", "--job-id", job.ID, "--bin", bin)
					worker.Stdout = cmd.ErrOrStderr()
					worker.Stderr = cmd.ErrOrStderr()
					if err := worker.Start(); err != nil {
						return err
					}
					fmt.Fprintf(cmd.OutOrStdout(), "job %s submitted\n", job.ID)
					return nil
				}
				// For sync, execute the prepared job
				result, execErr := store.ExecutePreparedSkill(cmd.Context(), job.ID, bin)
				if execErr != nil {
					return execErr
				}
				job, _ = store.Get(cmd.Context(), job.ID)
				if err := handleArtifacts(cmd.Context(), cfg, job.ID, result); err != nil {
					return err
				}
				if err := writeEnvelope(cmd.OutOrStdout(), result); err != nil {
					return err
				}
				logger := cmd.ErrOrStderr()
				if _, err := logger.Write([]byte("job " + job.ID + " state " + string(job.State) + "\n")); err != nil {
					return err
				}
				return nil
			}

			if async {
				job, err := store.PrepareSkillJob(cmd.Context(), skillName, data)
				if err != nil {
					return err
				}
				worker := exec.CommandContext(cmd.Context(), os.Args[0], "jobs", "exec-skill", "--job-id", job.ID, "--bin", bin)
				worker.Stdout = cmd.ErrOrStderr()
				worker.Stderr = cmd.ErrOrStderr()
				if err := worker.Start(); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "job %s submitted\n", job.ID)
				return nil
			}
			job, result, runErr := store.RunSkill(cmd.Context(), skillName, bin, data)
			if runErr != nil {
				return runErr
			}
			if err := handleArtifacts(cmd.Context(), cfg, job.ID, result); err != nil {
				return err
			}
			if err := writeEnvelope(cmd.OutOrStdout(), result); err != nil {
				return err
			}
			logger := cmd.ErrOrStderr()
			if _, err := logger.Write([]byte("job " + job.ID + " state " + string(job.State) + "\n")); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&input, "input", "", "Inline JSON input (default: {})")
	cmd.Flags().StringVar(&inputFile, "input-file", "", "Path to JSON input file ('-' for stdin)")
	cmd.Flags().BoolVar(&async, "async", false, "Submit job and return immediately")
	cmd.Flags().BoolVar(&dedupe, "dedupe", false, "Reuse existing job with same args_hash")
	return cmd
}

func init() {
	rootCmd.AddCommand(newRunCommand())
}
