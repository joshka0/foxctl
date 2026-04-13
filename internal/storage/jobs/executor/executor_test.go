package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/execution"
	"github.com/jkatigb/agentctl/internal/platform/logging"
	"github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/runtime/observability"
	"github.com/jkatigb/agentctl/internal/storage/jobs/types"
)

func TestExecutorRunSkillSuccess(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	persist := newFakePersistence()
	runner := func(_ context.Context, _ skill.Manifest, _ string, _ []byte) ([]byte, []byte, error) {
		env := envelope.OK("test", map[string]string{"message": "ok"})
		buf, err := json.Marshal(env)
		if err != nil {
			return nil, nil, err
		}
		return buf, []byte("stderr"), nil
	}
	exec := New(root, persist, WithRunner(runner))

	manifest := skill.Manifest{
		Metadata: skill.Metadata{Name: "demo"},
		Distribution: skill.Distribution{
			Type: "exec",
			Exec: &skill.ExecDistribution{Entry: "/bin/echo"},
		},
	}
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

	manifest := skill.Manifest{
		Metadata: skill.Metadata{Name: "demo"},
		Distribution: skill.Distribution{
			Type: "exec",
			Exec: &skill.ExecDistribution{Entry: "/bin/echo"},
		},
	}
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

func TestExecutorLogsProgressFailures(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	persist := newFakePersistence()
	var logBuf bytes.Buffer
	logger := logging.New(logging.Config{
		Level:  logging.LevelWarn,
		Format: logging.FormatText,
		Writer: &logBuf,
	})
	runner := func(_ context.Context, _ skill.Manifest, _ string, _ []byte) ([]byte, []byte, error) {
		env := envelope.OK("test", map[string]string{"message": "ok"})
		buf, err := json.Marshal(env)
		if err != nil {
			return nil, nil, err
		}
		return buf, nil, nil
	}
	exec := New(root, persist,
		WithRunner(runner),
		WithLogger(logger),
		withProgressWriterFactory(func(string) (*progressWriter, error) {
			return &progressWriter{
				writeOverride: func(ProgressEvent) error { return errors.New("boom") },
				closeOverride: func() error { return nil },
			}, nil
		}),
	)
	manifest := skill.Manifest{
		Metadata: skill.Metadata{Name: "demo"},
		Distribution: skill.Distribution{
			Type: "exec",
			Exec: &skill.ExecDistribution{Entry: "/bin/echo"},
		},
	}
	if _, _, err := exec.RunSkill(ctx, manifest, "", []byte(`{"foo":"bar"}`)); err != nil {
		t.Fatalf("run skill: %v", err)
	}
	if !bytes.Contains(logBuf.Bytes(), []byte("progress write failed")) {
		t.Fatalf("expected progress warning, got %q", logBuf.String())
	}
}

func TestExecutorStartExecutionInitializesProgress(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	persist := newFakePersistence()

	job := types.Job{
		ID:        "job-start",
		Command:   "skill:demo",
		ArgsJSON:  "{}",
		ArgsHash:  "hash",
		State:     types.StateQueued,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := persist.InsertJob(ctx, job); err != nil {
		t.Fatalf("insert job: %v", err)
	}

	jobDir := filepath.Join(root, job.ID)
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		t.Fatalf("mkdir job dir: %v", err)
	}
	workspacePath := filepath.Join(root, "workspace")
	if err := os.WriteFile(filepath.Join(jobDir, "workspace"), []byte(workspacePath), 0o644); err != nil {
		t.Fatalf("write workspace: %v", err)
	}

	var messages []string
	exec := New(root, persist, withProgressWriterFactory(func(string) (*progressWriter, error) {
		return &progressWriter{
			writeOverride: func(ev ProgressEvent) error {
				messages = append(messages, ev.Message)
				return nil
			},
			closeOverride: func() error { return nil },
		}, nil
	}))

	newCtx, pw, cleanup, start, err := exec.startExecution(ctx, job.ID)
	if err != nil {
		t.Fatalf("start execution: %v", err)
	}
	if cleanup == nil {
		t.Fatal("expected cleanup function")
	}
	cleanup()
	if pw == nil {
		t.Fatal("expected progress writer")
	}
	if start.IsZero() {
		t.Fatal("expected start timestamp")
	}
	if len(messages) == 0 || messages[0] != "skill execution started" {
		t.Fatalf("expected progress message, got %v", messages)
	}
	if ws, ok := workspace.FromContext(newCtx); !ok || ws != workspacePath {
		t.Fatalf("expected workspace %q, got %q", workspacePath, ws)
	}
	stored, err := persist.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if stored.State != types.StateRunning {
		t.Fatalf("expected state running, got %s", stored.State)
	}
}

func TestExecutorHandleFailureUpdatesState(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	persist := newFakePersistence()

	job := types.Job{
		ID:        "job-fail",
		Command:   "skill:demo",
		ArgsJSON:  "{}",
		ArgsHash:  "hash",
		State:     types.StateRunning,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := persist.InsertJob(ctx, job); err != nil {
		t.Fatalf("insert job: %v", err)
	}

	exec := New(root, persist)
	stdout := []byte("ok output")
	stderr := []byte("boom")

	result, err := exec.handleFailure(ctx, job.ID, stdout, stderr, errors.New("fail"), nil)
	if err == nil {
		t.Fatal("expected failure error")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("boom")) {
		t.Fatalf("expected stderr detail in error, got %v", err)
	}
	if !bytes.Equal(result, stdout) {
		t.Fatalf("expected stdout passthrough")
	}
	stored, getErr := persist.Get(ctx, job.ID)
	if getErr != nil {
		t.Fatalf("get job: %v", getErr)
	}
	if stored.State != types.StateError {
		t.Fatalf("expected error state, got %s", stored.State)
	}
	if stored.Error == "" {
		t.Fatal("expected error message recorded")
	}
}

func TestExecutorHandleSuccessWritesResult(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	persist := newFakePersistence()

	job := types.Job{
		ID:        "job-success",
		Command:   "skill:demo",
		ArgsJSON:  "{}",
		ArgsHash:  "hash",
		State:     types.StateRunning,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := persist.InsertJob(ctx, job); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	jobDir := filepath.Join(root, job.ID)
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		t.Fatalf("mkdir job dir: %v", err)
	}

	var events []ProgressEvent
	pw := &progressWriter{
		writeOverride: func(ev ProgressEvent) error {
			events = append(events, ev)
			return nil
		},
		closeOverride: func() error { return nil },
	}

	exec := New(root, persist)
	env := envelope.OK("test", map[string]string{"message": "ok"})
	stdout, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal env: %v", err)
	}

	result, err := exec.handleSuccess(ctx, job.ID, stdout, pw)
	if err != nil {
		t.Fatalf("handle success: %v", err)
	}
	if !bytes.Equal(result, stdout) {
		t.Fatalf("expected stdout passthrough")
	}
	stored, getErr := persist.Get(ctx, job.ID)
	if getErr != nil {
		t.Fatalf("get job: %v", getErr)
	}
	if stored.State != types.StateOK {
		t.Fatalf("expected ok state, got %s", stored.State)
	}
	if stored.ResultPath == "" {
		t.Fatal("expected result path recorded")
	}
	if _, statErr := os.Stat(stored.ResultPath); statErr != nil {
		t.Fatalf("stat result: %v", statErr)
	}
	if len(events) == 0 || events[len(events)-1].Message != "skill completed" {
		t.Fatalf("expected completion event, got %v", events)
	}
}

func TestExecutorRunSkillUsesExecutor(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	persist := newFakePersistence()

	mockExec := execution.NewMockExecutor()
	mockExec.ExecuteFunc = func(context.Context, execution.ExecuteOptions) (*execution.Result, error) {
		return &execution.Result{Stdout: []byte("out"), Stderr: []byte("err")}, nil
	}

	exec := New(root, persist, WithSkillExecutor(mockExec))
	manifest := skill.Manifest{Metadata: skill.Metadata{Name: "demo"}}
	stdout, stderr, err := exec.runSkill(ctx, manifest, "artifact", []byte("input"))
	if err != nil {
		t.Fatalf("runSkill: %v", err)
	}
	if string(stdout) != "out" || string(stderr) != "err" {
		t.Fatalf("unexpected outputs: %q %q", stdout, stderr)
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

// TestExecutorWithSkillExecutor demonstrates using the new SkillExecutor interface
// with a mock executor for testing. This shows SPEC-004 integration.
func TestExecutorWithSkillExecutor(t *testing.T) {
	root := t.TempDir()
	persist := newFakePersistence()

	// Create a mock executor
	mockExec := execution.NewMockExecutor()
	mockExec.ExecuteFunc = func(_ context.Context, opts execution.ExecuteOptions) (*execution.Result, error) {
		// Verify options
		if opts.ManifestPath == "" {
			t.Error("expected manifest path")
		}

		// Return a success result
		env := envelope.OK("test", map[string]string{"mock": "true"})
		buf, err := json.Marshal(env)
		if err != nil {
			return nil, err
		}
		return &execution.Result{
			Stdout:   buf,
			Stderr:   []byte("mock stderr"),
			ExitCode: 0,
		}, nil
	}

	// Create executor with mock
	exec := New(root, persist, WithSkillExecutor(mockExec))

	// The current implementation still uses runner.Run directly for executeSkill,
	// but the interface is in place for future use and demonstrates the pattern.
	// The mock executor can be used for testing new methods that work with manifest paths.

	// Verify the executor was created successfully
	if exec == nil {
		t.Fatal("expected executor to be created")
		return
	}
	if exec.skillExecutor == nil {
		t.Fatal("expected skillExecutor to be set")
	}

	// This demonstrates that the executor can be created with a mock,
	// which enables better testing in the future.
	t.Log("Successfully created executor with mock SkillExecutor")
}

// TestExecutorEmitsWideEventOnSuccess verifies that successful skill execution
// emits a wide event with status "ok".
func TestExecutorEmitsWideEventOnSuccess(t *testing.T) {
	// Set up observability
	obsDir := t.TempDir()
	observability.SetObsDirForTesting(obsDir)
	observability.SetSamplerForTesting(observability.SampleAll{})

	ctx := context.Background()
	root := t.TempDir()
	persist := newFakePersistence()

	runner := func(_ context.Context, _ skill.Manifest, _ string, _ []byte) ([]byte, []byte, error) {
		env := envelope.OK("test-skill", map[string]string{"result": "success"})
		buf, _ := json.Marshal(env)
		return buf, nil, nil
	}
	exec := New(root, persist, WithRunner(runner))

	manifest := skill.Manifest{
		Metadata: skill.Metadata{Name: "test/success-skill", Version: "1.0.0"},
		Distribution: skill.Distribution{
			Type: "exec",
			Exec: &skill.ExecDistribution{Entry: "/bin/test"},
		},
	}

	_, _, err := exec.RunSkill(ctx, manifest, "artifact", []byte(`{}`))
	if err != nil {
		t.Fatalf("run skill: %v", err)
	}

	// Verify wide event was emitted
	events := readWideEvents(t, obsDir)
	if len(events) == 0 {
		t.Fatal("expected at least one wide event")
	}

	// Find the skill.run event
	var found *observability.WideEvent
	for i := range events {
		if events[i].Operation == observability.OpSkillRun {
			found = &events[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected skill.run wide event")
		return
	}

	if found.Status != observability.StatusOK {
		t.Errorf("Status = %q, want %q", found.Status, observability.StatusOK)
	}
	if found.Command != "test/success-skill" {
		t.Errorf("Command = %q, want test/success-skill", found.Command)
	}
	if found.Component != observability.ComponentSkill {
		t.Errorf("Component = %q, want %q", found.Component, observability.ComponentSkill)
	}
	if found.DurationMS < 0 {
		t.Error("DurationMS should be non-negative")
	}
	if found.Data["skill_version"] != "1.0.0" {
		t.Errorf("Data[skill_version] = %v, want 1.0.0", found.Data["skill_version"])
	}
}

// TestExecutorEmitsWideEventOnRunnerError verifies that runner failures emit
// an error wide event.
func TestExecutorEmitsWideEventOnRunnerError(t *testing.T) {
	obsDir := t.TempDir()
	observability.SetObsDirForTesting(obsDir)
	observability.SetSamplerForTesting(observability.SampleAll{})

	ctx := context.Background()
	root := t.TempDir()
	persist := newFakePersistence()

	runnerErr := errors.New("connection refused: dial tcp")
	runner := func(_ context.Context, _ skill.Manifest, _ string, _ []byte) ([]byte, []byte, error) {
		return nil, []byte("stderr output"), runnerErr
	}
	exec := New(root, persist, WithRunner(runner))

	manifest := skill.Manifest{
		Metadata: skill.Metadata{Name: "test/failing-skill", Version: "2.0.0"},
		Distribution: skill.Distribution{
			Type: "exec",
			Exec: &skill.ExecDistribution{Entry: "/bin/fail"},
		},
	}

	_, _, err := exec.RunSkill(ctx, manifest, "artifact", []byte(`{}`))
	if err == nil {
		t.Fatal("expected error from skill execution")
	}

	events := readWideEvents(t, obsDir)
	if len(events) == 0 {
		t.Fatal("expected at least one wide event")
	}

	var found *observability.WideEvent
	for i := range events {
		if events[i].Operation == observability.OpSkillRun {
			found = &events[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected skill.run wide event")
		return
	}

	if found.Status != observability.StatusError {
		t.Errorf("Status = %q, want %q", found.Status, observability.StatusError)
	}
	if found.Command != "test/failing-skill" {
		t.Errorf("Command = %q, want test/failing-skill", found.Command)
	}
	if found.ErrorType != "network" {
		t.Errorf("ErrorType = %q, want network", found.ErrorType)
	}
}

// TestExecutorEmitsWideEventOnEnvelopeError verifies that when a skill returns
// an error envelope (status="error"), it emits an error wide event with the
// skill's error code.
func TestExecutorEmitsWideEventOnEnvelopeError(t *testing.T) {
	obsDir := t.TempDir()
	observability.SetObsDirForTesting(obsDir)
	observability.SetSamplerForTesting(observability.SampleAll{})

	ctx := context.Background()
	root := t.TempDir()
	persist := newFakePersistence()

	runner := func(_ context.Context, _ skill.Manifest, _ string, _ []byte) ([]byte, []byte, error) {
		// Return an error envelope (skill succeeded but returned an error response)
		env := envelope.Error("test-skill", "EINVALID_INPUT", "field 'name' is required", nil)
		buf, _ := json.Marshal(env)
		return buf, nil, nil
	}
	exec := New(root, persist, WithRunner(runner))

	manifest := skill.Manifest{
		Metadata: skill.Metadata{Name: "test/validation-skill", Version: "1.0.0"},
		Distribution: skill.Distribution{
			Type: "exec",
			Exec: &skill.ExecDistribution{Entry: "/bin/validate"},
		},
	}

	// Should succeed (envelope is valid, just status=error)
	_, _, err := exec.RunSkill(ctx, manifest, "artifact", []byte(`{}`))
	if err != nil {
		t.Fatalf("run skill: %v", err)
	}

	events := readWideEvents(t, obsDir)
	if len(events) == 0 {
		t.Fatal("expected at least one wide event")
	}

	var found *observability.WideEvent
	for i := range events {
		if events[i].Operation == observability.OpSkillRun {
			found = &events[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected skill.run wide event")
		return
	}

	if found.Status != observability.StatusError {
		t.Errorf("Status = %q, want %q", found.Status, observability.StatusError)
	}
	if found.ErrorType != "skill_error" {
		t.Errorf("ErrorType = %q, want skill_error", found.ErrorType)
	}
	if found.ErrorCode != "EINVALID_INPUT" {
		t.Errorf("ErrorCode = %q, want EINVALID_INPUT", found.ErrorCode)
	}
	if found.ErrorMessage != "field 'name' is required" {
		t.Errorf("ErrorMessage = %q, want field 'name' is required", found.ErrorMessage)
	}
	if found.Data["envelope_error"] != true {
		t.Errorf("Data[envelope_error] = %v, want true", found.Data["envelope_error"])
	}
	if found.Data["error_code"] != "EINVALID_INPUT" {
		t.Errorf("Data[error_code] = %v, want EINVALID_INPUT", found.Data["error_code"])
	}
}

// readWideEvents reads all wide events from the observability directory.
func readWideEvents(t *testing.T, obsDir string) []observability.WideEvent {
	t.Helper()

	filePath := filepath.Join(obsDir, "events", observability.WideEventFileName+".ndjson")
	f, err := os.Open(filePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("open wide events file: %v", err)
	}
	defer f.Close()

	var events []observability.WideEvent
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var ev observability.WideEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			t.Fatalf("unmarshal wide event: %v", err)
		}
		events = append(events, ev)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan wide events: %v", err)
	}
	return events
}
