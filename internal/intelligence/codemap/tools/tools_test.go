package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	models "github.com/XiaoConstantine/mcp-go/pkg/model"
)

func TestNewRegistry(t *testing.T) {
	tmpDir := t.TempDir()

	r, err := NewRegistry(WithWorkspace(tmpDir))
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	if r == nil {
		t.Fatal("NewRegistry() returned nil")
	}
	if r.Tools() == nil {
		t.Error("expected tools registry to be set")
	}
}

func TestReadFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test file
	testFile := filepath.Join(tmpDir, "test.txt")
	content := "line1\nline2\nline3\nline4\nline5"
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	r, err := NewRegistry(WithWorkspace(tmpDir))
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	ctx := context.Background()

	t.Run("read full file", func(t *testing.T) {
		result, err := r.readFile(ctx, map[string]any{
			"path": "test.txt",
		})
		if err != nil {
			t.Fatalf("readFile() error = %v", err)
		}
		if result.IsError {
			t.Fatalf("readFile() returned error: %v", result.Content)
		}
		payload := callToolPayload(t, result)
		if payload["content"] != content {
			t.Fatalf("content=%q want %q", payload["content"], content)
		}
	})

	t.Run("read with line range", func(t *testing.T) {
		result, err := r.readFile(ctx, map[string]any{
			"path":       "test.txt",
			"start_line": float64(2),
			"end_line":   float64(4),
		})
		if err != nil {
			t.Fatalf("readFile() error = %v", err)
		}
		if result.IsError {
			t.Fatalf("readFile() returned error: %v", result.Content)
		}
		payload := callToolPayload(t, result)
		if payload["content"] != "line2\nline3\nline4" {
			t.Fatalf("content=%q", payload["content"])
		}
		if payload["start_line"] != float64(2) || payload["end_line"] != float64(4) {
			t.Fatalf("range=%v:%v", payload["start_line"], payload["end_line"])
		}
	})

	t.Run("missing path", func(t *testing.T) {
		result, err := r.readFile(ctx, map[string]any{})
		if err != nil {
			t.Fatalf("readFile() error = %v", err)
		}
		if !result.IsError {
			t.Error("expected error for missing path")
		}
	})

	t.Run("nonexistent file", func(t *testing.T) {
		result, err := r.readFile(ctx, map[string]any{
			"path": "nonexistent.txt",
		})
		if err != nil {
			t.Fatalf("readFile() error = %v", err)
		}
		if !result.IsError {
			t.Error("expected error for nonexistent file")
		}
	})

	t.Run("invalid line type", func(t *testing.T) {
		result, err := r.readFile(ctx, map[string]any{
			"path":       "test.txt",
			"start_line": "2",
		})
		if err != nil {
			t.Fatalf("readFile() error = %v", err)
		}
		if !result.IsError {
			t.Error("expected error for invalid start_line")
		}
	})
}

func TestFinishCodemap(t *testing.T) {
	tmpDir := t.TempDir()

	r, err := NewRegistry(WithWorkspace(tmpDir))
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	ctx := context.Background()

	t.Run("valid codemap", func(t *testing.T) {
		codemapJSON := `{
			"title": "Test Codemap",
			"description": "A test codemap for unit testing",
			"traces": [
				{
					"tree": "- Root\n  - Child",
					"annotations": [
						{
							"location": "@file.go:10",
							"note": "Important function"
						}
					]
				}
			]
		}`
		result, err := r.finishCodemap(ctx, map[string]any{
			"codemap": codemapJSON,
		})
		if err != nil {
			t.Fatalf("finishCodemap() error = %v", err)
		}
		if result.IsError {
			t.Fatalf("finishCodemap() returned error: %v", result.Content)
		}

		// Check that codemap was captured
		if r.FinalCodemap() == nil {
			t.Error("expected codemap to be captured")
		}
	})

	t.Run("missing title", func(t *testing.T) {
		codemapJSON := `{"description": "Description", "traces": []}`
		result, err := r.finishCodemap(ctx, map[string]any{
			"codemap": codemapJSON,
		})
		if err != nil {
			t.Fatalf("finishCodemap() error = %v", err)
		}
		if !result.IsError {
			t.Error("expected error for missing title")
		}
	})

	t.Run("missing description", func(t *testing.T) {
		result, err := r.finishCodemap(ctx, map[string]any{
			"title":  "Title",
			"traces": []any{},
		})
		if err != nil {
			t.Fatalf("finishCodemap() error = %v", err)
		}
		if !result.IsError {
			t.Error("expected error for missing description")
		}
	})
}

func TestHelperFunctions(t *testing.T) {
	t.Run("successResult", func(t *testing.T) {
		result := successResult(map[string]any{"key": "value"})
		if result.IsError {
			t.Error("successResult should not be error")
		}
		if len(result.Content) != 1 {
			t.Errorf("expected 1 content, got %d", len(result.Content))
		}
	})

	t.Run("errorResult", func(t *testing.T) {
		result := errorResult("test error")
		if !result.IsError {
			t.Error("errorResult should be error")
		}
		if len(result.Content) != 1 {
			t.Errorf("expected 1 content, got %d", len(result.Content))
		}
	})
}

func callToolPayload(t *testing.T, result *models.CallToolResult) map[string]any {
	t.Helper()

	if len(result.Content) != 1 {
		t.Fatalf("content count=%d", len(result.Content))
	}
	text, ok := result.Content[0].(models.TextContent)
	if !ok {
		t.Fatalf("content type=%T", result.Content[0])
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(text.Text), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return payload
}
