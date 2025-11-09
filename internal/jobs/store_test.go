package jobs

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubmitEchoCreatesResult(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
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
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
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
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
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
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
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
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
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

func TestRunSkillCreatesJobAndResult(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = store.Close() }()

	bin := buildTestSkill(t, `package main
import (
  "encoding/json"
  "fmt"
  "os"
)
func main() {
  var payload map[string]any
  json.NewDecoder(os.Stdin).Decode(&payload)
  fmt.Println("{\"version\":1,\"status\":\"ok\",\"command\":\"test\",\"data\":{\"message\":\"skill\"},\"meta\":{\"ts\":\"2025-01-01T00:00:00Z\"},\"error\":{}}")
}`)
	input := []byte(`{"foo":"bar"}`)
	job, result, err := store.RunSkill(ctx, "test/skill", bin, input)
	if err != nil {
		t.Fatalf("run skill: %v", err)
	}
	if job.State != StateOK {
		t.Fatalf("expected state ok got %s", job.State)
	}
	if len(result) == 0 {
		t.Fatalf("expected result bytes")
	}
}

func TestProgressStreamingWrites(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = store.Close() }()

	bin := buildTestSkill(t, `package main
import (
  "encoding/json"
  "fmt"
  "os"
  "time"
)
func main() {
  time.Sleep(100 * time.Millisecond)
  var payload map[string]any
  json.NewDecoder(os.Stdin).Decode(&payload)
  fmt.Println("{\"version\":1,\"status\":\"ok\",\"command\":\"test\",\"data\":{\"message\":\"skill\"},\"meta\":{\"ts\":\"2025-01-01T00:00:00Z\"},\"error\":{}}")
}`)
	input := []byte(`{"foo":"bar"}`)
	job, _, err := store.RunSkill(ctx, "test/skill", bin, input)
	if err != nil {
		t.Fatalf("run skill: %v", err)
	}

	// Check progress file exists
	progressPath := filepath.Join(root, job.ID, "progress.ndjson")
	if _, err := os.Stat(progressPath); err != nil {
		t.Fatalf("expected progress file: %v", err)
	}

	// Read progress events
	data, err := os.ReadFile(progressPath)
	if err != nil {
		t.Fatalf("read progress: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("expected progress events")
	}

	// Verify at least one event can be parsed
	var event ProgressEvent
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) > 0 && lines[0] != "" {
		if err := json.Unmarshal([]byte(lines[0]), &event); err == nil {
			if event.Timestamp.IsZero() {
				t.Fatalf("expected timestamp in progress event")
			}
		}
	}
}

func TestCrashRecoveryMarksOrphanedJobs(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	// Create a store and insert a running job directly
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	// Create a job in running state
	job, err := store.prepareSkillJob(ctx, "test", []byte(`{}`))
	if err != nil {
		t.Fatalf("prepare job: %v", err)
	}
	if err := store.updateState(ctx, job.ID, StateRunning, "", ""); err != nil {
		t.Fatalf("set running: %v", err)
	}

	// Close and reopen store (simulating restart)
	_ = store.Close()

	store, err = Open(ctx, root)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Explicitly call RecoverOrphanedJobs (simulating worker startup)
	if err := store.RecoverOrphanedJobs(ctx); err != nil {
		t.Fatalf("recover orphaned jobs: %v", err)
	}

	// Job should now be in error state
	recovered, err := store.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if recovered.State != StateError {
		t.Fatalf("expected error state, got %s", recovered.State)
	}
	if recovered.Error != "ERUNTIME_RESTART: process restarted" {
		t.Fatalf("unexpected error message: %s", recovered.Error)
	}
}

func TestComputeSkillArgsHash(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Compute hash before creating job
	input := []byte(`{"input":"value"}`)
	hash1 := store.ComputeSkillArgsHash("test", input)

	// Create job with same parameters
	job, err := store.prepareSkillJob(ctx, "test", input)
	if err != nil {
		t.Fatalf("prepare job: %v", err)
	}

	// Hashes should match
	if hash1 != job.ArgsHash {
		t.Fatalf("expected hash %s, got %s", hash1, job.ArgsHash)
	}

	// Different input should produce different hash
	hash2 := store.ComputeSkillArgsHash("test", []byte(`{"input":"different"}`))
	if hash1 == hash2 {
		t.Fatalf("expected different hashes for different inputs")
	}
}

func TestFindDuplicateJob(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create first job
	job1, err := store.prepareSkillJob(ctx, "test", []byte(`{"input":"value"}`))
	if err != nil {
		t.Fatalf("prepare job1: %v", err)
	}

	// Find duplicate (should return the same job)
	dup, err := store.FindDuplicateJob(ctx, job1.ArgsHash)
	if err != nil {
		t.Fatalf("find duplicate: %v", err)
	}
	if dup.ID != job1.ID {
		t.Fatalf("expected same job ID, got %s != %s", dup.ID, job1.ID)
	}

	// Create second job with different input
	job2, err := store.prepareSkillJob(ctx, "test", []byte(`{"input":"different"}`))
	if err != nil {
		t.Fatalf("prepare job2: %v", err)
	}

	// Should find job2 with its hash
	dup2, err := store.FindDuplicateJob(ctx, job2.ArgsHash)
	if err != nil {
		t.Fatalf("find duplicate2: %v", err)
	}
	if dup2.ID != job2.ID {
		t.Fatalf("expected job2 ID, got %s != %s", dup2.ID, job2.ID)
	}

	// Should not find non-existent hash
	if _, err := store.FindDuplicateJob(ctx, "nonexistent"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestWaitForCompletion(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Submit echo job (completes immediately)
	job, err := store.SubmitEcho(ctx, "wait test")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	// Wait should return immediately since job is already done
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

	// Write some progress events
	pw, err := NewProgressWriter(jobDir)
	if err != nil {
		t.Fatalf("new progress writer: %v", err)
	}
	_ = pw.WriteMessage("first event")
	_ = pw.WritePercent(50, "halfway")
	_ = pw.WriteMessage("final event")
	_ = pw.Close()

	// Read back events
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

func buildTestSkill(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	binPath := filepath.Join(dir, "skill")
	cmd := exec.Command("go", "build", "-o", binPath, srcPath)
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build skill: %v\n%s", err, out)
	}
	return binPath
}
