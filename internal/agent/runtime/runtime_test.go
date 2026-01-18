package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/XiaoConstantine/dspy-go/pkg/agents"
	"github.com/XiaoConstantine/dspy-go/pkg/core"
	"github.com/jkatigb/agentctl/internal/agent/types"
	"github.com/jkatigb/agentctl/internal/hooks"
)

type stubAgent struct {
	calls   []map[string]interface{}
	results []map[string]interface{}
}

func (s *stubAgent) Execute(_ context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	s.calls = append(s.calls, input)
	if len(s.results) == 0 {
		return map[string]interface{}{"result": "done"}, nil
	}
	result := s.results[0]
	s.results = s.results[1:]
	return result, nil
}

func (s *stubAgent) GetCapabilities() []core.Tool {
	return nil
}

func (s *stubAgent) GetMemory() agents.Memory {
	return nil
}

type stubHookDispatcher struct {
	blockOnce bool
	calls     []hooks.Input
}

func (s *stubHookDispatcher) Dispatch(ctx context.Context, input hooks.Input) (hooks.Result, error) {
	s.calls = append(s.calls, input)
	if s.blockOnce {
		s.blockOnce = false
		output := hooks.NewBlock("blocked by stop guard")
		output.Context = "Please run tests and continue."
		return hooks.Result{Output: output, Blocked: true, BlockedBy: "stop_guard"}, nil
	}
	return hooks.Result{Output: hooks.NewApprove("ok", nil)}, nil
}

func (s *stubHookDispatcher) DispatchAsync(ctx context.Context, input hooks.Input) <-chan hooks.Result {
	ch := make(chan hooks.Result, 1)
	result, _ := s.Dispatch(ctx, input)
	ch <- result
	close(ch)
	return ch
}

func TestBuildAgentSignature_CoderRole(t *testing.T) {
	cfg := types.AgentConfig{
		Role:        types.RoleCoder,
		ActorID:     "test-coder",
		WorkspaceID: "test-ws",
	}

	sig := buildAgentSignature(cfg)
	if sig == nil {
		t.Fatal("expected non-nil signature")
	}

	instruction := sig.Instruction

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

func TestBuildAgentSignature_PlannerRole(t *testing.T) {
	cfg := types.AgentConfig{
		Role:        types.RolePlanner,
		ActorID:     "test-planner",
		WorkspaceID: "test-ws",
	}

	sig := buildAgentSignature(cfg)
	if sig == nil {
		t.Fatal("expected non-nil signature")
	}

	instruction := sig.Instruction

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

func TestBuildAgentSignature_DefaultRole(t *testing.T) {
	cfg := types.AgentConfig{
		Role:        "unknown",
		ActorID:     "test-agent",
		WorkspaceID: "test-ws",
	}

	sig := buildAgentSignature(cfg)
	if sig == nil {
		t.Fatal("expected non-nil signature")
	}

	instruction := sig.Instruction

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

func TestBuildAgentSignature_HasInputAndOutputFields(t *testing.T) {
	cfg := types.AgentConfig{
		Role:        types.RoleCoder,
		ActorID:     "test-coder",
		WorkspaceID: "test-ws",
	}

	sig := buildAgentSignature(cfg)
	if sig == nil {
		t.Fatal("expected non-nil signature")
	}

	// Verify input field
	if len(sig.Inputs) == 0 {
		t.Error("signature should have input fields")
	}
	hasTaskInput := false
	for _, f := range sig.Inputs {
		if f.Name == "task" {
			hasTaskInput = true
			break
		}
	}
	if !hasTaskInput {
		t.Error("signature should have 'task' input field")
	}

	// Verify output field
	if len(sig.Outputs) == 0 {
		t.Error("signature should have output fields")
	}
	hasResultOutput := false
	for _, f := range sig.Outputs {
		if f.Name == "result" {
			hasResultOutput = true
			break
		}
	}
	if !hasResultOutput {
		t.Error("signature should have 'result' output field")
	}
}

func TestCoderSignature_ToolCategories(t *testing.T) {
	cfg := types.AgentConfig{
		Role:        types.RoleCoder,
		ActorID:     "test-coder",
		WorkspaceID: "test-ws",
	}

	sig := buildAgentSignature(cfg)
	instruction := sig.Instruction

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

func TestBuildAgentSignature_ReviewerRole(t *testing.T) {
	cfg := types.AgentConfig{
		Role:        types.RoleReviewer,
		ActorID:     "test-reviewer",
		WorkspaceID: "test-ws",
	}

	sig := buildAgentSignature(cfg)
	if sig == nil {
		t.Fatal("expected non-nil signature")
	}

	instruction := sig.Instruction

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
	dispatcher := &stubHookDispatcher{blockOnce: true}
	rt := &Runtime{
		config: Config{
			HookDispatcher: dispatcher,
			WorkspaceRoot:  "/workspace",
		},
	}
	agent := &stubAgent{
		results: []map[string]interface{}{
			{"result": "first response"},
			{"result": "final response"},
		},
	}
	session := &Session{
		ID:     "session-1",
		Status: types.StatusRunning,
		Agent:  agent,
		Config: types.AgentConfig{
			Role:        types.RoleCoder,
			ActorID:     "actor:agent:test",
			WorkspaceID: "ws-1",
			Timeout:     time.Minute,
		},
	}

	rt.runSession(context.Background(), session)

	if session.Status != types.StatusOK {
		t.Fatalf("expected status ok, got %s (error=%s)", session.Status, session.Error)
	}
	if session.Summary != "final response" {
		t.Fatalf("expected final response, got %q", session.Summary)
	}
	if len(agent.calls) != 2 {
		t.Fatalf("expected 2 agent calls, got %d", len(agent.calls))
	}
	if len(dispatcher.calls) != 2 {
		t.Fatalf("expected 2 hook calls, got %d", len(dispatcher.calls))
	}
	for i, call := range dispatcher.calls {
		if call.Event != hooks.EventStopRequested {
			t.Fatalf("hook call %d event = %s, want StopRequested", i, call.Event)
		}
	}

	secondPrompt, ok := agent.calls[1]["task"].(string)
	if !ok {
		t.Fatalf("expected task prompt string, got %T", agent.calls[1]["task"])
	}
	if !strings.Contains(secondPrompt, "Previous response:\nfirst response") {
		t.Errorf("expected prompt to include previous response, got %q", secondPrompt)
	}
	if !strings.Contains(secondPrompt, "Please run tests and continue.") {
		t.Errorf("expected prompt to include stop context, got %q", secondPrompt)
	}
}

func TestReviewerSignature_ToolCategories(t *testing.T) {
	cfg := types.AgentConfig{
		Role:        types.RoleReviewer,
		ActorID:     "test-reviewer",
		WorkspaceID: "test-ws",
	}

	sig := buildAgentSignature(cfg)
	instruction := sig.Instruction

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
