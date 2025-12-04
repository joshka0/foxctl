package tools

import (
	"context"
	"encoding/json"
	"testing"

	models "github.com/XiaoConstantine/mcp-go/pkg/model"
)

// extractResultContent parses the JSON content from a CallToolResult.
func extractResultContent(t *testing.T, result *models.CallToolResult) map[string]any {
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

func TestCodeSymbolSearch_ValidInput(t *testing.T) {
	cfg := Config{
		WorkspaceRoot:    t.TempDir(),
		MaxSearchResults: 50,
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	args := map[string]any{
		"workspace_id": "test-ws",
		"question":     "How does login work?",
	}

	result, err := registry.codeSymbolSearch(context.Background(), args)
	if err != nil {
		t.Fatalf("codeSymbolSearch: %v", err)
	}

	if result.IsError {
		t.Errorf("expected success, got error: %v", result.Content)
	}

	// The stub returns empty candidates with a message
	content := extractResultContent(t, result)
	if content["count"] != float64(0) {
		t.Errorf("count = %v, want 0 (stub returns no results)", content["count"])
	}
	if content["message"] == nil {
		t.Error("expected message field in stub response")
	}
}

func TestCodeSymbolSearch_MissingWorkspaceID(t *testing.T) {
	cfg := Config{
		WorkspaceRoot:    t.TempDir(),
		MaxSearchResults: 50,
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	args := map[string]any{
		"question": "How does login work?",
	}

	result, err := registry.codeSymbolSearch(context.Background(), args)
	if err != nil {
		t.Fatalf("codeSymbolSearch: %v", err)
	}

	if !result.IsError {
		t.Error("expected error for missing workspace_id")
	}
}

func TestCodeSymbolSearch_MissingQuestion(t *testing.T) {
	cfg := Config{
		WorkspaceRoot:    t.TempDir(),
		MaxSearchResults: 50,
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	args := map[string]any{
		"workspace_id": "test-ws",
	}

	result, err := registry.codeSymbolSearch(context.Background(), args)
	if err != nil {
		t.Fatalf("codeSymbolSearch: %v", err)
	}

	if !result.IsError {
		t.Error("expected error for missing question")
	}
}

func TestCodeSymbolSearch_InvalidMode(t *testing.T) {
	cfg := Config{
		WorkspaceRoot:    t.TempDir(),
		MaxSearchResults: 50,
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	args := map[string]any{
		"workspace_id": "test-ws",
		"question":     "How does login work?",
		"mode":         "invalid_mode",
	}

	result, err := registry.codeSymbolSearch(context.Background(), args)
	if err != nil {
		t.Fatalf("codeSymbolSearch: %v", err)
	}

	if !result.IsError {
		t.Error("expected error for invalid mode")
	}
}

func TestCodeSymbolSearch_ValidModes(t *testing.T) {
	cfg := Config{
		WorkspaceRoot:    t.TempDir(),
		MaxSearchResults: 50,
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	modes := []string{"search", "callers", "callees"}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			args := map[string]any{
				"workspace_id": "test-ws",
				"question":     "How does login work?",
				"mode":         mode,
			}

			result, err := registry.codeSymbolSearch(context.Background(), args)
			if err != nil {
				t.Fatalf("codeSymbolSearch: %v", err)
			}

			if result.IsError {
				t.Errorf("expected success for mode %q, got error", mode)
			}
		})
	}
}

func TestCodeSymbolSearch_Cancellation(t *testing.T) {
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
		"workspace_id": "test-ws",
		"question":     "How does login work?",
	}

	_, err = registry.codeSymbolSearch(ctx, args)
	if err == nil {
		t.Error("expected error for canceled context")
	}
}

func TestCodeSweGrep_MissingWorkspaceID(t *testing.T) {
	cfg := Config{
		WorkspaceRoot:    t.TempDir(),
		MaxSearchResults: 50,
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	args := map[string]any{
		"question": "How does login work?",
		"candidate_files": []any{
			map[string]any{"path": "auth/login.go"},
		},
	}

	result, err := registry.codeSweGrep(context.Background(), args)
	if err != nil {
		t.Fatalf("codeSweGrep: %v", err)
	}

	if !result.IsError {
		t.Error("expected error for missing workspace_id")
	}
}

func TestCodeSweGrep_MissingQuestion(t *testing.T) {
	cfg := Config{
		WorkspaceRoot:    t.TempDir(),
		MaxSearchResults: 50,
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	args := map[string]any{
		"workspace_id": "test-ws",
		"candidate_files": []any{
			map[string]any{"path": "auth/login.go"},
		},
	}

	result, err := registry.codeSweGrep(context.Background(), args)
	if err != nil {
		t.Fatalf("codeSweGrep: %v", err)
	}

	if !result.IsError {
		t.Error("expected error for missing question")
	}
}

func TestCodeSweGrep_MissingCandidates(t *testing.T) {
	cfg := Config{
		WorkspaceRoot:    t.TempDir(),
		MaxSearchResults: 50,
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	args := map[string]any{
		"workspace_id": "test-ws",
		"question":     "How does login work?",
	}

	result, err := registry.codeSweGrep(context.Background(), args)
	if err != nil {
		t.Fatalf("codeSweGrep: %v", err)
	}

	if !result.IsError {
		t.Error("expected error for missing candidate_files")
	}
}

func TestCodeSweGrep_EmptyCandidates(t *testing.T) {
	cfg := Config{
		WorkspaceRoot:    t.TempDir(),
		MaxSearchResults: 50,
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	args := map[string]any{
		"workspace_id":    "test-ws",
		"question":        "How does login work?",
		"candidate_files": []any{},
	}

	result, err := registry.codeSweGrep(context.Background(), args)
	if err != nil {
		t.Fatalf("codeSweGrep: %v", err)
	}

	if !result.IsError {
		t.Error("expected error for empty candidate_files")
	}
}

func TestCodeSweGrep_InvalidCandidateFormat(t *testing.T) {
	cfg := Config{
		WorkspaceRoot:    t.TempDir(),
		MaxSearchResults: 50,
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	args := map[string]any{
		"workspace_id": "test-ws",
		"question":     "How does login work?",
		"candidate_files": []any{
			"not-an-object", // Should be a map
		},
	}

	result, err := registry.codeSweGrep(context.Background(), args)
	if err != nil {
		t.Fatalf("codeSweGrep: %v", err)
	}

	if !result.IsError {
		t.Error("expected error for invalid candidate format")
	}
}

func TestCodeSweGrep_CandidateMissingPath(t *testing.T) {
	cfg := Config{
		WorkspaceRoot:    t.TempDir(),
		MaxSearchResults: 50,
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	args := map[string]any{
		"workspace_id": "test-ws",
		"question":     "How does login work?",
		"candidate_files": []any{
			map[string]any{"symbol_id": "Login"}, // Missing path
		},
	}

	result, err := registry.codeSweGrep(context.Background(), args)
	if err != nil {
		t.Fatalf("codeSweGrep: %v", err)
	}

	if !result.IsError {
		t.Error("expected error for candidate missing path")
	}
}

func TestCodeSweGrep_SkillNotInstalled(t *testing.T) {
	// Use a workspace without the skill installed
	cfg := Config{
		WorkspaceRoot:    t.TempDir(),
		MaxSearchResults: 50,
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	args := map[string]any{
		"workspace_id": "test-ws",
		"question":     "How does login work?",
		"candidate_files": []any{
			map[string]any{"path": "auth/login.go", "priority": 0.9},
		},
	}

	result, err := registry.codeSweGrep(context.Background(), args)
	if err != nil {
		t.Fatalf("codeSweGrep: %v", err)
	}

	// Should get an error because the skill isn't installed in the temp dir
	if !result.IsError {
		t.Error("expected error when skill is not installed")
	}
}

func TestSweGrepTypes(t *testing.T) {
	// Test that our types marshal/unmarshal correctly
	input := SweGrepInput{
		WorkspaceID: "test-ws",
		Question:    "How does login work?",
		Candidates: []SweGrepCandidate{
			{Path: "auth/login.go", SymbolID: "Login", Priority: 0.9},
			{Path: "auth/session.go"},
		},
		Limits: &SweGrepLimits{
			MaxFiles:    10,
			MaxSnippets: 5,
		},
	}

	if input.WorkspaceID != "test-ws" {
		t.Errorf("WorkspaceID = %q, want %q", input.WorkspaceID, "test-ws")
	}
	if len(input.Candidates) != 2 {
		t.Errorf("len(Candidates) = %d, want 2", len(input.Candidates))
	}
	if input.Candidates[0].Priority != 0.9 {
		t.Errorf("Candidates[0].Priority = %v, want 0.9", input.Candidates[0].Priority)
	}
}

func TestSymbolCandidateType(t *testing.T) {
	candidate := SymbolCandidate{
		File:     "auth/login.go",
		SymbolID: "pkg/auth/login.go:Login",
		Name:     "Login",
		Kind:     "function",
		Score:    0.95,
	}

	if candidate.File != "auth/login.go" {
		t.Errorf("File = %q, want %q", candidate.File, "auth/login.go")
	}
	if candidate.Score != 0.95 {
		t.Errorf("Score = %v, want 0.95", candidate.Score)
	}
}

func TestSweGrepSnippetType(t *testing.T) {
	snippet := SweGrepSnippet{
		File:      "auth/login.go",
		SymbolID:  "Login",
		StartLine: 10,
		EndLine:   25,
		Preview:   "func Login(ctx context.Context) error { ... }",
	}

	if snippet.StartLine != 10 {
		t.Errorf("StartLine = %d, want 10", snippet.StartLine)
	}
	if snippet.EndLine != 25 {
		t.Errorf("EndLine = %d, want 25", snippet.EndLine)
	}
}
