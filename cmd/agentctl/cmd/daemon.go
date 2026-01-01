package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jkatigb/agentctl/internal/daemon"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/spf13/cobra"
)

func newDaemonCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run agentctl as a persistent daemon for faster hook execution",
		Long: `Run agentctl as a persistent daemon for faster hook execution.

The daemon pre-loads configuration, opens database connections, and maintains
a warm gopls instance. Hook scripts can connect via Unix socket for sub-50ms
response times instead of the ~300ms cold start overhead.

Architecture:
  ┌─────────────────────────────────────────────────────────┐
  │                     agentctl daemon                      │
  │  ┌──────────────┐  ┌───────────────┐  ┌──────────────┐ │
  │  │ Config       │  │ SQLite Pool   │  │ gopls        │ │
  │  │ (cached)     │  │ (shared)      │  │ (pre-warmed) │ │
  │  └──────────────┘  └───────────────┘  └──────────────┘ │
  │                           │                             │
  │         Unix Socket: /tmp/agentctl-{uid}.sock           │
  └─────────────────────────────────────────────────────────┘

Usage in hooks:
  # Fast path - use daemon if available
  if agentctl daemon status --quiet; then
    agentctl run hooks/task_guard --daemon --input "$input"
  else
    agentctl run hooks/task_guard --ephemeral --input "$input"
  fi`,
	}
	cmd.AddCommand(
		newDaemonStartCommand(),
		newDaemonStopCommand(),
		newDaemonStatusCommand(),
	)
	return cmd
}

func newDaemonStartCommand() *cobra.Command {
	var background bool
	var workspace string

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the agentctl daemon",
		Long: `Start the agentctl daemon for faster skill execution.

The daemon:
1. Loads configuration once at startup
2. Opens shared database connections (jobs, cache, tasks)
3. Pre-warms gopls for Go codebases
4. Listens on a Unix socket for skill execution requests

Use --background to daemonize (fork to background).

Examples:
  agentctl daemon start
  agentctl daemon start --background
  agentctl daemon start --workspace /path/to/project`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDaemonStart(cmd, background, workspace)
		},
	}

	cmd.Flags().BoolVarP(&background, "background", "b", false, "Run in background (daemonize)")
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "Pre-warm for specific workspace")
	return cmd
}

func newDaemonStopCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the running daemon",
		Long: `Stop the running agentctl daemon gracefully.

This sends a shutdown signal to the daemon and waits for it to
clean up resources (close database connections, stop listeners).

Examples:
  agentctl daemon stop`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDaemonStop(cmd)
		},
	}
	return cmd
}

func newDaemonStatusCommand() *cobra.Command {
	var quiet bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check daemon status",
		Long: `Check if the agentctl daemon is running.

Returns exit code 0 if running, 1 if not.
Use --quiet to suppress output (for shell scripts).

Examples:
  agentctl daemon status
  agentctl daemon status --quiet && echo "Running"`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDaemonStatus(cmd, quiet)
		},
	}

	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "Suppress output (exit code only)")
	return cmd
}

func runDaemonStart(cmd *cobra.Command, background bool, workspace string) error {
	ctx := cmd.Context()

	// Load config
	cfg, ok := config.FromContext(ctx)
	if !ok {
		var err error
		cfg, err = config.Load(ctx)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
	}

	// Check if already running
	client := daemon.NewClient()
	if client.IsRunning() {
		status, err := client.Status()
		if err == nil {
			return writeJSON(cmd.OutOrStdout(), map[string]any{
				"status":  "error",
				"code":    "EALREADY",
				"message": "daemon already running",
				"data": map[string]any{
					"pid":     status.PID,
					"started": status.StartedAt,
					"socket":  daemon.SocketPath(),
				},
			})
		}
	}

	// Background mode - fork and exit
	if background {
		if err := daemon.Daemonize(); err != nil {
			return fmt.Errorf("daemonize: %w", err)
		}
		return writeJSON(cmd.OutOrStdout(), map[string]any{
			"status":  "ok",
			"message": "daemon started in background",
			"data": map[string]any{
				"socket": daemon.SocketPath(),
			},
		})
	}

	// Foreground mode - run the daemon
	svc, err := daemon.NewService(cfg, daemon.ServiceOptions{
		Workspace: workspace,
	})
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start service
	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.Run(ctx)
	}()

	fmt.Fprintf(cmd.ErrOrStderr(), "daemon started on %s\n", daemon.SocketPath())

	// Wait for signal or error
	select {
	case sig := <-sigCh:
		fmt.Fprintf(cmd.ErrOrStderr(), "received %s, shutting down...\n", sig)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return svc.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func runDaemonStop(cmd *cobra.Command) error {
	client := daemon.NewClient()

	if !client.IsRunning() {
		return writeJSON(cmd.OutOrStdout(), map[string]any{
			"status":  "ok",
			"message": "daemon not running",
		})
	}

	if err := client.Shutdown(); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	return writeJSON(cmd.OutOrStdout(), map[string]any{
		"status":  "ok",
		"message": "daemon stopped",
	})
}

func runDaemonStatus(cmd *cobra.Command, quiet bool) error {
	client := daemon.NewClient()

	if !client.IsRunning() {
		if quiet {
			os.Exit(1)
		}
		return writeJSON(cmd.OutOrStdout(), map[string]any{
			"status":  "ok",
			"data": map[string]any{
				"running": false,
				"socket":  daemon.SocketPath(),
			},
		})
	}

	status, err := client.Status()
	if err != nil {
		if quiet {
			os.Exit(1)
		}
		return writeJSON(cmd.OutOrStdout(), map[string]any{
			"status":  "error",
			"code":    "ECONNECT",
			"message": fmt.Sprintf("daemon not responding: %v", err),
		})
	}

	if quiet {
		return nil // Exit 0
	}

	return writeJSON(cmd.OutOrStdout(), map[string]any{
		"status": "ok",
		"data": map[string]any{
			"running":        true,
			"pid":            status.PID,
			"started_at":     status.StartedAt,
			"uptime_seconds": status.UptimeSeconds,
			"requests":       status.RequestCount,
			"socket":         daemon.SocketPath(),
			"warm_workspaces": status.WarmWorkspaces,
		},
	})
}

func writeJSON(w interface{ Write([]byte) (int, error) }, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func init() {
	rootCmd.AddCommand(newDaemonCommand())
}
