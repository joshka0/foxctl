package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	models "github.com/XiaoConstantine/mcp-go/pkg/model"
	"github.com/joshka0/foxctl/internal/storage/graph"
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

func TestSearchPatternArgs(t *testing.T) {
	t.Run("builds default skill input", func(t *testing.T) {
		input := searchPatternArgs{Pattern: "func main"}.skillInput("/workspace")

		if input["path"] != "/workspace" {
			t.Fatalf("path=%v", input["path"])
		}
		if input["pattern"] != "func main" {
			t.Fatalf("pattern=%v", input["pattern"])
		}
		if input["case_insensitive"] != true {
			t.Fatalf("case_insensitive=%v", input["case_insensitive"])
		}
		if input["max_matches"] != 50 || input["max_blocks"] != 20 || input["max_block_lines"] != 100 {
			t.Fatalf("limits=%v", input)
		}
	})

	t.Run("applies optional overrides", func(t *testing.T) {
		caseSensitive := false
		input := searchPatternArgs{
			Pattern:         "func main",
			Path:            "cmd/foxctl",
			CaseInsensitive: &caseSensitive,
			MaxMatches:      7,
		}.skillInput("/workspace")

		if input["path"] != filepath.Join("/workspace", "cmd/foxctl") {
			t.Fatalf("path=%v", input["path"])
		}
		if input["case_insensitive"] != false {
			t.Fatalf("case_insensitive=%v", input["case_insensitive"])
		}
		if input["max_matches"] != 7 {
			t.Fatalf("max_matches=%v", input["max_matches"])
		}
	})

	t.Run("rejects invalid argument type before skill execution", func(t *testing.T) {
		r, err := NewRegistry(WithWorkspace(t.TempDir()))
		if err != nil {
			t.Fatalf("NewRegistry() error = %v", err)
		}

		result, err := r.searchPattern(context.Background(), map[string]any{
			"pattern":     "func main",
			"max_matches": "7",
		})
		if err != nil {
			t.Fatalf("searchPattern() error = %v", err)
		}
		if !result.IsError {
			t.Fatal("expected invalid argument error")
		}
	})
}

func TestGetSymbolsArgs(t *testing.T) {
	t.Run("builds default skill input", func(t *testing.T) {
		input := getSymbolsArgs{Path: "internal/runtime"}.skillInput("/workspace")

		if input["path"] != filepath.Join("/workspace", "internal/runtime") {
			t.Fatalf("path=%v", input["path"])
		}
		if input["symbol_type"] != "all" {
			t.Fatalf("symbol_type=%v", input["symbol_type"])
		}
		if input["include_private"] != false {
			t.Fatalf("include_private=%v", input["include_private"])
		}
		if input["include_docs"] != true {
			t.Fatalf("include_docs=%v", input["include_docs"])
		}
		if input["max_results"] != 200 {
			t.Fatalf("max_results=%v", input["max_results"])
		}
	})

	t.Run("applies optional overrides", func(t *testing.T) {
		includePrivate := true
		includeDocs := false
		input := getSymbolsArgs{
			Path:           "internal/runtime",
			SymbolType:     "function",
			IncludePrivate: &includePrivate,
			IncludeDocs:    &includeDocs,
		}.skillInput("/workspace")

		if input["symbol_type"] != "function" {
			t.Fatalf("symbol_type=%v", input["symbol_type"])
		}
		if input["include_private"] != true {
			t.Fatalf("include_private=%v", input["include_private"])
		}
		if input["include_docs"] != false {
			t.Fatalf("include_docs=%v", input["include_docs"])
		}
	})

	t.Run("rejects invalid argument type before skill execution", func(t *testing.T) {
		r, err := NewRegistry(WithWorkspace(t.TempDir()))
		if err != nil {
			t.Fatalf("NewRegistry() error = %v", err)
		}

		result, err := r.getSymbols(context.Background(), map[string]any{
			"path":            "internal/runtime",
			"include_private": "true",
		})
		if err != nil {
			t.Fatalf("getSymbols() error = %v", err)
		}
		if !result.IsError {
			t.Fatal("expected invalid argument error")
		}
	})
}

func TestGetGraphNeighborsArgs(t *testing.T) {
	t.Run("builds default options", func(t *testing.T) {
		opts := getGraphNeighborsArgs{NodeID: "node-1"}.options()

		if opts.Direction != "both" {
			t.Fatalf("direction=%q", opts.Direction)
		}
		if len(opts.EdgeTypes) != 0 {
			t.Fatalf("edge_types=%v", opts.EdgeTypes)
		}
	})

	t.Run("applies option overrides", func(t *testing.T) {
		opts := getGraphNeighborsArgs{
			NodeID:    "node-1",
			Direction: "out",
			EdgeTypes: []string{"imports", "", "calls"},
		}.options()

		if opts.Direction != "out" {
			t.Fatalf("direction=%q", opts.Direction)
		}
		want := []graph.EdgeType{"imports", "calls"}
		if !reflect.DeepEqual(opts.EdgeTypes, want) {
			t.Fatalf("edge_types=%v want %v", opts.EdgeTypes, want)
		}
	})

	t.Run("rejects invalid edge type shape before graph lookup", func(t *testing.T) {
		r, err := NewRegistry(WithWorkspace(t.TempDir()))
		if err != nil {
			t.Fatalf("NewRegistry() error = %v", err)
		}

		result, err := r.getGraphNeighbors(context.Background(), map[string]any{
			"node_id":    "node-1",
			"edge_types": []any{"imports", 42},
		})
		if err != nil {
			t.Fatalf("getGraphNeighbors() error = %v", err)
		}
		if !result.IsError {
			t.Fatal("expected invalid argument error")
		}
	})
}

func TestSemanticSearchArgs(t *testing.T) {
	t.Run("builds default skill input", func(t *testing.T) {
		input := semanticSearchArgs{Query: "agent memory"}.skillInput("/workspace")

		if input["query"] != "agent memory" {
			t.Fatalf("query=%v", input["query"])
		}
		if !reflect.DeepEqual(input["scope"], []string{"symbols"}) {
			t.Fatalf("scope=%v", input["scope"])
		}
		if input["limit"] != 20 {
			t.Fatalf("limit=%v", input["limit"])
		}
		if input["workspace"] != "/workspace" {
			t.Fatalf("workspace=%v", input["workspace"])
		}
	})

	t.Run("applies optional overrides", func(t *testing.T) {
		input := semanticSearchArgs{
			Query: "agent memory",
			Scope: "sessions",
			Limit: 5,
		}.skillInput("/workspace")

		if !reflect.DeepEqual(input["scope"], []string{"sessions"}) {
			t.Fatalf("scope=%v", input["scope"])
		}
		if input["limit"] != 5 {
			t.Fatalf("limit=%v", input["limit"])
		}
	})

	t.Run("rejects invalid argument type before skill execution", func(t *testing.T) {
		r, err := NewRegistry(WithWorkspace(t.TempDir()))
		if err != nil {
			t.Fatalf("NewRegistry() error = %v", err)
		}

		result, err := r.semanticSearch(context.Background(), map[string]any{
			"query": "agent memory",
			"limit": "5",
		})
		if err != nil {
			t.Fatalf("semanticSearch() error = %v", err)
		}
		if !result.IsError {
			t.Fatal("expected invalid argument error")
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
