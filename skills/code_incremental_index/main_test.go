package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/langutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skilltest"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedding"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/embeddingtext"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/symbolutil"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/memory"
)

// applyDefaultsAndValidate applies defaults and validates required fields (mirrors run function).
func applyDefaultsAndValidate(in *input, workspace string) error {
	// Validate required field
	if in.File == "" {
		return fmt.Errorf("file is required")
	}
	// Apply defaults
	if in.WorkspaceID == "" {
		in.WorkspaceID = workspace
	}
	if in.Symbols == nil {
		t := true
		in.Symbols = &t
	}
	return nil
}

// parseInput is a test helper that parses JSON, applies defaults, and validates.
func parseInput(r io.Reader, workspace string) (input, error) {
	in, err := skilltest.ParseInput[input](r)
	if err != nil {
		return in, err
	}
	if err := applyDefaultsAndValidate(&in, workspace); err != nil {
		return in, err
	}
	return in, nil
}

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"foo.go", "go"},
		{"bar.py", "python"},
		{"baz.js", "javascript"},
		{"qux.jsx", "javascript"},
		{"main.ts", "typescript"},
		{"component.tsx", "typescript"},
		{"player.gd", ""}, // gdscript not supported by extractSymbols
		{"README.md", ""},
		{"data.json", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := langutil.DetectAllowed(tt.path, langutil.CommonCodeLanguages)
			if got != tt.want {
				t.Errorf("detectLanguage(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestExtractSymbols_Go(t *testing.T) {
	content := []byte(`package main

import "fmt"

// Hello prints a greeting.
func Hello(name string) {
	fmt.Println("Hello,", name)
}

type User struct {
	Name string
	Age  int
}

func (u *User) Greet() string {
	return "Hello, " + u.Name
}
`)

	symbols, err := extractSymbols(context.Background(), "go", "test.go", content)
	if err != nil {
		t.Fatalf("extractSymbols failed: %v", err)
	}

	// Should have: Hello (function), User (struct), User.Greet (method)
	if len(symbols) < 3 {
		t.Errorf("expected at least 3 symbols, got %d", len(symbols))
	}

	// Verify Hello function
	var found bool
	for _, s := range symbols {
		if s.Name == "Hello" && s.Kind == symbol.KindFunction {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find Hello function")
	}

	// Verify User struct
	found = false
	for _, s := range symbols {
		if s.Name == "User" && s.Kind == symbol.KindStruct {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find User struct")
	}

	// Verify User.Greet method
	found = false
	for _, s := range symbols {
		if s.Name == "User.Greet" && s.Kind == symbol.KindMethod {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find User.Greet method")
	}
}

func TestExtractSymbols_Python(t *testing.T) {
	content := []byte(`
def hello(name):
    print(f"Hello, {name}")

class User:
    def __init__(self, name):
        self.name = name

    def greet(self):
        return f"Hello, {self.name}"

def goodbye():
    print("Goodbye!")
`)

	symbols, err := extractSymbols(context.Background(), "python", "test.py", content)
	if err != nil {
		t.Fatalf("extractSymbols failed: %v", err)
	}

	// Should have: hello, User, goodbye (we don't extract methods inside classes with simple regex)
	if len(symbols) < 3 {
		t.Errorf("expected at least 3 symbols, got %d", len(symbols))
	}

	names := make(map[string]bool)
	for _, s := range symbols {
		names[s.Name] = true
	}

	if !names["hello"] {
		t.Error("expected to find hello function")
	}
	if !names["User"] {
		t.Error("expected to find User class")
	}
	if !names["goodbye"] {
		t.Error("expected to find goodbye function")
	}
}

func TestExtractSymbols_TypeScript(t *testing.T) {
	content := []byte(`
function hello(name: string): void {
    console.log("Hello, " + name);
}

class User {
    constructor(public name: string) {}

    greet(): string {
        return "Hello, " + this.name;
    }
}

interface Greeter {
    greet(): string;
}

export function goodbye(): void {
    console.log("Goodbye!");
}
`)

	symbols, err := extractSymbols(context.Background(), "typescript", "test.ts", content)
	if err != nil {
		t.Fatalf("extractSymbols failed: %v", err)
	}

	// Should have: hello, User, Greeter, goodbye
	if len(symbols) < 4 {
		t.Errorf("expected at least 4 symbols, got %d", len(symbols))
	}

	names := make(map[string]bool)
	for _, s := range symbols {
		names[s.Name] = true
	}

	if !names["hello"] {
		t.Error("expected to find hello function")
	}
	if !names["User"] {
		t.Error("expected to find User class")
	}
	if !names["Greeter"] {
		t.Error("expected to find Greeter interface")
	}
	if !names["goodbye"] {
		t.Error("expected to find goodbye function")
	}
}

func TestParseInput(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid", `{"file": "test.go"}`, false},
		{"with workspace", `{"file": "test.go", "workspace_id": "myws"}`, false},
		{"missing file", `{"workspace_id": "test"}`, true},
		{"empty", `{}`, true},
		{"invalid json", `{invalid}`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile, err := os.CreateTemp("", "input*.json")
			if err != nil {
				t.Fatal(err)
			}
			defer os.Remove(tmpFile.Name())

			if _, err := tmpFile.WriteString(tt.input); err != nil {
				t.Fatal(err)
			}
			if _, err := tmpFile.Seek(0, 0); err != nil {
				t.Fatal(err)
			}

			_, err = parseInput(tmpFile, "/test/workspace")
			if (err != nil) != tt.wantErr {
				t.Errorf("parseInput() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestExtractSymbols_EmptyFile(t *testing.T) {
	symbols, err := extractSymbols(context.Background(), "go", "empty.go", []byte("package main\n"))
	if err != nil {
		t.Fatalf("extractSymbols failed: %v", err)
	}
	// Empty file should return no symbols
	if len(symbols) != 0 {
		t.Errorf("expected 0 symbols for empty file, got %d", len(symbols))
	}
}

func TestExtractSymbols_UnsupportedLanguage(t *testing.T) {
	_, err := extractSymbols(context.Background(), "rust", "test.rs", []byte("fn main() {}"))
	if err == nil {
		t.Error("expected error for unsupported language")
	}
}

// Integration test helper - creates a temp file and tests extraction
func TestExtractSymbols_RealFile(t *testing.T) {
	// Create a temp Go file
	tmpDir := t.TempDir()
	goFile := filepath.Join(tmpDir, "test.go")

	content := []byte(`package test

func Add(a, b int) int {
	return a + b
}

func Subtract(a, b int) int {
	return a - b
}
`)

	if err := os.WriteFile(goFile, content, 0o644); err != nil {
		t.Fatal(err)
	}

	// Read and extract
	fileContent, err := os.ReadFile(goFile)
	if err != nil {
		t.Fatal(err)
	}

	symbols, err := extractSymbols(context.Background(), "go", "test.go", fileContent)
	if err != nil {
		t.Fatal(err)
	}

	if len(symbols) != 2 {
		t.Errorf("expected 2 symbols (Add, Subtract), got %d", len(symbols))
	}
}

func TestUpsertSymbolsUsesPackageScopedSymbolKeys(t *testing.T) {
	ctx := context.Background()
	store, err := memory.Open(ctx, t.TempDir(), "")
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer store.Close()

	workspace := "ws"
	symbolsA := []symbol.Symbol{{
		ID:       symbol.ID("pkg/a/foo.go", "New"),
		FilePath: "pkg/a/foo.go",
		Name:     "New",
		Language: "go",
		Kind:     symbol.KindFunction,
		Key:      symbol.GoSymbolKey("New"),
	}}
	symbolsB := []symbol.Symbol{{
		ID:       symbol.ID("pkg/b/foo.go", "New"),
		FilePath: "pkg/b/foo.go",
		Name:     "New",
		Language: "go",
		Kind:     symbol.KindFunction,
		Key:      symbol.GoSymbolKey("New"),
	}}

	if updated, deleted, err := upsertSymbols(ctx, store, workspace, "pkg/a/foo.go", "go", "session", symbolsA); err != nil {
		t.Fatalf("upsert package a: %v", err)
	} else if updated != 1 || deleted != 0 {
		t.Fatalf("upsert package a updated/deleted = %d/%d, want 1/0", updated, deleted)
	}
	if updated, deleted, err := upsertSymbols(ctx, store, workspace, "pkg/b/foo.go", "go", "session", symbolsB); err != nil {
		t.Fatalf("upsert package b: %v", err)
	} else if updated != 1 || deleted != 0 {
		t.Fatalf("upsert package b updated/deleted = %d/%d, want 1/0", updated, deleted)
	}

	entries, total, err := store.ListFiltered(ctx, workspace, storage.MemoryListFilter{Types: []string{symbol.SymbolType}}, 10, 0)
	if err != nil {
		t.Fatalf("list symbols: %v", err)
	}
	if total != 2 {
		t.Fatalf("stored symbols = %d, want 2", total)
	}

	wantNames := map[string]bool{
		symbolutil.KeyEntryName(workspace, symbolutil.DeriveSymbolPackage("pkg/a/foo.go", "go"), "New"): false,
		symbolutil.KeyEntryName(workspace, symbolutil.DeriveSymbolPackage("pkg/b/foo.go", "go"), "New"): false,
	}
	for _, entry := range entries {
		if _, ok := wantNames[entry.Name]; ok {
			wantNames[entry.Name] = true
		}
	}
	for name, found := range wantNames {
		if !found {
			t.Fatalf("missing package-scoped symbol entry %q", name)
		}
	}
}

func TestQueueEmbeddingsUsesEffectiveIDAndSymbolKeyDigest(t *testing.T) {
	ctx := context.Background()
	cacheRoot := t.TempDir()
	workspace := "ws"
	model := "test-model"
	fileContent := []byte(`package p

func legacy() string {
	return "stable"
}
`)
	sym := symbol.Symbol{
		ID:         symbol.ID("pkg/p/legacy.go", "legacy"),
		FilePath:   "pkg/p/legacy.go",
		Name:       "legacy",
		Language:   "go",
		Kind:       symbol.KindFunction,
		StartLine:  3,
		EndLine:    5,
		Signature:  "func legacy() string",
		BodyDigest: "sha256:body",
		Key:        symbol.GoNonExportedSymbolKey("legacy", "legacy.go"),
	}

	queued, skipped := queueEmbeddings(ctx, cacheRoot, workspace, []symbol.Symbol{sym}, fileContent, config.EmbedSymbolTextModeDocEnriched, model)
	if queued != 1 || skipped != 0 {
		t.Fatalf("queueEmbeddings queued/skipped = %d/%d, want 1/0", queued, skipped)
	}

	store, err := embedding.OpenStore(ctx, cacheRoot)
	if err != nil {
		t.Fatalf("open embedding store: %v", err)
	}
	defer store.Close()

	job, err := store.ClaimNext(ctx)
	if err != nil {
		t.Fatalf("claim job: %v", err)
	}
	if job == nil {
		t.Fatal("expected queued embedding job")
	}
	wantPkg := symbolutil.DeriveSymbolPackage(sym.FilePath, sym.Language)
	wantSymbolID := symbolutil.ScopedSymbolID(wantPkg, sym.EffectiveID())
	if job.SymbolID != wantSymbolID {
		t.Fatalf("job SymbolID = %q, want scoped symbol ID %q", job.SymbolID, wantSymbolID)
	}
	if job.SymbolID == sym.ID {
		t.Fatalf("job SymbolID used legacy ID %q", sym.ID)
	}
	if job.PackageID != wantPkg || job.SymbolKey != sym.EffectiveID() {
		t.Fatalf("job package/key = %q/%q, want %q/%q", job.PackageID, job.SymbolKey, wantPkg, sym.EffectiveID())
	}
	if job.MemoryName != symbolutil.KeyEntryName(workspace, wantPkg, sym.EffectiveID()) {
		t.Fatalf("job MemoryName = %q", job.MemoryName)
	}

	wantDigest := embeddingtext.BuildSymbolContentDigest(embeddingtext.SymbolDigestInput{
		Model:      model,
		Kind:       string(sym.Kind),
		Name:       sym.Name,
		SymbolKey:  sym.EffectiveID(),
		FilePath:   sym.FilePath,
		Signature:  sym.Signature,
		Doc:        sym.Documentation,
		BodyDigest: sym.BodyDigest,
	})
	if job.ContentDigest != wantDigest {
		t.Fatalf("job ContentDigest = %q, want key-aware digest %q", job.ContentDigest, wantDigest)
	}
}
