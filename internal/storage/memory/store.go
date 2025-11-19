// Package memory implements named memory storage for skill execution results and context data.
package memory

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jkatigb/agentctl/internal/adapters/artifacts"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/platform/timeutil"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/cache"
	"github.com/jkatigb/agentctl/internal/storage/cas"
	"github.com/jkatigb/agentctl/internal/storage/dbutil"
	"github.com/jkatigb/agentctl/internal/storage/sqliteutil"
	"github.com/jkatigb/agentctl/internal/storage/sqlutil"
)

// Ensure Store implements storage.MemoryStore.
var _ storage.MemoryStore = (*Store)(nil)

// NamedEntry aliases the shared storage type for backwards compatibility.
type NamedEntry = storage.NamedEntry

// ScoredEntry aliases the shared scored entry type.
type ScoredEntry = storage.ScoredEntry

// Store handles named memory persistence.
type Store struct {
	db              *sql.DB
	cas             *cas.Store
	artifactManager artifacts.Manager
	path            string
}

// Stats aliases the shared memory stats type.
type Stats = storage.MemoryStats

// Connection pool defaults for SQLite file-based storage
// These values provide reasonable defaults for typical workloads with moderate concurrency
const (
	defaultMaxOpenConns    = 10               // Max concurrent connections (balances concurrency with SQLite write serialization)
	defaultMaxIdleConns    = 5                // Idle connections kept ready
	defaultConnMaxLifetime = 10 * time.Minute // Connection recycling interval (SQLite best practice: 5-15 minutes)
	defaultConnMaxIdleTime = 15 * time.Minute // Idle connection timeout
)

// Open initializes the memory store rooted at the provided path.
func Open(ctx context.Context, root string, casPath string) (store *Store, err error) {
	dbPath := filepath.Join(root, "memory.db")
	db, err := sqliteutil.OpenDB(ctx, dbPath, migrate)
	if err != nil {
		return nil, fmt.Errorf("memory: open db: %w", err)
	}
	defer errs.CloseOnErr(db, &err)

	// Configure connection pool for optimal performance
	// Note: For SQLite, too many concurrent connections can cause contention
	// These defaults balance responsiveness with resource usage
	db.SetMaxOpenConns(defaultMaxOpenConns)
	db.SetMaxIdleConns(defaultMaxIdleConns)
	db.SetConnMaxLifetime(defaultConnMaxLifetime)
	db.SetConnMaxIdleTime(defaultConnMaxIdleTime)

	// sqliteutil.OpenDB handles directory creation, WAL configuration, and migration execution.

	var casStore *cas.Store
	var artifactMgr artifacts.Manager
	if casPath != "" {
		if casStore, err = cas.NewStore(casPath); err != nil {
			return nil, err
		}
		artifactMgr = artifacts.NewManager(casStore)
	}
	store = &Store{db: db, cas: casStore, artifactManager: artifactMgr, path: dbPath}
	return store, nil
}

// Close releases resources.
func (s *Store) Close() error {
	return s.db.Close()
}

// Stats returns entry counts for named memories.
func (s *Store) Stats(ctx context.Context) (Stats, error) {
	var count int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM named_memory`).Scan(&count); err != nil {
		return Stats{}, fmt.Errorf("memory: stats: %w", err)
	}
	return Stats{Named: count, Path: s.path}, nil
}

// Save inserts or updates a named memory.
func (s *Store) Save(ctx context.Context, entry NamedEntry) (NamedEntry, error) {
	now := timeutil.NowUTC()
	if entry.ID == "" {
		entry.ID = uuid.NewString()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	entry.UpdatedAt = now
	entry.LastAccess = now
	if entry.Type == "" {
		entry.Type = "result"
	}

	// Format digests with proper error handling
	digestsJSON, err := sqlutil.FormatJSON(entry.Digests)
	if err != nil {
		return NamedEntry{}, fmt.Errorf("memory: format digests: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
INSERT INTO named_memory (id, name, type, workspace, summary, result, digests, created_at, updated_at, last_accessed, access_count)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
ON CONFLICT(name, workspace) DO UPDATE SET
	id = excluded.id,
	type = excluded.type,
	summary = excluded.summary,
	result = excluded.result,
	digests = excluded.digests,
	updated_at = excluded.updated_at,
	last_accessed = excluded.last_accessed
`, entry.ID, entry.Name, entry.Type, entry.Workspace, entry.Summary, entry.Result, digestsJSON,
		sqlutil.FormatTimestamp(entry.CreatedAt), sqlutil.FormatTimestamp(entry.UpdatedAt), sqlutil.FormatTimestamp(entry.LastAccess))
	if err != nil {
		return NamedEntry{}, fmt.Errorf("memory: save: %w", err)
	}
	s.pin(ctx, entry.Digests)
	return entry, nil
}

// Get fetches a named memory by name+workspace.
func (s *Store) Get(ctx context.Context, name, workspace string) (NamedEntry, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, type, workspace, summary, result, digests, created_at, updated_at, last_accessed, access_count
		FROM named_memory
		WHERE name = ? AND workspace = ?`, name, workspace)
	var entry NamedEntry
	var digests string
	var created, updated, last string
	if err := row.Scan(&entry.ID, &entry.Name, &entry.Type, &entry.Workspace, &entry.Summary, &entry.Result, &digests, &created, &updated, &last, &entry.AccessCount); err != nil {
		if dbutil.IsNoRows(err) {
			return NamedEntry{}, ErrNotFound
		}
		return NamedEntry{}, fmt.Errorf("memory: get: %w", err)
	}

	// Parse digests with proper error handling
	if err := sqlutil.ScanJSON(digests, &entry.Digests); err != nil {
		return NamedEntry{}, fmt.Errorf("memory: scan digests: %w", err)
	}

	// Parse timestamps with proper error handling
	var err error
	entry.CreatedAt, err = sqlutil.ScanTimestamp(created)
	if err != nil {
		return NamedEntry{}, fmt.Errorf("memory: scan created_at: %w", err)
	}
	entry.UpdatedAt, err = sqlutil.ScanTimestamp(updated)
	if err != nil {
		return NamedEntry{}, fmt.Errorf("memory: scan updated_at: %w", err)
	}
	entry.LastAccess, err = sqlutil.ScanTimestamp(last)
	if err != nil {
		return NamedEntry{}, fmt.Errorf("memory: scan last_accessed: %w", err)
	}

	if _, updateErr := s.db.ExecContext(ctx, `
		UPDATE named_memory
		SET last_accessed = ?, access_count = access_count + 1
		WHERE id = ?`, sqlutil.FormatTimestamp(timeutil.NowUTC()), entry.ID); updateErr != nil {
		errs.Ignore(updateErr, "memory: refresh access metadata")
	}

	return entry, nil
}

// List returns named memories for a workspace.
func (s *Store) List(ctx context.Context, workspace string, limit int) ([]NamedEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, type, workspace, summary, result, digests, created_at, updated_at, last_accessed, access_count
		FROM named_memory
		WHERE workspace = ?
		ORDER BY updated_at DESC
		LIMIT ?`, workspace, limit)
	if err != nil {
		return nil, fmt.Errorf("memory: list: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close memory list rows")
	}()

	entries := make([]NamedEntry, 0, limit)
	for rows.Next() {
		var entry NamedEntry
		var digests string
		var created, updated, last string
		if err := rows.Scan(&entry.ID, &entry.Name, &entry.Type, &entry.Workspace, &entry.Summary, &entry.Result, &digests, &created, &updated, &last, &entry.AccessCount); err != nil {
			return nil, fmt.Errorf("memory: scan list: %w", err)
		}

		// Parse digests with proper error handling
		if err := sqlutil.ScanJSON(digests, &entry.Digests); err != nil {
			return nil, fmt.Errorf("memory: scan list digests: %w", err)
		}

		// Parse timestamps with proper error handling
		entry.CreatedAt, err = sqlutil.ScanTimestamp(created)
		if err != nil {
			return nil, fmt.Errorf("memory: scan list created_at: %w", err)
		}
		entry.UpdatedAt, err = sqlutil.ScanTimestamp(updated)
		if err != nil {
			return nil, fmt.Errorf("memory: scan list updated_at: %w", err)
		}
		entry.LastAccess, err = sqlutil.ScanTimestamp(last)
		if err != nil {
			return nil, fmt.Errorf("memory: scan list last_accessed: %w", err)
		}

		entries = append(entries, entry)
	}
	return entries, nil
}

// Delete removes a named memory and unpins digests.
func (s *Store) Delete(ctx context.Context, name, workspace string) error {
	row := s.db.QueryRowContext(ctx, `SELECT digests FROM named_memory WHERE name = ? AND workspace = ?`, name, workspace)
	var digests string
	if err := row.Scan(&digests); err != nil {
		if dbutil.IsNoRows(err) {
			return ErrNotFound
		}
		return fmt.Errorf("memory: scan digests: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM named_memory WHERE name = ? AND workspace = ?`, name, workspace); err != nil {
		return fmt.Errorf("memory: delete: %w", err)
	}

	var arr []string
	if err := sqlutil.ScanJSON(digests, &arr); err != nil {
		return fmt.Errorf("memory: scan delete digests: %w", err)
	}
	s.unpin(ctx, arr)
	return nil
}

// SaveOptions contains parameters for saving memory entries.
type SaveOptions struct {
	Name      string
	Type      string
	Workspace string
	Summary   string
	Result    []byte
}

// SaveResult stores a result envelope using structured options.
func (s *Store) SaveResult(ctx context.Context, opts SaveOptions) (NamedEntry, error) {
	entry := NamedEntry{
		Name:      opts.Name,
		Type:      opts.Type,
		Workspace: opts.Workspace,
		Summary:   opts.Summary,
		Result:    opts.Result,
		Digests:   cache.CollectDigests(opts.Result),
	}
	return s.Save(ctx, entry)
}

// SaveFromResult is a helper that stores a result envelope.
// Deprecated: Use SaveResult with SaveOptions instead for better clarity.
func (s *Store) SaveFromResult(ctx context.Context, name, typ, workspace, summary string, result []byte) (NamedEntry, error) {
	return s.SaveResult(ctx, SaveOptions{
		Name:      name,
		Type:      typ,
		Workspace: workspace,
		Summary:   summary,
		Result:    result,
	})
}

// ErrNotFound indicates missing entries.
var ErrNotFound = fmt.Errorf("memory: not found")

func (s *Store) pin(ctx context.Context, digests []string) {
	if s.artifactManager == nil {
		return
	}
	// Check for context cancellation
	if ctx.Err() != nil {
		return
	}
	if err := s.artifactManager.Pin(ctx, digests...); err != nil {
		errs.Ignore(err, "memory: pin digests")
	}
}

func (s *Store) unpin(ctx context.Context, digests []string) {
	if s.artifactManager == nil {
		return
	}
	// Check for context cancellation
	if ctx.Err() != nil {
		return
	}
	if err := s.artifactManager.Unpin(ctx, digests...); err != nil {
		errs.Ignore(err, "memory: unpin digests")
	}
}

func migrate(ctx context.Context, db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS named_memory (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	type TEXT NOT NULL,
	workspace TEXT NOT NULL,
	summary TEXT,
	result BLOB NOT NULL,
	digests TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	last_accessed TEXT NOT NULL,
	access_count INTEGER NOT NULL DEFAULT 0,
	UNIQUE(name, workspace)
);
CREATE INDEX IF NOT EXISTS idx_named_memory_ws_updated ON named_memory(workspace, updated_at DESC);
`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("memory: migrate: %w", err)
	}
	return nil
}

// Search finds entries whose name or summary contain the query string.
func (s *Store) Search(ctx context.Context, workspace, query string, limit int) ([]ScoredEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	like := "%" + strings.ToLower(query) + "%"
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, type, workspace, summary, result, digests, created_at, updated_at, last_accessed, access_count
		FROM named_memory
		WHERE workspace = ? AND (LOWER(name) LIKE ? OR LOWER(summary) LIKE ?)
		ORDER BY updated_at DESC
		LIMIT ?`, workspace, like, like, limit)
	if err != nil {
		return nil, fmt.Errorf("memory: search: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close memory search rows")
	}()
	entries, err := scanEntries(rows)
	if err != nil {
		return nil, err
	}
	var scored []ScoredEntry
	for _, entry := range entries {
		scored = append(scored, ScoredEntry{
			Entry: entry,
			Score: scoreEntry(entry),
		})
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			return scored[i].Entry.UpdatedAt.After(scored[j].Entry.UpdatedAt)
		}
		return scored[i].Score > scored[j].Score
	})
	if len(scored) > limit {
		scored = scored[:limit]
	}
	return scored, nil
}

// Update mutates summary and/or type for a named entry.
func (s *Store) Update(ctx context.Context, name, workspace string, summary, typ *string) (NamedEntry, error) {
	entry, err := s.Get(ctx, name, workspace)
	if err != nil {
		return NamedEntry{}, err
	}
	if summary != nil {
		entry.Summary = *summary
	}
	if typ != nil && *typ != "" {
		entry.Type = *typ
	}
	entry.UpdatedAt = timeutil.NowUTC()
	if _, err := s.db.ExecContext(ctx, `
		UPDATE named_memory
		SET summary = ?, type = ?, updated_at = ?
		WHERE id = ?`, entry.Summary, entry.Type, sqlutil.FormatTimestamp(entry.UpdatedAt), entry.ID); err != nil {
		return NamedEntry{}, fmt.Errorf("memory: update: %w", err)
	}
	return entry, nil
}

// Relevant ranks entries by recency/access frequency.
func (s *Store) Relevant(ctx context.Context, workspace string, limit int) ([]ScoredEntry, error) {
	if limit <= 0 {
		limit = 10
	}
	// Fetch a larger window (500 most recently accessed entries) to score client-side.
	// This prevents loading all entries for large datasets while still providing good ranking
	const maxWindow = 500
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, type, workspace, summary, result, digests, created_at, updated_at, last_accessed, access_count
		FROM named_memory
		WHERE workspace = ?
		ORDER BY last_accessed DESC, updated_at DESC
		LIMIT ?`, workspace, maxWindow)
	if err != nil {
		return nil, fmt.Errorf("memory: relevant: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close memory relevant rows")
	}()
	entries, err := scanEntries(rows)
	if err != nil {
		return nil, err
	}
	scored := make([]ScoredEntry, 0, len(entries))
	for _, entry := range entries {
		scored = append(scored, ScoredEntry{
			Entry: entry,
			Score: scoreEntry(entry),
		})
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			return scored[i].Entry.UpdatedAt.After(scored[j].Entry.UpdatedAt)
		}
		return scored[i].Score > scored[j].Score
	})
	if len(scored) > limit {
		scored = scored[:limit]
	}
	return scored, nil
}

func scanEntries(rows *sql.Rows) ([]NamedEntry, error) {
	var out []NamedEntry
	for rows.Next() {
		var entry NamedEntry
		var digests string
		var created, updated, last string
		if err := rows.Scan(&entry.ID, &entry.Name, &entry.Type, &entry.Workspace, &entry.Summary, &entry.Result, &digests, &created, &updated, &last, &entry.AccessCount); err != nil {
			return nil, fmt.Errorf("memory: scan: %w", err)
		}

		// Parse digests with proper error handling
		if err := sqlutil.ScanJSON(digests, &entry.Digests); err != nil {
			return nil, fmt.Errorf("memory: scan digests: %w", err)
		}

		// Parse timestamps with proper error handling
		var err error
		entry.CreatedAt, err = sqlutil.ScanTimestamp(created)
		if err != nil {
			return nil, fmt.Errorf("memory: scan created_at: %w", err)
		}
		entry.UpdatedAt, err = sqlutil.ScanTimestamp(updated)
		if err != nil {
			return nil, fmt.Errorf("memory: scan updated_at: %w", err)
		}
		entry.LastAccess, err = sqlutil.ScanTimestamp(last)
		if err != nil {
			return nil, fmt.Errorf("memory: scan last_accessed: %w", err)
		}

		out = append(out, entry)
	}
	return out, nil
}

func scoreEntry(entry NamedEntry) float64 {
	recencyHours := time.Since(entry.LastAccess).Hours()
	if recencyHours < 0 {
		recencyHours = 0
	}
	recency := 1.0 / (1.0 + recencyHours/24.0)
	frequency := math.Log1p(float64(entry.AccessCount))
	return 0.6*recency + 0.4*frequency
}
