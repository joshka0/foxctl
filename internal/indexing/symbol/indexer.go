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
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jkatigb/agentctl/internal/indexing"
	"github.com/jkatigb/agentctl/internal/indexing/codefilter"
	"github.com/jkatigb/agentctl/internal/indexing/embedding"
	"github.com/jkatigb/agentctl/internal/indexing/embeddingtext"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/platform/fsutil"
	platformsymbol "github.com/jkatigb/agentctl/internal/platform/symbolutil"
	workspaceutil "github.com/jkatigb/agentctl/internal/platform/workspace"
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
//
// Index:
// - Purpose: Update symbol and call indexes for post-review file changes
// - Flow: validate config → loop files → delete removed → detect language/extractor → indexFile → update counts → log summary
// - SideEffects: reads files; writes named memory entries; enqueues embedding jobs
// - FailureModes: file I/O errors, extractor errors, store save errors, embedding enqueue errors
// - Related: indexFile, deleteFileSymbols, enqueueEmbeddings, Extractor.Extract
// - Keywords: code_symbol_dag, post_review, files_indexed, files_skipped, files_failed, failures, embeddings, IndexerResult
func (idx *Indexer) Index(ctx context.Context, event indexing.PostReviewEvent) (*indexing.IndexerResult, error) {
	if !idx.config.Enabled {
		return &indexing.IndexerResult{
			IndexerID:    IndexerID,
			FilesSkipped: len(event.Files),
		}, nil
	}

	if canonicalWorkspace := workspaceutil.ID(idx.workspaceRoot); canonicalWorkspace != "" {
		if event.WorkspaceID == "" {
			event.WorkspaceID = canonicalWorkspace
		} else if event.WorkspaceID != canonicalWorkspace {
			idx.logger.Warn().
				Str("workspace_id", event.WorkspaceID).
				Str("canonical_workspace_id", canonicalWorkspace).
				Msg("overriding workspace id for symbol indexing")
			event.WorkspaceID = canonicalWorkspace
		}
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

		if codefilter.ShouldSkipPath(file.Path) {
			idx.logger.Debug().Str("path", file.Path).Msg("skipping non-app code path")
			result.FilesSkipped++
			continue
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
	pkg := platformsymbol.DeriveSymbolPackage(file.Path, lang)

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
	if !idx.config.Force && oldMeta != nil &&
		oldMeta.IndexSchema == CurrentFileMetaSchema &&
		oldMeta.ContentHash == fileDigest {
		idx.logger.Debug().Str("path", file.Path).Msg("file unchanged, skipping")
		return ErrUnchanged
	}

	// Extract symbols
	symbols, err := extractor.Extract(ctx, file.Path, content)
	if err != nil {
		return fmt.Errorf("extract symbols: %w", err)
	}

	// Assign stable SymbolKeys based on language
	for i := range symbols {
		if symbols[i].Key != "" {
			continue
		}
		switch lang {
		case "go":
			if symbols[i].Name == "init" {
				symbols[i].Key = GoInitSymbolKey(filepath.Base(symbols[i].FilePath))
			} else {
				r, _ := utf8.DecodeRuneInString(symbols[i].Name)
				if unicode.IsUpper(r) {
					symbols[i].Key = GoSymbolKey(symbols[i].Name)
				} else {
					symbols[i].Key = GoNonExportedSymbolKey(symbols[i].Name, filepath.Base(symbols[i].FilePath))
				}
			}
		case "typescript", "javascript":
			exported := strings.HasPrefix(strings.TrimSpace(symbols[i].Signature), "export ")
			symbols[i].Key = TSSymbolKey(symbols[i].Name, exported, filepath.Base(symbols[i].FilePath))
		case "elixir":
			symbols[i].Key = ElixirSymbolKey(symbols[i].Name)
		case "python":
			symbols[i].Key = PythonSymbolKey(symbols[i].Name)
		}
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
	embedMode := config.ResolveEmbedSymbolTextMode(idx.config.EmbeddingTextMode)
	useDocAwareDigest := embedMode == config.EmbedSymbolTextModeDocEnriched
	embedModel := ""
	if useDocAwareDigest {
		embedModel = idx.resolveEmbeddingModel(ctx)
	}

	// Track new symbol digests and IDs
	newDigests := make(map[string]string)
	newSymbolIDs := make(map[string]bool)
	nameToID := make(map[string]string)
	for _, sym := range symbols {
		nameToID[sym.Name] = sym.EffectiveID()
	}

	var savedCount, skippedCount int
	var embedInputs []embedding.SymbolInput

	// Index each symbol with per-symbol incrementality
	for _, sym := range symbols {
		sym.FileDigest = fileDigest
		sym.Language = lang
		symID := sym.EffectiveID()

		skipDigest := sym.BodyDigest
		if useDocAwareDigest {
			skipDigest = embeddingtext.BuildSymbolContentDigest(embeddingtext.SymbolDigestInput{
				Model:      embedModel,
				Kind:       string(sym.Kind),
				Name:       sym.Name,
				SymbolKey:  sym.EffectiveID(),
				FilePath:   sym.FilePath,
				Signature:  sym.Signature,
				Doc:        sym.Documentation,
				BodyDigest: sym.BodyDigest,
			})
		}
		newDigests[symID] = skipDigest
		newSymbolIDs[symID] = true

		// Per-symbol incremental check per spec §4.3:
		// If existing symbol has identical body_digest, skip save (reuse embedding)
		if !idx.config.Force {
			if oldDigest, exists := oldDigests[symID]; exists && oldDigest == skipDigest {
				skippedCount++
				continue
			}
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

		if err := idx.saveSymbol(ctx, event, pkg, sym, calls); err != nil {
			return fmt.Errorf("save symbol %s: %w", sym.Name, err)
		}
		savedCount++

		if idx.config.EmbeddingEnabled {
			content, digest := idx.buildEmbeddingPayload(sym, calls, content, embedMode, embedModel)
			if strings.TrimSpace(content) != "" {
				embedInputs = append(embedInputs, embedding.SymbolInput{
					SymbolID:      symID,
					FilePath:      sym.FilePath,
					SymbolName:    sym.Name,
					Content:       content,
					ContentDigest: digest,
				})
			}
		}
	}

	// Delete symbols that no longer exist in the file per spec §4.3
	var deletedCount int
	for oldID := range oldDigests {
		if !newSymbolIDs[oldID] {
			// Symbol was removed - delete its entry
			if err := idx.deleteSymbol(ctx, event.WorkspaceID, file.Path, oldID, pkg); err != nil {
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

	if idx.config.EmbeddingEnabled && len(embedInputs) > 0 {
		if err := idx.enqueueEmbeddings(ctx, event.WorkspaceID, embedInputs, embedModel); err != nil {
			idx.logger.Warn().
				Err(err).
				Str("path", file.Path).
				Int("queued", len(embedInputs)).
				Msg("failed to enqueue symbol embeddings")
		}
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

func (idx *Indexer) buildEmbeddingPayload(sym Symbol, calls []string, fileContent []byte, mode config.EmbedSymbolTextMode, model string) (string, string) {
	body := extractSymbolBody(fileContent, sym)
	if strings.TrimSpace(body) == "" {
		return "", ""
	}

	switch mode {
	case config.EmbedSymbolTextModeDocEnriched:
		aliases := embeddingtext.BuildSymbolAliases(embeddingtext.SymbolInfo{
			Name:     sym.Name,
			FilePath: sym.FilePath,
			Package:  filepath.ToSlash(filepath.Dir(sym.FilePath)),
		})
		info := embeddingtext.SymbolInfo{
			Name:      sym.Name,
			Kind:      string(sym.Kind),
			Package:   filepath.ToSlash(filepath.Dir(sym.FilePath)),
			FilePath:  sym.FilePath,
			Signature: sym.Signature,
			Doc:       sym.Documentation,
			Code:      body,
			Calls:     calls,
			Aliases:   aliases,
		}
		content := embeddingtext.BuildSymbolEmbeddingText(info, embeddingtext.DefaultSymbolTextOptionsDocEnriched())
		digest := embeddingtext.BuildSymbolContentDigest(embeddingtext.SymbolDigestInput{
			Model:      model,
			Kind:       string(sym.Kind),
			Name:       sym.Name,
			SymbolKey:  sym.EffectiveID(),
			FilePath:   sym.FilePath,
			Signature:  sym.Signature,
			Doc:        sym.Documentation,
			BodyDigest: sym.BodyDigest,
			Calls:      calls,
			Aliases:    aliases,
		})
		return content, digest
	default:
		content := body
		return content, embeddingtext.DigestSHA256(content)
	}
}

func (idx *Indexer) enqueueEmbeddings(ctx context.Context, workspaceID string, inputs []embedding.SymbolInput, model string) error {
	root, err := idx.resolveEmbeddingStoreRoot(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(root) == "" {
		return nil
	}

	store, err := embedding.OpenStore(ctx, root)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			idx.logger.Warn().Err(closeErr).Msg("failed to close embedding store")
		}
	}()

	_, err = store.Enqueue(ctx, embedding.EnqueueRequest{
		WorkspaceID: workspaceID,
		Symbols:     inputs,
		Priority:    embedding.PriorityNormal,
		Model:       model,
		Deduplicate: true,
	})
	return err
}

func (idx *Indexer) resolveEmbeddingStoreRoot(ctx context.Context) (string, error) {
	root := strings.TrimSpace(idx.config.EmbeddingStoreRoot)
	if root != "" {
		return root, nil
	}
	cfg, err := config.LoadCached(ctx)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(cfg.Paths.Cache), nil
}

func (idx *Indexer) resolveEmbeddingModel(ctx context.Context) string {
	model := strings.TrimSpace(idx.config.EmbeddingModel)
	if model != "" {
		return model
	}
	cfg, err := config.LoadCached(ctx)
	if err != nil {
		recommended, _ := semantic.ScopeModelRecommendation(semantic.ScopeSymbols)
		return strings.TrimSpace(recommended)
	}
	return semantic.ResolveModelForScope(semantic.ScopeSymbols, cfg)
}

func extractSymbolBody(content []byte, sym Symbol) string {
	if len(content) == 0 {
		return ""
	}

	start := sym.StartByte
	end := sym.EndByte
	if start >= 0 && end > start && end <= len(content) {
		return string(content[start:end])
	}

	lines := strings.Split(string(content), "\n")
	startLine := sym.StartLine
	endLine := sym.EndLine
	if startLine < 1 || startLine > len(lines) {
		return ""
	}
	if endLine < startLine || endLine > len(lines) {
		endLine = startLine
	}

	return strings.Join(lines[startLine-1:endLine], "\n")
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
			candidates = append(candidates, candidate{id: res.Symbol.EffectiveID(), filePath: res.Symbol.FilePath})
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
func (idx *Indexer) deleteSymbol(ctx context.Context, workspace, filePath, symbolID, pkg string) error {
	var errs []error

	// Delete key-based entry
	keyName := platformsymbol.KeyEntryName(workspace, pkg, symbolID)
	if err := idx.memoryStore.Delete(ctx, keyName, workspace); err != nil {
		// Not fatal - may not exist in key format yet
		idx.logger.Debug().Err(err).Str("name", keyName).Msg("key-based entry not found")
	}

	// Delete legacy file-path-based entry
	if legacyFile, legacyName, ok := splitLegacySymbolID(symbolID); ok {
		name := EntryName(workspace, legacyFile, legacyName)
		if err := idx.memoryStore.Delete(ctx, name, workspace); err != nil {
			idx.logger.Debug().Err(err).Str("name", name).Msg("legacy entry not found")
		}
	} else if filePath != "" {
		// symbolID is a SymbolKey (no ":" separator) — use the provided filePath
		// to construct the legacy entry name for backward-compat cleanup.
		symName := SymbolKey(symbolID).Name()
		// init@filename.go keys were stored with legacy name "init", not "init@filename.go"
		if strings.HasPrefix(symbolID, "init@") {
			symName = "init"
		}
		name := EntryName(workspace, filePath, symName)
		if err := idx.memoryStore.Delete(ctx, name, workspace); err != nil {
			idx.logger.Debug().Err(err).Str("name", name).Msg("legacy entry not found")
		}
	}

	// Always try to clean up call edges
	errs = append(errs, idx.deleteOutgoingCallEdges(ctx, workspace, symbolID))
	return errors.Join(errs...)
}

// splitLegacySymbolID attempts to split a symbol ID in legacy "filePath:symbolName" format.
func splitLegacySymbolID(symbolID string) (string, string, bool) {
	parts := strings.SplitN(symbolID, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// saveSymbol saves a symbol to the memory store.
func (idx *Indexer) saveSymbol(ctx context.Context, event indexing.PostReviewEvent, pkg string, sym Symbol, calls []string) error {
	if err := idx.saveCallEdges(ctx, event.WorkspaceID, sym.EffectiveID(), calls); err != nil {
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

	// Primary entry: key-based name (stable across file moves)
	primaryName := platformsymbol.KeyEntryName(event.WorkspaceID, pkg, sym.EffectiveID())
	entry := storage.NamedEntry{
		Name:      primaryName,
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
		LastModTime:   time.Now().Unix(),
		IndexSchema:   CurrentFileMetaSchema,
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
	lang := normalizeLanguageHint(fsutil.DetectLanguage(filePath))
	pkg := platformsymbol.DeriveSymbolPackage(filePath, lang)
	var errs []error

	// Try to delete using stored FileMeta digests (key-based IDs)
	if oldMeta := idx.loadFileMeta(ctx, workspace, filePath); oldMeta != nil {
		for oldID := range oldMeta.SymbolDigests {
			if err := idx.deleteSymbol(ctx, workspace, filePath, oldID, pkg); err != nil {
				idx.logger.Debug().Err(err).Str("id", oldID).Msg("failed to delete symbol by ID")
			}
		}
	}

	// Delete file meta
	metaName := FileMetaEntryName(workspace, filePath)
	if err := idx.memoryStore.Delete(ctx, metaName, workspace); err != nil {
		// Ignore not found errors
		if !errors.Is(err, memory.ErrNotFound) {
			idx.logger.Warn().Err(err).Str("path", filePath).Msg("failed to delete file meta")
			errs = append(errs, fmt.Errorf("delete file meta: %w", err))
		}
	}

	// Best-effort legacy prefix cleanup
	symbolPrefix := fmt.Sprintf("symbol://%s/%s:", workspace, filePath)
	deleted, err := idx.memoryStore.DeleteByNamePrefix(ctx, workspace, symbolPrefix)
	if err != nil {
		idx.logger.Warn().Err(err).Str("path", filePath).Msg("failed to delete legacy symbol entries")
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
		lang := normalizeLanguageHint(strings.ToLower(strings.TrimSpace(file.Language)))
		if lang == "" {
			return ""
		}
		if !idx.isLanguageAllowed(lang) {
			return ""
		}
		return lang
	}

	// Detect from extension (shared mapping used across the codebase)
	lang := fsutil.DetectLanguage(file.Path)
	if lang == "text" {
		return ""
	}
	lang = normalizeLanguageHint(lang)
	if lang == "" {
		return ""
	}
	if !idx.isLanguageAllowed(lang) {
		return ""
	}
	return lang
}

func (idx *Indexer) isLanguageAllowed(lang string) bool {
	if lang == "" {
		return false
	}
	if len(idx.config.Languages) == 0 {
		return true
	}
	for _, raw := range idx.config.Languages {
		filter := normalizeLanguageHint(strings.ToLower(strings.TrimSpace(raw)))
		if filter == "" {
			continue
		}
		if filter == lang {
			return true
		}
	}
	return false
}

func normalizeLanguageHint(hint string) string {
	switch strings.ToLower(strings.TrimSpace(hint)) {
	case "":
		return ""
	case "go":
		return "go"
	case "python", "py":
		return "python"
	case "typescript", "ts", "tsx":
		return "typescript"
	case "javascript", "js", "jsx":
		return "javascript"
	case "elixir", "ex", "exs":
		return "elixir"
	default:
		return strings.ToLower(strings.TrimSpace(hint))
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
