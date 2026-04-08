package eino

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/jkatigb/agentctl/internal/engine"
)

// stubAgent is a minimal adk.Agent for testing the adapter seam.
type stubAgent struct {
	name   string
	events []*adk.AgentEvent
}

func (s *stubAgent) Name(_ context.Context) string        { return s.name }
func (s *stubAgent) Description(_ context.Context) string { return "stub" }

func (s *stubAgent) Run(_ context.Context, _ *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	for _, e := range s.events {
		gen.Send(e)
	}
	gen.Close()
	return iter
}

func makeAssistantEvent(text string) *adk.AgentEvent {
	msg := &schema.Message{Role: schema.Assistant, Content: text}
	return &adk.AgentEvent{
		Output: &adk.AgentOutput{
			MessageOutput: &adk.MessageVariant{
				IsStreaming: false,
				Message:     msg,
				Role:        schema.Assistant,
			},
		},
	}
}

func makeToolCallEvent(id, name, args string) *adk.AgentEvent {
	msg := &schema.Message{
		Role: schema.Assistant,
		ToolCalls: []schema.ToolCall{
			{
				ID: id,
				Function: schema.FunctionCall{
					Name:      name,
					Arguments: args,
				},
			},
		},
	}
	return &adk.AgentEvent{
		Output: &adk.AgentOutput{
			MessageOutput: &adk.MessageVariant{
				IsStreaming: false,
				Message:     msg,
				Role:        schema.Assistant,
			},
		},
	}
}

func makeToolResultEvent(callID, content string) *adk.AgentEvent {
	msg := &schema.Message{
		Role:       schema.Tool,
		ToolCallID: callID,
		Content:    content,
	}
	return &adk.AgentEvent{
		Output: &adk.AgentOutput{
			MessageOutput: &adk.MessageVariant{
				IsStreaming: false,
				Message:     msg,
				Role:        schema.Tool,
			},
		},
	}
}

func TestEinoEngineAdapter_CollectsAssistantText(t *testing.T) {
	t.Parallel()

	stub := &stubAgent{
		name: "test-agent",
		events: []*adk.AgentEvent{
			makeAssistantEvent("Hello"),
			makeAssistantEvent(", world"),
		},
	}

	adapter, err := NewEinoEngineAdapter(stub)
	if err != nil {
		t.Fatalf("NewEinoEngineAdapter: %v", err)
	}

	out, err := adapter.Run(context.Background(), engine.EngineInput{
		Messages: []engine.Message{
			{Role: engine.RoleUser, Content: "Say hello"},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.AssistantText != "Hello, world" {
		t.Fatalf("AssistantText=%q want %q", out.AssistantText, "Hello, world")
	}
	if out.StopReason != engine.StopReasonEndTurn {
		t.Fatalf("StopReason=%q want %q", out.StopReason, engine.StopReasonEndTurn)
	}
}

func TestEinoEngineAdapter_CollectsToolCallsAndResults(t *testing.T) {
	t.Parallel()

	stub := &stubAgent{
		name: "tool-agent",
		events: []*adk.AgentEvent{
			makeToolCallEvent("call_1", "fs_ls", `{"path":"."}`),
			makeToolResultEvent("call_1", "file1.go\nfile2.go"),
			makeAssistantEvent("I found some files."),
		},
	}

	adapter, err := NewEinoEngineAdapter(stub)
	if err != nil {
		t.Fatalf("NewEinoEngineAdapter: %v", err)
	}

	out, err := adapter.Run(context.Background(), engine.EngineInput{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(out.ToolCalls) != 1 {
		t.Fatalf("len(ToolCalls)=%d want 1", len(out.ToolCalls))
	}
	if out.ToolCalls[0].ID != "call_1" {
		t.Errorf("ToolCall[0].ID=%q want %q", out.ToolCalls[0].ID, "call_1")
	}
	if out.ToolCalls[0].Name != "fs_ls" {
		t.Errorf("ToolCall[0].Name=%q want %q", out.ToolCalls[0].Name, "fs_ls")
	}

	if len(out.ToolResults) != 1 {
		t.Fatalf("len(ToolResults)=%d want 1", len(out.ToolResults))
	}
	if out.ToolResults[0].ToolCallID != "call_1" {
		t.Errorf("ToolResult[0].ToolCallID=%q want %q", out.ToolResults[0].ToolCallID, "call_1")
	}
	if out.ToolResults[0].Content != "file1.go\nfile2.go" {
		t.Errorf("ToolResult[0].Content=%q want %q", out.ToolResults[0].Content, "file1.go\nfile2.go")
	}

	if out.AssistantText != "I found some files." {
		t.Errorf("AssistantText=%q want %q", out.AssistantText, "I found some files.")
	}
}

func TestEinoEngineAdapter_EmptyEventsReturnsEmptyOutput(t *testing.T) {
	t.Parallel()

	stub := &stubAgent{name: "empty-agent", events: nil}
	adapter, err := NewEinoEngineAdapter(stub)
	if err != nil {
		t.Fatalf("NewEinoEngineAdapter: %v", err)
	}

	out, err := adapter.Run(context.Background(), engine.EngineInput{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.AssistantText != "" {
		t.Fatalf("AssistantText=%q want empty", out.AssistantText)
	}
}

func TestNewEinoEngineAdapter_RejectsNilAgent(t *testing.T) {
	t.Parallel()

	_, err := NewEinoEngineAdapter(nil)
	if err == nil {
		t.Fatal("expected error for nil agent")
	}
}

// TestIsEinoEnabled_GateOffByDefault verifies the default-path regression:
// when AGENTCTL_ENGINE_BACKEND is unset, IsEinoEnabled() must return false
// so the default LLMChatEngine path is never replaced.
func TestIsEinoEnabled_GateOffByDefault(t *testing.T) {
	t.Setenv(EnvEngineBackend, "")
	if IsEinoEnabled() {
		t.Fatal("IsEinoEnabled() must be false when AGENTCTL_ENGINE_BACKEND is unset")
	}
}

func TestIsEinoEnabled_TrueWhenSet(t *testing.T) {
	t.Setenv(EnvEngineBackend, "eino")
	if !IsEinoEnabled() {
		t.Fatal("IsEinoEnabled() must be true when AGENTCTL_ENGINE_BACKEND=eino")
	}
}

func TestIsEinoEnabled_CaseInsensitive(t *testing.T) {
	t.Setenv(EnvEngineBackend, "EINO")
	if !IsEinoEnabled() {
		t.Fatal("IsEinoEnabled() must be case-insensitive")
	}
}

// TestProvisionFromLLMConfig_RejectsBedrock verifies Bedrock is rejected cleanly.
func TestProvisionFromLLMConfig_RejectsBedrock(t *testing.T) {
	_, err := ProvisionFromLLMConfig(engine.LLMChatConfig{
		Provider: "bedrock",
		BaseURL:  "us-east-1",
		Model:    "some-model",
	}, nil, nil)
	if err == nil {
		t.Fatal("expected error for Bedrock provider")
	}
}

// TestProvisionFromLLMConfig_RejectsEmptyBaseURL verifies required field validation.
func TestProvisionFromLLMConfig_RejectsEmptyBaseURL(t *testing.T) {
	_, err := ProvisionFromLLMConfig(engine.LLMChatConfig{
		Provider: "openrouter",
		BaseURL:  "",
		Model:    "some-model",
	}, nil, nil)
	if err == nil {
		t.Fatal("expected error for empty BaseURL")
	}
}

// TestProvisionFromLLMConfig_RejectsEmptyModel verifies required field validation.
func TestProvisionFromLLMConfig_RejectsEmptyModel(t *testing.T) {
	_, err := ProvisionFromLLMConfig(engine.LLMChatConfig{
		Provider: "openrouter",
		BaseURL:  "https://openrouter.ai/api/v1",
		Model:    "",
	}, nil, nil)
	if err == nil {
		t.Fatal("expected error for empty Model")
	}
}

// TestProvisionFromLLMConfig_ValidConfigCreatesAdapter verifies happy path provisions an adapter.
func TestProvisionFromLLMConfig_ValidConfigCreatesAdapter(t *testing.T) {
	adapter, err := ProvisionFromLLMConfig(engine.LLMChatConfig{
		Provider: "openrouter",
		BaseURL:  "https://openrouter.ai/api/v1",
		Model:    "openrouter/aurora-alpha",
		APIKey:   "test-key",
	}, nil, nil)
	if err != nil {
		t.Fatalf("ProvisionFromLLMConfig: %v", err)
	}
	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}
}
