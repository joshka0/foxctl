package cmd

import (
	"testing"

	v2jido "github.com/joshka0/foxctl/internal/v2/adapters/jido"
)

func TestResolveOverseerDispatchParentAgentID_PrecedenceAndFallback(t *testing.T) {
	t.Run("explicit env precedence", func(t *testing.T) {
		t.Setenv(envCLIDispatchParentAgentID, "agent:cli-dispatch")
		t.Setenv(v2jido.EnvJidoOrchestrationDispatchParentAgentID, "agent:jido-dispatch")
		t.Setenv(envCLIParentAgentIDs, "agent:cli-parent-1,agent:cli-parent-2")
		t.Setenv(v2jido.EnvJidoOrchestrationParentAgentIDs, "agent:jido-parent-1,agent:jido-parent-2")

		if got := resolveOverseerDispatchParentAgentID(); got != "agent:cli-dispatch" {
			t.Fatalf("dispatch_parent_agent_id=%q want agent:cli-dispatch", got)
		}
	})

	t.Run("fallback to first parent id", func(t *testing.T) {
		t.Setenv(envCLIDispatchParentAgentID, "")
		t.Setenv(v2jido.EnvJidoOrchestrationDispatchParentAgentID, "")
		t.Setenv(envCLIParentAgentIDs, " ; agent:cli-parent-1 ; agent:cli-parent-2 ")
		t.Setenv(v2jido.EnvJidoOrchestrationParentAgentIDs, "agent:jido-parent-1,agent:jido-parent-2")

		if got := resolveOverseerDispatchParentAgentID(); got != "agent:cli-parent-1" {
			t.Fatalf("dispatch_parent_agent_id=%q want agent:cli-parent-1", got)
		}
	})

	t.Run("fallback to jido parent list when cli list missing", func(t *testing.T) {
		t.Setenv(envCLIDispatchParentAgentID, "")
		t.Setenv(v2jido.EnvJidoOrchestrationDispatchParentAgentID, "")
		t.Setenv(envCLIParentAgentIDs, "")
		t.Setenv(v2jido.EnvJidoOrchestrationParentAgentIDs, "agent:jido-parent-1,agent:jido-parent-2")

		if got := resolveOverseerDispatchParentAgentID(); got != "agent:jido-parent-1" {
			t.Fatalf("dispatch_parent_agent_id=%q want agent:jido-parent-1", got)
		}
	})
}
