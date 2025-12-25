package cmd

import (
	"context"
	"fmt"
	"time"

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
	cfg := config.MustFromContext(cmd.Context())
	data, err := loadSkillInput(cmd, cfg, flags.Input, flags.InputFile)
	if err != nil {
		return err
	}

	skillName := args[0]
	handle, err := findSkill(cfg, skillName)
	if err != nil {
		return err
	}

	// Track whether the user explicitly set --workspace (including empty string)
	flags.WorkspaceSet = cmd.Flags().Changed("workspace")

	opts, err := buildRunOptions(cfg, skillName, flags, data)
	if err != nil {
		return writeRunValidationError(cmd, skillName, err)
	}
	if opts.CLICommand == "" {
		opts.CLICommand = fmt.Sprintf("%s %s", cmd.CommandPath(), skillName)
	}

	// Apply timeout to context (default 2m if not specified)
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = runservice.DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
	defer cancel()

	executor := runservice.NewExecutor(ctx, cfg, handle, cmd.OutOrStdout(), cmd.ErrOrStderr(), opts)
	executor.SetAsyncRunner(defaultAsyncRunner)
	defer executor.Close()

	if done, err := executor.TryServeCache(data); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return writeTimeoutError(cmd, skillName, timeout)
		}
		return err
	} else if done {
		return nil
	}

	job, isDuplicate, err := executor.PrepareJob(data)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return writeTimeoutError(cmd, skillName, timeout)
		}
		return err
	}
	if isDuplicate {
		if err := executor.HandleDuplicate(job); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return writeTimeoutError(cmd, skillName, timeout)
			}
			return err
		}
		return nil
	}
	if opts.Async {
		if err := executor.SubmitAsync(job); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return writeTimeoutError(cmd, skillName, timeout)
			}
			return err
		}
		return nil
	}
	if err := executor.ExecuteSync(job); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return writeTimeoutError(cmd, skillName, timeout)
		}
		return err
	}
	return nil
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

func writeTimeoutError(cmd *cobra.Command, skill string, timeout time.Duration) error {
	msg := fmt.Sprintf("execution timed out after %s", timeout)

	data := map[string]any{
		"skill":   skill,
		"timeout": timeout.String(),
		"hint":    "Increase timeout with --timeout flag (e.g., --timeout 5m) or check if there are stuck jobs with: sqlite3 ~/.agentctl/jobs/jobs.db \"SELECT id,command,state FROM jobs WHERE state='running'\"",
	}

	env := protocol.Error("agentctl.run", protocol.ErrorCodeETimeout, msg, data, protocol.WithSource("cli"))
	if err := protocol.Write(cmd.OutOrStdout(), env); err != nil {
		return fmt.Errorf("write timeout error envelope: %w", err)
	}

	return context.DeadlineExceeded
}
