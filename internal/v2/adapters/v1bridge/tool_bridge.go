package v1bridge

import (
	"context"
	"encoding/json"

	v2errors "github.com/jkatigb/agentctl/internal/v2/core/errors"
	"github.com/jkatigb/agentctl/internal/v2/runtime/runner"
)

// LegacyToolExecutor is the minimal v1 tool executor contract needed by the bridge.
type LegacyToolExecutor interface {
	Execute(ctx context.Context, name string, args json.RawMessage) (string, error)
}

// ToolBridge adapts a v1 executor to runner.ToolExecutor.
type ToolBridge struct {
	legacy LegacyToolExecutor
}

// NewToolBridge creates a v1-to-v2 tool bridge.
func NewToolBridge(legacy LegacyToolExecutor) *ToolBridge {
	return &ToolBridge{legacy: legacy}
}

// Execute forwards tool execution to v1 and maps results/errors to v2 semantics.
func (b *ToolBridge) Execute(ctx context.Context, name string, args json.RawMessage) (runner.ToolResult, error) {
	if b == nil || b.legacy == nil {
		return runner.ToolResult{}, &v2errors.V2Error{
			Kind:    v2errors.ErrDependency,
			Message: "legacy tool executor is not configured",
			Fatal:   true,
		}
	}

	result, err := b.legacy.Execute(ctx, name, args)
	if err != nil {
		return runner.ToolResult{}, &v2errors.V2Error{
			Kind:      v2errors.ErrToolFailed,
			Message:   "legacy tool execution failed",
			Cause:     err,
			Fatal:     true,
			Retryable: true,
			Details: map[string]any{
				"tool": name,
			},
		}
	}

	return runner.ToolResult{
		Status: "ok",
		Output: result,
	}, nil
}

var _ runner.ToolExecutor = (*ToolBridge)(nil)
