package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/cmd/foxctl/cmd/memorycmd"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/dbdriver"
	memstore "github.com/joshka0/foxctl/internal/storage/memory"
	"github.com/spf13/cobra"
)

func newMemoryMigrateBackendCommand() *cobra.Command {
	var (
		workspaceFlag string
		sourceDriver  string
		targetDriver  string
		targetPath    string
		limit         int
		offset        int
		batchSize     int
		vectorDims    int
		apply         bool
	)
	cmd := &cobra.Command{
		Use:   "migrate-backend",
		Short: "Copy named memories between storage backends without old embeddings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return memorycmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				workspaceRoot := resolveWorkspace(cfg, workspaceFlag)
				workspaceID := workspace.ID(workspaceRoot)
				if strings.TrimSpace(workspaceID) == "" {
					return memorycmd.WriteArgError(cmd.OutOrStdout(), "foxctl.memory.migrate_backend", "invalid workspace", "Provide --workspace with a valid repository path.")
				}
				opts := memoryBackendMigrationOptions{
					WorkspaceID:  workspaceID,
					SourceDriver: strings.TrimSpace(sourceDriver),
					TargetDriver: strings.TrimSpace(targetDriver),
					TargetPath:   strings.TrimSpace(targetPath),
					Limit:        limit,
					Offset:       offset,
					BatchSize:    batchSize,
					VectorDims:   vectorDims,
					Apply:        apply,
				}
				result, err := runMemoryBackendMigration(ctx, cfg, opts)
				if err != nil {
					return err
				}
				return memorycmd.WriteOK(cmd.OutOrStdout(), "foxctl.memory.migrate_backend", result)
			})
		},
	}
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Workspace path (default: auto-detect)")
	cmd.Flags().StringVar(&sourceDriver, "source-driver", "sqlite", "Source backend driver (sqlite)")
	cmd.Flags().StringVar(&targetDriver, "target-driver", "turso", "Target backend driver (turso)")
	cmd.Flags().StringVar(&targetPath, "target-path", "", "Target Turso database path (default: storage/memory.turso)")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum entries to copy (0 means all)")
	cmd.Flags().IntVar(&offset, "offset", 0, "Source entry offset for chunked migrations")
	cmd.Flags().IntVar(&batchSize, "batch-size", 1000, "Entries to read per page")
	cmd.Flags().IntVar(&vectorDims, "vector-dims", 0, "Target vector dimensions (default: configured database vector dimensions)")
	cmd.Flags().BoolVar(&apply, "apply", false, "Apply the migration; without this flag the command only reports what would copy")
	return cmd
}

type memoryBackendMigrationOptions struct {
	WorkspaceID  string
	SourceDriver string
	TargetDriver string
	TargetPath   string
	Limit        int
	Offset       int
	BatchSize    int
	VectorDims   int
	Apply        bool
}

type memoryBackendMigrationResult struct {
	Workspace    string `json:"workspace"`
	SourceDriver string `json:"source_driver"`
	TargetDriver string `json:"target_driver"`
	TargetPath   string `json:"target_path,omitempty"`
	DryRun       bool   `json:"dry_run"`
	Scanned      int    `json:"scanned"`
	Copied       int    `json:"copied"`
	Limit        int    `json:"limit,omitempty"`
	Offset       int    `json:"offset,omitempty"`
}

func runMemoryBackendMigration(ctx context.Context, cfg config.Config, opts memoryBackendMigrationOptions) (memoryBackendMigrationResult, error) {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 1000
	}
	result := memoryBackendMigrationResult{
		Workspace:    opts.WorkspaceID,
		SourceDriver: opts.SourceDriver,
		TargetDriver: opts.TargetDriver,
		TargetPath:   opts.TargetPath,
		DryRun:       !opts.Apply,
		Limit:        opts.Limit,
		Offset:       opts.Offset,
	}

	source, err := openMemoryMigrationSource(ctx, cfg, opts.SourceDriver)
	if err != nil {
		return result, err
	}
	defer func() { _ = source.Close() }()

	var target storage.MemoryStore
	if opts.Apply {
		target, err = openMemoryMigrationTarget(ctx, cfg, opts.TargetDriver, opts.TargetPath, opts.VectorDims)
		if err != nil {
			return result, err
		}
		defer func() { _ = target.Close() }()
	}

	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}
	for {
		remaining := opts.BatchSize
		if opts.Limit > 0 && result.Scanned+remaining > opts.Limit {
			remaining = opts.Limit - result.Scanned
		}
		if remaining <= 0 {
			break
		}

		entries, total, err := source.ListFiltered(ctx, opts.WorkspaceID, storage.MemoryListFilter{}, remaining, offset)
		if err != nil {
			return result, fmt.Errorf("list source memories: %w", err)
		}
		if len(entries) == 0 {
			break
		}
		for _, entry := range entries {
			result.Scanned++
			if target != nil {
				if _, err := target.Save(ctx, entry); err != nil {
					return result, fmt.Errorf("copy memory %q: %w", entry.Name, err)
				}
				result.Copied++
			}
		}
		if opts.Limit > 0 && result.Scanned >= opts.Limit {
			break
		}
		if offset+len(entries) >= total {
			break
		}
		offset += len(entries)
	}
	if !opts.Apply {
		result.Copied = 0
	}
	return result, nil
}

func openMemoryMigrationSource(ctx context.Context, cfg config.Config, driver string) (storage.MemoryStore, error) {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "", "sqlite":
		return memstore.Open(ctx, cfg.Storage.Root, cfg.Paths.CAS)
	default:
		return nil, fmt.Errorf("memory backend migration: unsupported source driver %q", driver)
	}
}

func openMemoryMigrationTarget(ctx context.Context, cfg config.Config, driver, targetPath string, dims int) (storage.MemoryStore, error) {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "", "turso":
		if targetPath == "" {
			targetPath = filepath.Join(cfg.Storage.Root, "memory.turso")
		}
		if strings.HasPrefix(targetPath, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, err
			}
			targetPath = filepath.Join(home, strings.TrimPrefix(targetPath, "~/"))
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return nil, fmt.Errorf("create target directory: %w", err)
		}
		if dims <= 0 {
			dims = cfg.Database.Vector.Dimensions
		}
		if dims <= 0 {
			dims = dbdriver.GetDefaultVectorDimensions()
		}
		return memstore.OpenTurso(ctx, dbdriver.TursoConfig{
			Path:               targetPath,
			ReplicaPath:        targetPath,
			EnableVectorSearch: true,
			VectorDimensions:   dims,
		})
	default:
		return nil, fmt.Errorf("memory backend migration: unsupported target driver %q", driver)
	}
}
