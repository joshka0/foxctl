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
		symbolPending = append(symbolPending, doc)
	}

	result.Errors += embedDocuments(ctx, opts, "symbols", symbolPending)
	for _, doc := range symbolPending {
		if err := index.Upsert(ctx, doc); err != nil {
			result.Errors++
			continue
		}
		result.SymbolBuilt++
		result.Upserted++
	}

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
		filePending = append(filePending, doc)
	}

	result.Errors += embedDocuments(ctx, opts, "files", filePending)
	for _, doc := range filePending {
		if err := index.Upsert(ctx, doc); err != nil {
			result.Errors++
			continue
		}
		result.FileBuilt++
		result.Upserted++
	}

	if result.Upserted == 0 && result.Errors > 0 {
		return result, fmt.Errorf("searchindex: bootstrap completed with errors")
	}
	return result, nil
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

	symbolID := parsed.Symbol.EffectiveID()
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

func embedDocuments(ctx context.Context, opts BuildCodeOptions, stage string, docs []Document) int {
	provider := opts.EmbedProvider
	if provider == nil || len(docs) == 0 {
		return 0
	}

	type item struct {
		docIndex int
		text     string
	}
	items := make([]item, 0, len(docs))
	for i := range docs {
		text := embeddingTextForDocument(docs[i])
		if text == "" {
			continue
		}
		items = append(items, item{docIndex: i, text: text})
	}
	if len(items) == 0 {
		return 0
	}

	batchSize := opts.EmbedBatchSize
	if batchSize <= 0 {
		batchSize = 32
	}
	totalBatches := (len(items) + batchSize - 1) / batchSize
	errors := 0
	model := provider.Model()

	for batch := 0; batch < totalBatches; batch++ {
		start := batch * batchSize
		end := start + batchSize
		if end > len(items) {
			end = len(items)
		}
		chunk := items[start:end]
		texts := make([]string, len(chunk))
		for i := range chunk {
			texts[i] = chunk[i].text
		}
		if opts.Progress != nil {
			opts.Progress(BuildProgress{
				Stage:        stage,
				Batch:        batch + 1,
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
					Batch:        batch + 1,
					TotalBatches: totalBatches,
					Docs:         len(chunk),
					Embedded:     len(chunk),
				})
			}
			continue
		}

		batchErrors := 0
		embedded := 0
		for _, item := range chunk {
			embedding, err := provider.Embed(ctx, item.text)
			if err != nil {
				errors++
				batchErrors++
				continue
			}
			docs[item.docIndex].Embedding = embedding
			docs[item.docIndex].EmbeddingModel = model
			embedded++
		}
		if opts.Progress != nil {
			opts.Progress(BuildProgress{
				Stage:        stage,
				Batch:        batch + 1,
				TotalBatches: totalBatches,
				Docs:         len(chunk),
				Embedded:     embedded,
				Errors:       batchErrors,
			})
		}
	}
	return errors
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
