package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/jobs/types"
	"github.com/jkatigb/agentctl/internal/skill"
)

func TestSubmitEchoCreatesResult(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

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

func TestOpenCreatesNestedRoot(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	root := filepath.Join(base, "nested", "jobs")
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

	if _, err := os.Stat(root); err != nil {
		t.Fatalf("expected root directory to exist: %v", err)
	}
}

func TestListJobsOrder(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

	if _, err := store.SubmitEcho(ctx, "first"); err != nil {
		t.Fatalf("submit first: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
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
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

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
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

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
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

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
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

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
}
`)
	input := []byte(`{"foo":"bar"}`)
	manifest := testExecManifest("test/skill")
	job, result, err := store.RunSkill(ctx, manifest, bin, input)
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
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

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
}
`)
	input := []byte(`{"foo":"bar"}`)
	manifest := testExecManifest("test/skill")
	job, _, err := store.RunSkill(ctx, manifest, bin, input)
	if err != nil {
		t.Fatalf("run skill: %v", err)
	}

	progressPath := filepath.Join(root, job.ID, "progress.ndjson")
	if _, err := os.Stat(progressPath); err != nil {
		t.Fatalf("expected progress file: %v", err)
	}
	data, err := os.ReadFile(progressPath)
	if err != nil {
		t.Fatalf("read progress: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("expected progress events")
	}
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

func TestFindOrPrepareSkillJobDedupes(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

	input := []byte(`{"foo":"bar"}`)
	job1, dup1, err := store.FindOrPrepareSkillJob(ctx, "test", input, true)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if dup1 {
		t.Fatalf("expected first job not to be duplicate")
	}
	job2, dup2, err := store.FindOrPrepareSkillJob(ctx, "test", input, true)
	if err != nil {
		t.Fatalf("second prepare: %v", err)
	}
	if !dup2 {
		t.Fatalf("expected duplicate on second call")
	}
	if job1.ID != job2.ID {
		t.Fatalf("expected same job id, got %s != %s", job1.ID, job2.ID)
	}
}

func TestWaitForCompletion(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

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

	progressPath := filepath.Join(jobDir, "progress.ndjson")
	events := []ProgressEvent{
		{Message: "first event", Timestamp: time.Now().UTC()},
		{Percent: 50, Message: "halfway", Timestamp: time.Now().UTC()},
		{Message: "final event", Timestamp: time.Now().UTC()},
	}
	f, err := os.Create(progressPath)
	if err != nil {
		t.Fatalf("create progress: %v", err)
	}
	enc := json.NewEncoder(f)
	for _, event := range events {
		if err := enc.Encode(event); err != nil {
			t.Fatalf("encode event: %v", err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close progress file: %v", err)
	}

	pr, err := OpenProgressReader(jobDir)
	if err != nil {
		t.Fatalf("open progress reader: %v", err)
	}
	t.Cleanup(func() {
		if err := pr.Close(); err != nil {
			t.Fatalf("close progress reader: %v", err)
		}
	})

	readEvents := []ProgressEvent{}
	for {
		event, err := pr.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("read event: %v", err)
		}
		readEvents = append(readEvents, event)
	}

	if len(readEvents) != len(events) {
		t.Fatalf("expected %d events, got %d", len(events), len(readEvents))
	}
	if readEvents[0].Message != "first event" {
		t.Fatalf("unexpected first message: %s", readEvents[0].Message)
	}
	if readEvents[1].Percent != 50 {
		t.Fatalf("expected percent 50, got %f", readEvents[1].Percent)
	}
}

func TestComputeSkillArgsHash(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

	input := []byte(`{"input":"value"}`)
	hash1 := store.ComputeSkillArgsHash("test", input)
	job, dup, err := store.FindOrPrepareSkillJob(ctx, "test", input, false)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if dup {
		t.Fatalf("unexpected duplicate")
	}
	if hash1 != job.ArgsHash {
		t.Fatalf("expected hash %s, got %s", hash1, job.ArgsHash)
	}
	hash2 := store.ComputeSkillArgsHash("test", []byte(`{"input":"different"}`))
	if hash1 == hash2 {
		t.Fatalf("expected different hashes for different inputs")
	}

	sameLen1 := []byte(`{"a":"aa"}`)
	sameLen2 := []byte(`{"b":"bb"}`)
	if len(sameLen1) != len(sameLen2) {
		t.Fatalf("test inputs must be same length")
	}
	hash3 := store.ComputeSkillArgsHash("test", sameLen1)
	hash4 := store.ComputeSkillArgsHash("test", sameLen2)
	if hash3 == hash4 {
		t.Fatalf("expected hashes to differ for same length inputs")
	}
}

func TestTailProgressFollowReadsAfterEOF(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	root := t.TempDir()
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

	now := time.Now().UTC()
	job := Job{
		ID:        newJobID(),
		Command:   "skill",
		ArgsJSON:  "{}",
		ArgsHash:  types.HashArgs("skill", []byte("{}")),
		State:     StateRunning,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.persist.InsertJob(ctx, job); err != nil {
		t.Fatalf("insert job: %v", err)
	}

	jobDir := store.jobDir(job.ID)
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		t.Fatalf("job dir: %v", err)
	}
	progressPath := filepath.Join(jobDir, "progress.ndjson")
	if err := os.WriteFile(progressPath, nil, 0o644); err != nil {
		t.Fatalf("create progress: %v", err)
	}

	var buf bytes.Buffer
	var wg sync.WaitGroup
	errCh := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		errCh <- store.TailProgress(ctx, job.ID, true, &buf)
	}()

	time.Sleep(200 * time.Millisecond)

	appendLine := func(line string) {
		f, openErr := os.OpenFile(progressPath, os.O_APPEND|os.O_WRONLY, 0)
		if openErr != nil {
			t.Fatalf("open progress for append: %v", openErr)
		}
		if _, writeErr := f.WriteString(line + "\n"); writeErr != nil {
			if closeErr := f.Close(); closeErr != nil {
				t.Fatalf("close progress after write error: %v", closeErr)
			}
			t.Fatalf("append progress: %v", writeErr)
		}
		if closeErr := f.Close(); closeErr != nil {
			t.Fatalf("close progress: %v", closeErr)
		}
	}

	appendLine(`{"message":"ready"}`)
	time.Sleep(200 * time.Millisecond)

	if err := store.persist.UpdateState(ctx, job.ID, StateOK, "", ""); err != nil {
		t.Fatalf("update state: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("tail progress: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("context done before tail returned: %v", ctx.Err())
	}

	wg.Wait()

	output := strings.TrimSpace(buf.String())
	if !strings.Contains(output, `{"message":"ready"}`) {
		t.Fatalf("expected progress output, got %q", output)
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

func testExecManifest(name string) skill.Manifest {
	return skill.Manifest{
		APIVersion: "agentctl/v1",
		Kind:       "Skill",
		Metadata: skill.Metadata{
			Name:        name,
			Version:     "0.0.1",
			Description: "test manifest",
		},
		Distribution: skill.Distribution{
			Type: "exec",
			Exec: &skill.ExecDistribution{Entry: "skill"},
		},
		IO: skill.IOConfig{
			Format:         "JSON",
			InlineOutputKB: 32,
		},
		Signature: skill.Signature{
			Command: name,
		},
		Capabilities: skill.Capabilities{
			Network: "none",
		},
	}
}
