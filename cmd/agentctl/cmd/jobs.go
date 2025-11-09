package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"

	"github.com/jkatigb/agentctl/internal/envelope"
	"github.com/jkatigb/agentctl/internal/jobs"
	"github.com/spf13/cobra"
)

func newJobsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "jobs",
		Short: "Manage agentctl jobs",
	}
	cmd.AddCommand(
		newJobsSubmitCommand(),
		newJobsListCommand(),
		newJobsStatusCommand(),
		newJobsResultCommand(),
		newJobsCancelCommand(),
		newJobsRemoveCommand(),
		newJobsExecSkillCommand(),
	)
	return cmd
}

func init() {
	rootCmd.AddCommand(newJobsCommand())
}

func newJobsSubmitCommand() *cobra.Command {
	var message string
	cmd := &cobra.Command{
		Use:   "submit",
		Short: "Submit a sample echo job",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, cleanup, err := openJobStore(cmd.Context())
			if err != nil {
				return err
			}
			defer cleanup()

			job, err := store.SubmitEcho(cmd.Context(), message)
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("agentctl.jobs.submit", job))
		},
	}
	cmd.Flags().StringVar(&message, "message", "hello world", "Message to echo")
	return cmd
}

func newJobsListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List recent jobs",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, cleanup, err := openJobStore(cmd.Context())
			if err != nil {
				return err
			}
			defer cleanup()

			jobsList, err := store.List(cmd.Context(), 50)
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("agentctl.jobs.list", map[string]any{"jobs": jobsList}))
		},
	}
	return cmd
}

func newJobsStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status <job-id>",
		Short: "Show job status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, cleanup, err := openJobStore(cmd.Context())
			if err != nil {
				return err
			}
			defer cleanup()

			job, err := store.Get(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("agentctl.jobs.status", job))
		},
	}
	return cmd
}

func newJobsResultCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "result <job-id>",
		Short: "Print the job result envelope",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, cleanup, err := openJobStore(cmd.Context())
			if err != nil {
				return err
			}
			defer cleanup()

			data, err := store.Result(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if _, err := out.Write(bytes.TrimSpace(data)); err != nil {
				return err
			}
			if _, err := out.Write([]byte("\n")); err != nil {
				return err
			}
			return nil
		},
	}
	return cmd
}

func newJobsCancelCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cancel <job-id>",
		Short: "Cancel a queued or running job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, cleanup, err := openJobStore(cmd.Context())
			if err != nil {
				return err
			}
			defer cleanup()

			if err := store.Cancel(cmd.Context(), args[0]); err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("agentctl.jobs.cancel", map[string]string{"id": args[0]}))
		},
	}
	return cmd
}

func newJobsRemoveCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rm <job-id>",
		Short: "Remove a job and its artifacts",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := commandConfig(cmd.Context())
			if err != nil {
				return err
			}
			store, cleanup, err := openJobStore(cmd.Context())
			if err != nil {
				return err
			}
			defer cleanup()
			if err := releaseArtifacts(cmd.Context(), cfg, args[0]); err != nil {
				return err
			}
			if err := os.RemoveAll(filepath.Join(cfg.Paths.Jobs, args[0])); err != nil {
				return err
			}
			if err := store.Delete(cmd.Context(), args[0]); err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("agentctl.jobs.rm", map[string]string{"id": args[0]}))
		},
	}
	return cmd
}

func openJobStore(ctx context.Context) (*jobs.Store, func(), error) {
	cfg, err := commandConfig(ctx)
	if err != nil {
		return nil, nil, err
	}
	store, err := jobs.Open(ctx, cfg.Paths.Jobs)
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() {
		_ = store.Close()
	}
	return store, cleanup, nil
}
