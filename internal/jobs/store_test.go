package jobs

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestSubmitEchoCreatesResult(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t, root)
	defer func() { _ = store.Close() }()

	job, err := store.SubmitEcho(ctx, "hello world")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if job.State != StateOK {
		t.Fatalf("expected state ok got %s", job.State)
	}
	if job.ResultPath == "" {
		t.Fatalf("expected result path to be set")
	}
	data, err := os.ReadFile(job.ResultPath)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	var env map[string]any
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if env["status"] != "ok" {
		t.Fatalf("expected ok envelope, got %v", env["status"])
	}
}

func TestListJobsOrder(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t, root)
	defer func() { _ = store.Close() }()

	if _, err := store.SubmitEcho(ctx, "first"); err != nil {
		t.Fatalf("submit first: %v", err)
	}
	if _, err := store.SubmitEcho(ctx, "second"); err != nil {
		t.Fatalf("submit second: %v", err)
	}

	jobs, err := store.List(ctx, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs got %d", len(jobs))
	}
	if jobs[0].CreatedAt.Before(jobs[1].CreatedAt) {
		t.Fatalf("expected newest job first")
	}
}

func TestCancelRequiresPendingState(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t, root)
	defer func() { _ = store.Close() }()

	job, err := store.SubmitEcho(ctx, "done")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if err := store.Cancel(ctx, job.ID); err == nil {
		t.Fatalf("expected cancel to fail on completed job")
	}
}

func TestResultReadsFile(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t, root)
	defer func() { _ = store.Close() }()

	job, err := store.SubmitEcho(ctx, "result test")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	data, err := store.Result(ctx, job.ID)
	if err != nil {
		t.Fatalf("result: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("expected data")
	}
}

func TestJobDirectoriesCreated(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t, root)
	defer func() { _ = store.Close() }()

	job, err := store.SubmitEcho(ctx, "dirs")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	dir := filepath.Join(root, job.ID)
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("expected job dir: %v", err)
	}
}

func TestRecoverOrphanedJobs(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t, root)

	job, _, err := store.FindOrPrepareSkillJob(ctx, "test", []byte(`{"input":"value"}`), false)
	if err != nil {
		t.Fatalf("prepare job: %v", err)
	}

	if _, err := store.db.ExecContext(ctx, `UPDATE jobs SET state = ? WHERE id = ?`, StateRunning, job.ID); err != nil {
		t.Fatalf("force running state: %v", err)
	}

	_ = store.Close()

	store = openTestStore(t, root)
	defer func() { _ = store.Close() }()

	recoveredCount, err := store.RecoverOrphanedJobs(ctx)
	if err != nil {
		t.Fatalf("recover orphaned jobs: %v", err)
	}
	if recoveredCount != 1 {
		t.Fatalf("expected 1 recovered job got %d", recoveredCount)
	}

	recovered, err := store.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if recovered.State != StateError {
		t.Fatalf("expected error state, got %s", recovered.State)
	}
}

func TestComputeSkillArgsHash(t *testing.T) {
	root := t.TempDir()
	store := openTestStore(t, root)
	defer func() { _ = store.Close() }()

	input := []byte(`{"input":"value"}`)
	hash1 := store.ComputeSkillArgsHash("test", input)

	job, _, err := store.FindOrPrepareSkillJob(context.Background(), "test", input, false)
	if err != nil {
		t.Fatalf("prepare job: %v", err)
	}

	if hash1 != job.ArgsHash {
		t.Fatalf("expected hash %s, got %s", hash1, job.ArgsHash)
	}

	hash2 := store.ComputeSkillArgsHash("test", []byte(`{"input":"different"}`))
	if hash1 == hash2 {
		t.Fatalf("expected different hashes for different inputs")
	}
}

func TestFindDuplicateJob(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t, root)
	defer func() { _ = store.Close() }()

	job1, _, err := store.FindOrPrepareSkillJob(ctx, "test", []byte(`{"input":"value"}`), false)
	if err != nil {
		t.Fatalf("prepare job1: %v", err)
	}

	dup, err := store.FindDuplicateJob(ctx, job1.ArgsHash)
	if err != nil {
		t.Fatalf("find duplicate: %v", err)
	}
	if dup.ID != job1.ID {
		t.Fatalf("expected same job ID, got %s != %s", dup.ID, job1.ID)
	}

	if _, err := store.FindDuplicateJob(ctx, "missing"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestFindOrPrepareSkillJobDedupes(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t, root)
	defer func() { _ = store.Close() }()

	job1, dup, err := store.FindOrPrepareSkillJob(ctx, "test", []byte(`{"input":"value"}`), true)
	if err != nil {
		t.Fatalf("first job: %v", err)
	}
	if dup {
		t.Fatalf("expected new job on first call")
	}

	job2, dup, err := store.FindOrPrepareSkillJob(ctx, "test", []byte(`{"input":"value"}`), true)
	if err != nil {
		t.Fatalf("second job: %v", err)
	}
	if !dup {
		t.Fatalf("expected duplicate flag on second call")
	}
	if job1.ID != job2.ID {
		t.Fatalf("expected matching job IDs")
	}
}

func TestWaitForCompletion(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t, root)
	defer func() { _ = store.Close() }()

	job, err := store.SubmitEcho(ctx, "wait test")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	finalJob, err := store.WaitForCompletion(ctx, job.ID, 0)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if finalJob.State != StateOK {
		t.Fatalf("expected ok state, got %s", finalJob.State)
	}
}

func TestProgressReader(t *testing.T) {
	root := t.TempDir()
	jobDir := filepath.Join(root, "test-job")
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	pw, err := NewProgressWriter(jobDir)
	if err != nil {
		t.Fatalf("new progress writer: %v", err)
	}
	_ = pw.WriteMessage("first event")
	_ = pw.WritePercent(50, "halfway")
	_ = pw.WriteMessage("final event")
	_ = pw.Close()

	pr, err := OpenProgressReader(jobDir)
	if err != nil {
		t.Fatalf("open progress reader: %v", err)
	}
	defer func() { _ = pr.Close() }()

	events := []ProgressEvent{}
	for {
		event, err := pr.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("read event: %v", err)
		}
		events = append(events, event)
	}

	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	if events[0].Message != "first event" {
		t.Fatalf("unexpected first message: %s", events[0].Message)
	}
	if events[1].Percent != 50 {
		t.Fatalf("expected percent 50, got %f", events[1].Percent)
	}
}

func openTestStore(t *testing.T, root string) *Store {
	t.Helper()
	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return store
}
