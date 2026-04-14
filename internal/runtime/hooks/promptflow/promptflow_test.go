package promptflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/runtime/hooks/sessionmode"
)

func TestDetectAnchorSetsAnchorAndTodoMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspaceRoot := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	called := false
	response, err := DetectAnchor(context.Background(), Dependencies{
		RunSkill: func(ctx context.Context, skill string, input any, workspace string, out any) error {
			if skill != "session/anchor" {
				t.Fatalf("unexpected skill %s", skill)
			}
			called = true
			return nil
		},
		DetectIdentity: func(workspace string) (string, string, string) {
			return "sid-123", "claude", "claude"
		},
	}, AnchorRequest{
		Workspace: workspaceRoot,
		Payload: AnchorPayload{
			Prompt: "/anchor Ship hook cleanup /todo",
		},
	})
	if err != nil {
		t.Fatalf("DetectAnchor: %v", err)
	}
	if !called {
		t.Fatalf("expected session/anchor skill call")
	}
	if !response.AnchorSet || !response.TodoMode {
		t.Fatalf("unexpected response: %#v", response)
	}
	if !strings.Contains(response.Context, "Anchor set") {
		t.Fatalf("context = %q", response.Context)
	}

	flags, err := sessionmode.Read("sid-123", time.Now())
	if err != nil {
		t.Fatalf("sessionmode.Read: %v", err)
	}
	if !flags.Todo || flags.AnchorGoal != "Ship hook cleanup" {
		t.Fatalf("unexpected flags: %#v", flags)
	}
}

func TestDetectAnchorShowsUsageForEmptyGoal(t *testing.T) {
	response, err := DetectAnchor(context.Background(), Dependencies{}, AnchorRequest{
		Workspace: t.TempDir(),
		Payload: AnchorPayload{
			Prompt: "/anchor",
		},
	})
	if err != nil {
		t.Fatalf("DetectAnchor: %v", err)
	}
	if !strings.Contains(response.Context, "Usage: /anchor <goal>") {
		t.Fatalf("context = %q", response.Context)
	}
}

func TestNormalizeAnchorPromptIsCaseInsensitive(t *testing.T) {
	got := normalizeAnchorPrompt("/ANCHOR Ship hook cleanup /TODO")
	if got != "Ship hook cleanup" {
		t.Fatalf("normalizeAnchorPrompt() = %q", got)
	}
}
