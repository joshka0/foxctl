package cmd

import (
	"fmt"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/protocol"
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
	data, err := loadSkillInput(cmd, cfg, flags.Input, flags.InputFile)
	if err != nil {
		return err
	}

	skillName := args[0]
	handle, err := findSkill(cfg, skillName)
	if err != nil {
		return err
	}

	opts, err := buildRunOptions(cfg, skillName, flags, data)
	if err != nil {
		return writeRunValidationError(cmd, skillName, err)
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

func writeRunValidationError(cmd *cobra.Command, skill string, cause error) error {
	msg := cause.Error()

	data := protocol.ValidationErrorData{
		Reason: msg,
		Hint: fmt.Sprintf(
			"Check your run flags and skill parameters. For full input schema and workflows, run: agentctl skills help %s --json",
			skill,
		),
		Context: map[string]any{
			"command": cmd.CommandPath(),
			"skill":   skill,
		},
	}

	env := protocol.ValidationError("agentctl.run", msg, data, protocol.WithSource("cli"))
	if err := protocol.Write(cmd.OutOrStdout(), env); err != nil {
		return fmt.Errorf("write run validation error envelope: %w", err)
	}

	// Keep non-zero exit behavior.
	return cause
}
