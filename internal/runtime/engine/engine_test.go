package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/runtime/hooks"
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

// --- ToolRunner Tests ---

func TestToolRunner_Execute_Basic(t *testing.T) {
	executor := &MockToolExecutor{
		ExecuteFn: func(ctx context.Context, name string, args json.RawMessage) (string, error) {
			return `{"result": "success"}`, nil
		},
	}
	dispatcher := &MockHookDispatcher{}

	runner := NewToolRunner(executor, dispatcher, DefaultToolRunnerConfig())

	call := ToolCall{
		ID:        "call-1",
		Name:      "test.tool",
		Arguments: json.RawMessage(`{"arg": "value"}`),
	}

	result, err := runner.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ToolCallID != "call-1" {
		t.Errorf("expected call-1, got %s", result.ToolCallID)
	}
	if result.IsError {
		t.Error("expected success, got error")
	}
	if result.Content != `{"result": "success"}` {
		t.Errorf("unexpected content: %s", result.Content)
	}

	// Verify hooks were dispatched
	if len(dispatcher.Calls) != 2 {
		t.Errorf("expected 2 hook calls (pre+post), got %d", len(dispatcher.Calls))
	}
	if dispatcher.Calls[0].Event != hooks.EventPreToolUse {
		t.Errorf("expected PreToolUse, got %s", dispatcher.Calls[0].Event)
	}
	if dispatcher.Calls[1].Event != hooks.EventPostToolUse {
		t.Errorf("expected PostToolUse, got %s", dispatcher.Calls[1].Event)
	}
}

func TestToolRunner_Execute_HookBlock(t *testing.T) {
	executor := &MockToolExecutor{
		ExecuteFn: func(ctx context.Context, name string, args json.RawMessage) (string, error) {
			t.Fatal("executor should not be called when hook blocks")
			return "", nil
		},
	}
	dispatcher := &MockHookDispatcher{
		DispatchFn: func(ctx context.Context, input hooks.Input) (hooks.Result, error) {
			return hooks.Result{Output: hooks.NewBlock("security policy")}, nil
		},
	}

	runner := NewToolRunner(executor, dispatcher, DefaultToolRunnerConfig())

	call := ToolCall{
		ID:        "call-1",
		Name:      "dangerous.tool",
		Arguments: json.RawMessage(`{}`),
	}

	result, err := runner.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error result when blocked")
	}
	if !strings.Contains(result.Content, "Blocked by hook") {
		t.Errorf("expected blocked message, got: %s", result.Content)
	}
}

func TestToolRunner_Execute_HookUpdatesInput(t *testing.T) {
	var receivedArgs json.RawMessage
	executor := &MockToolExecutor{
		ExecuteFn: func(ctx context.Context, name string, args json.RawMessage) (string, error) {
			receivedArgs = args
			return "ok", nil
		},
	}
	dispatcher := &MockHookDispatcher{
		DispatchFn: func(ctx context.Context, input hooks.Input) (hooks.Result, error) {
			if input.Event == hooks.EventPreToolUse {
				out := hooks.NewApprove("modified", nil)
				out.UpdatedToolInput = json.RawMessage(`{"modified": true}`)
				return hooks.Result{Output: out}, nil
			}
			return hooks.Result{Output: hooks.NewNone()}, nil
		},
	}

	runner := NewToolRunner(executor, dispatcher, DefaultToolRunnerConfig())

	call := ToolCall{
		ID:        "call-1",
		Name:      "test.tool",
		Arguments: json.RawMessage(`{"original": true}`),
	}

	_, err := runner.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(receivedArgs) != `{"modified": true}` {
		t.Errorf("expected modified args, got: %s", string(receivedArgs))
	}
}

func TestToolRunner_Execute_ExecutorError(t *testing.T) {
	executor := &MockToolExecutor{
		ExecuteFn: func(ctx context.Context, name string, args json.RawMessage) (string, error) {
			return "", errors.New("tool execution failed")
		},
	}

	runner := NewToolRunner(executor, nil, DefaultToolRunnerConfig())

	call := ToolCall{
		ID:        "call-1",
		Name:      "failing.tool",
		Arguments: json.RawMessage(`{}`),
	}

	result, err := runner.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error result")
	}
	if result.Content != "tool execution failed" {
		t.Errorf("expected error message, got: %s", result.Content)
	}
}

func TestToolRunner_Execute_NoDispatcher(t *testing.T) {
	executor := &MockToolExecutor{
		ExecuteFn: func(ctx context.Context, name string, args json.RawMessage) (string, error) {
			return "success", nil
		},
	}

	// No dispatcher - should still work
	runner := NewToolRunner(executor, nil, DefaultToolRunnerConfig())

	call := ToolCall{
		ID:        "call-1",
		Name:      "test.tool",
		Arguments: json.RawMessage(`{}`),
	}

	result, err := runner.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Content != "success" {
		t.Errorf("expected success, got: %s", result.Content)
	}
}

func TestToolRunner_List(t *testing.T) {
	tools := []ToolDef{
		{Name: "tool1", Description: "Tool 1"},
		{Name: "tool2", Description: "Tool 2"},
	}

	executor := &MockToolExecutor{Tools: tools}
	runner := NewToolRunner(executor, nil, DefaultToolRunnerConfig())

	result := runner.List()
	if len(result) != 2 {
		t.Errorf("expected 2 tools, got %d", len(result))
	}
}

func TestToolRunner_List_NilExecutor(t *testing.T) {
	runner := &ToolRunner{}
	result := runner.List()
	if result != nil {
		t.Errorf("expected nil for nil executor, got %v", result)
	}
}

// --- Engine Types Tests ---

func TestMessage_Factories(t *testing.T) {
	user := NewUserMessage("hello")
	if user.Role != RoleUser || user.Content != "hello" {
		t.Errorf("unexpected user message: %+v", user)
	}

	assistant := NewAssistantMessage("hi there")
	if assistant.Role != RoleAssistant || assistant.Content != "hi there" {
		t.Errorf("unexpected assistant message: %+v", assistant)
	}

	system := NewSystemMessage("you are helpful")
	if system.Role != RoleSystem || system.Content != "you are helpful" {
		t.Errorf("unexpected system message: %+v", system)
	}

	toolCalls := []ToolCall{{ID: "tc-1", Name: "test"}}
	assistantTC := NewAssistantToolCallMessage(toolCalls)
	if assistantTC.Role != RoleAssistant || len(assistantTC.ToolCalls) != 1 {
		t.Errorf("unexpected assistant tool call message: %+v", assistantTC)
	}

	toolResult := NewToolResultMessage("tc-1", "test", "result", false)
	if toolResult.Role != RoleTool || toolResult.ToolCallID != "tc-1" {
		t.Errorf("unexpected tool result message: %+v", toolResult)
	}
}

func TestTokenUsage_Add(t *testing.T) {
	usage := TokenUsage{}
	usage.Add(100, 50)

	if usage.InputTokens != 100 {
		t.Errorf("expected 100 input tokens, got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 50 {
		t.Errorf("expected 50 output tokens, got %d", usage.OutputTokens)
	}
	if usage.TotalTokens != 150 {
		t.Errorf("expected 150 total tokens, got %d", usage.TotalTokens)
	}

	usage.Add(50, 25)
	if usage.TotalTokens != 225 {
		t.Errorf("expected 225 total after second add, got %d", usage.TotalTokens)
	}
}

func TestEngineConfig_Defaults(t *testing.T) {
	cfg := DefaultEngineConfig()

	if cfg.MaxIterations != 50 {
		t.Errorf("expected 50 max iterations, got %d", cfg.MaxIterations)
	}
	if cfg.MaxResultBytes != 100*1024 {
		t.Errorf("expected 100KB max result bytes, got %d", cfg.MaxResultBytes)
	}
	if cfg.Temperature != 0.0 {
		t.Errorf("expected 0.0 temperature, got %f", cfg.Temperature)
	}
	if cfg.MaxTokens != 8192 {
		t.Errorf("expected 8192 max tokens, got %d", cfg.MaxTokens)
	}
}

func TestEngineConfig_Options(t *testing.T) {
	cfg := DefaultEngineConfig()

	WithMaxIterations(100)(&cfg)
	if cfg.MaxIterations != 100 {
		t.Errorf("expected 100, got %d", cfg.MaxIterations)
	}

	WithMaxResultBytes(50 * 1024)(&cfg)
	if cfg.MaxResultBytes != 50*1024 {
		t.Errorf("expected 50KB, got %d", cfg.MaxResultBytes)
	}

	WithTemperature(0.7)(&cfg)
	if cfg.Temperature != 0.7 {
		t.Errorf("expected 0.7, got %f", cfg.Temperature)
	}

	WithMaxTokens(4096)(&cfg)
	if cfg.MaxTokens != 4096 {
		t.Errorf("expected 4096, got %d", cfg.MaxTokens)
	}
}

// --- ToolRunnerConfig Tests ---

func TestToolRunnerConfig_Options(t *testing.T) {
	cfg := DefaultToolRunnerConfig()

	WithToolRunnerWorkspace("/path/to/workspace")(&cfg)
	if cfg.Workspace != "/path/to/workspace" {
		t.Errorf("expected /path/to/workspace, got %s", cfg.Workspace)
	}

	WithToolRunnerSession("session-123")(&cfg)
	if cfg.SessionID != "session-123" {
		t.Errorf("expected session-123, got %s", cfg.SessionID)
	}

	WithToolRunnerActor("actor-456")(&cfg)
	if cfg.ActorID != "actor-456" {
		t.Errorf("expected actor-456, got %s", cfg.ActorID)
	}
}

// --- Hook Dispatch Context Tests ---

func TestToolRunner_Execute_HookError(t *testing.T) {
	executor := &MockToolExecutor{
		ExecuteFn: func(ctx context.Context, name string, args json.RawMessage) (string, error) {
			t.Fatal("executor should not be called when hook errors")
			return "", nil
		},
	}
	dispatcher := &MockHookDispatcher{
		DispatchFn: func(ctx context.Context, input hooks.Input) (hooks.Result, error) {
			return hooks.Result{}, errors.New("hook failed")
		},
	}

	runner := NewToolRunner(executor, dispatcher, DefaultToolRunnerConfig())

	call := ToolCall{
		ID:        "call-1",
		Name:      "test.tool",
		Arguments: json.RawMessage(`{}`),
	}

	result, err := runner.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("unexpected error from Execute: %v", err)
	}

	if !result.IsError {
		t.Error("expected error result when hook fails")
	}
	if !strings.Contains(result.Content, "Hook error") {
		t.Errorf("expected hook error message, got: %s", result.Content)
	}
}

func TestToolRunner_Execute_SessionContext(t *testing.T) {
	var receivedInput hooks.Input
	dispatcher := &MockHookDispatcher{
		DispatchFn: func(ctx context.Context, input hooks.Input) (hooks.Result, error) {
			if input.Event == hooks.EventPreToolUse {
				receivedInput = input
			}
			return hooks.Result{Output: hooks.NewApprove("ok", nil)}, nil
		},
	}

	executor := &MockToolExecutor{
		ExecuteFn: func(ctx context.Context, name string, args json.RawMessage) (string, error) {
			return "ok", nil
		},
	}

	cfg := DefaultToolRunnerConfig()
	cfg.SessionID = "session-abc"
	cfg.ActorID = "actor-xyz"
	cfg.Workspace = "/workspace"

	runner := NewToolRunner(executor, dispatcher, cfg)

	call := ToolCall{
		ID:        "call-1",
		Name:      "test.tool",
		Arguments: json.RawMessage(`{"key": "value"}`),
	}

	_, err := runner.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedInput.SessionID != "session-abc" {
		t.Errorf("expected session-abc, got %s", receivedInput.SessionID)
	}
	if receivedInput.ActorID != "actor-xyz" {
		t.Errorf("expected actor-xyz, got %s", receivedInput.ActorID)
	}
	if receivedInput.WorkspaceRoot != "/workspace" {
		t.Errorf("expected /workspace, got %s", receivedInput.WorkspaceRoot)
	}
	if receivedInput.ToolName != "test.tool" {
		t.Errorf("expected test.tool, got %s", receivedInput.ToolName)
	}
}

// --- MockActionExecutor for testing ---

type MockActionExecutor struct {
	ExecuteFn func(ctx context.Context, actions []hooks.Action, input hooks.Input) (string, error)
	Calls     []struct {
		Actions []hooks.Action
		Input   hooks.Input
	}
}

func (m *MockActionExecutor) Execute(ctx context.Context, actions []hooks.Action, input hooks.Input) (string, error) {
	m.Calls = append(m.Calls, struct {
		Actions []hooks.Action
		Input   hooks.Input
	}{Actions: actions, Input: input})

	if m.ExecuteFn != nil {
		return m.ExecuteFn(ctx, actions, input)
	}
	return "", nil
}

func TestToolRunner_Execute_ActionExecutor(t *testing.T) {
	executor := &MockToolExecutor{
		ExecuteFn: func(ctx context.Context, name string, args json.RawMessage) (string, error) {
			return "tool result", nil
		},
	}

	// Dispatcher returns actions in PostToolUse
	dispatcher := &MockHookDispatcher{
		DispatchFn: func(ctx context.Context, input hooks.Input) (hooks.Result, error) {
			if input.Event == hooks.EventPostToolUse {
				out := hooks.NewNone()
				out.Actions = []hooks.Action{
					{Type: hooks.ActionInjectContext, Text: "Injected by hook"},
					{Type: hooks.ActionRunSkill, Skill: "memory/store"},
				}
				return hooks.Result{Output: out}, nil
			}
			return hooks.Result{Output: hooks.NewApprove("ok", nil)}, nil
		},
	}

	actionExec := &MockActionExecutor{
		ExecuteFn: func(ctx context.Context, actions []hooks.Action, input hooks.Input) (string, error) {
			// Return injected context
			for _, a := range actions {
				if a.Type == hooks.ActionInjectContext {
					return a.Text, nil
				}
			}
			return "", nil
		},
	}

	cfg := DefaultToolRunnerConfig()
	cfg.ActionExecutor = actionExec
	cfg.SessionID = "session-123"
	cfg.ActorID = "actor:test"

	runner := NewToolRunner(executor, dispatcher, cfg)

	call := ToolCall{
		ID:        "call-1",
		Name:      "test.tool",
		Arguments: json.RawMessage(`{}`),
	}

	result, err := runner.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify action executor was called
	if len(actionExec.Calls) != 1 {
		t.Fatalf("expected 1 action executor call, got %d", len(actionExec.Calls))
	}

	// Verify actions were passed
	if len(actionExec.Calls[0].Actions) != 2 {
		t.Errorf("expected 2 actions, got %d", len(actionExec.Calls[0].Actions))
	}

	// Verify context was injected into result
	if !strings.Contains(result.Content, "tool result") {
		t.Errorf("expected original result, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Injected by hook") {
		t.Errorf("expected injected context, got: %s", result.Content)
	}
}

func TestToolRunner_Execute_ActionExecutor_NoActions(t *testing.T) {
	executor := &MockToolExecutor{
		ExecuteFn: func(ctx context.Context, name string, args json.RawMessage) (string, error) {
			return "tool result", nil
		},
	}

	// Dispatcher returns no actions
	dispatcher := &MockHookDispatcher{}

	actionExec := &MockActionExecutor{}

	cfg := DefaultToolRunnerConfig()
	cfg.ActionExecutor = actionExec

	runner := NewToolRunner(executor, dispatcher, cfg)

	call := ToolCall{
		ID:        "call-1",
		Name:      "test.tool",
		Arguments: json.RawMessage(`{}`),
	}

	result, err := runner.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Action executor should not be called when no actions
	if len(actionExec.Calls) != 0 {
		t.Errorf("expected 0 action executor calls, got %d", len(actionExec.Calls))
	}

	// Result should be unchanged
	if result.Content != "tool result" {
		t.Errorf("expected 'tool result', got: %s", result.Content)
	}
}
