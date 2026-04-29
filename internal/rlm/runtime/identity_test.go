package runtime

import (
	"testing"

	"github.com/joshka0/foxctl/internal/rlm"
)

func TestPlanIdentityDefaults(t *testing.T) {
	t.Parallel()

	identity := PlanIdentity(rlm.Task{})
	if identity.RunID != "run-unknown" {
		t.Fatalf("RunID = %q, want run-unknown", identity.RunID)
	}
	if identity.AgentID != "agent-root" {
		t.Fatalf("AgentID = %q, want agent-root", identity.AgentID)
	}
	if identity.ParentAgentID != "" {
		t.Fatalf("ParentAgentID = %q, want empty", identity.ParentAgentID)
	}
	if identity.OutputNamespace != "runs/run-unknown/agents/agent-root" {
		t.Fatalf("OutputNamespace = %q", identity.OutputNamespace)
	}
}

func TestPlanIdentitySanitizesIDs(t *testing.T) {
	t.Parallel()

	identity := PlanIdentity(rlm.Task{
		RunID:         "Run 01 ./../../",
		AgentID:       "Root_Agent/Child #1",
		ParentAgentID: "Parent Agent",
	})

	if identity.RunID != "run-01" {
		t.Fatalf("RunID = %q, want run-01", identity.RunID)
	}
	if identity.AgentID != "root_agent/child-1" {
		t.Fatalf("AgentID = %q, want root_agent/child-1", identity.AgentID)
	}
	if identity.ParentAgentID != "parent-agent" {
		t.Fatalf("ParentAgentID = %q, want parent-agent", identity.ParentAgentID)
	}
	if identity.OutputNamespace != "runs/run-01/agents/root_agent/child-1" {
		t.Fatalf("OutputNamespace = %q", identity.OutputNamespace)
	}
}

func TestChildIdentityBuildsReadableHierarchy(t *testing.T) {
	t.Parallel()

	root := PlanIdentity(rlm.Task{
		RunID:   "run-main",
		AgentID: "agent-root",
	})
	child := ChildIdentity(root, 1)
	grandchild := ChildIdentity(child, 1)

	if child.AgentID != "agent-root/rlm-0001" {
		t.Fatalf("child AgentID = %q, want agent-root/rlm-0001", child.AgentID)
	}
	if child.ParentAgentID != "agent-root" {
		t.Fatalf("child ParentAgentID = %q, want agent-root", child.ParentAgentID)
	}
	if grandchild.AgentID != "agent-root/rlm-0001/rlm-0001" {
		t.Fatalf("grandchild AgentID = %q", grandchild.AgentID)
	}
	if grandchild.OutputNamespace != "runs/run-main/agents/agent-root/rlm-0001/rlm-0001" {
		t.Fatalf("grandchild OutputNamespace = %q", grandchild.OutputNamespace)
	}
}

func TestOutputRootPathUsesWorkspaceRoot(t *testing.T) {
	t.Parallel()

	if got := OutputRootPath(""); got != "out" {
		t.Fatalf("OutputRootPath(\"\") = %q, want out", got)
	}
	if got := OutputRootPath("/workspace/root"); got != "/workspace/root/out" {
		t.Fatalf("OutputRootPath(workspace) = %q", got)
	}
}

func TestPlanIdentityPreservesExplicitOutputRoot(t *testing.T) {
	t.Parallel()

	identity := PlanIdentity(rlm.Task{
		RunID:         "run-main",
		AgentID:       "agent-root",
		WorkspaceRoot: "/workspace/repo",
		OutputRoot:    "/workspace/out",
	})
	if identity.OutputRoot != "/workspace/out" {
		t.Fatalf("OutputRoot = %q, want /workspace/out", identity.OutputRoot)
	}
	child := ChildIdentity(identity, 1)
	if child.OutputRoot != "/workspace/out" {
		t.Fatalf("child OutputRoot = %q, want /workspace/out", child.OutputRoot)
	}
}
