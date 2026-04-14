package runtime

import (
	"testing"

	"github.com/joshka0/foxctl/internal/agent/types"
	"github.com/joshka0/foxctl/internal/runtime/engine"
)

func TestWorkspaceRootForConfig_UsesOverride(t *testing.T) {
	rt := &Runtime{
		config: Config{
			WorkspaceRoot: "/default/workspace",
		},
	}

	got := rt.workspaceRootForConfig(types.AgentConfig{WorkspaceRoot: "/override/workspace"})
	if got != "/override/workspace" {
		t.Fatalf("workspace root=%q want override", got)
	}

	fallback := rt.workspaceRootForConfig(types.AgentConfig{})
	if fallback != "/default/workspace" {
		t.Fatalf("workspace root fallback=%q", fallback)
	}
}

func TestBuildEngineInput_UsesSessionWorkspaceOverride(t *testing.T) {
	rt := &Runtime{
		config: Config{
			WorkspaceRoot: "/default/workspace",
		},
	}
	session := &Session{
		ID: "sess-1",
		Config: types.AgentConfig{
			WorkspaceRoot: "/vault/workspace",
		},
		SystemPrompt: "system",
		Tools: []engine.ToolDef{
			{Name: "fs.read_file"},
		},
	}

	input := rt.buildEngineInput(session, "hello")
	if input.Workspace != "/vault/workspace" {
		t.Fatalf("workspace=%q want override", input.Workspace)
	}
}
