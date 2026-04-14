package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage/cas"
	"github.com/joshka0/foxctl/internal/storage/tasks"
)

func TestRun_ScopesBySessionIDAndCountsUnscoped(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()

	cfg := config.Config{
		Home:           tmp,
		InlineOutputKB: 100,
		MaxCaptureKB:   1024,
		Paths: config.Paths{
			CAS:   filepath.Join(tmp, "cas"),
			Jobs:  filepath.Join(tmp, "jobs"),
			Cache: filepath.Join(tmp, "cache"),
		},
		Storage: config.StorageSettings{Root: filepath.Join(tmp, "storage")},
	}
	store, err := tasks.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	casStore, err := cas.NewStore(cfg.Paths.CAS)
	if err != nil {
		t.Fatalf("cas store: %v", err)
	}

	ws := "test-workspace"

	if _, err := store.Add(ctx, tasks.Task{WorkspaceID: ws, Title: "Unscoped", Status: "pending"}); err != nil {
		t.Fatalf("add unscoped: %v", err)
	}
	if _, err := store.Add(ctx, tasks.Task{WorkspaceID: ws, Title: "A1", Status: "pending", SessionID: "session-a"}); err != nil {
		t.Fatalf("add session-a: %v", err)
	}
	if _, err := store.Add(ctx, tasks.Task{WorkspaceID: ws, Title: "B1", Status: "pending", SessionID: "session-b"}); err != nil {
		t.Fatalf("add session-b: %v", err)
	}
	if _, err := store.Add(ctx, tasks.Task{WorkspaceID: ws, Title: "Done", Status: "completed", SessionID: "session-a"}); err != nil {
		t.Fatalf("add completed: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := &skillmain.RunContext{
		Config:    cfg,
		CASStore:  casStore,
		Stores:    skillmain.NewStoreProvider(cfg),
		Workspace: ws,
		SessionID: "session-a",
		Stdout:    buf,
	}

	in := input{WorkspaceID: ws, SessionID: "session-a", TopN: 5, MinPending: 1}
	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data")
	}

	if sid, _ := data["session_id"].(string); sid != "session-a" {
		t.Fatalf("session_id = %q, want session-a", sid)
	}
	if count, _ := data["incomplete_count"].(float64); int(count) != 1 {
		t.Fatalf("incomplete_count = %d, want 1", int(count))
	}
	if count, _ := data["unscoped_incomplete_count"].(float64); int(count) != 1 {
		t.Fatalf("unscoped_incomplete_count = %d, want 1", int(count))
	}
}
