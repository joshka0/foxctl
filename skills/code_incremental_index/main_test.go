package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jkatigb/agentctl/internal/indexing/symbol"
)

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
			got := detectLanguage(tt.path)
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
