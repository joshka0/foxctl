package semantic

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// FileEmbeddingType is the memory entry type for single-file embeddings.
const FileEmbeddingType = "file_embedding"

// FileEmbeddingChunkType is the memory entry type for chunked file embeddings.
const FileEmbeddingChunkType = "file_embedding_chunk"

// FileEmbeddingResult is the structured metadata stored in NamedEntry.Result
// for file_embedding entries.
type FileEmbeddingResult struct {
	// Path is the workspace-relative file path.
	Path string `json:"path"`

	// Digest is the CAS digest of the file content used for embedding.
	Digest string `json:"digest,omitempty"`

	// Language is the detected or configured language (optional).
	Language string `json:"language,omitempty"`

	// SizeBytes is the file size in bytes.
	SizeBytes int64 `json:"size_bytes,omitempty"`

	// Embedding is the generated embedding vector (for single-file embeddings).
	// Empty for chunked files; use chunk entries for embeddings.
	Embedding []float32 `json:"embedding,omitempty"`

	// ChunkCount is the number of chunks (0 for single-embedding files).
	ChunkCount int `json:"chunk_count,omitempty"`

	// ChunkingConfigHash identifies the chunking configuration used.
	// Empty for single-embedding files.
	ChunkingConfigHash string `json:"chunking_config_hash,omitempty"`

	// Source tracks provenance of the embedding.
	Source *EmbeddingSource `json:"source,omitempty"`
}

// ChunkEmbeddingResult is the structured metadata stored in NamedEntry.Result
// for file_embedding_chunk entries.
type ChunkEmbeddingResult struct {
	// Path is the workspace-relative file path.
	Path string `json:"path"`

	// Digest is the CAS digest of the file content.
	Digest string `json:"digest,omitempty"`

	// Language is the detected or configured language (optional).
	Language string `json:"language,omitempty"`

	// Embedding is the generated embedding vector for this chunk.
	Embedding []float32 `json:"embedding,omitempty"`

	// Chunk contains chunk-specific metadata.
	Chunk ChunkInfo `json:"chunk"`

	// Source tracks provenance of the embedding.
	Source *EmbeddingSource `json:"source,omitempty"`
}

// ChunkInfo describes a single chunk within a file.
type ChunkInfo struct {
	// ID is a stable identifier for this chunk.
	ID string `json:"id"`

	// Kind describes how the planner produced this chunk.
	Kind string `json:"kind,omitempty"`

	// Index is the zero-based chunk index.
	Index int `json:"index"`

	// Of is the total number of chunks for this file.
	Of int `json:"of"`

	// SizeBytes is the number of bytes of semantic text embedded for this chunk.
	SizeBytes int64 `json:"size_bytes,omitempty"`

	// Span describes the byte or line range of this chunk.
	Span *ChunkSpan `json:"span,omitempty"`

	// SymbolIdentifiers contains language symbols used to produce this chunk.
	SymbolIdentifiers []string `json:"symbol_identifiers,omitempty"`
}

// ChunkSpan describes the range of a chunk within a file.
type ChunkSpan struct {
	// Unit is "byte" or "line".
	Unit string `json:"unit"`

	// Start is the start offset (byte or line, 0-indexed).
	Start int `json:"start"`

	// End is the end offset (exclusive).
	End int `json:"end"`
}

// EmbeddingSource tracks the provenance of an embedding.
type EmbeddingSource struct {
	// TaskID is the task that triggered (re)indexing.
	TaskID string `json:"task_id,omitempty"`

	// ReviewID is the review record if triggered post-review.
	ReviewID string `json:"review_id,omitempty"`

	// Actor is the actor that created the embedding.
	Actor string `json:"actor,omitempty"`

	// Reason describes why the embedding was created/updated.
	Reason string `json:"reason,omitempty"`
}

// Config holds configuration for the semantic file indexer.
type Config struct {
	// Enabled controls whether semantic indexing is active.
	Enabled bool `json:"enabled"`

	// ChunkBytes is the target chunk size in bytes (0 = no chunking).
	ChunkBytes int `json:"chunk_bytes,omitempty"`

	// ChunkOverlapBytes is the overlap between chunks (default: 0).
	ChunkOverlapBytes int `json:"chunk_overlap_bytes,omitempty"`

	// ChunkDelay is an optional delay between chunk embedding requests.
	ChunkDelay time.Duration `json:"chunk_delay,omitempty"`

	// MaxFileKB is the maximum file size in KB to index (0 = no limit).
	MaxFileKB int `json:"max_file_kb,omitempty"`

	// IncludeGlobs are glob patterns for files to include.
	IncludeGlobs []string `json:"include_globs,omitempty"`

	// ExcludeGlobs are glob patterns for files to exclude.
	ExcludeGlobs []string `json:"exclude_globs,omitempty"`

	// ProviderModel is the embedding model identifier (for config hash).
	ProviderModel string `json:"provider_model,omitempty"`
}

// ChunkingConfigHash computes a stable hash for the chunking configuration.
// This is used to detect when chunk boundaries need to be recalculated.
func (c Config) ChunkingConfigHash() string {
	if c.ChunkBytes == 0 {
		return "" // No chunking
	}
	data := fmt.Sprintf("planner=file-spans-v1;chunk_bytes=%d;overlap=%d;model=%s",
		c.ChunkBytes, c.ChunkOverlapBytes, c.ProviderModel)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:8]) // First 8 bytes for brevity
}

// FileEmbeddingName generates the canonical name for a file embedding entry.
// Format: file://<workspace>/<path>
// Uses url.PathEscape for each path segment to consistently encode URI-special characters.
func FileEmbeddingName(workspace, path string) string {
	return fmt.Sprintf("file://%s/%s", url.PathEscape(workspace), escapePathSegments(path))
}

// ChunkEmbeddingName generates the canonical name for a chunk embedding entry.
// Format: file://<workspace>/<path>#chunk-<chunk_id>?cfg=<hash>
// Uses url.PathEscape for path segments and url.QueryEscape for query parameters.
func ChunkEmbeddingName(workspace, path, chunkID, configHash string) string {
	return fmt.Sprintf("file://%s/%s#chunk-%s?cfg=%s",
		url.PathEscape(workspace),
		escapePathSegments(path),
		url.PathEscape(chunkID),
		url.QueryEscape(configHash))
}

// escapePathSegments applies url.PathEscape to each segment of a path,
// preserving the "/" separators. This ensures URI-special characters like
// "#" and "?" are properly encoded while maintaining path structure.
func escapePathSegments(path string) string {
	segments := strings.Split(path, "/")
	for i, seg := range segments {
		segments[i] = url.PathEscape(seg)
	}
	return strings.Join(segments, "/")
}

// MarshalResult serializes a result struct to JSON bytes for storage.
func MarshalResult(v any) ([]byte, error) {
	return json.Marshal(v)
}

// UnmarshalFileResult deserializes a FileEmbeddingResult from JSON bytes.
func UnmarshalFileResult(data []byte) (*FileEmbeddingResult, error) {
	var result FileEmbeddingResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UnmarshalChunkResult deserializes a ChunkEmbeddingResult from JSON bytes.
func UnmarshalChunkResult(data []byte) (*ChunkEmbeddingResult, error) {
	var result ChunkEmbeddingResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
