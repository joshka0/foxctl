package auth

import (
	"context"

	"github.com/casbin/casbin/v2"
	"github.com/jkatigb/agentctl/internal/runtime/hooks"
)

// PolicyHookRunner evaluates Casbin policies for hook events.
type PolicyHookRunner struct {
	enforcer *casbin.Enforcer
}

// NewPolicyHookRunner builds a policy hook runner for the provided enforcer.
func NewPolicyHookRunner(enforcer *casbin.Enforcer) *PolicyHookRunner {
	return &PolicyHookRunner{enforcer: enforcer}
}

// Evaluate authorizes the incoming hook input against Casbin policy.
func (r *PolicyHookRunner) Evaluate(ctx context.Context, input hooks.Input) (hooks.Decision, error) {
	select {
	case <-ctx.Done():
		return hooks.DecisionBlock, ctx.Err()
	default:
	}

	if r == nil || r.enforcer == nil {
		return hooks.DecisionApprove, nil
	}

	toolName := input.ToolName
	if toolName == "" {
		toolName = input.ToolCanonical
	}

	resource := "tool:" + toolName
	allowed, err := Enforce(r.enforcer, input.Principal, resource, "execute")
	if err != nil {
		return hooks.DecisionBlock, err
	}
	if !allowed {
		return hooks.DecisionBlock, nil
	}

	return hooks.DecisionApprove, nil
}
