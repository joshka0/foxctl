package jobs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/envelope"
	"github.com/jkatigb/agentctl/internal/skill"
)

func TestExecutorRunSkillSuccess(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t, root)
	defer func() { _ = store.Close() }()

	exec := NewExecutor(store)
	exec.run = func(context.Context, skill.Manifest, string, []byte) ([]byte, []byte, error) {
		env := envelope.OK("test", map[string]string{"message": "hi"})
		buf, _ := json.Marshal(env)
		return buf, []byte("stderr output"), nil
	}

	manifest := skill.Manifest{Metadata: skill.Metadata{Name: "test"}}
	job, result, err := exec.RunSkill(ctx, manifest, "artifact", []byte(`{"foo":"bar"}`))
	if err != nil {
		t.Fatalf("run skill: %v", err)
	}
	if job.State != StateOK {
		t.Fatalf("expected ok state, got %s", job.State)
	}
	if len(result) == 0 {
		t.Fatalf("expected result bytes")
	}

	stored, err := store.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if stored.State != StateOK {
		t.Fatalf("expected stored state ok, got %s", stored.State)
	}

	stderrPath := filepath.Join(root, job.ID, "stderr.log")
	stderr, err := os.ReadFile(stderrPath)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	if !strings.Contains(string(stderr), "stderr output") {
		t.Fatalf("expected stderr content")
	}

	progressPath := filepath.Join(root, job.ID, "progress.ndjson")
	progress, err := os.ReadFile(progressPath)
	if err != nil {
		t.Fatalf("read progress: %v", err)
	}
	if !strings.Contains(string(progress), "skill completed") {
		t.Fatalf("expected completion progress event")
	}
}

func TestExecutorRunSkillRunnerError(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t, root)
	defer func() { _ = store.Close() }()

	exec := NewExecutor(store)
	exec.run = func(context.Context, skill.Manifest, string, []byte) ([]byte, []byte, error) {
		return nil, []byte("failure"), assertError("boom")
	}

	manifest := skill.Manifest{Metadata: skill.Metadata{Name: "test"}}
	job, _, err := exec.RunSkill(ctx, manifest, "artifact", []byte(`{}`))
	if err == nil {
		t.Fatalf("expected error from runner")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected boom error, got %v", err)
	}

	stored, err := store.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if stored.State != StateError {
		t.Fatalf("expected error state, got %s", stored.State)
	}
}

func TestExecutorRunSkillInvalidJSON(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t, root)
	defer func() { _ = store.Close() }()

	exec := NewExecutor(store)
	exec.run = func(context.Context, skill.Manifest, string, []byte) ([]byte, []byte, error) {
		return []byte("not-json"), nil, nil
	}

	manifest := skill.Manifest{Metadata: skill.Metadata{Name: "test"}}
	job, _, err := exec.RunSkill(ctx, manifest, "artifact", []byte(`{}`))
	if err == nil {
		t.Fatalf("expected invalid json error")
	}
	if !strings.Contains(err.Error(), "invalid result envelope") {
		t.Fatalf("expected invalid envelope error, got %v", err)
	}

	stored, err := store.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if stored.State != StateError {
		t.Fatalf("expected error state, got %s", stored.State)
	}
}

func TestExecutorRunSkillValidationFailure(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t, root)
	defer func() { _ = store.Close() }()

	exec := NewExecutor(store)
	exec.run = func(context.Context, skill.Manifest, string, []byte) ([]byte, []byte, error) {
		buf, _ := json.Marshal(envelope.Envelope{})
		return buf, nil, nil
	}

	manifest := skill.Manifest{Metadata: skill.Metadata{Name: "test"}}
	job, _, err := exec.RunSkill(ctx, manifest, "artifact", []byte(`{}`))
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "envelope validation failed") {
		t.Fatalf("unexpected error: %v", err)
	}

	stored, err := store.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if stored.State != StateError {
		t.Fatalf("expected error state, got %s", stored.State)
	}
}

type assertError string

func (e assertError) Error() string { return string(e) }
