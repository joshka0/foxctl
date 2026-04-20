package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadShellStateUsesDefaultWithoutEpicID(t *testing.T) {
	state, err := LoadShellState(Options{})
	if err != nil {
		t.Fatalf("LoadShellState returned error: %v", err)
	}
	if state.EpicTitle != "Go TUI Agent Shell" {
		t.Fatalf("EpicTitle = %q, want default title", state.EpicTitle)
	}
}

func TestLoadShellStateRequiresExistingEpicMetaWhenEpicIDProvided(t *testing.T) {
	opts := Options{
		EpicID:   "missing-epic",
		EpicsDir: t.TempDir(),
	}

	_, err := LoadShellState(opts)
	if err == nil {
		t.Fatal("LoadShellState error = nil, want missing epic error")
	}
	if !strings.Contains(err.Error(), "missing-epic") {
		t.Fatalf("error %q does not include epic ID", err)
	}
	if !strings.Contains(err.Error(), "--epics-dir") {
		t.Fatalf("error %q does not include remediation hint", err)
	}
}

func TestLoadShellStateRejectsPathLikeEpicID(t *testing.T) {
	_, err := LoadShellState(Options{
		EpicID:   "../escape",
		EpicsDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("LoadShellState error = nil, want path-like epic id rejection")
	}
	if !strings.Contains(err.Error(), "must be a single directory name") {
		t.Fatalf("error %q does not explain path-like epic id rejection", err)
	}
}

func TestLoadShellStateDerivesTitleAndStatusFromRealMirrorShape(t *testing.T) {
	epicsDir := t.TempDir()
	epicID := "epic-real-shape"
	writeEpicMetaFile(t, epicsDir, epicID, `{
  "epic": {
    "id": "epic-real-shape",
    "finalized": true,
    "final_brief": {
      "workspace_id": "/tmp/real-workspace",
      "subject": "Epic Finalized: Go TUI Agent Shell",
      "body": "Ready epic brief"
    },
    "meta": {
      "goal": "Imported from docs/plans/go-tui-agent-shell.md",
      "outcome": "Build a shell from a ready implementation epic."
    },
    "milestone_count": 1,
    "story_count": 2,
    "log_count": 0
  }
}`)

	state, err := LoadShellState(Options{
		EpicID:   epicID,
		EpicsDir: epicsDir,
	})
	if err != nil {
		t.Fatalf("LoadShellState error: %v", err)
	}
	if state.EpicTitle != "Go TUI Agent Shell" {
		t.Fatalf("EpicTitle = %q, want derived final-brief title", state.EpicTitle)
	}
	if state.EpicStatus != "finalized" {
		t.Fatalf("EpicStatus = %q, want finalized", state.EpicStatus)
	}
	if state.Workspace != "/tmp/real-workspace" {
		t.Fatalf("Workspace = %q, want final brief workspace", state.Workspace)
	}
}

func TestLoadShellStateEpicMirrorMapping(t *testing.T) {
	tests := []struct {
		name          string
		workspaceFlag string
		wantWorkspace string
	}{
		{
			name:          "uses epic workspace when workspace flag is empty",
			workspaceFlag: "",
			wantWorkspace: "/tmp/epic-workspace",
		},
		{
			name:          "workspace flag overrides epic workspace",
			workspaceFlag: "/tmp/override-workspace",
			wantWorkspace: "/tmp/override-workspace",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			epicsDir := t.TempDir()
			epicID := "epic-ready-123"
			writeEpicMetaFile(t, epicsDir, epicID, `{
  "epic": {
    "id": "epic-ready-123",
    "title": "TUI slice epic",
    "status": "finalized",
    "root": {
      "workspace_id": "/tmp/epic-workspace",
      "subject": "Epic: TUI slice epic"
    },
    "final_brief": {
      "subject": "Epic Finalized: TUI slice epic",
      "body": "Finalized after milestone contracts, story planning, and dispatch-ready checks."
    },
    "meta": {
      "goal": "Ship the go-tui shell epic mirror adapter.",
      "outcome": "Implementation-ready adapter with deterministic parsing."
    },
    "milestone_count": 2,
    "story_count": 5,
    "question_count": 3,
    "open_questions": 1,
    "quiet_milestone_count": 1,
    "quiet_story_count": 2,
    "log_count": 2,
    "logs": [
      {
        "label": "adapter-slice-complete",
        "meta": {
          "completed": ["wired flags", "added adapter"],
          "next_focus": ["hook live api adapter", "add stream transport"],
          "notes": "ready for next slice"
        }
      },
      {
        "label": "older-log",
        "meta": {
          "completed": "added shell defaults",
          "notes": "baseline complete"
        }
      }
    ]
  }
}`)

			state, err := LoadShellState(Options{
				Workspace: tc.workspaceFlag,
				EpicID:    epicID,
				EpicsDir:  epicsDir,
			})
			if err != nil {
				t.Fatalf("LoadShellState error: %v", err)
			}

			if state.Workspace != tc.wantWorkspace {
				t.Fatalf("Workspace = %q, want %q", state.Workspace, tc.wantWorkspace)
			}
			if state.EpicTitle != "TUI slice epic" {
				t.Fatalf("EpicTitle = %q, want %q", state.EpicTitle, "TUI slice epic")
			}
			if state.EpicStatus != "finalized" {
				t.Fatalf("EpicStatus = %q, want %q", state.EpicStatus, "finalized")
			}

			if state.Continuity.EpicID != epicID {
				t.Fatalf("Continuity.EpicID = %q, want %q", state.Continuity.EpicID, epicID)
			}
			if state.Continuity.Status != "finalized" {
				t.Fatalf("Continuity.Status = %q, want %q", state.Continuity.Status, "finalized")
			}
			if got := state.Continuity.Boundary; !strings.Contains(got, "deterministic parsing") {
				t.Fatalf("Continuity.Boundary = %q, want outcome-derived boundary", got)
			}
			if got := state.Continuity.Next; !strings.Contains(got, "hook live api adapter") {
				t.Fatalf("Continuity.Next = %q, want next_focus text", got)
			}

			if len(state.Memory) == 0 {
				t.Fatal("Memory is empty, want final brief/log summaries")
			}
			if state.Memory[0].Title != "Final Brief" {
				t.Fatalf("Memory[0].Title = %q, want %q", state.Memory[0].Title, "Final Brief")
			}
			if got := state.Memory[0].Summary; !strings.Contains(got, "Finalized after milestone contracts") {
				t.Fatalf("Memory[0].Summary = %q, want final brief body", got)
			}

			if len(state.Transcript) < 3 {
				t.Fatalf("Transcript length = %d, want at least 3 entries", len(state.Transcript))
			}
			if got := state.Transcript[1].Text; !strings.Contains(got, "Milestones=2") || !strings.Contains(got, "Stories=5") {
				t.Fatalf("Transcript counts entry = %q, want milestone/story counts", got)
			}

			if len(state.Workers) != 3 {
				t.Fatalf("Workers length = %d, want 3", len(state.Workers))
			}
			if got := state.Workers[0].Status; got != "2 total" {
				t.Fatalf("Workers[0].Status = %q, want %q", got, "2 total")
			}
			if got := state.Workers[1].Status; got != "5 total" {
				t.Fatalf("Workers[1].Status = %q, want %q", got, "5 total")
			}
			if got := state.Workers[2].Status; got != "1 open" {
				t.Fatalf("Workers[2].Status = %q, want %q", got, "1 open")
			}
		})
	}
}

func writeEpicMetaFile(t *testing.T, epicsDir, epicID, payload string) {
	t.Helper()
	epicDir := filepath.Join(epicsDir, epicID)
	if err := os.MkdirAll(epicDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error: %v", epicDir, err)
	}
	metaPath := filepath.Join(epicDir, "meta.json")
	if err := os.WriteFile(metaPath, []byte(payload), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error: %v", metaPath, err)
	}
}
