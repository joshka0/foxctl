package persist

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/storage/jobs/types"
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

func TestIsFilesystemAccessError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "readonly", err: errors.New("attempt to write a readonly database"), want: true},
		{name: "permission", err: errors.New("open /path/jobs.db: permission denied"), want: true},
		{name: "operation not permitted", err: errors.New("open /path/jobs.db: operation not permitted"), want: true},
		{name: "sqlite unable open", err: errors.New("sqliteutil: check journal_mode: unable to open database file (14)"), want: true},
		{name: "wrapped", err: fmt.Errorf("jobs: open db: %w", errors.New("read-only file system")), want: true},
		{name: "migration", err: errors.New("jobs: migrate: syntax error"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isFilesystemAccessError(tt.err); got != tt.want {
				t.Fatalf("isFilesystemAccessError()=%v want %v", got, tt.want)
			}
		})
	}
}
