package sqliteutil

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
)

// Pool manages a pool of SQLite database connections.
// Unlike a standard database connection pool (which pools connections to a single DB),
// this pool manages connections to multiple SQLite databases keyed by path.
// This is useful for daemon mode where we want to keep connections open
// across multiple requests to avoid the ~50ms open/close overhead per request.
type Pool struct {
	mu       sync.RWMutex
	dbs      map[string]*pooledDB
	migrates map[string]func(context.Context, *sql.DB) error
	closed   bool
}

// pooledDB wraps a database connection with reference counting.
type pooledDB struct {
	db       *sql.DB
	refCount int
}

// NewPool creates a new database connection pool.
func NewPool() *Pool {
	return &Pool{
		dbs:      make(map[string]*pooledDB),
		migrates: make(map[string]func(context.Context, *sql.DB) error),
	}
}

// Get returns a database connection for the given path.
// If a connection doesn't exist, it opens one using OpenDB.
// The caller should call Release when done (though in daemon mode,
// connections are typically held for the lifetime of the daemon).
func (p *Pool) Get(ctx context.Context, path string, migrate func(context.Context, *sql.DB) error) (*sql.DB, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil, fmt.Errorf("pool is closed")
	}

	// Check for existing connection
	if pdb, ok := p.dbs[path]; ok {
		pdb.refCount++
		return pdb.db, nil
	}

	// Open new connection
	db, err := OpenDB(ctx, path, migrate)
	if err != nil {
		return nil, err
	}

	// Configure for connection pooling
	// Keep 1 connection open, allow up to 5 concurrent (for WAL mode)
	db.SetMaxIdleConns(1)
	db.SetMaxOpenConns(5)

	// Store in pool
	p.dbs[path] = &pooledDB{
		db:       db,
		refCount: 1,
	}
	if migrate != nil {
		p.migrates[path] = migrate
	}

	return db, nil
}

// Release decrements the reference count for a database.
// In daemon mode, this typically doesn't close the connection -
// use Close to close all connections.
func (p *Pool) Release(path string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if pdb, ok := p.dbs[path]; ok {
		pdb.refCount--
		// Don't close even if refCount reaches 0 - keep for reuse
	}
}

// Close closes all pooled database connections.
func (p *Pool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.closed = true

	var firstErr error
	for path, pdb := range p.dbs {
		if err := pdb.db.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("close %s: %w", path, err)
		}
	}

	p.dbs = make(map[string]*pooledDB)
	return firstErr
}

// Stats returns statistics about the pool.
func (p *Pool) Stats() PoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := PoolStats{
		DatabaseCount: len(p.dbs),
		Databases:     make(map[string]DBStats),
	}

	for path, pdb := range p.dbs {
		sqlStats := pdb.db.Stats()
		stats.Databases[path] = DBStats{
			RefCount:   pdb.refCount,
			OpenConns:  sqlStats.OpenConnections,
			InUse:      sqlStats.InUse,
			Idle:       sqlStats.Idle,
			WaitCount:  sqlStats.WaitCount,
			WaitTimeMs: sqlStats.WaitDuration.Milliseconds(),
		}
	}

	return stats
}

// PoolStats contains pool statistics.
type PoolStats struct {
	DatabaseCount int                `json:"database_count"`
	Databases     map[string]DBStats `json:"databases"`
}

// DBStats contains database connection statistics.
type DBStats struct {
	RefCount   int   `json:"ref_count"`
	OpenConns  int   `json:"open_connections"`
	InUse      int   `json:"in_use"`
	Idle       int   `json:"idle"`
	WaitCount  int64 `json:"wait_count"`
	WaitTimeMs int64 `json:"wait_time_ms"`
}

// GlobalPool is a shared connection pool for daemon mode.
// In non-daemon mode, this is nil and each store manages its own connection.
var GlobalPool *Pool

// SetGlobalPool sets the global connection pool.
// Called by daemon startup to enable connection sharing.
func SetGlobalPool(pool *Pool) {
	GlobalPool = pool
}

// GetGlobalPool returns the global connection pool, or nil if not in daemon mode.
func GetGlobalPool() *Pool {
	return GlobalPool
}
