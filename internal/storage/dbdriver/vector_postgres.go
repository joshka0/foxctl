package dbdriver

import (
	"fmt"
	"strings"
)

// pgQuoteIdent safely quotes a PostgreSQL identifier with double quotes.
// Embedded double quotes are escaped by doubling them.
func pgQuoteIdent(name string) string {
	return "\"" + strings.ReplaceAll(name, "\"", "\"\"") + "\""
}

// pgCosineSimilarity returns a SQL expression for cosine similarity using pgvector.
// Returns 1 - distance so higher = more similar in the range [-1, 1].
// pgvector's <=> operator returns cosine distance (0 = identical, 2 = opposite).
func pgCosineSimilarity(columnName string, queryVector Vector) string {
	return fmt.Sprintf("(1 - (%s <=> '%s'))", pgQuoteIdent(columnName), queryVector.String())
}

// pgCosineSimilarityScore returns a SQL expression that maps cosine similarity
// into a normalized [0,1] similarity score.
func pgCosineSimilarityScore(columnName string, queryVector Vector) string {
	return fmt.Sprintf("((1 - (%s <=> '%s')) + 1.0) / 2.0", pgQuoteIdent(columnName), queryVector.String())
}

// pgEuclideanDistance returns a SQL expression for Euclidean distance using pgvector.
func pgEuclideanDistance(columnName string, queryVector Vector) string {
	return fmt.Sprintf("(%s <-> '%s')", pgQuoteIdent(columnName), queryVector.String())
}

// pgVectorExpression returns a pgvector-compatible vector literal for INSERT/UPDATE.
func pgVectorExpression(vector Vector) string {
	return fmt.Sprintf("'%s'", vector.String())
}

// pgCreateVectorColumnSQL returns DDL to add a vector column to a PostgreSQL table.
// Uses ADD COLUMN IF NOT EXISTS (PostgreSQL-native).
func pgCreateVectorColumnSQL(tableName, columnName string, dimensions int) string {
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s vector(%d)",
		pgQuoteIdent(tableName), pgQuoteIdent(columnName), dimensions)
}

// pgCreateVectorIndexSQL returns DDL to create an HNSW vector index for cosine search.
// HNSW parameters: m=16, ef_construction=64 are good defaults for most workloads.
func pgCreateVectorIndexSQL(tableName, columnName, indexName string) string {
	return fmt.Sprintf(
		"CREATE INDEX IF NOT EXISTS %s ON %s USING hnsw (%s vector_cosine_ops) WITH (m = 16, ef_construction = 64)",
		pgQuoteIdent(indexName), pgQuoteIdent(tableName), pgQuoteIdent(columnName),
	)
}
