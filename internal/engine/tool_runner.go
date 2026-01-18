package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jkatigb/agentctl/internal/hooks"
)

// ToolExecutor executes a tool and returns the result.
// This interface abstracts the underlying tool implementation (dspy-go, MCP, etc.).
type ToolExecutor interface {
	// Execute runs a tool with the given arguments.
	Execute(ctx context.Context, name string, args json.RawMessage) (string, error)

	// List returns all available tool definitions.
	List() []ToolDef
}

// ToolRunner executes tools with hook integration.
//
// CAS offload for large results is NOT handled here - it should be
// implemented at the skill level or by the caller if needed.
type ToolRunner struct {
	// executor executes the actual tool.
	executor ToolExecutor

	// dispatcher dispatches hooks.
	dispatcher hooks.Dispatcher

	// config holds runner configuration.
	config ToolRunnerConfig
}

// ToolRunnerConfig configures the tool runner.
type ToolRunnerConfig struct {
	// Workspace is the workspace root for hook context.
	Workspace string

	// WorkspaceID is the stable workspace ID for hook context.
	WorkspaceID string

	// SessionID is the session identifier for hook context.
	SessionID string

	// ActorID is the actor identifier for hook context.
	ActorID string
}

// DefaultToolRunnerConfig returns sensible defaults.
func DefaultToolRunnerConfig() ToolRunnerConfig {
	return ToolRunnerConfig{}
}

// NewToolRunner creates a new tool runner.
func NewToolRunner(executor ToolExecutor, dispatcher hooks.Dispatcher, cfg ToolRunnerConfig) *ToolRunner {
	return &ToolRunner{
		executor:   executor,
		dispatcher: dispatcher,
		config:     cfg,
	}
}

// SetSessionID updates the session ID for hook context.
// This is useful when the session ID is not known at construction time.
func (r *ToolRunner) SetSessionID(sessionID string) {
	r.config.SessionID = sessionID
}

// Execute runs a tool with hook integration.
//
// Large results are returned as-is. CAS offload should be handled
// by the caller or at the skill level if needed.
func (r *ToolRunner) Execute(ctx context.Context, call ToolCall) (ToolResult, error) {
	start := time.Now()

	// 1. Dispatch PreToolUse hook
	preOutput, err := r.dispatchPreToolUse(ctx, call)
	if err != nil {
		return ToolResult{
			ToolCallID: call.ID,
			Content:    fmt.Sprintf("Hook error: %v", err),
			IsError:    true,
		}, nil
	}

	// Check if blocked
	if preOutput.Decision.IsBlocking() {
		return ToolResult{
			ToolCallID: call.ID,
			Content:    fmt.Sprintf("Blocked by hook: %s", preOutput.Reason),
			IsError:    true,
		}, nil
	}

	// Use updated tool input if provided
	args := call.Arguments
	if len(preOutput.UpdatedToolInput) > 0 {
		args = preOutput.UpdatedToolInput
	}

	// 2. Execute tool
	result, execErr := r.executor.Execute(ctx, call.Name, args)

	duration := time.Since(start)
	durationMS := duration.Milliseconds()

	// 3. Process result
	var toolResult ToolResult
	if execErr != nil {
		toolResult = ToolResult{
			ToolCallID: call.ID,
			Content:    execErr.Error(),
			IsError:    true,
		}
	} else {
		toolResult = ToolResult{
			ToolCallID: call.ID,
			Content:    result,
			IsError:    false,
		}
	}

	// 4. Dispatch PostToolUse hook
	_ = r.dispatchPostToolUse(ctx, call, toolResult, durationMS)

	return toolResult, nil
}

// dispatchPreToolUse dispatches the PreToolUse hook.
func (r *ToolRunner) dispatchPreToolUse(ctx context.Context, call ToolCall) (hooks.Output, error) {
	if r.dispatcher == nil {
		return hooks.NewApprove("no dispatcher", nil), nil
	}

	input := hooks.Input{
		Event:         hooks.EventPreToolUse,
		ToolName:      call.Name,
		ToolCanonical: call.Name,
		ToolKind:      hooks.ClassifyToolKind(call.Name, call.Name),
		ToolInput:     call.Arguments,
		SessionID:     r.config.SessionID,
		ActorID:       r.config.ActorID,
		WorkspaceID:   r.config.WorkspaceID,
		WorkspaceRoot: r.config.Workspace,
	}

	result, err := r.dispatcher.Dispatch(ctx, input)
	return result.Output, err
}

// dispatchPostToolUse dispatches the PostToolUse hook.
func (r *ToolRunner) dispatchPostToolUse(ctx context.Context, call ToolCall, result ToolResult, durationMS int64) hooks.Output {
	if r.dispatcher == nil {
		return hooks.NewNone()
	}

	// Prepare observation (what goes back to LLM)
	observation, _ := json.Marshal(map[string]any{
		"content":    result.Content,
		"is_error":   result.IsError,
		"cas_digest": result.CASDigest,
	})

	input := hooks.Input{
		Event:           hooks.EventPostToolUse,
		ToolName:        call.Name,
		ToolCanonical:   call.Name,
		ToolKind:        hooks.ClassifyToolKind(call.Name, call.Name),
		ToolInput:       call.Arguments,
		ToolObservation: observation,
		ToolDurationMS:  durationMS,
		SessionID:       r.config.SessionID,
		ActorID:         r.config.ActorID,
		WorkspaceID:     r.config.WorkspaceID,
		WorkspaceRoot:   r.config.Workspace,
	}

	if result.IsError {
		input.ToolError = result.Content
	}

	hookResult, _ := r.dispatcher.Dispatch(ctx, input)
	return hookResult.Output
}

// List returns all available tool definitions.
func (r *ToolRunner) List() []ToolDef {
	if r.executor == nil {
		return nil
	}
	return r.executor.List()
}

// ToolRunnerOption configures a ToolRunner.
type ToolRunnerOption func(*ToolRunnerConfig)

// WithToolRunnerWorkspace sets the workspace.
func WithToolRunnerWorkspace(workspace string) ToolRunnerOption {
	return func(c *ToolRunnerConfig) {
		c.Workspace = workspace
	}
}

// WithToolRunnerSession sets the session ID.
func WithToolRunnerSession(sessionID string) ToolRunnerOption {
	return func(c *ToolRunnerConfig) {
		c.SessionID = sessionID
	}
}

// WithToolRunnerActor sets the actor ID.
func WithToolRunnerActor(actorID string) ToolRunnerOption {
	return func(c *ToolRunnerConfig) {
		c.ActorID = actorID
	}
}

// DspyToolExecutorAdapter wraps a dspy-go tool registry as a ToolExecutor.
type DspyToolExecutorAdapter struct {
	// executeFunc executes a tool by name. This is typically
	// obtained from the dspy-go agent's tool execution.
	executeFunc func(ctx context.Context, name string, args json.RawMessage) (string, error)

	// tools are the available tool definitions.
	tools []ToolDef
}

// NewDspyToolExecutorAdapter creates an adapter for dspy-go tools.
func NewDspyToolExecutorAdapter(
	executeFunc func(ctx context.Context, name string, args json.RawMessage) (string, error),
	tools []ToolDef,
) *DspyToolExecutorAdapter {
	return &DspyToolExecutorAdapter{
		executeFunc: executeFunc,
		tools:       tools,
	}
}

// Execute implements ToolExecutor.
func (a *DspyToolExecutorAdapter) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if a.executeFunc == nil {
		return "", fmt.Errorf("no execute function configured")
	}
	return a.executeFunc(ctx, name, args)
}

// List implements ToolExecutor.
func (a *DspyToolExecutorAdapter) List() []ToolDef {
	return a.tools
}

// MockToolExecutor is a test double for ToolExecutor.
type MockToolExecutor struct {
	ExecuteFn func(ctx context.Context, name string, args json.RawMessage) (string, error)
	Tools     []ToolDef
}

// Execute implements ToolExecutor.
func (m *MockToolExecutor) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if m.ExecuteFn != nil {
		return m.ExecuteFn(ctx, name, args)
	}
	return `{"result": "mock"}`, nil
}

// List implements ToolExecutor.
func (m *MockToolExecutor) List() []ToolDef {
	return m.Tools
}
