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

	"github.com/joshka0/foxctl/internal/agent/optimization"
	"github.com/joshka0/foxctl/internal/agent/types"
	"github.com/joshka0/foxctl/internal/runtime/agentprompt"
	"github.com/joshka0/foxctl/internal/runtime/engine"
	"github.com/joshka0/foxctl/internal/storage/sessions"
	einoadapter "github.com/joshka0/foxctl/internal/v2/adapters/eino"
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

func TestBuildTaskPromptPrefersTargetProfileVariant(t *testing.T) {
	ctx := context.Background()
	store, err := optimization.OpenPromptVariantStore(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("open prompt variant store: %v", err)
	}
	defer store.Close() //nolint:errcheck

	if _, err := store.Save(ctx, optimization.PromptVariant{
		WorkspaceID:    "/tmp/ws",
		AgentRole:      "coder",
		TargetProfile:  "generic",
		Mode:           "gepa",
		OriginalPrompt: "base",
		Prompt:         "generic runtime prompt",
	}); err != nil {
		t.Fatalf("save generic variant: %v", err)
	}
	if _, err := store.Save(ctx, optimization.PromptVariant{
		WorkspaceID:    "/tmp/ws",
		AgentRole:      "coder",
		TargetProfile:  "local_lmstudio",
		Mode:           "gepa",
		OriginalPrompt: "base",
		Prompt:         "local runtime prompt",
	}); err != nil {
		t.Fatalf("save local variant: %v", err)
	}

	rt := NewRuntime(Config{
		LLMProvider:        "lmstudio",
		PromptVariantStore: store,
	})
	prompt := rt.buildTaskPrompt(ctx, types.AgentConfig{
		Role:        types.RoleCoder,
		WorkspaceID: "/tmp/ws",
		LLMProvider: "lmstudio",
		LLMModel:    "liquid/lfm2.5-1.2b",
	})
	if !strings.Contains(prompt, "local runtime prompt") {
		t.Fatalf("prompt=%q missing local runtime prompt", prompt)
	}
	if !strings.Contains(prompt, "STRUCTURED SHELL POLICY:") {
		t.Fatalf("prompt=%q missing shell guidance", prompt)
	}
}

func TestShouldPreferRawToolDataForMemoryScouts(t *testing.T) {
	t.Parallel()

	if !shouldPreferRawToolData(string(types.RoleMemoryFactScout), "agent_memory_search") {
		t.Fatal("memory_fact_scout should prefer raw agent_memory_search payloads")
	}
	if !shouldPreferRawToolData(string(types.RoleContextWikiScout), "context_retrieve") {
		t.Fatal("contextwiki_scout should prefer raw context_retrieve payloads")
	}
	if shouldPreferRawToolData(string(types.RoleDAGScout), "agent_memory_search") {
		t.Fatal("dag_scout should not prefer raw memory payloads")
	}
}

func TestBuildContextSearchInputUsesCodeProfile(t *testing.T) {
	t.Parallel()

	got := buildContextSearchInput("legacy agent runtime scout role", 7)
	if !strings.Contains(got, `"profile": "code"`) {
		t.Fatalf("input=%q missing code profile", got)
	}
	if !strings.Contains(got, `"include_context": false`) {
		t.Fatalf("input=%q missing include_context false", got)
	}
	if !strings.Contains(got, `"limit": 7`) {
		t.Fatalf("input=%q missing limit", got)
	}
}

func TestBuildTaskPromptVariantAppendsTaskContext(t *testing.T) {
	ctx := context.Background()
	store, err := optimization.OpenPromptVariantStore(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("open prompt variant store: %v", err)
	}
	defer store.Close() //nolint:errcheck

	if _, err := store.Save(ctx, optimization.PromptVariant{
		WorkspaceID:    "/tmp/ws",
		AgentRole:      "coder",
		TargetProfile:  "local_lmstudio",
		Mode:           "gepa",
		OriginalPrompt: "base",
		Prompt:         "local runtime prompt",
	}); err != nil {
		t.Fatalf("save local variant: %v", err)
	}

	rt := NewRuntime(Config{
		LLMProvider:        "lmstudio",
		PromptVariantStore: store,
	})
	prompt := rt.buildTaskPrompt(ctx, types.AgentConfig{
		Role:        types.RoleCoder,
		WorkspaceID: "/tmp/ws",
		LLMProvider: "lmstudio",
		LLMModel:    "liquid/lfm2.5-1.2b",
		TaskID:      "task-123",
		EpicID:      "epic-9",
	})
	if !strings.Contains(prompt, "local runtime prompt") {
		t.Fatalf("prompt=%q missing variant prompt", prompt)
	}
	if !strings.Contains(prompt, "STRUCTURED SHELL POLICY:") {
		t.Fatalf("prompt=%q missing shell guidance", prompt)
	}
	if !strings.Contains(prompt, "Assigned task: task-123") {
		t.Fatalf("prompt=%q missing task context", prompt)
	}
	if !strings.Contains(prompt, "Epic: epic-9") {
		t.Fatalf("prompt=%q missing epic context", prompt)
	}
}

func TestBuildTaskPromptGenericAppendsStructuredShellGuidance(t *testing.T) {
	rt := NewRuntime(Config{})
	prompt := rt.buildTaskPrompt(context.Background(), types.AgentConfig{
		Role:        types.RoleReviewer,
		WorkspaceID: "ws-test",
	})
	if !strings.Contains(prompt, "Please analyze the workspace and complete your assigned work.") {
		t.Fatalf("prompt=%q missing generic body", prompt)
	}
	if !strings.Contains(prompt, "STRUCTURED SHELL POLICY:") {
		t.Fatalf("prompt=%q missing shell guidance", prompt)
	}
}

func TestBuildTaskPromptGenericOmitsStructuredShellGuidanceForPlanner(t *testing.T) {
	rt := NewRuntime(Config{})
	prompt := rt.buildTaskPrompt(context.Background(), types.AgentConfig{
		Role:        types.RolePlanner,
		WorkspaceID: "ws-test",
	})
	if strings.Contains(prompt, "STRUCTURED SHELL POLICY:") {
		t.Fatalf("planner prompt should not include shell guidance: %q", prompt)
	}
}

func TestSpawnPersistsResolvedPromptVariant(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	sessionStore, err := sessions.Open(ctx, root)
	if err != nil {
		t.Fatalf("open session store: %v", err)
	}
	defer sessionStore.Close() //nolint:errcheck

	variantStore, err := optimization.OpenPromptVariantStore(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("open prompt variant store: %v", err)
	}
	defer variantStore.Close() //nolint:errcheck

	if _, err := variantStore.Save(ctx, optimization.PromptVariant{
		WorkspaceID:    "ws-test",
		AgentRole:      "coder",
		TargetProfile:  "local_lmstudio",
		Mode:           "gepa",
		OriginalPrompt: "base",
		Prompt:         "resolved runtime prompt",
	}); err != nil {
		t.Fatalf("save prompt variant: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(mockLLMResponse("done"))
	}))
	defer server.Close()

	rt := NewRuntime(Config{
		DefaultMaxIterations: 1,
		DefaultTimeout:       5 * time.Second,
		SessionStore:         sessionStore,
		PromptVariantStore:   variantStore,
		WorkspaceRoot:        root,
		LLMProvider:          "lmstudio",
		LLMModel:             "liquid/lfm2.5-1.2b",
		LLMAPIKey:            "test-key",
		LLMBaseURL:           server.URL,
	})

	session, err := rt.Spawn(ctx, types.AgentConfig{
		Role:        types.RoleCoder,
		ActorID:     "actor:test:coder",
		WorkspaceID: "ws-test",
	})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer func() {
		session.cancel()
		<-session.done
	}()

	stored, err := sessionStore.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !strings.HasPrefix(stored.Prompt, "resolved runtime prompt") {
		t.Fatalf("stored.Prompt=%q want prefix resolved runtime prompt", stored.Prompt)
	}
	if !strings.Contains(stored.Prompt, "STRUCTURED SHELL POLICY:") {
		t.Fatalf("stored.Prompt=%q missing shell guidance", stored.Prompt)
	}
	if stored.PromptHash == "" {
		t.Fatal("stored.PromptHash is empty")
	}
}

func TestCreateEngine_DefaultPathUnaffectedWhenEinoDisabled(t *testing.T) {
	t.Setenv(einoadapter.EnvEngineBackend, "")

	rt := NewRuntime(Config{
		DefaultMaxIterations: 1,
		DefaultTimeout:       5 * time.Second,
		LLMProvider:          "openai",
		LLMModel:             "test-model",
		LLMAPIKey:            "test-key",
		LLMBaseURL:           "http://example.invalid",
	})

	eng, tools, err := rt.createEngine(types.AgentConfig{
		Role:          types.RoleCoder,
		ActorID:       "actor:test:coder",
		WorkspaceID:   "ws-test",
		MaxIterations: 1,
		Timeout:       5 * time.Second,
	}, "session-test")
	if err != nil {
		t.Fatalf("createEngine() error = %v", err)
	}
	if eng == nil {
		t.Fatal("createEngine() returned nil engine")
	}
	if _, ok := eng.(*engine.LLMChatEngine); !ok {
		t.Fatalf("createEngine() engine type = %T want *engine.LLMChatEngine when Eino gate is disabled", eng)
	}
	if len(tools) == 0 {
		t.Fatal("createEngine() returned no tool definitions for default path")
	}
}

func TestCreateEngine_EinoGateProvisionsRealAdapter(t *testing.T) {
	t.Setenv(einoadapter.EnvEngineBackend, "eino")

	rt := NewRuntime(Config{
		DefaultMaxIterations: 1,
		DefaultTimeout:       5 * time.Second,
		LLMProvider:          "openai",
		LLMModel:             "test-model",
		LLMAPIKey:            "test-key",
		LLMBaseURL:           "http://example.invalid",
	})

	eng, _, err := rt.createEngine(types.AgentConfig{
		Role:          types.RoleCoder,
		ActorID:       "actor:test:coder",
		WorkspaceID:   "ws-test",
		MaxIterations: 1,
		Timeout:       5 * time.Second,
	}, "session-test")
	if err != nil {
		t.Fatalf("createEngine() error = %v; gate-on should succeed with valid config", err)
	}
	if eng == nil {
		t.Fatal("createEngine() engine = nil; expected a provisioned EinoEngineAdapter")
	}
}

func TestCreateEngine_EinoGatePassesTools(t *testing.T) {
	t.Setenv(einoadapter.EnvEngineBackend, "eino")

	rt := NewRuntime(Config{
		DefaultMaxIterations: 1,
		DefaultTimeout:       5 * time.Second,
		LLMProvider:          "openai",
		LLMModel:             "test-model",
		LLMAPIKey:            "test-key",
		LLMBaseURL:           "http://example.invalid",
	})

	_, tools, err := rt.createEngine(types.AgentConfig{
		Role:          types.RoleCoder,
		ActorID:       "actor:test:coder",
		WorkspaceID:   "ws-test",
		MaxIterations: 1,
		Timeout:       5 * time.Second,
	}, "session-test")
	if err != nil {
		t.Fatalf("createEngine() error = %v", err)
	}

	// Verify that tools are returned even when Eino is enabled.
	// In Milestone 1 spike, tools might have been empty or ignored.
	// In Milestone 2, they must be passed through and returned.
	if len(tools) == 0 {
		t.Error("createEngine() returned no tool definitions when Eino is enabled")
	}

	// Check for a known coder tool
	found := false
	for _, tool := range tools {
		if tool.Name == "fs_read_file" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("createEngine() tools missing 'fs_read_file'; got %v", tools)
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
	for _, want := range []string{"think", "context_search", "semantic_search_code", "smart_search", "memory_query"} {
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

	for _, want := range []string{"think", "repo_index_build", "repo_index_enrich_summaries", "repo_index_search", "repo_index_expand", "repo_index_open", "repo_index_dag_grep"} {
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

	for _, want := range []string{"think", "code_symbols", "context_grep", "code_search", "refactor_scout"} {
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

func TestBuildToolDefsForRole_MemoryFactScout(t *testing.T) {
	names := toolNamesForRole(types.RoleMemoryFactScout)

	for _, want := range []string{"think", "semantic_search_memories", "agent_memory_search", "agent_memory_context", "memory_query", "session_recall", "annotation_recall", "context_filter"} {
		if !hasToolName(names, want) {
			t.Errorf("memory_fact_scout should have %q, got %v", want, names)
		}
	}

	for _, deny := range []string{"fs_read_file", "code_search", "fs_list_dir", "fs_write_file"} {
		if hasToolName(names, deny) {
			t.Errorf("memory_fact_scout should NOT have %q", deny)
		}
	}
}

func TestBuildToolDefsForRole_MemoryTimelineScout(t *testing.T) {
	names := toolNamesForRole(types.RoleMemoryTimelineScout)

	for _, want := range []string{"think", "semantic_search_sessions", "session_timeline", "session_recall", "agent_memory_search", "agent_memory_context", "context_filter"} {
		if !hasToolName(names, want) {
			t.Errorf("memory_timeline_scout should have %q, got %v", want, names)
		}
	}

	for _, deny := range []string{"fs_read_file", "code_search", "fs_list_dir", "fs_write_file", "annotation_recall", "annotation_category_stats"} {
		if hasToolName(names, deny) {
			t.Errorf("memory_timeline_scout should NOT have %q", deny)
		}
	}
}

func TestBuildToolDefsForRole_ContextWikiContextScout(t *testing.T) {
	names := toolNamesForRole(types.RoleContextWikiScout)

	for _, want := range []string{"think", "semantic_search_context", "context_show", "context_retrieve", "obsidian_index_search", "obsidian_read", "obsidian_related", "context_filter"} {
		if !hasToolName(names, want) {
			t.Errorf("contextwiki_scout should have %q, got %v", want, names)
		}
	}

	for _, deny := range []string{"fs_read_file", "code_search", "fs_list_dir", "fs_write_file", "memory_query"} {
		if hasToolName(names, deny) {
			t.Errorf("contextwiki_scout should NOT have %q", deny)
		}
	}
}

func TestBuildToolDefsForRole_ResearcherUnchanged(t *testing.T) {
	names := toolNamesForRole(types.RoleResearcher)

	// Researcher must still have base tools + foxctl tools
	for _, want := range []string{"fs_read_file", "code_search", "think", "shell", "context_search", "semantic_search_code", "semantic_search_sessions", "semantic_search_memories", "semantic_search_context", "smart_search", "refactor_scout", "code_search_ensemble", "context_grep", "code_symbols", "repo_index_build", "repo_index_enrich_summaries", "repo_index_search", "annotation_recall", "context_show", "context_retrieve", "obsidian_index_search", "obsidian_read", "obsidian_related"} {
		if !hasToolName(names, want) {
			t.Errorf("researcher should still have %q, got %v", want, names)
		}
	}
}

func TestBuildToolDefsForRole_SubcallWorker(t *testing.T) {
	names := toolNamesForRole(types.RoleSubcallWorker)

	for _, want := range []string{"fs_read_file", "code_search", "think", "shell", "context_search", "smart_search", "refactor_scout", "code_search_ensemble", "context_grep", "code_symbols", "repo_index_build", "repo_index_enrich_summaries", "repo_index_search", "memory_query", "session_recall", "context_show", "context_retrieve"} {
		if !hasToolName(names, want) {
			t.Errorf("subcall_worker should have %q, got %v", want, names)
		}
	}

	for _, deny := range []string{"fs_list_dir", "fs_write_file", "heartwood_state", "heartwood_action"} {
		if hasToolName(names, deny) {
			t.Errorf("subcall_worker should NOT have %q, got %v", deny, names)
		}
	}
}

func TestIsRefactorEntryPrompt(t *testing.T) {
	if !isRefactorEntryPrompt("Find the strongest refactor entrypoints in Go") {
		t.Fatal("expected refactor-entrypoint prompt to match")
	}
	if isRefactorEntryPrompt("Summarize the current architecture") {
		t.Fatal("did not expect non-refactor prompt to match")
	}
}

func TestInferRefactorScoutLanguage(t *testing.T) {
	if got := inferRefactorScoutLanguage("In the Go code under internal/ find refactor hotspots"); got != "go" {
		t.Fatalf("got %q want go", got)
	}
	if got := inferRefactorScoutLanguage("In the TypeScript code under packages/ find refactor hotspots"); got != "typescript" {
		t.Fatalf("got %q want typescript", got)
	}
}

func TestInferRefactorScoutPath(t *testing.T) {
	if got := inferRefactorScoutPath("In the Go code under internal/ find refactor hotspots"); got != "internal" {
		t.Fatalf("got %q want internal", got)
	}
	if got := inferRefactorScoutPath("In the Go code under cmd/foxctl/cmd find refactor hotspots"); got != "cmd/foxctl/cmd" {
		t.Fatalf("got %q want cmd/foxctl/cmd", got)
	}
}

func TestApplyRefactorRouteToolSubset_Researcher(t *testing.T) {
	r := NewRuntime(Config{})
	tools := []engine.ToolDef{
		{Name: "think"},
		{Name: "refactor_scout"},
		{Name: "semantic_search_code"},
		{Name: "smart_search"},
		{Name: "repo_index_search"},
		{Name: "code_symbols"},
		{Name: "fs_read_file"},
		{Name: "context_search"},
		{Name: "code_search"},
		{Name: "context_grep"},
	}
	got := r.applyRefactorRouteToolSubset(types.RoleResearcher, "Find the strongest refactor entrypoints in the Go code under internal/", tools)
	names := make([]string, 0, len(got))
	for _, tool := range got {
		names = append(names, tool.Name)
	}
	if hasToolName(names, "code_search") {
		t.Fatalf("code_search should be filtered for refactor route, got %v", names)
	}
	for _, want := range []string{"refactor_scout", "semantic_search_code", "smart_search", "repo_index_search", "code_symbols", "fs_read_file"} {
		if !hasToolName(names, want) {
			t.Fatalf("missing %s in %v", want, names)
		}
	}
}

func TestMergeRefactorScoutTaskPrompt(t *testing.T) {
	got := mergeRefactorScoutTaskPrompt("Find refactor entrypoints.", "Scout says: use foo.go::Bar")
	if !strings.Contains(got, "Scout says: use foo.go::Bar") {
		t.Fatalf("preface missing: %q", got)
	}
	if !strings.Contains(got, "Original task:\nFind refactor entrypoints.") {
		t.Fatalf("original task missing: %q", got)
	}
}

func TestBuildToolDefsForRole_CoderUnchanged(t *testing.T) {
	names := toolNamesForRole(types.RoleCoder)

	for _, want := range []string{"fs_read_file", "code_search", "think", "shell", "fs_write_file", "fs_list_dir", "heartwood_state", "heartwood_action"} {
		if !hasToolName(names, want) {
			t.Errorf("coder should still have %q, got %v", want, names)
		}
	}
}

func TestBuildToolDefsForRole_OverseerIncludesShell(t *testing.T) {
	names := toolNamesForRole(types.RoleOverseer)

	for _, want := range []string{"think", "shell", "context_search", "smart_search", "code_search_ensemble", "agent_spawn"} {
		if !hasToolName(names, want) {
			t.Errorf("overseer should have %q, got %v", want, names)
		}
	}
}

func TestSummarizeToolData_Shell(t *testing.T) {
	got := summarizeToolData("shell", map[string]any{
		"summary": "abc123 fix foo\nabc456 add bar",
		"route": map[string]any{
			"skill":  "git/status",
			"intent": "git_log",
			"notes":  []any{"ignored --stat and returned compact commit log output"},
		},
		"measure": map[string]any{
			"raw":     map[string]any{"combined_tokens": 200.0, "combined_bytes": 800.0},
			"reduced": map[string]any{"tokens": 20.0, "bytes": 120.0},
			"savings": map[string]any{"tokens_saved_percent": 90.0, "bytes_saved_percent": 85.0},
		},
	})
	for _, want := range []string{
		"Structured shell",
		"Backend: git/status",
		"Intent: git_log",
		"Notes: ignored --stat and returned compact commit log output",
		"abc123 fix foo",
		"raw 200 -> reduced 20",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary missing %q\n%s", want, got)
		}
	}
}

func TestBuildRepoIndexSearchInputPreservesInlineMode(t *testing.T) {
	input, err := buildRepoIndexSearchInput(map[string]any{
		"query":       " gather_context ",
		"inline_mode": " preview ",
		"limit":       float64(10),
	}, "/tmp/workspace")
	if err != nil {
		t.Fatalf("buildRepoIndexSearchInput returned error: %v", err)
	}
	if got, want := input["query"], "gather_context"; got != want {
		t.Fatalf("query=%v want %s", got, want)
	}
	if got, want := input["workspace"], "/tmp/workspace"; got != want {
		t.Fatalf("workspace=%v want %s", got, want)
	}
	if got, want := input["limit"], 10; got != want {
		t.Fatalf("limit=%v want %d", got, want)
	}
	if got, want := input["inline_mode"], "preview"; got != want {
		t.Fatalf("inline_mode=%v want %s", got, want)
	}
}

func TestBuildRepoIndexExpandInputPreservesInlineModeAndDirection(t *testing.T) {
	input, err := buildRepoIndexExpandInput(map[string]any{
		"seeds":        []any{"sym:one", "sym:two"},
		"edge_types":   []any{"CALLS"},
		"direction":    " in ",
		"inline_mode":  " full ",
		"depth":        float64(2),
		"budget":       float64(12),
		"per_node_cap": float64(4),
	}, "")
	if err != nil {
		t.Fatalf("buildRepoIndexExpandInput returned error: %v", err)
	}
	if got, want := input["workspace"], "."; got != want {
		t.Fatalf("workspace=%v want %s", got, want)
	}
	if got, want := input["direction"], "in"; got != want {
		t.Fatalf("direction=%v want %s", got, want)
	}
	if got, want := input["inline_mode"], "full"; got != want {
		t.Fatalf("inline_mode=%v want %s", got, want)
	}
	if got, want := input["depth"], 2; got != want {
		t.Fatalf("depth=%v want %d", got, want)
	}
	if got, want := input["budget"], 12; got != want {
		t.Fatalf("budget=%v want %d", got, want)
	}
	if got, want := input["per_node_cap"], 4; got != want {
		t.Fatalf("per_node_cap=%v want %d", got, want)
	}
}

func TestStringArgUsesEveryArgumentAsAKey(t *testing.T) {
	args := map[string]any{"inline_mode": " preview "}
	if got, want := stringArg(args, "inline_mode", ""), "preview"; got != want {
		t.Fatalf("stringArg=%q want %q", got, want)
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
	var callCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
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
	if callCount.Load() < 2 {
		t.Errorf("expected at least 2 LLM calls (1 failure + 1 success), got %d", callCount.Load())
	}
}

func TestEngineRetryExhausted(t *testing.T) {
	// Mock server: always returns 500 — both attempts fail.
	var callCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
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
	calls := callCount.Load()
	if calls < 2 {
		t.Errorf("expected at least 2 LLM calls (original + 1 retry), got %d", calls)
	}
}

func TestEngineMaxIterationsDoesNotRetry(t *testing.T) {
	// Mock server:
	// 1) First call returns a tool call with no assistant text.
	// 2) Finalize call returns assistant text.
	// Any additional call indicates runtime retried the engine run.
	var callCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
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
	calls := callCount.Load()
	if calls != 2 {
		t.Errorf("expected exactly 2 LLM calls (tool-call + finalize) with no retry, got %d", calls)
	}
}

func TestEngineRetrySkipsContextErrors(t *testing.T) {
	// Mock server: returns 500, but context is canceled so no retry should happen.
	var callCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
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
