package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/storage/testwatch"
	"github.com/spf13/cobra"
)

func newTestWatchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test-watch",
		Short: "Configure per-workspace test watchers",
		Long: `Configure test watchers for a workspace. Watchers run test commands
when relevant files change and track pass/fail status.

Configuration is stored in .foxctl/test-watch.yaml in the workspace root.
This is foxctl-owned config (not harness-specific) and can be read by any
agent integration.

Use 'foxctl watch tests' to start the watcher daemon.`,
	}
	cmd.AddCommand(
		newTestWatchListCommand(),
		newTestWatchAddCommand(),
		newTestWatchRemoveCommand(),
	)
	return cmd
}

func newTestWatchListCommand() *cobra.Command {
	var workspaceDir string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List configured test watchers for a workspace",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Determine workspace root
			var err error
			if workspaceDir == "" {
				workspaceDir, err = os.Getwd()
				if err != nil {
					return fmt.Errorf("get working directory: %w", err)
				}
			}

			cfg, exists, err := loadTestWatchConfig(workspaceDir)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			if !exists {
				data := map[string]any{
					"watchers":      []any{},
					"config_exists": false,
					"config_path":   testwatch.ConfigPath(workspaceDir),
					"summary":       "No test-watch configuration found. Use 'foxctl test-watch add' to configure watchers.",
				}
				env := envelope.OK("test-watch/list", data, envelope.WithMeta(envelope.Meta{Source: "cli"}))
				return envelope.Write(cmd.OutOrStdout(), env)
			}

			// Build watcher list for output
			watchers := make([]map[string]any, 0, len(cfg.Watchers))
			for _, w := range cfg.Watchers {
				watcher := map[string]any{
					"id":           w.ID,
					"command":      w.Command,
					"debounce":     w.EffectiveDebounce(cfg).String(),
					"min_interval": w.EffectiveMinInterval().String(),
				}
				if len(w.Include) > 0 {
					watcher["include"] = w.Include
				}
				if len(w.Exclude) > 0 {
					watcher["exclude"] = w.Exclude
				}
				watchers = append(watchers, watcher)
			}

			data := map[string]any{
				"watchers":      watchers,
				"config_exists": true,
				"config_path":   testwatch.ConfigPath(workspaceDir),
				"summary":       fmt.Sprintf("Found %d watcher(s) in %s", len(cfg.Watchers), testwatch.ConfigPath(workspaceDir)),
			}
			if cfg.Debounce > 0 {
				data["default_debounce"] = cfg.Debounce.String()
			}

			env := envelope.OK("test-watch/list", data, envelope.WithMeta(envelope.Meta{Source: "cli"}))
			return envelope.Write(cmd.OutOrStdout(), env)
		},
	}

	cmd.Flags().StringVar(&workspaceDir, "workspace", "", "Workspace root directory (default: current directory)")
	return cmd
}

func newTestWatchAddCommand() *cobra.Command {
	var workspaceDir string
	var watcherID string
	var command string
	var include []string
	var exclude []string
	var debounce string
	var minInterval string

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add or update a test watcher",
		Long: `Add or update a test watcher for the workspace.

If a watcher with the same ID already exists, it will be updated.

Examples:
  # Add a Go watcher
  foxctl test-watch add --id go --command "go test ./..." --include "**/*.go"

  # Add a JS/TS watcher with custom intervals
  foxctl test-watch add --id js --command "npm test" \
    --include "**/*.js" --include "**/*.ts" \
    --debounce 3s --min-interval 30s`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Determine workspace root
			var err error
			if workspaceDir == "" {
				workspaceDir, err = os.Getwd()
				if err != nil {
					return fmt.Errorf("get working directory: %w", err)
				}
			}

			cfg, exists, err := loadTestWatchConfig(workspaceDir)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			if !exists {
				cfg = testwatch.NewConfig()
			}

			// Parse durations
			var debounceDur, minIntervalDur time.Duration
			if debounce != "" {
				debounceDur, err = time.ParseDuration(debounce)
				if err != nil {
					return fmt.Errorf("invalid debounce %q: %w", debounce, err)
				}
			}
			if minInterval != "" {
				minIntervalDur, err = time.ParseDuration(minInterval)
				if err != nil {
					return fmt.Errorf("invalid min-interval %q: %w", minInterval, err)
				}
			}

			// Check if updating existing
			existing := cfg.GetWatcher(watcherID)
			isUpdate := existing != nil

			// Build watcher config
			w := testwatch.WatcherConfig{
				ID:          watcherID,
				Command:     command,
				Include:     include,
				Exclude:     exclude,
				Debounce:    debounceDur,
				MinInterval: minIntervalDur,
			}

			// Upsert
			cfg.UpsertWatcher(w)

			// Save config
			if err := testwatch.SaveConfig(workspaceDir, cfg); err != nil {
				return fmt.Errorf("save config: %w", err)
			}

			action := "added"
			if isUpdate {
				action = "updated"
			}

			data := map[string]any{
				"watcher": map[string]any{
					"id":           w.ID,
					"command":      w.Command,
					"include":      w.Include,
					"exclude":      w.Exclude,
					"debounce":     w.EffectiveDebounce(cfg).String(),
					"min_interval": w.EffectiveMinInterval().String(),
				},
				"action":      action,
				"config_path": testwatch.ConfigPath(workspaceDir),
				"summary":     fmt.Sprintf("Watcher %q %s in %s", watcherID, action, testwatch.ConfigPath(workspaceDir)),
			}

			env := envelope.OK("test-watch/add", data, envelope.WithMeta(envelope.Meta{Source: "cli"}))
			return envelope.Write(cmd.OutOrStdout(), env)
		},
	}

	cmd.Flags().StringVar(&workspaceDir, "workspace", "", "Workspace root directory (default: current directory)")
	cmd.Flags().StringVar(&watcherID, "id", "", "Watcher ID (e.g., 'go', 'js', 'python')")
	cmd.Flags().StringVar(&command, "command", "", "Test command to run")
	cmd.Flags().StringSliceVar(&include, "include", nil, "Glob patterns for files that trigger this watcher")
	cmd.Flags().StringSliceVar(&exclude, "exclude", nil, "Glob patterns for files to ignore")
	cmd.Flags().StringVar(&debounce, "debounce", "", "Debounce duration (e.g., '2s', '500ms')")
	cmd.Flags().StringVar(&minInterval, "min-interval", "", "Minimum interval between runs (e.g., '20s')")

	if err := cmd.MarkFlagRequired("id"); err != nil {
		panic(err)
	}
	if err := cmd.MarkFlagRequired("command"); err != nil {
		panic(err)
	}

	return cmd
}

func newTestWatchRemoveCommand() *cobra.Command {
	var workspaceDir string
	var watcherID string

	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a test watcher",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Determine workspace root
			var err error
			if workspaceDir == "" {
				workspaceDir, err = os.Getwd()
				if err != nil {
					return fmt.Errorf("get working directory: %w", err)
				}
			}

			cfg, exists, err := loadTestWatchConfig(workspaceDir)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			if !exists {
				return fmt.Errorf("no test-watch configuration found at %s", testwatch.ConfigPath(workspaceDir))
			}

			// Remove watcher
			if !cfg.RemoveWatcher(watcherID) {
				return fmt.Errorf("watcher %q not found", watcherID)
			}

			// Save config
			if err := testwatch.SaveConfig(workspaceDir, cfg); err != nil {
				return fmt.Errorf("save config: %w", err)
			}

			data := map[string]any{
				"watcher_id":  watcherID,
				"config_path": testwatch.ConfigPath(workspaceDir),
				"summary":     fmt.Sprintf("Watcher %q removed from %s", watcherID, testwatch.ConfigPath(workspaceDir)),
			}

			env := envelope.OK("test-watch/remove", data, envelope.WithMeta(envelope.Meta{Source: "cli"}))
			return envelope.Write(cmd.OutOrStdout(), env)
		},
	}

	cmd.Flags().StringVar(&workspaceDir, "workspace", "", "Workspace root directory (default: current directory)")
	cmd.Flags().StringVar(&watcherID, "id", "", "Watcher ID to remove")

	if err := cmd.MarkFlagRequired("id"); err != nil {
		panic(err)
	}

	return cmd
}

func init() {
	rootCmd.AddCommand(newTestWatchCommand())
}
