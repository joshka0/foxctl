package runtime

import (
	"context"
	"testing"

	"github.com/jkatigb/agentctl/internal/agent/types"
)

func TestValidateSpawnDepth(t *testing.T) {
	tests := []struct {
		name           string
		callerDepth    int
		callerMaxDepth int
		callerLocalMax int
		wantErr        bool
	}{
		{
			name:           "overseer can spawn (depth 0, max 3)",
			callerDepth:    0,
			callerMaxDepth: 3,
			callerLocalMax: 3,
			wantErr:        false,
		},
		{
			name:           "agent can spawn (depth 1, max 3)",
			callerDepth:    1,
			callerMaxDepth: 3,
			callerLocalMax: 3,
			wantErr:        false,
		},
		{
			name:           "at global limit (depth 3, max 3)",
			callerDepth:    3,
			callerMaxDepth: 3,
			callerLocalMax: 3,
			wantErr:        true,
		},
		{
			name:           "at local limit (depth 2, local_max 2)",
			callerDepth:    2,
			callerMaxDepth: 5,
			callerLocalMax: 2,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := types.SpawnRequest{
				CallerDepth:         tt.callerDepth,
				CallerMaxDepth:      tt.callerMaxDepth,
				CallerLocalMaxDepth: tt.callerLocalMax,
				EpicID:              "test-epic",
				RequestedSubagents: []types.SubagentRequest{
					{Role: types.RoleCoder, Task: "test task"},
				},
			}

			// Create a minimal overseer for testing
			rt := NewRuntime(RuntimeConfig{
				DefaultMaxDepth: 5,
				LLMAPIKey:       "test-key", // Won't actually be used
			})
			o := NewOverseer(rt, OverseerConfig{
				MaxDepth:            5,
				MaxConcurrentAgents: 10,
			})

			resp, err := o.HandleSpawnRequest(context.Background(), req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantErr {
				if len(resp.DeniedAgents) == 0 {
					t.Errorf("expected denial, got %d spawned", len(resp.SpawnedAgents))
				}
			} else {
				// Note: spawn will fail because LLM isn't actually configured,
				// but it should at least pass depth validation
				if len(resp.DeniedAgents) > 0 {
					reason := resp.DeniedAgents[0].Reason
					if reason == "depth_limit_exceeded" || reason == "local_limit_exceeded" {
						t.Errorf("unexpected depth denial: %s", reason)
					}
					// Other reasons (like spawn_failed due to LLM) are expected
				}
			}
		})
	}
}

func TestComputeChildDepthLimits(t *testing.T) {
	tests := []struct {
		name              string
		parentDepth       int
		parentMaxDepth    int
		parentLocalMax    int
		requestedLocalMax int
		wantChildDepth    int
		wantChildMaxDepth int
		wantChildLocalMax int
	}{
		{
			name:              "basic inheritance",
			parentDepth:       0,
			parentMaxDepth:    3,
			parentLocalMax:    3,
			requestedLocalMax: 0,
			wantChildDepth:    1,
			wantChildMaxDepth: 3,
			wantChildLocalMax: 3,
		},
		{
			name:              "tighten local max",
			parentDepth:       1,
			parentMaxDepth:    5,
			parentLocalMax:    5,
			requestedLocalMax: 3,
			wantChildDepth:    2,
			wantChildMaxDepth: 5,
			wantChildLocalMax: 3,
		},
		{
			name:              "cannot loosen local max",
			parentDepth:       1,
			parentMaxDepth:    5,
			parentLocalMax:    3,
			requestedLocalMax: 5, // Tries to exceed parent's limit
			wantChildDepth:    2,
			wantChildMaxDepth: 5,
			wantChildLocalMax: 3, // Clamped to parent's limit
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			childDepth, childMaxDepth, childLocalMax := computeChildDepthLimits(
				tt.parentDepth,
				tt.parentMaxDepth,
				tt.parentLocalMax,
				tt.requestedLocalMax,
			)

			if childDepth != tt.wantChildDepth {
				t.Errorf("childDepth = %d, want %d", childDepth, tt.wantChildDepth)
			}
			if childMaxDepth != tt.wantChildMaxDepth {
				t.Errorf("childMaxDepth = %d, want %d", childMaxDepth, tt.wantChildMaxDepth)
			}
			if childLocalMax != tt.wantChildLocalMax {
				t.Errorf("childLocalMax = %d, want %d", childLocalMax, tt.wantChildLocalMax)
			}
		})
	}
}

// computeChildDepthLimits is a local test helper mirroring the tools version.
func computeChildDepthLimits(parentDepth, parentMaxDepth, parentLocalMaxDepth, requestedLocalMaxDepth int) (childDepth, childMaxDepth, childLocalMaxDepth int) {
	childDepth = parentDepth + 1
	childMaxDepth = parentMaxDepth

	childLocalMaxDepth = parentLocalMaxDepth
	if requestedLocalMaxDepth > 0 && requestedLocalMaxDepth < parentLocalMaxDepth {
		childLocalMaxDepth = requestedLocalMaxDepth
	}

	return childDepth, childMaxDepth, childLocalMaxDepth
}

func TestOverseerRoleFilter(t *testing.T) {
	rt := NewRuntime(RuntimeConfig{
		DefaultMaxDepth: 5,
		LLMAPIKey:       "test-key",
	})
	o := NewOverseer(rt, OverseerConfig{
		MaxDepth:            5,
		MaxConcurrentAgents: 10,
		AllowedRoles:        []types.AgentRole{types.RoleCoder}, // Only coders allowed
	})

	req := types.SpawnRequest{
		CallerDepth:         0,
		CallerMaxDepth:      5,
		CallerLocalMaxDepth: 5,
		EpicID:              "test-epic",
		RequestedSubagents: []types.SubagentRequest{
			{Role: types.RolePlanner, Task: "planning task"}, // Not allowed
			{Role: types.RoleCoder, Task: "coding task"},     // Allowed
		},
	}

	resp, err := o.HandleSpawnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Planner should be denied due to role filter
	plannerDenied := false
	for _, d := range resp.DeniedAgents {
		if d.Role == types.RolePlanner && d.Reason == types.DenialPolicyViolation {
			plannerDenied = true
			break
		}
	}
	if !plannerDenied {
		t.Error("expected planner to be denied due to role filter")
	}

	// Coder might fail for other reasons (LLM not configured) but not for role
	coderDeniedForRole := false
	for _, d := range resp.DeniedAgents {
		if d.Role == types.RoleCoder && d.Reason == types.DenialPolicyViolation {
			coderDeniedForRole = true
			break
		}
	}
	if coderDeniedForRole {
		t.Error("coder should not be denied for role violation")
	}
}

func TestHierarchyNode(t *testing.T) {
	node := &HierarchyNode{
		SessionID: "session-1",
		ActorID:   OverseerActorID,
		Role:      types.RolePlanner,
		Depth:     0,
		Status:    types.StatusRunning,
		Children: []*HierarchyNode{
			{
				SessionID: "session-2",
				ActorID:   "actor:agent:coder:epic1",
				Role:      types.RoleCoder,
				Depth:     1,
				Status:    types.StatusRunning,
				Children:  []*HierarchyNode{},
			},
		},
	}

	if node.Depth != 0 {
		t.Errorf("root depth = %d, want 0", node.Depth)
	}
	if len(node.Children) != 1 {
		t.Errorf("root children count = %d, want 1", len(node.Children))
	}
	if node.Children[0].Depth != 1 {
		t.Errorf("child depth = %d, want 1", node.Children[0].Depth)
	}
}
