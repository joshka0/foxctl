package dbdriver

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
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

	// Get vector dimensions from DB if available
	dimensions := 384 // default
	if vd, ok := db.(interface{ GetVectorDimensions() int }); ok {
		dimensions = vd.GetVectorDimensions()
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

// MarshalJSON implements json.Marshaler for Vector
func (v Vector) MarshalJSON() ([]byte, error) {
	return json.Marshal([]float32(v))
}

// UnmarshalJSON implements json.Unmarshaler for Vector
func (v *Vector) UnmarshalJSON(data []byte) error {
	var floats []float32
	if err := json.Unmarshal(data, &floats); err != nil {
		return err
	}
	*v = floats
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
	var query string
	if vh.db.GetDriverType() == DriverPostgres {
		query = pgCreateVectorColumnSQL(tableName, columnName, vh.dimensions)
	} else {
		query = fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s F32_BLOB(%d)", tableName, columnName, vh.dimensions)
	}
	_, err := vh.db.ExecContext(ctx, query)
	if err != nil {
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "duplicate column") || strings.Contains(errMsg, "already exists") {
			return nil
		}
		return fmt.Errorf("create vector column: %w", err)
	}
	return nil
}

// CreateVectorIndex creates a vector search index
func (vh *VectorHelper) CreateVectorIndex(ctx context.Context, tableName, columnName, indexName string) error {
	var query string
	if vh.db.GetDriverType() == DriverPostgres {
		query = pgCreateVectorIndexSQL(tableName, columnName, indexName)
	} else {
		query = fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s (libsql_vector_idx(%s))",
			indexName, tableName, columnName)
	}
	_, err := vh.db.ExecContext(ctx, query)
	return err
}

// VectorExpression inserts a vector into the database using the vector() SQL function
// Returns a SQL expression that can be used in INSERT/UPDATE statements
func (vh *VectorHelper) VectorExpression(vector Vector) string {
	if vh.db.GetDriverType() == DriverPostgres {
		return pgVectorExpression(vector)
	}
	return fmt.Sprintf("vector('%s')", vector.String())
}

// CosineSimilarity returns a SQL expression for cosine similarity/distance between two vectors.
// PostgreSQL: returns similarity (1 - distance); higher = more similar. Use ORDER BY ... DESC.
// libSQL: returns distance via vector_distance_cos; lower = more similar. Use ORDER BY ... ASC.
// Prefer SearchSimilar() for cross-driver queries that handle ordering automatically.
func (vh *VectorHelper) CosineSimilarity(columnName string, queryVector Vector) string {
	if vh.db.GetDriverType() == DriverPostgres {
		return pgCosineSimilarity(columnName, queryVector)
	}
	return fmt.Sprintf("vector_distance_cos(%s, '%s')", columnName, queryVector.String())
}

// EuclideanDistance calculates Euclidean distance between two vectors
// Returns a SQL expression that can be used in SELECT/WHERE/ORDER BY clauses
func (vh *VectorHelper) EuclideanDistance(columnName string, queryVector Vector) string {
	if vh.db.GetDriverType() == DriverPostgres {
		return pgEuclideanDistance(columnName, queryVector)
	}
	return fmt.Sprintf("vector_distance_l2(%s, '%s')", columnName, queryVector.String())
}

// VectorTopK performs a vector similarity search using the index.
// For libSQL: returns a virtual table expression using vector_top_k().
// For PostgreSQL: returns empty string (Postgres uses ORDER BY <=> in the main query).
// Callers should use SearchSimilar() for cross-driver compatibility.
func (vh *VectorHelper) VectorTopK(indexName string, queryVector Vector, k int) string {
	if vh.db.GetDriverType() == DriverPostgres {
		// PostgreSQL does not have a vector_top_k equivalent.
		// Return empty string; callers should use SearchSimilar() instead.
		return ""
	}
	return fmt.Sprintf("vector_top_k('%s', '%s', %d)", indexName, queryVector.String(), k)
}

// ExtractVector extracts a vector from the database into a string representation.
// PostgreSQL returns vector columns directly; libSQL uses vector_extract().
func (vh *VectorHelper) ExtractVector(columnName string) string {
	if vh.db.GetDriverType() == DriverPostgres {
		return columnName
	}
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

	if vh.db.GetDriverType() == DriverPostgres {
		// PostgreSQL: use ORDER BY with <=> operator (HNSW index is used automatically)
		qVC := pgQuoteIdent(vectorColumn)
		query = fmt.Sprintf(`
			SELECT *
			FROM %s
		`, pgQuoteIdent(tableName))
		if additionalWhere != "" {
			query += " WHERE " + additionalWhere
		}
		query += fmt.Sprintf(`
			ORDER BY %s <=> '%s'
			LIMIT %d
		`, qVC, queryVector.String(), limit)
	} else if indexName != "" {
		// libSQL: Use vector index for fast approximate search
		query = fmt.Sprintf(`
			SELECT t.*
			FROM %s vt
			JOIN %s t ON t.rowid = vt.id
		`, vh.VectorTopK(indexName, queryVector, limit), tableName)

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
	for i, val := range v {
		if math.IsNaN(float64(val)) || math.IsInf(float64(val), 0) {
			return fmt.Errorf("vector contains NaN or Infinity at index %d", i)
		}
	}
	return nil
}
