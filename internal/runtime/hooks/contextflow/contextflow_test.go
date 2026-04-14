package contextflow

import (
	"context"
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/storage/contextbuffer"
)

func TestDrainUpdaterContext(t *testing.T) {
	storageRoot := t.TempDir()
	store, err := contextbuffer.Open(context.Background(), storageRoot)
	if err != nil {
		t.Fatalf("open contextbuffer: %v", err)
	}
	defer store.Close()

	_, err = store.Enqueue(context.Background(), contextbuffer.EnqueueParams{
		WorkspaceID: "/tmp/workspace",
		SessionID:   "sid-123",
		Source:      "context-updater",
		Text:        "Relevant memory block",
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	response, err := DrainUpdaterContext(context.Background(), Dependencies{
		StorageRoot: storageRoot,
	}, DrainRequest{
		Workspace: "/tmp/workspace",
		SessionID: "sid-123",
	})
	if err != nil {
		t.Fatalf("DrainUpdaterContext: %v", err)
	}
	if response.Decision != "approve" {
		t.Fatalf("decision = %q", response.Decision)
	}
	if response.Count != 1 {
		t.Fatalf("count = %d", response.Count)
	}
	if !strings.Contains(response.Context, "Relevant memory block") {
		t.Fatalf("context = %q", response.Context)
	}

	remaining, err := store.Count(context.Background(), "/tmp/workspace", "sid-123")
	if err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("expected consumed entries, remaining=%d", remaining)
	}
}
