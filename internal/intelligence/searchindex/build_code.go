package searchindex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/codefilter"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"
	"github.com/joshka0/foxctl/internal/platform/symbolutil"
	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/storage"
)

// BootstrapSource provides symbol and file summary entries for index bootstrap.
type BootstrapSource interface {
	ListByType(ctx context.Context, workspaceID, entryType string, limit int) ([]storage.NamedEntry, error)
}

// BuildCodeOptions controls index bootstrap behavior.
type BuildCodeOptions struct {
	// Limit caps per-type listing from source systems.
	Limit int

	// EmbedProvider optionally embeds built documents for vector recall.
	EmbedProvider semantic.EmbeddingProvider

	// EmbedBatchSize controls batched embedding request size.
	// When <= 0, a conservative default is used.
	EmbedBatchSize int

	// Progress, when set, receives batch-level progress updates.
	Progress func(BuildProgress)

	// EnvelopeProvider optionally enriches code documents with deterministic
	// semantic context supplied by a caller-owned provider.
	EnvelopeProvider CodeEnvelopeProvider

	// IncludeCoChangeNeighborsInEnvelope allows provider-supplied co-change
	// neighbors to enter embedding text. By default they remain metadata-only.
	IncludeCoChangeNeighborsInEnvelope bool
}

// BuildCodeResult summarizes bootstrap activity.
type BuildCodeResult struct {
	WorkspaceID   string
	SymbolFetched int
	FileFetched   int
	SymbolBuilt   int
	FileBuilt     int
	Upserted      int
	Skipped       int
	Errors        int
}

// BuildProgress reports batch-level progress during bootstrap.
type BuildProgress struct {
	Stage        string
	Batch        int
	TotalBatches int
	Docs         int
	Embedded     int
	Errors       int
}

// BuildCodeDocuments hydrates searchindex from the legacy symbol + file summary data.
//
// This is intentionally conservative and uses existing persisted outputs from
// indexing/symbol and indexing/filesummary as input.
func BuildCodeDocuments(ctx context.Context, source BootstrapSource, index Store, workspaceID string, opts BuildCodeOptions) (BuildCodeResult, error) {
	result := BuildCodeResult{WorkspaceID: workspace.CanonicalID(workspaceID)}
	if source == nil || index == nil {
		return result, fmt.Errorf("searchindex: build code docs: source and index are required")
	}

	symbolDocs, err := source.ListByType(ctx, result.WorkspaceID, symbol.SymbolType, opts.Limit)
	if err != nil {
		return result, fmt.Errorf("searchindex: list symbol entries: %w", err)
	}
	result.SymbolFetched = len(symbolDocs)

	fileDocs, err := source.ListByType(ctx, result.WorkspaceID, symbol.FileSummaryType, opts.Limit)
	if err != nil {
		return result, fmt.Errorf("searchindex: list file-summary entries: %w", err)
	}
	result.FileFetched = len(fileDocs)

	var symbolPending []Document
	for _, entry := range symbolDocs {
		doc, ok := documentFromSymbol(entry)
		if !ok {
			result.Errors++
			result.Skipped++
			continue
		}
		if codefilter.ShouldSkipPath(doc.Path) {
			result.Skipped++
			continue
		}
		if !documentExists(result.WorkspaceID, doc) {
			result.Skipped++
			continue
		}
		doc = enrichDocumentWithCodeEnvelope(ctx, opts, doc)
		symbolPending = append(symbolPending, doc)
	}

	built, errors := embedAndUpsertDocuments(ctx, index, opts, "symbols", symbolPending)
	result.SymbolBuilt += built
	result.Upserted += built
	result.Errors += errors

	var filePending []Document
	for _, entry := range fileDocs {
		doc, ok := documentFromFileSummary(result.WorkspaceID, entry)
		if !ok {
			result.Errors++
			result.Skipped++
			continue
		}
		if codefilter.ShouldSkipPath(doc.Path) {
			result.Skipped++
			continue
		}
		if !documentExists(result.WorkspaceID, doc) {
			result.Skipped++
			continue
		}
		doc = enrichDocumentWithCodeEnvelope(ctx, opts, doc)
		filePending = append(filePending, doc)
	}

	built, errors = embedAndUpsertDocuments(ctx, index, opts, "files", filePending)
	result.FileBuilt += built
	result.Upserted += built
	result.Errors += errors

	if result.Upserted == 0 && result.Errors > 0 {
		return result, fmt.Errorf("searchindex: bootstrap completed with errors")
	}
	return result, nil
}

func enrichDocumentWithCodeEnvelope(ctx context.Context, opts BuildCodeOptions, doc Document) Document {
	if opts.EnvelopeProvider == nil {
		return doc
	}
	bits, err := opts.EnvelopeProvider.BuildCodeEnvelope(ctx, CodeEnvelopeRequest{
		Document:                           doc,
		IncludeCoChangeNeighborsInEnvelope: opts.IncludeCoChangeNeighborsInEnvelope,
	})
	if err != nil {
		return doc
	}
	return applySemanticEnvelope(doc, bits, opts)
}

func documentFromSymbol(entry storage.NamedEntry) (Document, bool) {
	parsed, err := symbol.UnmarshalResult(entry.Result)
	if err != nil {
		return Document{}, false
	}
	if parsed.Symbol.FilePath == "" {
		return Document{}, false
	}
	workspaceID := workspace.CanonicalID(entry.Workspace)

	symbolKey := parsed.Symbol.EffectiveID()
	symbolID := symbolKey
	metadata := map[string]any{"symbol_key": symbolKey}
	if scoped, ok := symbolutil.ScopedSymbolIDFromKeyEntryName(workspaceID, entry.Name); ok {
		symbolID = scoped
		metadata["symbol_ref"] = scoped
	}
	searchText := encodeSearchText(
		parsed.Symbol.Name,
		string(parsed.Symbol.Kind),
		parsed.Symbol.Language,
		parsed.Symbol.Signature,
		parsed.Symbol.Documentation,
		parsed.Symbol.FilePath,
		entry.Summary,
	)
	return Document{
		ID:          symbolDocumentID(workspaceID, symbolID),
		WorkspaceID: workspaceID,
		Scope:       ScopeCode,
		Kind:        KindSymbol,
		GroupKey:    parsed.Symbol.FilePath,
		Path:        parsed.Symbol.FilePath,
		SymbolID:    symbolID,
		SymbolName:  parsed.Symbol.Name,
		Title:       parsed.Symbol.Name,
		Summary:     entry.Summary,
		SearchText:  searchText,
		Keywords: append([]string{
			parsed.Symbol.Name,
			string(parsed.Symbol.Kind),
			parsed.Symbol.Language,
			filepath.Base(parsed.Symbol.FilePath),
		}, parsed.Calls...),
		Anchor: Anchor{
			Type:      AnchorSymbol,
			Path:      parsed.Symbol.FilePath,
			StartLine: parsed.Symbol.StartLine,
			EndLine:   parsed.Symbol.EndLine,
			StartByte: parsed.Symbol.StartByte,
			EndByte:   parsed.Symbol.EndByte,
		},
		Metadata: metadata,
	}, true
}

func documentFromFileSummary(workspaceID string, entry storage.NamedEntry) (Document, bool) {
	parsed, err := symbol.UnmarshalFileSummaryResult(entry.Result)
	if err != nil {
		return Document{}, false
	}
	path := parsed.FilePath
	if path == "" {
		path = extractFilePathFromEntry(entry.Name)
	}
	if path == "" {
		return Document{}, false
	}

	searchText := encodeSearchText(
		path,
		parsed.Package,
		strings.Join(parsed.Symbols, " "),
		normString(entry.Summary),
	)
	return Document{
		ID:          fileDocumentID(workspaceID, path),
		WorkspaceID: workspaceID,
		Scope:       ScopeCode,
		Kind:        KindFile,
		GroupKey:    path,
		Path:        path,
		Title:       path,
		Summary:     entry.Summary,
		SearchText:  searchText,
		Keywords:    append([]string{filepath.Dir(path), parsed.Language, parsed.Package}, parsed.Symbols...),
		Anchor: Anchor{
			Type: AnchorLine,
			Path: path,
		},
	}, true
}

func documentExists(workspaceRoot string, doc Document) bool {
	if strings.TrimSpace(workspaceRoot) == "" || strings.TrimSpace(doc.Path) == "" {
		return true
	}
	if !filepath.IsAbs(workspaceRoot) {
		return true
	}
	if info, err := os.Stat(workspaceRoot); err != nil || !info.IsDir() {
		return true
	}
	path := doc.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspaceRoot, path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func symbolDocumentID(workspaceID, symbolID string) string {
	if symbolID == "" {
		return ""
	}
	return fmt.Sprintf("search://%s/symbol/%s", workspaceID, symbolID)
}

func fileDocumentID(workspaceID, path string) string {
	if path == "" {
		return ""
	}
	return fmt.Sprintf("search://%s/file/%s", workspaceID, path)
}

func extractFilePathFromEntry(entryName string) string {
	parts := strings.SplitN(entryName, "://", 2)
	if len(parts) < 2 {
		return ""
	}
	trimmed := parts[1]
	slash := strings.Index(trimmed, "/")
	if slash < 0 {
		return ""
	}
	trimmed = trimmed[slash+1:]

	firstSlash := strings.Index(trimmed, "/")
	if firstSlash < 0 {
		return ""
	}
	return trimmed[firstSlash+1:]
}

func normString(value string) string {
	return strings.TrimSpace(value)
}

type embeddingItem struct {
	docIndex int
	text     string
}

func embedAndUpsertDocuments(ctx context.Context, index Store, opts BuildCodeOptions, stage string, docs []Document) (built int, errors int) {
	items := embeddingItemsForDocuments(docs)
	if opts.EmbedProvider == nil || len(items) == 0 {
		return upsertDocuments(ctx, index, docs)
	}

	indexed := make([]bool, len(docs))
	batchSize := opts.EmbedBatchSize
	if batchSize <= 0 {
		batchSize = 32
	}
	totalBatches := (len(items) + batchSize - 1) / batchSize

	for batch := 0; batch < totalBatches; batch++ {
		start := batch * batchSize
		end := start + batchSize
		if end > len(items) {
			end = len(items)
		}
		chunk := items[start:end]
		errors += embedDocumentChunk(ctx, opts, stage, batch+1, totalBatches, chunk, docs)

		batchDocs := make([]Document, 0, len(chunk))
		for _, item := range chunk {
			batchDocs = append(batchDocs, docs[item.docIndex])
			indexed[item.docIndex] = true
		}
		batchBuilt, batchErrors := upsertDocuments(ctx, index, batchDocs)
		built += batchBuilt
		errors += batchErrors
	}

	var unembedded []Document
	for i, doc := range docs {
		if !indexed[i] {
			unembedded = append(unembedded, doc)
		}
	}
	unembeddedBuilt, unembeddedErrors := upsertDocuments(ctx, index, unembedded)
	built += unembeddedBuilt
	errors += unembeddedErrors
	return built, errors
}

func embeddingItemsForDocuments(docs []Document) []embeddingItem {
	items := make([]embeddingItem, 0, len(docs))
	for i := range docs {
		text := embeddingTextForDocument(docs[i])
		if text == "" {
			continue
		}
		items = append(items, embeddingItem{docIndex: i, text: text})
	}
	return items
}

func embedDocumentChunk(ctx context.Context, opts BuildCodeOptions, stage string, batch int, totalBatches int, chunk []embeddingItem, docs []Document) int {
	provider := opts.EmbedProvider
	if provider == nil || len(chunk) == 0 {
		return 0
	}

	texts := make([]string, len(chunk))
	for i := range chunk {
		texts[i] = chunk[i].text
	}
	model := provider.Model()
	if opts.Progress != nil {
		opts.Progress(BuildProgress{
			Stage:        stage,
			Batch:        batch,
			TotalBatches: totalBatches,
			Docs:         len(chunk),
		})
	}

	if embeddings, err := provider.EmbedBatch(ctx, texts); err == nil && len(embeddings) == len(chunk) {
		for i, embedding := range embeddings {
			docs[chunk[i].docIndex].Embedding = embedding
			docs[chunk[i].docIndex].EmbeddingModel = model
		}
		if opts.Progress != nil {
			opts.Progress(BuildProgress{
				Stage:        stage,
				Batch:        batch,
				TotalBatches: totalBatches,
				Docs:         len(chunk),
				Embedded:     len(chunk),
			})
		}
		return 0
	}

	errors := 0
	embedded := 0
	for _, item := range chunk {
		embedding, err := provider.Embed(ctx, item.text)
		if err != nil {
			errors++
			continue
		}
		docs[item.docIndex].Embedding = embedding
		docs[item.docIndex].EmbeddingModel = model
		embedded++
	}
	if opts.Progress != nil {
		opts.Progress(BuildProgress{
			Stage:        stage,
			Batch:        batch,
			TotalBatches: totalBatches,
			Docs:         len(chunk),
			Embedded:     embedded,
			Errors:       errors,
		})
	}
	return errors
}

func upsertDocuments(ctx context.Context, index Store, docs []Document) (built int, errors int) {
	for _, doc := range docs {
		if err := index.Upsert(ctx, doc); err != nil {
			errors++
			continue
		}
		built++
	}
	return built, errors
}

func embeddingTextForDocument(doc Document) string {
	var parts []string
	if doc.Title != "" {
		parts = append(parts, strings.TrimSpace(doc.Title))
	}
	if doc.SymbolName != "" && doc.SymbolName != doc.Title {
		parts = append(parts, strings.TrimSpace(doc.SymbolName))
	}
	if doc.Summary != "" {
		parts = append(parts, strings.TrimSpace(doc.Summary))
	}
	if doc.Path != "" {
		parts = append(parts, strings.TrimSpace(doc.Path))
	}
	if len(doc.Keywords) > 0 {
		parts = append(parts, strings.Join(doc.Keywords, " "))
	}
	for _, section := range envelopeTextSectionsFromMetadata(doc.Metadata) {
		parts = append(parts, section.Name+": "+section.Text)
	}
	if cochange := coChangeTextFromMetadata(doc.Metadata); cochange != "" {
		parts = append(parts, "cochange: "+cochange)
	}
	text := strings.TrimSpace(strings.Join(parts, "\n"))
	if text == "" {
		text = strings.TrimSpace(doc.SearchText)
	}
	switch doc.Kind {
	case KindSymbol:
		return truncateEmbeddingText(text, 1200)
	case KindFile:
		return truncateEmbeddingText(text, 1800)
	default:
		return truncateEmbeddingText(text, 1500)
	}
}

func truncateEmbeddingText(text string, max int) string {
	text = strings.TrimSpace(text)
	if max <= 0 || len(text) <= max {
		return text
	}
	return strings.TrimSpace(text[:max])
}
