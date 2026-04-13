package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/fsnotify/fsnotify"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/runtime/observability"
	"github.com/jkatigb/agentctl/internal/storage/testwatch"
	testwatchrunner "github.com/jkatigb/agentctl/internal/tooling/testwatch"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

func newWatchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Run background watchers",
		Long: `Run background watchers for the workspace.

Watchers monitor files and trigger actions when changes occur.`,
	}
	cmd.AddCommand(
		newWatchTestsCommand(),
	)
	return cmd
}

func newWatchTestsCommand() *cobra.Command {
	var workspaceDir string
	var once bool
	var statusOnly bool

	cmd := &cobra.Command{
		Use:   "tests",
		Short: "Run test watchers for the workspace",
		Long: `Start test watchers that run test commands when relevant files change.

Configuration is read from .agentctl/test-watch.yaml in the workspace root.
Use 'agentctl test-watch add' to configure watchers.

Modes:
  - Default: Watch for file changes and run tests as configured
  - --once: Run all watchers once and exit
  - --status-only: Print current test status and exit`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			// No-op logger for runner internals - we emit via observability
			log := zerolog.New(io.Discard) //nolint:forbidigo // no-op logger for runner internals

			// Determine workspace root
			var err error
			if workspaceDir == "" {
				workspaceDir, err = os.Getwd()
				if err != nil {
					return fmt.Errorf("get working directory: %w", err)
				}
			}

			// Resolve to absolute path
			workspaceDir, err = filepath.Abs(workspaceDir)
			if err != nil {
				return fmt.Errorf("resolve workspace path: %w", err)
			}

			// Derive workspace ID from path
			workspaceID := deriveWorkspaceID(workspaceDir)

			// Load config
			cfg, err := loadConfig(ctx)
			if err != nil {
				return fmt.Errorf("load agentctl config: %w", err)
			}

			// Open test status store
			store, err := testwatch.Open(ctx, cfg.Storage.Root)
			if err != nil {
				return fmt.Errorf("open test watch store: %w", err)
			}
			defer func() {
				// Cleanup in defer; error is not actionable.
				_ = store.Close() //nolint:errcheck
			}()

			// Handle --status-only
			if statusOnly {
				return handleStatusOnly(ctx, cmd, store, workspaceID)
			}

			twCfg, exists, err := loadTestWatchConfig(workspaceDir)
			if err != nil {
				return fmt.Errorf("load test-watch config: %w", err)
			}
			if !exists {
				return fmt.Errorf("no test-watch configuration found at %s\n\nUse 'agentctl test-watch add' to configure watchers", testwatch.ConfigPath(workspaceDir))
			}

			if len(twCfg.Watchers) == 0 {
				return fmt.Errorf("no watchers configured in %s\n\nUse 'agentctl test-watch add' to add watchers", testwatch.ConfigPath(workspaceDir))
			}

			// Create runner
			runner := testwatchrunner.NewRunner(workspaceID, workspaceDir, twCfg, store, log)
			defer runner.Stop()

			// Handle --once mode
			if once {
				observability.Emit(ctx, observability.NewEvent("watch.run_once").
					WithComponent(observability.ComponentCLI).
					WithData("workspace", workspaceDir).
					Success(0))
				if err := runner.RunOnce(ctx); err != nil {
					return err
				}

				// Output status
				return handleStatusOnly(ctx, cmd, store, workspaceID)
			}

			// Watch mode
			observability.Emit(ctx, observability.NewEvent("watch.start").
				WithComponent(observability.ComponentCLI).
				WithData("workspace", workspaceDir).
				WithData("watchers", len(twCfg.Watchers)).
				Success(0))

			// Set up file watcher
			watcher, err := fsnotify.NewWatcher()
			if err != nil {
				return fmt.Errorf("create file watcher: %w", err)
			}
			defer func() {
				// Watcher cleanup in defer; error is not actionable.
				_ = watcher.Close() //nolint:errcheck
			}()

			// Add workspace to watcher (recursive)
			if err := addRecursive(watcher, workspaceDir); err != nil {
				return fmt.Errorf("add workspace to watcher: %w", err)
			}

			// Handle shutdown signals
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

			// Run initial tests
			observability.Emit(ctx, observability.NewEvent("watch.initial_run").
				WithComponent(observability.ComponentCLI).
				Success(0))
			if err := runner.RunOnce(ctx); err != nil {
				observability.Emit(ctx, observability.NewEvent("watch.initial_run_error").
					WithComponent(observability.ComponentCLI).
					Error(err, 0))
			}

			observability.Emit(ctx, observability.NewEvent("watch.watching").
				WithComponent(observability.ComponentCLI).
				Success(0))

			// Event loop
			for {
				select {
				case event, ok := <-watcher.Events:
					if !ok {
						return nil
					}

					// Only handle write/create events
					if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
						// Skip hidden files and directories
						base := filepath.Base(event.Name)
						if len(base) > 0 && base[0] == '.' {
							continue
						}

						observability.Emit(ctx, observability.NewEvent("watch.file_changed").
							WithComponent(observability.ComponentCLI).
							WithData("file", event.Name).
							WithData("op", event.Op.String()).
							Success(0))
						runner.OnFileChange(ctx, event.Name)
					}

					// Handle new directories
					if event.Op&fsnotify.Create != 0 {
						if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
							// Best-effort add directory to watcher.
							_ = addRecursive(watcher, event.Name) //nolint:errcheck
						}
					}

				case err, ok := <-watcher.Errors:
					if !ok {
						return nil
					}
					observability.Emit(ctx, observability.NewEvent("watch.error").
						WithComponent(observability.ComponentCLI).
						Error(err, 0))

				case sig := <-sigCh:
					observability.Emit(ctx, observability.NewEvent("watch.shutdown").
						WithComponent(observability.ComponentCLI).
						WithData("signal", sig.String()).
						Success(0))
					return nil

				case <-ctx.Done():
					return ctx.Err()
				}
			}
		},
	}

	cmd.Flags().StringVar(&workspaceDir, "workspace", "", "Workspace root directory (default: current directory)")
	cmd.Flags().BoolVar(&once, "once", false, "Run all watchers once and exit")
	cmd.Flags().BoolVar(&statusOnly, "status-only", false, "Print current test status and exit")

	return cmd
}

func handleStatusOnly(ctx context.Context, cmd *cobra.Command, store testwatch.Store, workspaceID string) error {
	statuses, err := store.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("list test status: %w", err)
	}

	results := make([]map[string]any, 0, len(statuses))
	for _, s := range statuses {
		result := map[string]any{
			"watcher_id": s.WatcherID,
			"status":     string(s.Status),
			"command":    s.Command,
			"summary":    s.Summary,
		}
		if s.StartedAt != nil {
			result["started_at"] = s.StartedAt.Format("2006-01-02T15:04:05Z")
		}
		if s.FinishedAt != nil {
			result["finished_at"] = s.FinishedAt.Format("2006-01-02T15:04:05Z")
		}
		if len(s.Failures) > 0 {
			result["failures"] = s.Failures
		}
		results = append(results, result)
	}

	// Build summary
	var passCnt, failCnt int
	for _, s := range statuses {
		switch s.Status {
		case testwatch.StatusPass:
			passCnt++
		case testwatch.StatusFail, testwatch.StatusError:
			failCnt++
		}
	}

	summary := fmt.Sprintf("%d watcher(s): %d passing, %d failing", len(statuses), passCnt, failCnt)

	data := map[string]any{
		"workspace_id": workspaceID,
		"statuses":     results,
		"summary":      summary,
	}

	env := envelope.OK("watch/tests/status", data, envelope.WithMeta(envelope.Meta{Source: "cli"}))
	return envelope.Write(cmd.OutOrStdout(), env)
}

func addRecursive(watcher *fsnotify.Watcher, path string) error {
	return filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		// Skip hidden directories
		if info.IsDir() {
			base := filepath.Base(path)
			if len(base) > 0 && base[0] == '.' {
				return filepath.SkipDir
			}

			// Skip common non-source directories
			switch base {
			case "node_modules", "vendor", "__pycache__", ".git", "dist", "build":
				return filepath.SkipDir
			}

			return watcher.Add(path)
		}
		return nil
	})
}

func deriveWorkspaceID(path string) string {
	h := sha256.Sum256([]byte(path))
	return "ws-" + hex.EncodeToString(h[:8])
}

func init() {
	rootCmd.AddCommand(newWatchCommand())
}
