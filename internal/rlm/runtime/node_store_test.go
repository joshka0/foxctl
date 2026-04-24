package runtime

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryNodeStoreTransitions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		transitions []NodeStatus
		wantStatus  NodeStatus
	}{
		{
			name:        "queued_to_running",
			transitions: []NodeStatus{NodeStatusRunning},
			wantStatus:  NodeStatusRunning,
		},
		{
			name:        "queued_to_waiting",
			transitions: []NodeStatus{NodeStatusWaiting},
			wantStatus:  NodeStatusWaiting,
		},
		{
			name:        "queued_to_canceled",
			transitions: []NodeStatus{NodeStatusCanceled},
			wantStatus:  NodeStatusCanceled,
		},
		{
			name:        "running_to_waiting",
			transitions: []NodeStatus{NodeStatusRunning, NodeStatusWaiting},
			wantStatus:  NodeStatusWaiting,
		},
		{
			name:        "running_to_completed",
			transitions: []NodeStatus{NodeStatusRunning, NodeStatusCompleted},
			wantStatus:  NodeStatusCompleted,
		},
		{
			name:        "running_to_failed",
			transitions: []NodeStatus{NodeStatusRunning, NodeStatusFailed},
			wantStatus:  NodeStatusFailed,
		},
		{
			name:        "running_to_canceled",
			transitions: []NodeStatus{NodeStatusRunning, NodeStatusCanceled},
			wantStatus:  NodeStatusCanceled,
		},
		{
			name:        "waiting_to_running",
			transitions: []NodeStatus{NodeStatusRunning, NodeStatusWaiting, NodeStatusRunning},
			wantStatus:  NodeStatusRunning,
		},
		{
			name:        "waiting_to_completed",
			transitions: []NodeStatus{NodeStatusRunning, NodeStatusWaiting, NodeStatusCompleted},
			wantStatus:  NodeStatusCompleted,
		},
		{
			name:        "waiting_to_failed",
			transitions: []NodeStatus{NodeStatusRunning, NodeStatusWaiting, NodeStatusFailed},
			wantStatus:  NodeStatusFailed,
		},
		{
			name:        "waiting_to_canceled",
			transitions: []NodeStatus{NodeStatusRunning, NodeStatusWaiting, NodeStatusCanceled},
			wantStatus:  NodeStatusCanceled,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			now := time.Date(2026, time.March, 10, 9, 8, 7, 0, time.UTC)
			store := NewMemoryNodeStore(WithMemoryNodeStoreNow(func() time.Time {
				now = now.Add(time.Second)
				return now
			}))
			ctx := context.Background()

			if _, err := store.CreateRun(ctx, Run{
				ID:         "run-1",
				RootNodeID: "root",
				Status:     NodeStatusQueued,
			}); err != nil {
				t.Fatalf("CreateRun() error = %v", err)
			}
			if _, err := store.CreateNode(ctx, Node{
				RunID:  "run-1",
				ID:     "root",
				Status: NodeStatusQueued,
			}); err != nil {
				t.Fatalf("CreateNode() error = %v", err)
			}

			var node Node
			var err error
			for _, next := range tc.transitions {
				node, err = store.UpdateNodeStatus(ctx, "run-1", "root", next)
				if err != nil {
					t.Fatalf("UpdateNodeStatus(%q) error = %v", next, err)
				}
			}
			if node.Status != tc.wantStatus {
				t.Fatalf("final status = %q, want %q", node.Status, tc.wantStatus)
			}

			snapshot, err := store.GetNode(ctx, "run-1", "root")
			if err != nil {
				t.Fatalf("GetNode() error = %v", err)
			}
			if snapshot.Status != tc.wantStatus {
				t.Fatalf("snapshot status = %q, want %q", snapshot.Status, tc.wantStatus)
			}
		})
	}
}

func TestMemoryNodeStoreRejectsInvalidTransition(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		prep         []NodeStatus
		next         NodeStatus
		wantPrevious NodeStatus
	}{
		{
			name:         "queued_to_completed_rejected",
			next:         NodeStatusCompleted,
			wantPrevious: NodeStatusQueued,
		},
		{
			name:         "queued_to_failed_rejected",
			next:         NodeStatusFailed,
			wantPrevious: NodeStatusQueued,
		},
		{
			name:         "running_to_queued_rejected",
			prep:         []NodeStatus{NodeStatusRunning},
			next:         NodeStatusQueued,
			wantPrevious: NodeStatusRunning,
		},
		{
			name:         "waiting_to_queued_rejected",
			prep:         []NodeStatus{NodeStatusRunning, NodeStatusWaiting},
			next:         NodeStatusQueued,
			wantPrevious: NodeStatusWaiting,
		},
		{
			name:         "completed_to_running_rejected",
			prep:         []NodeStatus{NodeStatusRunning, NodeStatusCompleted},
			next:         NodeStatusRunning,
			wantPrevious: NodeStatusCompleted,
		},
		{
			name:         "failed_to_waiting_rejected",
			prep:         []NodeStatus{NodeStatusRunning, NodeStatusFailed},
			next:         NodeStatusWaiting,
			wantPrevious: NodeStatusFailed,
		},
		{
			name:         "canceled_to_running_rejected",
			prep:         []NodeStatus{NodeStatusCanceled},
			next:         NodeStatusRunning,
			wantPrevious: NodeStatusCanceled,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := NewMemoryNodeStore()
			ctx := context.Background()

			if _, err := store.CreateRun(ctx, Run{
				ID:         "run-1",
				RootNodeID: "root",
				Status:     NodeStatusQueued,
			}); err != nil {
				t.Fatalf("CreateRun() error = %v", err)
			}
			if _, err := store.CreateNode(ctx, Node{
				RunID:  "run-1",
				ID:     "root",
				Status: NodeStatusQueued,
			}); err != nil {
				t.Fatalf("CreateNode() error = %v", err)
			}
			for _, status := range tc.prep {
				if _, err := store.UpdateNodeStatus(ctx, "run-1", "root", status); err != nil {
					t.Fatalf("prep UpdateNodeStatus(%q) error = %v", status, err)
				}
			}

			_, err := store.UpdateNodeStatus(ctx, "run-1", "root", tc.next)
			if !errors.Is(err, ErrInvalidNodeStatusTransition) {
				t.Fatalf("UpdateNodeStatus(%q) error = %v, want ErrInvalidNodeStatusTransition", tc.next, err)
			}

			snapshot, err := store.GetNode(ctx, "run-1", "root")
			if err != nil {
				t.Fatalf("GetNode() error = %v", err)
			}
			if snapshot.Status != tc.wantPrevious {
				t.Fatalf("snapshot status = %q, want %q", snapshot.Status, tc.wantPrevious)
			}
		})
	}
}

func TestMemoryNodeStoreSnapshotsAreImmutable(t *testing.T) {
	t.Parallel()

	store := NewMemoryNodeStore()
	ctx := context.Background()

	_, err := store.CreateRun(ctx, Run{
		ID:         "run-1",
		RootNodeID: "root",
		Status:     NodeStatusQueued,
		Metadata: map[string]any{
			"owner": "worker-a",
			"nested": map[string]any{
				"phase": "initial",
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	_, err = store.CreateNode(ctx, Node{
		RunID:  "run-1",
		ID:     "root",
		Status: NodeStatusQueued,
		Metadata: map[string]any{
			"plan": map[string]any{
				"step": "queue",
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}
	_, err = store.SetNodeResult(ctx, "run-1", "root", NodeResult{
		Status:  NodeStatusQueued,
		Summary: "initial",
		Findings: []Finding{
			{
				ID:      "f-1",
				Summary: "first",
				EvidenceRefs: []EvidenceRef{
					{Kind: "path", Ref: "internal/rlm/runtime/node.go"},
				},
				ArtifactRefs: []ArtifactRef{
					{ID: "a-1", URI: "runs/run-1/nodes/root/result.json"},
				},
				Metadata: map[string]any{
					"rank": 1,
				},
			},
		},
		EvidenceRefs: []EvidenceRef{
			{Kind: "tool", Ref: "rlm_query"},
		},
		ArtifactRefs: []ArtifactRef{
			{ID: "result", URI: "sha256:abc"},
		},
		Metadata: map[string]any{
			"state": map[string]any{
				"phase": "queued",
			},
		},
	})
	if err != nil {
		t.Fatalf("SetNodeResult() error = %v", err)
	}

	runSnapshot, err := store.GetRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	nodeSnapshot, err := store.GetNode(ctx, "run-1", "root")
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	nodesSnapshot, err := store.ListNodes(ctx, "run-1")
	if err != nil {
		t.Fatalf("ListNodes() error = %v", err)
	}

	runSnapshot.Metadata["owner"] = "mutated"
	runSnapshot.Metadata["nested"].(map[string]any)["phase"] = "mutated"

	nodeSnapshot.Status = NodeStatusFailed
	nodeSnapshot.Metadata["plan"].(map[string]any)["step"] = "mutated"
	nodeSnapshot.Result.Summary = "mutated"
	nodeSnapshot.Result.Findings[0].Summary = "mutated"
	nodeSnapshot.Result.Findings[0].EvidenceRefs[0].Ref = "mutated"
	nodeSnapshot.Result.Findings[0].ArtifactRefs[0].URI = "mutated"
	nodeSnapshot.Result.Findings[0].Metadata["rank"] = 99
	nodeSnapshot.Result.EvidenceRefs[0].Ref = "mutated"
	nodeSnapshot.Result.ArtifactRefs[0].URI = "mutated"
	nodeSnapshot.Result.Metadata["state"].(map[string]any)["phase"] = "mutated"

	nodesSnapshot[0].Status = NodeStatusCanceled
	nodesSnapshot[0].Result.Summary = "mutated-by-list"

	runAfter, err := store.GetRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("GetRun() after mutation error = %v", err)
	}
	nodeAfter, err := store.GetNode(ctx, "run-1", "root")
	if err != nil {
		t.Fatalf("GetNode() after mutation error = %v", err)
	}

	if runAfter.Metadata["owner"] != "worker-a" {
		t.Fatalf("run metadata mutated: owner=%v", runAfter.Metadata["owner"])
	}
	if runAfter.Metadata["nested"].(map[string]any)["phase"] != "initial" {
		t.Fatalf("run nested metadata mutated: phase=%v", runAfter.Metadata["nested"].(map[string]any)["phase"])
	}
	if nodeAfter.Status != NodeStatusQueued {
		t.Fatalf("node status mutated: %q", nodeAfter.Status)
	}
	if nodeAfter.Metadata["plan"].(map[string]any)["step"] != "queue" {
		t.Fatalf("node metadata mutated: %v", nodeAfter.Metadata["plan"].(map[string]any)["step"])
	}
	if nodeAfter.Result.Summary != "initial" {
		t.Fatalf("result summary mutated: %q", nodeAfter.Result.Summary)
	}
	if nodeAfter.Result.Findings[0].Summary != "first" {
		t.Fatalf("finding summary mutated: %q", nodeAfter.Result.Findings[0].Summary)
	}
	if nodeAfter.Result.Findings[0].EvidenceRefs[0].Ref != "internal/rlm/runtime/node.go" {
		t.Fatalf("finding evidence ref mutated: %q", nodeAfter.Result.Findings[0].EvidenceRefs[0].Ref)
	}
	if nodeAfter.Result.Findings[0].ArtifactRefs[0].URI != "runs/run-1/nodes/root/result.json" {
		t.Fatalf("finding artifact ref mutated: %q", nodeAfter.Result.Findings[0].ArtifactRefs[0].URI)
	}
	if nodeAfter.Result.Findings[0].Metadata["rank"] != 1 {
		t.Fatalf("finding metadata mutated: %v", nodeAfter.Result.Findings[0].Metadata["rank"])
	}
	if nodeAfter.Result.EvidenceRefs[0].Ref != "rlm_query" {
		t.Fatalf("result evidence ref mutated: %q", nodeAfter.Result.EvidenceRefs[0].Ref)
	}
	if nodeAfter.Result.ArtifactRefs[0].URI != "sha256:abc" {
		t.Fatalf("result artifact ref mutated: %q", nodeAfter.Result.ArtifactRefs[0].URI)
	}
	if nodeAfter.Result.Metadata["state"].(map[string]any)["phase"] != "queued" {
		t.Fatalf("result metadata mutated: %v", nodeAfter.Result.Metadata["state"].(map[string]any)["phase"])
	}
}
