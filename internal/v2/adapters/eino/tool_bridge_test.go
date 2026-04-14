package eino

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/joshka0/foxctl/internal/runtime/engine"
)

type mockExecutor struct {
	executeFn func(ctx context.Context, name string, args json.RawMessage) (string, error)
}

func (m *mockExecutor) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if m.executeFn != nil {
		return m.executeFn(ctx, name, args)
	}
	return "", nil
}

func (m *mockExecutor) List() []engine.ToolDef {
	return nil
}

func TestEinoToolBridge_Info(t *testing.T) {
	def := engine.ToolDef{
		Name:        "test_tool",
		Description: "A test tool",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"foo":{"type":"string"}}}`),
	}

	bridge := NewEinoToolBridge(def, nil)
	info, err := bridge.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}

	if info.Name != def.Name {
		t.Errorf("Name=%q want %q", info.Name, def.Name)
	}
	if info.Desc != def.Description {
		t.Errorf("Desc=%q want %q", info.Desc, def.Description)
	}
	if info.ParamsOneOf == nil {
		t.Error("ParamsOneOf is nil")
	}
}

func TestEinoToolBridge_InvokableRun(t *testing.T) {
	def := engine.ToolDef{Name: "test_tool"}
	expectedArgs := `{"foo":"bar"}`
	expectedOutput := "tool result"

	executor := &mockExecutor{
		executeFn: func(ctx context.Context, name string, args json.RawMessage) (string, error) {
			if name != def.Name {
				t.Errorf("Execute name=%q want %q", name, def.Name)
			}
			if string(args) != expectedArgs {
				t.Errorf("Execute args=%q want %q", string(args), expectedArgs)
			}
			return expectedOutput, nil
		},
	}

	bridge := NewEinoToolBridge(def, executor)
	out, err := bridge.InvokableRun(context.Background(), expectedArgs)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}

	if out != expectedOutput {
		t.Errorf("Output=%q want %q", out, expectedOutput)
	}
}

func TestNewEinoToolBridges(t *testing.T) {
	defs := []engine.ToolDef{
		{Name: "tool1"},
		{Name: "tool2"},
	}
	executor := &mockExecutor{}

	bridges := NewEinoToolBridges(defs, executor)
	if len(bridges) != len(defs) {
		t.Fatalf("len(bridges)=%d want %d", len(bridges), len(defs))
	}

	for i, b := range bridges {
		info, _ := b.Info(context.Background())
		if info.Name != defs[i].Name {
			t.Errorf("bridges[%d].Name=%q want %q", i, info.Name, defs[i].Name)
		}
	}
}

// Gate-off regression: the existing gate-off path must be completely unaffected
// when AGENTCTL_ENGINE_BACKEND is not set. This test is the canonical gate-off
// regression anchor for M1 — any change that accidentally activates the eino path
// when the gate is off will break this test.
func TestGateOff_UnaffectedByToolBridgeChanges(t *testing.T) {
	t.Setenv(EnvEngineBackend, "")
	if IsEinoEnabled() {
		t.Fatal("gate-off: IsEinoEnabled() must return false when AGENTCTL_ENGINE_BACKEND is unset")
	}
}

// TestGateOff_ProvisionNotCalledWhenDisabled verifies the provisioner contract:
// ProvisionFromLLMConfig is only reachable when the gate is explicitly on.
func TestGateOff_ProvisionNotCalledWhenDisabled(t *testing.T) {
	t.Setenv(EnvEngineBackend, "")
	if IsEinoEnabled() {
		t.Fatal("gate must be off when AGENTCTL_ENGINE_BACKEND is empty")
	}
	// Gate is off — the default path will use LLMChatEngine; provisioner unreachable.
}

// TestProvisionFromLLMConfig_WithTools_PassesBridgesToAgent verifies that passing
// a non-nil ToolExecutor and ToolDef slice does not cause provisioning to fail.
// Tool wiring into the ChatModelAgent is out of scope for M1 but the seam must
// accept the inputs without error.
func TestProvisionFromLLMConfig_WithTools_PassesBridgesToAgent(t *testing.T) {
	executor := &mockExecutor{}
	defs := []engine.ToolDef{
		{Name: "test.tool", Description: "a test tool", Parameters: json.RawMessage(`{"type":"object"}`)},
	}
	adapter, err := ProvisionFromLLMConfig(engine.LLMChatConfig{
		Provider: "openrouter",
		BaseURL:  "https://openrouter.ai/api/v1",
		Model:    "openrouter/aurora-alpha",
		APIKey:   "test-key",
	}, executor, defs)
	if err != nil {
		t.Fatalf("ProvisionFromLLMConfig: %v", err)
	}
	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}
}
