package semantic

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/jkatigb/agentctl/internal/indexing"
	"github.com/jkatigb/agentctl/internal/storage"
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

	// Create result metadata
	result := FileEmbeddingResult{
		Path:      file.Path,
		Digest:    file.Digest,
		Language:  file.Language,
		SizeBytes: file.SizeBytes,
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
func (idx *Indexer) indexChunkedFile(ctx context.Context, event indexing.PostReviewEvent, file indexing.FileChange, content []byte, configHash string) error {
	chunks := idx.splitIntoChunks(content)
	chunkCount := len(chunks)

	idx.logger.Debug().
		Str("path", file.Path).
		Int("chunk_count", chunkCount).
		Str("config_hash", configHash).
		Msg("indexing chunked file")

	// First, create the parent file_embedding entry
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

	fileName := FileEmbeddingName(event.WorkspaceID, file.Path)
	fileEntry := storage.NamedEntry{
		Name:      fileName,
		Type:      FileEmbeddingType,
		Workspace: event.WorkspaceID,
		Summary:   fmt.Sprintf("Semantic embedding for %s (%d chunks)", file.Path, chunkCount),
		Result:    fileResultBytes,
	}

	if _, err := idx.memoryStore.Save(ctx, fileEntry); err != nil {
		return fmt.Errorf("save file entry: %w", err)
	}

	// Then, create chunk entries
	for i, chunk := range chunks {
		chunkID := fmt.Sprintf("%d", i)

		// Generate embedding for chunk
		embedding, err := idx.provider.Embed(ctx, string(chunk.Content))
		if err != nil {
			return fmt.Errorf("embed chunk %d: %w", i, err)
		}

		chunkResult := ChunkEmbeddingResult{
			Path:     file.Path,
			Digest:   file.Digest,
			Language: file.Language,
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
			return fmt.Errorf("marshal chunk result: %w", err)
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
			return fmt.Errorf("save chunk entry %d: %w", i, err)
		}

		idx.logger.Debug().
			Str("path", file.Path).
			Int("chunk", i).
			Int("embedding_dim", len(embedding)).
			Msg("indexed chunk")
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

// deleteFileEmbedding removes a file's embedding entries.
func (idx *Indexer) deleteFileEmbedding(ctx context.Context, workspace, path string) error {
	name := FileEmbeddingName(workspace, path)
	if err := idx.memoryStore.Delete(ctx, name, workspace); err != nil {
		// Ignore not found errors
		if err.Error() != "not found" {
			return err
		}
	}
	// TODO: Also delete chunk entries if they exist
	return nil
}

// readFileContent reads the content of a file from the workspace.
func (idx *Indexer) readFileContent(path string) ([]byte, error) {
	fullPath := filepath.Join(idx.workspaceRoot, path)
	f, err := os.Open(fullPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	return io.ReadAll(f)
}
