package sessionkit

import (
	"context"
	"os"
	"path/filepath"

	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage/memory"
	"github.com/joshka0/foxctl/internal/storage/sessions"
	"github.com/joshka0/foxctl/internal/storage/tasks"
)

// OpenSessions opens the sessions store using paths from config.
// Returns the store, a cleanup function, and any error.
func OpenSessions(ctx context.Context, cfg config.Config) (*sessions.Store, func(), error) {
	store, err := sessions.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() {
		_ = store.Close()
	}
	return store, cleanup, nil
}

// OpenTasks opens the tasks store using paths from config.
// Returns the store, a cleanup function, and any error.
func OpenTasks(ctx context.Context, cfg config.Config) (tasks.Store, func(), error) {
	store, err := tasks.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() {
		_ = store.Close()
	}
	return store, cleanup, nil
}

// OpenMemory opens the memory store using paths from config.
// Returns the store, a cleanup function, and any error.
func OpenMemory(ctx context.Context, cfg config.Config) (*memory.Store, func(), error) {
	store, err := memory.Open(ctx, cfg.Storage.Root, cfg.Paths.CAS)
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() {
		_ = store.Close()
	}
	return store, cleanup, nil
}

// OpenMemoryInCache opens the memory store in cache path (for ephemeral data).
// Some skills store session_snapshot in cache rather than storage.
func OpenMemoryInCache(ctx context.Context, cfg config.Config) (*memory.Store, func(), error) {
	store, err := memory.Open(ctx, cfg.Paths.Cache, cfg.Paths.CAS)
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() {
		_ = store.Close()
	}
	return store, cleanup, nil
}

// StorePaths holds resolved paths for session skill operations.
type StorePaths struct {
	FoxctlHome  string // ~/.foxctl or FOXCTL_HOME
	StorageRoot string // ~/.foxctl/storage
	CachePath   string // ~/.foxctl/cache
	CASPath     string // ~/.foxctl/cas
	ArchivesDir string // ~/.foxctl/archives
	PlansDir    string // ~/.claude/plans
	ClaudeHome  string // ~/.claude
}

// ResolvePaths resolves all standard paths from config.
func ResolvePaths(cfg config.Config) StorePaths {
	home := cfg.Home
	// Derive Claude home from user home directory
	userHome, _ := os.UserHomeDir()
	claudeHome := filepath.Join(userHome, ".claude")
	return StorePaths{
		FoxctlHome:  home,
		StorageRoot: cfg.Storage.Root,
		CachePath:   cfg.Paths.Cache,
		CASPath:     cfg.Paths.CAS,
		ArchivesDir: filepath.Join(home, "archives"),
		PlansDir:    filepath.Join(claudeHome, "plans"),
		ClaudeHome:  claudeHome,
	}
}
