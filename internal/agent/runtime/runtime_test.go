package runtime

import (
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/agent/types"
	"github.com/jkatigb/agentctl/internal/agentprompt"
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
