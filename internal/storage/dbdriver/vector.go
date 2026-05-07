package dbdriver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
)

// ErrVectorIndexUnsupported indicates that the database supports exact vector
// functions but not ANN/vector index DDL for the current driver.
var ErrVectorIndexUnsupported = errors.New("vector index unsupported")

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
		query = fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s BLOB", tableName, columnName)
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
		_, err := vh.db.ExecContext(ctx, query)
		return err
	}
	return fmt.Errorf("%w: %s does not support indexed dense vector search on Turso main", ErrVectorIndexUnsupported, vh.db.GetDriverType())
}

// VectorExpression inserts a vector into the database using the native vector SQL function.
// Returns a SQL expression that can be used in INSERT/UPDATE statements
func (vh *VectorHelper) VectorExpression(vector Vector) string {
	if vh.db.GetDriverType() == DriverPostgres {
		return pgVectorExpression(vector)
	}
	return tursoVectorExpression(vector)
}

// CosineSimilarity returns a SQL expression for cosine-based scoring between two vectors.
// This method has driver-specific semantics:
//   - PostgreSQL: 1 - distance, where higher values are more similar.
//   - Turso: vector_distance_cos distance, where lower values are more similar.
//
// Prefer CosineSimilarityScore() when you need a consistent [0,1] score with
// higher values meaning more similar across drivers.
func (vh *VectorHelper) CosineSimilarity(columnName string, queryVector Vector) string {
	return cosineSimilarityExpr(vh.db.GetDriverType(), columnName, queryVector)
}

// CosineSimilarityScore returns a SQL expression for cosine similarity normalized to [0,1]
// across supported drivers, where higher values are more similar.
func (vh *VectorHelper) CosineSimilarityScore(columnName string, queryVector Vector) string {
	return cosineSimilarityScoreExpr(vh.db.GetDriverType(), columnName, queryVector)
}

func cosineSimilarityExpr(driver DriverType, columnName string, queryVector Vector) string {
	if driver == DriverPostgres {
		return pgCosineSimilarity(columnName, queryVector)
	}
	return fmt.Sprintf("vector_distance_cos(%s, %s)", columnName, tursoVectorExpression(queryVector))
}

func cosineSimilarityScoreExpr(driver DriverType, columnName string, queryVector Vector) string {
	if driver == DriverPostgres {
		return pgCosineSimilarityScore(columnName, queryVector)
	}
	return fmt.Sprintf("(1 - (vector_distance_cos(%s, %s) / 2.0))", columnName, tursoVectorExpression(queryVector))
}

// EuclideanDistance calculates Euclidean distance between two vectors
// Returns a SQL expression that can be used in SELECT/WHERE/ORDER BY clauses
func (vh *VectorHelper) EuclideanDistance(columnName string, queryVector Vector) string {
	if vh.db.GetDriverType() == DriverPostgres {
		return pgEuclideanDistance(columnName, queryVector)
	}
	return fmt.Sprintf("vector_distance_l2(%s, %s)", columnName, tursoVectorExpression(queryVector))
}

// ExtractVector extracts a vector from the database into a string representation.
// PostgreSQL returns vector columns directly; Turso uses vector_extract().
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
	} else {
		// Full table scan with exact normalized cosine score calculation
		query = fmt.Sprintf(`
			SELECT *
			FROM %s
		`, tableName)

		if additionalWhere != "" {
			query += " WHERE " + additionalWhere
		}

		query += fmt.Sprintf(`
			ORDER BY %s DESC
			LIMIT %d
		`, vh.CosineSimilarityScore(vectorColumn, queryVector), limit)
	}

	return vh.db.QueryContext(ctx, query, args...)
}

func tursoVectorExpression(vector Vector) string {
	return fmt.Sprintf("vector32('%s')", vector.String())
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
