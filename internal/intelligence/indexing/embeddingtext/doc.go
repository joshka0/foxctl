// Package embeddingtext provides utilities for building and normalizing
// text content for vector embeddings.
//
// The package supports two modes of operation:
//   - Raw mode: Embed the original content as-is
//   - Doc-enriched mode: Combine doc comments, signatures, and relationships
//
// Index:
//   Purpose: Build high-quality text for embedding generation
//   Keywords: embedding text, normalize, digest, symbol text
//   Related: semantic.EmbeddingProvider, symbol.Symbol, indexing.embeddingtext
//   Flow: extract symbol info → normalize → build text → digest
//   Resources: symbol metadata, embedding provider
//   Events: none
//   OutputFields: embedding text, digest
//
// [[domain:embedding-text-generation]]
package embeddingtext
