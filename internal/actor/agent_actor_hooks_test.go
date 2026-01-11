package actor

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jkatigb/agentctl/internal/hooks"
)

// MockHookDispatcher is a test double for hooks.Dispatcher.
type MockHookDispatcher struct {
	DispatchFn func(ctx context.Context, input hooks.Input) (hooks.Result, error)
	Calls      []hooks.Input
}

func (m *MockHookDispatcher) Dispatch(ctx context.Context, input hooks.Input) (hooks.Result, error) {
	m.Calls = append(m.Calls, input)
	if m.DispatchFn != nil {
		return m.DispatchFn(ctx, input)
	}
	return hooks.Result{Output: hooks.NewApprove("mock", nil)}, nil
}

func (m *MockHookDispatcher) DispatchAsync(ctx context.Context, input hooks.Input) <-chan hooks.Result {
	ch := make(chan hooks.Result, 1)
	result, _ := m.Dispatch(ctx, input)
	ch <- result
	close(ch)
	return ch
}

func TestBuildHookInput(t *testing.T) {
	actor := &AgentActor{
		BaseActor:     NewBaseActor(Config{ID: "test-actor", Namespace: "test-ns"}),
		sessionID:     "session-123",
		workspaceRoot: "/workspace",
	}

	t.Run("basic input", func(t *testing.T) {
		input := actor.buildHookInput(hooks.EventSessionStart, nil)

		if input.Event != hooks.EventSessionStart {
			t.Errorf("expected EventSessionStart, got %s", input.Event)
		}
		if input.ActorID != "test-actor" {
			t.Errorf("expected test-actor, got %s", input.ActorID)
		}
		if input.SessionID != "session-123" {
			t.Errorf("expected session-123, got %s", input.SessionID)
		}
		if input.WorkspaceRoot != "/workspace" {
			t.Errorf("expected /workspace, got %s", input.WorkspaceRoot)
		}
	})

	t.Run("with opts", func(t *testing.T) {
		input := actor.buildHookInput(hooks.EventLLMRequest, map[string]any{
			"turn_id":        "turn-456",
			"correlation_id": "corr-789",
			"prompt":         "test prompt",
		})

		if input.TurnID != "turn-456" {
			t.Errorf("expected turn-456, got %s", input.TurnID)
		}
		if input.CorrelationID != "corr-789" {
			t.Errorf("expected corr-789, got %s", input.CorrelationID)
		}
		if input.Prompt != "test prompt" {
			t.Errorf("expected 'test prompt', got %s", input.Prompt)
		}
	})

	t.Run("with tool context", func(t *testing.T) {
		toolInput := json.RawMessage(`{"path": "/file.txt"}`)
		input := actor.buildHookInput(hooks.EventPreToolUse, map[string]any{
			"tool_name":  "fs.read_file",
			"tool_input": toolInput,
		})

		if input.ToolName != "fs.read_file" {
			t.Errorf("expected fs.read_file, got %s", input.ToolName)
		}
		if string(input.ToolInput) != `{"path": "/file.txt"}` {
			t.Errorf("expected tool input, got %s", string(input.ToolInput))
		}
	})

	t.Run("with mailbox message", func(t *testing.T) {
		mbMsg := &hooks.MailboxMessage{
			ID:     "msg-1",
			FromNS: "sender",
			ToNS:   "receiver",
			Type:   "agent.ask",
		}
		input := actor.buildHookInput(hooks.EventMessageReceived, map[string]any{
			"mailbox_message": mbMsg,
		})

		if input.MailboxMessage == nil {
			t.Fatal("expected mailbox message")
		}
		if input.MailboxMessage.ID != "msg-1" {
			t.Errorf("expected msg-1, got %s", input.MailboxMessage.ID)
		}
	})
}

func TestDispatchHook_NoDispatcher(t *testing.T) {
	actor := &AgentActor{
		BaseActor: NewBaseActor(Config{ID: "test-actor", Namespace: "test-ns"}),
	}

	result := actor.dispatchHook(context.Background(), hooks.EventSessionStart, nil)

	if result.Output.Decision != hooks.DecisionApprove {
		t.Errorf("expected approve, got %s", result.Output.Decision)
	}
	if result.Output.Reason != "no dispatcher" {
		t.Errorf("expected 'no dispatcher', got %s", result.Output.Reason)
	}
}

func TestDispatchHook_WithDispatcher(t *testing.T) {
	mock := &MockHookDispatcher{}
	actor := &AgentActor{
		BaseActor: NewBaseActor(Config{ID: "test-actor", Namespace: "test-ns"}),
		hooks:     mock,
		sessionID: "session-123",
	}

	result := actor.dispatchHook(context.Background(), hooks.EventSessionStart, nil)

	if result.Output.Decision != hooks.DecisionApprove {
		t.Errorf("expected approve, got %s", result.Output.Decision)
	}

	// Verify dispatch was called
	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mock.Calls))
	}
	if mock.Calls[0].Event != hooks.EventSessionStart {
		t.Errorf("expected EventSessionStart, got %s", mock.Calls[0].Event)
	}
}

func TestDispatchHook_Blocked(t *testing.T) {
	mock := &MockHookDispatcher{
		DispatchFn: func(ctx context.Context, input hooks.Input) (hooks.Result, error) {
			return hooks.Result{
				Output:    hooks.NewBlock("blocked by test"),
				Blocked:   true,
				BlockedBy: "test-hook",
			}, nil
		},
	}
	actor := &AgentActor{
		BaseActor: NewBaseActor(Config{ID: "test-actor", Namespace: "test-ns"}),
		hooks:     mock,
	}

	result := actor.dispatchHook(context.Background(), hooks.EventLLMRequest, nil)

	if !result.Blocked {
		t.Error("expected blocked=true")
	}
	if result.BlockedBy != "test-hook" {
		t.Errorf("expected blocked by test-hook, got %s", result.BlockedBy)
	}
	if result.Output.Reason != "blocked by test" {
		t.Errorf("expected 'blocked by test', got %s", result.Output.Reason)
	}
}

func TestDispatchHook_WithUpdatedAssistantText(t *testing.T) {
	mock := &MockHookDispatcher{
		DispatchFn: func(ctx context.Context, input hooks.Input) (hooks.Result, error) {
			output := hooks.NewApprove("modified", nil)
			output.UpdatedAssistantText = "modified response"
			return hooks.Result{Output: output}, nil
		},
	}
	actor := &AgentActor{
		BaseActor: NewBaseActor(Config{ID: "test-actor", Namespace: "test-ns"}),
		hooks:     mock,
	}

	result := actor.dispatchHook(context.Background(), hooks.EventPostAgentTurn, map[string]any{
		"assistant_text": "original response",
	})

	if result.Output.UpdatedAssistantText != "modified response" {
		t.Errorf("expected 'modified response', got %s", result.Output.UpdatedAssistantText)
	}
}

func TestDispatchHook_EventSequence(t *testing.T) {
	mock := &MockHookDispatcher{}
	actor := &AgentActor{
		BaseActor:     NewBaseActor(Config{ID: "test-actor", Namespace: "test-ns"}),
		hooks:         mock,
		sessionID:     "session-123",
		workspaceRoot: "/workspace",
	}

	// Simulate a typical request flow
	events := []hooks.Event{
		hooks.EventSessionStart,
		hooks.EventMessageReceived,
		hooks.EventLLMRequest,
		hooks.EventLLMResponse,
		hooks.EventPostAgentTurn,
		hooks.EventSessionEnd,
	}

	for _, event := range events {
		actor.dispatchHook(context.Background(), event, nil)
	}

	if len(mock.Calls) != len(events) {
		t.Fatalf("expected %d calls, got %d", len(events), len(mock.Calls))
	}

	for i, event := range events {
		if mock.Calls[i].Event != event {
			t.Errorf("call %d: expected %s, got %s", i, event, mock.Calls[i].Event)
		}
	}
}

func TestAgentActorConfig_HooksField(t *testing.T) {
	mock := &MockHookDispatcher{}
	cfg := AgentActorConfig{
		Hooks: mock,
	}

	if cfg.Hooks == nil {
		t.Error("expected Hooks field to be set")
	}
}
