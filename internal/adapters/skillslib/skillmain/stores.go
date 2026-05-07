package skillmain

import (
	"context"
	"fmt"
	"sync"

	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/memory"
	"github.com/joshka0/foxctl/internal/storage/sessions"
	"github.com/joshka0/foxctl/internal/storage/tasks"
)

// StoreProvider manages lazy-initialized store instances with lifecycle management.
// Stores are opened on first access and closed together via Close().
type StoreProvider struct {
	cfg config.Config

	mu                    sync.Mutex
	memoryStore           *memory.Store
	configuredMemoryStore storage.MemoryStore
	cacheMemory           *memory.Store
	sessStore             *sessions.Store
	taskStore             tasks.Store
	closeFuncs            []func()
}

// NewStoreProvider creates a new StoreProvider from config.
func NewStoreProvider(cfg config.Config) *StoreProvider {
	return &StoreProvider{cfg: cfg}
}

// Memory returns the memory store, opening it lazily on first call.
// Uses cfg.Storage.Root for persistent storage.
func (sp *StoreProvider) Memory(ctx context.Context) (*memory.Store, error) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if sp.memoryStore != nil {
		return sp.memoryStore, nil
	}

	store, err := memory.Open(ctx, sp.cfg.Storage.Root, sp.cfg.Paths.CAS)
	if err != nil {
		return nil, fmt.Errorf("open memory store: %w", err)
	}
	sp.memoryStore = store
	sp.closeFuncs = append(sp.closeFuncs, func() { _ = store.Close() })
	return store, nil
}

// ConfiguredMemory returns the configured memory backend, including Turso when enabled.
func (sp *StoreProvider) ConfiguredMemory(ctx context.Context) (storage.MemoryStore, error) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if sp.configuredMemoryStore != nil {
		return sp.configuredMemoryStore, nil
	}

	store, err := memory.OpenWithConfig(ctx, sp.cfg)
	if err != nil {
		return nil, fmt.Errorf("open configured memory store: %w", err)
	}
	sp.configuredMemoryStore = store
	sp.closeFuncs = append(sp.closeFuncs, func() { _ = store.Close() })
	return store, nil
}

// MemoryInCache returns the memory store using the cache path (for ephemeral data).
// Some skills store session_snapshot in cache rather than storage.
func (sp *StoreProvider) MemoryInCache(ctx context.Context) (*memory.Store, error) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if sp.cacheMemory != nil {
		return sp.cacheMemory, nil
	}

	store, err := memory.Open(ctx, sp.cfg.Paths.Cache, sp.cfg.Paths.CAS)
	if err != nil {
		return nil, fmt.Errorf("open cache memory store: %w", err)
	}
	sp.cacheMemory = store
	sp.closeFuncs = append(sp.closeFuncs, func() { _ = store.Close() })
	return store, nil
}

// Sessions returns the sessions store, opening it lazily on first call.
func (sp *StoreProvider) Sessions(ctx context.Context) (*sessions.Store, error) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if sp.sessStore != nil {
		return sp.sessStore, nil
	}

	store, err := sessions.Open(ctx, sp.cfg.Storage.Root)
	if err != nil {
		return nil, fmt.Errorf("open sessions store: %w", err)
	}
	sp.sessStore = store
	sp.closeFuncs = append(sp.closeFuncs, func() { _ = store.Close() })
	return store, nil
}

// Tasks returns the tasks store, opening it lazily on first call.
func (sp *StoreProvider) Tasks(ctx context.Context) (tasks.Store, error) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if sp.taskStore != nil {
		return sp.taskStore, nil
	}

	store, err := tasks.Open(ctx, sp.cfg.Storage.Root)
	if err != nil {
		return nil, fmt.Errorf("open tasks store: %w", err)
	}
	sp.taskStore = store
	sp.closeFuncs = append(sp.closeFuncs, func() { _ = store.Close() })
	return store, nil
}

// Close releases all opened stores.
func (sp *StoreProvider) Close() error {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	for _, fn := range sp.closeFuncs {
		fn()
	}
	sp.closeFuncs = nil
	sp.memoryStore = nil
	sp.configuredMemoryStore = nil
	sp.cacheMemory = nil
	sp.sessStore = nil
	sp.taskStore = nil
	return nil
}
