package jobs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/storage/jobs/types"
)

func TestStoreSubmitEchoWithFakes(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	fakePersist := newFakePersistence()
	store := New(root, fakePersist, nil)

	job, err := store.SubmitEcho(ctx, "hello fake")
	if err != nil {
		t.Fatalf("submit echo: %v", err)
	}
	if job.State != StateOK {
		t.Fatalf("expected final state ok, got %s", job.State)
	}
	if len(fakePersist.insertedJobs) != 1 {
		t.Fatalf("expected 1 inserted job, got %d", len(fakePersist.insertedJobs))
	}
	if len(fakePersist.stateUpdates) == 0 || fakePersist.stateUpdates[len(fakePersist.stateUpdates)-1].state != StateOK {
		t.Fatalf("expected final state update to OK")
	}
	result := fakePersist.jobs[job.ID].ResultPath
	if result == "" {
		t.Fatalf("expected result path to be recorded")
	}
	data, err := os.ReadFile(result)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	var env map[string]any
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	dataField, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data field in envelope, got %T", env["data"])
	}
	msg, ok := dataField["message"].(string)
	if !ok {
		t.Fatalf("expected message string, got %T", dataField["message"])
	}
	if msg != "hello fake" {
		t.Fatalf("expected message recorded, got %v", msg)
	}
}

func TestStoreRunAndPrepareUseExecutor(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	fakePersist := newFakePersistence()
	fakeExec := &fakeExecutor{}

	manifest := skill.Manifest{Metadata: skill.Metadata{Name: "test"}}
	expectedJob := types.Job{ID: "job-1", State: types.StateOK}
	fakePersist.jobs[expectedJob.ID] = expectedJob
	fakeExec.runSkillFn = func(ctx context.Context, m skill.Manifest, artifactPath string, input []byte) (types.Job, []byte, error) {
		if m.Metadata.Name != manifest.Metadata.Name {
			t.Fatalf("unexpected manifest: %+v", m)
		}
		return expectedJob, []byte("result"), nil
	}
	fakeExec.findOrPrepareFn = func(ctx context.Context, name string, input []byte, dedupe bool) (types.Job, bool, error) {
		return types.Job{ID: "prepared", Command: name, ArgsJSON: string(input)}, false, nil
	}

	store := New(root, fakePersist, fakeExec)

	job, result, err := store.RunSkill(ctx, manifest, "artifact", []byte("input"))
	if err != nil {
		t.Fatalf("run skill: %v", err)
	}
	if string(result) != "result" {
		t.Fatalf("unexpected result bytes: %s", string(result))
	}
	if job.ID != expectedJob.ID {
		t.Fatalf("expected job %s, got %s", expectedJob.ID, job.ID)
	}
	if len(fakeExec.runSkillCalls) != 1 {
		t.Fatalf("expected 1 run call, got %d", len(fakeExec.runSkillCalls))
	}
	call := fakeExec.runSkillCalls[0]
	if call.artifactPath != "artifact" {
		t.Fatalf("unexpected artifact path %s", call.artifactPath)
	}
	if string(call.input) != "input" {
		t.Fatalf("unexpected input %s", string(call.input))
	}

	prepared, err := store.PrepareSkillJob(ctx, "prep", []byte("payload"))
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if prepared.Command != "prep" {
		t.Fatalf("expected command prep, got %s", prepared.Command)
	}
	if len(fakeExec.findOrPrepareCalls) != 1 {
		t.Fatalf("expected 1 prepare call, got %d", len(fakeExec.findOrPrepareCalls))
	}
	prepCall := fakeExec.findOrPrepareCalls[0]
	if prepCall.name != "prep" {
		t.Fatalf("unexpected prepare name %s", prepCall.name)
	}
	if string(prepCall.input) != "payload" {
		t.Fatalf("unexpected prepare input %s", string(prepCall.input))
	}
}

func TestStoreSetWorkspaceRecordsValue(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	fakePersist := newFakePersistence()
	store := New(root, fakePersist, nil)

	if err := store.SetWorkspace(ctx, "job-1", "/tmp/workspace"); err != nil {
		t.Fatalf("set workspace: %v", err)
	}
	workspacePath := filepath.Join(root, "job-1", "workspace")
	data, err := os.ReadFile(workspacePath)
	if err != nil {
		t.Fatalf("read workspace: %v", err)
	}
	if string(data) != "/tmp/workspace" {
		t.Fatalf("unexpected workspace value %s", string(data))
	}

	if err := store.SetWorkspace(ctx, "job-1", "/tmp/new"); err != nil {
		t.Fatalf("set workspace second time: %v", err)
	}
	data, err = os.ReadFile(workspacePath)
	if err != nil {
		t.Fatalf("read workspace after second call: %v", err)
	}
	if string(data) != "/tmp/workspace" {
		t.Fatalf("expected workspace to remain unchanged, got %s", string(data))
	}
}

type fakePersistence struct {
	mu           sync.Mutex
	jobs         map[string]types.Job
	insertedJobs []types.Job
	stateUpdates []stateUpdate
}

type stateUpdate struct {
	id         string
	state      types.State
	errMsg     string
	resultPath string
}

func newFakePersistence() *fakePersistence {
	return &fakePersistence{jobs: make(map[string]types.Job)}
}

func (f *fakePersistence) Close() error { return nil }

func (f *fakePersistence) List(ctx context.Context, limit int) ([]types.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	jobs := make([]types.Job, 0, len(f.jobs))
	for _, job := range f.jobs {
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func (f *fakePersistence) Get(ctx context.Context, id string) (types.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	job, ok := f.jobs[id]
	if !ok {
		return types.Job{}, types.ErrNotFound
	}
	return job, nil
}

func (f *fakePersistence) InsertJob(ctx context.Context, job types.Job) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jobs[job.ID] = job
	f.insertedJobs = append(f.insertedJobs, job)
	return nil
}

func (f *fakePersistence) UpdateState(ctx context.Context, id string, newState types.State, errMsg, resultPath string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	job, ok := f.jobs[id]
	if !ok {
		return types.ErrNotFound
	}
	job.State = newState
	job.Error = errMsg
	if resultPath != "" {
		job.ResultPath = resultPath
	}
	job.UpdatedAt = time.Now().UTC()
	f.jobs[id] = job
	f.stateUpdates = append(f.stateUpdates, stateUpdate{id: id, state: newState, errMsg: errMsg, resultPath: resultPath})
	return nil
}

func (f *fakePersistence) Delete(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.jobs, id)
	return nil
}

func (f *fakePersistence) RecoverOrphanedJobs(ctx context.Context) (int64, error) {
	return 0, nil
}

type fakeExecutor struct {
	runSkillFn        func(ctx context.Context, manifest skill.Manifest, artifactPath string, input []byte) (types.Job, []byte, error)
	findOrPrepareFn   func(ctx context.Context, name string, input []byte, dedupe bool) (types.Job, bool, error)
	executePreparedFn func(ctx context.Context, jobID string, manifest skill.Manifest, artifactPath string, input []byte) ([]byte, error)

	runSkillCalls      []runSkillCall
	findOrPrepareCalls []findOrPrepareCall
}

type runSkillCall struct {
	manifest     skill.Manifest
	artifactPath string
	input        []byte
}

type findOrPrepareCall struct {
	name   string
	input  []byte
	dedupe bool
}

func (f *fakeExecutor) RunSkill(ctx context.Context, manifest skill.Manifest, artifactPath string, input []byte) (types.Job, []byte, error) {
	f.runSkillCalls = append(f.runSkillCalls, runSkillCall{manifest: manifest, artifactPath: artifactPath, input: append([]byte(nil), input...)})
	if f.runSkillFn != nil {
		return f.runSkillFn(ctx, manifest, artifactPath, input)
	}
	return types.Job{}, nil, nil
}

func (f *fakeExecutor) FindOrPrepareSkillJob(ctx context.Context, name string, input []byte, dedupe bool) (types.Job, bool, error) {
	f.findOrPrepareCalls = append(f.findOrPrepareCalls, findOrPrepareCall{name: name, input: append([]byte(nil), input...), dedupe: dedupe})
	if f.findOrPrepareFn != nil {
		return f.findOrPrepareFn(ctx, name, input, dedupe)
	}
	return types.Job{}, false, nil
}

func (f *fakeExecutor) ExecutePrepared(ctx context.Context, jobID string, manifest skill.Manifest, artifactPath string, input []byte) ([]byte, error) {
	if f.executePreparedFn != nil {
		return f.executePreparedFn(ctx, jobID, manifest, artifactPath, input)
	}
	return nil, nil
}
