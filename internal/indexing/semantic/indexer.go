package semantic

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/indexing"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/rs/zerolog"
)

// IndexerID is the canonical identifier for the semantic file indexer.
const IndexerID = "semantic_file_index"

// Indexer implements the indexing.Indexer interface for semantic file embeddings.
type Indexer struct {
	config        Config
	memoryStore   storage.MemoryStore
	provider      EmbeddingProvider
	workspaceRoot string
	logger        zerolog.Logger
}

// NewIndexer creates a new semantic file indexer.
func NewIndexer(
	cfg Config,
	memoryStore storage.MemoryStore,
	provider EmbeddingProvider,
	workspaceRoot string,
	logger zerolog.Logger,
) *Indexer {
	return &Indexer{
		config:        cfg,
		memoryStore:   memoryStore,
		provider:      provider,
		workspaceRoot: workspaceRoot,
		logger:        logger.With().Str("indexer", IndexerID).Logger(),
	}
}

// ID returns the indexer identifier.
func (idx *Indexer) ID() string {
	return IndexerID
}

// Index processes a post-review event and updates file embeddings.
func (idx *Indexer) Index(ctx context.Context, event indexing.PostReviewEvent) (*indexing.IndexerResult, error) {
	if !idx.config.Enabled {
		return &indexing.IndexerResult{
			IndexerID:    IndexerID,
			FilesSkipped: len(event.Files),
		}, nil
	}

	result := &indexing.IndexerResult{
		IndexerID: IndexerID,
	}

	for _, file := range event.Files {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		// Handle deleted files
		if file.ChangeKind == indexing.ChangeKindDeleted {
			if err := idx.deleteFileEmbedding(ctx, event.WorkspaceID, file.Path); err != nil {
				idx.logger.Warn().Err(err).Str("path", file.Path).Msg("failed to delete embedding")
				result.FilesFailed++
				result.Failures = append(result.Failures, indexing.IndexerFailure{
					Path:         file.Path,
					ErrorCode:    "DELETE_FAILED",
					ErrorMessage: err.Error(),
				})
			} else {
				result.FilesIndexed++
			}
			continue
		}

		// Index or update the file
		if err := idx.indexFile(ctx, event, file); err != nil {
			idx.logger.Warn().Err(err).Str("path", file.Path).Msg("failed to index file")
			result.FilesFailed++
			result.Failures = append(result.Failures, indexing.IndexerFailure{
				Path:         file.Path,
				ErrorCode:    "INDEX_FAILED",
				ErrorMessage: err.Error(),
			})
			continue
		}

		result.FilesIndexed++
	}

	idx.logger.Info().
		Int("indexed", result.FilesIndexed).
		Int("failed", result.FilesFailed).
		Int("skipped", result.FilesSkipped).
		Msg("semantic indexing completed")

	return result, nil
}

// indexFile indexes a single file, creating or updating its embedding.
func (idx *Indexer) indexFile(ctx context.Context, event indexing.PostReviewEvent, file indexing.FileChange) error {
	// Read file content
	content, err := idx.readFileContent(file.Path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	// Check if chunking is needed
	configHash := idx.config.ChunkingConfigHash()
	if idx.config.ChunkBytes > 0 && len(content) > idx.config.ChunkBytes {
		return idx.indexChunkedFile(ctx, event, file, content, configHash)
	}

	return idx.indexSingleFile(ctx, event, file, content)
}

// indexSingleFile creates a single embedding for the entire file.
func (idx *Indexer) indexSingleFile(ctx context.Context, event indexing.PostReviewEvent, file indexing.FileChange, content []byte) error {
	// Generate embedding
	embedding, err := idx.provider.Embed(ctx, string(content))
	if err != nil {
		return fmt.Errorf("embed: %w", err)
	}

	// Create result metadata with embedding
	result := FileEmbeddingResult{
		Path:      file.Path,
		Digest:    file.Digest,
		Language:  file.Language,
		SizeBytes: file.SizeBytes,
		Embedding: embedding,
		Source: &EmbeddingSource{
			TaskID:   event.TaskID,
			ReviewID: event.ReviewID,
			Actor:    "actor:system:semantic_indexer",
			Reason:   event.Reason,
		},
	}

	resultBytes, err := MarshalResult(result)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}

	// Save to memory store
	name := FileEmbeddingName(event.WorkspaceID, file.Path)
	entry := storage.NamedEntry{
		Name:      name,
		Type:      FileEmbeddingType,
		Workspace: event.WorkspaceID,
		Summary:   fmt.Sprintf("Semantic embedding for %s", file.Path),
		Result:    resultBytes,
	}

	if _, err := idx.memoryStore.Save(ctx, entry); err != nil {
		return fmt.Errorf("save entry: %w", err)
	}

	idx.logger.Debug().
		Str("path", file.Path).
		Int("embedding_dim", len(embedding)).
		Msg("indexed file")

	return nil
}

// indexChunkedFile creates multiple chunk embeddings for a large file.
// It implements cleanup on failure to prevent partial index state.
func (idx *Indexer) indexChunkedFile(ctx context.Context, event indexing.PostReviewEvent, file indexing.FileChange, content []byte, configHash string) error {
	chunks := idx.splitIntoChunks(content)
	chunkCount := len(chunks)

	idx.logger.Debug().
		Str("path", file.Path).
		Int("chunk_count", chunkCount).
		Str("config_hash", configHash).
		Msg("indexing chunked file")

	// Phase 1: Generate all embeddings first (fail fast before any persistence)
	chunkEmbeddings := make([][]float32, chunkCount)
	for i, chunk := range chunks {
		embedding, err := idx.provider.Embed(ctx, string(chunk.Content))
		if err != nil {
			return fmt.Errorf("embed chunk %d: %w", i, err)
		}
		chunkEmbeddings[i] = embedding
	}

	// Phase 2: Prepare all entries
	fileName := FileEmbeddingName(event.WorkspaceID, file.Path)
	fileResult := FileEmbeddingResult{
		Path:               file.Path,
		Digest:             file.Digest,
		Language:           file.Language,
		SizeBytes:          file.SizeBytes,
		ChunkCount:         chunkCount,
		ChunkingConfigHash: configHash,
		Source: &EmbeddingSource{
			TaskID:   event.TaskID,
			ReviewID: event.ReviewID,
			Actor:    "actor:system:semantic_indexer",
			Reason:   event.Reason,
		},
	}

	fileResultBytes, err := MarshalResult(fileResult)
	if err != nil {
		return fmt.Errorf("marshal file result: %w", err)
	}

	fileEntry := storage.NamedEntry{
		Name:      fileName,
		Type:      FileEmbeddingType,
		Workspace: event.WorkspaceID,
		Summary:   fmt.Sprintf("Semantic embedding for %s (%d chunks)", file.Path, chunkCount),
		Result:    fileResultBytes,
	}

	// Phase 3: Save file entry first
	if _, err := idx.memoryStore.Save(ctx, fileEntry); err != nil {
		return fmt.Errorf("save file entry: %w", err)
	}

	// Phase 4: Save chunk entries with cleanup on failure
	savedChunks := 0
	var saveErr error

	for i, chunk := range chunks {
		chunkID := fmt.Sprintf("%d", i)

		chunkResult := ChunkEmbeddingResult{
			Path:      file.Path,
			Digest:    file.Digest,
			Language:  file.Language,
			Embedding: chunkEmbeddings[i],
			Chunk: ChunkInfo{
				ID:    chunkID,
				Index: i,
				Of:    chunkCount,
				Span: &ChunkSpan{
					Unit:  "byte",
					Start: chunk.Start,
					End:   chunk.End,
				},
			},
			Source: &EmbeddingSource{
				TaskID:   event.TaskID,
				ReviewID: event.ReviewID,
				Actor:    "actor:system:semantic_indexer",
				Reason:   event.Reason,
			},
		}

		chunkResultBytes, err := MarshalResult(chunkResult)
		if err != nil {
			saveErr = fmt.Errorf("marshal chunk result %d: %w", i, err)
			break
		}

		chunkName := ChunkEmbeddingName(event.WorkspaceID, file.Path, chunkID, configHash)
		chunkEntry := storage.NamedEntry{
			Name:      chunkName,
			Type:      FileEmbeddingChunkType,
			Workspace: event.WorkspaceID,
			Summary:   fmt.Sprintf("Chunk %d/%d of %s", i+1, chunkCount, file.Path),
			Result:    chunkResultBytes,
		}

		if _, err := idx.memoryStore.Save(ctx, chunkEntry); err != nil {
			saveErr = fmt.Errorf("save chunk entry %d: %w", i, err)
			break
		}

		savedChunks++

		idx.logger.Debug().
			Str("path", file.Path).
			Int("chunk", i).
			Int("embedding_dim", len(chunkEmbeddings[i])).
			Msg("indexed chunk")
	}

	// Cleanup on failure: remove file entry and any saved chunks
	if saveErr != nil {
		idx.logger.Warn().
			Err(saveErr).
			Str("path", file.Path).
			Int("saved_chunks", savedChunks).
			Msg("chunk indexing failed, cleaning up partial state")

		// Delete the file entry and any saved chunks
		if cleanupErr := idx.deleteFileEmbedding(ctx, event.WorkspaceID, file.Path); cleanupErr != nil {
			idx.logger.Warn().
				Err(cleanupErr).
				Str("path", file.Path).
				Msg("failed to cleanup partial index state")
		}

		return saveErr
	}

	return nil
}

// Chunk represents a portion of file content.
type Chunk struct {
	Content []byte
	Start   int
	End     int
}

// splitIntoChunks divides content into fixed-size overlapping chunks.
func (idx *Indexer) splitIntoChunks(content []byte) []Chunk {
	chunkSize := idx.config.ChunkBytes
	overlap := idx.config.ChunkOverlapBytes

	if chunkSize <= 0 {
		return []Chunk{{Content: content, Start: 0, End: len(content)}}
	}

	var chunks []Chunk
	start := 0
	for start < len(content) {
		end := start + chunkSize
		if end > len(content) {
			end = len(content)
		}

		chunks = append(chunks, Chunk{
			Content: content[start:end],
			Start:   start,
			End:     end,
		})

		// Move start forward, accounting for overlap
		nextStart := end - overlap
		if nextStart <= start {
			// overlap >= chunkSize would cause no progress
			break
		}
		start = nextStart
	}

	return chunks
}

// deleteFileEmbedding removes a file's embedding entries (both single-file and chunks).
func (idx *Indexer) deleteFileEmbedding(ctx context.Context, workspace, path string) error {
	// Delete the single-file embedding entry
	name := FileEmbeddingName(workspace, path)
	if err := idx.memoryStore.Delete(ctx, name, workspace); err != nil {
		// Ignore not found errors
		if !errors.Is(err, memory.ErrNotFound) {
			return err
		}
	}

	// Delete all chunk entries for this file
	// Chunks are named: file://<workspace>/<path>#chunk-<id>?cfg=<hash>
	chunkPrefix := name + "#chunk-"
	if _, err := idx.memoryStore.DeleteByNamePrefix(ctx, workspace, chunkPrefix); err != nil {
		idx.logger.Warn().Err(err).Str("path", path).Msg("failed to delete chunk entries")
	}

	return nil
}

// maxReadFileSize is the maximum file size we'll read (10MB).
const maxReadFileSize = 10 * 1024 * 1024

// readFileContent reads file content from the workspace with path validation and size limits.
func (idx *Indexer) readFileContent(path string) ([]byte, error) {
	// Clean the path to prevent traversal attacks
	cleanPath := filepath.Clean(path)

	// Reject absolute paths or paths that escape the workspace
	if filepath.IsAbs(cleanPath) {
		return nil, fmt.Errorf("absolute paths not allowed: %s", path)
	}
	if strings.HasPrefix(cleanPath, "..") {
		return nil, fmt.Errorf("path traversal not allowed: %s", path)
	}

	// Join with workspace root and resolve
	fullPath := filepath.Join(idx.workspaceRoot, cleanPath)

	// Resolve to absolute path
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}
	absWorkspace, err := filepath.Abs(idx.workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace: %w", err)
	}

	// Resolve symlinks to detect symlink-based traversal attacks
	// EvalSymlinks also calls Clean and Abs internally
	evalPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		// If the file doesn't exist yet, EvalSymlinks fails; fall back to absPath
		// but this is fine since os.Stat below will catch non-existent files
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("resolve symlinks for path: %w", err)
		}
		evalPath = absPath
	}
	evalWorkspace, err := filepath.EvalSymlinks(absWorkspace)
	if err != nil {
		return nil, fmt.Errorf("resolve symlinks for workspace: %w", err)
	}

	// Ensure the resolved path (with symlinks evaluated) is within the workspace
	if !strings.HasPrefix(evalPath, evalWorkspace+string(filepath.Separator)) && evalPath != evalWorkspace {
		return nil, fmt.Errorf("path escapes workspace: %s", path)
	}

	// Stat the file to check type and size
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file: %s", path)
	}
	if info.Size() > maxReadFileSize {
		return nil, fmt.Errorf("file too large (%d bytes, max %d): %s", info.Size(), maxReadFileSize, path)
	}

	// Open and read with size limit
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	// Use LimitReader as additional safety even though we checked size
	return io.ReadAll(io.LimitReader(f, maxReadFileSize))
}
