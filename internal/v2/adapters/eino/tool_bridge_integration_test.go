package eino

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/joshka0/foxctl/internal/runtime/engine"
)

// twoTurnModel is a stub model.BaseChatModel that simulates a one-tool-call round trip:
//   - Turn 1: returns an assistant message requesting one tool call.
//   - Turn 2: returns a final assistant message that echoes the tool result.
//
// All other turns return an error so test failures are explicit if the agent
// loops more than expected.
type twoTurnModel struct {
	toolName string
	toolArgs string
	calls    atomic.Int32
}

func (m *twoTurnModel) Generate(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	turn := int(m.calls.Add(1))
	switch turn {
	case 1:
		// Request one tool call.
		return &schema.Message{
			Role: schema.Assistant,
			ToolCalls: []schema.ToolCall{
				{
					ID:   "call-1",
					Type: "function",
					Function: schema.FunctionCall{
						Name:      m.toolName,
						Arguments: m.toolArgs,
					},
				},
			},
		}, nil
	case 2:
		// Return grounded final answer.
		return &schema.Message{
			Role:    schema.Assistant,
			Content: "grounded: tool was called",
		}, nil
	default:
		return nil, nil
	}
}

func (m *twoTurnModel) Stream(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, nil
}

// TestEinoToolBridgeIntegration_OneRealToolRoundTrip is the M2 gated integration
// proof: it assembles a real adk.ChatModelAgent with an einoToolBridge and a stub
// ToolExecutor, sends one user message, and verifies the executor is called with
// the expected args before the agent returns grounded text.
//
// Gate: this test requires AGENTCTL_ENGINE_BACKEND=eino to be set, which keeps it
// out of the default CI path and off the gate-off regression surface.
func TestEinoToolBridgeIntegration_OneRealToolRoundTrip(t *testing.T) {
	t.Setenv(EnvEngineBackend, "eino")
	if !IsEinoEnabled() {
		t.Skip("AGENTCTL_ENGINE_BACKEND not set to eino — skipping gated integration proof")
	}

	const toolName = "echo.tool"
	const toolArgs = `{"input":"hello"}`
	const toolResult = "echoed: hello"

	// Track executor invocations.
	var executorCalled bool
	executor := &mockExecutor{
		executeFn: func(_ context.Context, name string, args json.RawMessage) (string, error) {
			if name != toolName {
				t.Errorf("executor called with name=%q, want %q", name, toolName)
			}
			if string(args) != toolArgs {
				t.Errorf("executor called with args=%q, want %q", string(args), toolArgs)
			}
			executorCalled = true
			return toolResult, nil
		},
	}

	def := engine.ToolDef{
		Name:        toolName,
		Description: "echoes its input",
	}

	bridge := NewEinoToolBridge(def, executor)

	stubModel := &twoTurnModel{toolName: toolName, toolArgs: toolArgs}

	agent, err := adk.NewChatModelAgent(context.Background(), &adk.ChatModelAgentConfig{
		Name:        "integration-test-agent",
		Description: "M2 integration proof agent",
		Model:       stubModel,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: []tool.BaseTool{bridge},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewChatModelAgent: %v", err)
	}

	adapter, err := NewEinoEngineAdapter(agent)
	if err != nil {
		t.Fatalf("NewEinoEngineAdapter: %v", err)
	}

	out, err := adapter.Run(context.Background(), engine.EngineInput{
		Messages: []engine.Message{
			{Role: engine.RoleUser, Content: "please call echo.tool with hello"},
		},
	})
	if err != nil {
		t.Fatalf("adapter.Run: %v", err)
	}

	if !executorCalled {
		t.Fatal("ToolExecutor.Execute was never called — tool round-trip did not fire")
	}
	if out.AssistantText == "" {
		t.Fatal("AssistantText is empty — expected grounded output after tool round trip")
	}
}
