package inboxflow

import (
	"context"
	"testing"

	"github.com/joshka0/foxctl/internal/runtime/hooks/lifecycle"
	"github.com/joshka0/foxctl/internal/storage/contextbuffer"
)

func TestReadInboxPreTool(t *testing.T) {
	deps := Dependencies{
		RunSkill: func(ctx context.Context, skill string, input any, workspace string, out any) error {
			if skill != "hooks/overseer_inbox" {
				t.Fatalf("unexpected skill %s", skill)
			}
			target := out.(*overseerInboxEnvelope)
			target.Data.HookOutput.Context = "Overseer wants priority change"
			return nil
		},
		DetectIdentity: func(workspace string) (string, string, string) {
			return "sid-123", "claude", "claude"
		},
	}
	response, err := ReadInboxPreTool(context.Background(), deps, PreToolRequest{
		Workspace: "/tmp/workspace",
	})
	if err != nil {
		t.Fatalf("ReadInboxPreTool: %v", err)
	}
	if response.Decision != "approve" {
		t.Fatalf("decision = %q", response.Decision)
	}
	if response.Context == "" {
		t.Fatalf("expected context")
	}
}

func TestReadInboxPostToolEnqueuesContext(t *testing.T) {
	storageRoot := t.TempDir()
	workspaceRoot := t.TempDir()
	deps := Dependencies{
		StorageRoot: storageRoot,
		RunSkill: func(ctx context.Context, skill string, input any, workspace string, out any) error {
			target := out.(*overseerInboxEnvelope)
			target.Data.HookOutput.Context = "Overseer follow-up"
			return nil
		},
		DetectIdentity: func(workspace string) (string, string, string) {
			return "sid-123", "claude", "claude"
		},
	}
	response, err := ReadInboxPostTool(context.Background(), deps, PostToolRequest{
		Workspace: workspaceRoot,
	})
	if err != nil {
		t.Fatalf("ReadInboxPostTool: %v", err)
	}
	if !response.Enqueued {
		t.Fatalf("expected enqueue")
	}
	store, err := contextbuffer.Open(context.Background(), storageRoot)
	if err != nil {
		t.Fatalf("open contextbuffer: %v", err)
	}
	defer store.Close()
	result, err := store.Drain(context.Background(), contextbuffer.DrainParams{
		WorkspaceID:  workspaceRoot,
		SessionID:    "sid-123",
		Sources:      []string{"Overseer Messages"},
		Limit:        10,
		MarkConsumed: true,
	})
	if err != nil {
		t.Fatalf("drain contextbuffer: %v", err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result.Entries))
	}
}

func TestNewDependenciesCopiesLifecycleDeps(t *testing.T) {
	life := lifecycle.Dependencies{StorageRoot: "/tmp/storage"}
	deps := NewDependencies(life)
	if deps.StorageRoot != life.StorageRoot {
		t.Fatalf("storage root = %q", deps.StorageRoot)
	}
}

func TestReadInboxPostToolPreservesContextWhenBufferUnavailable(t *testing.T) {
	deps := Dependencies{
		StorageRoot: "",
		RunSkill: func(ctx context.Context, skill string, input any, workspace string, out any) error {
			target := out.(*overseerInboxEnvelope)
			target.Data.HookOutput.Context = "Overseer follow-up"
			return nil
		},
		DetectIdentity: func(workspace string) (string, string, string) {
			return "sid-123", "claude", "claude"
		},
	}
	response, err := ReadInboxPostTool(context.Background(), deps, PostToolRequest{
		Workspace: "/tmp/workspace",
	})
	if err != nil {
		t.Fatalf("ReadInboxPostTool: %v", err)
	}
	if response.Context != "Overseer follow-up" {
		t.Fatalf("context = %q", response.Context)
	}
	if response.Enqueued {
		t.Fatalf("expected enqueue to fail-open without storage root")
	}
}
