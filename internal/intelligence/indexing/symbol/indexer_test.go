package symbol

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/intelligence/indexing"
	"github.com/joshka0/foxctl/internal/platform/symbolutil"
	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/storage/memory"
	"github.com/rs/zerolog"
)

// keyEntryName constructs the expected key-based entry name for test lookups.
func keyEntryName(workspace, filePath, symbolName string) string {
	pkg := symbolutil.DeriveSymbolPackage(filePath, "go")
	key := GoSymbolKey(symbolName).String()
	return symbolutil.KeyEntryName(workspace, pkg, key)
}

func setupTestIndexer(t *testing.T, cfg Config) (*Indexer, *memory.Store, string, string) {
	t.Helper()

	tmpDir := t.TempDir()
	workspaceDir := filepath.Join(tmpDir, "workspace")
	storageDir := filepath.Join(tmpDir, "storage")
	casDir := filepath.Join(tmpDir, "cas")

	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	store, err := memory.Open(ctx, storageDir, casDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	logger := zerolog.Nop()
	idx := NewIndexer(cfg, store, nil, workspaceDir, logger)

	workspaceID := workspace.ID(workspaceDir)
	return idx, store, workspaceDir, workspaceID
}

func createTestFile(t *testing.T, dir, path, content string) {
	t.Helper()
	fullPath := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIndexer_ID(t *testing.T) {
	idx, _, _, _ := setupTestIndexer(t, Config{Enabled: true})
	if idx.ID() != IndexerID {
		t.Errorf("expected ID %q, got %q", IndexerID, idx.ID())
	}
}

func TestIndexer_Index_Disabled(t *testing.T) {
	idx, _, _, workspaceID := setupTestIndexer(t, Config{Enabled: false})

	event := indexing.PostReviewEvent{
		WorkspaceID: workspaceID,
		Files: []indexing.FileChange{
			{Path: "main.go", ChangeKind: indexing.ChangeKindModified},
		},
	}

	result, err := idx.Index(context.Background(), event)
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}
	if result.FilesSkipped != 1 {
		t.Errorf("expected 1 file skipped, got %d", result.FilesSkipped)
	}
}

func TestIndexer_Index_GoFile(t *testing.T) {
	idx, store, workspaceDir, workspaceID := setupTestIndexer(t, Config{Enabled: true})

	goContent := `package main

import "fmt"

// Greet returns a greeting message.
func Greet(name string) string {
	return fmt.Sprintf("Hello, %s!", name)
}

// User represents a user.
type User struct {
	Name  string
	Email string
}

// GetName returns the user's name.
func (u *User) GetName() string {
	return u.Name
}

func main() {
	user := &User{Name: "Alice"}
	fmt.Println(Greet(user.GetName()))
}
`
	createTestFile(t, workspaceDir, "main.go", goContent)

	event := indexing.PostReviewEvent{
		WorkspaceID: workspaceID,
		TaskID:      "task-123",
		ReviewID:    "review-456",
		Reason:      "post_review",
		Files: []indexing.FileChange{
			{Path: "main.go", ChangeKind: indexing.ChangeKindModified, Language: "go"},
		},
	}

	result, err := idx.Index(context.Background(), event)
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}
	if result.FilesIndexed != 1 {
		t.Errorf("expected 1 file indexed, got %d", result.FilesIndexed)
	}

	// Verify symbols were saved
	ctx := context.Background()

	// Check Greet function
	greetName := keyEntryName(workspaceID, "main.go", "Greet")
	greetEntry, err := store.Get(ctx, greetName, workspaceID)
	if err != nil {
		t.Fatalf("Get Greet entry failed: %v", err)
	}
	if greetEntry.Type != SymbolType {
		t.Errorf("expected type %q, got %q", SymbolType, greetEntry.Type)
	}

	greetResult, err := UnmarshalResult(greetEntry.Result)
	if err != nil {
		t.Fatalf("Unmarshal result failed: %v", err)
	}
	if greetResult.Symbol.Kind != KindFunction {
		t.Errorf("expected kind 'function', got %q", greetResult.Symbol.Kind)
	}
	if greetResult.Symbol.Documentation == "" {
		t.Error("expected documentation to be extracted")
	}

	// Check User struct
	userName := keyEntryName(workspaceID, "main.go", "User")
	userEntry, err := store.Get(ctx, userName, workspaceID)
	if err != nil {
		t.Fatalf("Get User entry failed: %v", err)
	}

	userResult, err := UnmarshalResult(userEntry.Result)
	if err != nil {
		t.Fatalf("Unmarshal result failed: %v", err)
	}
	if userResult.Symbol.Kind != KindStruct {
		t.Errorf("expected kind 'struct', got %q", userResult.Symbol.Kind)
	}

	// Check User.GetName method
	getNameName := keyEntryName(workspaceID, "main.go", "User.GetName")
	getNameEntry, err := store.Get(ctx, getNameName, workspaceID)
	if err != nil {
		t.Fatalf("Get User.GetName entry failed: %v", err)
	}

	getNameResult, err := UnmarshalResult(getNameEntry.Result)
	if err != nil {
		t.Fatalf("Unmarshal result failed: %v", err)
	}
	if getNameResult.Symbol.Kind != KindMethod {
		t.Errorf("expected kind 'method', got %q", getNameResult.Symbol.Kind)
	}

	// Check file meta
	metaName := FileMetaEntryName(workspaceID, "main.go")
	metaEntry, err := store.Get(ctx, metaName, workspaceID)
	if err != nil {
		t.Fatalf("Get file meta failed: %v", err)
	}

	meta, err := UnmarshalFileMeta(metaEntry.Result)
	if err != nil {
		t.Fatalf("Unmarshal meta failed: %v", err)
	}
	if meta.Count < 4 {
		t.Errorf("expected at least 4 symbols, got %d", meta.Count)
	}
}

func TestIndexer_Index_UnsupportedLanguage(t *testing.T) {
	idx, _, workspaceDir, workspaceID := setupTestIndexer(t, Config{Enabled: true})

	createTestFile(t, workspaceDir, "data.json", `{"key": "value"}`)

	event := indexing.PostReviewEvent{
		WorkspaceID: workspaceID,
		Files: []indexing.FileChange{
			{Path: "data.json", ChangeKind: indexing.ChangeKindAdded},
		},
	}

	result, err := idx.Index(context.Background(), event)
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}
	if result.FilesSkipped != 1 {
		t.Errorf("expected 1 file skipped, got %d", result.FilesSkipped)
	}
}

func TestIndexer_Index_DeletedFile(t *testing.T) {
	idx, store, workspaceDir, workspaceID := setupTestIndexer(t, Config{Enabled: true})

	// First, index a file
	createTestFile(t, workspaceDir, "to_delete.go", "package main\n\nfunc Delete() {}\n")

	ctx := context.Background()
	addEvent := indexing.PostReviewEvent{
		WorkspaceID: workspaceID,
		Files: []indexing.FileChange{
			{Path: "to_delete.go", ChangeKind: indexing.ChangeKindAdded, Language: "go"},
		},
	}
	_, err := idx.Index(ctx, addEvent)
	if err != nil {
		t.Fatalf("Initial index failed: %v", err)
	}

	// Verify file meta exists
	metaName := FileMetaEntryName(workspaceID, "to_delete.go")
	_, err = store.Get(ctx, metaName, workspaceID)
	if err != nil {
		t.Fatalf("File meta should exist: %v", err)
	}

	// Delete the file
	deleteEvent := indexing.PostReviewEvent{
		WorkspaceID: workspaceID,
		Files: []indexing.FileChange{
			{Path: "to_delete.go", ChangeKind: indexing.ChangeKindDeleted},
		},
	}

	result, err := idx.Index(ctx, deleteEvent)
	if err != nil {
		t.Fatalf("Delete index failed: %v", err)
	}
	if result.FilesIndexed != 1 {
		t.Errorf("expected 1 file indexed (deleted), got %d", result.FilesIndexed)
	}

	// Verify file meta is deleted
	_, err = store.Get(ctx, metaName, workspaceID)
	if err == nil {
		t.Error("file meta should be deleted")
	}
}

func TestIndexer_Index_IncrementalUpdate(t *testing.T) {
	idx, store, workspaceDir, workspaceID := setupTestIndexer(t, Config{Enabled: true})

	// Initial content
	createTestFile(t, workspaceDir, "incremental.go", "package main\n\nfunc First() {}\n")

	ctx := context.Background()
	event := indexing.PostReviewEvent{
		WorkspaceID: workspaceID,
		Files: []indexing.FileChange{
			{Path: "incremental.go", ChangeKind: indexing.ChangeKindAdded, Language: "go"},
		},
	}

	_, err := idx.Index(ctx, event)
	if err != nil {
		t.Fatalf("First index failed: %v", err)
	}

	// Get initial digest
	metaName := FileMetaEntryName(workspaceID, "incremental.go")
	metaEntry, err := store.Get(ctx, metaName, workspaceID)
	if err != nil {
		t.Fatalf("Get meta failed: %v", err)
	}
	firstMeta, err := UnmarshalFileMeta(metaEntry.Result)
	if err != nil {
		t.Fatalf("failed to unmarshal file meta: %v", err)
	}

	// Update without changing content - should skip
	event.Files[0].ChangeKind = indexing.ChangeKindModified
	_, err = idx.Index(ctx, event)
	if err != nil {
		t.Fatalf("Second index failed: %v", err)
	}

	// Now actually change the content
	createTestFile(t, workspaceDir, "incremental.go", "package main\n\nfunc First() {}\nfunc Second() {}\n")

	_, err = idx.Index(ctx, event)
	if err != nil {
		t.Fatalf("Third index failed: %v", err)
	}

	// Check that meta was updated
	metaEntry, err = store.Get(ctx, metaName, workspaceID)
	if err != nil {
		t.Fatalf("Get updated meta failed: %v", err)
	}
	secondMeta, err := UnmarshalFileMeta(metaEntry.Result)
	if err != nil {
		t.Fatalf("failed to unmarshal file meta: %v", err)
	}

	if firstMeta.ContentHash == secondMeta.ContentHash {
		t.Error("content hash should have changed")
	}
	if secondMeta.Count <= firstMeta.Count {
		t.Errorf("symbol count should have increased: %d -> %d", firstMeta.Count, secondMeta.Count)
	}
}

func TestGoExtractor_ExtractFunctions(t *testing.T) {
	extractor := NewGoExtractor()

	content := []byte(`package test

// Add adds two numbers.
func Add(a, b int) int {
	return a + b
}

func (c *Calculator) Multiply(x, y float64) float64 {
	return x * y
}
`)

	ctx := context.Background()
	symbols, err := extractor.Extract(ctx, "test.go", content)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if len(symbols) < 2 {
		t.Fatalf("expected at least 2 symbols, got %d", len(symbols))
	}

	// Find Add function
	var addSym *Symbol
	for i := range symbols {
		if symbols[i].Name == "Add" {
			addSym = &symbols[i]
			break
		}
	}

	if addSym == nil {
		t.Fatal("Add function not found")
		return
	}
	if addSym.Kind != KindFunction {
		t.Errorf("expected kind 'function', got %q", addSym.Kind)
	}
	if addSym.Documentation == "" {
		t.Error("expected documentation")
	}
	if addSym.Signature == "" {
		t.Error("expected signature")
	}

	// Find Calculator.Multiply method
	var mulSym *Symbol
	for i := range symbols {
		if symbols[i].Name == "Calculator.Multiply" {
			mulSym = &symbols[i]
			break
		}
	}

	if mulSym == nil {
		t.Fatal("Calculator.Multiply method not found")
		return
	}
	if mulSym.Kind != KindMethod {
		t.Errorf("expected kind 'method', got %q", mulSym.Kind)
	}
}

func TestGoExtractor_ExtractTypes(t *testing.T) {
	extractor := NewGoExtractor()

	content := []byte(`package test

// Config holds configuration.
type Config struct {
	Name string
	Port int
}

type Handler interface {
	Handle() error
}
`)

	ctx := context.Background()
	symbols, err := extractor.Extract(ctx, "types.go", content)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// Find Config struct
	var configSym *Symbol
	for i := range symbols {
		if symbols[i].Name == "Config" {
			configSym = &symbols[i]
			break
		}
	}

	if configSym == nil {
		t.Fatal("Config struct not found")
		return
	}
	if configSym.Kind != KindStruct {
		t.Errorf("expected kind 'struct', got %q", configSym.Kind)
	}

	// Find Handler interface
	var handlerSym *Symbol
	for i := range symbols {
		if symbols[i].Name == "Handler" {
			handlerSym = &symbols[i]
			break
		}
	}

	if handlerSym == nil {
		t.Fatal("Handler interface not found")
		return
	}
	if handlerSym.Kind != KindInterface {
		t.Errorf("expected kind 'interface', got %q", handlerSym.Kind)
	}
}

// =============================================================================
// D1: Explicit call extraction tests
// =============================================================================

// TestGoExtractor_ExtractCalls_SimpleCalls tests that ExtractCalls identifies
// direct function calls within a function body.
func TestGoExtractor_ExtractCalls_SimpleCalls(t *testing.T) {
	extractor := NewGoExtractor()

	content := []byte(`package test

func helper() {}

func doSomething() string {
	return "done"
}

func main() {
	helper()
	result := doSomething()
	_ = result
}
`)

	ctx := context.Background()
	symbols, err := extractor.Extract(ctx, "calls.go", content)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// Find main function
	var mainSym *Symbol
	for i := range symbols {
		if symbols[i].Name == "main" {
			mainSym = &symbols[i]
			break
		}
	}
	if mainSym == nil {
		t.Fatal("main function not found")
		return
	}

	// Extract calls from main
	calls, err := extractor.ExtractCalls(ctx, *mainSym, content)
	if err != nil {
		t.Fatalf("ExtractCalls failed: %v", err)
	}

	// Should find helper and doSomething
	callSet := make(map[string]bool)
	for _, c := range calls {
		callSet[c] = true
	}

	if !callSet["helper"] {
		t.Error("expected call to 'helper'")
	}
	if !callSet["doSomething"] {
		t.Error("expected call to 'doSomething'")
	}
}

// TestGoExtractor_ExtractCalls_QualifiedCalls tests that ExtractCalls identifies
// qualified calls like pkg.Func or receiver.Method.
func TestGoExtractor_ExtractCalls_QualifiedCalls(t *testing.T) {
	extractor := NewGoExtractor()

	content := []byte(`package test

import "fmt"

type Service struct{}

func (s *Service) Run() {}

func process(svc *Service) {
	fmt.Println("processing")
	svc.Run()
}
`)

	ctx := context.Background()
	symbols, err := extractor.Extract(ctx, "qualified.go", content)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// Find process function
	var processSym *Symbol
	for i := range symbols {
		if symbols[i].Name == "process" {
			processSym = &symbols[i]
			break
		}
	}
	if processSym == nil {
		t.Fatal("process function not found")
		return
	}

	// Extract calls from process
	calls, err := extractor.ExtractCalls(ctx, *processSym, content)
	if err != nil {
		t.Fatalf("ExtractCalls failed: %v", err)
	}

	callSet := make(map[string]bool)
	for _, c := range calls {
		callSet[c] = true
	}

	// Should find fmt.Println and svc.Run (or just Run depending on impl)
	if !callSet["fmt.Println"] {
		t.Error("expected call to 'fmt.Println'")
	}
	if !callSet["svc.Run"] && !callSet["Run"] {
		t.Error("expected call to 'svc.Run' or 'Run'")
	}
}

// TestGoExtractor_ExtractCalls_NoCalls tests that ExtractCalls returns empty
// for functions with no calls.
func TestGoExtractor_ExtractCalls_NoCalls(t *testing.T) {
	extractor := NewGoExtractor()

	content := []byte(`package test

func returnValue() int {
	return 42
}
`)

	ctx := context.Background()
	symbols, err := extractor.Extract(ctx, "nocalls.go", content)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if len(symbols) == 0 {
		t.Fatal("expected at least one symbol")
		return
	}

	calls, err := extractor.ExtractCalls(ctx, symbols[0], content)
	if err != nil {
		t.Fatalf("ExtractCalls failed: %v", err)
	}

	if len(calls) != 0 {
		t.Errorf("expected 0 calls, got %d: %v", len(calls), calls)
	}
}

// TestGoExtractor_ExtractCalls_NestedCalls tests that ExtractCalls identifies
// calls within nested expressions and closures.
func TestGoExtractor_ExtractCalls_NestedCalls(t *testing.T) {
	extractor := NewGoExtractor()

	content := []byte(`package test

func outer() {}
func inner() int { return 1 }
func wrapper() int { return 2 }

func complex() {
	if inner() > 0 {
		outer()
	}
	fn := func() {
		wrapper()
	}
	fn()
}
`)

	ctx := context.Background()
	symbols, err := extractor.Extract(ctx, "nested.go", content)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// Find complex function
	var complexSym *Symbol
	for i := range symbols {
		if symbols[i].Name == "complex" {
			complexSym = &symbols[i]
			break
		}
	}
	if complexSym == nil {
		t.Fatal("complex function not found")
		return
	}

	calls, err := extractor.ExtractCalls(ctx, *complexSym, content)
	if err != nil {
		t.Fatalf("ExtractCalls failed: %v", err)
	}

	callSet := make(map[string]bool)
	for _, c := range calls {
		callSet[c] = true
	}

	// Should find inner, outer, wrapper (from closure), and fn
	if !callSet["inner"] {
		t.Error("expected call to 'inner'")
	}
	if !callSet["outer"] {
		t.Error("expected call to 'outer'")
	}
	if !callSet["wrapper"] {
		t.Error("expected call to 'wrapper'")
	}
}

// TestGoExtractor_ExtractCalls_InvalidBounds tests that ExtractCalls handles
// invalid symbol bounds gracefully.
func TestGoExtractor_ExtractCalls_InvalidBounds(t *testing.T) {
	extractor := NewGoExtractor()
	ctx := context.Background()
	content := []byte("package test\nfunc foo() {}")

	// Symbol with invalid bounds
	invalidSym := Symbol{
		ID:        "test.go:invalid",
		FilePath:  "test.go",
		Name:      "invalid",
		StartByte: -1,
		EndByte:   100,
	}

	calls, err := extractor.ExtractCalls(ctx, invalidSym, content)
	if err != nil {
		t.Errorf("ExtractCalls should not error on invalid bounds: %v", err)
	}
	if len(calls) > 0 {
		t.Errorf("expected empty calls for invalid bounds, got %v", calls)
	}
}

func TestSymbolID(t *testing.T) {
	id := ID("pkg/auth/login.go", "Login")
	expected := "pkg/auth/login.go:Login"
	if id != expected {
		t.Errorf("expected %q, got %q", expected, id)
	}
}

func TestEntryName(t *testing.T) {
	name := EntryName("workspace-123", "pkg/auth/login.go", "Login")
	expected := "symbol://workspace-123/pkg/auth/login.go:Login"
	if name != expected {
		t.Errorf("expected %q, got %q", expected, name)
	}
}

func TestFileMetaEntryName(t *testing.T) {
	name := FileMetaEntryName("workspace-123", "pkg/auth/login.go")
	expected := "symbol-meta://workspace-123/pkg/auth/login.go"
	if name != expected {
		t.Errorf("expected %q, got %q", expected, name)
	}
}

func TestComputeDigest(t *testing.T) {
	content := []byte("hello world")
	digest := ComputeDigest(content)
	if !hasPrefix(digest, "sha256:") {
		t.Errorf("expected sha256 prefix, got %q", digest)
	}
	// Should be deterministic
	digest2 := ComputeDigest(content)
	if digest != digest2 {
		t.Error("digest should be deterministic")
	}
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func TestIndexer_ErrUnchanged(t *testing.T) {
	idx, _, workspaceDir, workspaceID := setupTestIndexer(t, Config{Enabled: true})

	goContent := `package main

func Greet() string {
	return "Hello"
}
`
	createTestFile(t, workspaceDir, "greet.go", goContent)

	event := indexing.PostReviewEvent{
		WorkspaceID: workspaceID,
		Files: []indexing.FileChange{
			{Path: "greet.go", ChangeKind: indexing.ChangeKindModified, Language: "go"},
		},
	}

	// First index should succeed
	result1, err := idx.Index(context.Background(), event)
	if err != nil {
		t.Fatalf("First index failed: %v", err)
	}
	if result1.FilesIndexed != 1 {
		t.Errorf("expected 1 file indexed on first run, got %d", result1.FilesIndexed)
	}

	// Second index with same content should be skipped (not failed)
	result2, err := idx.Index(context.Background(), event)
	if err != nil {
		t.Fatalf("Second index failed: %v", err)
	}
	if result2.FilesSkipped != 1 {
		t.Errorf("expected 1 file skipped on second run (unchanged), got skipped=%d indexed=%d failed=%d",
			result2.FilesSkipped, result2.FilesIndexed, result2.FilesFailed)
	}
	if result2.FilesFailed != 0 {
		t.Errorf("unchanged file should not be counted as failed, got %d", result2.FilesFailed)
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || containsSubstring(s, substr)))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestIndexer_PerSymbolIncrementality tests that only changed symbols are re-indexed
// per spec §4.3.
func TestIndexer_PerSymbolIncrementality(t *testing.T) {
	idx, store, workspaceDir, workspaceID := setupTestIndexer(t, Config{Enabled: true})

	// Initial content with two functions
	initialContent := `package main

func First() string {
	return "first"
}

func Second() string {
	return "second"
}
`
	createTestFile(t, workspaceDir, "funcs.go", initialContent)

	ctx := context.Background()
	event := indexing.PostReviewEvent{
		WorkspaceID: workspaceID,
		Files: []indexing.FileChange{
			{Path: "funcs.go", ChangeKind: indexing.ChangeKindAdded, Language: "go"},
		},
	}

	// First index
	result1, err := idx.Index(ctx, event)
	if err != nil {
		t.Fatalf("First index failed: %v", err)
	}
	if result1.FilesIndexed != 1 {
		t.Errorf("expected 1 file indexed, got %d", result1.FilesIndexed)
	}

	// Get initial digests from file meta
	metaName := FileMetaEntryName(workspaceID, "funcs.go")
	metaEntry, err := store.Get(ctx, metaName, workspaceID)
	if err != nil {
		t.Fatalf("Get meta failed: %v", err)
	}
	meta1, err := UnmarshalFileMeta(metaEntry.Result)
	if err != nil {
		t.Fatalf("failed to unmarshal file meta: %v", err)
	}
	if len(meta1.SymbolDigests) < 2 {
		t.Fatalf("expected at least 2 symbol digests, got %d", len(meta1.SymbolDigests))
	}

	// Get First function's entry
	firstEntry1, err := store.Get(ctx, keyEntryName(workspaceID, "funcs.go", "First"), workspaceID)
	if err != nil {
		t.Fatalf("Get First failed: %v", err)
	}
	firstResult1, err := UnmarshalResult(firstEntry1.Result)
	if err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	firstDigest1 := firstResult1.Symbol.BodyDigest

	// Modify ONLY Second function
	modifiedContent := `package main

func First() string {
	return "first"
}

func Second() string {
	return "modified second"
}
`
	createTestFile(t, workspaceDir, "funcs.go", modifiedContent)
	event.Files[0].ChangeKind = indexing.ChangeKindModified

	// Second index
	result2, err := idx.Index(ctx, event)
	if err != nil {
		t.Fatalf("Second index failed: %v", err)
	}
	if result2.FilesIndexed != 1 {
		t.Errorf("expected 1 file indexed, got %d", result2.FilesIndexed)
	}

	// Verify First function was NOT re-saved (same body_digest)
	firstEntry2, err := store.Get(ctx, keyEntryName(workspaceID, "funcs.go", "First"), workspaceID)
	if err != nil {
		t.Fatalf("Get First after update failed: %v", err)
	}
	firstResult2, err := UnmarshalResult(firstEntry2.Result)
	if err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	// The body digest should be the same
	if firstResult2.Symbol.BodyDigest != firstDigest1 {
		t.Error("First function's body_digest changed when it shouldn't have")
	}

	// Verify Second function WAS updated
	secondEntry, err := store.Get(ctx, keyEntryName(workspaceID, "funcs.go", "Second"), workspaceID)
	if err != nil {
		t.Fatalf("Get Second failed: %v", err)
	}
	secondResult, err := UnmarshalResult(secondEntry.Result)
	if err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	// The body should contain "modified"
	if secondResult.Symbol.BodyDigest == meta1.SymbolDigests["funcs.go:Second"] {
		t.Error("Second function's body_digest should have changed")
	}
}

// TestIndexer_SymbolDeletion tests that removed symbols are deleted from the index
// per spec §4.3.
func TestIndexer_SymbolDeletion(t *testing.T) {
	idx, store, workspaceDir, workspaceID := setupTestIndexer(t, Config{Enabled: true})

	// Initial content with two functions
	initialContent := `package main

func KeepMe() {}
func DeleteMe() {}
`
	createTestFile(t, workspaceDir, "deletion.go", initialContent)

	ctx := context.Background()
	event := indexing.PostReviewEvent{
		WorkspaceID: workspaceID,
		Files: []indexing.FileChange{
			{Path: "deletion.go", ChangeKind: indexing.ChangeKindAdded, Language: "go"},
		},
	}

	// First index
	_, err := idx.Index(ctx, event)
	if err != nil {
		t.Fatalf("First index failed: %v", err)
	}

	// Verify both symbols exist
	_, err = store.Get(ctx, keyEntryName(workspaceID, "deletion.go", "KeepMe"), workspaceID)
	if err != nil {
		t.Fatalf("KeepMe should exist: %v", err)
	}
	_, err = store.Get(ctx, keyEntryName(workspaceID, "deletion.go", "DeleteMe"), workspaceID)
	if err != nil {
		t.Fatalf("DeleteMe should exist: %v", err)
	}

	// Remove DeleteMe function
	modifiedContent := `package main

func KeepMe() {}
`
	createTestFile(t, workspaceDir, "deletion.go", modifiedContent)
	event.Files[0].ChangeKind = indexing.ChangeKindModified

	// Second index
	_, err = idx.Index(ctx, event)
	if err != nil {
		t.Fatalf("Second index failed: %v", err)
	}

	// Verify KeepMe still exists
	_, err = store.Get(ctx, keyEntryName(workspaceID, "deletion.go", "KeepMe"), workspaceID)
	if err != nil {
		t.Fatalf("KeepMe should still exist: %v", err)
	}

	// Verify DeleteMe is gone
	_, err = store.Get(ctx, keyEntryName(workspaceID, "deletion.go", "DeleteMe"), workspaceID)
	if err == nil {
		t.Error("DeleteMe should have been deleted")
	}
}

// TestIndexer_LargeFileThreshold tests that files exceeding MaxFileKB are skipped
// per spec §4.2.
func TestIndexer_LargeFileThreshold(t *testing.T) {
	// Set a small MaxFileKB for testing
	idx, _, workspaceDir, workspaceID := setupTestIndexer(t, Config{
		Enabled:   true,
		MaxFileKB: 1, // 1KB limit
	})

	// Create a file larger than 1KB
	largeContent := "package main\n\n" + string(make([]byte, 2*1024)) // ~2KB
	createTestFile(t, workspaceDir, "large.go", largeContent)

	event := indexing.PostReviewEvent{
		WorkspaceID: workspaceID,
		Files: []indexing.FileChange{
			{Path: "large.go", ChangeKind: indexing.ChangeKindAdded, Language: "go"},
		},
	}

	result, err := idx.Index(context.Background(), event)
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	// File should be skipped due to size and counted as skipped (not indexed).
	if result.FilesIndexed != 0 {
		t.Errorf("expected 0 files indexed for large file, got %d", result.FilesIndexed)
	}
	if result.FilesSkipped != 1 {
		t.Errorf("expected 1 file skipped for large file, got indexed=%d skipped=%d failed=%d",
			result.FilesIndexed, result.FilesSkipped, result.FilesFailed)
	}
}
