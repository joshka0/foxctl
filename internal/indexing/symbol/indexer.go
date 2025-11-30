package symbol

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

// IndexerID is the canonical identifier for the symbol indexer.
const IndexerID = "code_symbol_dag"

// ErrUnchanged indicates a file was skipped because its content hasn't changed.
var ErrUnchanged = errors.New("file unchanged")

// Indexer implements the indexing.Indexer interface for code symbols.
type Indexer struct {
	config        Config
	memoryStore   storage.MemoryStore
	registry      *ExtractorRegistry
	workspaceRoot string
	logger        zerolog.Logger
}

// NewIndexer creates a new symbol indexer.
func NewIndexer(
	cfg Config,
	memoryStore storage.MemoryStore,
	registry *ExtractorRegistry,
	workspaceRoot string,
	logger zerolog.Logger,
) *Indexer {
	if registry == nil {
		registry = DefaultRegistry()
	}
	return &Indexer{
		config:        cfg,
		memoryStore:   memoryStore,
		registry:      registry,
		workspaceRoot: workspaceRoot,
		logger:        logger.With().Str("indexer", IndexerID).Logger(),
	}
}

// ID returns the indexer identifier.
func (idx *Indexer) ID() string {
	return IndexerID
}

// Index processes a post-review event and updates symbol index.
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
			if err := idx.deleteFileSymbols(ctx, event.WorkspaceID, file.Path); err != nil {
				idx.logger.Warn().Err(err).Str("path", file.Path).Msg("failed to delete symbols")
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

		// Get the language
		lang := idx.detectLanguage(file)
		if lang == "" {
			idx.logger.Debug().Str("path", file.Path).Msg("unsupported language, skipping")
			result.FilesSkipped++
			continue
		}

		// Check if we have an extractor for this language
		extractor := idx.registry.Get(lang)
		if extractor == nil {
			idx.logger.Debug().Str("path", file.Path).Str("lang", lang).Msg("no extractor for language")
			result.FilesSkipped++
			continue
		}

		// Index the file
		if err := idx.indexFile(ctx, event, file, lang, extractor); err != nil {
			if errors.Is(err, ErrUnchanged) {
				// File content unchanged - count as skipped, not failed
				result.FilesSkipped++
				continue
			}
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
		Msg("symbol indexing completed")

	return result, nil
}

// indexFile indexes a single file's symbols.
func (idx *Indexer) indexFile(ctx context.Context, event indexing.PostReviewEvent, file indexing.FileChange, lang string, extractor Extractor) error {
	// Read file content
	content, err := idx.readFileContent(file.Path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	fileDigest := ComputeDigest(content)

	// Check file meta for unchanged files
	if !idx.fileChanged(ctx, event.WorkspaceID, file.Path, fileDigest) {
		idx.logger.Debug().Str("path", file.Path).Msg("file unchanged, skipping")
		return ErrUnchanged
	}

	// Extract symbols
	symbols, err := extractor.Extract(ctx, file.Path, content)
	if err != nil {
		return fmt.Errorf("extract symbols: %w", err)
	}

	if len(symbols) == 0 {
		idx.logger.Debug().Str("path", file.Path).Msg("no symbols found")
		return nil
	}

	// Index each symbol
	for _, sym := range symbols {
		sym.FileDigest = fileDigest
		sym.Language = lang

		// Extract calls for this symbol
		var calls []string
		if extractedCalls, extractErr := extractor.ExtractCalls(ctx, sym, content); extractErr != nil {
			idx.logger.Warn().
				Err(extractErr).
				Str("symbol", sym.ID).
				Str("path", file.Path).
				Msg("failed to extract calls, proceeding without call graph")
			// Continue with empty calls - symbol indexing should not fail due to call extraction
		} else {
			calls = extractedCalls
		}

		if err := idx.saveSymbol(ctx, event, sym, calls); err != nil {
			return fmt.Errorf("save symbol %s: %w", sym.Name, err)
		}
	}

	// Update file meta
	if err := idx.updateFileMeta(ctx, event.WorkspaceID, file.Path, fileDigest, len(symbols)); err != nil {
		return fmt.Errorf("update file meta: %w", err)
	}

	idx.logger.Debug().
		Str("path", file.Path).
		Int("symbols", len(symbols)).
		Msg("indexed file symbols")

	return nil
}

// saveSymbol saves a symbol to the memory store.
func (idx *Indexer) saveSymbol(ctx context.Context, event indexing.PostReviewEvent, sym Symbol, calls []string) error {
	result := Result{
		Symbol: sym,
		Source: &Source{
			TaskID:   event.TaskID,
			ReviewID: event.ReviewID,
			Actor:    "actor:system:symbol_indexer",
			Reason:   event.Reason,
		},
		Calls: calls,
	}

	resultBytes, err := MarshalResult(result)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}

	name := EntryName(event.WorkspaceID, sym.FilePath, sym.Name)
	entry := storage.NamedEntry{
		Name:      name,
		Type:      SymbolType,
		Workspace: event.WorkspaceID,
		Summary:   fmt.Sprintf("%s %s in %s", sym.Kind, sym.Name, sym.FilePath),
		Result:    resultBytes,
	}

	if _, err := idx.memoryStore.Save(ctx, entry); err != nil {
		return fmt.Errorf("save entry: %w", err)
	}

	return nil
}

// fileChanged checks if a file has changed since last indexing.
func (idx *Indexer) fileChanged(ctx context.Context, workspace, filePath, currentDigest string) bool {
	name := FileMetaEntryName(workspace, filePath)
	entry, err := idx.memoryStore.Get(ctx, name, workspace)
	if err != nil {
		// No previous meta = file is new
		return true
	}

	meta, err := UnmarshalFileMeta(entry.Result)
	if err != nil {
		return true
	}

	return meta.ContentHash != currentDigest
}

// updateFileMeta updates the file meta entry.
func (idx *Indexer) updateFileMeta(ctx context.Context, workspace, filePath, digest string, symbolCount int) error {
	meta := FileMeta{
		FilePath:    filePath,
		ContentHash: digest,
		Count:       symbolCount,
	}

	metaBytes, err := MarshalResult(meta)
	if err != nil {
		return err
	}

	name := FileMetaEntryName(workspace, filePath)
	entry := storage.NamedEntry{
		Name:      name,
		Type:      FileMetaType,
		Workspace: workspace,
		Summary:   fmt.Sprintf("Symbol meta for %s (%d symbols)", filePath, symbolCount),
		Result:    metaBytes,
	}

	_, err = idx.memoryStore.Save(ctx, entry)
	return err
}

// deleteFileSymbols removes all symbols for a file.
// Returns accumulated errors if any delete operations fail.
func (idx *Indexer) deleteFileSymbols(ctx context.Context, workspace, filePath string) error {
	var errs []error

	// Delete file meta
	metaName := FileMetaEntryName(workspace, filePath)
	if err := idx.memoryStore.Delete(ctx, metaName, workspace); err != nil {
		// Ignore not found errors
		if !errors.Is(err, memory.ErrNotFound) {
			idx.logger.Warn().Err(err).Str("path", filePath).Msg("failed to delete file meta")
			errs = append(errs, fmt.Errorf("delete file meta: %w", err))
		}
	}

	// Delete all symbol entries for this file
	// Symbols are named: symbol://<workspace>/<file_path>:<symbol_name>
	symbolPrefix := fmt.Sprintf("symbol://%s/%s:", workspace, filePath)
	deleted, err := idx.memoryStore.DeleteByNamePrefix(ctx, workspace, symbolPrefix)
	if err != nil {
		idx.logger.Warn().Err(err).Str("path", filePath).Msg("failed to delete symbol entries")
		errs = append(errs, fmt.Errorf("delete symbol entries: %w", err))
	} else if deleted > 0 {
		idx.logger.Debug().Str("path", filePath).Int("count", deleted).Msg("deleted symbol entries")
	}

	return errors.Join(errs...)
}

// detectLanguage detects the programming language from file extension or metadata.
func (idx *Indexer) detectLanguage(file indexing.FileChange) string {
	// Use provided language if available
	if file.Language != "" {
		return strings.ToLower(file.Language)
	}

	// Detect from extension
	ext := strings.ToLower(filepath.Ext(file.Path))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js":
		return "javascript"
	case ".ts":
		return "typescript"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".hpp":
		return "cpp"
	case ".rb":
		return "ruby"
	case ".gd":
		return "gdscript"
	default:
		return ""
	}
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
	defer func() { _ = f.Close() }()

	// Use LimitReader as additional safety even though we checked size
	return io.ReadAll(io.LimitReader(f, maxReadFileSize))
}
