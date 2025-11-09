package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jkatigb/agentctl/internal/cache"
	"github.com/jkatigb/agentctl/internal/cas"
	_ "modernc.org/sqlite" // sqlite driver
)

// NamedEntry represents a saved memory.
type NamedEntry struct {
	ID          string
	Name        string
	Type        string
	Workspace   string
	Summary     string
	Result      []byte
	Digests     []string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	LastAccess  time.Time
	AccessCount int
}

// ScoredEntry couples a memory entry with its relevance score.
type ScoredEntry struct {
	Entry NamedEntry
	Score float64
}

// Store handles named memory persistence.
type Store struct {
	db  *sql.DB
	cas *cas.Store
}

// Open initializes the memory store rooted at the provided path.
func Open(ctx context.Context, root string, casPath string) (*Store, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("memory: ensure root: %w", err)
	}
	dbPath := filepath.Join(root, "memory.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("memory: open db: %w", err)
	}
	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode=WAL;`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("memory: pragma: %w", err)
	}
	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	var casStore *cas.Store
	if casPath != "" {
		if casStore, err = cas.NewStore(casPath); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return &Store{db: db, cas: casStore}, nil
}

// Close releases resources.
func (s *Store) Close() error {
	return s.db.Close()
}

// Save inserts or updates a named memory.
func (s *Store) Save(ctx context.Context, entry NamedEntry) (NamedEntry, error) {
	now := time.Now().UTC()
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
	digestsJSON, _ := json.Marshal(entry.Digests)

	_, err := s.db.ExecContext(ctx, `
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
`, entry.ID, entry.Name, entry.Type, entry.Workspace, entry.Summary, entry.Result, string(digestsJSON),
		entry.CreatedAt.Format(time.RFC3339Nano), entry.UpdatedAt.Format(time.RFC3339Nano), entry.LastAccess.Format(time.RFC3339Nano))
	if err != nil {
		return NamedEntry{}, fmt.Errorf("memory: save: %w", err)
	}
	s.pin(entry.Digests)
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
		if errorsIsNoRows(err) {
			return NamedEntry{}, ErrNotFound
		}
		return NamedEntry{}, fmt.Errorf("memory: get: %w", err)
	}
	_ = json.Unmarshal([]byte(digests), &entry.Digests)
	entry.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	entry.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	entry.LastAccess, _ = time.Parse(time.RFC3339Nano, last)

	_, _ = s.db.ExecContext(ctx, `
		UPDATE named_memory
		SET last_accessed = ?, access_count = access_count + 1
		WHERE id = ?`, time.Now().UTC().Format(time.RFC3339Nano), entry.ID)

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
	defer rows.Close()

	var entries []NamedEntry
	for rows.Next() {
		var entry NamedEntry
		var digests string
		var created, updated, last string
		if err := rows.Scan(&entry.ID, &entry.Name, &entry.Type, &entry.Workspace, &entry.Summary, &entry.Result, &digests, &created, &updated, &last, &entry.AccessCount); err != nil {
			return nil, fmt.Errorf("memory: scan list: %w", err)
		}
		_ = json.Unmarshal([]byte(digests), &entry.Digests)
		entry.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		entry.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		entry.LastAccess, _ = time.Parse(time.RFC3339Nano, last)
		entries = append(entries, entry)
	}
	return entries, nil
}

// Delete removes a named memory and unpins digests.
func (s *Store) Delete(ctx context.Context, name, workspace string) error {
	row := s.db.QueryRowContext(ctx, `SELECT digests FROM named_memory WHERE name = ? AND workspace = ?`, name, workspace)
	var digests string
	if err := row.Scan(&digests); err != nil {
		if errorsIsNoRows(err) {
			return ErrNotFound
		}
		return fmt.Errorf("memory: scan digests: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM named_memory WHERE name = ? AND workspace = ?`, name, workspace); err != nil {
		return fmt.Errorf("memory: delete: %w", err)
	}
	var arr []string
	_ = json.Unmarshal([]byte(digests), &arr)
	s.unpin(arr)
	return nil
}

// SaveFromResult is a helper that stores a result envelope.
func (s *Store) SaveFromResult(ctx context.Context, name, typ, workspace, summary string, result []byte) (NamedEntry, error) {
	entry := NamedEntry{
		Name:      name,
		Type:      typ,
		Workspace: workspace,
		Summary:   summary,
		Result:    result,
		Digests:   cache.CollectDigests(result),
	}
	return s.Save(ctx, entry)
}

// ErrNotFound indicates missing entries.
var ErrNotFound = fmt.Errorf("memory: not found")

func (s *Store) pin(digests []string) {
	if s.cas == nil {
		return
	}
	for _, d := range digests {
		_ = s.cas.Pin(context.Background(), d)
	}
}

func (s *Store) unpin(digests []string) {
	if s.cas == nil {
		return
	}
	for _, d := range digests {
		_ = s.cas.Unpin(context.Background(), d)
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
`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("memory: migrate: %w", err)
	}
	return nil
}

func errorsIsNoRows(err error) bool {
	return err == sql.ErrNoRows
}

// Search finds entries whose name or summary contain the query string.
func (s *Store) Search(ctx context.Context, workspace, query string, limit int) ([]NamedEntry, error) {
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
	defer rows.Close()
	return scanEntries(rows)
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
	entry.UpdatedAt = time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `
		UPDATE named_memory
		SET summary = ?, type = ?, updated_at = ?
		WHERE id = ?`, entry.Summary, entry.Type, entry.UpdatedAt.Format(time.RFC3339Nano), entry.ID); err != nil {
		return NamedEntry{}, fmt.Errorf("memory: update: %w", err)
	}
	return entry, nil
}

// Relevant ranks entries by recency/access frequency.
func (s *Store) Relevant(ctx context.Context, workspace string, limit int) ([]ScoredEntry, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, type, workspace, summary, result, digests, created_at, updated_at, last_accessed, access_count
		FROM named_memory
		WHERE workspace = ?`, workspace)
	if err != nil {
		return nil, fmt.Errorf("memory: relevant: %w", err)
	}
	defer rows.Close()
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
		_ = json.Unmarshal([]byte(digests), &entry.Digests)
		entry.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		entry.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		entry.LastAccess, _ = time.Parse(time.RFC3339Nano, last)
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
