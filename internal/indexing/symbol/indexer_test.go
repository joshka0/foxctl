package symbol

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jkatigb/agentctl/internal/indexing"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/rs/zerolog"
)

func setupTestIndexer(t *testing.T, cfg Config) (*Indexer, *memory.Store, string) {
	t.Helper()

	tmpDir := t.TempDir()
	workspaceDir := filepath.Join(tmpDir, "workspace")
	storageDir := filepath.Join(tmpDir, "storage")
	casDir := filepath.Join(tmpDir, "cas")

	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	store, err := memory.Open(ctx, storageDir, casDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	logger := zerolog.Nop()
	idx := NewIndexer(cfg, store, nil, workspaceDir, logger)

	return idx, store, workspaceDir
}

func createTestFile(t *testing.T, dir, path, content string) {
	t.Helper()
	fullPath := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestIndexer_ID(t *testing.T) {
	idx, _, _ := setupTestIndexer(t, Config{Enabled: true})
	if idx.ID() != IndexerID {
		t.Errorf("expected ID %q, got %q", IndexerID, idx.ID())
	}
}

func TestIndexer_Index_Disabled(t *testing.T) {
	idx, _, _ := setupTestIndexer(t, Config{Enabled: false})

	event := indexing.PostReviewEvent{
		WorkspaceID: "ws-1",
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
	idx, store, workspaceDir := setupTestIndexer(t, Config{Enabled: true})

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
		WorkspaceID: "ws-go-test",
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
	greetName := EntryName("ws-go-test", "main.go", "Greet")
	greetEntry, err := store.Get(ctx, greetName, "ws-go-test")
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
	userName := EntryName("ws-go-test", "main.go", "User")
	userEntry, err := store.Get(ctx, userName, "ws-go-test")
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
	getNameName := EntryName("ws-go-test", "main.go", "User.GetName")
	getNameEntry, err := store.Get(ctx, getNameName, "ws-go-test")
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
	metaName := FileMetaEntryName("ws-go-test", "main.go")
	metaEntry, err := store.Get(ctx, metaName, "ws-go-test")
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
	idx, _, workspaceDir := setupTestIndexer(t, Config{Enabled: true})

	createTestFile(t, workspaceDir, "data.json", `{"key": "value"}`)

	event := indexing.PostReviewEvent{
		WorkspaceID: "ws-unsupported",
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
	idx, store, workspaceDir := setupTestIndexer(t, Config{Enabled: true})

	// First, index a file
	createTestFile(t, workspaceDir, "to_delete.go", "package main\n\nfunc Delete() {}\n")

	ctx := context.Background()
	addEvent := indexing.PostReviewEvent{
		WorkspaceID: "ws-delete",
		Files: []indexing.FileChange{
			{Path: "to_delete.go", ChangeKind: indexing.ChangeKindAdded, Language: "go"},
		},
	}
	_, err := idx.Index(ctx, addEvent)
	if err != nil {
		t.Fatalf("Initial index failed: %v", err)
	}

	// Verify file meta exists
	metaName := FileMetaEntryName("ws-delete", "to_delete.go")
	_, err = store.Get(ctx, metaName, "ws-delete")
	if err != nil {
		t.Fatalf("File meta should exist: %v", err)
	}

	// Delete the file
	deleteEvent := indexing.PostReviewEvent{
		WorkspaceID: "ws-delete",
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
	_, err = store.Get(ctx, metaName, "ws-delete")
	if err == nil {
		t.Error("file meta should be deleted")
	}
}

func TestIndexer_Index_IncrementalUpdate(t *testing.T) {
	idx, store, workspaceDir := setupTestIndexer(t, Config{Enabled: true})

	// Initial content
	createTestFile(t, workspaceDir, "incremental.go", "package main\n\nfunc First() {}\n")

	ctx := context.Background()
	event := indexing.PostReviewEvent{
		WorkspaceID: "ws-incremental",
		Files: []indexing.FileChange{
			{Path: "incremental.go", ChangeKind: indexing.ChangeKindAdded, Language: "go"},
		},
	}

	_, err := idx.Index(ctx, event)
	if err != nil {
		t.Fatalf("First index failed: %v", err)
	}

	// Get initial digest
	metaName := FileMetaEntryName("ws-incremental", "incremental.go")
	metaEntry, err := store.Get(ctx, metaName, "ws-incremental")
	if err != nil {
		t.Fatalf("Get meta failed: %v", err)
	}
	firstMeta, _ := UnmarshalFileMeta(metaEntry.Result)

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
	metaEntry, err = store.Get(ctx, metaName, "ws-incremental")
	if err != nil {
		t.Fatalf("Get updated meta failed: %v", err)
	}
	secondMeta, _ := UnmarshalFileMeta(metaEntry.Result)

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
	}
	if handlerSym.Kind != KindInterface {
		t.Errorf("expected kind 'interface', got %q", handlerSym.Kind)
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
