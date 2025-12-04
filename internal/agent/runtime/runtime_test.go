package runtime

import (
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/agent/types"
)

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
