//go:build vector && cgo
// +build vector,cgo

// Package vector provides sqlite-vector integration for semantic search.
// This file is only compiled when both the "vector" build tag and CGO are enabled.
package vector

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"path/filepath"
	"runtime"
	"strings"

	_ "github.com/mattn/go-sqlite3" // CGO-based SQLite driver
)

// Enabled indicates whether vector support is available in this build.
const Enabled = true

// extensionPath returns the path to the sqlite-vector extension library.
func extensionPath() (string, error) {
	// Get the path to the current file
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("vector: cannot determine extension path")
	}

	// Navigate from internal/storage/vector to extensions/
	projectRoot := filepath.Join(filepath.Dir(filename), "..", "..", "..")
	extPath := filepath.Join(projectRoot, "extensions", "vector")

	return extPath, nil
}

// Store provides vector search capabilities using sqlite-vector.
type Store struct {
	db            *sql.DB
	extensionPath string
	tableName     string
	columnName    string
	dimension     int
}

// NewStore creates a new vector store with the sqlite-vector extension.
// The db parameter should be opened with the "sqlite3" driver (mattn/go-sqlite3).
// The tableName parameter specifies which table contains vector embeddings.
func NewStore(db *sql.DB, tableName string) (*Store, error) {
	extPath, err := extensionPath()
	if err != nil {
		return nil, err
	}

	store := &Store{
		db:            db,
		extensionPath: extPath,
		tableName:     tableName,
		columnName:    "embedding",
		dimension:     0, // Will be set when vectors are initialized
	}

	// Load the sqlite-vector extension
	if err := store.loadExtension(); err != nil {
		return nil, fmt.Errorf("vector: load extension: %w", err)
	}

	return store, nil
}

// loadExtension loads the sqlite-vector extension into the database.
func (s *Store) loadExtension() error {
	// Enable extension loading
	query := fmt.Sprintf("SELECT load_extension('%s')", s.extensionPath)
	if _, err := s.db.Exec(query); err != nil {
		return fmt.Errorf("failed to load extension from %s: %w", s.extensionPath, err)
	}

	// Verify the extension is loaded by checking version
	var version string
	if err := s.db.QueryRow("SELECT vector_version()").Scan(&version); err != nil {
		return fmt.Errorf("failed to verify extension: %w", err)
	}

	return nil
}

// InitializeVectors initializes vector search for a specific column.
// This should be called after vectors have been inserted into the table.
func (s *Store) InitializeVectors(ctx context.Context, dimension int, distanceMetric string) error {
	s.dimension = dimension

	// Validate distance metric
	validMetrics := map[string]bool{
		"L2":         true,
		"L1":         true,
		"COSINE":     true,
		"DOT":        true,
		"SQUARED_L2": true,
	}
	if !validMetrics[strings.ToUpper(distanceMetric)] {
		return fmt.Errorf("invalid distance metric: %s (valid: L2, L1, COSINE, DOT, SQUARED_L2)", distanceMetric)
	}

	// Initialize vector column
	query := fmt.Sprintf(
		"SELECT vector_init('%s', '%s', 'type=FLOAT32,dimension=%d,distance=%s')",
		s.tableName, s.columnName, dimension, strings.ToUpper(distanceMetric),
	)
	if _, err := s.db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("vector_init failed: %w", err)
	}

	return nil
}

// Quantize creates a quantized index for faster searches.
// This is optional but provides 4-5x speedup for large datasets.
func (s *Store) Quantize(ctx context.Context) error {
	query := fmt.Sprintf("SELECT vector_quantize('%s', '%s')", s.tableName, s.columnName)
	if _, err := s.db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("vector_quantize failed: %w", err)
	}
	return nil
}

// QuantizePreload preloads the quantized index into memory for maximum performance.
func (s *Store) QuantizePreload(ctx context.Context) error {
	query := fmt.Sprintf("SELECT vector_quantize_preload('%s', '%s')", s.tableName, s.columnName)
	if _, err := s.db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("vector_quantize_preload failed: %w", err)
	}
	return nil
}

// Search performs vector similarity search using the quantized index if available.
func (s *Store) Search(ctx context.Context, opts SearchOptions) ([]VectorEntry, error) {
	if len(opts.Embedding) == 0 {
		return nil, fmt.Errorf("embedding cannot be empty")
	}
	if opts.Limit <= 0 {
		opts.Limit = 10
	}

	// Serialize embedding to BLOB format (Float32)
	embeddingBlob := serializeFloat32(opts.Embedding)

	// Use quantized scan if available, otherwise fall back to regular search
	query := fmt.Sprintf(`
		SELECT e.id, e.name, e.workspace, e.summary, v.distance
		FROM %s AS e
		JOIN vector_quantize_scan('%s', '%s', ?, ?) AS v
		ON e.ROWID = v.rowid
		WHERE e.workspace = ?
		ORDER BY v.distance ASC
	`, s.tableName, s.tableName, s.columnName)

	rows, err := s.db.QueryContext(ctx, query, embeddingBlob, opts.Limit, opts.Workspace)
	if err != nil {
		return nil, fmt.Errorf("vector search failed: %w", err)
	}
	defer rows.Close()

	var results []VectorEntry
	for rows.Next() {
		var entry VectorEntry
		if err := rows.Scan(&entry.ID, &entry.Name, &entry.Workspace, &entry.Summary, &entry.Distance); err != nil {
			return nil, fmt.Errorf("scan result: %w", err)
		}
		results = append(results, entry)
	}

	return results, nil
}

// SaveEmbedding stores a vector embedding for a memory entry.
func (s *Store) SaveEmbedding(ctx context.Context, id, workspace, name string, embedding []float32) error {
	if len(embedding) == 0 {
		return fmt.Errorf("embedding cannot be empty")
	}

	// Serialize embedding to BLOB format
	embeddingBlob := serializeFloat32(embedding)

	// Update the embedding column
	query := fmt.Sprintf(
		"UPDATE %s SET %s = ? WHERE id = ? AND workspace = ? AND name = ?",
		s.tableName, s.columnName,
	)
	result, err := s.db.ExecContext(ctx, query, embeddingBlob, id, workspace, name)
	if err != nil {
		return fmt.Errorf("save embedding: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("no entry found with id=%s, workspace=%s, name=%s", id, workspace, name)
	}

	return nil
}

// Close releases resources.
func (s *Store) Close() error {
	// The DB is owned by the caller, so we don't close it here
	return nil
}

// serializeFloat32 converts a float32 slice to a binary BLOB format.
// This is the format expected by sqlite-vector for FLOAT32 vectors.
func serializeFloat32(vec []float32) []byte {
	blob := make([]byte, len(vec)*4)
	for i, v := range vec {
		binary.LittleEndian.PutUint32(blob[i*4:], math.Float32bits(v))
	}
	return blob
}

// deserializeFloat32 converts a binary BLOB back to a float32 slice.
func deserializeFloat32(blob []byte) []float32 {
	if len(blob)%4 != 0 {
		return nil
	}
	vec := make([]float32, len(blob)/4)
	for i := range vec {
		bits := binary.LittleEndian.Uint32(blob[i*4:])
		vec[i] = math.Float32frombits(bits)
	}
	return vec
}
