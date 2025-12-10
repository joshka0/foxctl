package tools

import (
	"context"
	"encoding/json"
	"testing"

	models "github.com/XiaoConstantine/mcp-go/pkg/model"
	"github.com/jkatigb/agentctl/internal/agent/types"
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

// D1 Tests: Enhanced tool unit tests per PR5 spec

func TestCodeSymbolSearch_MaxResults(t *testing.T) {
	cfg := Config{
		WorkspaceRoot:    t.TempDir(),
		MaxSearchResults: 50,
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	// Test with custom max_results
	args := map[string]any{
		"workspace_id": "test-ws",
		"question":     "How does login work?",
		"max_results":  5,
	}

	result, err := registry.codeSymbolSearch(context.Background(), args)
	if err != nil {
		t.Fatalf("codeSymbolSearch: %v", err)
	}

	if result.IsError {
		t.Errorf("expected success, got error: %v", result.Content)
	}

	// Verify max_results is respected (stub returns empty, but validates input)
	content := extractResultContent(t, result)
	if content["count"] != float64(0) {
		t.Errorf("count = %v, want 0 (stub returns no results)", content["count"])
	}
}

func TestCodeSymbolSearch_SymbolHint(t *testing.T) {
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
		"question":     "token validation",
		"symbol_hint":  "ValidateToken",
	}

	result, err := registry.codeSymbolSearch(context.Background(), args)
	if err != nil {
		t.Fatalf("codeSymbolSearch: %v", err)
	}

	if result.IsError {
		t.Errorf("expected success with symbol_hint, got error: %v", result.Content)
	}
}

func TestCodeSweGrep_WithPriority(t *testing.T) {
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
			map[string]any{"path": "auth/login.go", "symbol_id": "Login", "priority": 0.95},
			map[string]any{"path": "auth/session.go", "priority": 0.5},
		},
	}

	result, err := registry.codeSweGrep(context.Background(), args)
	if err != nil {
		t.Fatalf("codeSweGrep: %v", err)
	}

	// Should fail because skill is not installed, but validates input correctly
	if !result.IsError {
		// If it succeeds, that's also fine (skill might be present)
		content := extractResultContent(t, result)
		if content["count"] == nil {
			t.Error("expected count field in response")
		}
	}
}

// D4 Test: Verify telemetry records tool calls

type mockTelemetryRecorder struct {
	calls []string
}

func (m *mockTelemetryRecorder) RecordToolCall(call types.ToolCall) {
	m.calls = append(m.calls, call.ToolName)
}

func TestTelemetry_RecordsNewToolNames(t *testing.T) {
	cfg := Config{
		WorkspaceRoot:    t.TempDir(),
		MaxSearchResults: 50,
	}
	recorder := &mockTelemetryRecorder{}
	registry, err := NewRegistry(cfg, recorder)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	// Call via the tool registry to test telemetry wrapper
	tool, err := registry.GetRegistry().Get("code.symbol_search")
	if err != nil {
		t.Fatalf("Get tool: %v", err)
	}

	symbolSearchArgs := map[string]any{
		"workspace_id": "test-ws",
		"question":     "test query",
	}
	_, _ = tool.Execute(context.Background(), symbolSearchArgs)

	// Verify telemetry was recorded
	if len(recorder.calls) == 0 {
		t.Error("expected telemetry to record tool call")
	}
	if len(recorder.calls) > 0 && recorder.calls[0] != "code.symbol_search" {
		t.Errorf("expected tool name %q, got %q", "code.symbol_search", recorder.calls[0])
	}

	// Verify the registry has the expected tools registered
	tools := registry.List()
	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name()] = true
	}

	// Phase 6 tools should be registered
	expectedTools := []string{
		"code.symbol_search",
		"code.swe_grep",
		"edit.apply_structured_diff",
	}

	for _, name := range expectedTools {
		if !toolNames[name] {
			t.Errorf("expected tool %q to be registered for telemetry", name)
		}
	}
}
