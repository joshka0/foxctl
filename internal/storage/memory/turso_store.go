//go:build cgo && !race

// Package memory implements named memory storage for skill execution results and context data.
package memory

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sort"
	"strings"

	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/platform/timeutil"
	"github.com/jkatigb/agentctl/internal/storage/dbdriver"
	"github.com/jkatigb/agentctl/internal/storage/sqlutil"
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
	hasIndex        bool // true if vector index exists
	vectorDimension int  // configured embedding dimensions
}

// DefaultEmbeddingModel is the default model for embeddings.
const DefaultEmbeddingModel = "gemini-embedding-001"

// OpenTurso initializes a memory store using Turso database.
func OpenTurso(ctx context.Context, cfg dbdriver.TursoConfig) (*TursoStore, error) {
	// Ensure vector search is enabled
	cfg.EnableVectorSearch = true
	if cfg.VectorDimensions == 0 {
		cfg.VectorDimensions = 3072 // Gemini embedding-001 dimensions
	}

	// Create migration function that uses configured dimensions and default model
	migrate := func(ctx context.Context, db *sql.DB) error {
		return migrateTursoWithDimensions(ctx, db, cfg.VectorDimensions, DefaultEmbeddingModel)
	}

	db, err := dbdriver.OpenDB(ctx, dbdriver.Config{
		Driver: dbdriver.DriverTurso,
		Turso:  cfg,
	}, migrate)
	if err != nil {
		return nil, fmt.Errorf("memory: open turso: %w", err)
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

	store := &TursoStore{db: db, vh: vh, vectorDimension: cfg.VectorDimensions}

	// Validate dimension metadata matches configuration
	if err := store.validateDimensions(ctx, cfg.VectorDimensions); err != nil {
		_ = db.Close()
		return nil, err
	}

	// Check if vector index exists, create if missing
	store.hasIndex = store.checkVectorIndex(ctx)
	if !store.hasIndex {
		// Auto-create vector index for faster similarity search
		if err := store.CreateVectorIndex(ctx); err != nil {
			// Non-fatal: log warning but continue with full-table scan fallback
			// Vector index creation may fail on older libsql versions
			log.Printf("[WARN] memory: vector index creation failed (will use ORDER BY fallback): %v", err)
		}
	}

	return store, nil
}

// validateDimensions checks that stored metadata dimensions match config.
func (s *TursoStore) validateDimensions(ctx context.Context, expectedDims int) error {
	var storedDims int
	err := s.db.QueryRowContext(ctx, `
		SELECT dimensions FROM embedding_metadata WHERE table_name = 'named_memory' LIMIT 1
	`).Scan(&storedDims)

	if err == sql.ErrNoRows {
		// No metadata yet, this is fine for first run
		return nil
	}
	if err != nil {
		// Table might not exist yet (pre-migration), skip validation
		return nil
	}

	if storedDims != expectedDims {
		return fmt.Errorf("memory: dimension mismatch: stored=%d, config=%d (recreate database or update config)", storedDims, expectedDims)
	}
	return nil
}

// migrateTursoWithDimensions runs migrations with configurable vector dimensions and model.
func migrateTursoWithDimensions(ctx context.Context, db *sql.DB, dimensions int, model string) error {
	// Create embedding_metadata table for dimension tracking
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS embedding_metadata (
			table_name TEXT PRIMARY KEY,
			column_name TEXT NOT NULL,
			dimensions INTEGER NOT NULL,
			model TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)
	`)
	if err != nil {
		return fmt.Errorf("create embedding_metadata table: %w", err)
	}

	// Create named_memory table with F32_BLOB for native vector search
	memoryQuery := fmt.Sprintf(`
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
			embedding F32_BLOB(%d),
			embedding_model TEXT,
			UNIQUE(name, workspace)
		)
	`, dimensions)
	if _, err = db.ExecContext(ctx, memoryQuery); err != nil {
		return fmt.Errorf("create named_memory table: %w", err)
	}

	// Record metadata for named_memory embedding column (including model for provenance)
	now := timeutil.NowUTC().Format("2006-01-02T15:04:05Z")
	_, err = db.ExecContext(ctx, `
		INSERT INTO embedding_metadata (table_name, column_name, dimensions, model, created_at, updated_at)
		VALUES ('named_memory', 'embedding', ?, ?, ?, ?)
		ON CONFLICT(table_name) DO UPDATE SET
			dimensions = excluded.dimensions,
			model = excluded.model,
			updated_at = excluded.updated_at
	`, dimensions, model, now, now)
	if err != nil {
		return fmt.Errorf("insert memory metadata: %w", err)
	}

	// Create indexes
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_named_memory_ws_updated ON named_memory(workspace, updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_named_memory_name_ws ON named_memory(name, workspace)`,
	}
	for _, idx := range indexes {
		if _, err := db.ExecContext(ctx, idx); err != nil {
			return fmt.Errorf("create index: %w", err)
		}
	}

	return nil
}

// checkVectorIndex checks if the vector index exists.
func (s *TursoStore) checkVectorIndex(ctx context.Context) bool {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type='index' AND name='idx_memory_embedding_vec'
	`).Scan(&count)
	return err == nil && count > 0
}

// CreateVectorIndex creates a vector index for faster similarity search.
func (s *TursoStore) CreateVectorIndex(ctx context.Context) error {
	err := s.vh.CreateVectorIndex(ctx, "named_memory", "embedding", "idx_memory_embedding_vec")
	if err != nil {
		return fmt.Errorf("memory: create vector index: %w", err)
	}
	s.hasIndex = true
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

// SaveWithEmbedding saves a named memory entry with its embedding vector.
func (s *TursoStore) SaveWithEmbedding(ctx context.Context, entry NamedEntry, embedding []float32, model string) (NamedEntry, error) {
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

	// Validate embedding dimensions
	if len(embedding) != s.vectorDimension {
		return NamedEntry{}, fmt.Errorf("memory: embedding dimension mismatch: got %d, expected %d", len(embedding), s.vectorDimension)
	}

	// Format digests
	digestsJSON, err := sqlutil.FormatJSON(entry.Digests)
	if err != nil {
		return NamedEntry{}, fmt.Errorf("memory: format digests: %w", err)
	}

	// Convert embedding to vector string
	vectorStr := float32sToVectorString(embedding)

	query := fmt.Sprintf(`
		INSERT INTO named_memory (
			id, name, type, workspace, summary, result, digests,
			created_at, updated_at, last_accessed, access_count,
			embedding, embedding_model
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, vector('%s'), ?)
		ON CONFLICT(name, workspace) DO UPDATE SET
			id = excluded.id,
			type = excluded.type,
			summary = excluded.summary,
			result = excluded.result,
			digests = excluded.digests,
			updated_at = excluded.updated_at,
			last_accessed = excluded.last_accessed,
			embedding = excluded.embedding,
			embedding_model = excluded.embedding_model
	`, vectorStr)

	_, err = s.db.ExecContext(ctx, query,
		entry.ID, entry.Name, entry.Type, entry.Workspace, entry.Summary, entry.Result, digestsJSON,
		sqlutil.FormatTimestamp(entry.CreatedAt), sqlutil.FormatTimestamp(entry.UpdatedAt),
		sqlutil.FormatTimestamp(entry.LastAccess), model)
	if err != nil {
		return NamedEntry{}, fmt.Errorf("memory: save with embedding: %w", err)
	}

	return entry, nil
}

// float32sToVectorString converts a float32 slice to Turso vector string format.
func float32sToVectorString(values []float32) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = fmt.Sprintf("%f", v)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// SearchSimilar finds entries similar to the given embedding using native vector search.
func (s *TursoStore) SearchSimilar(ctx context.Context, workspace string, embedding []float32, limit int) ([]ScoredEntry, error) {
	if limit <= 0 {
		limit = 10
	}

	// Convert query embedding to dbdriver.Vector
	vec := make(dbdriver.Vector, len(embedding))
	copy(vec, embedding)

	var query string
	var rows *sql.Rows
	var err error

	if s.hasIndex {
		// Use vector_top_k for fast indexed search
		topKExpr := s.vh.VectorTopK("idx_memory_embedding_vec", vec, limit*2)
		distExpr := s.vh.CosineSimilarity("m.embedding", vec)
		query = fmt.Sprintf(`
			SELECT m.id, m.name, m.type, m.workspace, m.summary, m.result, m.digests,
				m.created_at, m.updated_at, m.last_accessed, m.access_count,
				%s as distance
			FROM %s vt
			JOIN named_memory m ON m.rowid = vt.id
			WHERE m.workspace = ?`, distExpr, topKExpr)
		rows, err = s.db.QueryContext(ctx, query, workspace)
	} else {
		// Fallback to full table scan with cosine distance
		distExpr := s.vh.CosineSimilarity("embedding", vec)
		query = fmt.Sprintf(`
			SELECT id, name, type, workspace, summary, result, digests,
				created_at, updated_at, last_accessed, access_count,
				%s as distance
			FROM named_memory
			WHERE embedding IS NOT NULL AND workspace = ?
			ORDER BY distance ASC
			LIMIT ?`, distExpr)
		rows, err = s.db.QueryContext(ctx, query, workspace, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("memory: search similar: %w", err)
	}
	defer func() { errs.Ignore(rows.Close(), "close similar rows") }()

	var results []ScoredEntry
	for rows.Next() {
		var entry NamedEntry
		var digestsJSON string
		var createdAt, updatedAt, lastAccess string
		var distance float64

		err := rows.Scan(
			&entry.ID, &entry.Name, &entry.Type, &entry.Workspace, &entry.Summary,
			&entry.Result, &digestsJSON, &createdAt, &updatedAt, &lastAccess,
			&entry.AccessCount, &distance,
		)
		if err != nil {
			continue
		}

		// Parse digests
		_ = sqlutil.ScanJSON(digestsJSON, &entry.Digests)

		// Parse timestamps
		entry.CreatedAt, _ = sqlutil.ScanTimestamp(createdAt)
		entry.UpdatedAt, _ = sqlutil.ScanTimestamp(updatedAt)
		entry.LastAccess, _ = sqlutil.ScanTimestamp(lastAccess)

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

// --- Basic CRUD operations (delegate to underlying store patterns) ---

// Save saves a named memory entry without embedding.
func (s *TursoStore) Save(ctx context.Context, entry NamedEntry) (NamedEntry, error) {
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

	digestsJSON, err := sqlutil.FormatJSON(entry.Digests)
	if err != nil {
		return NamedEntry{}, fmt.Errorf("memory: format digests: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO named_memory (
			id, name, type, workspace, summary, result, digests,
			created_at, updated_at, last_accessed, access_count
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
		ON CONFLICT(name, workspace) DO UPDATE SET
			id = excluded.id,
			type = excluded.type,
			summary = excluded.summary,
			result = excluded.result,
			digests = excluded.digests,
			updated_at = excluded.updated_at,
			last_accessed = excluded.last_accessed
	`, entry.ID, entry.Name, entry.Type, entry.Workspace, entry.Summary, entry.Result, digestsJSON,
		sqlutil.FormatTimestamp(entry.CreatedAt), sqlutil.FormatTimestamp(entry.UpdatedAt),
		sqlutil.FormatTimestamp(entry.LastAccess))
	if err != nil {
		return NamedEntry{}, fmt.Errorf("memory: save: %w", err)
	}

	return entry, nil
}

// Get retrieves a named memory by name+workspace.
func (s *TursoStore) Get(ctx context.Context, name, workspace string) (NamedEntry, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, type, workspace, summary, result, digests,
			created_at, updated_at, last_accessed, access_count
		FROM named_memory
		WHERE name = ? AND workspace = ?`, name, workspace)

	var entry NamedEntry
	var digestsJSON string
	var createdAt, updatedAt, lastAccess string

	err := row.Scan(
		&entry.ID, &entry.Name, &entry.Type, &entry.Workspace, &entry.Summary,
		&entry.Result, &digestsJSON, &createdAt, &updatedAt, &lastAccess, &entry.AccessCount,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return NamedEntry{}, ErrNotFound
		}
		return NamedEntry{}, fmt.Errorf("memory: get: %w", err)
	}

	_ = sqlutil.ScanJSON(digestsJSON, &entry.Digests)
	entry.CreatedAt, _ = sqlutil.ScanTimestamp(createdAt)
	entry.UpdatedAt, _ = sqlutil.ScanTimestamp(updatedAt)
	entry.LastAccess, _ = sqlutil.ScanTimestamp(lastAccess)

	// Update access metadata
	_, _ = s.db.ExecContext(ctx, `
		UPDATE named_memory
		SET last_accessed = ?, access_count = access_count + 1
		WHERE id = ?`, sqlutil.FormatTimestamp(timeutil.NowUTC()), entry.ID)

	return entry, nil
}

// List returns named memories for a workspace.
func (s *TursoStore) List(ctx context.Context, workspace string, limit int) ([]NamedEntry, error) {
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, type, workspace, summary, result, digests,
			created_at, updated_at, last_accessed, access_count
		FROM named_memory
		WHERE workspace = ?
		ORDER BY updated_at DESC
		LIMIT ?`, workspace, limit)
	if err != nil {
		return nil, fmt.Errorf("memory: list: %w", err)
	}
	defer func() { errs.Ignore(rows.Close(), "close list rows") }()

	var entries []NamedEntry
	for rows.Next() {
		var entry NamedEntry
		var digestsJSON string
		var createdAt, updatedAt, lastAccess string

		if err := rows.Scan(
			&entry.ID, &entry.Name, &entry.Type, &entry.Workspace, &entry.Summary,
			&entry.Result, &digestsJSON, &createdAt, &updatedAt, &lastAccess, &entry.AccessCount,
		); err != nil {
			continue
		}

		_ = sqlutil.ScanJSON(digestsJSON, &entry.Digests)
		entry.CreatedAt, _ = sqlutil.ScanTimestamp(createdAt)
		entry.UpdatedAt, _ = sqlutil.ScanTimestamp(updatedAt)
		entry.LastAccess, _ = sqlutil.ScanTimestamp(lastAccess)

		entries = append(entries, entry)
	}

	return entries, rows.Err()
}

// Delete removes a named memory.
func (s *TursoStore) Delete(ctx context.Context, name, workspace string) error {
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

	like := "%" + strings.ToLower(query) + "%"
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, type, workspace, summary, result, digests,
			created_at, updated_at, last_accessed, access_count
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
		var digestsJSON string
		var createdAt, updatedAt, lastAccess string

		if err := rows.Scan(
			&entry.ID, &entry.Name, &entry.Type, &entry.Workspace, &entry.Summary,
			&entry.Result, &digestsJSON, &createdAt, &updatedAt, &lastAccess, &entry.AccessCount,
		); err != nil {
			continue
		}

		_ = sqlutil.ScanJSON(digestsJSON, &entry.Digests)
		entry.CreatedAt, _ = sqlutil.ScanTimestamp(createdAt)
		entry.UpdatedAt, _ = sqlutil.ScanTimestamp(updatedAt)
		entry.LastAccess, _ = sqlutil.ScanTimestamp(lastAccess)

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

	// Validate embedding dimensions
	if len(embedding) != s.vectorDimension {
		return fmt.Errorf("memory: embedding dimension mismatch: got %d, expected %d", len(embedding), s.vectorDimension)
	}

	// Convert embedding to vector string for Turso
	vectorStr := float32sToVectorString(embedding)

	query := fmt.Sprintf(`
		UPDATE named_memory
		SET embedding = vector('%s'), updated_at = ?
		WHERE name = ? AND workspace = ?
	`, vectorStr)

	_, err := s.db.ExecContext(ctx, query, sqlutil.FormatTimestamp(timeutil.NowUTC()), name, workspace)
	if err != nil {
		return fmt.Errorf("memory: update embedding: %w", err)
	}
	return nil
}

// scoreEntry is defined in store.go - using same function for both SQLite and Turso
