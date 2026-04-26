package contextengine

import (
	"encoding/json"
	"testing"
	"time"
)

func TestProjectionMeta_Validate(t *testing.T) {
	t.Parallel()
	validMeta := ProjectionMeta{
		ProjectionID:      "proj-1",
		ProjectionType:    "top_of_mind",
		ProjectionVersion: 1,
		WorkspaceID:       "ws-1",
		GeneratedAt:       time.Now(),
	}

	t.Run("valid", func(t *testing.T) {
		if err := validMeta.Validate(); err != nil {
			t.Errorf("expected valid, got %v", err)
		}
	})

	t.Run("missing_projection_id", func(t *testing.T) {
		m := validMeta
		m.ProjectionID = ""
		if err := m.Validate(); err == nil {
			t.Error("expected error for missing projection_id")
		}
	})

	t.Run("missing_projection_type", func(t *testing.T) {
		m := validMeta
		m.ProjectionType = ""
		if err := m.Validate(); err == nil {
			t.Error("expected error for missing projection_type")
		}
	})

	t.Run("version_less_than_1", func(t *testing.T) {
		m := validMeta
		m.ProjectionVersion = 0
		if err := m.Validate(); err == nil {
			t.Error("expected error for projection_version < 1")
		}
	})

	t.Run("missing_workspace_id", func(t *testing.T) {
		m := validMeta
		m.WorkspaceID = ""
		if err := m.Validate(); err == nil {
			t.Error("expected error for missing workspace_id")
		}
	})
}

func TestProjectionMeta_RoundTrip(t *testing.T) {
	t.Parallel()
	orig := ProjectionMeta{
		ProjectionID:        "proj-1",
		ProjectionType:     "task_context",
		ProjectionVersion:  3,
		WorkspaceID:        "ws-1",
		GeneratedFromEvents: []string{"evt-1", "evt-2"},
		GeneratedAt:        time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		ExpiresAt:          time.Date(2025, 1, 16, 10, 30, 0, 0, time.UTC),
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got ProjectionMeta
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.ProjectionID != orig.ProjectionID {
		t.Errorf("ProjectionID: got %q, want %q", got.ProjectionID, orig.ProjectionID)
	}
	if got.ProjectionType != orig.ProjectionType {
		t.Errorf("ProjectionType: got %q, want %q", got.ProjectionType, orig.ProjectionType)
	}
	if got.ProjectionVersion != orig.ProjectionVersion {
		t.Errorf("ProjectionVersion: got %d, want %d", got.ProjectionVersion, orig.ProjectionVersion)
	}
	if got.WorkspaceID != orig.WorkspaceID {
		t.Errorf("WorkspaceID: got %q, want %q", got.WorkspaceID, orig.WorkspaceID)
	}
	if len(got.GeneratedFromEvents) != len(orig.GeneratedFromEvents) {
		t.Errorf("GeneratedFromEvents: got %d, want %d", len(got.GeneratedFromEvents), len(orig.GeneratedFromEvents))
	}
}

func TestWorkingSet_AddDirtyRef(t *testing.T) {
	t.Parallel()
	ws := &WorkingSet{WorkspaceID: "ws-1", UpdatedAt: time.Now()}
	ref := EvidenceRef{Type: RefTypePath, Ref: "src/main.go"}

	ws.AddDirtyRef(ref)
	if len(ws.DirtyRefs) != 1 {
		t.Fatalf("expected 1 dirty ref, got %d", len(ws.DirtyRefs))
	}

	ws.AddDirtyRef(ref)
	if len(ws.DirtyRefs) != 1 {
		t.Error("expected deduplication to prevent duplicate")
	}

	ws.AddDirtyRef(EvidenceRef{Type: RefTypeSymbol, Ref: "main"})
	if len(ws.DirtyRefs) != 2 {
		t.Errorf("expected 2 dirty refs, got %d", len(ws.DirtyRefs))
	}
}

func TestWorkingSet_AddRecentCommand(t *testing.T) {
	t.Parallel()
	ws := &WorkingSet{WorkspaceID: "ws-1", UpdatedAt: time.Now()}

	ws.AddRecentCommand("build", 3)
	ws.AddRecentCommand("test", 3)
	ws.AddRecentCommand("lint", 3)

	if len(ws.RecentCommands) != 3 {
		t.Fatalf("expected 3 commands, got %d", len(ws.RecentCommands))
	}

	ws.AddRecentCommand("build", 3)
	if len(ws.RecentCommands) != 3 {
		t.Error("expected deduplication to prevent duplicate")
	}

	ws.AddRecentCommand("fmt", 3)
	if len(ws.RecentCommands) != 3 {
		t.Errorf("expected bounded to 3, got %d", len(ws.RecentCommands))
	}
	if ws.RecentCommands[0] != "test" {
		t.Errorf("expected oldest removed, got %q", ws.RecentCommands[0])
	}
}

func TestWorkingSet_Validate(t *testing.T) {
	t.Parallel()
	validWS := WorkingSet{
		WorkspaceID: "ws-1",
		UpdatedAt:  time.Now(),
	}

	t.Run("valid", func(t *testing.T) {
		if err := validWS.Validate(); err != nil {
			t.Errorf("expected valid, got %v", err)
		}
	})

	t.Run("missing_workspace_id", func(t *testing.T) {
		ws := validWS
		ws.WorkspaceID = ""
		if err := ws.Validate(); err == nil {
			t.Error("expected error for missing workspace_id")
		}
	})

	t.Run("invalid_dirty_ref", func(t *testing.T) {
		ws := validWS
		ws.DirtyRefs = []EvidenceRef{{Type: "invalid", Ref: "x"}}
		if err := ws.Validate(); err == nil {
			t.Error("expected error for invalid dirty ref")
		}
	})
}

func TestWorkingSet_RoundTrip(t *testing.T) {
	t.Parallel()
	orig := WorkingSet{
		WorkspaceID:     "ws-1",
		DirtyRefs:       []EvidenceRef{{Type: RefTypePath, Ref: "src/main.go"}},
		RecentCommands:  []string{"build", "test"},
		RecentFailures:  []string{"lint"},
		RecentSuccesses: []string{"fmt"},
		UpdatedAt:       time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got WorkingSet
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.WorkspaceID != orig.WorkspaceID {
		t.Errorf("WorkspaceID: got %q, want %q", got.WorkspaceID, orig.WorkspaceID)
	}
	if len(got.DirtyRefs) != len(orig.DirtyRefs) {
		t.Errorf("DirtyRefs: got %d, want %d", len(got.DirtyRefs), len(orig.DirtyRefs))
	}
	if len(got.RecentCommands) != len(orig.RecentCommands) {
		t.Errorf("RecentCommands: got %d, want %d", len(got.RecentCommands), len(orig.RecentCommands))
	}
}

func TestWorkingSet_AddRecentFailure(t *testing.T) {
	t.Parallel()
	ws := &WorkingSet{WorkspaceID: "ws-1", UpdatedAt: time.Now()}

	ws.AddRecentFailure("lint", 3)
	ws.AddRecentFailure("test", 3)
	ws.AddRecentFailure("build", 3)

	if len(ws.RecentFailures) != 3 {
		t.Fatalf("expected 3 failures, got %d", len(ws.RecentFailures))
	}

	ws.AddRecentFailure("lint", 3)
	if len(ws.RecentFailures) != 3 {
		t.Error("expected deduplication to prevent duplicate")
	}

	ws.AddRecentFailure("fmt", 3)
	if len(ws.RecentFailures) != 3 {
		t.Errorf("expected bounded to 3, got %d", len(ws.RecentFailures))
	}
	if ws.RecentFailures[0] != "test" {
		t.Errorf("expected oldest removed, got %q", ws.RecentFailures[0])
	}
}

func TestWorkingSet_AddRecentSuccess(t *testing.T) {
	t.Parallel()
	ws := &WorkingSet{WorkspaceID: "ws-1", UpdatedAt: time.Now()}

	ws.AddRecentSuccess("fmt", 3)
	ws.AddRecentSuccess("vet", 3)
	ws.AddRecentSuccess("docs", 3)

	if len(ws.RecentSuccesses) != 3 {
		t.Fatalf("expected 3 successes, got %d", len(ws.RecentSuccesses))
	}

	ws.AddRecentSuccess("fmt", 3)
	if len(ws.RecentSuccesses) != 3 {
		t.Error("expected deduplication to prevent duplicate")
	}

	ws.AddRecentSuccess("lint", 3)
	if len(ws.RecentSuccesses) != 3 {
		t.Errorf("expected bounded to 3, got %d", len(ws.RecentSuccesses))
	}
	if ws.RecentSuccesses[0] != "vet" {
		t.Errorf("expected oldest removed, got %q", ws.RecentSuccesses[0])
	}
}

func TestTaskContext_Validate(t *testing.T) {
	t.Parallel()
	validTC := TaskContext{
		WorkspaceID: "ws-1",
		TaskID:      "task-1",
		ProjectionMeta: ProjectionMeta{
			ProjectionID:      "proj-1",
			ProjectionType:    "task_context",
			ProjectionVersion: 1,
			WorkspaceID:       "ws-1",
			GeneratedAt:       time.Now(),
		},
		UpdatedAt: time.Now(),
	}

	t.Run("valid", func(t *testing.T) {
		if err := validTC.Validate(); err != nil {
			t.Errorf("expected valid, got %v", err)
		}
	})

	t.Run("missing_workspace_id", func(t *testing.T) {
		tc := validTC
		tc.WorkspaceID = ""
		if err := tc.Validate(); err == nil {
			t.Error("expected error for missing workspace_id")
		}
	})

	t.Run("missing_task_id", func(t *testing.T) {
		tc := validTC
		tc.TaskID = ""
		if err := tc.Validate(); err == nil {
			t.Error("expected error for missing task_id")
		}
	})

	t.Run("invalid_projection_meta", func(t *testing.T) {
		tc := validTC
		tc.ProjectionMeta.ProjectionID = ""
		if err := tc.Validate(); err == nil {
			t.Error("expected error for invalid projection_meta")
		}
	})

	t.Run("invalid_related_code_ref", func(t *testing.T) {
		tc := validTC
		tc.RelatedCodeRefs = []EvidenceRef{{Type: "bad", Ref: "x"}}
		if err := tc.Validate(); err == nil {
			t.Error("expected error for invalid related_code_ref")
		}
	})
}

func TestTaskContext_RoundTrip(t *testing.T) {
	t.Parallel()
	orig := TaskContext{
		WorkspaceID: "ws-1",
		TaskID:      "task-1",
		Objective:   "Implement feature X",
		Status:      "in_progress",
		Scope:       ClaimScope{Path: "src/feature_x/"},
		OpenGaps:    []string{"need tests", "docs incomplete"},
		NextActions: []string{"write tests", "update docs"},
		RelatedCodeRefs: []EvidenceRef{
			{Type: RefTypePath, Ref: "src/feature_x/main.go"},
		},
		ProjectionMeta: ProjectionMeta{
			ProjectionID:      "proj-1",
			ProjectionType:    "task_context",
			ProjectionVersion: 2,
			WorkspaceID:       "ws-1",
			GeneratedAt:       time.Now(),
		},
		UpdatedAt: time.Now(),
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got TaskContext
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.WorkspaceID != orig.WorkspaceID {
		t.Errorf("WorkspaceID: got %q, want %q", got.WorkspaceID, orig.WorkspaceID)
	}
	if got.TaskID != orig.TaskID {
		t.Errorf("TaskID: got %q, want %q", got.TaskID, orig.TaskID)
	}
	if len(got.RelatedCodeRefs) != len(orig.RelatedCodeRefs) {
		t.Errorf("RelatedCodeRefs: got %d, want %d", len(got.RelatedCodeRefs), len(orig.RelatedCodeRefs))
	}
}

func TestContextPacket_Validate(t *testing.T) {
	t.Parallel()
	validCP := ContextPacket{
		WorkspaceID: "ws-1",
		TaskID:      "task-1",
		Objective:   "Get things done",
	}

	t.Run("valid", func(t *testing.T) {
		if err := validCP.Validate(); err != nil {
			t.Errorf("expected valid, got %v", err)
		}
	})

	t.Run("missing_workspace_id", func(t *testing.T) {
		cp := validCP
		cp.WorkspaceID = ""
		if err := cp.Validate(); err == nil {
			t.Error("expected error for missing workspace_id")
		}
	})

	t.Run("invalid_relevant_ref", func(t *testing.T) {
		cp := validCP
		cp.RelevantRefs = []EvidenceRef{{Type: "bad", Ref: "x"}}
		if err := cp.Validate(); err == nil {
			t.Error("expected error for invalid relevant_ref")
		}
	})
}

func TestContextPacket_RoundTrip(t *testing.T) {
	t.Parallel()
	orig := ContextPacket{
		WorkspaceID:     "ws-1",
		TaskID:         "task-1",
		SessionID:      "session-1",
		Objective:      "Complete the implementation",
		Phase:          "implementation",
		HardConstraints: []string{"must use Go 1.21+", "no external deps"},
		Blockers:       []string{"waiting on API spec"},
		RecentDecisions: []RecentDecision{
			{ID: "dec-1", Text: "Use SQLite for storage", Ref: "docs/decisions/001.md"},
		},
		OpenLoops:    []string{"error handling", "test coverage"},
		NextActions:  []string{"write unit tests", "update README"},
		RelevantRefs: []EvidenceRef{{Type: RefTypePath, Ref: "src/main.go"}},
		Metadata:     map[string]any{"priority": "high"},
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got ContextPacket
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.WorkspaceID != orig.WorkspaceID {
		t.Errorf("WorkspaceID: got %q, want %q", got.WorkspaceID, orig.WorkspaceID)
	}
	if got.Objective != orig.Objective {
		t.Errorf("Objective: got %q, want %q", got.Objective, orig.Objective)
	}
	if len(got.RecentDecisions) != len(orig.RecentDecisions) {
		t.Errorf("RecentDecisions: got %d, want %d", len(got.RecentDecisions), len(orig.RecentDecisions))
	}
}

func TestRecentDecision_RoundTrip(t *testing.T) {
	t.Parallel()
	orig := RecentDecision{
		ID:   "dec-1",
		Text: "Use SQLite for storage",
		Ref:  "docs/decisions/001.md",
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got RecentDecision
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.ID != orig.ID {
		t.Errorf("ID: got %q, want %q", got.ID, orig.ID)
	}
	if got.Text != orig.Text {
		t.Errorf("Text: got %q, want %q", got.Text, orig.Text)
	}
}
