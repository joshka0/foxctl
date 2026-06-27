package contextengine

import (
	"encoding/json"
	"testing"
	"time"
)

func TestContextEventKind_IsValid(t *testing.T) {
	t.Parallel()
	kinds := []ContextEventKind{
		EventKindCodeChangedDirty,
		EventKindCodeIndexedWorktree,
		EventKindCodeValidated,
		EventKindCodeCommitted,
		EventKindTaskChanged,
		EventKindSessionTurnCaptured,
		EventKindToolEvidenceProduced,
		EventKindRetrievalExecuted,
		EventKindRetrievalMissed,
		EventKindAnswerCorrected,
		EventKindMemoryClaimProposed,
		EventKindMemoryClaimPromoted,
		EventKindMemoryClaimRevalidate,
		EventKindMemoryClaimInvalidated,
		EventKindProjectionGenerated,
	}

	for _, k := range kinds {
		t.Run(string(k), func(t *testing.T) {
			if !k.IsValid() {
				t.Errorf("expected %q to be valid", k)
			}
		})
	}

	t.Run("invalid", func(t *testing.T) {
		if ContextEventKind("invalid").IsValid() {
			t.Error("expected invalid to be false")
		}
	})
}

func TestContextEvent_Validate(t *testing.T) {
	t.Parallel()
	validEvent := ContextEvent{
		ID:          "evt-1",
		WorkspaceID: "ws-1",
		Kind:        EventKindCodeChangedDirty,
		Source:      "workspace-scanner",
		CreatedAt:   time.Now(),
	}

	t.Run("valid", func(t *testing.T) {
		if err := validEvent.Validate(); err != nil {
			t.Errorf("expected valid, got %v", err)
		}
	})

	t.Run("missing_id", func(t *testing.T) {
		e := validEvent
		e.ID = ""
		if err := e.Validate(); err == nil {
			t.Error("expected error for missing id")
		}
	})

	t.Run("missing_workspace_id", func(t *testing.T) {
		e := validEvent
		e.WorkspaceID = ""
		if err := e.Validate(); err == nil {
			t.Error("expected error for missing workspace_id")
		}
	})

	t.Run("invalid_kind", func(t *testing.T) {
		e := validEvent
		e.Kind = "invalid"
		if err := e.Validate(); err == nil {
			t.Error("expected error for invalid kind")
		}
	})

	t.Run("missing_source", func(t *testing.T) {
		e := validEvent
		e.Source = ""
		if err := e.Validate(); err == nil {
			t.Error("expected error for missing source")
		}
	})

	t.Run("valid_with_optional_fields", func(t *testing.T) {
		e := validEvent
		e.TaskID = "task-1"
		e.SessionID = "session-1"
		e.Refs = []EvidenceRef{{Type: RefTypePath, Ref: "src/main.go"}}
		e.Data = map[string]any{"key": "value"}
		if err := e.Validate(); err != nil {
			t.Errorf("expected valid with optional fields, got %v", err)
		}
	})
}

func TestContextEvent_RoundTrip(t *testing.T) {
	t.Parallel()
	orig := ContextEvent{
		ID:          "evt-1",
		WorkspaceID: "ws-1",
		Kind:        EventKindRetrievalExecuted,
		Source:      "retrieval-engine",
		TaskID:      "task-42",
		SessionID:   "session-1",
		Refs: []EvidenceRef{
			{Type: RefTypePath, Ref: "src/auth.go"},
			{Type: RefTypeSymbol, Ref: "AuthMiddleware"},
		},
		Data: map[string]any{
			"query":       "authentication middleware",
			"hit_count":   5,
			"duration_ms": 150,
		},
		CreatedAt: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got ContextEvent
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.ID != orig.ID {
		t.Errorf("ID: got %q, want %q", got.ID, orig.ID)
	}
	if got.Kind != orig.Kind {
		t.Errorf("Kind: got %q, want %q", got.Kind, orig.Kind)
	}
	if len(got.Refs) != len(orig.Refs) {
		t.Errorf("Refs: got %d, want %d", len(got.Refs), len(orig.Refs))
	}
	if got.TaskID != orig.TaskID {
		t.Errorf("TaskID: got %q, want %q", got.TaskID, orig.TaskID)
	}
}

func TestContextEventKind_AllValues(t *testing.T) {
	t.Parallel()
	// Verify all constants are reachable and valid
	allKinds := []ContextEventKind{
		EventKindCodeChangedDirty,
		EventKindCodeIndexedWorktree,
		EventKindCodeValidated,
		EventKindCodeCommitted,
		EventKindTaskChanged,
		EventKindSessionTurnCaptured,
		EventKindToolEvidenceProduced,
		EventKindRetrievalExecuted,
		EventKindRetrievalMissed,
		EventKindAnswerCorrected,
		EventKindMemoryClaimProposed,
		EventKindMemoryClaimPromoted,
		EventKindMemoryClaimRevalidate,
		EventKindMemoryClaimInvalidated,
		EventKindProjectionGenerated,
	}
	for _, k := range allKinds {
		if !k.IsValid() {
			t.Errorf("constant %q should be valid", k)
		}
	}
}
