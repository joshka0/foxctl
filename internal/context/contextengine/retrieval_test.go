package contextengine

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRetrievalFeedbackKind_IsValid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		kind  RetrievalFeedbackKind
		valid bool
	}{
		{RetrievalFeedbackKindEvidenceUsed, true},
		{RetrievalFeedbackKindAnswerAccepted, true},
		{RetrievalFeedbackKindAnswerCorrected, true},
		{RetrievalFeedbackKindRetrievalMissed, true},
		{RetrievalFeedbackKindWrongFileRetrieved, true},
		{RetrievalFeedbackKindStaleContextUsed, true},
		{RetrievalFeedbackKindGapCreated, true},
		{RetrievalFeedbackKind("invalid"), false},
		{RetrievalFeedbackKind(""), false},
	}

	for _, tc := range tests {
		t.Run(string(tc.kind), func(t *testing.T) {
			if got := tc.kind.IsValid(); got != tc.valid {
				t.Errorf("IsValid() = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestRetrievalEpisode_Validate(t *testing.T) {
	t.Parallel()
	validEp := RetrievalEpisode{
		ID:        "ep-1",
		WorkspaceID: "ws-1",
		Query:     "how does auth work",
		Lane:      LaneCode,
		CreatedAt: time.Now(),
	}

	t.Run("valid", func(t *testing.T) {
		if err := validEp.Validate(); err != nil {
			t.Errorf("expected valid, got %v", err)
		}
	})

	t.Run("missing_id", func(t *testing.T) {
		ep := validEp
		ep.ID = ""
		if err := ep.Validate(); err == nil {
			t.Error("expected error for missing id")
		}
	})

	t.Run("missing_workspace_id", func(t *testing.T) {
		ep := validEp
		ep.WorkspaceID = ""
		if err := ep.Validate(); err == nil {
			t.Error("expected error for missing workspace_id")
		}
	})

	t.Run("missing_query", func(t *testing.T) {
		ep := validEp
		ep.Query = ""
		if err := ep.Validate(); err == nil {
			t.Error("expected error for missing query")
		}
	})

	t.Run("invalid_lane", func(t *testing.T) {
		ep := validEp
		ep.Lane = EvidenceLane("invalid")
		if err := ep.Validate(); err == nil {
			t.Error("expected error for invalid lane")
		}
	})

	t.Run("empty_sub_episode_id", func(t *testing.T) {
		ep := validEp
		ep.SubEpisodeIDs = []string{"ep-1", ""}
		if err := ep.Validate(); err == nil {
			t.Error("expected error for empty sub_episode_id")
		}
	})
}

func TestRetrievalEpisode_RoundTrip(t *testing.T) {
	t.Parallel()
	orig := RetrievalEpisode{
		ID:             "ep-1",
		WorkspaceID:    "ws-1",
		Query:          "authentication middleware",
		Lane:           LaneMixed,
		PackID:         "pack-42",
		DurationMs:     150,
		TokensUsed:     500,
		HitCount:       10,
		SubEpisodeIDs:  []string{"ep-2", "ep-3"},
		CreatedAt:      time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got RetrievalEpisode
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.ID != orig.ID {
		t.Errorf("ID: got %q, want %q", got.ID, orig.ID)
	}
	if got.Lane != orig.Lane {
		t.Errorf("Lane: got %q, want %q", got.Lane, orig.Lane)
	}
	if got.DurationMs != orig.DurationMs {
		t.Errorf("DurationMs: got %d, want %d", got.DurationMs, orig.DurationMs)
	}
	if len(got.SubEpisodeIDs) != len(orig.SubEpisodeIDs) {
		t.Errorf("SubEpisodeIDs: got %d, want %d", len(got.SubEpisodeIDs), len(orig.SubEpisodeIDs))
	}
}

func TestRetrievalFeedback_Validate(t *testing.T) {
	t.Parallel()
	validFB := RetrievalFeedback{
		ID:         "fb-1",
		WorkspaceID: "ws-1",
		EpisodeID:  "ep-1",
		Kind:       RetrievalFeedbackKindAnswerAccepted,
		Query:      "how does auth work",
		CreatedAt:  time.Now(),
	}

	t.Run("valid", func(t *testing.T) {
		if err := validFB.Validate(); err != nil {
			t.Errorf("expected valid, got %v", err)
		}
	})

	t.Run("missing_id", func(t *testing.T) {
		fb := validFB
		fb.ID = ""
		if err := fb.Validate(); err == nil {
			t.Error("expected error for missing id")
		}
	})

	t.Run("missing_workspace_id", func(t *testing.T) {
		fb := validFB
		fb.WorkspaceID = ""
		if err := fb.Validate(); err == nil {
			t.Error("expected error for missing workspace_id")
		}
	})

	t.Run("missing_episode_id", func(t *testing.T) {
		fb := validFB
		fb.EpisodeID = ""
		if err := fb.Validate(); err == nil {
			t.Error("expected error for missing episode_id")
		}
	})

	t.Run("invalid_kind", func(t *testing.T) {
		fb := validFB
		fb.Kind = RetrievalFeedbackKind("invalid")
		if err := fb.Validate(); err == nil {
			t.Error("expected error for invalid kind")
		}
	})

	t.Run("missing_query", func(t *testing.T) {
		fb := validFB
		fb.Query = ""
		if err := fb.Validate(); err == nil {
			t.Error("expected error for missing query")
		}
	})

	t.Run("invalid_used_ref", func(t *testing.T) {
		fb := validFB
		fb.UsedRefs = []EvidenceRef{{Type: "bad", Ref: "x"}}
		if err := fb.Validate(); err == nil {
			t.Error("expected error for invalid used_ref")
		}
	})
}

func TestRetrievalFeedback_RoundTrip(t *testing.T) {
	t.Parallel()
	orig := RetrievalFeedback{
		ID:             "fb-1",
		WorkspaceID:    "ws-1",
		EpisodeID:      "ep-1",
		Kind:           RetrievalFeedbackKindAnswerCorrected,
		Query:          "how does auth work",
		UsedRefs:       []EvidenceRef{{Type: RefTypePath, Ref: "src/auth.go"}},
		CorrectionStmt: "The auth system uses JWT tokens, not sessions",
		CreatedAt:      time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got RetrievalFeedback
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.ID != orig.ID {
		t.Errorf("ID: got %q, want %q", got.ID, orig.ID)
	}
	if got.Kind != orig.Kind {
		t.Errorf("Kind: got %q, want %q", got.Kind, orig.Kind)
	}
	if got.CorrectionStmt != orig.CorrectionStmt {
		t.Errorf("CorrectionStmt: got %q, want %q", got.CorrectionStmt, orig.CorrectionStmt)
	}
	if len(got.UsedRefs) != len(orig.UsedRefs) {
		t.Errorf("UsedRefs: got %d, want %d", len(got.UsedRefs), len(orig.UsedRefs))
	}
}

func TestRetrievalFeedbackKind_AllValues(t *testing.T) {
	t.Parallel()
	// Verify all constants are reachable and valid
	kinds := []RetrievalFeedbackKind{
		RetrievalFeedbackKindEvidenceUsed,
		RetrievalFeedbackKindAnswerAccepted,
		RetrievalFeedbackKindAnswerCorrected,
		RetrievalFeedbackKindRetrievalMissed,
		RetrievalFeedbackKindWrongFileRetrieved,
		RetrievalFeedbackKindStaleContextUsed,
		RetrievalFeedbackKindGapCreated,
	}
	for _, k := range kinds {
		if !k.IsValid() {
			t.Errorf("constant %q should be valid", k)
		}
	}
}

func TestRetrievalEpisode_AllLanes(t *testing.T) {
	t.Parallel()
	// Verify episode validates with all lane types
	lanes := []EvidenceLane{LaneCode, LaneMemory, LaneContext, LaneTask, LaneMixed}
	for _, lane := range lanes {
		ep := RetrievalEpisode{
			ID:          "ep-1",
			WorkspaceID: "ws-1",
			Query:       "test query",
			Lane:        lane,
			CreatedAt:   time.Now(),
		}
		if err := ep.Validate(); err != nil {
			t.Errorf("lane %q should be valid: %v", lane, err)
		}
	}
}
