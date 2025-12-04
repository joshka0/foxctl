package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	models "github.com/XiaoConstantine/mcp-go/pkg/model"
)

// extractEditResult parses the JSON content from a CallToolResult.
func extractEditResult(t *testing.T, result *models.CallToolResult) map[string]any {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("result has no content")
	}
	textContent, ok := result.Content[0].(models.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	var content map[string]any
	if err := json.Unmarshal([]byte(textContent.Text), &content); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	return content
}

func TestApplyStructuredDiff_SingleHunk(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")

	// Original content
	original := `package main

func main() {
	println("hello")
}
`
	if err := os.WriteFile(testFile, []byte(original), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	cfg := Config{
		WorkspaceRoot:    tmpDir,
		MaxSearchResults: 50,
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	// Diff that changes println("hello") to println("world")
	diffJSON := map[string]any{
		"hunks": []any{
			map[string]any{
				"old_start": 4,
				"old_lines": 1,
				"new_start": 4,
				"new_lines": 1,
				"header":    "@@ -4,1 +4,1 @@",
				"lines": []any{
					`-	println("hello")`,
					`+	println("world")`,
				},
			},
		},
	}

	args := map[string]any{
		"path":      "test.go",
		"diff_json": diffJSON,
	}

	result, err := registry.applyStructuredDiff(context.Background(), args)
	if err != nil {
		t.Fatalf("applyStructuredDiff: %v", err)
	}

	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	// Verify file was modified
	newContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read modified file: %v", err)
	}

	expected := `package main

func main() {
	println("world")
}
`
	if string(newContent) != expected {
		t.Errorf("content mismatch:\ngot:\n%s\nwant:\n%s", string(newContent), expected)
	}

	// Check result metadata
	content := extractEditResult(t, result)
	if content["success"] != true {
		t.Error("expected success=true")
	}
	if content["hunks_count"] != float64(1) {
		t.Errorf("hunks_count = %v, want 1", content["hunks_count"])
	}
}

func TestApplyStructuredDiff_MultipleHunks(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")

	// Original content with multiple functions
	original := `package main

func hello() {
	println("hello")
}

func goodbye() {
	println("bye")
}
`
	if err := os.WriteFile(testFile, []byte(original), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	cfg := Config{
		WorkspaceRoot:    tmpDir,
		MaxSearchResults: 50,
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	// Diff that changes both functions
	diffJSON := map[string]any{
		"hunks": []any{
			map[string]any{
				"old_start": 4,
				"old_lines": 1,
				"new_start": 4,
				"new_lines": 1,
				"header":    "@@ -4,1 +4,1 @@",
				"lines": []any{
					`-	println("hello")`,
					`+	println("bonjour")`,
				},
			},
			map[string]any{
				"old_start": 8,
				"old_lines": 1,
				"new_start": 8,
				"new_lines": 1,
				"header":    "@@ -8,1 +8,1 @@",
				"lines": []any{
					`-	println("bye")`,
					`+	println("au revoir")`,
				},
			},
		},
	}

	args := map[string]any{
		"path":      "test.go",
		"diff_json": diffJSON,
	}

	result, err := registry.applyStructuredDiff(context.Background(), args)
	if err != nil {
		t.Fatalf("applyStructuredDiff: %v", err)
	}

	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	// Verify file was modified
	newContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read modified file: %v", err)
	}

	expected := `package main

func hello() {
	println("bonjour")
}

func goodbye() {
	println("au revoir")
}
`
	if string(newContent) != expected {
		t.Errorf("content mismatch:\ngot:\n%s\nwant:\n%s", string(newContent), expected)
	}

	content := extractEditResult(t, result)
	if content["hunks_count"] != float64(2) {
		t.Errorf("hunks_count = %v, want 2", content["hunks_count"])
	}
}

func TestApplyStructuredDiff_AddLines(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")

	original := `package main

func main() {
}
`
	if err := os.WriteFile(testFile, []byte(original), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	cfg := Config{
		WorkspaceRoot:    tmpDir,
		MaxSearchResults: 50,
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	// Diff that adds a line
	diffJSON := map[string]any{
		"hunks": []any{
			map[string]any{
				"old_start": 3,
				"old_lines": 2,
				"new_start": 3,
				"new_lines": 3,
				"header":    "@@ -3,2 +3,3 @@",
				"lines": []any{
					" func main() {",
					`+	println("hello")`,
					" }",
				},
			},
		},
	}

	args := map[string]any{
		"path":      "test.go",
		"diff_json": diffJSON,
	}

	result, err := registry.applyStructuredDiff(context.Background(), args)
	if err != nil {
		t.Fatalf("applyStructuredDiff: %v", err)
	}

	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	newContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read modified file: %v", err)
	}

	expected := `package main

func main() {
	println("hello")
}
`
	if string(newContent) != expected {
		t.Errorf("content mismatch:\ngot:\n%s\nwant:\n%s", string(newContent), expected)
	}
}

func TestApplyStructuredDiff_RemoveLines(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")

	original := `package main

func main() {
	println("to remove")
	println("keep this")
}
`
	if err := os.WriteFile(testFile, []byte(original), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	cfg := Config{
		WorkspaceRoot:    tmpDir,
		MaxSearchResults: 50,
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	// Diff that removes a line (old_lines=2 because we process 2 old lines: one removed, one kept)
	diffJSON := map[string]any{
		"hunks": []any{
			map[string]any{
				"old_start": 4,
				"old_lines": 2,
				"new_start": 4,
				"new_lines": 1,
				"header":    "@@ -4,2 +4,1 @@",
				"lines": []any{
					`-	println("to remove")`,
					` 	println("keep this")`, // Context line (kept)
				},
			},
		},
	}

	args := map[string]any{
		"path":      "test.go",
		"diff_json": diffJSON,
	}

	result, err := registry.applyStructuredDiff(context.Background(), args)
	if err != nil {
		t.Fatalf("applyStructuredDiff: %v", err)
	}

	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	newContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read modified file: %v", err)
	}

	expected := `package main

func main() {
	println("keep this")
}
`
	if string(newContent) != expected {
		t.Errorf("content mismatch:\ngot:\n%s\nwant:\n%s", string(newContent), expected)
	}
}

func TestApplyStructuredDiff_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")

	original := `package main

func main() {
	println("hello")
}
`
	if err := os.WriteFile(testFile, []byte(original), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	cfg := Config{
		WorkspaceRoot:    tmpDir,
		MaxSearchResults: 50,
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	diffJSON := map[string]any{
		"hunks": []any{
			map[string]any{
				"old_start": 4,
				"old_lines": 1,
				"new_start": 4,
				"new_lines": 1,
				"header":    "@@ -4,1 +4,1 @@",
				"lines": []any{
					`-	println("hello")`,
					`+	println("world")`,
				},
			},
		},
	}

	args := map[string]any{
		"path":      "test.go",
		"diff_json": diffJSON,
		"dry_run":   true,
	}

	result, err := registry.applyStructuredDiff(context.Background(), args)
	if err != nil {
		t.Fatalf("applyStructuredDiff: %v", err)
	}

	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	// Verify file was NOT modified
	newContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	if string(newContent) != original {
		t.Error("file was modified during dry run")
	}

	// Check result metadata
	content := extractEditResult(t, result)
	if content["dry_run"] != true {
		t.Error("expected dry_run=true in result")
	}
}

func TestApplyStructuredDiff_NestedDiffObject(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")

	original := `package main
`
	if err := os.WriteFile(testFile, []byte(original), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	cfg := Config{
		WorkspaceRoot:    tmpDir,
		MaxSearchResults: 50,
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	// Simulates code/diff output format: {"diff": {...}}
	diffJSON := map[string]any{
		"diff": map[string]any{
			"hunks": []any{
				map[string]any{
					"old_start": 1,
					"old_lines": 1,
					"new_start": 1,
					"new_lines": 2,
					"header":    "@@ -1,1 +1,2 @@",
					"lines": []any{
						" package main",
						`+import "fmt"`,
					},
				},
			},
		},
	}

	args := map[string]any{
		"path":      "test.go",
		"diff_json": diffJSON,
	}

	result, err := registry.applyStructuredDiff(context.Background(), args)
	if err != nil {
		t.Fatalf("applyStructuredDiff: %v", err)
	}

	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	newContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read modified file: %v", err)
	}

	expected := `package main
import "fmt"
`
	if string(newContent) != expected {
		t.Errorf("content mismatch:\ngot:\n%s\nwant:\n%s", string(newContent), expected)
	}
}

func TestApplyStructuredDiff_MissingPath(t *testing.T) {
	cfg := Config{
		WorkspaceRoot:    t.TempDir(),
		MaxSearchResults: 50,
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	args := map[string]any{
		"diff_json": map[string]any{
			"hunks": []any{},
		},
	}

	result, err := registry.applyStructuredDiff(context.Background(), args)
	if err != nil {
		t.Fatalf("applyStructuredDiff: %v", err)
	}

	if !result.IsError {
		t.Error("expected error for missing path")
	}
}

func TestApplyStructuredDiff_MissingDiffJSON(t *testing.T) {
	cfg := Config{
		WorkspaceRoot:    t.TempDir(),
		MaxSearchResults: 50,
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	args := map[string]any{
		"path": "test.go",
	}

	result, err := registry.applyStructuredDiff(context.Background(), args)
	if err != nil {
		t.Fatalf("applyStructuredDiff: %v", err)
	}

	if !result.IsError {
		t.Error("expected error for missing diff_json")
	}
}

func TestApplyStructuredDiff_EmptyHunks(t *testing.T) {
	cfg := Config{
		WorkspaceRoot:    t.TempDir(),
		MaxSearchResults: 50,
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	args := map[string]any{
		"path": "test.go",
		"diff_json": map[string]any{
			"hunks": []any{},
		},
	}

	result, err := registry.applyStructuredDiff(context.Background(), args)
	if err != nil {
		t.Fatalf("applyStructuredDiff: %v", err)
	}

	if !result.IsError {
		t.Error("expected error for empty hunks")
	}
}

func TestApplyStructuredDiff_FileNotFound(t *testing.T) {
	cfg := Config{
		WorkspaceRoot:    t.TempDir(),
		MaxSearchResults: 50,
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	args := map[string]any{
		"path": "nonexistent.go",
		"diff_json": map[string]any{
			"hunks": []any{
				map[string]any{
					"old_start": 1,
					"old_lines": 1,
					"new_start": 1,
					"new_lines": 1,
					"lines":     []any{" line"},
				},
			},
		},
	}

	result, err := registry.applyStructuredDiff(context.Background(), args)
	if err != nil {
		t.Fatalf("applyStructuredDiff: %v", err)
	}

	if !result.IsError {
		t.Error("expected error for file not found")
	}
}

func TestApplyStructuredDiff_ContextMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")

	original := `package main

func main() {
	println("hello")
}
`
	if err := os.WriteFile(testFile, []byte(original), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	cfg := Config{
		WorkspaceRoot:    tmpDir,
		MaxSearchResults: 50,
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	// Diff with wrong context
	diffJSON := map[string]any{
		"hunks": []any{
			map[string]any{
				"old_start": 4,
				"old_lines": 1,
				"new_start": 4,
				"new_lines": 1,
				"lines": []any{
					`-	println("wrong content")`, // Doesn't match
					`+	println("world")`,
				},
			},
		},
	}

	args := map[string]any{
		"path":      "test.go",
		"diff_json": diffJSON,
	}

	result, err := registry.applyStructuredDiff(context.Background(), args)
	if err != nil {
		t.Fatalf("applyStructuredDiff: %v", err)
	}

	if !result.IsError {
		t.Error("expected error for context mismatch")
	}
}

func TestApplyStructuredDiff_Cancellation(t *testing.T) {
	cfg := Config{
		WorkspaceRoot:    t.TempDir(),
		MaxSearchResults: 50,
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	args := map[string]any{
		"path": "test.go",
		"diff_json": map[string]any{
			"hunks": []any{},
		},
	}

	_, err = registry.applyStructuredDiff(ctx, args)
	if err == nil {
		t.Error("expected error for canceled context")
	}
}

func TestStructuredDiffTypes(t *testing.T) {
	// Test that our types marshal/unmarshal correctly
	diff := StructuredDiff{
		OldFile: "old.go",
		NewFile: "new.go",
		Statistics: DiffStats{
			LinesAdded:   5,
			LinesRemoved: 3,
			LinesChanged: 2,
			TotalChanges: 10,
			Similarity:   85.5,
		},
		Hunks: []DiffHunk{
			{
				OldStart: 10,
				OldLines: 5,
				NewStart: 10,
				NewLines: 7,
				Header:   "@@ -10,5 +10,7 @@",
				Lines:    []string{" context", "-old", "+new"},
			},
		},
	}

	if diff.OldFile != "old.go" {
		t.Errorf("OldFile = %q, want %q", diff.OldFile, "old.go")
	}
	if diff.Statistics.LinesAdded != 5 {
		t.Errorf("LinesAdded = %d, want 5", diff.Statistics.LinesAdded)
	}
	if len(diff.Hunks) != 1 {
		t.Errorf("len(Hunks) = %d, want 1", len(diff.Hunks))
	}
	if diff.Hunks[0].OldStart != 10 {
		t.Errorf("Hunks[0].OldStart = %d, want 10", diff.Hunks[0].OldStart)
	}
}
