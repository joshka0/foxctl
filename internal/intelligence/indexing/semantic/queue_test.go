package semantic

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/platform/workspace"
)

func TestQueueStoreEnqueueClaimAndDedupeSemanticFile(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	workspaceDir := filepath.Join(tmpDir, "workspace")
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspaceDir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := OpenQueueStore(ctx, filepath.Join(tmpDir, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	workspaceID := workspace.ID(workspaceDir)
	req := FileQueueRequest{
		Workspace: workspaceDir,
		JobType:   JobTypeUpdateFiles,
		Args: JobArgs{
			WorkspaceID: workspaceID,
			Files: []JobFileInput{{
				Path:       "main.go",
				ChangeKind: ChangeKindModified,
			}},
			Reason: ReasonManual,
			TaskID: "task-1",
		},
		Provider:     "noop",
		Model:        "test-model",
		ChunkBytes:   32,
		ChunkOverlap: 4,
		ChunkDelay:   10 * time.Millisecond,
	}

	result, err := store.EnqueueFiles(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Queued != 1 || result.Skipped != 0 {
		t.Fatalf("enqueue result=%+v want queued=1 skipped=0", result)
	}
	duplicate, err := store.EnqueueFiles(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.Queued != 0 || duplicate.Skipped != 1 {
		t.Fatalf("duplicate result=%+v want queued=0 skipped=1", duplicate)
	}

	queued, err := store.ClaimNext(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if queued == nil {
		t.Fatal("expected queued job")
	}
	if queued.Payload.JobType != JobTypeUpdateFiles {
		t.Fatalf("job_type=%q", queued.Payload.JobType)
	}
	if queued.Payload.File.Digest == "" {
		t.Fatal("expected file digest")
	}
	if queued.Payload.File.Language != "go" {
		t.Fatalf("language=%q want go", queued.Payload.File.Language)
	}
	if queued.Payload.Task.Scope != string(ScopeFileSummaries) {
		t.Fatalf("task scope=%q want %q", queued.Payload.Task.Scope, ScopeFileSummaries)
	}
	if queued.Payload.ChunkDelay() != 10*time.Millisecond {
		t.Fatalf("chunk_delay=%s", queued.Payload.ChunkDelay())
	}
	if got := queued.Payload.JobArgs(); got.WorkspaceID != workspaceID || len(got.Files) != 1 || got.TaskID != "task-1" {
		t.Fatalf("job args=%+v", got)
	}
}

func TestQueueStoreEnqueueDeletedFileDoesNotStat(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	workspaceDir := filepath.Join(tmpDir, "workspace")
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatal(err)
	}

	store, err := OpenQueueStore(ctx, filepath.Join(tmpDir, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	workspaceID := workspace.ID(workspaceDir)
	result, err := store.EnqueueFiles(ctx, FileQueueRequest{
		Workspace: workspaceDir,
		JobType:   JobTypeUpdateFiles,
		Args: JobArgs{
			WorkspaceID: workspaceID,
			Files: []JobFileInput{{
				Path:       "deleted.go",
				ChangeKind: ChangeKindDeleted,
			}},
			Reason: ReasonManual,
		},
		Model: "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Queued != 1 {
		t.Fatalf("queued=%d want 1", result.Queued)
	}
}

func TestQueueStoreStatsCleanupAndPurgeAreWorkspaceScoped(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	workspaceDir := filepath.Join(tmpDir, "workspace")
	otherDir := filepath.Join(tmpDir, "other")
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{workspaceDir, otherDir} {
		if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	store, err := OpenQueueStore(ctx, filepath.Join(tmpDir, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	workspaceID := workspace.ID(workspaceDir)
	otherID := workspace.ID(otherDir)
	for _, item := range []struct {
		root string
		id   string
	}{
		{root: workspaceDir, id: workspaceID},
		{root: otherDir, id: otherID},
	} {
		_, err := store.EnqueueFiles(ctx, FileQueueRequest{
			Workspace: item.root,
			JobType:   JobTypeUpdateFiles,
			Args: JobArgs{
				WorkspaceID: item.id,
				Files: []JobFileInput{{
					Path:       "main.go",
					ChangeKind: ChangeKindModified,
				}},
				Reason: ReasonManual,
			},
			Model: "test-model",
		})
		if err != nil {
			t.Fatalf("enqueue %s: %v", item.id, err)
		}
	}

	stats, err := store.Stats(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if stats.QueuedCount != 1 {
		t.Fatalf("workspace stats=%+v want one queued job", stats)
	}

	claimed, err := store.ClaimNext(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil {
		t.Fatal("expected workspace job")
	}
	if err := store.Complete(ctx, claimed.ID); err != nil {
		t.Fatal(err)
	}
	deleted, err := store.Cleanup(ctx, workspaceID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("cleanup deleted=%d want 1", deleted)
	}

	deleted, err = store.Purge(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Fatalf("purge workspace deleted=%d want 0 after cleanup", deleted)
	}
	deleted, err = store.Purge(ctx, otherID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("purge other deleted=%d want 1", deleted)
	}
}
