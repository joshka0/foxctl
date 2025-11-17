package dbdriver

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// VectorHelper provides vector search utilities for databases that support it
type VectorHelper struct {
	db         DB
	dimensions int
}

// NewVectorHelper creates a new vector helper
func NewVectorHelper(db DB) (*VectorHelper, error) {
	if !db.IsVectorSearchEnabled() {
		return nil, fmt.Errorf("vector search is not enabled for this database")
	}

	// Get vector dimensions from Turso DB
	dimensions := 384 // default
	if turso, ok := db.(*tursoDB); ok {
		dimensions = turso.GetVectorDimensions()
	}

	return &VectorHelper{
		db:         db,
		dimensions: dimensions,
	}, nil
}

// Vector represents a vector embedding
type Vector []float32

// String returns the string representation of a vector for SQL queries
func (v Vector) String() string {
	parts := make([]string, len(v))
	for i, val := range v {
		parts[i] = fmt.Sprintf("%f", val)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// MarshalJSON marshals the vector to JSON
func (v Vector) MarshalJSON() ([]byte, error) {
	return json.Marshal([]float32(v))
}

// UnmarshalJSON unmarshals JSON to a vector
func (v *Vector) UnmarshalJSON(data []byte) error {
	var floats []float32
	if err := json.Unmarshal(data, &floats); err != nil {
		return err
	}
	*v = Vector(floats)
	return nil
}

// ParseVector parses a vector from a string or JSON
func ParseVector(data string) (Vector, error) {
	var v Vector
	if err := json.Unmarshal([]byte(data), &v); err != nil {
		return nil, fmt.Errorf("failed to parse vector: %w", err)
	}
	return v, nil
}

// CreateVectorColumn creates a vector column in the specified table
func (vh *VectorHelper) CreateVectorColumn(ctx context.Context, tableName, columnName string) error {
	query := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s F32_BLOB(%d)", tableName, columnName, vh.dimensions)
	_, err := vh.db.ExecContext(ctx, query)
	return err
}

// CreateVectorIndex creates a vector search index
func (vh *VectorHelper) CreateVectorIndex(ctx context.Context, tableName, columnName, indexName string) error {
	query := fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s (libsql_vector_idx(%s))",
		indexName, tableName, columnName)
	_, err := vh.db.ExecContext(ctx, query)
	return err
}

// InsertVector inserts a vector into the database using the vector() SQL function
// Returns a SQL expression that can be used in INSERT/UPDATE statements
func (vh *VectorHelper) VectorExpression(vector Vector) string {
	return fmt.Sprintf("vector('%s')", vector.String())
}

// CosineSimilarity calculates cosine similarity between two vectors
// Returns a SQL expression that can be used in SELECT/WHERE/ORDER BY clauses
func (vh *VectorHelper) CosineSimilarity(columnName string, queryVector Vector) string {
	return fmt.Sprintf("vector_distance_cos(%s, '%s')", columnName, queryVector.String())
}

// EuclideanDistance calculates Euclidean distance between two vectors
// Returns a SQL expression that can be used in SELECT/WHERE/ORDER BY clauses
func (vh *VectorHelper) EuclideanDistance(columnName string, queryVector Vector) string {
	return fmt.Sprintf("vector_distance_l2(%s, '%s')", columnName, queryVector.String())
}

// VectorTopK performs a vector similarity search using the index
// Returns the SQL for querying top K similar vectors
func (vh *VectorHelper) VectorTopK(indexName string, queryVector Vector, k int) string {
	return fmt.Sprintf("vector_top_k('%s', '%s', %d)", indexName, queryVector.String(), k)
}

// ExtractVector extracts a vector from the database into a string representation
func (vh *VectorHelper) ExtractVector(columnName string) string {
	return fmt.Sprintf("vector_extract(%s)", columnName)
}

// SearchSimilar searches for similar vectors using the index
// This is a high-level helper that performs the full query
func (vh *VectorHelper) SearchSimilar(
	ctx context.Context,
	tableName string,
	indexName string,
	vectorColumn string,
	queryVector Vector,
	limit int,
	additionalWhere string,
	args ...any,
) (*sql.Rows, error) {
	var query string

	if indexName != "" {
		// Use vector index for fast approximate search
		query = fmt.Sprintf(`
			SELECT t.*
			FROM %s(%s, %d) vt
			JOIN %s t ON t.rowid = vt.id
		`, vh.VectorTopK(indexName, queryVector, limit), indexName, limit, tableName)

		if additionalWhere != "" {
			query += " WHERE " + additionalWhere
		}
	} else {
		// Full table scan with exact distance calculation
		query = fmt.Sprintf(`
			SELECT *
			FROM %s
		`, tableName)

		if additionalWhere != "" {
			query += " WHERE " + additionalWhere
		}

		query += fmt.Sprintf(`
			ORDER BY %s
			LIMIT %d
		`, vh.CosineSimilarity(vectorColumn, queryVector), limit)
	}

	return vh.db.QueryContext(ctx, query, args...)
}

// GetDimensions returns the configured vector dimensions
func (vh *VectorHelper) GetDimensions() int {
	return vh.dimensions
}

// ValidateVector validates that a vector has the correct dimensions
func (vh *VectorHelper) ValidateVector(v Vector) error {
	if len(v) != vh.dimensions {
		return fmt.Errorf("vector has %d dimensions, expected %d", len(v), vh.dimensions)
	}
	return nil
}
