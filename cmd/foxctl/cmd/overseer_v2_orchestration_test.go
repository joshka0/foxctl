package cmd

import (
	"testing"

	v2jido "github.com/joshka0/foxctl/internal/v2/adapters/jido"
)

func TestResolveOverseerDispatchParentAgentID_PrefersExplicitEnv(t *testing.T) {
	t.Setenv(v2jido.EnvJidoOrchestrationDispatchParentAgentID, "agent:dispatch-root")
	t.Setenv(v2jido.EnvJidoOrchestrationParentAgentIDs, "agent:overseer-1,agent:overseer-2")

	if got := resolveOverseerDispatchParentAgentID(); got != "agent:dispatch-root" {
		t.Fatalf("dispatch_parent_agent_id=%q want agent:dispatch-root", got)
	}
}

func TestResolveOverseerDispatchParentAgentID_FallsBackToFirstParent(t *testing.T) {
	t.Setenv(v2jido.EnvJidoOrchestrationDispatchParentAgentID, "")
	t.Setenv(v2jido.EnvJidoOrchestrationParentAgentIDs, " agent:overseer-1 ; agent:overseer-2 ")

	if got := resolveOverseerDispatchParentAgentID(); got != "agent:overseer-1" {
		t.Fatalf("dispatch_parent_agent_id=%q want agent:overseer-1", got)
	}
}
