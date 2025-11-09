package jobs

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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
