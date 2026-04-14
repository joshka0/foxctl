package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/runtime/daemon"
	"github.com/jkatigb/agentctl/internal/runtime/runservice"
	"github.com/spf13/cobra"
)

type daemonClient interface {
	IsRunning() bool
	Run(skill string, input []byte, workspace string, ephemeral bool) (*daemon.RunResult, error)
}

var defaultNewDaemonClient = func() daemonClient { return daemon.NewClient() }

var newDaemonClient = defaultNewDaemonClient

func newRunCommand() *cobra.Command {
	var flags runCommandFlags
	cmd := &cobra.Command{
		Use:   "run <skill-name>",
		Short: "Run a skill and record the result as a job",
		Args: func(cmd *cobra.Command, args []string) error {
			showExamples, err := cmd.Flags().GetBool("examples")
			if err != nil {
				return err
			}
			if showExamples {
				if len(args) > 1 {
					return fmt.Errorf("expected zero or one skill name when using --examples")
				}
				return nil
			}
			return cobra.MinimumNArgs(1)(cmd, args)
		},
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
	if flags.Examples {
		return writeRunExamples(cmd, args)
	}
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

	// Setup output formatter if --format or --jq specified
	formatter := NewOutputFormatter(cmd.OutOrStdout(), flags.Format, flags.JQ)
	stdout := formatter.Writer()

	// Daemon mode: execute via persistent daemon for faster hook execution
	if flags.Daemon {
		err := executeViaDaemonWithWriter(ctx, cmd, skillName, data, opts, stdout)
		if flushErr := formatter.Flush(); flushErr != nil && err == nil {
			return flushErr
		}
		return err
	}

	executor := runservice.NewExecutor(ctx, cfg, handle, stdout, cmd.ErrOrStderr(), opts)
	executor.SetAsyncRunner(defaultAsyncRunner)
	defer executor.Close()

	// Ephemeral mode: skip job persistence for faster execution
	if opts.Ephemeral {
		if err := executor.ExecuteEphemeral(data); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return writeTimeoutError(cmd, skillName, timeout)
			}
			return err
		}
		return formatter.Flush()
	}

	if done, err := executor.TryServeCache(data); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return writeTimeoutError(cmd, skillName, timeout)
		}
		return err
	} else if done {
		return formatter.Flush()
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
		return formatter.Flush()
	}
	if opts.Async {
		if err := executor.SubmitAsync(job); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return writeTimeoutError(cmd, skillName, timeout)
			}
			return err
		}
		return formatter.Flush()
	}
	if err := executor.ExecuteSync(job); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return writeTimeoutError(cmd, skillName, timeout)
		}
		return err
	}

	// Flush formatter to apply any transformations
	return formatter.Flush()
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

// executeViaDaemonWithWriter runs a skill via the daemon with a custom writer.
// Falls back to normal execution if the daemon is not available.
func executeViaDaemonWithWriter(ctx context.Context, cmd *cobra.Command, skillName string, input []byte, opts runservice.RunOptions, stdout io.Writer) error {
	client := newDaemonClient()

	// Check if daemon is running
	if !client.IsRunning() {
		// Fall back to ephemeral mode with warning
		fmt.Fprintf(cmd.ErrOrStderr(), "daemon not running, falling back to ephemeral mode\n")
		opts.Ephemeral = true
		cfg := config.MustFromContext(cmd.Context())
		handle, err := findSkill(cfg, skillName)
		if err != nil {
			return err
		}
		// Use the timeout-wrapped context for fallback execution
		executor := runservice.NewExecutor(ctx, cfg, handle, stdout, cmd.ErrOrStderr(), opts)
		defer executor.Close()
		return executor.ExecuteEphemeral(input)
	}

	// Execute via daemon
	result, err := client.Run(skillName, input, opts.Workspace, opts.Ephemeral)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "daemon execution failed, falling back to ephemeral mode\n")
		opts.Ephemeral = true
		cfg := config.MustFromContext(cmd.Context())
		handle, findErr := findSkill(cfg, skillName)
		if findErr != nil {
			return findErr
		}
		executor := runservice.NewExecutor(ctx, cfg, handle, stdout, cmd.ErrOrStderr(), opts)
		defer executor.Close()
		return executor.ExecuteEphemeral(input)
	}

	var env envelope.Envelope
	if unmarshalErr := json.Unmarshal(result.Output, &env); unmarshalErr != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "daemon returned invalid output, falling back to ephemeral mode\n")
		opts.Ephemeral = true
		cfg := config.MustFromContext(cmd.Context())
		handle, findErr := findSkill(cfg, skillName)
		if findErr != nil {
			return findErr
		}
		executor := runservice.NewExecutor(ctx, cfg, handle, stdout, cmd.ErrOrStderr(), opts)
		defer executor.Close()
		return executor.ExecuteEphemeral(input)
	}
	if validateErr := protocol.Validate(env); validateErr != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "daemon returned invalid envelope, falling back to ephemeral mode\n")
		opts.Ephemeral = true
		cfg := config.MustFromContext(cmd.Context())
		handle, findErr := findSkill(cfg, skillName)
		if findErr != nil {
			return findErr
		}
		executor := runservice.NewExecutor(ctx, cfg, handle, stdout, cmd.ErrOrStderr(), opts)
		defer executor.Close()
		return executor.ExecuteEphemeral(input)
	}

	_, err = stdout.Write(result.Output)
	return err
}
