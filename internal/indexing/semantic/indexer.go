package semantic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/indexing"
	"github.com/jkatigb/agentctl/internal/platform/fsutil"
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
//
// Index:
// - Purpose: Update semantic file embeddings for post-review changes
// - Flow: validate config → loop files → delete removed → embed/update → record results
// - SideEffects: reads files; writes embeddings to memory store
// - FailureModes: file I/O errors, embedding provider errors, store errors
// - Related: deleteFileEmbedding, indexFile
// - Keywords: semantic_file_index, embeddings, post_review, files_indexed, files_failed
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
	if err := idx.memoryStore.UpdateEmbedding(ctx, name, event.WorkspaceID, embedding); err != nil {
		return fmt.Errorf("update embedding: %w", err)
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
		if err := idx.memoryStore.UpdateEmbedding(ctx, chunkName, event.WorkspaceID, chunkEmbeddings[i]); err != nil {
			saveErr = fmt.Errorf("update chunk embedding %d: %w", i, err)
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

// =============================================================================
// Job Execution Logic (P3.S4)
// =============================================================================

// RunInitFilesJob executes a semantic_index.init_files job in-process.
// It indexes all provided files, treating them as new additions.
func (idx *Indexer) RunInitFilesJob(ctx context.Context, args JobArgs) (*JobResult, error) {
	if err := args.Validate(); err != nil {
		return nil, fmt.Errorf("validate args: %w", err)
	}

	return idx.runIndexJob(ctx, args, true)
}

// RunUpdateFilesJob executes a semantic_index.update_files job in-process.
// It processes file changes (add, modify, delete) based on ChangeKind.
func (idx *Indexer) RunUpdateFilesJob(ctx context.Context, args JobArgs) (*JobResult, error) {
	if err := args.Validate(); err != nil {
		return nil, fmt.Errorf("validate args: %w", err)
	}

	return idx.runIndexJob(ctx, args, false)
}

// runIndexJob is the shared implementation for init_files and update_files.
// isInit=true treats all files as new (ignores ChangeKind).
func (idx *Indexer) runIndexJob(ctx context.Context, args JobArgs, isInit bool) (*JobResult, error) {
	result := &JobResult{
		Summary: JobSummary{},
	}

	configHash := idx.config.ChunkingConfigHash()

	for _, file := range args.Files {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		// Handle deleted files (only in update mode)
		if !isInit && file.ChangeKind == ChangeKindDeleted {
			if err := idx.deleteFileEmbedding(ctx, args.WorkspaceID, file.Path); err != nil {
				idx.addJobFailure(result, file, ErrCodeSemanticIndexNotFound, err)
			} else {
				result.Summary.FilesIndexed++
			}
			continue
		}

		// Index the file
		chunksIndexed, err := idx.indexFileForJob(ctx, args, file, configHash)
		if err != nil {
			idx.addJobFailure(result, file, idx.classifyError(err), err)
			continue
		}

		result.Summary.FilesIndexed++
		result.Summary.ChunksIndexed += chunksIndexed
	}

	idx.logger.Info().
		Str("workspace_id", args.WorkspaceID).
		Int("files_indexed", result.Summary.FilesIndexed).
		Int("chunks_indexed", result.Summary.ChunksIndexed).
		Int("failures", len(result.Failures)).
		Msg("job completed")

	return result, nil
}

// indexFileForJob indexes a single file for a job, returning chunk count.
func (idx *Indexer) indexFileForJob(ctx context.Context, args JobArgs, file JobFileInput, configHash string) (chunksIndexed int, err error) {
	// Read file content
	content, err := idx.readFileContent(file.Path)
	if err != nil {
		return 0, fmt.Errorf("read file: %w", err)
	}

	// Determine language (use provided or detect)
	language := file.Language
	if language == "" {
		language = fsutil.DetectLanguage(file.Path)
	}

	// Compute digest if not provided
	digest := file.Digest
	if digest == "" {
		digest = computeDigest(content)
	}

	// Determine size
	sizeBytes := file.SizeBytes
	if sizeBytes == 0 {
		sizeBytes = int64(len(content))
	}

	source := &EmbeddingSource{
		TaskID:   args.TaskID,
		ReviewID: args.ReviewID,
		Actor:    "actor:system:semantic_indexer",
		Reason:   string(args.Reason),
	}

	// Check if chunking is needed
	if idx.config.ChunkBytes > 0 && len(content) > idx.config.ChunkBytes {
		chunks := idx.splitIntoChunks(content)
		chunkCount := len(chunks)

		idx.logger.Info().Str("path", file.Path).Int("chunks", chunkCount).Msg("indexing chunked file")

		// Phase 1: Generate all embeddings first
		chunkEmbeddings := make([][]float32, chunkCount)
		for i, chunk := range chunks {
			embedding, err := idx.provider.Embed(ctx, string(chunk.Content))
			if err != nil {
				return 0, fmt.Errorf("embed chunk %d: %w", i, err)
			}
			chunkEmbeddings[i] = embedding
		}

		// Phase 2: Save file entry (no embedding, just metadata)
		fileResult := FileEmbeddingResult{
			Path:               file.Path,
			Digest:             digest,
			Language:           language,
			SizeBytes:          sizeBytes,
			ChunkCount:         chunkCount,
			ChunkingConfigHash: configHash,
			Source:             source,
		}

		if err := idx.saveFileEntry(ctx, args.WorkspaceID, file.Path, fileResult); err != nil {
			return 0, err
		}

		// Phase 3: Save all chunks
		for i, chunk := range chunks {
			chunkID := fmt.Sprintf("%d", i)
			chunkResult := ChunkEmbeddingResult{
				Path:      file.Path,
				Digest:    digest,
				Language:  language,
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
				Source: source,
			}

			if err := idx.saveChunkEntry(ctx, args.WorkspaceID, file.Path, chunkID, configHash, chunkResult); err != nil {
				// Cleanup on failure; error is not actionable.
				_ = idx.deleteFileEmbedding(ctx, args.WorkspaceID, file.Path) //nolint:errcheck
				return 0, err
			}
		}

		return chunkCount, nil
	}

	// Single file embedding
	idx.logger.Info().Str("path", file.Path).Int("size", len(content)).Msg("indexing file")
	embedding, err := idx.provider.Embed(ctx, string(content))
	if err != nil {
		return 0, fmt.Errorf("embed: %w", err)
	}

	fileResult := FileEmbeddingResult{
		Path:      file.Path,
		Digest:    digest,
		Language:  language,
		SizeBytes: sizeBytes,
		Embedding: embedding,
		Source:    source,
	}

	if err := idx.saveFileEntry(ctx, args.WorkspaceID, file.Path, fileResult); err != nil {
		return 0, err
	}

	return 0, nil
}

// saveFileEntry saves a file embedding entry to the memory store.
func (idx *Indexer) saveFileEntry(ctx context.Context, workspace, path string, result FileEmbeddingResult) error {
	resultBytes, err := MarshalResult(result)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}

	name := FileEmbeddingName(workspace, path)
	summary := fmt.Sprintf("Semantic embedding for %s", path)
	if result.ChunkCount > 0 {
		summary = fmt.Sprintf("Semantic embedding for %s (%d chunks)", path, result.ChunkCount)
	}

	entry := storage.NamedEntry{
		Name:      name,
		Type:      FileEmbeddingType,
		Workspace: workspace,
		Summary:   summary,
		Result:    resultBytes,
	}

	if _, err := idx.memoryStore.Save(ctx, entry); err != nil {
		return fmt.Errorf("save entry: %w", err)
	}

	// Store embedding in dedicated column for vector similarity search
	if len(result.Embedding) > 0 {
		if err := idx.memoryStore.UpdateEmbedding(ctx, name, workspace, result.Embedding); err != nil {
			return fmt.Errorf("update embedding: %w", err)
		}
	}

	return nil
}

// saveChunkEntry saves a chunk embedding entry to the memory store.
func (idx *Indexer) saveChunkEntry(ctx context.Context, workspace, path, chunkID, configHash string, result ChunkEmbeddingResult) error {
	resultBytes, err := MarshalResult(result)
	if err != nil {
		return fmt.Errorf("marshal chunk result: %w", err)
	}

	name := ChunkEmbeddingName(workspace, path, chunkID, configHash)
	entry := storage.NamedEntry{
		Name:      name,
		Type:      FileEmbeddingChunkType,
		Workspace: workspace,
		Summary:   fmt.Sprintf("Chunk %d/%d of %s", result.Chunk.Index+1, result.Chunk.Of, path),
		Result:    resultBytes,
	}

	if _, err := idx.memoryStore.Save(ctx, entry); err != nil {
		return fmt.Errorf("save chunk entry: %w", err)
	}

	// Store embedding in dedicated column for vector similarity search
	if len(result.Embedding) > 0 {
		if err := idx.memoryStore.UpdateEmbedding(ctx, name, workspace, result.Embedding); err != nil {
			return fmt.Errorf("update chunk embedding: %w", err)
		}
	}

	return nil
}

// addJobFailure appends a failure to the job result.
func (idx *Indexer) addJobFailure(result *JobResult, file JobFileInput, code string, err error) {
	result.Failures = append(result.Failures, JobFailure{
		File: JobFailureFile{
			Path:   file.Path,
			Digest: file.Digest,
		},
		ErrorCode:    code,
		ErrorMessage: err.Error(),
		Timestamp:    time.Now().UTC(),
	})
}

// classifyError maps an error to an appropriate error code.
func (idx *Indexer) classifyError(err error) string {
	errStr := err.Error()

	if strings.Contains(errStr, "embed") {
		return ErrCodeEmbeddingProviderFailure
	}
	if strings.Contains(errStr, "read file") || strings.Contains(errStr, "not a regular file") {
		return ErrCodeCASResolveError
	}
	if strings.Contains(errStr, "path escapes") || strings.Contains(errStr, "traversal") {
		return ErrCodeCASResolveError
	}

	return ErrCodeSemanticIndexNotFound
}

// computeDigest computes a SHA-256 digest of the content.
func computeDigest(content []byte) string {
	h := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(h[:])
}

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
	// Use evalPath (symlink-resolved) for all I/O to avoid TOCTOU vulnerabilities
	info, err := os.Stat(evalPath)
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
	f, err := os.Open(evalPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		// File cleanup in defer; error is not actionable.
		_ = f.Close() //nolint:errcheck
	}()

	// Use LimitReader as additional safety even though we checked size
	return io.ReadAll(io.LimitReader(f, maxReadFileSize))
}
