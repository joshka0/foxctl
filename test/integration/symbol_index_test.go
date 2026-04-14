//go:build integration

// Package integration contains integration tests for foxctl subsystems.
// These tests require the "integration" build tag to run.
package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/intelligence/indexing"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"
	platformsymbol "github.com/joshka0/foxctl/internal/platform/symbolutil"
	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/storage/memory"
	"github.com/rs/zerolog"
)

// setupMemoryStore creates a memory store for testing.
func setupMemoryStore(t *testing.T) *memory.Store {
	t.Helper()
	tmpDir := t.TempDir()
	storageDir := filepath.Join(tmpDir, "storage")
	casDir := filepath.Join(tmpDir, "cas")

	ctx := context.Background()
	store, err := memory.Open(ctx, storageDir, casDir)
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func canonicalWorkspaceID(path string) string {
	if id := workspace.ID(path); id != "" {
		return id
	}
	return path
}

// TestSymbolIndex_PostReviewFlow tests the full post-review → symbol index flow.
// This is a D4 integration test per Phase 4 spec.
func TestSymbolIndex_PostReviewFlow(t *testing.T) {
	// Create temp workspace
	workspaceDir := t.TempDir()
	workspaceID := canonicalWorkspaceID(workspaceDir)

	// Create test files
	files := map[string]string{
		"main.go": `package main

import "fmt"

func main() {
	fmt.Println(greet("World"))
}

func greet(name string) string {
	return "Hello, " + name
}
`,
		"utils/helper.go": `package utils

// Helper provides utility functions.
type Helper struct{}

// DoWork performs work.
func (h *Helper) DoWork() error {
	return nil
}
`,
	}

	for path, content := range files {
		fullPath := filepath.Join(workspaceDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write file failed: %v", err)
		}
	}

	// Set up memory store
	memStore := setupMemoryStore(t)

	logger := zerolog.New(zerolog.NewTestWriter(t)).Level(zerolog.DebugLevel)

	// Create post-review handler with symbol indexer
	handlerCfg := indexing.PostReviewConfig{
		Enabled: true,
		Mode:    indexing.FanoutModeInline,
		Indexers: []indexing.IndexerConfig{
			{
				ID:           "code_symbol_dag",
				Enabled:      true,
				IncludeGlobs: []string{"**/*.go"},
				ExcludeGlobs: []string{"vendor/**"},
			},
		},
	}

	handler := indexing.NewPostReviewHandler(handlerCfg, logger)

	// Register symbol indexer
	err := symbol.RegisterWithHandler(
		handler,
		memStore,
		workspaceDir,
		handlerCfg.Indexers[0],
		logger,
	)
	if err != nil {
		t.Fatalf("register indexer: %v", err)
	}

	// Create post-review event
	event := indexing.PostReviewEvent{
		ID:          "evt-001",
		WorkspaceID: workspaceID,
		TaskID:      "task-123",
		ReviewID:    "review-456",
		Reason:      "post_review",
		Files: []indexing.FileChange{
			{Path: "main.go", ChangeKind: indexing.ChangeKindAdded, Language: "go"},
			{Path: "utils/helper.go", ChangeKind: indexing.ChangeKindAdded, Language: "go"},
		},
	}

	// Handle the event
	ctx := context.Background()
	result, err := handler.Handle(ctx, event)
	if err != nil {
		t.Fatalf("handle event: %v", err)
	}

	// Verify results
	if result.Skipped {
		t.Fatalf("indexing was skipped: %s", result.Reason)
	}
	if len(result.IndexerResults) != 1 {
		t.Fatalf("expected 1 indexer result, got %d", len(result.IndexerResults))
	}

	ir := result.IndexerResults[0]
	if ir.IndexerID != "code_symbol_dag" {
		t.Errorf("expected indexer_id 'code_symbol_dag', got %q", ir.IndexerID)
	}
	if ir.FilesIndexed != 2 {
		t.Errorf("expected 2 files indexed, got %d", ir.FilesIndexed)
	}
	if ir.Error != "" {
		t.Errorf("unexpected error: %s", ir.Error)
	}

	// Verify symbols were stored by querying memory store
	entries, err := memStore.List(ctx, workspaceID, 100)
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}

	// Expected symbols:
	// main.go: main, greet (2 functions)
	// utils/helper.go: Helper (struct), Helper.DoWork (method) (2 symbols)
	// Total: 4 symbols
	if len(entries) < 4 {
		t.Errorf("expected at least 4 symbol entries, got %d", len(entries))
		for _, e := range entries {
			t.Logf("  entry: %s", e.Name)
		}
	}

	// Verify specific symbols exist
	symbolNames := make(map[string]bool)
	for _, e := range entries {
		symbolNames[e.Name] = true
	}

	mainPkg := platformsymbol.DeriveSymbolPackage("main.go", "go")
	utilsPkg := platformsymbol.DeriveSymbolPackage("utils/helper.go", "go")
	expectedSymbols := []string{
		platformsymbol.KeyEntryName(workspaceID, mainPkg, symbol.GoNonExportedSymbolKey("main", "main.go").String()),
		platformsymbol.KeyEntryName(workspaceID, mainPkg, symbol.GoNonExportedSymbolKey("greet", "main.go").String()),
		platformsymbol.KeyEntryName(workspaceID, utilsPkg, symbol.GoSymbolKey("Helper").String()),
		platformsymbol.KeyEntryName(workspaceID, utilsPkg, symbol.GoSymbolKey("Helper.DoWork").String()),
	}

	for _, name := range expectedSymbols {
		if !symbolNames[name] {
			t.Errorf("expected symbol %q not found", name)
		}
	}
}

// TestSymbolIndex_IncrementalUpdate tests that only changed files are re-indexed.
func TestSymbolIndex_IncrementalUpdate(t *testing.T) {
	workspaceDir := t.TempDir()
	workspaceID := canonicalWorkspaceID(workspaceDir)

	// Create initial file
	initialContent := `package main

func original() {}
`
	if err := os.WriteFile(filepath.Join(workspaceDir, "app.go"), []byte(initialContent), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	memStore := setupMemoryStore(t)

	logger := zerolog.New(zerolog.NewTestWriter(t)).Level(zerolog.DebugLevel)

	handlerCfg := indexing.PostReviewConfig{
		Enabled: true,
		Mode:    indexing.FanoutModeInline,
		Indexers: []indexing.IndexerConfig{
			{ID: "code_symbol_dag", Enabled: true},
		},
	}

	handler := indexing.NewPostReviewHandler(handlerCfg, logger)
	err := symbol.RegisterWithHandler(handler, memStore, workspaceDir, handlerCfg.Indexers[0], logger)
	if err != nil {
		t.Fatalf("register indexer: %v", err)
	}

	ctx := context.Background()

	// First indexing
	event1 := indexing.PostReviewEvent{
		WorkspaceID: workspaceID,
		Files: []indexing.FileChange{
			{Path: "app.go", ChangeKind: indexing.ChangeKindAdded, Language: "go"},
		},
	}

	result1, err := handler.Handle(ctx, event1)
	if err != nil {
		t.Fatalf("first handle: %v", err)
	}
	if result1.IndexerResults[0].FilesIndexed != 1 {
		t.Errorf("first pass: expected 1 file indexed, got %d", result1.IndexerResults[0].FilesIndexed)
	}

	// Second indexing with same content - should be skipped
	event2 := indexing.PostReviewEvent{
		WorkspaceID: workspaceID,
		Files: []indexing.FileChange{
			{Path: "app.go", ChangeKind: indexing.ChangeKindModified, Language: "go"},
		},
	}

	result2, err := handler.Handle(ctx, event2)
	if err != nil {
		t.Fatalf("second handle: %v", err)
	}
	if result2.IndexerResults[0].FilesSkipped != 1 {
		t.Errorf("second pass: expected 1 file skipped (unchanged), got skipped=%d indexed=%d",
			result2.IndexerResults[0].FilesSkipped, result2.IndexerResults[0].FilesIndexed)
	}

	// Modify file content
	modifiedContent := `package main

func original() {}
func added() {}
`
	if err := os.WriteFile(filepath.Join(workspaceDir, "app.go"), []byte(modifiedContent), 0o644); err != nil {
		t.Fatalf("write modified file: %v", err)
	}

	// Third indexing - should detect change and re-index
	event3 := indexing.PostReviewEvent{
		WorkspaceID: workspaceID,
		Files: []indexing.FileChange{
			{Path: "app.go", ChangeKind: indexing.ChangeKindModified, Language: "go"},
		},
	}

	result3, err := handler.Handle(ctx, event3)
	if err != nil {
		t.Fatalf("third handle: %v", err)
	}
	if result3.IndexerResults[0].FilesIndexed != 1 {
		t.Errorf("third pass: expected 1 file indexed (changed), got indexed=%d skipped=%d",
			result3.IndexerResults[0].FilesIndexed, result3.IndexerResults[0].FilesSkipped)
	}

	// Verify both symbols exist
	entries, err := memStore.List(ctx, workspaceID, 100)
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}

	// Filter to only code_symbol entries (excludes file_meta, call edges)
	var symbolCount int
	for _, e := range entries {
		if e.Type == "code_symbol" {
			symbolCount++
		}
	}
	if symbolCount != 2 {
		t.Errorf("expected 2 code_symbol entries (original + added), got %d", symbolCount)
	}
}

// TestSymbolIndex_CallGraph tests that call graph edges are correctly extracted.
func TestSymbolIndex_CallGraph(t *testing.T) {
	workspaceDir := t.TempDir()
	workspaceID := canonicalWorkspaceID(workspaceDir)

	content := `package main

func caller() {
	callee()
	helper()
}

func callee() {}

func helper() {}
`
	if err := os.WriteFile(filepath.Join(workspaceDir, "calls.go"), []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	memStore := setupMemoryStore(t)

	logger := zerolog.New(zerolog.NewTestWriter(t)).Level(zerolog.DebugLevel)

	handlerCfg := indexing.PostReviewConfig{
		Enabled: true,
		Mode:    indexing.FanoutModeInline,
		Indexers: []indexing.IndexerConfig{
			{ID: "code_symbol_dag", Enabled: true},
		},
	}

	handler := indexing.NewPostReviewHandler(handlerCfg, logger)
	err := symbol.RegisterWithHandler(handler, memStore, workspaceDir, handlerCfg.Indexers[0], logger)
	if err != nil {
		t.Fatalf("register indexer: %v", err)
	}

	ctx := context.Background()
	event := indexing.PostReviewEvent{
		WorkspaceID: workspaceID,
		Files: []indexing.FileChange{
			{Path: "calls.go", ChangeKind: indexing.ChangeKindAdded, Language: "go"},
		},
	}

	_, err = handler.Handle(ctx, event)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}

	// Verify call edges are stored in symbol entries
	// (calls are embedded in Result.Calls, not separate entries)
	allEntries, err := memStore.List(ctx, workspaceID, 100)
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}

	// Find the caller symbol using key-based entry name (stable across file moves)
	// "caller" is non-exported, so key includes filename: "calls.go/caller"
	pkg := platformsymbol.DeriveSymbolPackage("calls.go", "go")
	callerKey := symbol.GoNonExportedSymbolKey("caller", "calls.go")
	callerKeyName := platformsymbol.KeyEntryName(workspaceID, pkg, callerKey.String())
	var callerEntry *memory.NamedEntry
	for i := range allEntries {
		if allEntries[i].Type == symbol.SymbolType && allEntries[i].Name == callerKeyName {
			callerEntry = &allEntries[i]
			break
		}
	}

	if callerEntry == nil {
		t.Fatalf("caller symbol not found: %s", callerKeyName)
	}

	// Unmarshal and check calls
	result, err := symbol.UnmarshalResult(callerEntry.Result)
	if err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	// caller() calls callee() and helper() - should have exactly 2 call edges
	if len(result.Calls) != 2 {
		t.Errorf("expected 2 calls, got %d: %v", len(result.Calls), result.Calls)
	}
}
