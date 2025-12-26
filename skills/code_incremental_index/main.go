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
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/indexing/embedding"
	"github.com/jkatigb/agentctl/internal/indexing/symbol"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/graph"
	"github.com/jkatigb/agentctl/internal/storage/memory"
)

// Command is the skill command name.
const Command = "code/incremental_index"

// input matches the skill input specification.
type input struct {
	File        string `json:"file"`
	WorkspaceID string `json:"workspace_id"`
	Symbols     *bool  `json:"symbols"`
	Embed       bool   `json:"embed"`
	EmbedQueue  bool   `json:"embed_queue"`
}

// output contains the skill result data.
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

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("ERUNTIME", err)
	}

	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	// Pass workspace path so parseInput can use it as the default workspace scope
	workspacePath := rc.PathValidator.Workspace()
	in, err := parseInput(os.Stdin, workspacePath)
	if err != nil {
		fail("EARG", err)
	}

	if err := run(ctx, rc, in); err != nil {
		fail("ERUNTIME", err)
	}
}

func parseInput(r io.Reader, workspacePath string) (input, error) {
	var in input
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return input{}, fmt.Errorf("decode input: %w", err)
	}
	if in.File == "" {
		return input{}, fmt.Errorf("file is required")
	}
	// Use the actual workspace path for scoping, not a hash or "default"
	// This ensures symbols are indexed under the same key used by semantic search
	if in.WorkspaceID == "" {
		in.WorkspaceID = workspacePath
	}
	// Default symbols=true
	if in.Symbols == nil {
		t := true
		in.Symbols = &t
	}
	return in, nil
}

func run(ctx context.Context, rc *runner.RunnerContext, in input) error {
	start := time.Now()

	// Resolve file path
	workspace := rc.PathValidator.Workspace()
	absPath, err := rc.PathValidator.ValidatePath(in.File)
	if err != nil {
		return fmt.Errorf("path validation: %w", err)
	}

	// Get relative path for storage
	relPath, err := filepath.Rel(workspace, absPath)
	if err != nil {
		relPath = in.File
	}
	relPath = filepath.ToSlash(relPath)

	// Detect language
	lang := detectLanguage(absPath)
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
		return fmt.Errorf("read file: %w", err)
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

	// Extract symbols if enabled
	var symbols []symbol.Symbol
	if *in.Symbols {
		symbols, err = extractSymbols(ctx, lang, relPath, content)
		if err != nil {
			// Log but don't fail - partial indexing is acceptable
			symbols = nil
		}
	}

	// Open memory store
	store, err := memory.Open(ctx, rc.Config.Paths.Cache, rc.Config.Paths.CAS)
	if err != nil {
		return fmt.Errorf("open memory store: %w", err)
	}
	defer func() { errs.Ignore(store.Close(), "close memory store") }()

	// Upsert symbols and delete stale ones
	updated, deleted, err := upsertSymbols(ctx, store, in.WorkspaceID, relPath, rc.SessionID, symbols)
	if err != nil {
		return fmt.Errorf("upsert symbols: %w", err)
	}

	// Queue embeddings for updated symbols
	var embeddingQueued, embeddingSkipped int
	if in.EmbedQueue && len(symbols) > 0 {
		embeddingQueued, embeddingSkipped = queueEmbeddings(ctx, rc.Config.Paths.Cache, in.WorkspaceID, symbols, content)
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

// extractSymbols extracts code symbols from the file content.
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
		return nil, fmt.Errorf("unsupported language: %s", lang)
	}
}

// extractPythonSymbols does simple regex-based extraction for Python.
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

// extractJSSymbols does simple regex-based extraction for JavaScript/TypeScript.
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
				symbols = append(symbols, symbol.Symbol{
					ID:        symbol.ID(filePath, name),
					FilePath:  filePath,
					Name:      name,
					Language:  lang,
					Kind:      symbol.KindFunction,
					StartLine: i + 1,
					Signature: trimmed,
				})
			}
		}

		// Class declarations
		if strings.HasPrefix(trimmed, "class ") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 {
				name := strings.TrimSuffix(parts[1], "{")
				symbols = append(symbols, symbol.Symbol{
					ID:        symbol.ID(filePath, name),
					FilePath:  filePath,
					Name:      name,
					Language:  lang,
					Kind:      symbol.KindClass,
					StartLine: i + 1,
					Signature: trimmed,
				})
			}
		}

		// Interface declarations (TypeScript)
		if lang == "typescript" && strings.HasPrefix(trimmed, "interface ") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 {
				name := strings.TrimSuffix(parts[1], "{")
				symbols = append(symbols, symbol.Symbol{
					ID:        symbol.ID(filePath, name),
					FilePath:  filePath,
					Name:      name,
					Language:  lang,
					Kind:      symbol.KindInterface,
					StartLine: i + 1,
					Signature: trimmed,
				})
			}
		}

		// Export function/const (common patterns)
		if strings.HasPrefix(trimmed, "export function ") {
			parts := strings.SplitN(strings.TrimPrefix(trimmed, "export "), "(", 2)
			if len(parts) == 2 {
				name := strings.TrimSpace(strings.TrimPrefix(parts[0], "function"))
				symbols = append(symbols, symbol.Symbol{
					ID:        symbol.ID(filePath, name),
					FilePath:  filePath,
					Name:      name,
					Language:  lang,
					Kind:      symbol.KindFunction,
					StartLine: i + 1,
					Signature: trimmed,
				})
			}
		}
	}

	return symbols, nil
}

// upsertSymbols saves new/updated symbols and removes stale ones.
// Returns (updated count, deleted count, error).
func upsertSymbols(ctx context.Context, store storage.MemoryStore, workspaceID, filePath, sessionID string, symbols []symbol.Symbol) (int, int, error) {
	// Build a map of new symbol entry names
	newSymbolNames := make(map[string]bool)
	for _, sym := range symbols {
		entryName := symbol.EntryName(workspaceID, filePath, sym.Name)
		newSymbolNames[entryName] = true
	}

	// Get existing symbols for this file by searching with file path prefix
	// Entry names follow: symbol://<workspace>/<file_path>:<symbol_name>
	prefix := fmt.Sprintf("symbol://%s/%s:", workspaceID, filePath)

	// Find existing symbols to detect stale ones
	existingEntries, err := store.List(ctx, workspaceID, 1000)
	if err != nil {
		return 0, 0, fmt.Errorf("list entries: %w", err)
	}

	// Collect stale symbols to delete
	var staleNames []string
	for _, entry := range existingEntries {
		if entry.Type != symbol.SymbolType {
			continue
		}
		if !strings.HasPrefix(entry.Name, prefix) {
			continue
		}
		// If not in new symbols, it's stale
		if !newSymbolNames[entry.Name] {
			staleNames = append(staleNames, entry.Name)
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
		entryName := symbol.EntryName(workspaceID, filePath, sym.Name)

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
func detectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js", ".jsx":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	default:
		return ""
	}
}

// queueEmbeddings enqueues symbols for background embedding generation.
// Returns (queued count, skipped count).
func queueEmbeddings(ctx context.Context, storageRoot, workspaceID string, symbols []symbol.Symbol, fileContent []byte) (int, int) {
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
		// Extract symbol body from file content
		body := extractSymbolBody(contentStr, sym)
		if body == "" {
			continue
		}

		symbolInputs = append(symbolInputs, embedding.SymbolInput{
			SymbolID:   sym.ID,
			FilePath:   sym.FilePath,
			SymbolName: sym.Name,
			Content:    body,
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
		Deduplicate: true,
	})
	if err != nil {
		return 0, len(symbols)
	}

	return result.Queued, result.Skipped
}

// extractSymbolBody extracts the body text for a symbol from file content.
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

// inferEndLine tries to find the end of a code block based on indentation.
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

func emit(rc *runner.RunnerContext, out output) error {
	return rc.Emit(Command, out, "application/json", envelope.Meta{Source: "run", Runner: "exec"})
}

func fail(code string, err error) {
	env := envelope.Error(Command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit failure")
	os.Exit(1)
}

// ingestGraphEdges extracts call and import relationships and stores them in the graph store.
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

// extractGoImports extracts import paths from Go source code.
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
