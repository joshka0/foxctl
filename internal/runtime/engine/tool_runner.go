package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/joshka0/foxctl/internal/runtime/hooks"
)

// ToolExecutor executes a tool and returns the result.
// This interface abstracts the underlying tool implementation (MCP, CLI adapters, etc.).
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

	// NormalizeToolName optionally normalizes raw tool names before hook dispatch
	// and execution. Return false to mark the tool as unknown.
	NormalizeToolName func(rawName string) (canonicalName string, ok bool)

	// ActionExecutor processes hook output actions. Optional - actions are
	// skipped if nil.
	ActionExecutor hooks.ActionExecutor
}

// DefaultToolRunnerConfig returns sensible defaults.
func DefaultToolRunnerConfig() ToolRunnerConfig {
	return ToolRunnerConfig{}
}

// NewToolRunner creates a new tool runner.
//
// Index:
//   Purpose: Initialize a tool runner with hooks and configuration
//   Keywords: tool_runner, hooks, dispatcher, executor, config
//   Related: ToolRunner.Execute, ToolExecutor
//   Flow: store executor/dispatcher/config → return runner
//   Resources: none
//   Events: none
//   OutputFields: ToolRunner
//
// [[domain:tool-runner-lifecycle]]
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
//
// Index:
//   Purpose: Execute a tool call with pre/post hook processing
//   Keywords: tool_execute, hooks, tool_call, tool_result, post_tool_use
//   Related: ToolRunner.dispatchPreToolUse, ToolRunner.dispatchPostToolUse
//   Flow: dispatch pre-hook → execute tool → build result → dispatch post-hook → apply actions
//   Resources: ToolExecutor, hooks.Dispatcher
//   Events: hook.pre_tool_use_error, hook.post_tool_dispatch_failed
//   OutputFields: ToolResult
//
// [[protocol:tool-execution-with-hooks]]
// [[invariant:pre-tool-block-is-respected]]
func (r *ToolRunner) Execute(ctx context.Context, call ToolCall) (ToolResult, error) {
	start := time.Now()

	if r.config.NormalizeToolName != nil {
		normalizedName, ok := r.config.NormalizeToolName(call.Name)
		if !ok {
			return ToolResult{
				ToolCallID: call.ID,
				Content:    fmt.Sprintf("unknown tool: %s", call.Name),
				IsError:    true,
			}, nil
		}
		call.Name = normalizedName
	}

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
	postOutput := r.dispatchPostToolUse(ctx, call, toolResult, durationMS)

	// 5. Process hook actions
	if r.config.ActionExecutor != nil && len(postOutput.Actions) > 0 {
		hookInput := hooks.Input{
			Event:         hooks.EventPostToolUse,
			ToolName:      call.Name,
			SessionID:     r.config.SessionID,
			ActorID:       r.config.ActorID,
			WorkspaceID:   r.config.WorkspaceID,
			WorkspaceRoot: r.config.Workspace,
		}

		injectedCtx, _ := r.config.ActionExecutor.Execute(ctx, postOutput.Actions, hookInput)

		// Append injected context to tool result if present
		if injectedCtx != "" {
			toolResult.Content = toolResult.Content + "\n\n---\n" + injectedCtx
		}
	}

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

// WithActionExecutor sets the action executor for processing hook actions.
func WithActionExecutor(executor hooks.ActionExecutor) ToolRunnerOption {
	return func(c *ToolRunnerConfig) {
		c.ActionExecutor = executor
	}
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
