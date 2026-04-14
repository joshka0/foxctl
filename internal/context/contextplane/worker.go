package contextplane

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/obsidianindex"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
	taskstore "github.com/jkatigb/agentctl/internal/storage/tasks"
)

// WorkerConfig configures the daemonized ACA maintenance worker.
type WorkerConfig struct {
	Config    config.Config
	Workspace string
	VaultPath string
	Interval  time.Duration
}

// Worker keeps ACA orientation and maintenance state fresh for one workspace.
type Worker struct {
	cfg WorkerConfig
}

// NewWorker returns a background ACA maintenance worker.
func NewWorker(cfg WorkerConfig) *Worker {
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Minute
	}
	return &Worker{cfg: cfg}
}

// Run executes one immediate refresh and then continues on a bounded ticker until canceled.
func (w *Worker) Run(ctx context.Context) error {
	if err := w.RunOnce(ctx); err != nil && ctx.Err() == nil {
		return err
	}
	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := w.RunOnce(ctx); err != nil && ctx.Err() == nil {
				return err
			}
		}
	}
}

// RunOnce refreshes top-of-mind and maintenance state for the configured workspace.
func (w *Worker) RunOnce(ctx context.Context) error {
	workspacePath := strings.TrimSpace(w.cfg.Workspace)
	if workspacePath == "" {
		return nil
	}
	store := NewWorkspaceStore(workspacePath)
	if _, err := store.EnsureLayout(); err != nil {
		return err
	}

	tasksDB, err := taskstore.Open(ctx, w.cfg.Config.Storage.Root)
	if err != nil {
		return fmt.Errorf("open task store: %w", err)
	}
	defer func() { _ = tasksDB.Close() }()

	sessionStore, err := sessions.OpenFromConfig(ctx, w.cfg.Config)
	if err != nil {
		return fmt.Errorf("open session store: %w", err)
	}
	defer func() { _ = sessionStore.Close() }()

	orienter := NewOrienter(tasksDB, sessionStore)
	top, err := orienter.Build(ctx, workspacePath)
	if err != nil {
		return fmt.Errorf("build top_of_mind: %w", err)
	}
	if _, err := store.SaveTopOfMind(top); err != nil {
		return fmt.Errorf("save top_of_mind: %w", err)
	}

	if strings.TrimSpace(w.cfg.VaultPath) == "" {
		_, err = store.GenerateMaintenanceTasks(ctx, 50)
		return err
	}

	index, err := obsidianindex.Open(ctx, w.cfg.Config.Storage.Root, w.cfg.VaultPath)
	if err != nil {
		return fmt.Errorf("open obsidian index: %w", err)
	}
	defer func() { _ = index.Close() }()
	if _, err := index.Rebuild(ctx, filepath.Clean(w.cfg.VaultPath)); err != nil {
		return fmt.Errorf("rebuild obsidian index: %w", err)
	}
	health, err := index.Health(ctx)
	if err != nil {
		return fmt.Errorf("obsidian health: %w", err)
	}
	_, err = store.GenerateMaintenanceTasksWithHealth(ctx, 50, &health)
	return err
}
