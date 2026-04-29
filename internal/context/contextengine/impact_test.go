package contextengine

import (
	"encoding/json"
	"testing"
	"time"
)

func TestImpactEdgeKind_IsValid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		kind  ImpactEdgeKind
		valid bool
	}{
		{ImpactEdgeKindDependsOn, true},
		{ImpactEdgeKindCites, true},
		{ImpactEdgeKindGeneratedFrom, true},
		{ImpactEdgeKindValidates, true},
		{ImpactEdgeKindInvalidates, true},
		{ImpactEdgeKindSupersedes, true},
		{ImpactEdgeKindRelatesTo, true},
		{ImpactEdgeKind("invalid"), false},
		{ImpactEdgeKind(""), false},
	}

	for _, tc := range tests {
		t.Run(string(tc.kind), func(t *testing.T) {
			if got := tc.kind.IsValid(); got != tc.valid {
				t.Errorf("IsValid() = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestImpactEdge_Validate(t *testing.T) {
	t.Parallel()
	validEdge := ImpactEdge{
		ID:          "edge-1",
		WorkspaceID: "ws-1",
		From:        EvidenceRef{Type: RefTypePath, Ref: "src/a.go"},
		To:          EvidenceRef{Type: RefTypePath, Ref: "src/b.go"},
		Kind:        ImpactEdgeKindDependsOn,
		CreatedAt:   time.Now(),
	}

	t.Run("valid", func(t *testing.T) {
		if err := validEdge.Validate(); err != nil {
			t.Errorf("expected valid, got %v", err)
		}
	})

	t.Run("missing_id", func(t *testing.T) {
		e := validEdge
		e.ID = ""
		if err := e.Validate(); err == nil {
			t.Error("expected error for missing id")
		}
	})

	t.Run("missing_workspace_id", func(t *testing.T) {
		e := validEdge
		e.WorkspaceID = ""
		if err := e.Validate(); err == nil {
			t.Error("expected error for missing workspace_id")
		}
	})

	t.Run("invalid_from_ref", func(t *testing.T) {
		e := validEdge
		e.From = EvidenceRef{Type: "bad", Ref: "x"}
		if err := e.Validate(); err == nil {
			t.Error("expected error for invalid from ref")
		}
	})

	t.Run("invalid_to_ref", func(t *testing.T) {
		e := validEdge
		e.To = EvidenceRef{Type: "bad", Ref: "x"}
		if err := e.Validate(); err == nil {
			t.Error("expected error for invalid to ref")
		}
	})

	t.Run("invalid_kind", func(t *testing.T) {
		e := validEdge
		e.Kind = "invalid"
		if err := e.Validate(); err == nil {
			t.Error("expected error for invalid kind")
		}
	})

	t.Run("valid_with_source_event", func(t *testing.T) {
		e := validEdge
		e.SourceEventID = "evt-1"
		if err := e.Validate(); err != nil {
			t.Errorf("expected valid with source event, got %v", err)
		}
	})
}

func TestImpactEdge_RoundTrip(t *testing.T) {
	t.Parallel()
	orig := ImpactEdge{
		ID:            "edge-1",
		WorkspaceID:   "ws-1",
		From:          EvidenceRef{Type: RefTypeSymbol, Ref: "AuthService"},
		To:            EvidenceRef{Type: RefTypeSymbol, Ref: "DatabaseLayer"},
		Kind:          ImpactEdgeKindCites,
		SourceEventID: "evt-42",
		CreatedAt:     time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got ImpactEdge
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.ID != orig.ID {
		t.Errorf("ID: got %q, want %q", got.ID, orig.ID)
	}
	if got.Kind != orig.Kind {
		t.Errorf("Kind: got %q, want %q", got.Kind, orig.Kind)
	}
	if got.SourceEventID != orig.SourceEventID {
		t.Errorf("SourceEventID: got %q, want %q", got.SourceEventID, orig.SourceEventID)
	}
}

func TestImpactEdgeKind_AllValues(t *testing.T) {
	t.Parallel()
	kinds := []ImpactEdgeKind{
		ImpactEdgeKindDependsOn,
		ImpactEdgeKindCites,
		ImpactEdgeKindGeneratedFrom,
		ImpactEdgeKindValidates,
		ImpactEdgeKindInvalidates,
		ImpactEdgeKindSupersedes,
		ImpactEdgeKindRelatesTo,
	}
	for _, k := range kinds {
		if !k.IsValid() {
			t.Errorf("constant %q should be valid", k)
		}
	}
}
