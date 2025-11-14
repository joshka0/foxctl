package persist

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/jobs/types"
)

func TestInsertAndGetJob(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

	now := time.Now().UTC()
	job := types.Job{
		ID:        "job-1",
		Command:   "test",
		ArgsJSON:  "{}",
		ArgsHash:  "hash",
		State:     types.StateQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.InsertJob(ctx, job); err != nil {
		t.Fatalf("insert: %v", err)
	}
	stored, err := store.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Command != job.Command {
		t.Fatalf("expected command %s got %s", job.Command, stored.Command)
	}
}

func TestUpdateStateValidation(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

	now := time.Now().UTC()
	job := types.Job{
		ID:        "job-1",
		Command:   "test",
		ArgsJSON:  "{}",
		ArgsHash:  "hash",
		State:     types.StateQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.InsertJob(ctx, job); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := store.UpdateState(ctx, job.ID, types.StateOK, "", ""); err == nil {
		t.Fatalf("expected invalid transition error")
	} else if !errors.Is(err, types.ErrInvalidState) {
		t.Fatalf("expected ErrInvalidState, got %v", err)
	}
}

func TestFindOrInsertJobDedupes(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

	now := time.Now().UTC()
	job := types.Job{
		ID:        "job-1",
		Command:   "test",
		ArgsJSON:  "{}",
		ArgsHash:  "hash",
		State:     types.StateQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}
	inserted, dup, err := store.FindOrInsertJob(ctx, job)
	if err != nil {
		t.Fatalf("find or insert: %v", err)
	}
	if dup {
		t.Fatalf("expected first insert to not be duplicate")
	}
	if inserted.ID == "" {
		t.Fatalf("expected job id")
	}
	_, dup, err = store.FindOrInsertJob(ctx, job)
	if err != nil {
		t.Fatalf("second find or insert: %v", err)
	}
	if !dup {
		t.Fatalf("expected duplicate on second call")
	}
}

func TestRecoverOrphanedJobs(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

	now := time.Now().UTC()
	job := types.Job{
		ID:        "job-1",
		Command:   "test",
		ArgsJSON:  "{}",
		ArgsHash:  "hash",
		State:     types.StateQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.InsertJob(ctx, job); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := store.UpdateState(ctx, job.ID, types.StateRunning, "", ""); err != nil {
		t.Fatalf("set running: %v", err)
	}

	recovered, err := store.RecoverOrphanedJobs(ctx)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("expected 1 recovered job, got %d", recovered)
	}
	stored, err := store.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.State != types.StateError {
		t.Fatalf("expected error state, got %s", stored.State)
	}
}
