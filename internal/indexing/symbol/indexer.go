package symbol

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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

// indexFile indexes a single file's symbols with per-symbol incremental updates.
// Per spec §4.3: unchanged body_digest means embeddings can be reused.
func (idx *Indexer) indexFile(ctx context.Context, event indexing.PostReviewEvent, file indexing.FileChange, lang string, extractor Extractor) error {
	// Read file content
	content, err := idx.readFileContent(file.Path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	// Check large-file thresholds per spec §4.2
	if idx.config.MaxFileKB > 0 && len(content) > idx.config.MaxFileKB*1024 {
		idx.logger.Debug().
			Str("path", file.Path).
			Int("size_kb", len(content)/1024).
			Int("max_kb", idx.config.MaxFileKB).
			Msg("file exceeds size limit, skipping")
		// Use ErrUnchanged as a generic "skipped" sentinel so the caller can
		// treat this as skipped (not indexed, not failed).
		return ErrUnchanged
	}

	fileDigest := ComputeDigest(content)

	// Load existing file meta for per-symbol comparison
	oldMeta := idx.loadFileMeta(ctx, event.WorkspaceID, file.Path)

	// Check file-level freshness first
	if oldMeta != nil && oldMeta.ContentHash == fileDigest {
		idx.logger.Debug().Str("path", file.Path).Msg("file unchanged, skipping")
		return ErrUnchanged
	}

	// Extract symbols
	symbols, err := extractor.Extract(ctx, file.Path, content)
	if err != nil {
		return fmt.Errorf("extract symbols: %w", err)
	}

	// Check LOC limit for large files per spec §4.2
	if idx.config.MaxFileLOC > 0 && len(symbols) == 0 {
		// Count lines for empty-symbol files to decide if we should skip
		lineCount := countLines(content)
		if lineCount > idx.config.MaxFileLOC {
			idx.logger.Debug().
				Str("path", file.Path).
				Int("lines", lineCount).
				Int("max_loc", idx.config.MaxFileLOC).
				Msg("file exceeds LOC limit with no extractable symbols, skipping")
			// Signal skip to the caller via ErrUnchanged sentinel.
			return ErrUnchanged
		}
	}

	if len(symbols) == 0 {
		idx.logger.Debug().Str("path", file.Path).Msg("no symbols found")
		// Still update meta to mark file as processed
		if err := idx.updateFileMetaFull(ctx, event.WorkspaceID, file.Path, fileDigest, 0, nil); err != nil {
			return fmt.Errorf("update file meta: %w", err)
		}
		return nil
	}

	// Build map of old symbol digests for per-symbol incremental comparison
	oldDigests := make(map[string]string)
	if oldMeta != nil && oldMeta.SymbolDigests != nil {
		oldDigests = oldMeta.SymbolDigests
	}

	// Track new symbol digests and IDs
	newDigests := make(map[string]string)
	newSymbolIDs := make(map[string]bool)
	nameToID := make(map[string]string)
	for _, sym := range symbols {
		nameToID[sym.Name] = sym.ID
	}

	var savedCount, skippedCount int

	// Index each symbol with per-symbol incrementality
	for _, sym := range symbols {
		sym.FileDigest = fileDigest
		sym.Language = lang

		newDigests[sym.ID] = sym.BodyDigest
		newSymbolIDs[sym.ID] = true

		// Per-symbol incremental check per spec §4.3:
		// If existing symbol has identical body_digest, skip save (reuse embedding)
		if oldDigest, exists := oldDigests[sym.ID]; exists && oldDigest == sym.BodyDigest {
			skippedCount++
			continue
		}

		// Extract calls for this symbol
		var calls []string
		if extractedCalls, extractErr := extractor.ExtractCalls(ctx, sym, content); extractErr != nil {
			idx.logger.Warn().
				Err(extractErr).
				Str("symbol", sym.ID).
				Str("path", file.Path).
				Msg("failed to extract calls, proceeding without call graph")
		} else {
			calls = idx.resolveCallTargets(ctx, event.WorkspaceID, extractedCalls, nameToID)
		}

		if err := idx.saveSymbol(ctx, event, sym, calls); err != nil {
			return fmt.Errorf("save symbol %s: %w", sym.Name, err)
		}
		savedCount++
	}

	// Delete symbols that no longer exist in the file per spec §4.3
	var deletedCount int
	for oldID := range oldDigests {
		if !newSymbolIDs[oldID] {
			// Symbol was removed - delete its entry
			if err := idx.deleteSymbol(ctx, event.WorkspaceID, file.Path, oldID); err != nil {
				idx.logger.Warn().
					Err(err).
					Str("symbol_id", oldID).
					Str("path", file.Path).
					Msg("failed to delete removed symbol")
			} else {
				deletedCount++
			}
		}
	}

	// Update file meta with new symbol digests
	if err := idx.updateFileMetaFull(ctx, event.WorkspaceID, file.Path, fileDigest, len(symbols), newDigests); err != nil {
		return fmt.Errorf("update file meta: %w", err)
	}

	idx.logger.Debug().
		Str("path", file.Path).
		Int("total", len(symbols)).
		Int("saved", savedCount).
		Int("skipped", skippedCount).
		Int("deleted", deletedCount).
		Msg("indexed file symbols")

	return nil
}

func (idx *Indexer) resolveCallTargets(ctx context.Context, workspace string, callNames []string, nameToID map[string]string) []string {
	if len(callNames) == 0 {
		return []string{}
	}
	seen := make(map[string]bool)
	var out []string
	for _, callName := range callNames {
		callName = strings.TrimSpace(callName)
		if callName == "" {
			continue
		}
		if id, ok := nameToID[callName]; ok {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
			continue
		}

		// Best-effort cross-file resolution by symbol name.
		results, err := idx.memoryStore.Search(ctx, workspace, ":"+callName, 20)
		if err != nil {
			idx.logger.Debug().
				Err(err).
				Str("workspace", workspace).
				Str("call_name", callName).
				Msg("failed to search for call target, proceeding without resolution")
			continue
		}
		type candidate struct {
			id       string
			filePath string
		}
		var candidates []candidate
		for _, scored := range results {
			entry := scored.Entry
			if entry.Type != SymbolType {
				continue
			}
			res, parseErr := UnmarshalResult(entry.Result)
			if parseErr != nil {
				continue
			}
			if res.Symbol.Name != callName {
				continue
			}
			candidates = append(candidates, candidate{id: res.Symbol.ID, filePath: res.Symbol.FilePath})
		}
		if len(candidates) == 0 {
			continue
		}
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].filePath != candidates[j].filePath {
				return candidates[i].filePath < candidates[j].filePath
			}
			return candidates[i].id < candidates[j].id
		})
		chosen := candidates[0].id
		if chosen != "" && !seen[chosen] {
			seen[chosen] = true
			out = append(out, chosen)
		}
	}
	if out == nil {
		return []string{}
	}
	return out
}

// countLines counts the number of lines in content.
func countLines(content []byte) int {
	if len(content) == 0 {
		return 0
	}
	count := 1
	for _, b := range content {
		if b == '\n' {
			count++
		}
	}
	return count
}

// loadFileMeta loads the file meta entry, returning nil if not found.
func (idx *Indexer) loadFileMeta(ctx context.Context, workspace, filePath string) *FileMeta {
	name := FileMetaEntryName(workspace, filePath)
	entry, err := idx.memoryStore.Get(ctx, name, workspace)
	if err != nil {
		return nil
	}

	meta, err := UnmarshalFileMeta(entry.Result)
	if err != nil {
		return nil
	}
	return meta
}

// deleteSymbol deletes a single symbol entry.
func (idx *Indexer) deleteSymbol(ctx context.Context, workspace, filePath, symbolID string) error {
	// Extract symbol name from ID (format: "file_path:symbol_name")
	parts := strings.SplitN(symbolID, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid symbol ID format: %s", symbolID)
	}
	symbolName := parts[1]

	name := EntryName(workspace, filePath, symbolName)
	if err := idx.memoryStore.Delete(ctx, name, workspace); err != nil {
		return err
	}
	return idx.deleteOutgoingCallEdges(ctx, workspace, symbolID)
}

// saveSymbol saves a symbol to the memory store.
func (idx *Indexer) saveSymbol(ctx context.Context, event indexing.PostReviewEvent, sym Symbol, calls []string) error {
	if err := idx.saveCallEdges(ctx, event.WorkspaceID, sym.ID, calls); err != nil {
		return fmt.Errorf("save call edges: %w", err)
	}

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

func (idx *Indexer) saveCallEdges(ctx context.Context, workspace, sourceID string, targets []string) error {
	if err := idx.deleteOutgoingCallEdges(ctx, workspace, sourceID); err != nil {
		return err
	}

	seen := make(map[string]bool)
	for _, targetID := range targets {
		targetID = strings.TrimSpace(targetID)
		if targetID == "" {
			continue
		}
		if seen[targetID] {
			continue
		}
		seen[targetID] = true

		edge := CallEdge{
			SourceID: sourceID,
			TargetID: targetID,
			Count:    1,
		}
		b, err := MarshalResult(edge)
		if err != nil {
			return err
		}
		name := callEdgeEntryName(workspace, sourceID, targetID)
		_, err = idx.memoryStore.Save(ctx, storage.NamedEntry{
			Name:      name,
			Type:      CallEdgeType,
			Workspace: workspace,
			Summary:   fmt.Sprintf("call edge %s -> %s", sourceID, targetID),
			Result:    b,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (idx *Indexer) deleteOutgoingCallEdges(ctx context.Context, workspace, sourceID string) error {
	prefix := fmt.Sprintf("call://%s/%s->", workspace, sourceID)
	_, err := idx.memoryStore.DeleteByNamePrefix(ctx, workspace, prefix)
	return err
}

// updateFileMetaFull updates the file meta entry with full symbol digest tracking.
// This enables per-symbol incremental updates per spec §4.3.
func (idx *Indexer) updateFileMetaFull(ctx context.Context, workspace, filePath, digest string, symbolCount int, symbolDigests map[string]string) error {
	meta := FileMeta{
		FilePath:      filePath,
		ContentHash:   digest,
		Count:         symbolCount,
		SymbolDigests: symbolDigests,
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
	case ".ts", ".tsx":
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
	defer func() {
		// File cleanup in defer; error is not actionable.
		_ = f.Close() //nolint:errcheck
	}()

	// Use LimitReader as additional safety even though we checked size
	return io.ReadAll(io.LimitReader(f, maxReadFileSize))
}
