package codecontext_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/codecontext"
	"github.com/jkatigb/agentctl/internal/domain/policy"
)

// createTestFile creates a temporary file with the given content.
func createTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	return path
}

// createTestDir creates a temporary directory for tests.
func createTestDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "codecontext-collect-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestCollect_BasicEvidence(t *testing.T) {
	dir := createTestDir(t)
	content := `package main

func Hello() {
	println("Hello, World!")
}

func Goodbye() {
	println("Goodbye!")
}
`
	createTestFile(t, dir, "main.go", content)

	validator, err := policy.NewPathValidator(dir, nil)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	ctx := context.Background()
	evidence, err := codecontext.Collect(ctx, codecontext.CollectOpts{
		Candidates: []codecontext.Candidate{
			{Path: "main.go", Priority: 1.0},
		},
		Query:         "Hello function",
		PathValidator: validator,
	})
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if evidence.Stats.FilesProcessed != 1 {
		t.Errorf("FilesProcessed = %d, want 1", evidence.Stats.FilesProcessed)
	}
	if len(evidence.Snippets) == 0 {
		t.Error("Expected at least one snippet")
	}

	// Should find the Hello function
	found := false
	for _, s := range evidence.Snippets {
		if strings.Contains(s.Text, "Hello") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected to find snippet containing 'Hello'")
	}
}

func TestCollect_WithLineHint(t *testing.T) {
	dir := createTestDir(t)
	content := `line 1
line 2
line 3
line 4
line 5
line 6
line 7
line 8
line 9
line 10
`
	createTestFile(t, dir, "test.txt", content)

	validator, err := policy.NewPathValidator(dir, nil)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	ctx := context.Background()
	evidence, err := codecontext.Collect(ctx, codecontext.CollectOpts{
		Candidates: []codecontext.Candidate{
			{Path: "test.txt", LineHint: 5, Priority: 1.0},
		},
		PathValidator: validator,
		ContextLines:  2,
	})
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if len(evidence.Snippets) != 1 {
		t.Fatalf("Expected 1 snippet, got %d", len(evidence.Snippets))
	}

	snippet := evidence.Snippets[0]
	// With line hint 5 and context 2, should get lines 3-7
	if snippet.StartLine > 5 || snippet.EndLine < 5 {
		t.Errorf("Snippet should include line 5, got lines %d-%d", snippet.StartLine, snippet.EndLine)
	}
	if !strings.Contains(snippet.Text, "line 5") {
		t.Errorf("Snippet should contain 'line 5', got: %s", snippet.Text)
	}
}

func TestCollect_PriorityOrder(t *testing.T) {
	dir := createTestDir(t)
	createTestFile(t, dir, "low.txt", "low priority content")
	createTestFile(t, dir, "high.txt", "high priority content")

	validator, err := policy.NewPathValidator(dir, nil)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	ctx := context.Background()
	evidence, err := codecontext.Collect(ctx, codecontext.CollectOpts{
		Candidates: []codecontext.Candidate{
			{Path: "low.txt", Priority: 0.1},
			{Path: "high.txt", Priority: 0.9},
		},
		PathValidator: validator,
		MaxFiles:      2,
	})
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if evidence.Stats.FilesProcessed != 2 {
		t.Errorf("FilesProcessed = %d, want 2", evidence.Stats.FilesProcessed)
	}

	// First snippet should be from high priority file
	if len(evidence.Snippets) > 0 && !strings.Contains(evidence.Snippets[0].File, "high.txt") {
		t.Errorf("First snippet should be from high.txt, got %s", evidence.Snippets[0].File)
	}
}

func TestCollect_MaxFilesLimit(t *testing.T) {
	dir := createTestDir(t)
	for i := 1; i <= 10; i++ {
		createTestFile(t, dir, "file"+string(rune('0'+i))+".txt", "content")
	}

	validator, err := policy.NewPathValidator(dir, nil)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	candidates := make([]codecontext.Candidate, 10)
	for i := 0; i < 10; i++ {
		candidates[i] = codecontext.Candidate{
			Path:     "file" + string(rune('0'+i+1)) + ".txt",
			Priority: float64(i) / 10.0,
		}
	}

	ctx := context.Background()
	evidence, err := codecontext.Collect(ctx, codecontext.CollectOpts{
		Candidates:    candidates,
		PathValidator: validator,
		MaxFiles:      3,
	})
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if evidence.Stats.FilesProcessed != 3 {
		t.Errorf("FilesProcessed = %d, want 3", evidence.Stats.FilesProcessed)
	}
	if !evidence.Truncated {
		t.Error("Expected Truncated = true")
	}
}

func TestCollect_DeduplicatesPaths(t *testing.T) {
	dir := createTestDir(t)
	createTestFile(t, dir, "test.txt", "content")

	validator, err := policy.NewPathValidator(dir, nil)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	ctx := context.Background()
	evidence, err := codecontext.Collect(ctx, codecontext.CollectOpts{
		Candidates: []codecontext.Candidate{
			{Path: "test.txt", Priority: 1.0},
			{Path: "test.txt", Priority: 0.5}, // duplicate
			{Path: "test.txt", Priority: 0.1}, // duplicate
		},
		PathValidator: validator,
	})
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if evidence.Stats.FilesProcessed != 1 {
		t.Errorf("FilesProcessed = %d, want 1 (duplicates should be skipped)", evidence.Stats.FilesProcessed)
	}
}

func TestCollect_RecordsFileErrors(t *testing.T) {
	dir := createTestDir(t)
	createTestFile(t, dir, "exists.txt", "content")

	validator, err := policy.NewPathValidator(dir, nil)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	ctx := context.Background()
	evidence, err := codecontext.Collect(ctx, codecontext.CollectOpts{
		Candidates: []codecontext.Candidate{
			{Path: "exists.txt", Priority: 1.0},
			{Path: "nonexistent.txt", Priority: 0.5},
		},
		PathValidator: validator,
	})
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if evidence.Stats.FilesProcessed != 1 {
		t.Errorf("FilesProcessed = %d, want 1", evidence.Stats.FilesProcessed)
	}
	if evidence.Stats.FilesSkipped != 1 {
		t.Errorf("FilesSkipped = %d, want 1", evidence.Stats.FilesSkipped)
	}
	if len(evidence.Stats.FileErrors) != 1 {
		t.Errorf("FileErrors length = %d, want 1", len(evidence.Stats.FileErrors))
	}
	if evidence.Stats.FileErrors[0].Code != "ENOTFOUND" {
		t.Errorf("FileErrors[0].Code = %q, want 'ENOTFOUND'", evidence.Stats.FileErrors[0].Code)
	}
}

func TestCollect_RequiresValidator(t *testing.T) {
	ctx := context.Background()
	_, err := codecontext.Collect(ctx, codecontext.CollectOpts{
		Candidates: []codecontext.Candidate{
			{Path: "test.txt"},
		},
		// No PathValidator
	})
	if err == nil {
		t.Fatal("Expected error when PathValidator is nil")
	}
}

func TestCollect_SkipsEmptyPaths(t *testing.T) {
	dir := createTestDir(t)
	createTestFile(t, dir, "test.txt", "content")

	validator, err := policy.NewPathValidator(dir, nil)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	ctx := context.Background()
	evidence, err := codecontext.Collect(ctx, codecontext.CollectOpts{
		Candidates: []codecontext.Candidate{
			{Path: "", Priority: 1.0}, // empty, should skip
			{Path: "test.txt", Priority: 0.5},
		},
		PathValidator: validator,
	})
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if evidence.Stats.FilesProcessed != 1 {
		t.Errorf("FilesProcessed = %d, want 1", evidence.Stats.FilesProcessed)
	}
}
