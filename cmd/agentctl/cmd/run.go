package cmd

import (
	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/runservice"
	"github.com/spf13/cobra"
)

func newRunCommand() *cobra.Command {
	var flags runCommandFlags
	cmd := &cobra.Command{
		Use:   "run <skill-name>",
		Short: "Run a skill and record the result as a job",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return executeRunCommand(cmd, args, flags)
		},
	}
	bindRunFlags(cmd, &flags)
	return cmd
}

func init() {
	rootCmd.AddCommand(newRunCommand())
}

func executeRunCommand(cmd *cobra.Command, args []string, flags runCommandFlags) error {
	cfg, err := config.Load(cmd.Context())
	if err != nil {
		return err
	}
	data, err := loadSkillInput(cmd, flags.Input, flags.InputFile)
	if err != nil {
		return err
	}
	handle, err := findSkill(cfg, args[0])
	if err != nil {
		return err
	}
	opts, err := buildRunOptions(cfg, args[0], flags, data)
	if err != nil {
		return err
	}
	executor := runservice.NewExecutor(cmd.Context(), cfg, handle, cmd.OutOrStdout(), cmd.ErrOrStderr(), opts)
	executor.SetAsyncRunner(defaultAsyncRunner)
	defer executor.Close()

	if done, err := executor.TryServeCache(data); err != nil {
		return err
	} else if done {
		return nil
	}

	job, isDuplicate, err := executor.PrepareJob(data)
	if err != nil {
		return err
	}
	if isDuplicate {
		return executor.HandleDuplicate(job)
	}
	if opts.Async {
		return executor.SubmitAsync(job)
	}
	return executor.ExecuteSync(job)
}
