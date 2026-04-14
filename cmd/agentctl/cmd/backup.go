package cmd

import (
	"fmt"
	"strings"

	backupDomain "github.com/jkatigb/agentctl/internal/domain/backup"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/storage/backup"
	"github.com/spf13/cobra"
)

func newBackupCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Backup and restore agentctl data",
		Long: `Backup and restore agentctl databases, CAS, memory, sessions, and jobs.

Components available for backup:
  - databases: SQLite databases (tasks, graph, memory, sessions, etc.)
  - cas:       Content-addressable storage
  - memory:    Memory store files
  - sessions:  Session files
  - jobs:      Recent job artifacts (last 7 days)`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newBackupCreateCommand())
	cmd.AddCommand(newBackupListCommand())
	cmd.AddCommand(newBackupRestoreCommand())
	cmd.AddCommand(newBackupInfoCommand())

	return cmd
}

func newBackupCreateCommand() *cobra.Command {
	var (
		outputPath string
		name       string
		components []string
		exclude    []string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new backup",
		Long: `Create a backup archive containing agentctl data.

By default, all components are included. Use --components to include only
specific components, or --exclude to exclude certain components.

Examples:
  # Create a full backup
  agentctl backup create

  # Create a backup with custom name
  agentctl backup create --name "before-migration"

  # Backup only databases
  agentctl backup create --components databases

  # Backup everything except jobs
  agentctl backup create --exclude jobs

  # Backup to a specific location
  agentctl backup create --output /path/to/backup.tar.gz`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, ok := config.FromContext(cmd.Context())
			if !ok {
				return fmt.Errorf("configuration not loaded")
			}

			svc := backup.NewService(cfg)

			opts := backupDomain.CreateOptions{
				OutputPath: outputPath,
				Name:       name,
			}

			// Parse components
			for _, c := range components {
				opts.Components = append(opts.Components, backupDomain.Component(c))
			}
			for _, c := range exclude {
				opts.ExcludeComponents = append(opts.ExcludeComponents, backupDomain.Component(c))
			}

			result, err := svc.Create(cmd.Context(), opts)
			if err != nil {
				return fmt.Errorf("create backup: %w", err)
			}

			data := map[string]any{
				"path":            result.Path,
				"files_processed": result.FilesProcessed,
				"bytes_processed": result.BytesProcessed,
				"duration_ms":     result.Duration.Milliseconds(),
				"components":      result.Manifest.Components,
				"stats":           result.Manifest.Stats,
			}

			if len(result.Warnings) > 0 {
				data["warnings"] = result.Warnings
			}

			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.backup.create", data, protocol.WithSource("run"))
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output path for the backup archive")
	cmd.Flags().StringVarP(&name, "name", "n", "", "Custom name for the backup (used in default filename)")
	cmd.Flags().StringSliceVarP(&components, "components", "c", nil, "Components to include (databases, cas, memory, sessions, jobs)")
	cmd.Flags().StringSliceVarP(&exclude, "exclude", "e", nil, "Components to exclude")

	return cmd
}

func newBackupListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available backups",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, ok := config.FromContext(cmd.Context())
			if !ok {
				return fmt.Errorf("configuration not loaded")
			}

			svc := backup.NewService(cfg)

			backups, err := svc.List(cmd.Context())
			if err != nil {
				return fmt.Errorf("list backups: %w", err)
			}

			data := map[string]any{
				"backups": backups,
				"count":   len(backups),
			}

			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.backup.list", data, protocol.WithSource("run"))
		},
	}

	return cmd
}

func newBackupInfoCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info <backup-path-or-name>",
		Short: "Show detailed information about a backup",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, ok := config.FromContext(cmd.Context())
			if !ok {
				return fmt.Errorf("configuration not loaded")
			}

			svc := backup.NewService(cfg)

			// Resolve backup path
			path := args[0]
			if !strings.HasPrefix(path, "/") && !strings.Contains(path, "/") {
				// Assume it's a backup name in the default directory
				path = fmt.Sprintf("%s/backups/%s", cfg.Home, path)
				if !strings.HasSuffix(path, ".tar.gz") {
					path += ".tar.gz"
				}
			}

			manifest, err := svc.GetManifest(cmd.Context(), path)
			if err != nil {
				return fmt.Errorf("read backup: %w", err)
			}

			data := map[string]any{
				"path":     path,
				"manifest": manifest,
			}

			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.backup.info", data, protocol.WithSource("run"))
		},
	}

	return cmd
}

func newBackupRestoreCommand() *cobra.Command {
	var (
		components []string
		force      bool
		dryRun     bool
	)

	cmd := &cobra.Command{
		Use:   "restore <backup-path-or-name>",
		Short: "Restore data from a backup",
		Long: `Restore agentctl data from a backup archive.

By default, all components in the backup are restored. Use --components to
restore only specific components.

WARNING: Existing files will be skipped unless --force is specified.

Examples:
  # Restore from a backup
  agentctl backup restore backup_2024-01-15_120000.tar.gz

  # Restore only databases
  agentctl backup restore backup.tar.gz --components databases

  # Force overwrite existing files
  agentctl backup restore backup.tar.gz --force

  # Dry run to see what would be restored
  agentctl backup restore backup.tar.gz --dry-run`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, ok := config.FromContext(cmd.Context())
			if !ok {
				return fmt.Errorf("configuration not loaded")
			}

			svc := backup.NewService(cfg)

			// Resolve backup path
			path := args[0]
			if !strings.HasPrefix(path, "/") && !strings.Contains(path, "/") {
				// Assume it's a backup name in the default directory
				path = fmt.Sprintf("%s/backups/%s", cfg.Home, path)
				if !strings.HasSuffix(path, ".tar.gz") {
					path += ".tar.gz"
				}
			}

			opts := backupDomain.RestoreOptions{
				Force:  force,
				DryRun: dryRun,
			}

			for _, c := range components {
				opts.Components = append(opts.Components, backupDomain.Component(c))
			}

			result, err := svc.Restore(cmd.Context(), path, opts)
			if err != nil {
				return fmt.Errorf("restore backup: %w", err)
			}

			data := map[string]any{
				"path":            result.Path,
				"files_processed": result.FilesProcessed,
				"bytes_processed": result.BytesProcessed,
				"duration_ms":     result.Duration.Milliseconds(),
				"dry_run":         dryRun,
			}

			if len(result.Warnings) > 0 {
				data["warnings"] = result.Warnings
			}

			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.backup.restore", data, protocol.WithSource("run"))
		},
	}

	cmd.Flags().StringSliceVarP(&components, "components", "c", nil, "Components to restore (default: all in backup)")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Overwrite existing files")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be restored without making changes")

	return cmd
}

func init() {
	rootCmd.AddCommand(newBackupCommand())
}
