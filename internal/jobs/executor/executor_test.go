package executor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/envelope"
	"github.com/jkatigb/agentctl/internal/jobs/types"
	"github.com/jkatigb/agentctl/internal/skill"
)

func TestExecutorRunSkillSuccess(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	persist := newFakePersistence()
	runner := func(_ context.Context, _ skill.Manifest, _ string, _ []byte) ([]byte, []byte, error) {
		env := envelope.OK("test", map[string]string{"message": "ok"})
		buf, _ := json.Marshal(env)
		return buf, []byte("stderr"), nil
	}
	exec := New(root, persist, WithRunner(runner))

	manifest := skill.Manifest{Metadata: skill.Metadata{Name: "demo"}}
	job, result, err := exec.RunSkill(ctx, manifest, "artifact", []byte(`{"foo":"bar"}`))
	if err != nil {
		t.Fatalf("run skill: %v", err)
	}
	if len(result) == 0 {
		t.Fatalf("expected result bytes")
	}

	stored, err := persist.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.State != types.StateOK {
		t.Fatalf("expected state ok, got %s", stored.State)
	}
	if stored.ResultPath == "" {
		t.Fatalf("expected result path")
	}
	if _, err := os.Stat(filepath.Join(root, job.ID, "progress.ndjson")); err != nil {
		t.Fatalf("expected progress file: %v", err)
	}
}

func TestExecutorRunSkillValidationError(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	persist := newFakePersistence()
	runner := func(_ context.Context, _ skill.Manifest, _ string, _ []byte) ([]byte, []byte, error) {
		return []byte("not-json"), nil, nil
	}
	exec := New(root, persist, WithRunner(runner))

	manifest := skill.Manifest{Metadata: skill.Metadata{Name: "demo"}}
	job, _, err := exec.RunSkill(ctx, manifest, "artifact", []byte(`{"foo":"bar"}`))
	if err == nil {
		t.Fatalf("expected validation error")
	}
	stored, getErr := persist.Get(ctx, job.ID)
	if getErr != nil {
		t.Fatalf("get: %v", getErr)
	}
	if stored.State != types.StateError {
		t.Fatalf("expected error state, got %s", stored.State)
	}
	if stored.Error == "" {
		t.Fatalf("expected error message to be recorded")
	}
}

func TestExecutorFindOrPrepareSkillJobDedupes(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	persist := newFakePersistence()
	exec := New(root, persist, WithRunner(func(_ context.Context, _ skill.Manifest, _ string, _ []byte) ([]byte, []byte, error) {
		return nil, nil, errors.New("not used")
	}))

	job1, dup1, err := exec.FindOrPrepareSkillJob(ctx, "demo", []byte(`{"foo":"bar"}`), true)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if dup1 {
		t.Fatalf("expected first job not duplicate")
	}
	job2, dup2, err := exec.FindOrPrepareSkillJob(ctx, "demo", []byte(`{"foo":"bar"}`), true)
	if err != nil {
		t.Fatalf("duplicate prepare: %v", err)
	}
	if !dup2 {
		t.Fatalf("expected duplicate on second call")
	}
	if job1.ID != job2.ID {
		t.Fatalf("expected same job id")
	}
	if len(persist.snapshot()) != 1 {
		t.Fatalf("expected one job in persistence, got %d", len(persist.snapshot()))
	}
}

type fakePersistence struct {
	mu   sync.Mutex
	jobs map[string]types.Job
}

func newFakePersistence() *fakePersistence {
	return &fakePersistence{jobs: make(map[string]types.Job)}
}

func (f *fakePersistence) InsertJob(_ context.Context, job types.Job) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jobs[job.ID] = job
	return nil
}

func (f *fakePersistence) FindOrInsertJob(_ context.Context, job types.Job) (types.Job, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.jobs {
		if existing.ArgsHash == job.ArgsHash {
			return existing, true, nil
		}
	}
	f.jobs[job.ID] = job
	return job, false, nil
}

func (f *fakePersistence) UpdateState(_ context.Context, id string, newState types.State, errMsg, resultPath string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	job, ok := f.jobs[id]
	if !ok {
		return types.ErrNotFound
	}
	if !validTransition(job.State, newState) {
		return types.ErrInvalidState
	}
	job.State = newState
	job.Error = errMsg
	if resultPath != "" {
		job.ResultPath = resultPath
	}
	job.UpdatedAt = time.Now().UTC()
	f.jobs[id] = job
	return nil
}

func (f *fakePersistence) Get(_ context.Context, id string) (types.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	job, ok := f.jobs[id]
	if !ok {
		return types.Job{}, types.ErrNotFound
	}
	return job, nil
}

func (f *fakePersistence) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.jobs, id)
	return nil
}

func (f *fakePersistence) snapshot() map[string]types.Job {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]types.Job, len(f.jobs))
	for k, v := range f.jobs {
		out[k] = v
	}
	return out
}

func validTransition(current, next types.State) bool {
	switch next {
	case types.StateQueued:
		return current == types.StateQueued
	case types.StateRunning:
		return current == types.StateQueued || current == types.StateRunning
	case types.StateOK:
		return current == types.StateRunning || current == types.StateOK
	case types.StateError:
		return current == types.StateQueued || current == types.StateRunning || current == types.StateError
	case types.StateCanceled:
		return current == types.StateQueued || current == types.StateRunning || current == types.StateCanceled
	default:
		return false
	}
}
