package memory

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/dbdriver"
)

// VectorStore extends Store with vector search capabilities
type VectorStore struct {
	*Store
	vectorHelper *dbdriver.VectorHelper
	db           dbdriver.DB
}

// VectorEntry extends NamedEntry with vector embedding support
type VectorEntry struct {
	storage.NamedEntry
	Embedding dbdriver.Vector `json:"embedding,omitempty"`
}

// EnableVectorSearch enables vector search on an existing memory store
// This requires the database to support vector operations (Turso with vector search enabled)
func (s *Store) EnableVectorSearch(db dbdriver.DB) (*VectorStore, error) {
	if !db.IsVectorSearchEnabled() {
		return nil, fmt.Errorf("vector search is not enabled on this database")
	}

	vectorHelper, err := dbdriver.NewVectorHelper(db)
	if err != nil {
		return nil, fmt.Errorf("failed to create vector helper: %w", err)
	}

	return &VectorStore{
		Store:        s,
		vectorHelper: vectorHelper,
		db:           db,
	}, nil
}

// migrateVectorSchema adds vector search columns to the memory store
// This should be called during migration if vector search is enabled
func migrateVectorSchema(ctx context.Context, db *sql.DB, dimensions int) error {
	// Add vector embedding column
	alterQuery := fmt.Sprintf(`
		ALTER TABLE named_memory
		ADD COLUMN embedding F32_BLOB(%d)
	`, dimensions)

	if _, err := db.ExecContext(ctx, alterQuery); err != nil {
		// Check if column already exists (ignore error if it does)
		if !isColumnExistsError(err) {
			return fmt.Errorf("failed to add embedding column: %w", err)
		}
	}

	// Create vector index for fast similarity search
	indexQuery := `
		CREATE INDEX IF NOT EXISTS idx_memory_vector
		ON named_memory (libsql_vector_idx(embedding))
	`

	if _, err := db.ExecContext(ctx, indexQuery); err != nil {
		return fmt.Errorf("failed to create vector index: %w", err)
	}

	return nil
}

// isColumnExistsError checks if the error is due to column already existing
func isColumnExistsError(err error) bool {
	if err == nil {
		return false
	}
	// SQLite error for duplicate column
	return err.Error() == "duplicate column name: embedding" ||
		// Check for other variations of the error message
		err.Error() == "table named_memory has no column named embedding"
}

// SaveWithEmbedding saves a memory entry with vector embedding
func (vs *VectorStore) SaveWithEmbedding(ctx context.Context, entry VectorEntry) (VectorEntry, error) {
	// Validate vector dimensions
	if err := vs.vectorHelper.ValidateVector(entry.Embedding); err != nil {
		return VectorEntry{}, fmt.Errorf("invalid embedding: %w", err)
	}

	// Save the base entry first
	savedEntry, err := vs.Save(ctx, entry.NamedEntry)
	if err != nil {
		return VectorEntry{}, err
	}

	// Update with embedding
	updateQuery := fmt.Sprintf(`
		UPDATE named_memory
		SET embedding = %s
		WHERE id = ?
	`, vs.vectorHelper.VectorExpression(entry.Embedding))

	if _, err := vs.db.ExecContext(ctx, updateQuery, savedEntry.ID); err != nil {
		return VectorEntry{}, fmt.Errorf("failed to save embedding: %w", err)
	}

	return VectorEntry{
		NamedEntry: savedEntry,
		Embedding:  entry.Embedding,
	}, nil
}

// SearchSimilar finds memories similar to the given embedding
func (vs *VectorStore) SearchSimilar(
	ctx context.Context,
	queryEmbedding dbdriver.Vector,
	workspace string,
	limit int,
) ([]VectorEntry, error) {
	// Validate query vector
	if err := vs.vectorHelper.ValidateVector(queryEmbedding); err != nil {
		return nil, fmt.Errorf("invalid query embedding: %w", err)
	}

	// Use vector index for search
	query := fmt.Sprintf(`
		SELECT
			t.id, t.name, t.type, t.workspace, t.summary,
			t.result, t.digests, t.created_at, t.updated_at,
			t.last_accessed, t.access_count,
			%s as similarity
		FROM vector_top_k('idx_memory_vector', '%s', ?) vt
		JOIN named_memory t ON t.rowid = vt.id
		WHERE t.workspace = ?
		ORDER BY similarity ASC
	`, vs.vectorHelper.CosineSimilarity("t.embedding", queryEmbedding), queryEmbedding.String())

	rows, err := vs.db.QueryContext(ctx, query, limit, workspace)
	if err != nil {
		return nil, fmt.Errorf("vector search failed: %w", err)
	}
	defer rows.Close()

	var results []VectorEntry
	for rows.Next() {
		var entry VectorEntry
		var similarity float64

		err := rows.Scan(
			&entry.ID,
			&entry.Name,
			&entry.Type,
			&entry.Workspace,
			&entry.Summary,
			&entry.Result,
			&entry.Digests,
			&entry.CreatedAt,
			&entry.UpdatedAt,
			&entry.LastAccess,
			&entry.AccessCount,
			&similarity,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan result: %w", err)
		}

		results = append(results, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating results: %w", err)
	}

	return results, nil
}

// GetWithEmbedding retrieves a memory entry with its embedding
func (vs *VectorStore) GetWithEmbedding(ctx context.Context, name, workspace string) (VectorEntry, error) {
	query := fmt.Sprintf(`
		SELECT
			id, name, type, workspace, summary, result, digests,
			created_at, updated_at, last_accessed, access_count,
			%s as embedding_str
		FROM named_memory
		WHERE name = ? AND workspace = ?
	`, vs.vectorHelper.ExtractVector("embedding"))

	var entry VectorEntry
	var embeddingStr sql.NullString

	err := vs.db.QueryRowContext(ctx, query, name, workspace).Scan(
		&entry.ID,
		&entry.Name,
		&entry.Type,
		&entry.Workspace,
		&entry.Summary,
		&entry.Result,
		&entry.Digests,
		&entry.CreatedAt,
		&entry.UpdatedAt,
		&entry.LastAccess,
		&entry.AccessCount,
		&embeddingStr,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return VectorEntry{}, storage.ErrNotFound
		}
		return VectorEntry{}, fmt.Errorf("failed to get entry: %w", err)
	}

	// Parse embedding if present
	if embeddingStr.Valid && embeddingStr.String != "" {
		embedding, err := dbdriver.ParseVector(embeddingStr.String)
		if err != nil {
			return VectorEntry{}, fmt.Errorf("failed to parse embedding: %w", err)
		}
		entry.Embedding = embedding
	}

	return entry, nil
}

// GetVectorDimensions returns the configured vector dimensions
func (vs *VectorStore) GetVectorDimensions() int {
	return vs.vectorHelper.GetDimensions()
}
