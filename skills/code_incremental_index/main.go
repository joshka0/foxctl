// Package main implements the code/incremental_index skill.
//
// This skill indexes a single file's symbols into the memory store for fast
// code search. It's designed to be called by hooks (e.g., PostToolUse on Edit)
// for live indexing during development.
//
// Additionally, it ingests call and import relationships into the graph store
// for PageRank-boosted code search per the dependency graph design.
package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/langutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/codefilter"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedding"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/embeddingtext"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"
	"github.com/joshka0/foxctl/internal/platform/config"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
	"github.com/joshka0/foxctl/internal/platform/symbolutil"
	ws "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/graph"
	"github.com/joshka0/foxctl/internal/storage/memory"
)

// Command is the skill command name.
const Command = "code/incremental_index"

// input matches the skill input specification for incremental file indexing with multiple processing options.
type input struct {
	File        string `json:"file"`
	WorkspaceID string `json:"workspace_id"`
	Symbols     *bool  `json:"symbols"`
	Embed       bool   `json:"embed"`
	EmbedQueue  bool   `json:"embed_queue"`
}

// output contains the skill result data with indexing statistics and timing information for performance tracking.
type output struct {
	File             string `json:"file"`
	Language         string `json:"language"`
	SymbolsExtracted int    `json:"symbols_extracted"`
	SymbolsUpdated   int    `json:"symbols_updated"`
	SymbolsDeleted   int    `json:"symbols_deleted"`
	EmbeddingQueued  int    `json:"embedding_queued"`
	EmbeddingSkipped int    `json:"embedding_skipped,omitempty"`
	CallEdgesCreated int    `json:"call_edges_created,omitempty"`
	ImportEdges      int    `json:"import_edges,omitempty"`
	DurationMS       int64  `json:"duration_ms"`
	Skipped          bool   `json:"skipped,omitempty"`
	SkipReason       string `json:"skip_reason,omitempty"`
}

// main is the skill entry point for code/incremental_index with live file indexing capabilities.
func main() {
	skillmain.Main(Command, run)
}

// run orchestrates incremental file indexing with symbol extraction, embedding queuing, and graph edge ingestion.
//
// Index:
//
//	Purpose: Index individual files for live code search with symbol extraction, embedding, and graph relationships
//	Flow: resolve path → detect language → extract symbols → upsert to memory store → queue embeddings → ingest graph edges
//	SideEffects: updates memory store; queues embedding jobs; updates graph store with call/import relationships
//	FailureModes: file access errors, parsing failures, storage errors, unsupported languages
//	Observability: emits indexing statistics, timing metrics, and skip reasons for unsupported files
//	Related: extractSymbols, upsertSymbols, queueEmbeddings, ingestGraphEdges
//	Keywords: code/incremental_index, file_indexing, symbol_extraction, embedding_queue, graph_ingestion, live_indexing
//
// [[domain:incremental-code-indexing]]
// [[protocol:live-symbol-indexing]]
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Apply defaults not handled by skillmain
	if in.WorkspaceID == "" {
		in.WorkspaceID = ws.ID(rc.PathValidator.Workspace())
	}
	if in.Symbols == nil {
		t := true
		in.Symbols = &t
	}
	start := time.Now()

	// Resolve file path
	workspace := rc.PathValidator.Workspace()
	absPath, err := skillmain.ValidatePath(rc, in.File)
	if err != nil {
		return err
	}

	// Get relative path for storage
	relPath, err := filepath.Rel(workspace, absPath)
	if err != nil {
		relPath = in.File
	}
	relPath = filepath.ToSlash(relPath)
	if codefilter.ShouldSkipPath(relPath) {
		return emit(rc, output{
			File:       relPath,
			Skipped:    true,
			SkipReason: "non-app code path",
			DurationMS: time.Since(start).Milliseconds(),
		})
	}

	// Detect language
	lang := langutil.DetectAllowed(absPath, langutil.CommonCodeLanguages)
	if lang == "" {
		return emit(rc, output{
			File:       relPath,
			Skipped:    true,
			SkipReason: "unsupported file type",
			DurationMS: time.Since(start).Milliseconds(),
		})
	}

	// Read file content
	content, err := os.ReadFile(absPath)
	if err != nil {
		return skillerr.WrapIO("read file", err)
	}

	// Skip large files (>512KB)
	if len(content) > 512*1024 {
		return emit(rc, output{
			File:       relPath,
			Language:   lang,
			Skipped:    true,
			SkipReason: "file too large (>512KB)",
			DurationMS: time.Since(start).Milliseconds(),
		})
	}

	// Compute content hash for unchanged-file skip.
	contentHash := fmt.Sprintf("%x", sha256.Sum256(content))

	// Open memory store early to check content hash (uses Storage.Root for persistent data)
	store, err := memory.OpenWithConfig(ctx, rc.Config)
	if err != nil {
		return skillerr.WrapIO("open memory store", err)
	}
	defer func() { errs.Ignore(store.Close(), "close memory store") }()

	// Skip if content hash unchanged since last index.
	if !in.Embed && contentUnchanged(ctx, store, in.WorkspaceID, relPath, contentHash) {
		return emit(rc, output{
			File:       relPath,
			Language:   lang,
			Skipped:    true,
			SkipReason: "content unchanged",
			DurationMS: time.Since(start).Milliseconds(),
		})
	}

	// Extract symbols if enabled
	var symbols []symbol.Symbol
	if *in.Symbols {
		symbols, err = extractSymbols(ctx, lang, relPath, content)
		if err != nil {
			// Log but don't fail - partial indexing is acceptable
			symbols = nil
		} else {
			setSymbolKeys(symbols, lang)
		}
	}

	// Upsert symbols and delete stale ones
	updated, deleted, err := upsertSymbols(ctx, store, in.WorkspaceID, relPath, lang, rc.SessionID, symbols)
	if err != nil {
		return skillerr.WrapIO("upsert symbols", err)
	}

	// Persist content hash for future unchanged-file skip
	if updated > 0 || deleted > 0 {
		storeContentHash(ctx, store, in.WorkspaceID, relPath, contentHash)
	}

	// Queue embeddings for updated symbols
	var embeddingQueued, embeddingSkipped int
	if in.EmbedQueue && len(symbols) > 0 {
		symbolTextMode := config.ResolveEmbedSymbolTextMode(rc.Config.Embedding.Flags.SymbolTextMode)
		embeddingModel := semantic.ResolveModelForScope(semantic.ScopeSymbols, rc.Config)
		embeddingQueued, embeddingSkipped = queueEmbeddings(ctx, rc.Config.Paths.Cache, in.WorkspaceID, symbols, content, symbolTextMode, embeddingModel)
	}

	// Ingest calls and imports into graph store for PageRank
	var callEdgesCreated, importEdgesCreated int
	if len(symbols) > 0 && lang == "go" {
		callEdgesCreated, importEdgesCreated = ingestGraphEdges(ctx, rc.Config.Storage.Root, in.WorkspaceID, relPath, symbols, content)
	}

	return emit(rc, output{
		File:             relPath,
		Language:         lang,
		SymbolsExtracted: len(symbols),
		SymbolsUpdated:   updated,
		SymbolsDeleted:   deleted,
		EmbeddingQueued:  embeddingQueued,
		EmbeddingSkipped: embeddingSkipped,
		CallEdgesCreated: callEdgesCreated,
		ImportEdges:      importEdgesCreated,
		DurationMS:       time.Since(start).Milliseconds(),
	})
}

// extractSymbols extracts code symbols from the file content using language-specific extractors with fallback support.
func extractSymbols(ctx context.Context, lang, filePath string, content []byte) ([]symbol.Symbol, error) {
	switch lang {
	case "go":
		extractor := symbol.NewGoExtractor()
		return extractor.Extract(ctx, filePath, content)
	case "python":
		return extractPythonSymbols(filePath, content)
	case "javascript", "typescript":
		return extractJSSymbols(filePath, content, lang)
	default:
		return nil, skillerr.Validationf("unsupported language: %s", lang)
	}
}

func setSymbolKeys(symbols []symbol.Symbol, lang string) {
	for i, sym := range symbols {
		if sym.Key != "" {
			continue
		}
		switch lang {
		case "go":
			if sym.Name == "init" {
				sym.Key = symbol.GoInitSymbolKey(filepath.Base(sym.FilePath))
			} else {
				r, _ := utf8.DecodeRuneInString(sym.Name)
				if unicode.IsUpper(r) {
					sym.Key = symbol.GoSymbolKey(sym.Name)
				} else {
					sym.Key = symbol.GoNonExportedSymbolKey(sym.Name, filepath.Base(sym.FilePath))
				}
			}
		case "typescript":
			sym.Key = symbol.TSSymbolKey(sym.Name, false, filepath.Base(sym.FilePath))
		case "python":
			sym.Key = symbol.PythonSymbolKey(sym.Name)
		}
		symbols[i] = sym
	}
}

// extractPythonSymbols does simple regex-based extraction for Python functions and classes with line tracking.
func extractPythonSymbols(filePath string, content []byte) ([]symbol.Symbol, error) {
	var symbols []symbol.Symbol
	lines := strings.Split(string(content), "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Function definition
		if strings.HasPrefix(trimmed, "def ") {
			parts := strings.SplitN(trimmed, "(", 2)
			if len(parts) == 2 {
				name := strings.TrimSpace(strings.TrimPrefix(parts[0], "def"))
				symbols = append(symbols, symbol.Symbol{
					ID:        symbol.ID(filePath, name),
					FilePath:  filePath,
					Name:      name,
					Language:  "python",
					Kind:      symbol.KindFunction,
					StartLine: i + 1,
					Signature: trimmed,
				})
			}
		}

		// Class definition
		if strings.HasPrefix(trimmed, "class ") {
			parts := strings.SplitN(trimmed, "(", 2)
			name := strings.TrimSpace(strings.TrimPrefix(parts[0], "class"))
			name = strings.TrimSuffix(name, ":")
			symbols = append(symbols, symbol.Symbol{
				ID:        symbol.ID(filePath, name),
				FilePath:  filePath,
				Name:      name,
				Language:  "python",
				Kind:      symbol.KindClass,
				StartLine: i + 1,
				Signature: trimmed,
			})
		}
	}

	return symbols, nil
}

// extractJSSymbols does simple regex-based extraction for JavaScript/TypeScript functions, classes, and interfaces.
func extractJSSymbols(filePath string, content []byte, lang string) ([]symbol.Symbol, error) {
	var symbols []symbol.Symbol
	lines := strings.Split(string(content), "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Function declarations
		if strings.HasPrefix(trimmed, "function ") {
			parts := strings.SplitN(trimmed, "(", 2)
			if len(parts) == 2 {
				name := strings.TrimSpace(strings.TrimPrefix(parts[0], "function"))
				exported := false
				symbols = append(symbols, symbol.Symbol{
					ID:        symbol.ID(filePath, name),
					FilePath:  filePath,
					Name:      name,
					Language:  lang,
					Kind:      symbol.KindFunction,
					StartLine: i + 1,
					Signature: trimmed,
					Key:       symbol.TSSymbolKey(name, exported, filepath.Base(filePath)),
				})
			}
		}

		// Class declarations
		if strings.HasPrefix(trimmed, "class ") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 {
				name := strings.TrimSuffix(parts[1], "{")
				exported := false
				symbols = append(symbols, symbol.Symbol{
					ID:        symbol.ID(filePath, name),
					FilePath:  filePath,
					Name:      name,
					Language:  lang,
					Kind:      symbol.KindClass,
					StartLine: i + 1,
					Signature: trimmed,
					Key:       symbol.TSSymbolKey(name, exported, filepath.Base(filePath)),
				})
			}
		}

		// Interface declarations (TypeScript)
		if lang == "typescript" && strings.HasPrefix(trimmed, "interface ") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 {
				name := strings.TrimSuffix(parts[1], "{")
				exported := false
				symbols = append(symbols, symbol.Symbol{
					ID:        symbol.ID(filePath, name),
					FilePath:  filePath,
					Name:      name,
					Language:  lang,
					Kind:      symbol.KindInterface,
					StartLine: i + 1,
					Signature: trimmed,
					Key:       symbol.TSSymbolKey(name, exported, filepath.Base(filePath)),
				})
			}
		}

		// Export function/const (common patterns)
		if strings.HasPrefix(trimmed, "export function ") {
			parts := strings.SplitN(strings.TrimPrefix(trimmed, "export "), "(", 2)
			if len(parts) == 2 {
				name := strings.TrimSpace(strings.TrimPrefix(parts[0], "function"))
				exported := true
				symbols = append(symbols, symbol.Symbol{
					ID:        symbol.ID(filePath, name),
					FilePath:  filePath,
					Name:      name,
					Language:  lang,
					Kind:      symbol.KindFunction,
					StartLine: i + 1,
					Signature: trimmed,
					Key:       symbol.TSSymbolKey(name, exported, filepath.Base(filePath)),
				})
			}
		}
	}

	return symbols, nil
}

// upsertSymbols saves new/updated symbols and removes stale ones with session tracking and deduplication.
// Returns (updated count, deleted count, error).
func upsertSymbols(ctx context.Context, store storage.MemoryStore, workspaceID, filePath, lang, sessionID string, symbols []symbol.Symbol) (int, int, error) {
	pkg := symbolutil.DeriveSymbolPackage(filePath, lang)

	// Build a map of new symbol entry names
	newSymbolNames := make(map[string]bool)
	for _, sym := range symbols {
		entryName := symbolutil.KeyEntryName(workspaceID, pkg, sym.EffectiveID())
		newSymbolNames[entryName] = true
	}

	// Get existing symbols for this package by searching with package-scoped key prefix
	prefix := fmt.Sprintf("symbol://%s/%s::", workspaceID, pkg)

	// Paginate through all symbol entries to detect stale ones for the CURRENT file.
	// The prefix matches all symbols in the package, so we must check each entry's
	// stored FilePath to avoid accidentally deleting symbols from other files.
	var staleNames []string
	const pageSize = 500
	for offset := 0; ; offset += pageSize {
		page, total, err := store.ListFiltered(ctx, workspaceID, storage.MemoryListFilter{
			Types: []string{symbol.SymbolType},
		}, pageSize, offset)
		if err != nil {
			return 0, 0, skillerr.WrapIO("list entries", err)
		}
		for _, entry := range page {
			if !strings.HasPrefix(entry.Name, prefix) {
				continue
			}
			if newSymbolNames[entry.Name] {
				continue
			}
			stored, unmarshalErr := symbol.UnmarshalResult(entry.Result)
			if unmarshalErr != nil {
				continue
			}
			if stored.Symbol.FilePath != filePath {
				continue
			}
			staleNames = append(staleNames, entry.Name)
		}
		if offset+pageSize >= total {
			break
		}
	}

	// Delete stale symbols
	deleted := 0
	for _, name := range staleNames {
		if err := store.Delete(ctx, name, workspaceID); err != nil {
			// Log but continue - best effort deletion
			continue
		}
		deleted++
	}

	// Upsert new symbols
	updated := 0
	for _, sym := range symbols {
		entryName := symbolutil.KeyEntryName(workspaceID, pkg, sym.EffectiveID())

		// Serialize symbol result
		result := symbol.Result{Symbol: sym}
		resultBytes, err := symbol.MarshalResult(result)
		if err != nil {
			continue
		}

		// Create entry
		entry := storage.NamedEntry{
			Name:      entryName,
			Type:      symbol.SymbolType,
			Workspace: workspaceID,
			Summary:   sym.Name,
			Result:    resultBytes,
			SessionID: sessionID,
		}

		// Save (upsert)
		if _, err := store.Save(ctx, entry); err != nil {
			continue
		}
		updated++
	}

	return updated, deleted, nil
}

// detectLanguage returns the language based on file extension.
// queueEmbeddings enqueues symbols for background embedding generation with deduplication and content enrichment.
// Returns (queued count, skipped count).
func queueEmbeddings(ctx context.Context, storageRoot, workspaceID string, symbols []symbol.Symbol, fileContent []byte, mode config.EmbedSymbolTextMode, model string) (int, int) {
	// Open embedding store
	store, err := embedding.OpenStore(ctx, storageRoot)
	if err != nil {
		// Don't fail - embedding is optional
		return 0, len(symbols)
	}
	defer func() { errs.Ignore(store.Close(), "close embedding store") }()

	// Convert symbols to embedding inputs
	symbolInputs := make([]embedding.SymbolInput, 0, len(symbols))
	contentStr := string(fileContent)

	for _, sym := range symbols {
		symbolLang := strings.TrimSpace(sym.Language)
		if symbolLang == "" {
			symbolLang = langutil.DetectAllowed(sym.FilePath, langutil.CommonCodeLanguages)
		}
		pkg := symbolutil.DeriveSymbolPackage(sym.FilePath, symbolLang)
		symbolKey := strings.TrimSpace(sym.Key.String())
		if symbolKey == "" {
			symbolKey = sym.EffectiveID()
		}
		// Extract symbol body from file content
		body := extractSymbolBody(contentStr, sym)
		content := body
		contentDigest := ""
		if mode == config.EmbedSymbolTextModeDocEnriched {
			info := embeddingtext.SymbolInfo{
				Name:      sym.Name,
				Kind:      string(sym.Kind),
				FilePath:  sym.FilePath,
				Signature: sym.Signature,
				Doc:       sym.Documentation,
				Code:      body,
			}
			content = embeddingtext.BuildSymbolEmbeddingText(info, embeddingtext.DefaultSymbolTextOptionsDocEnriched())
			contentDigest = embeddingtext.BuildSymbolContentDigest(embeddingtext.SymbolDigestInput{
				Model:      model,
				Kind:       string(sym.Kind),
				Name:       sym.Name,
				SymbolKey:  sym.EffectiveID(),
				FilePath:   sym.FilePath,
				Signature:  sym.Signature,
				Doc:        sym.Documentation,
				BodyDigest: sym.BodyDigest,
			})
		} else {
			contentDigest = embeddingtext.DigestSHA256(content)
		}
		if strings.TrimSpace(content) == "" {
			continue
		}

		symbolInputs = append(symbolInputs, embedding.SymbolInput{
			SymbolID:      symbolutil.ScopedSymbolID(pkg, symbolKey),
			FilePath:      sym.FilePath,
			SymbolName:    sym.Name,
			Language:      symbolLang,
			PackageID:     pkg,
			SymbolKey:     symbolKey,
			MemoryName:    symbolutil.KeyEntryName(workspaceID, pkg, symbolKey),
			Content:       content,
			ContentDigest: contentDigest,
		})
	}

	if len(symbolInputs) == 0 {
		return 0, len(symbols)
	}

	// Enqueue with deduplication enabled
	result, err := store.Enqueue(ctx, embedding.EnqueueRequest{
		WorkspaceID: workspaceID,
		Symbols:     symbolInputs,
		Priority:    embedding.PriorityNormal,
		Model:       model,
		Deduplicate: true,
	})
	if err != nil {
		return 0, len(symbols)
	}

	return result.Queued, result.Skipped
}

// extractSymbolBody extracts the body text for a symbol from file content using line ranges and indentation heuristics.
func extractSymbolBody(content string, sym symbol.Symbol) string {
	lines := strings.Split(content, "\n")

	// Use symbol's line range if available
	startLine := sym.StartLine
	endLine := sym.EndLine

	// Clamp to valid range
	if startLine < 1 {
		startLine = 1
	}
	if startLine > len(lines) {
		return ""
	}

	// If no end line, try to infer based on indentation (simple heuristic)
	if endLine < startLine {
		endLine = inferEndLine(lines, startLine-1)
	}

	if endLine > len(lines) {
		endLine = len(lines)
	}

	// Extract body
	bodyLines := lines[startLine-1 : endLine]
	return strings.Join(bodyLines, "\n")
}

// inferEndLine tries to find the end of a code block based on indentation and closing patterns with fallback logic.
func inferEndLine(lines []string, startIdx int) int {
	if startIdx >= len(lines) {
		return startIdx + 1
	}

	// Get baseline indentation
	startLine := lines[startIdx]
	baseIndent := len(startLine) - len(strings.TrimLeft(startLine, " \t"))

	// Look for closing brace or dedent
	for i := startIdx + 1; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Skip empty lines
		if trimmed == "" {
			continue
		}

		indent := len(line) - len(strings.TrimLeft(line, " \t"))

		// Found a line at same or lower indentation level
		if indent <= baseIndent && trimmed != "" {
			// Include closing brace if that's what we found
			if trimmed == "}" || trimmed == "end" {
				return i + 1
			}
			return i
		}
	}

	// Return end of file if no closing found
	return len(lines)
}

// contentUnchanged checks if the file's content hash matches the previously indexed hash.
// This allows skipping unchanged files entirely during live indexing.
func contentUnchanged(ctx context.Context, store storage.MemoryStore, workspaceID, filePath, contentHash string) bool {
	if contentHash == "" {
		return false
	}
	hashName := fmt.Sprintf("_idx_hash://%s/%s", workspaceID, filePath)
	entry, err := store.Get(ctx, hashName, workspaceID)
	if err != nil {
		return false
	}
	// The hash is stored in the Summary field
	return entry.Summary == contentHash
}

// storeContentHash persists the content hash for future unchanged-file detection.
func storeContentHash(ctx context.Context, store storage.MemoryStore, workspaceID, filePath, contentHash string) {
	hashName := fmt.Sprintf("_idx_hash://%s/%s", workspaceID, filePath)
	entry := storage.NamedEntry{
		Name:      hashName,
		Type:      "index_hash",
		Workspace: workspaceID,
		Summary:   contentHash,
	}
	_, _ = store.Save(ctx, entry)
}

// emit outputs the result envelope with indexing statistics and timing information for skill completion.
func emit(rc *skillmain.RunContext, out output) error {
	return skillout.Emit(rc, Command, out)
}

// ingestGraphEdges extracts call and import relationships and stores them in the graph store for PageRank analysis.
// This enables PageRank-boosted code search by building the dependency graph.
// Returns (call edges created, import edges created).
func ingestGraphEdges(ctx context.Context, storageRoot, workspace, filePath string, symbols []symbol.Symbol, content []byte) (int, int) {
	// Open graph store
	graphStore, err := graph.Open(ctx, storageRoot)
	if err != nil {
		// Don't fail - graph ingestion is optional enhancement
		return 0, 0
	}
	defer func() { errs.Ignore(graphStore.Close(), "close graph store") }()

	var callEdges, importEdges int
	now := time.Now().UTC()

	// Create file node for import relationships
	fileNodeID := "file:" + filePath
	if err := graphStore.UpsertNode(ctx, graph.Node{
		Workspace:   workspace,
		NodeID:      fileNodeID,
		NodeType:    graph.NodeTypeFile,
		Title:       filepath.Base(filePath),
		CurrentPath: filePath,
		LastSeen:    now,
	}); err != nil {
		// Log but continue
		_ = err
	}

	// Extract Go imports and create edges
	imports := extractGoImports(content)
	for _, imp := range imports {
		// Create import edge: file → import_path
		// Use "pkg:" prefix for external packages
		targetID := "pkg:" + imp
		edge := graph.Edge{
			Workspace: workspace,
			FromID:    fileNodeID,
			FromType:  graph.NodeTypeFile,
			ToID:      targetID,
			ToType:    graph.NodeTypeFile, // Could be NodeTypeSymbol for local imports
			EdgeType:  graph.EdgeTypeImports,
			Weight:    1.0,
			CreatedAt: now,
		}
		if err := graphStore.UpsertEdge(ctx, edge); err == nil {
			importEdges++
		}
	}

	// Create symbol nodes and extract call edges
	extractor := symbol.NewGoExtractor()
	for _, sym := range symbols {
		// Create symbol node
		symbolNodeID := "symbol:" + sym.ID
		if err := graphStore.UpsertNode(ctx, graph.Node{
			Workspace:   workspace,
			NodeID:      symbolNodeID,
			NodeType:    graph.NodeTypeSymbol,
			Title:       sym.Name,
			CurrentPath: sym.FilePath,
			LastSeen:    now,
			Metadata: map[string]string{
				"kind":      string(sym.Kind),
				"signature": sym.Signature,
			},
		}); err != nil {
			continue
		}

		// Extract calls from this symbol
		calls, err := extractor.ExtractCalls(ctx, sym, content)
		if err != nil {
			continue
		}

		// Create call edges
		for _, callName := range calls {
			// Skip qualified calls (e.g., "fmt.Println", "http.Get") - we can't resolve
			// cross-package targets without import resolution. Only same-file/package
			// unqualified calls (e.g., "helper") can be matched within the same file.
			if strings.Contains(callName, ".") {
				continue
			}

			// Create edge: symbol → called_symbol
			// The target may not exist yet; we create the edge anyway
			// and the node will be created when that file is indexed
			targetID := "symbol:" + filePath + ":" + callName
			edge := graph.Edge{
				Workspace: workspace,
				FromID:    symbolNodeID,
				FromType:  graph.NodeTypeSymbol,
				ToID:      targetID,
				ToType:    graph.NodeTypeSymbol,
				EdgeType:  graph.EdgeTypeCalls,
				Weight:    1.0,
				CreatedAt: now,
			}
			if err := graphStore.UpsertEdge(ctx, edge); err == nil {
				callEdges++
			}
		}
	}

	return callEdges, importEdges
}

// extractGoImports extracts import paths from Go source code with external package filtering and stdlib exclusion.
func extractGoImports(content []byte) []string {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", content, parser.ImportsOnly)
	if err != nil {
		return nil
	}

	var imports []string
	for _, imp := range file.Imports {
		// Extract import path, removing quotes
		importPath := strings.Trim(imp.Path.Value, `"`)
		// Skip standard library (no dots in path)
		if !strings.Contains(importPath, ".") {
			continue
		}
		imports = append(imports, importPath)
	}
	return imports
}
