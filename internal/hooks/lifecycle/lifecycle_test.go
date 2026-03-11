package lifecycle

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStartCombinesOrientationRestoreAndAnchor(t *testing.T) {
	storageRoot := t.TempDir()
	workspaceRoot := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	deps := Dependencies{
		StorageRoot: storageRoot,
		EnsureDaemon: func(ctx context.Context, workspace string) bool {
			return true
		},
		WarmWorkspace: func(ctx context.Context, workspace string) {},
		DetectIdentity: func(workspace string) (string, string, string) {
			return "sid-123", "claude", "claude"
		},
		RunSkill: func(ctx context.Context, skill string, input any, workspace string, out any) error {
			switch skill {
			case "session/restore":
				target := out.(*sessionRestoreEnvelope)
				target.Data.HookOutput.Context = "Restored context"
			case "session/anchor":
				target := out.(*sessionAnchorEnvelope)
				target.Data.Found = true
				target.Data.Anchor.MainPrompt = "Ship hook lifecycle cleanup"
				target.Data.Anchor.PendingQuestion = "What is left in SessionEnd?"
			default:
				t.Fatalf("unexpected skill %s", skill)
			}
			return nil
		},
	}

	response, err := Start(context.Background(), deps, StartRequest{
		Workspace: workspaceRoot,
		Source:    "resume",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !response.DaemonReady {
		t.Fatalf("expected daemon ready")
	}
	if !strings.Contains(response.Context, "Latest Orientation") {
		t.Fatalf("expected orientation context, got %q", response.Context)
	}
	if !strings.Contains(response.Context, "Restored context") {
		t.Fatalf("expected restore context, got %q", response.Context)
	}
	if !strings.Contains(response.Context, "Ship hook lifecycle cleanup") {
		t.Fatalf("expected anchor context, got %q", response.Context)
	}
}

func TestEndCapturesHandoffAndWritesSummary(t *testing.T) {
	t.Setenv("CEREBRAS_API_KEY", "test-key")
	storageRoot := t.TempDir()
	workspaceRoot := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	deps := Dependencies{
		StorageRoot: storageRoot,
		RunSkill: func(ctx context.Context, skill string, input any, workspace string, out any) error {
			switch skill {
			case "session/capture":
				target := out.(*sessionCaptureEnvelope)
				target.Data.SessionID = "sid-123"
				target.Data.Status = "captured"
			case "session/summarize":
				target := out.(*sessionSummarizeEnvelope)
				target.Data.UserPreferences = []string{"Prefer concise hook wrappers"}
			default:
				t.Fatalf("unexpected skill %s", skill)
			}
			return nil
		},
		AppendSummary: func(workspace string, prefs, gotchas, timeSinks []string) bool {
			return len(prefs) == 1 && prefs[0] == "Prefer concise hook wrappers"
		},
	}

	response, err := End(context.Background(), deps, EndRequest{
		Workspace: workspaceRoot,
		Payload: EndPayload{
			AssistantText: "Finished hook session-end extraction.",
		},
	})
	if err != nil {
		t.Fatalf("End: %v", err)
	}
	if response.CaptureStatus != "captured" {
		t.Fatalf("capture status = %q", response.CaptureStatus)
	}
	if response.HandoffPath == "" {
		t.Fatalf("expected handoff path")
	}
	if !response.SummaryWritten {
		t.Fatalf("expected summary written")
	}
}

func TestRestorePostcompactConsumesMarker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspaceRoot := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	markerPath := pendingRestoreMarkerPath(workspaceRoot)
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		t.Fatalf("mkdir marker dir: %v", err)
	}
	if err := os.WriteFile(markerPath, []byte(`{"session_id":"sid-123"}`), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	deps := Dependencies{
		RunSkill: func(ctx context.Context, skill string, input any, workspace string, out any) error {
			if skill != "session/restore" {
				t.Fatalf("unexpected skill %s", skill)
			}
			target := out.(*sessionRestoreEnvelope)
			target.Data.HookOutput.Context = "Recovered post-compact context"
			return nil
		},
	}

	response, err := RestorePostcompact(context.Background(), deps, PostcompactRestoreRequest{
		Workspace: workspaceRoot,
	})
	if err != nil {
		t.Fatalf("RestorePostcompact: %v", err)
	}
	if response.Decision != "approve" {
		t.Fatalf("decision = %q", response.Decision)
	}
	if !strings.Contains(response.Context, "Recovered post-compact context") {
		t.Fatalf("context = %q", response.Context)
	}
	if _, err := os.Stat(markerPath); err == nil {
		t.Fatalf("expected marker to be deleted")
	}
}
