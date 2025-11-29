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
		return nil
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
		calls, _ := extractor.ExtractCalls(ctx, sym, content)

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
func (idx *Indexer) deleteFileSymbols(ctx context.Context, workspace, filePath string) error {
	// Delete file meta
	metaName := FileMetaEntryName(workspace, filePath)
	if err := idx.memoryStore.Delete(ctx, metaName, workspace); err != nil {
		// Ignore not found errors
		if !errors.Is(err, memory.ErrNotFound) {
			idx.logger.Warn().Err(err).Str("path", filePath).Msg("failed to delete file meta")
		}
	}

	// TODO: Delete all symbol entries for this file
	// This requires listing symbols by file path, which may need a different approach
	// For now, orphaned symbols will be cleaned up on next full reindex

	return nil
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

// readFileContent reads file content from the workspace.
func (idx *Indexer) readFileContent(path string) ([]byte, error) {
	fullPath := filepath.Join(idx.workspaceRoot, path)
	f, err := os.Open(fullPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	return io.ReadAll(f)
}
