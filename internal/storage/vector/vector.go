// Package vector re-exports the foxsearch vector module.
//
// The implementation moved to github.com/joshka0/foxsearch/vector as part of
// the foxsearch extraction; this shim preserves the foxctl import path for
// existing consumers. New code may import either path.
package vector

import "github.com/joshka0/foxsearch/vector"

type (
	// IDEmbedding pairs a string ID with its embedding vector.
	IDEmbedding = vector.IDEmbedding
	// ScoredID is an ID with a similarity score.
	ScoredID = vector.ScoredID
	// VectorSearcher is the common interface for vector search backends.
	VectorSearcher = vector.VectorSearcher
	// BruteForceSearcher performs exact cosine search over candidates.
	BruteForceSearcher = vector.BruteForceSearcher
	// TurbovecSearcher uses the turbovec sidecar for ANN search with exact reranking.
	TurbovecSearcher = vector.TurbovecSearcher
)

var (
	// NewSearcher returns a turbovec-backed searcher when available, else brute force.
	NewSearcher = vector.NewSearcher
	// SerializeF32 encodes a float32 slice to little-endian bytes.
	SerializeF32 = vector.SerializeF32
	// DeserializeF32 decodes little-endian bytes to a float32 slice.
	DeserializeF32 = vector.DeserializeF32
	// Cosine returns the cosine similarity of two vectors.
	Cosine = vector.Cosine
	// DimsFromBytes returns the vector dimension implied by a serialized payload.
	DimsFromBytes = vector.DimsFromBytes
)
