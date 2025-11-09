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
			bin, err := resolveSkillBinary(args[0])
			if err != nil {
				return err
			}

			store, cleanup, err := openJobStore(cmd.Context())
			if err != nil {
				return err
			}
			defer cleanup()
			if async {
				job, err := store.PrepareSkillJob(cmd.Context(), args[0], data)
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
			job, result, runErr := store.RunSkill(cmd.Context(), args[0], bin, data)
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
	return cmd
}

func init() {
	rootCmd.AddCommand(newRunCommand())
}
