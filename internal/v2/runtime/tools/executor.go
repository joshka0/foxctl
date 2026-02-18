package tools

import (
	"context"
	"encoding/json"
	"strings"

	v2errors "github.com/jkatigb/agentctl/internal/v2/core/errors"
	coretool "github.com/jkatigb/agentctl/internal/v2/core/tool"
	"github.com/jkatigb/agentctl/internal/v2/runtime/runner"
)

// DelegateExecutor executes resolved, validated tool calls.
type DelegateExecutor interface {
	Execute(ctx context.Context, name string, args json.RawMessage) (runner.ToolResult, error)
}

// Executor is the single v2 runtime tool execution path.
type Executor struct {
	catalog  *Catalog
	profile  coretool.ProcessProfile
	delegate DelegateExecutor
}

// NewExecutor builds a runtime tool executor for one process profile.
func NewExecutor(catalog *Catalog, profile coretool.ProcessProfile, delegate DelegateExecutor) *Executor {
	return &Executor{
		catalog:  catalog,
		profile:  profile,
		delegate: delegate,
	}
}

// Execute validates allowlist and args, then delegates execution.
func (e *Executor) Execute(ctx context.Context, name string, args json.RawMessage) (runner.ToolResult, error) {
	if e == nil || e.catalog == nil {
		return runner.ToolResult{}, &v2errors.V2Error{
			Kind:    v2errors.ErrDependency,
			Message: "tool catalog is not configured",
			Fatal:   true,
		}
	}
	if e.delegate == nil {
		return runner.ToolResult{}, &v2errors.V2Error{
			Kind:    v2errors.ErrDependency,
			Message: "tool delegate is not configured",
			Fatal:   true,
		}
	}

	toolDef, ok := e.catalog.Resolve(name, e.profile)
	if !ok {
		return runner.ToolResult{}, &v2errors.V2Error{
			Kind:    v2errors.ErrPolicyViolation,
			Message: "tool not allowed for profile",
			Fatal:   true,
			Details: map[string]any{
				"tool":    strings.TrimSpace(name),
				"profile": string(e.profile),
			},
		}
	}

	if err := validateArgs(toolDef.schema, args); err != nil {
		return runner.ToolResult{}, &v2errors.V2Error{
			Kind:    v2errors.ErrValidation,
			Message: "invalid tool arguments",
			Cause:   err,
			Fatal:   true,
			Details: map[string]any{
				"tool": toolDef.def.Name,
			},
		}
	}

	result, err := e.delegate.Execute(ctx, toolDef.def.Name, args)
	if err != nil {
		return runner.ToolResult{}, &v2errors.V2Error{
			Kind:      v2errors.ErrToolFailed,
			Message:   "tool execution failed",
			Cause:     err,
			Fatal:     true,
			Retryable: true,
			Details: map[string]any{
				"tool": toolDef.def.Name,
			},
		}
	}
	if strings.TrimSpace(result.Status) == "" {
		result.Status = "ok"
	}
	return result, nil
}

var _ runner.ToolExecutor = (*Executor)(nil)
