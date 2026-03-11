package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/agent/types"
	"github.com/jkatigb/agentctl/internal/agentprompt"
	"github.com/jkatigb/agentctl/internal/engine"
)

func TestAgentInstruction_CoderRole(t *testing.T) {
	cfg := types.AgentConfig{
		Role:        types.RoleCoder,
		ActorID:     "test-coder",
		WorkspaceID: "test-ws",
	}

	instruction := agentprompt.Instruction(cfg.Role)

	// Verify Phase 6 code tools are in Coder signature
	codeTools := []string{
		"code.symbol_search",
		"code.swe_grep",
		"code.search",
	}
	for _, tool := range codeTools {
		if !strings.Contains(instruction, tool) {
			t.Errorf("Coder signature should mention %q", tool)
		}
	}

	// Verify Phase 6 edit tool is in Coder signature
	if !strings.Contains(instruction, "edit.apply_structured_diff") {
		t.Error("Coder signature should mention edit.apply_structured_diff")
	}

	// Verify existing tools are still present
	existingTools := []string{
		"fs.read_file",
		"fs.list_dir",
		"edit.create_file",
		"edit.apply_patch",
		"tests.run",
	}
	for _, tool := range existingTools {
		if !strings.Contains(instruction, tool) {
			t.Errorf("Coder signature should mention %q", tool)
		}
	}

	// Verify workflow guidance is present
	if !strings.Contains(instruction, "code.symbol_search to find relevant symbols") {
		t.Error("Coder signature should include workflow guidance for symbol_search")
	}
}

func TestAgentInstruction_PlannerRole(t *testing.T) {
	cfg := types.AgentConfig{
		Role:        types.RolePlanner,
		ActorID:     "test-planner",
		WorkspaceID: "test-ws",
	}

	instruction := agentprompt.Instruction(cfg.Role)

	// Verify planner has its own tools
	plannerTools := []string{
		"todo.add",
		"todo.query",
		"todo.graph_insights",
		"mail.send",
	}
	for _, tool := range plannerTools {
		if !strings.Contains(instruction, tool) {
			t.Errorf("Planner signature should mention %q", tool)
		}
	}

	// Verify planner does NOT have low-level coding tools in its signature
	// (Per spec: keep Overseer/Planner focused on orchestration, not low-level editing)
	lowLevelTools := []string{
		"code.symbol_search",
		"code.swe_grep",
		"edit.apply_structured_diff",
		"fs.read_file",
	}
	for _, tool := range lowLevelTools {
		if strings.Contains(instruction, tool) {
			t.Errorf("Planner signature should NOT mention %q (should focus on orchestration)", tool)
		}
	}
}

func TestAgentInstruction_DefaultRole(t *testing.T) {
	role := types.AgentRole("unknown")
	instruction := agentprompt.Instruction(role)

	// Default role should have a generic instruction
	if !strings.Contains(instruction, "helpful agent") {
		t.Error("Default role should have generic 'helpful agent' instruction")
	}

	// Default role should NOT list specific tools
	specificTools := []string{
		"code.symbol_search",
		"todo.add",
	}
	for _, tool := range specificTools {
		if strings.Contains(instruction, tool) {
			t.Errorf("Default role should NOT list specific tool %q", tool)
		}
	}
}

func TestCoderSignature_ToolCategories(t *testing.T) {
	cfg := types.AgentConfig{
		Role:        types.RoleCoder,
		ActorID:     "test-coder",
		WorkspaceID: "test-ws",
	}

	instruction := agentprompt.Instruction(cfg.Role)

	// Verify the instruction has categorized sections
	categories := []string{
		"Code Search & Retrieval Tools:",
		"File Operations:",
		"Edit Tools:",
		"Testing:",
	}

	for _, cat := range categories {
		if !strings.Contains(instruction, cat) {
			t.Errorf("Coder signature should have category section %q", cat)
		}
	}
}

func TestAgentInstruction_ReviewerRole(t *testing.T) {
	cfg := types.AgentConfig{
		Role:        types.RoleReviewer,
		ActorID:     "test-reviewer",
		WorkspaceID: "test-ws",
	}

	instruction := agentprompt.Instruction(cfg.Role)

	// Verify reviewer has retrieval tools (read/inspect)
	retrievalTools := []string{
		"code.symbol_search",
		"code.swe_grep",
		"code.search",
		"fs.read_file",
		"fs.list_dir",
	}
	for _, tool := range retrievalTools {
		if !strings.Contains(instruction, tool) {
			t.Errorf("Reviewer signature should mention %q for inspection", tool)
		}
	}

	// Verify reviewer has validation and coordination tools
	otherTools := []string{
		"tests.run",
		"mail.send",
		"todo.add",
	}
	for _, tool := range otherTools {
		if !strings.Contains(instruction, tool) {
			t.Errorf("Reviewer signature should mention %q", tool)
		}
	}

	// Verify reviewer does NOT have edit tools
	editTools := []string{
		"edit.create_file",
		"edit.apply_patch",
		"edit.apply_structured_diff",
	}
	for _, tool := range editTools {
		if strings.Contains(instruction, tool) {
			t.Errorf("Reviewer signature should NOT mention %q (read-only role)", tool)
		}
	}

	// Verify reviewer identity
	if !strings.Contains(instruction, "code review agent") {
		t.Error("Reviewer signature should identify as 'code review agent'")
	}

	// Verify reviewer guidance about not applying edits
	if !strings.Contains(instruction, "do not directly apply edits") {
		t.Error("Reviewer signature should explicitly state it does not apply edits")
	}
}

func TestRunSession_StopRequestedBlocksAndContinues(t *testing.T) {
	// TODO: This test was written for an earlier implementation that used a dspy-go Agent interface.
	// The current implementation uses LLMChatEngine which requires different mocking.
	// Skip this test until we can properly refactor it to work with the LLMChatEngine-based implementation.
	// The hook dispatch functionality is still tested through integration tests and the
	// dispatchStopRequested method is used in the actual runSession flow.
	t.Skip("Test needs refactoring: Session now uses LLMChatEngine instead of Agent interface")
}

func TestReviewerSignature_ToolCategories(t *testing.T) {
	cfg := types.AgentConfig{
		Role:        types.RoleReviewer,
		ActorID:     "test-reviewer",
		WorkspaceID: "test-ws",
	}

	instruction := agentprompt.Instruction(cfg.Role)

	// Verify the instruction has categorized sections
	categories := []string{
		"Code Search & Retrieval Tools",
		"File Operations",
		"Validation:",
		"Coordination:",
		"Workflow:",
	}

	for _, cat := range categories {
		if !strings.Contains(instruction, cat) {
			t.Errorf("Reviewer signature should have category section %q", cat)
		}
	}
}

// helper: returns tool names from buildToolDefsForRole.
func toolNamesForRole(role types.AgentRole) []string {
	defs := buildToolDefsForRole(role, false, false, nil)
	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = d.Name
	}
	return names
}

func hasToolName(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

func TestBuildToolDefsForRole_SemanticScout(t *testing.T) {
	names := toolNamesForRole(types.RoleSemanticScout)

	// Must have
	for _, want := range []string{"think", "context_search", "smart_search", "memory_query"} {
		if !hasToolName(names, want) {
			t.Errorf("semantic_scout should have %q, got %v", want, names)
		}
	}

	// Must NOT have
	for _, deny := range []string{"fs_read_file", "code_search", "fs_list_dir"} {
		if hasToolName(names, deny) {
			t.Errorf("semantic_scout should NOT have %q", deny)
		}
	}
}

func TestBuildToolDefsForRole_DAGScout(t *testing.T) {
	names := toolNamesForRole(types.RoleDAGScout)

	for _, want := range []string{"think", "repo_index_search", "repo_index_expand", "repo_index_open", "repo_index_dag_grep"} {
		if !hasToolName(names, want) {
			t.Errorf("dag_scout should have %q, got %v", want, names)
		}
	}

	for _, deny := range []string{"fs_read_file", "code_search", "fs_list_dir"} {
		if hasToolName(names, deny) {
			t.Errorf("dag_scout should NOT have %q", deny)
		}
	}
}

func TestBuildToolDefsForRole_SymbolScout(t *testing.T) {
	names := toolNamesForRole(types.RoleSymbolScout)

	for _, want := range []string{"think", "code_symbols", "context_grep", "code_search"} {
		if !hasToolName(names, want) {
			t.Errorf("symbol_scout should have %q, got %v", want, names)
		}
	}

	for _, deny := range []string{"fs_read_file", "fs_list_dir"} {
		if hasToolName(names, deny) {
			t.Errorf("symbol_scout should NOT have %q", deny)
		}
	}
}

func TestBuildToolDefsForRole_AnnotationScout(t *testing.T) {
	names := toolNamesForRole(types.RoleAnnotationScout)

	// Must have
	for _, want := range []string{"think", "annotation_recall", "annotation_list_sessions", "annotation_category_stats", "memory_query"} {
		if !hasToolName(names, want) {
			t.Errorf("annotation_scout should have %q, got %v", want, names)
		}
	}

	// Must NOT have
	for _, deny := range []string{"fs_read_file", "code_search", "fs_list_dir", "context_search"} {
		if hasToolName(names, deny) {
			t.Errorf("annotation_scout should NOT have %q", deny)
		}
	}
}

func TestBuildToolDefsForRole_ResearcherUnchanged(t *testing.T) {
	names := toolNamesForRole(types.RoleResearcher)

	// Researcher must still have base tools + agentctl tools
	for _, want := range []string{"fs_read_file", "code_search", "think", "context_search", "smart_search", "context_grep", "code_symbols", "repo_index_search", "annotation_recall", "context_show", "context_retrieve", "obsidian_index_search", "obsidian_read", "obsidian_related"} {
		if !hasToolName(names, want) {
			t.Errorf("researcher should still have %q, got %v", want, names)
		}
	}
}

func TestBuildToolDefsForRole_CoderUnchanged(t *testing.T) {
	names := toolNamesForRole(types.RoleCoder)

	for _, want := range []string{"fs_read_file", "code_search", "think", "fs_write_file", "fs_list_dir", "heartwood_state", "heartwood_action"} {
		if !hasToolName(names, want) {
			t.Errorf("coder should still have %q, got %v", want, names)
		}
	}
}

// --- Engine retry tests ---

// mockLLMResponse returns a valid OpenAI-compatible JSON response.
func mockLLMResponse(content string) []byte {
	resp := map[string]any{
		"id": "test-resp",
		"choices": []map[string]any{
			{
				"message":       map[string]string{"role": "assistant", "content": content},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 5},
	}
	b, _ := json.Marshal(resp)
	return b
}

func mockLLMToolCallResponse(toolName string, args string) []byte {
	resp := map[string]any{
		"id": "test-tool-resp",
		"choices": []map[string]any{
			{
				"message": map[string]any{
					"role":    "assistant",
					"content": "",
					"tool_calls": []map[string]any{
						{
							"id":   "call-1",
							"type": "function",
							"function": map[string]string{
								"name":      toolName,
								"arguments": args,
							},
						},
					},
				},
				"finish_reason": "tool_calls",
			},
		},
		"usage": map[string]int{"prompt_tokens": 12, "completion_tokens": 3},
	}
	b, _ := json.Marshal(resp)
	return b
}

// newTestEngine creates an LLMChatEngine pointing at the given test server URL.
func newTestEngine(t *testing.T, serverURL string) *engine.LLMChatEngine {
	t.Helper()
	eng, err := engine.NewLLMChatEngine(engine.LLMChatConfig{
		APIKey:        "test-key",
		BaseURL:       serverURL,
		Model:         "test-model",
		MaxIterations: 10,
	})
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	return eng
}

// newTestSession creates a minimal Session for retry testing.
func newTestSession(eng *engine.LLMChatEngine) *Session {
	return &Session{
		ID: "test-session",
		Config: types.AgentConfig{
			Role:    types.RoleCoder,
			Prompt:  "test prompt",
			Timeout: 30 * time.Second,
		},
		Engine:    eng,
		StartedAt: time.Now(),
		ToolCalls: []types.ToolCall{},
		Children:  []string{},
		done:      make(chan struct{}),
	}
}

func TestEngineRetryOnFirstError(t *testing.T) {
	// Mock server: first call returns 500 (engine error), second call succeeds.
	var callCount int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt64(&callCount, 1)
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("temporary failure"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(mockLLMResponse("Recovered successfully"))
	}))
	defer server.Close()

	eng := newTestEngine(t, server.URL)
	session := newTestSession(eng)

	rt := &Runtime{
		sessions: make(map[string]*Session),
		config:   Config{DefaultMaxIterations: 10},
	}

	ctx := context.Background()
	rt.runSession(ctx, session)

	if session.Status != types.StatusOK {
		t.Errorf("expected StatusOK after retry, got %s (error: %s)", session.Status, session.Error)
	}
	if atomic.LoadInt64(&callCount) < 2 {
		t.Errorf("expected at least 2 LLM calls (1 failure + 1 success), got %d", atomic.LoadInt64(&callCount))
	}
}

func TestEngineRetryExhausted(t *testing.T) {
	// Mock server: always returns 500 — both attempts fail.
	var callCount int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&callCount, 1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("persistent failure"))
	}))
	defer server.Close()

	eng := newTestEngine(t, server.URL)
	session := newTestSession(eng)

	rt := &Runtime{
		sessions: make(map[string]*Session),
		config:   Config{DefaultMaxIterations: 10},
	}

	ctx := context.Background()
	rt.runSession(ctx, session)

	if session.Status != types.StatusError {
		t.Errorf("expected StatusError after retries exhausted, got %s", session.Status)
	}
	if session.Error == "" {
		t.Error("expected non-empty error message")
	}
	calls := atomic.LoadInt64(&callCount)
	if calls < 2 {
		t.Errorf("expected at least 2 LLM calls (original + 1 retry), got %d", calls)
	}
}

func TestEngineMaxIterationsDoesNotRetry(t *testing.T) {
	// Mock server:
	// 1) First call returns a tool call with no assistant text.
	// 2) Finalize call returns assistant text.
	// Any additional call indicates runtime retried the engine run.
	var callCount int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt64(&callCount, 1)
		w.WriteHeader(http.StatusOK)

		switch n {
		case 1:
			_, _ = w.Write(mockLLMToolCallResponse("fs_read_file", `{"path":"README.md"}`))
		case 2:
			_, _ = w.Write(mockLLMResponse("Finalized report after max iterations"))
		default:
			_, _ = w.Write(mockLLMResponse("unexpected extra call"))
		}
	}))
	defer server.Close()

	eng, err := engine.NewLLMChatEngine(engine.LLMChatConfig{
		APIKey:        "test-key",
		BaseURL:       server.URL,
		Model:         "test-model",
		MaxIterations: 1, // Force max-iterations after first tool-call iteration.
	})
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}

	session := newTestSession(eng)

	rt := &Runtime{
		sessions: make(map[string]*Session),
		config:   Config{DefaultMaxIterations: 10},
	}

	ctx := context.Background()
	rt.runSession(ctx, session)

	if session.Status != types.StatusOK {
		t.Errorf("expected StatusOK after max-iterations finalize, got %s (error: %s)", session.Status, session.Error)
	}
	if session.Error != "" {
		t.Errorf("expected empty session error, got %q", session.Error)
	}
	if session.Summary == "" {
		t.Error("expected non-empty summary from finalize output")
	}
	calls := atomic.LoadInt64(&callCount)
	if calls != 2 {
		t.Errorf("expected exactly 2 LLM calls (tool-call + finalize) with no retry, got %d", calls)
	}
}

func TestEngineRetrySkipsContextErrors(t *testing.T) {
	// Mock server: returns 500, but context is canceled so no retry should happen.
	var callCount int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&callCount, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(mockLLMResponse("should not reach here"))
	}))
	defer server.Close()

	eng := newTestEngine(t, server.URL)
	session := newTestSession(eng)

	rt := &Runtime{
		sessions: make(map[string]*Session),
		config:   Config{DefaultMaxIterations: 10},
	}

	// Cancel context before running — the engine should detect cancellation
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	rt.runSession(ctx, session)

	if session.Status != types.StatusCanceled {
		t.Errorf("expected StatusCanceled for context error, got %s (error: %s)", session.Status, session.Error)
	}
}
