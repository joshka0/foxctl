package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/storage/jobs/fsutil"
	"github.com/joshka0/foxctl/internal/storage/jobs/types"
)

func TestSubmitEchoCreatesResult(t *testing.T) {
	testEnv := newStoreTestEnv(t)

	job, err := testEnv.store.SubmitEcho(testEnv.ctx, "hello world")
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
	_ = openStoreForTest(ctx, t, root)

	if _, err := os.Stat(root); err != nil {
		t.Fatalf("expected root directory to exist: %v", err)
	}
}

func TestListJobsOrder(t *testing.T) {
	testEnv := newStoreTestEnv(t)

	if _, err := testEnv.store.SubmitEcho(testEnv.ctx, "first"); err != nil {
		t.Fatalf("submit first: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if _, err := testEnv.store.SubmitEcho(testEnv.ctx, "second"); err != nil {
		t.Fatalf("submit second: %v", err)
	}

	jobs, err := testEnv.store.List(testEnv.ctx, 10)
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
	testEnv := newStoreTestEnv(t)

	job, err := testEnv.store.SubmitEcho(testEnv.ctx, "done")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if err := testEnv.store.Cancel(testEnv.ctx, job.ID); err == nil {
		t.Fatalf("expected cancel to fail on completed job")
	}
}

func TestCancelMarksQueuedAndRunningJobsCanceled(t *testing.T) {
	testEnv := newStoreTestEnv(t)

	tests := []struct {
		name  string
		state State
	}{
		{name: "queued", state: StateQueued},
		{name: "running", state: StateRunning},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := insertStoreJobInState(t, testEnv, "job-cancel-"+tt.name, tt.state)

			if err := testEnv.store.Cancel(testEnv.ctx, job.ID); err != nil {
				t.Fatalf("cancel %s job: %v", tt.state, err)
			}

			stored, err := testEnv.store.Get(testEnv.ctx, job.ID)
			if err != nil {
				t.Fatalf("get canceled job: %v", err)
			}
			if stored.State != StateCanceled {
				t.Fatalf("state=%s want %s", stored.State, StateCanceled)
			}
			if err := testEnv.store.Cancel(testEnv.ctx, job.ID); !errors.Is(err, ErrInvalidState) {
				t.Fatalf("cancel terminal job error=%v want ErrInvalidState", err)
			}
		})
	}
}

func TestResultReadsFile(t *testing.T) {
	testEnv := newStoreTestEnv(t)

	job, err := testEnv.store.SubmitEcho(testEnv.ctx, "result test")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	data, err := testEnv.store.Result(testEnv.ctx, job.ID)
	if err != nil {
		t.Fatalf("result: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("expected data")
	}
}

func TestJobDirectoriesCreated(t *testing.T) {
	testEnv := newStoreTestEnv(t)

	job, err := testEnv.store.SubmitEcho(testEnv.ctx, "dirs")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	dir := filepath.Join(testEnv.root, job.ID)
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("expected job dir: %v", err)
	}
}

func TestOpenStoreRequiresInjectedExecutorForSkillJobs(t *testing.T) {
	testEnv := newStoreTestEnv(t)

	_, _, err := testEnv.store.FindOrPrepareSkillJob(testEnv.ctx, "test", []byte(`{"foo":"bar"}`), true)
	if !errors.Is(err, errSkillExecutorNotConfigured) {
		t.Fatalf("expected skill executor configuration error, got %v", err)
	}
}

func TestWaitForCompletion(t *testing.T) {
	testEnv := newStoreTestEnv(t)

	job, err := testEnv.store.SubmitEcho(testEnv.ctx, "wait test")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	finalJob, err := testEnv.store.WaitForCompletion(testEnv.ctx, job.ID, 0)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if finalJob.State != StateOK {
		t.Fatalf("expected ok state, got %s", finalJob.State)
	}
}

func TestWaitForCompletionReturnsEveryTerminalState(t *testing.T) {
	testEnv := newStoreTestEnv(t)

	for _, state := range []State{StateOK, StateError, StateCanceled} {
		t.Run(string(state), func(t *testing.T) {
			job := insertStoreJobInState(t, testEnv, "job-wait-"+string(state), state)
			ctx, cancel := context.WithTimeout(testEnv.ctx, 250*time.Millisecond)
			defer cancel()

			finalJob, err := testEnv.store.WaitForCompletion(ctx, job.ID, time.Hour)
			if err != nil {
				t.Fatalf("wait for %s job: %v", state, err)
			}
			if finalJob.State != state {
				t.Fatalf("state=%s want %s", finalJob.State, state)
			}
		})
	}
}

func TestWaitForCompletionReturnsWhenContextCanceledDuringPoll(t *testing.T) {
	testEnv := newStoreTestEnv(t)
	job := insertStoreJobInState(t, testEnv, "job-wait-cancel-context", StateRunning)
	notifyPersist := &getNotifyingPersistence{
		Persistence: testEnv.store.persist,
		targetID:    job.ID,
		seenRunning: make(chan struct{}),
	}
	store := New(testEnv.root, notifyPersist, nil)

	ctx, cancel := context.WithCancel(testEnv.ctx)
	errCh := make(chan error, 1)
	go func() {
		_, err := store.WaitForCompletion(ctx, job.ID, time.Hour)
		errCh <- err
	}()

	select {
	case <-notifyPersist.seenRunning:
	case <-time.After(500 * time.Millisecond):
		cancel()
		t.Fatal("wait did not read running job before polling")
	}
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("wait error=%v want context.Canceled", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("wait did not return after context cancellation")
	}
}

func TestProgressReader(t *testing.T) {
	testEnv := newStoreTestEnv(t)
	jobDir := filepath.Join(testEnv.root, "test-job")
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
	testEnv := newStoreTestEnv(t)

	input := []byte(`{"input":"value"}`)
	hash1 := testEnv.store.ComputeSkillArgsHash("test", input)
	if expected := types.ComputeSkillArgsHash("test", input); hash1 != expected {
		t.Fatalf("expected hash %s, got %s", expected, hash1)
	}
	hash2 := testEnv.store.ComputeSkillArgsHash("test", []byte(`{"input":"different"}`))
	if hash1 == hash2 {
		t.Fatalf("expected different hashes for different inputs")
	}

	sameLen1 := []byte(`{"a":"aa"}`)
	sameLen2 := []byte(`{"b":"bb"}`)
	if len(sameLen1) != len(sameLen2) {
		t.Fatalf("test inputs must be same length")
	}
	hash3 := testEnv.store.ComputeSkillArgsHash("test", sameLen1)
	hash4 := testEnv.store.ComputeSkillArgsHash("test", sameLen2)
	if hash3 == hash4 {
		t.Fatalf("expected hashes to differ for same length inputs")
	}
}

func TestTailProgressFollowReadsAfterEOF(t *testing.T) {
	testEnv := newStoreTestEnv(t)
	ctx, cancel := context.WithTimeout(testEnv.ctx, 5*time.Second)
	defer cancel()

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
	if err := testEnv.store.persist.InsertJob(ctx, job); err != nil {
		t.Fatalf("insert job: %v", err)
	}

	jobDir := fsutil.JobDir(testEnv.root, job.ID)
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		t.Fatalf("job dir: %v", err)
	}
	progressPath := filepath.Join(jobDir, "progress.ndjson")
	if err := os.WriteFile(progressPath, nil, 0o644); err != nil {
		t.Fatalf("create progress: %v", err)
	}

	watch := startTailWatcher(ctx, t, testEnv.store, job.ID, true)

	time.Sleep(200 * time.Millisecond)

	appendProgressLine(t, progressPath, `{"message":"ready"}`)
	time.Sleep(200 * time.Millisecond)

	if err := testEnv.store.persist.UpdateState(ctx, job.ID, StateOK, "", ""); err != nil {
		t.Fatalf("update state: %v", err)
	}

	select {
	case err := <-watch.ErrCh():
		if err != nil {
			t.Fatalf("tail progress: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("context done before tail returned: %v", ctx.Err())
	}

	watch.Wait()

	output := strings.TrimSpace(watch.Buffer().String())
	if !strings.Contains(output, `{"message":"ready"}`) {
		t.Fatalf("expected progress output, got %q", output)
	}
}

type storeTestEnv struct {
	ctx   context.Context
	root  string
	store *Store
}

func newStoreTestEnv(t *testing.T) storeTestEnv {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	store := openStoreForTest(ctx, t, root)
	return storeTestEnv{ctx: ctx, root: root, store: store}
}

func insertStoreJobInState(t testing.TB, env storeTestEnv, id string, state State) Job {
	t.Helper()
	now := time.Now().UTC()
	job := Job{
		ID:        id,
		Command:   "skill:test",
		ArgsJSON:  "{}",
		ArgsHash:  types.HashArgs(id, []byte("{}")),
		State:     StateQueued,
		CreatedAt: now,
		UpdatedAt: now,
		ExpiresAt: now.Add(types.DefaultMaxJobAge),
	}
	if err := env.store.persist.InsertJob(env.ctx, job); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	switch state {
	case StateQueued:
	case StateRunning:
		if err := env.store.persist.UpdateState(env.ctx, job.ID, StateRunning, "", ""); err != nil {
			t.Fatalf("set running: %v", err)
		}
	case StateOK:
		if err := env.store.persist.UpdateState(env.ctx, job.ID, StateRunning, "", ""); err != nil {
			t.Fatalf("set running: %v", err)
		}
		if err := env.store.persist.UpdateState(env.ctx, job.ID, StateOK, "", filepath.Join(env.root, id, "result.json")); err != nil {
			t.Fatalf("set ok: %v", err)
		}
	case StateError:
		if err := env.store.persist.UpdateState(env.ctx, job.ID, StateError, "failed", ""); err != nil {
			t.Fatalf("set error: %v", err)
		}
	case StateCanceled:
		if err := env.store.persist.UpdateState(env.ctx, job.ID, StateCanceled, "", ""); err != nil {
			t.Fatalf("set canceled: %v", err)
		}
	default:
		t.Fatalf("unsupported state %q", state)
	}
	stored, err := env.store.Get(env.ctx, job.ID)
	if err != nil {
		t.Fatalf("get inserted job: %v", err)
	}
	return stored
}

func openStoreForTest(ctx context.Context, t testing.TB, root string) *Store {
	t.Helper()
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})
	return store
}

type tailWatcher struct {
	buf    *bytes.Buffer
	errCh  <-chan error
	waitFn func()
}

type getNotifyingPersistence struct {
	Persistence
	targetID    string
	seenRunning chan struct{}
	once        sync.Once
}

func (p *getNotifyingPersistence) Get(ctx context.Context, id string) (Job, error) {
	job, err := p.Persistence.Get(ctx, id)
	if err == nil && id == p.targetID && job.State == StateRunning {
		p.once.Do(func() {
			close(p.seenRunning)
		})
	}
	return job, err
}

func startTailWatcher(ctx context.Context, t testing.TB, store *Store, jobID string, follow bool) tailWatcher {
	t.Helper()
	buf := &bytes.Buffer{}
	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		errCh <- store.TailProgress(ctx, jobID, follow, buf)
	}()
	return tailWatcher{
		buf:   buf,
		errCh: errCh,
		waitFn: func() {
			wg.Wait()
		},
	}
}

func (tw tailWatcher) Buffer() *bytes.Buffer {
	return tw.buf
}

func (tw tailWatcher) ErrCh() <-chan error {
	return tw.errCh
}

func (tw tailWatcher) Wait() {
	if tw.waitFn != nil {
		tw.waitFn()
	}
}

func appendProgressLine(t testing.TB, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open progress for append: %v", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Fatalf("close progress: %v", err)
		}
	}()
	if _, err := f.WriteString(line + "\n"); err != nil {
		t.Fatalf("append progress: %v", err)
	}
}
