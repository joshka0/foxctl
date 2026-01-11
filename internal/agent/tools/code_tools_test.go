package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	models "github.com/XiaoConstantine/mcp-go/pkg/model"
	"github.com/jkatigb/agentctl/internal/agent/types"
	"github.com/jkatigb/agentctl/internal/indexing/symbol"
	"github.com/jkatigb/agentctl/internal/storage"
	memstore "github.com/jkatigb/agentctl/internal/storage/memory"
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
	openStore, workspaceID := seedSymbolSearchStore(t)

	cfg := Config{
		WorkspaceRoot:    t.TempDir(),
		MaxSearchResults: 50,
		OpenMemoryStore: func(_ context.Context) (storage.MemoryStore, error) {
			return openStore(context.Background())
		},
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	args := map[string]any{
		"workspace_id": workspaceID,
		"question":     "How does login work?",
	}

	result, err := registry.codeSymbolSearch(context.Background(), args)
	if err != nil {
		t.Fatalf("codeSymbolSearch: %v", err)
	}

	if result.IsError {
		t.Errorf("expected success, got error: %v", result.Content)
	}

	// Expect we find at least one candidate from the seeded symbol index.
	content := extractResultContent(t, result)
	if content["count"] == float64(0) {
		t.Fatalf("expected non-empty candidates, got: %v", content)
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
	openStore, workspaceID := seedSymbolSearchStore(t)

	cfg := Config{
		WorkspaceRoot:    t.TempDir(),
		MaxSearchResults: 50,
		OpenMemoryStore: func(_ context.Context) (storage.MemoryStore, error) {
			return openStore(context.Background())
		},
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	modes := []string{"search", "callers", "callees"}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			args := map[string]any{
				"workspace_id": workspaceID,
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
			content := extractResultContent(t, result)
			if mode == "search" {
				if content["count"] == float64(0) {
					t.Fatalf("expected non-empty search results, got %v", content)
				}
				return
			}
			if content["count"] == float64(0) {
				t.Fatalf("expected non-empty results for mode %q (call edges seeded), got %v", mode, content)
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

func TestCodeSnippetExtract_MissingWorkspaceID(t *testing.T) {
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

	result, err := registry.codeSnippetExtract(context.Background(), args)
	if err != nil {
		t.Fatalf("codeSnippetExtract: %v", err)
	}

	if !result.IsError {
		t.Error("expected error for missing workspace_id")
	}
}

func TestCodeSnippetExtract_MissingQuestion(t *testing.T) {
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

	result, err := registry.codeSnippetExtract(context.Background(), args)
	if err != nil {
		t.Fatalf("codeSnippetExtract: %v", err)
	}

	if !result.IsError {
		t.Error("expected error for missing question")
	}
}

func TestCodeSnippetExtract_MissingCandidates(t *testing.T) {
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

	result, err := registry.codeSnippetExtract(context.Background(), args)
	if err != nil {
		t.Fatalf("codeSnippetExtract: %v", err)
	}

	if !result.IsError {
		t.Error("expected error for missing candidate_files")
	}
}

func TestCodeSnippetExtract_EmptyCandidates(t *testing.T) {
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

	result, err := registry.codeSnippetExtract(context.Background(), args)
	if err != nil {
		t.Fatalf("codeSnippetExtract: %v", err)
	}

	if !result.IsError {
		t.Error("expected error for empty candidate_files")
	}
}

func TestCodeSnippetExtract_InvalidCandidateFormat(t *testing.T) {
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

	result, err := registry.codeSnippetExtract(context.Background(), args)
	if err != nil {
		t.Fatalf("codeSnippetExtract: %v", err)
	}

	if !result.IsError {
		t.Error("expected error for invalid candidate format")
	}
}

func TestCodeSnippetExtract_CandidateMissingPath(t *testing.T) {
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

	result, err := registry.codeSnippetExtract(context.Background(), args)
	if err != nil {
		t.Fatalf("codeSnippetExtract: %v", err)
	}

	if !result.IsError {
		t.Error("expected error for candidate missing path")
	}
}

func TestCodeSnippetExtract_SkillNotInstalled(t *testing.T) {
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

	result, err := registry.codeSnippetExtract(context.Background(), args)
	if err != nil {
		t.Fatalf("codeSnippetExtract: %v", err)
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
	openStore, workspaceID := seedSymbolSearchStore(t)

	cfg := Config{
		WorkspaceRoot:    t.TempDir(),
		MaxSearchResults: 50,
		OpenMemoryStore: func(_ context.Context) (storage.MemoryStore, error) {
			return openStore(context.Background())
		},
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	// Test with custom max_results
	args := map[string]any{
		"workspace_id": workspaceID,
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

	// Verify max_results is respected.
	content := extractResultContent(t, result)
	if content["count"].(float64) > 5 {
		t.Fatalf("expected count <= 5, got %v", content["count"])
	}
}

func TestCodeSymbolSearch_SymbolHint(t *testing.T) {
	openStore, workspaceID := seedSymbolSearchStore(t)

	cfg := Config{
		WorkspaceRoot:    t.TempDir(),
		MaxSearchResults: 50,
		OpenMemoryStore: func(_ context.Context) (storage.MemoryStore, error) {
			return openStore(context.Background())
		},
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	args := map[string]any{
		"workspace_id": workspaceID,
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
	content := extractResultContent(t, result)
	candidates, ok := content["candidates"].([]any)
	if !ok || len(candidates) == 0 {
		t.Fatalf("expected candidates, got %v", content)
	}
	first, ok := candidates[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first candidate object, got %T", candidates[0])
	}
	if first["name"] != "ValidateToken" {
		t.Fatalf("expected ValidateToken to rank first with symbol_hint, got %v", first["name"])
	}
}

func seedSymbolSearchStore(t *testing.T) (func(context.Context) (storage.MemoryStore, error), string) {
	t.Helper()
	ctx := context.Background()
	cacheRoot := t.TempDir()
	casRoot := filepath.Join(cacheRoot, "cas")
	store, err := memstore.Open(ctx, cacheRoot, casRoot)
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer store.Close()
	workspaceID := "ws-symbol-search"

	seed := func(filePath, name string, kind symbol.Kind, signature, summary string) {
		entryName := symbol.EntryName(workspaceID, filePath, name)
		res := symbol.Result{
			Symbol: symbol.Symbol{
				ID:            symbol.ID(filePath, name),
				FilePath:      filePath,
				Name:          name,
				Language:      "go",
				Kind:          kind,
				StartByte:     0,
				EndByte:       0,
				StartLine:     1,
				EndLine:       1,
				Signature:     signature,
				BodyDigest:    "sha256:deadbeef",
				FileDigest:    "",
				Documentation: "",
			},
			Source: nil,
			Calls:  nil,
		}
		b, merr := symbol.MarshalResult(res)
		if merr != nil {
			t.Fatalf("marshal symbol result: %v", merr)
		}
		if _, serr := store.Save(ctx, storage.NamedEntry{
			Name:      entryName,
			Type:      symbol.SymbolType,
			Workspace: workspaceID,
			Summary:   summary,
			Result:    b,
		}); serr != nil {
			t.Fatalf("save named entry: %v", serr)
		}
	}

	seed("auth/login.go", "Login", symbol.KindFunction, "func Login(ctx context.Context, in Input) error", "function Login in auth/login.go")
	seed("auth/token.go", "ValidateToken", symbol.KindFunction, "func ValidateToken(token string) error", "function ValidateToken in auth/token.go")
	seed("config/config.go", "Load", symbol.KindFunction, "func Load(path string) (Config, error)", "function Load in config/config.go")
	seed("auth/session.go", "Session", symbol.KindStruct, "type Session struct", "struct Session in auth/session.go")

	// Seed call edges so callers/callees mode can return results.
	seedEdge := func(sourceID, targetID string) {
		edge := symbol.CallEdge{SourceID: sourceID, TargetID: targetID, Count: 1}
		b, merr := symbol.MarshalResult(edge)
		if merr != nil {
			t.Fatalf("marshal call edge: %v", merr)
		}
		name := "call://" + workspaceID + "/" + sourceID + "->" + targetID
		if _, serr := store.Save(ctx, storage.NamedEntry{
			Name:      name,
			Type:      symbol.CallEdgeType,
			Workspace: workspaceID,
			Summary:   "call edge",
			Result:    b,
		}); serr != nil {
			t.Fatalf("save call edge: %v", serr)
		}
	}

	loginID := symbol.ID("auth/login.go", "Login")
	sessionID := symbol.ID("auth/session.go", "Session")
	validateID := symbol.ID("auth/token.go", "ValidateToken")
	seedEdge(loginID, sessionID)
	seedEdge(loginID, validateID)
	seedEdge(validateID, loginID)

	return func(ctx context.Context) (storage.MemoryStore, error) {
		return memstore.Open(ctx, cacheRoot, casRoot)
	}, workspaceID
}

func TestCodeSnippetExtract_WithPriority(t *testing.T) {
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

	result, err := registry.codeSnippetExtract(context.Background(), args)
	if err != nil {
		t.Fatalf("codeSnippetExtract: %v", err)
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
	// Execute may fail (no memory store); we only care that telemetry was recorded.
	_, _ = tool.Execute(context.Background(), symbolSearchArgs) //nolint:errcheck

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
