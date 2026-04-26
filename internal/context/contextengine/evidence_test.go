package contextengine

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEvidenceNodeType_IsValid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		nodeType EvidenceNodeType
		valid    bool
	}{
		{EvidenceNodeTypeCode, true},
		{EvidenceNodeTypeMemory, true},
		{EvidenceNodeTypeContext, true},
		{EvidenceNodeTypeTask, true},
		{EvidenceNodeTypeTrajectory, true},
		{EvidenceNodeTypeObservation, true},
		{EvidenceNodeTypeTension, true},
		{EvidenceNodeTypeRetrieval, true},
		{EvidenceNodeType("invalid"), false},
		{EvidenceNodeType(""), false},
	}

	for _, tc := range tests {
		t.Run(string(tc.nodeType), func(t *testing.T) {
			if got := tc.nodeType.IsValid(); got != tc.valid {
				t.Errorf("IsValid() = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestGrounding_IsValid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		g    Grounding
		valid bool
	}{
		{GroundingLoaded, true},
		{GroundingIndexed, true},
		{GroundingSemantic, true},
		{GroundingInferred, true},
		{GroundingValidated, true},
		{Grounding("invalid"), false},
		{Grounding(""), false},
	}

	for _, tc := range tests {
		t.Run(string(tc.g), func(t *testing.T) {
			if got := tc.g.IsValid(); got != tc.valid {
				t.Errorf("IsValid() = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestEvidenceNode_Validate(t *testing.T) {
	t.Parallel()
	validNode := EvidenceNode{
		ID:          "node-1",
		WorkspaceID: "ws-1",
		NodeType:    EvidenceNodeTypeCode,
		Ref:         EvidenceRef{Type: RefTypePath, Ref: "src/main.go"},
		FirstSeen:   time.Now(),
		LastSeen:    time.Now(),
	}

	t.Run("valid", func(t *testing.T) {
		if err := validNode.Validate(); err != nil {
			t.Errorf("expected valid, got %v", err)
		}
	})

	t.Run("missing_id", func(t *testing.T) {
		n := validNode
		n.ID = ""
		if err := n.Validate(); err == nil {
			t.Error("expected error for missing id")
		}
	})

	t.Run("missing_workspace_id", func(t *testing.T) {
		n := validNode
		n.WorkspaceID = ""
		if err := n.Validate(); err == nil {
			t.Error("expected error for missing workspace_id")
		}
	})

	t.Run("invalid_node_type", func(t *testing.T) {
		n := validNode
		n.NodeType = "invalid"
		if err := n.Validate(); err == nil {
			t.Error("expected error for invalid node_type")
		}
	})

	t.Run("invalid_ref", func(t *testing.T) {
		n := validNode
		n.Ref = EvidenceRef{Type: "bad", Ref: "x"}
		if err := n.Validate(); err == nil {
			t.Error("expected error for invalid ref")
		}
	})

	t.Run("invalid_grounding", func(t *testing.T) {
		n := validNode
		n.Grounding = "invalid"
		if err := n.Validate(); err == nil {
			t.Error("expected error for invalid grounding")
		}
	})

	t.Run("valid_with_optional_fields", func(t *testing.T) {
		n := validNode
		n.Statement = "This is a code snippet"
		n.Confidence = 0.95
		n.Grounding = GroundingValidated
		n.Count = 5
		n.Metadata = map[string]any{"key": "value"}
		if err := n.Validate(); err != nil {
			t.Errorf("expected valid with optional fields, got %v", err)
		}
	})
}

func TestEvidenceNode_RoundTrip(t *testing.T) {
	t.Parallel()
	orig := EvidenceNode{
		ID:          "node-1",
		WorkspaceID: "ws-1",
		NodeType:    EvidenceNodeTypeCode,
		Ref:         EvidenceRef{Type: RefTypePath, Ref: "src/main.go", Title: "Main file"},
		Statement:   "Main application entry point",
		Confidence:  0.9,
		Grounding:   GroundingSemantic,
		Count:       3,
		FirstSeen:   time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
		LastSeen:    time.Date(2025, 1, 16, 14, 30, 0, 0, time.UTC),
		Metadata:    map[string]any{"lines": 150},
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got EvidenceNode
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.ID != orig.ID {
		t.Errorf("ID: got %q, want %q", got.ID, orig.ID)
	}
	if got.NodeType != orig.NodeType {
		t.Errorf("NodeType: got %q, want %q", got.NodeType, orig.NodeType)
	}
	if got.Confidence != orig.Confidence {
		t.Errorf("Confidence: got %f, want %f", got.Confidence, orig.Confidence)
	}
}

func TestEvidenceLane_IsValid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		lane  EvidenceLane
		valid bool
	}{
		{LaneCode, true},
		{LaneMemory, true},
		{LaneContext, true},
		{LaneTask, true},
		{LaneMixed, true},
		{EvidenceLane("invalid"), false},
		{EvidenceLane(""), false},
	}

	for _, tc := range tests {
		t.Run(string(tc.lane), func(t *testing.T) {
			if got := tc.lane.IsValid(); got != tc.valid {
				t.Errorf("IsValid() = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestEvidenceTelemetry_RoundTrip(t *testing.T) {
	t.Parallel()
	orig := EvidenceTelemetry{
		DurationMs:  150,
		TokensUsed: 500,
		LanesFused: 3,
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got EvidenceTelemetry
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.DurationMs != orig.DurationMs {
		t.Errorf("DurationMs: got %d, want %d", got.DurationMs, orig.DurationMs)
	}
	if got.TokensUsed != orig.TokensUsed {
		t.Errorf("TokensUsed: got %d, want %d", got.TokensUsed, orig.TokensUsed)
	}
	if got.LanesFused != orig.LanesFused {
		t.Errorf("LanesFused: got %d, want %d", got.LanesFused, orig.LanesFused)
	}
}

func TestEvidencePack_Validate(t *testing.T) {
	t.Parallel()
	validPack := EvidencePack{
		ID:          "pack-1",
		WorkspaceID: "ws-1",
		Query:       "authentication flow",
		Lane:        LaneCode,
	}

	t.Run("valid", func(t *testing.T) {
		if err := validPack.Validate(); err != nil {
			t.Errorf("expected valid, got %v", err)
		}
	})

	t.Run("missing_id", func(t *testing.T) {
		p := validPack
		p.ID = ""
		if err := p.Validate(); err == nil {
			t.Error("expected error for missing id")
		}
	})

	t.Run("missing_workspace_id", func(t *testing.T) {
		p := validPack
		p.WorkspaceID = ""
		if err := p.Validate(); err == nil {
			t.Error("expected error for missing workspace_id")
		}
	})

	t.Run("missing_query", func(t *testing.T) {
		p := validPack
		p.Query = ""
		if err := p.Validate(); err == nil {
			t.Error("expected error for missing query")
		}
	})

	t.Run("invalid_lane", func(t *testing.T) {
		p := validPack
		p.Lane = "invalid"
		if err := p.Validate(); err == nil {
			t.Error("expected error for invalid lane")
		}
	})

	t.Run("invalid_node", func(t *testing.T) {
		p := validPack
		p.Nodes = []EvidenceNode{{
			ID:          "node-1",
			WorkspaceID: "ws-1",
			NodeType:    EvidenceNodeTypeCode,
			Ref:         EvidenceRef{Type: "bad", Ref: "x"},
		}}
		if err := p.Validate(); err == nil {
			t.Error("expected error for invalid node")
		}
	})

	t.Run("valid_with_nodes", func(t *testing.T) {
		p := validPack
		p.Nodes = []EvidenceNode{
			{
				ID:          "node-1",
				WorkspaceID: "ws-1",
				NodeType:    EvidenceNodeTypeCode,
				Ref:         EvidenceRef{Type: RefTypePath, Ref: "src/auth.go"},
			},
		}
		p.Telemetry = EvidenceTelemetry{DurationMs: 100, TokensUsed: 200}
		if err := p.Validate(); err != nil {
			t.Errorf("expected valid with nodes, got %v", err)
		}
	})
}

func TestEvidencePack_RoundTrip(t *testing.T) {
	t.Parallel()
	orig := EvidencePack{
		ID:          "pack-1",
		WorkspaceID: "ws-1",
		Query:       "auth middleware implementation",
		Lane:        LaneMixed,
		Nodes: []EvidenceNode{
			{
				ID:          "node-1",
				WorkspaceID: "ws-1",
				NodeType:    EvidenceNodeTypeCode,
				Ref:         EvidenceRef{Type: RefTypePath, Ref: "src/auth.go"},
				Confidence:  0.9,
			},
		},
		Telemetry: EvidenceTelemetry{
			DurationMs:  200,
			TokensUsed: 800,
			LanesFused: 2,
		},
		Metadata: map[string]any{"retrieval_mode": "hybrid"},
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got EvidencePack
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.ID != orig.ID {
		t.Errorf("ID: got %q, want %q", got.ID, orig.ID)
	}
	if got.Lane != orig.Lane {
		t.Errorf("Lane: got %q, want %q", got.Lane, orig.Lane)
	}
	if len(got.Nodes) != len(orig.Nodes) {
		t.Errorf("Nodes: got %d, want %d", len(got.Nodes), len(orig.Nodes))
	}
	if got.Telemetry.DurationMs != orig.Telemetry.DurationMs {
		t.Errorf("Telemetry.DurationMs: got %d, want %d", got.Telemetry.DurationMs, orig.Telemetry.DurationMs)
	}
}
