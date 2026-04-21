package llm

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/joshka0/foxctl/internal/runtime/engine"
	corerun "github.com/joshka0/foxctl/internal/v2/core/run"
	coretool "github.com/joshka0/foxctl/internal/v2/core/tool"
	"github.com/joshka0/foxctl/internal/v2/runtime/runner"
)

func TestChatModelPassesToolsAndReturnsToolCalls(t *testing.T) {
	t.Parallel()

	var got engine.EngineInput
	model := NewChatModelForTest(func(_ context.Context, in engine.EngineInput) (engine.EngineOutput, error) {
		got = in
		return engine.EngineOutput{
			ToolCalls: []engine.ToolCall{{
				ID:        "call-1",
				Name:      "context_show",
				Arguments: json.RawMessage(`{"scope":"current"}`),
			}},
			StopReason: engine.StopReasonEndTurn,
		}, nil
	})

	resp, err := model.Complete(context.Background(), runner.ModelInput{
		Prompt: "show context",
		Tools: []coretool.ToolDef{{
			Name:        "context_show",
			Description: "Read context",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		}},
		Messages: []runner.ModelMessage{
			{Role: "user", Content: "show context"},
			{
				Role: "assistant",
				ToolCalls: []corerun.ToolCall{{
					ID:   "prior-call",
					Name: "context_show",
					Args: json.RawMessage(`{}`),
				}},
			},
			{Role: "tool", ToolCallID: "prior-call", Name: "context_show", Content: "previous result"},
		},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if resp.Done {
		t.Fatal("expected tool call response to keep the runner loop open")
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("tool calls=%d want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "call-1" || resp.ToolCalls[0].Name != "context_show" {
		t.Fatalf("tool call=%+v", resp.ToolCalls[0])
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "context_show" {
		t.Fatalf("engine tools=%+v", got.Tools)
	}
	if len(got.Messages) != 3 {
		t.Fatalf("messages=%d want 3", len(got.Messages))
	}
	if got.Messages[1].ToolCalls[0].ID != "prior-call" {
		t.Fatalf("prior tool call=%+v", got.Messages[1].ToolCalls[0])
	}
	if got.Messages[2].ToolCallID != "prior-call" {
		t.Fatalf("tool result message=%+v", got.Messages[2])
	}
}
