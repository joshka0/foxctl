package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	errs "github.com/joshka0/foxctl/internal/platform/errors"
	"github.com/joshka0/foxctl/internal/platform/timeutil"
	ws "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/storage/dbdriver"
	"github.com/joshka0/foxctl/internal/storage/dbutil"
	"github.com/joshka0/foxctl/internal/storage/sqliteutil"
	"github.com/joshka0/foxctl/internal/storage/sqlutil"
)

// Ensure TursoStore implements VectorMemoryStore.
var _ VectorMemoryStore = (*TursoStore)(nil)

// VectorMemoryStore extends MemoryStore with vector search capabilities.
type VectorMemoryStore interface {
	// SearchSimilar finds entries similar to the given embedding using vector search.
	SearchSimilar(ctx context.Context, workspace string, embedding []float32, limit int) ([]ScoredEntry, error)
	// SaveWithEmbedding saves a named memory with its embedding vector.
	SaveWithEmbedding(ctx context.Context, entry NamedEntry, embedding []float32, model string) (NamedEntry, error)
	// Close releases resources.
	Close() error
	// Stats returns memory statistics.
	Stats(ctx context.Context) (Stats, error)
}

// TursoStore handles named memory persistence using Turso with native vector search.
type TursoStore struct {
	db              dbdriver.DB
	vh              *dbdriver.VectorHelper
	vectorDimension int // configured embedding dimensions
}

// DefaultEmbeddingModel is the default model for embeddings.
const DefaultEmbeddingModel = "gemini-embedding-001"

// OpenTurso initializes a memory store using Turso database.
func OpenTurso(ctx context.Context, cfg dbdriver.TursoConfig) (*TursoStore, error) {
	cfg.EnableVectorSearch = true
	if cfg.VectorDimensions == 0 {
		cfg.VectorDimensions = dbdriver.GetDefaultVectorDimensions()
	}
	return openVectorStore(ctx, dbdriver.Config{
		Driver: dbdriver.DriverTurso,
		Turso:  cfg,
	}, cfg.VectorDimensions, "turso")
}

func openVectorStore(ctx context.Context, cfg dbdriver.Config, vectorDimensions int, backend string) (*TursoStore, error) {
	// Create migration function that uses configured dimensions.
	migrate := func(ctx context.Context, db *sql.DB) error {
		return migrateTursoWithDimensions(ctx, db, vectorDimensions)
	}

	db, err := dbdriver.OpenDB(ctx, cfg, migrate)
	if err != nil {
		return nil, fmt.Errorf("memory: open %s: %w", backend, err)
	}

	// Create vector helper
	vh, err := dbdriver.NewVectorHelper(db)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("memory: create vector helper: %w", err)
	}

	db.SetMaxOpenConns(defaultMaxOpenConns)
	db.SetMaxIdleConns(defaultMaxIdleConns)
	db.SetConnMaxLifetime(defaultConnMaxLifetime)
	db.SetConnMaxIdleTime(defaultConnMaxIdleTime)

	store := &TursoStore{db: db, vh: vh, vectorDimension: vectorDimensions}

	// Validate dimension metadata matches configuration
	if err := store.validateDimensions(ctx, vectorDimensions); err != nil {
		_ = db.Close()
		return nil, err
	}

	store.repairWorkspaceIDs(ctx)
	return store, nil
}

// validateDimensions checks that stored metadata dimensions match config.
func (s *TursoStore) validateDimensions(ctx context.Context, expectedDims int) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT dimensions FROM embedding_metadata
	`)
	if err != nil {
		// Table might not exist yet (pre-migration), skip validation
		return nil
	}
	defer func() {
		errs.Ignore(rows.Close(), "close embedding_metadata rows")
	}()

	for rows.Next() {
		var storedDims int
		if scanErr := rows.Scan(&storedDims); scanErr != nil {
			return nil
		}
		if storedDims != expectedDims {
			return fmt.Errorf("memory: dimension mismatch: stored=%d, config=%d (recreate database or update config)", storedDims, expectedDims)
		}
	}
	if err := rows.Err(); err != nil {
		return nil
	}
	return nil
}

// migrateTursoWithDimensions runs migrations with configurable vector dimensions.
// Parameters: ctx (context), db (database connection), dimensions (vector size).
func migrateTursoWithDimensions(ctx context.Context, db *sql.DB, dimensions int) error {
	// Create embedding_metadata table to track provider/model/dimensions per workspace
	// This enables detection of dimension mismatches if embedding models change
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS embedding_metadata (
			workspace TEXT PRIMARY KEY,
			provider TEXT NOT NULL,
			model TEXT NOT NULL,
			dimensions INTEGER NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)
	`)
	if err != nil {
		return fmt.Errorf("create embedding_metadata table: %w", err)
	}

	_, err = db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS indexer_state (
			workspace TEXT NOT NULL,
			indexer_id TEXT NOT NULL,
			last_indexed_head_sha TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(workspace, indexer_id)
		)
	`)
	if err != nil {
		return fmt.Errorf("create indexer_state table: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_indexer_state_ws ON indexer_state(workspace)`); err != nil {
		return fmt.Errorf("create indexer_state index: %w", err)
	}

	// Create named_memory table with BLOB embeddings for Turso vector32() values.
	memoryQuery := `
		CREATE TABLE IF NOT EXISTS named_memory (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			workspace TEXT NOT NULL,
			session_id TEXT,
			summary TEXT,
			result BLOB NOT NULL,
			digests TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			last_accessed TEXT NOT NULL,
			access_count INTEGER NOT NULL DEFAULT 0,
			embedding BLOB,
			embedding_model TEXT,
			UNIQUE(name, workspace)
		)
	`
	if _, err = db.ExecContext(ctx, memoryQuery); err != nil {
		return fmt.Errorf("create named_memory table: %w", err)
	}

	// Create indexes
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_named_memory_ws_updated ON named_memory(workspace, updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_named_memory_name_ws ON named_memory(name, workspace)`,
		`CREATE INDEX IF NOT EXISTS idx_named_memory_session ON named_memory(session_id)`,
	}
	for _, idx := range indexes {
		if _, err := db.ExecContext(ctx, idx); err != nil {
			return fmt.Errorf("create index: %w", err)
		}
	}

	// Add atomic processing columns for SimpleMem-style semantic lossless compression.
	// These columns support UpdateAtomic which stores self-contained rewrites and extracted metadata.
	// Ignore duplicate-column errors for existing databases.
	atomicColumns := []string{
		`ALTER TABLE named_memory ADD COLUMN atomic_text TEXT`, // Self-contained, disambiguated rewrite
		`ALTER TABLE named_memory ADD COLUMN entities TEXT`,    // JSON array of extracted entities
		`ALTER TABLE named_memory ADD COLUMN keywords TEXT`,    // JSON array of BM25 keywords
	}
	for _, stmt := range atomicColumns {
		// Ignore errors from "duplicate column" - columns may already exist.
		_, _ = db.ExecContext(ctx, stmt) //nolint:errcheck
	}

	lifecycleColumns := []string{
		`ALTER TABLE named_memory ADD COLUMN lifecycle_state TEXT NOT NULL DEFAULT 'active'`,
		`ALTER TABLE named_memory ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE named_memory ADD COLUMN review_status TEXT NOT NULL DEFAULT 'unreviewed'`,
		`ALTER TABLE named_memory ADD COLUMN superseded_by TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE named_memory ADD COLUMN review_notes TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE named_memory ADD COLUMN last_used_at TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE named_memory ADD COLUMN last_validated_at TEXT NOT NULL DEFAULT ''`,
	}
	for _, stmt := range lifecycleColumns {
		_, _ = db.ExecContext(ctx, stmt) //nolint:errcheck
	}
	telemetryColumns := []string{
		`ALTER TABLE named_memory ADD COLUMN selected_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE named_memory ADD COLUMN use_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE named_memory ADD COLUMN success_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE named_memory ADD COLUMN failure_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE named_memory ADD COLUMN patch_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE named_memory ADD COLUMN restore_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE named_memory ADD COLUMN last_selected_at TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE named_memory ADD COLUMN last_succeeded_at TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE named_memory ADD COLUMN last_failed_at TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE named_memory ADD COLUMN last_patched_at TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE named_memory ADD COLUMN last_restored_at TEXT NOT NULL DEFAULT ''`,
	}
	for _, stmt := range telemetryColumns {
		_, _ = db.ExecContext(ctx, stmt) //nolint:errcheck
	}
	if _, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_named_memory_lifecycle ON named_memory(workspace, lifecycle_state, updated_at DESC)`); err != nil {
		return fmt.Errorf("create lifecycle index: %w", err)
	}

	return nil
}

// Close releases resources.
func (s *TursoStore) Close() error {
	return s.db.Close()
}

// Stats returns memory count.
func (s *TursoStore) Stats(ctx context.Context) (Stats, error) {
	var count int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM named_memory`).Scan(&count); err != nil {
		return Stats{}, fmt.Errorf("memory: stats: %w", err)
	}

	var withEmbedding int64
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM named_memory WHERE embedding IS NOT NULL`).Scan(&withEmbedding)

	return Stats{
		Named: count,
		Path:  "turso",
	}, nil
}

// ExistsByNameSuffix checks if any entry exists with a name ending in the given suffix.
// Used for content-hash deduplication across sessions (e.g., suffix ":<type>:<digest>").
func (s *TursoStore) ExistsByNameSuffix(ctx context.Context, workspace, suffix string) (bool, error) {
	workspace = ws.CanonicalID(workspace)
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM named_memory
		WHERE workspace = ? AND name LIKE '%' || ?`,
		workspace, suffix).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("memory: exists by suffix: %w", err)
	}
	return count > 0, nil
}

// SaveWithEmbedding saves a named memory entry with its embedding vector.
func (s *TursoStore) SaveWithEmbedding(ctx context.Context, entry NamedEntry, embedding []float32, model string) (NamedEntry, error) {
	entry.Workspace = ws.CanonicalID(entry.Workspace)
	now := timeutil.NowUTC()
	if entry.ID == "" {
		entry.ID = fmt.Sprintf("%s:%s", entry.Workspace, entry.Name)
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	entry.UpdatedAt = now
	entry.LastAccess = now
	if entry.Type == "" {
		entry.Type = "result"
	}
	if strings.TrimSpace(entry.LifecycleState) == "" {
		entry.LifecycleState = "active"
	}
	if strings.TrimSpace(entry.ReviewStatus) == "" {
		entry.ReviewStatus = "unreviewed"
	}

	// Validate embedding dimensions
	if len(embedding) != s.vectorDimension {
		return NamedEntry{}, fmt.Errorf("memory: embedding dimension mismatch: got %d, expected %d", len(embedding), s.vectorDimension)
	}

	// Format digests
	digestsJSON, err := sqlutil.FormatJSON(entry.Digests)
	if err != nil {
		return NamedEntry{}, fmt.Errorf("memory: format digests: %w", err)
	}

	vec := make(dbdriver.Vector, len(embedding))
	copy(vec, embedding)
	vectorExpr := s.vh.VectorExpression(vec)

	query := fmt.Sprintf(`
		INSERT INTO named_memory (
			id, name, type, workspace, summary, result, digests, session_id,
			created_at, updated_at, last_accessed, access_count,
			lifecycle_state, pinned, review_status, superseded_by, review_notes, last_used_at, last_validated_at,
			selected_count, use_count, success_count, failure_count, patch_count, restore_count,
			last_selected_at, last_succeeded_at, last_failed_at, last_patched_at, last_restored_at,
			embedding, embedding_model
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, %s, ?)
		ON CONFLICT(name, workspace) DO UPDATE SET
			id = excluded.id,
			type = excluded.type,
			summary = excluded.summary,
			result = excluded.result,
			digests = excluded.digests,
			session_id = COALESCE(NULLIF(excluded.session_id, ''), session_id),
			updated_at = excluded.updated_at,
			last_accessed = excluded.last_accessed,
			embedding = excluded.embedding,
			embedding_model = excluded.embedding_model
	`, vectorExpr)

	_, err = s.db.ExecContext(ctx, query,
		entry.ID, entry.Name, entry.Type, entry.Workspace, entry.Summary, entry.Result, digestsJSON, entry.SessionID,
		sqlutil.FormatTimestamp(entry.CreatedAt), sqlutil.FormatTimestamp(entry.UpdatedAt),
		sqlutil.FormatTimestamp(entry.LastAccess),
		entry.LifecycleState, boolToInt(entry.Pinned), entry.ReviewStatus, entry.SupersededBy, entry.ReviewNotes,
		sqlutil.FormatTimestamp(entry.LastUsedAt), sqlutil.FormatTimestamp(entry.LastValidatedAt),
		entry.SelectedCount, entry.UseCount, entry.SuccessCount, entry.FailureCount, entry.PatchCount, entry.RestoreCount,
		sqlutil.FormatTimestamp(entry.LastSelectedAt), sqlutil.FormatTimestamp(entry.LastSucceededAt),
		sqlutil.FormatTimestamp(entry.LastFailedAt), sqlutil.FormatTimestamp(entry.LastPatchedAt),
		sqlutil.FormatTimestamp(entry.LastRestoredAt), model)
	if err != nil {
		return NamedEntry{}, fmt.Errorf("memory: save with embedding: %w", err)
	}

	return entry, nil
}

// SearchSimilar finds entries similar to the given embedding using native vector search.
func (s *TursoStore) SearchSimilar(ctx context.Context, workspace string, embedding []float32, limit int) ([]ScoredEntry, error) {
	if limit <= 0 {
		limit = 10
	}
	workspace = ws.CanonicalID(workspace)

	// Convert query embedding to dbdriver.Vector
	vec := make(dbdriver.Vector, len(embedding))
	copy(vec, embedding)

	distExpr := s.vh.CosineSimilarity("embedding", vec)
	query := fmt.Sprintf(`
		SELECT %s,
			%s as distance
		FROM named_memory
		WHERE embedding IS NOT NULL AND workspace = ?
		ORDER BY distance ASC
		LIMIT ?`, namedEntrySelectColumns, distExpr)
	rows, err := s.db.QueryContext(ctx, query, workspace, limit)
	if err != nil {
		return nil, fmt.Errorf("memory: search similar: %w", err)
	}
	defer func() { errs.Ignore(rows.Close(), "close similar rows") }()

	var results []ScoredEntry
	for rows.Next() {
		var entry NamedEntry
		var distance float64

		if err := scanEntryValues(rows, &entry, &distance); err != nil {
			return nil, fmt.Errorf("memory: scan similar: %w", err)
		}

		// Convert cosine distance to similarity (distance is in [0, 2], normalize to [0, 1])
		// distance=0 (identical) → similarity=1.0
		// distance=2 (opposite) → similarity=0.0
		similarity := 1.0 - distance/2.0

		results = append(results, ScoredEntry{
			Entry: entry,
			Score: similarity,
		})
	}

	// Sort by similarity descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	// Apply limit
	if len(results) > limit {
		results = results[:limit]
	}

	return results, rows.Err()
}

// SearchSimilarByType finds entries of a specific type using native vector search.
func (s *TursoStore) SearchSimilarByType(ctx context.Context, workspace, entryType string, embedding []float32, limit int) ([]ScoredEntry, error) {
	workspace = ws.CanonicalID(workspace)
	if entryType == "" {
		return s.SearchSimilar(ctx, workspace, embedding, limit)
	}
	if limit <= 0 {
		limit = 10
	}

	vec := make(dbdriver.Vector, len(embedding))
	copy(vec, embedding)

	distExpr := s.vh.CosineSimilarity("embedding", vec)
	query := fmt.Sprintf(`
		SELECT %s,
			%s as distance
		FROM named_memory
		WHERE embedding IS NOT NULL AND workspace = ? AND type = ?
		ORDER BY distance ASC
		LIMIT ?`, namedEntrySelectColumns, distExpr)
	rows, err := s.db.QueryContext(ctx, query, workspace, entryType, limit)
	if err != nil {
		return nil, fmt.Errorf("memory: search similar by type: %w", err)
	}
	defer func() { errs.Ignore(rows.Close(), "close similar rows") }()

	var results []ScoredEntry
	for rows.Next() {
		var entry NamedEntry
		var distance float64

		if err := scanEntryValues(rows, &entry, &distance); err != nil {
			return nil, fmt.Errorf("memory: scan similar by type: %w", err)
		}

		similarity := 1.0 - distance/2.0

		results = append(results, ScoredEntry{
			Entry: entry,
			Score: similarity,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > limit {
		results = results[:limit]
	}

	return results, rows.Err()
}

// --- Basic CRUD operations (delegate to underlying store patterns) ---

// Save saves a named memory entry without embedding.
func (s *TursoStore) Save(ctx context.Context, entry NamedEntry) (NamedEntry, error) {
	entry.Workspace = ws.CanonicalID(entry.Workspace)
	now := timeutil.NowUTC()
	if entry.ID == "" {
		entry.ID = fmt.Sprintf("%s:%s", entry.Workspace, entry.Name)
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	entry.UpdatedAt = now
	entry.LastAccess = now
	if entry.Type == "" {
		entry.Type = "result"
	}
	if strings.TrimSpace(entry.LifecycleState) == "" {
		entry.LifecycleState = "active"
	}
	if strings.TrimSpace(entry.ReviewStatus) == "" {
		entry.ReviewStatus = "unreviewed"
	}

	digestsJSON, err := sqlutil.FormatJSON(entry.Digests)
	if err != nil {
		return NamedEntry{}, fmt.Errorf("memory: format digests: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO named_memory (
			id, name, type, workspace, summary, result, digests, session_id,
			created_at, updated_at, last_accessed, access_count,
			lifecycle_state, pinned, review_status, superseded_by, review_notes, last_used_at, last_validated_at,
			selected_count, use_count, success_count, failure_count, patch_count, restore_count,
			last_selected_at, last_succeeded_at, last_failed_at, last_patched_at, last_restored_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(name, workspace) DO UPDATE SET
			id = excluded.id,
			type = excluded.type,
			summary = excluded.summary,
			result = excluded.result,
			digests = excluded.digests,
			session_id = COALESCE(NULLIF(excluded.session_id, ''), session_id),
			updated_at = excluded.updated_at,
			last_accessed = excluded.last_accessed
	`, entry.ID, entry.Name, entry.Type, entry.Workspace, entry.Summary, entry.Result, digestsJSON, entry.SessionID,
		sqlutil.FormatTimestamp(entry.CreatedAt), sqlutil.FormatTimestamp(entry.UpdatedAt),
		sqlutil.FormatTimestamp(entry.LastAccess),
		entry.LifecycleState, boolToInt(entry.Pinned), entry.ReviewStatus, entry.SupersededBy, entry.ReviewNotes,
		sqlutil.FormatTimestamp(entry.LastUsedAt), sqlutil.FormatTimestamp(entry.LastValidatedAt),
		entry.SelectedCount, entry.UseCount, entry.SuccessCount, entry.FailureCount, entry.PatchCount, entry.RestoreCount,
		sqlutil.FormatTimestamp(entry.LastSelectedAt), sqlutil.FormatTimestamp(entry.LastSucceededAt),
		sqlutil.FormatTimestamp(entry.LastFailedAt), sqlutil.FormatTimestamp(entry.LastPatchedAt),
		sqlutil.FormatTimestamp(entry.LastRestoredAt))
	if err != nil {
		return NamedEntry{}, fmt.Errorf("memory: save: %w", err)
	}

	return entry, nil
}

// Get retrieves a named memory by name+workspace.
func (s *TursoStore) Get(ctx context.Context, name, workspace string) (NamedEntry, error) {
	workspace = ws.CanonicalID(workspace)
	row := s.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT %s
		FROM named_memory
		WHERE name = ? AND workspace = ?`, namedEntrySelectColumns), name, workspace)
	entry, err := scanEntry(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return NamedEntry{}, ErrNotFound
		}
		return NamedEntry{}, fmt.Errorf("memory: get: %w", err)
	}

	// Update access metadata
	_, _ = s.db.ExecContext(ctx, `
		UPDATE named_memory
		SET last_accessed = ?, access_count = access_count + 1
		WHERE id = ?`, sqlutil.FormatTimestamp(timeutil.NowUTC()), entry.ID)

	return entry, nil
}

// getWithoutTracking retrieves a named memory without updating access metadata.
// Use this for internal operations (like Update) that shouldn't count as user access.
func (s *TursoStore) getWithoutTracking(ctx context.Context, name, workspace string) (NamedEntry, error) {
	workspace = ws.CanonicalID(workspace)
	row := s.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT %s
		FROM named_memory
		WHERE name = ? AND workspace = ?`, namedEntrySelectColumns), name, workspace)
	entry, err := scanEntry(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return NamedEntry{}, ErrNotFound
		}
		return NamedEntry{}, fmt.Errorf("memory: get: %w", err)
	}

	// No access tracking update - intentional for internal use
	return entry, nil
}

// List returns named memories for a workspace.
func (s *TursoStore) List(ctx context.Context, workspace string, limit int) ([]NamedEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	workspace = ws.CanonicalID(workspace)

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT %s
		FROM named_memory
		WHERE workspace = ?
		ORDER BY updated_at DESC
		LIMIT ?`, namedEntrySelectColumns), workspace, limit)
	if err != nil {
		return nil, fmt.Errorf("memory: list: %w", err)
	}
	defer func() { errs.Ignore(rows.Close(), "close list rows") }()

	return scanEntries(rows)
}

// Delete removes a named memory.
func (s *TursoStore) Delete(ctx context.Context, name, workspace string) error {
	workspace = ws.CanonicalID(workspace)
	result, err := s.db.ExecContext(ctx, `DELETE FROM named_memory WHERE name = ? AND workspace = ?`, name, workspace)
	if err != nil {
		return fmt.Errorf("memory: delete: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// Search performs text search on memory names and summaries.
func (s *TursoStore) Search(ctx context.Context, workspace, query string, limit int) ([]ScoredEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	workspace = ws.CanonicalID(workspace)

	like := "%" + strings.ToLower(query) + "%"
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+namedEntrySelectColumns+`
		FROM named_memory
		WHERE workspace = ? AND (LOWER(name) LIKE ? OR LOWER(summary) LIKE ?)
		ORDER BY updated_at DESC
		LIMIT ?`, workspace, like, like, limit)
	if err != nil {
		return nil, fmt.Errorf("memory: search: %w", err)
	}
	defer func() { errs.Ignore(rows.Close(), "close search rows") }()

	var results []ScoredEntry
	for rows.Next() {
		var entry NamedEntry

		if err := scanEntryValues(rows, &entry); err != nil {
			continue
		}

		results = append(results, ScoredEntry{
			Entry: entry,
			Score: scoreEntry(entry),
		})
	}

	// Sort by score
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results, rows.Err()
}

// UpdateEmbedding stores an embedding vector for a named memory entry.
func (s *TursoStore) UpdateEmbedding(ctx context.Context, name, workspace string, embedding []float32) error {
	if len(embedding) == 0 {
		return nil
	}
	workspace = ws.CanonicalID(workspace)

	// Validate embedding dimensions
	if len(embedding) != s.vectorDimension {
		return fmt.Errorf("memory: embedding dimension mismatch: got %d, expected %d", len(embedding), s.vectorDimension)
	}

	vec := make(dbdriver.Vector, len(embedding))
	copy(vec, embedding)
	vectorExpr := s.vh.VectorExpression(vec)

	query := fmt.Sprintf(`
		UPDATE named_memory
		SET embedding = %s, updated_at = ?
		WHERE name = ? AND workspace = ?
	`, vectorExpr)

	_, err := s.db.ExecContext(ctx, query, sqlutil.FormatTimestamp(timeutil.NowUTC()), name, workspace)
	if err != nil {
		return fmt.Errorf("memory: update embedding: %w", err)
	}
	return nil
}

// GetEmbedding retrieves the embedding vector for a named memory entry.
// Returns nil when the entry exists but has no stored embedding.
func (s *TursoStore) GetEmbedding(ctx context.Context, name, workspace string) ([]float32, error) {
	workspace = ws.CanonicalID(workspace)
	query := fmt.Sprintf(`
		SELECT %s
		FROM named_memory
		WHERE name = ? AND workspace = ?
	`, s.vh.ExtractVector("embedding"))

	var embeddingStr sql.NullString
	err := s.db.QueryRowContext(ctx, query, name, workspace).Scan(&embeddingStr)
	if err != nil {
		if dbutil.IsNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("memory: get embedding: %w", err)
	}
	if !embeddingStr.Valid || strings.TrimSpace(embeddingStr.String) == "" {
		return nil, nil
	}
	embedding, err := dbdriver.ParseVector(embeddingStr.String)
	if err != nil {
		return nil, fmt.Errorf("memory: parse embedding: %w", err)
	}
	return []float32(embedding), nil
}

// SyncSymbolEmbeddings fills Turso named_memory.embedding from queued symbol embeddings.
// The embedding queue is SQLite-backed, so Turso cannot rely on ATTACH DATABASE here.
func (s *TursoStore) SyncSymbolEmbeddings(ctx context.Context, embeddingDBPath string, opts SyncSymbolEmbeddingsOptions) (int, error) {
	if s == nil {
		return 0, fmt.Errorf("memory: sync embeddings: nil store")
	}
	embeddingDBPath = strings.TrimSpace(embeddingDBPath)
	if embeddingDBPath == "" {
		return 0, fmt.Errorf("memory: sync embeddings: embedding db path required")
	}
	workspaceID := ws.CanonicalID(strings.TrimSpace(opts.WorkspaceID))
	if workspaceID == "" {
		return 0, fmt.Errorf("memory: sync embeddings: workspace required")
	}

	embeddingDB, err := sqliteutil.OpenDB(ctx, embeddingDBPath, nil)
	if err != nil {
		return 0, fmt.Errorf("memory: sync embeddings: open embedding db: %w", err)
	}
	defer func() {
		errs.Ignore(embeddingDB.Close(), "close embedding sync db")
	}()

	query := `
SELECT symbol_id, embedding, model, dimensions
FROM symbol_embeddings
WHERE workspace_id = ?`
	args := []any{workspaceID}
	if len(opts.SymbolIDs) > 0 {
		ids := make([]string, 0, len(opts.SymbolIDs))
		for _, id := range opts.SymbolIDs {
			id = strings.TrimSpace(id)
			if id != "" {
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			placeholders := make([]string, 0, len(ids))
			for _, id := range ids {
				placeholders = append(placeholders, "?")
				args = append(args, id)
			}
			query += " AND symbol_id IN (" + strings.Join(placeholders, ", ") + ")"
		}
	}

	rows, err := embeddingDB.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("memory: sync embeddings: query symbols: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close symbol embedding rows")
	}()

	updated := 0
	var firstModel string
	firstDimensions := 0
	for rows.Next() {
		var symbolID string
		var embeddingBytes []byte
		var model string
		var dimensions int
		if err := rows.Scan(&symbolID, &embeddingBytes, &model, &dimensions); err != nil {
			return updated, fmt.Errorf("memory: sync embeddings: scan symbol: %w", err)
		}

		var embedding []float32
		if err := json.Unmarshal(embeddingBytes, &embedding); err != nil {
			return updated, fmt.Errorf("memory: sync embeddings: decode %s: %w", symbolID, err)
		}
		if len(embedding) == 0 {
			continue
		}
		if dimensions > 0 && dimensions != len(embedding) {
			return updated, fmt.Errorf("memory: sync embeddings: %s dimensions metadata=%d vector=%d", symbolID, dimensions, len(embedding))
		}
		if s.vectorDimension > 0 && len(embedding) != s.vectorDimension {
			return updated, fmt.Errorf("memory: embedding dimension mismatch: got %d, expected %d", len(embedding), s.vectorDimension)
		}

		vec := make(dbdriver.Vector, len(embedding))
		copy(vec, embedding)
		vectorExpr := s.vh.VectorExpression(vec)
		where := "name = ? AND workspace = ?"
		if opts.OnlyMissing {
			where += " AND (embedding IS NULL OR LENGTH(embedding) = 0)"
		}
		stmt := fmt.Sprintf(`
UPDATE named_memory
SET embedding = %s, embedding_model = ?, updated_at = ?
WHERE %s
`, vectorExpr, where)
		result, err := s.db.ExecContext(
			ctx, stmt,
			model,
			sqlutil.FormatTimestamp(timeutil.NowUTC()),
			fmt.Sprintf("symbol://%s/%s", workspaceID, symbolID),
			workspaceID,
		)
		if err != nil {
			return updated, fmt.Errorf("memory: sync embeddings: update %s: %w", symbolID, err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return updated, fmt.Errorf("memory: sync embeddings: rows affected: %w", err)
		}
		updated += int(affected)
		if affected > 0 && firstDimensions == 0 {
			firstModel = strings.TrimSpace(model)
			firstDimensions = len(embedding)
		}
	}
	if err := rows.Err(); err != nil {
		return updated, fmt.Errorf("memory: sync embeddings: rows: %w", err)
	}
	if updated > 0 && firstDimensions > 0 {
		if err := s.SetEmbeddingMetadata(ctx, EmbeddingMetadata{
			Workspace:  workspaceID,
			Model:      firstModel,
			Dimensions: firstDimensions,
		}); err != nil {
			return updated, err
		}
	}
	return updated, nil
}

// SearchSimilarGlobal finds entries similar to the given embedding across ALL workspaces.
// This enables cross-workspace knowledge sharing when using a centralized Turso database.
func (s *TursoStore) SearchSimilarGlobal(ctx context.Context, embedding []float32, limit int) ([]ScoredEntry, error) {
	if limit <= 0 {
		limit = 10
	}

	// Convert query embedding to dbdriver.Vector
	vec := make(dbdriver.Vector, len(embedding))
	copy(vec, embedding)

	distExpr := s.vh.CosineSimilarity("embedding", vec)
	query := fmt.Sprintf(`
		SELECT %s,
			%s as distance
		FROM named_memory
		WHERE embedding IS NOT NULL
		ORDER BY distance ASC
		LIMIT ?`, namedEntrySelectColumns, distExpr)
	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("memory: search similar global: %w", err)
	}
	defer func() { errs.Ignore(rows.Close(), "close global search rows") }()

	var results []ScoredEntry
	for rows.Next() {
		var entry NamedEntry
		var distance float64

		if err := scanEntryValues(rows, &entry, &distance); err != nil {
			return nil, fmt.Errorf("memory: scan similar global: %w", err)
		}

		// Convert cosine distance to similarity (distance is in [0, 2], normalize to [0, 1])
		// distance=0 (identical) → similarity=1.0
		// distance=2 (opposite) → similarity=0.0
		similarity := 1.0 - distance/2.0

		results = append(results, ScoredEntry{
			Entry: entry,
			Score: similarity,
		})
	}

	// Sort by similarity descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	// Apply limit
	if len(results) > limit {
		results = results[:limit]
	}

	return results, rows.Err()
}

// SearchSimilarMultiWorkspace finds entries similar to the given embedding in specified workspaces.
// Useful for targeted cross-workspace search when you know which workspaces to query.
func (s *TursoStore) SearchSimilarMultiWorkspace(ctx context.Context, workspaces []string, embedding []float32, limit int) ([]ScoredEntry, error) {
	if limit <= 0 {
		limit = 10
	}
	if len(workspaces) == 0 {
		return nil, nil
	}

	for i := range workspaces {
		workspaces[i] = ws.CanonicalID(workspaces[i])
	}

	// Convert query embedding to dbdriver.Vector
	vec := make(dbdriver.Vector, len(embedding))
	copy(vec, embedding)

	// Build workspace IN clause
	placeholders := make([]string, len(workspaces))
	args := make([]any, len(workspaces)+1) // workspaces + limit
	for i, ws := range workspaces {
		placeholders[i] = "?"
		args[i] = ws
	}
	args[len(workspaces)] = limit
	inClause := strings.Join(placeholders, ", ")

	var rows *sql.Rows
	var err error

	// Full table scan with workspace filter (index doesn't filter by workspace)
	distExpr := s.vh.CosineSimilarity("embedding", vec)
	query := fmt.Sprintf(`
		SELECT %s,
			%s as distance
		FROM named_memory
		WHERE embedding IS NOT NULL AND workspace IN (%s)
		ORDER BY distance ASC
		LIMIT ?`, namedEntrySelectColumns, distExpr, inClause)
	rows, err = s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("memory: search similar multi-workspace: %w", err)
	}
	defer func() { errs.Ignore(rows.Close(), "close multi-workspace search rows") }()

	var results []ScoredEntry
	for rows.Next() {
		var entry NamedEntry
		var distance float64

		if err := scanEntryValues(rows, &entry, &distance); err != nil {
			return nil, fmt.Errorf("memory: scan similar multi-workspace: %w", err)
		}

		similarity := 1.0 - distance/2.0
		results = append(results, ScoredEntry{
			Entry: entry,
			Score: similarity,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > limit {
		results = results[:limit]
	}

	return results, rows.Err()
}

// ListWorkspaces returns distinct workspace IDs in the store.
func (s *TursoStore) ListWorkspaces(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT workspace FROM named_memory ORDER BY workspace`)
	if err != nil {
		return nil, fmt.Errorf("memory: list workspaces: %w", err)
	}
	defer func() { errs.Ignore(rows.Close(), "close workspace rows") }()

	var workspaces []string
	for rows.Next() {
		var ws string
		if err := rows.Scan(&ws); err != nil {
			continue
		}
		workspaces = append(workspaces, ws)
	}
	return workspaces, rows.Err()
}

// DeleteByNamePrefix deletes all entries matching the name prefix.
func (s *TursoStore) DeleteByNamePrefix(ctx context.Context, workspace, namePrefix string) (int, error) {
	workspace = ws.CanonicalID(workspace)
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM named_memory
		WHERE workspace = ? AND name LIKE ? || '%'`,
		workspace, namePrefix)
	if err != nil {
		return 0, fmt.Errorf("memory: delete by prefix: %w", err)
	}
	count, _ := result.RowsAffected()
	return int(count), nil
}

// Update updates an existing memory entry's summary and/or type.
// Uses getWithoutTracking to avoid incrementing access_count for internal operations.
func (s *TursoStore) Update(ctx context.Context, name, workspace string, summary, typ *string) (NamedEntry, error) {
	entry, err := s.getWithoutTracking(ctx, name, workspace)
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

// UpdateLifecycle updates lifecycle metadata for a named memory entry.
func (s *TursoStore) UpdateLifecycle(ctx context.Context, name, workspace string, update LifecycleUpdate) (NamedEntry, error) {
	entry, err := s.getWithoutTracking(ctx, name, workspace)
	if err != nil {
		return NamedEntry{}, err
	}
	if strings.TrimSpace(entry.LifecycleState) == "" {
		entry.LifecycleState = "active"
	}
	if strings.TrimSpace(entry.ReviewStatus) == "" {
		entry.ReviewStatus = "unreviewed"
	}
	if state := strings.TrimSpace(update.LifecycleState); state != "" {
		entry.LifecycleState = state
	}
	if status := strings.TrimSpace(update.ReviewStatus); status != "" {
		entry.ReviewStatus = status
	}
	entry.SupersededBy = strings.TrimSpace(update.SupersededBy)
	entry.ReviewNotes = strings.TrimSpace(update.ReviewNotes)
	if update.LastUsedAt != nil {
		entry.LastUsedAt = update.LastUsedAt.UTC()
	}
	if update.LastValidatedAt != nil {
		entry.LastValidatedAt = update.LastValidatedAt.UTC()
	}
	entry.UpdatedAt = timeutil.NowUTC()
	if _, err := s.db.ExecContext(
		ctx, `
		UPDATE named_memory
		SET lifecycle_state = ?,
		    review_status = ?,
		    superseded_by = ?,
		    review_notes = ?,
		    last_used_at = ?,
		    last_validated_at = ?,
		    updated_at = ?
		WHERE id = ?`,
		entry.LifecycleState,
		entry.ReviewStatus,
		entry.SupersededBy,
		entry.ReviewNotes,
		sqlutil.FormatTimestamp(entry.LastUsedAt),
		sqlutil.FormatTimestamp(entry.LastValidatedAt),
		sqlutil.FormatTimestamp(entry.UpdatedAt),
		entry.ID,
	); err != nil {
		return NamedEntry{}, fmt.Errorf("memory: update lifecycle: %w", err)
	}
	return entry, nil
}

// UpdateTelemetry records an explicit telemetry action for a named memory entry.
func (s *TursoStore) UpdateTelemetry(ctx context.Context, name, workspace string, update TelemetryUpdate) (NamedEntry, error) {
	workspace = ws.CanonicalID(workspace)
	entry, err := s.getWithoutTracking(ctx, name, workspace)
	if err != nil {
		return NamedEntry{}, err
	}
	at := timeutil.NowUTC()
	if update.At != nil {
		at = update.At.UTC()
	}
	column, timestampColumn, err := telemetryColumnsForAction(update.Action)
	if err != nil {
		return NamedEntry{}, err
	}
	updatedAt := timeutil.NowUTC()
	query := fmt.Sprintf(`
		UPDATE named_memory
		SET %s = %s + 1,
		    %s = ?,
		    updated_at = ?
		WHERE id = ?`, column, column, timestampColumn)
	if _, err := s.db.ExecContext(ctx, query, sqlutil.FormatTimestamp(at), sqlutil.FormatTimestamp(updatedAt), entry.ID); err != nil {
		return NamedEntry{}, fmt.Errorf("memory: update telemetry: %w", err)
	}
	return s.getWithoutTracking(ctx, name, workspace)
}

// RecordAccessBatch records surfaced retrieval access without touching outcome telemetry.
func (s *TursoStore) RecordAccessBatch(ctx context.Context, workspace string, names []string, at time.Time) (int, error) {
	workspace = ws.CanonicalID(workspace)
	names = normalizedAccessBatchNames(names)
	if len(names) == 0 {
		return 0, nil
	}
	if at.IsZero() {
		at = timeutil.NowUTC()
	} else {
		at = at.UTC()
	}

	placeholders := make([]string, len(names))
	args := make([]any, 0, len(names)+2)
	args = append(args, sqlutil.FormatTimestamp(at), workspace)
	for i, name := range names {
		placeholders[i] = "?"
		args = append(args, name)
	}

	result, err := s.db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE named_memory
		SET last_accessed = ?,
		    access_count = access_count + 1
		WHERE workspace = ? AND name IN (%s)`, strings.Join(placeholders, ",")), args...)
	if err != nil {
		return 0, fmt.Errorf("memory: record access batch: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return int(affected), nil
}

// Relevant returns the most relevant entries for the workspace based on recency and access patterns.
func (s *TursoStore) Relevant(ctx context.Context, workspace string, limit int) ([]ScoredEntry, error) {
	if limit <= 0 {
		limit = 10
	}
	workspace = ws.CanonicalID(workspace)
	const maxWindow = 500
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT %s
		FROM named_memory
		WHERE workspace = ?
		ORDER BY last_accessed DESC, updated_at DESC
		LIMIT ?`, namedEntrySelectColumns), workspace, maxWindow)
	if err != nil {
		return nil, fmt.Errorf("memory: relevant: %w", err)
	}
	defer func() { errs.Ignore(rows.Close(), "close relevant rows") }()

	var entries []NamedEntry
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("memory: relevant rows: %w", err)
	}

	// Score and sort entries
	scored := make([]ScoredEntry, 0, len(entries))
	for _, e := range entries {
		scored = append(scored, ScoredEntry{Entry: e, Score: scoreEntry(e)})
	}
	sort.Slice(scored, func(i, j int) bool { return scored[i].Score > scored[j].Score })

	if len(scored) > limit {
		scored = scored[:limit]
	}
	return scored, nil
}

// SaveFromResult saves a memory entry from skill result data.
func (s *TursoStore) SaveFromResult(ctx context.Context, name, typ, workspace, summary string, result []byte) (NamedEntry, error) {
	entry := NamedEntry{
		Name:      name,
		Type:      typ,
		Workspace: workspace,
		Summary:   summary,
		Result:    result,
	}
	return s.Save(ctx, entry)
}

// SaveResult stores a result envelope using structured options.
func (s *TursoStore) SaveResult(ctx context.Context, opts SaveOptions) (NamedEntry, error) {
	entry := NamedEntry{
		Name:      opts.Name,
		Type:      opts.Type,
		Workspace: opts.Workspace,
		Summary:   opts.Summary,
		Result:    opts.Result,
		SessionID: opts.SessionID,
	}
	return s.Save(ctx, entry)
}

// ListFiltered returns named memories for a workspace with optional filters.
func (s *TursoStore) ListFiltered(ctx context.Context, workspace string, filter ListFilter, limit, offset int) ([]NamedEntry, int, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	workspace = ws.CanonicalID(workspace)

	where := []string{"workspace = ?"}
	args := []any{workspace}

	if strings.TrimSpace(filter.SessionID) != "" {
		where = append(where, "session_id = ?")
		args = append(args, strings.TrimSpace(filter.SessionID))
	}

	if len(filter.Types) > 0 {
		placeholders := make([]string, 0, len(filter.Types))
		for _, t := range filter.Types {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			placeholders = append(placeholders, "?")
			args = append(args, t)
		}
		if len(placeholders) > 0 {
			where = append(where, fmt.Sprintf("type IN (%s)", strings.Join(placeholders, ",")))
		}
	}

	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := s.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM named_memory WHERE %s", whereSQL), args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("memory: list filtered count: %w", err)
	}

	q := fmt.Sprintf(`
		SELECT %s
		FROM named_memory
		WHERE %s
		ORDER BY updated_at DESC
		LIMIT ? OFFSET ?`, namedEntrySelectColumns, whereSQL)
	qArgs := append(append([]any{}, args...), limit, offset)

	rows, err := s.db.QueryContext(ctx, q, qArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("memory: list filtered: %w", err)
	}
	defer func() { errs.Ignore(rows.Close(), "close rows") }()

	var entries []NamedEntry
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("memory: scan filtered: %w", err)
		}
		entries = append(entries, entry)
	}

	return entries, total, nil
}

// ListWithoutEmbedding returns memories that don't have embeddings yet.
func (s *TursoStore) ListWithoutEmbedding(ctx context.Context, workspace string, limit int) ([]NamedEntry, error) {
	return s.ListWithoutEmbeddingPage(ctx, workspace, limit, 0)
}

// ListWithoutEmbeddingPage returns memories that don't have embeddings yet using limit/offset pagination.
func (s *TursoStore) ListWithoutEmbeddingPage(ctx context.Context, workspace string, limit, offset int) ([]NamedEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	workspace = ws.CanonicalID(workspace)

	// Check for NULL or zero-length BLOB embedding.
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT %s
		FROM named_memory
		WHERE workspace = ? AND (embedding IS NULL OR LENGTH(embedding) = 0)
		ORDER BY updated_at DESC
		LIMIT ? OFFSET ?`, namedEntrySelectColumns), workspace, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("memory: list without embedding: %w", err)
	}
	defer func() { errs.Ignore(rows.Close(), "close rows") }()

	var entries []NamedEntry
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("memory: scan without embedding: %w", err)
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// ValidateEmbeddingDimensions checks if new embedding dimensions are compatible with existing ones.
func (s *TursoStore) ValidateEmbeddingDimensions(ctx context.Context, workspace string, dimensions int) error {
	// TursoStore validates dimensions at open time via vectorDimension field
	if s.vectorDimension > 0 && dimensions != s.vectorDimension {
		return fmt.Errorf("memory: dimension mismatch: store configured for %d, got %d", s.vectorDimension, dimensions)
	}
	return nil
}

// SetEmbeddingMetadata stores embedding configuration for a workspace.
func (s *TursoStore) SetEmbeddingMetadata(ctx context.Context, meta EmbeddingMetadata) error {
	meta.Workspace = ws.CanonicalID(meta.Workspace)
	now := timeutil.NowUTC()
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = now
	}
	meta.UpdatedAt = now

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO embedding_metadata (workspace, provider, model, dimensions, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(workspace) DO UPDATE SET
			provider = excluded.provider,
			model = excluded.model,
			dimensions = excluded.dimensions,
			updated_at = excluded.updated_at
	`, meta.Workspace, meta.Provider, meta.Model, meta.Dimensions,
		sqlutil.FormatTimestamp(meta.CreatedAt), sqlutil.FormatTimestamp(meta.UpdatedAt))
	if err != nil {
		return fmt.Errorf("memory: set embedding metadata: %w", err)
	}
	return nil
}

// UpdateAtomic stores atomic processing results for a named memory entry.
// atomicText is the self-contained rewrite, entities are extracted identifiers,
// keywords are BM25-optimized search terms.
func (s *TursoStore) UpdateAtomic(ctx context.Context, name, workspace, atomicText string, entities, keywords []string) error {
	workspace = ws.CanonicalID(workspace)
	entitiesJSON, err := sqlutil.FormatJSON(entities)
	if err != nil {
		return fmt.Errorf("memory: marshal entities: %w", err)
	}
	keywordsJSON, err := sqlutil.FormatJSON(keywords)
	if err != nil {
		return fmt.Errorf("memory: marshal keywords: %w", err)
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE named_memory
		SET atomic_text = ?, entities = ?, keywords = ?, updated_at = ?
		WHERE name = ? AND workspace = ?
	`, atomicText, entitiesJSON, keywordsJSON, sqlutil.FormatTimestamp(timeutil.NowUTC()), name, workspace)
	if err != nil {
		return fmt.Errorf("memory: update atomic: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("memory: entry not found: %s in workspace %s", name, workspace)
	}
	return nil
}

// scoreEntry is defined in store.go - using same function for both SQLite and Turso
