package persist

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/quick"
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

func TestUpdateStateAllowsDocumentedLifecycleTransitions(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	tests := []struct {
		name       string
		from       types.State
		target     types.State
		errMsg     string
		resultPath string
	}{
		{name: "queued to running", from: types.StateQueued, target: types.StateRunning},
		{name: "queued to error", from: types.StateQueued, target: types.StateError, errMsg: "prepare failed"},
		{name: "queued to canceled", from: types.StateQueued, target: types.StateCanceled},
		{name: "running to ok", from: types.StateRunning, target: types.StateOK, resultPath: "/tmp/result.json"},
		{name: "running to error", from: types.StateRunning, target: types.StateError, errMsg: "run failed"},
		{name: "running to canceled", from: types.StateRunning, target: types.StateCanceled},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := insertTestJobWithState(t, ctx, store, fmt.Sprintf("job-transition-%d", i), tt.from)

			if err := store.UpdateState(ctx, job.ID, tt.target, tt.errMsg, tt.resultPath); err != nil {
				t.Fatalf("UpdateState(%s -> %s): %v", tt.from, tt.target, err)
			}

			got, err := store.Get(ctx, job.ID)
			if err != nil {
				t.Fatalf("get job: %v", err)
			}
			if got.State != tt.target {
				t.Fatalf("state=%s want %s", got.State, tt.target)
			}
			if got.Error != tt.errMsg {
				t.Fatalf("error=%q want %q", got.Error, tt.errMsg)
			}
			if tt.resultPath != "" && got.ResultPath != tt.resultPath {
				t.Fatalf("result_path=%q want %q", got.ResultPath, tt.resultPath)
			}
		})
	}
}

func TestUpdateStatePropertyTerminalJobsCannotTransitionToDifferentState(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)
	caseID := 0

	terminalStates := []types.State{
		types.StateOK,
		types.StateError,
		types.StateCanceled,
	}
	targetStates := []types.State{
		types.StateQueued,
		types.StateRunning,
		types.StateOK,
		types.StateError,
		types.StateCanceled,
	}

	property := func(terminalSeed, targetSeed uint8) bool {
		terminal := terminalStates[int(terminalSeed)%len(terminalStates)]
		target := targetStates[int(targetSeed)%len(targetStates)]
		if target == terminal {
			return true
		}

		jobID := fmt.Sprintf("job-terminal-%d", caseID)
		caseID++
		job := insertTestJobWithState(t, ctx, store, jobID, terminal)
		before, err := store.Get(ctx, job.ID)
		if err != nil {
			t.Logf("get before: %v", err)
			return false
		}

		err = store.UpdateState(ctx, job.ID, target, "late mutation", "/tmp/late-result.json")
		if !errors.Is(err, types.ErrInvalidState) {
			t.Logf("UpdateState(%s -> %s) err=%v want ErrInvalidState", terminal, target, err)
			return false
		}
		after, err := store.Get(ctx, job.ID)
		if err != nil {
			t.Logf("get after: %v", err)
			return false
		}
		if after.State != before.State || after.Error != before.Error || after.ResultPath != before.ResultPath {
			t.Logf("terminal job mutated: before=%+v after=%+v", before, after)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("terminal job state-machine property failed: %v", err)
	}
}

func TestUpdateStateRejectsTerminalSameStateMetadataMutation(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	for _, terminal := range []types.State{types.StateOK, types.StateError, types.StateCanceled} {
		t.Run(string(terminal), func(t *testing.T) {
			job := insertTestJobWithState(t, ctx, store, "job-terminal-same-"+string(terminal), terminal)
			before, err := store.Get(ctx, job.ID)
			if err != nil {
				t.Fatalf("get before: %v", err)
			}

			err = store.UpdateState(ctx, job.ID, terminal, "late mutation", "/tmp/late-result.json")
			if !errors.Is(err, types.ErrInvalidState) {
				t.Fatalf("UpdateState(%s -> %s) error=%v want ErrInvalidState", terminal, terminal, err)
			}

			after, err := store.Get(ctx, job.ID)
			if err != nil {
				t.Fatalf("get after: %v", err)
			}
			if after.State != before.State ||
				after.Error != before.Error ||
				after.ResultPath != before.ResultPath ||
				!after.UpdatedAt.Equal(before.UpdatedAt) {
				t.Fatalf("terminal job mutated: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestInsertJobRejectsUnknownStateWithoutPersisting(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	now := time.Now().UTC()
	job := types.Job{
		ID:        "job-invalid-state",
		Command:   "test",
		ArgsJSON:  "{}",
		ArgsHash:  "invalid-state-hash",
		State:     types.State("paused"),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.InsertJob(ctx, job); err == nil {
		t.Fatal("expected invalid state error")
	} else if !errors.Is(err, types.ErrInvalidState) {
		t.Fatalf("error=%v want ErrInvalidState", err)
	}
	if _, err := store.Get(ctx, job.ID); !errors.Is(err, types.ErrNotFound) {
		t.Fatalf("invalid-state job lookup error=%v want ErrNotFound", err)
	}
}

func TestFindOrInsertJobRejectsUnknownStateWithoutPersisting(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	now := time.Now().UTC()
	job := types.Job{
		ID:        "job-invalid-find-or-insert",
		Command:   "test",
		ArgsJSON:  "{}",
		ArgsHash:  "invalid-find-or-insert-hash",
		State:     types.State("paused"),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, _, err := store.FindOrInsertJob(ctx, job); err == nil {
		t.Fatal("expected invalid state error")
	} else if !errors.Is(err, types.ErrInvalidState) {
		t.Fatalf("error=%v want ErrInvalidState", err)
	}
	if _, err := store.Get(ctx, job.ID); !errors.Is(err, types.ErrNotFound) {
		t.Fatalf("invalid-state job lookup error=%v want ErrNotFound", err)
	}
}

func TestInsertJobRejectsGeneratedUnknownStates(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	prop := func(raw string) bool {
		token := safeJobToken(raw)
		now := time.Now().UTC()
		job := types.Job{
			ID:        "job-generated-" + token,
			Command:   "test",
			ArgsJSON:  "{}",
			ArgsHash:  "hash-generated-" + token,
			State:     types.State("unknown:" + raw),
			CreatedAt: now,
			UpdatedAt: now,
		}
		err := store.InsertJob(ctx, job)
		if !errors.Is(err, types.ErrInvalidState) {
			t.Logf("InsertJob state=%q err=%v want ErrInvalidState", job.State, err)
			return false
		}
		_, getErr := store.Get(ctx, job.ID)
		return errors.Is(getErr, types.ErrNotFound)
	}

	if err := quick.Check(prop, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("generated unknown state was accepted: %v", err)
	}
}

func TestJobReadsRejectCorruptPersistedState(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)
	sqlStore := store.(*sqlStore)

	now := time.Now().UTC()
	job := types.Job{
		ID:        "job-corrupt-state",
		Command:   "test",
		ArgsJSON:  "{}",
		ArgsHash:  "corrupt-state-hash",
		State:     types.StateQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.InsertJob(ctx, job); err != nil {
		t.Fatalf("insert job: %v", err)
	}

	if _, err := sqlStore.db.ExecContext(ctx, `
		UPDATE jobs SET state = $1 WHERE id = $2
	`, "paused", job.ID); err != nil {
		t.Fatalf("corrupt job state: %v", err)
	}

	if _, err := store.Get(ctx, job.ID); !errors.Is(err, types.ErrInvalidState) {
		t.Fatalf("Get() error=%v, want ErrInvalidState", err)
	}
	if _, err := store.List(ctx, 10); !errors.Is(err, types.ErrInvalidState) {
		t.Fatalf("List() error=%v, want ErrInvalidState", err)
	}
	if _, err := store.FindDuplicateJob(ctx, job.ArgsHash); !errors.Is(err, types.ErrInvalidState) {
		t.Fatalf("FindDuplicateJob() error=%v, want ErrInvalidState", err)
	}
	if _, _, err := store.FindOrInsertJob(ctx, types.Job{
		ID:        "job-corrupt-state-duplicate",
		Command:   job.Command,
		ArgsJSON:  job.ArgsJSON,
		ArgsHash:  job.ArgsHash,
		State:     types.StateQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}); !errors.Is(err, types.ErrInvalidState) {
		t.Fatalf("FindOrInsertJob() error=%v, want ErrInvalidState", err)
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

func TestFindOrInsertJobReturnsExistingDuplicateWithExpiresAt(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	firstCreated := time.Now().UTC().Add(-time.Minute)
	firstExpires := firstCreated.Add(15 * time.Minute)
	first := types.Job{
		ID:        "job-original-expires",
		Command:   "test",
		ArgsJSON:  `{"path":"main.go"}`,
		ArgsHash:  "same-expiring-args",
		State:     types.StateQueued,
		CreatedAt: firstCreated,
		UpdatedAt: firstCreated,
		ExpiresAt: firstExpires,
	}
	inserted, found, err := store.FindOrInsertJob(ctx, first)
	if err != nil {
		t.Fatalf("find or insert first: %v", err)
	}
	if found {
		t.Fatal("first insert unexpectedly found an existing job")
	}
	if !inserted.ExpiresAt.Equal(firstExpires) {
		t.Fatalf("inserted expires_at=%v want %v", inserted.ExpiresAt, firstExpires)
	}

	second := types.Job{
		ID:        "job-duplicate-expires",
		Command:   "test",
		ArgsJSON:  `{"path":"main.go"}`,
		ArgsHash:  first.ArgsHash,
		State:     types.StateQueued,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	duplicate, found, err := store.FindOrInsertJob(ctx, second)
	if err != nil {
		t.Fatalf("find or insert duplicate: %v", err)
	}
	if !found {
		t.Fatal("second insert with same args hash should return existing job")
	}
	if duplicate.ID != first.ID {
		t.Fatalf("duplicate id=%q want original id %q", duplicate.ID, first.ID)
	}
	if !duplicate.ExpiresAt.Equal(firstExpires) {
		t.Fatalf("duplicate expires_at=%v want %v", duplicate.ExpiresAt, firstExpires)
	}
}

func TestFindOrInsertJobReturnsExistingJobWithoutInsertingDuplicate(t *testing.T) {
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

	firstCreated := time.Now().UTC().Add(-time.Minute)
	first := types.Job{
		ID:        "job-original",
		Command:   "test",
		ArgsJSON:  `{"path":"main.go"}`,
		ArgsHash:  "same-args",
		State:     types.StateQueued,
		CreatedAt: firstCreated,
		UpdatedAt: firstCreated,
	}
	inserted, found, err := store.FindOrInsertJob(ctx, first)
	if err != nil {
		t.Fatalf("find or insert first: %v", err)
	}
	if found {
		t.Fatal("first insert unexpectedly found an existing job")
	}
	if inserted.ID != first.ID {
		t.Fatalf("first inserted id=%q want %q", inserted.ID, first.ID)
	}

	secondCreated := time.Now().UTC()
	second := types.Job{
		ID:        "job-duplicate",
		Command:   "test",
		ArgsJSON:  `{"path":"main.go"}`,
		ArgsHash:  "same-args",
		State:     types.StateQueued,
		CreatedAt: secondCreated,
		UpdatedAt: secondCreated,
	}
	duplicate, found, err := store.FindOrInsertJob(ctx, second)
	if err != nil {
		t.Fatalf("find or insert duplicate: %v", err)
	}
	if !found {
		t.Fatal("second insert with same args hash should return existing job")
	}
	if duplicate.ID != first.ID {
		t.Fatalf("duplicate id=%q want original id %q", duplicate.ID, first.ID)
	}
	if duplicate.ArgsHash != first.ArgsHash {
		t.Fatalf("duplicate args hash=%q want %q", duplicate.ArgsHash, first.ArgsHash)
	}

	jobs, err := store.List(ctx, 10)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("jobs len=%d want exactly one durable job after dedupe: %+v", len(jobs), jobs)
	}
	if jobs[0].ID != first.ID {
		t.Fatalf("stored job id=%q want original id %q", jobs[0].ID, first.ID)
	}
	if _, err := store.Get(ctx, second.ID); !errors.Is(err, types.ErrNotFound) {
		t.Fatalf("duplicate job id lookup error=%v want ErrNotFound", err)
	}
}

func TestFindDuplicateJobFindsNewestByArgsHash(t *testing.T) {
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
	older := types.Job{
		ID:        "job-older",
		Command:   "test",
		ArgsJSON:  `{"path":"main.go"}`,
		ArgsHash:  "same-args",
		State:     types.StateQueued,
		CreatedAt: now.Add(-time.Minute),
		UpdatedAt: now.Add(-time.Minute),
	}
	newer := types.Job{
		ID:        "job-newer",
		Command:   "test",
		ArgsJSON:  `{"path":"main.go"}`,
		ArgsHash:  "same-args",
		State:     types.StateQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}
	other := types.Job{
		ID:        "job-other",
		Command:   "test",
		ArgsJSON:  `{"path":"other.go"}`,
		ArgsHash:  "other-args",
		State:     types.StateQueued,
		CreatedAt: now.Add(time.Minute),
		UpdatedAt: now.Add(time.Minute),
	}
	for _, job := range []types.Job{older, newer, other} {
		if err := store.InsertJob(ctx, job); err != nil {
			t.Fatalf("insert %s: %v", job.ID, err)
		}
	}

	duplicate, err := store.FindDuplicateJob(ctx, "same-args")
	if err != nil {
		t.Fatalf("find duplicate: %v", err)
	}
	if duplicate.ID != newer.ID {
		t.Fatalf("duplicate id=%q want newest matching id %q", duplicate.ID, newer.ID)
	}
	if duplicate.ArgsHash != "same-args" {
		t.Fatalf("duplicate args hash=%q want same-args", duplicate.ArgsHash)
	}
	if _, err := store.FindDuplicateJob(ctx, "missing-args"); !errors.Is(err, types.ErrNotFound) {
		t.Fatalf("missing duplicate error=%v want ErrNotFound", err)
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

func openTestStore(t *testing.T, ctx context.Context) Store {
	t.Helper()
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
	return store
}

func insertTestJobWithState(t *testing.T, ctx context.Context, store Store, id string, state types.State) types.Job {
	t.Helper()
	now := time.Now().UTC()
	job := types.Job{
		ID:        id,
		Command:   "test",
		ArgsJSON:  "{}",
		ArgsHash:  "hash-" + id,
		State:     state,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.InsertJob(ctx, job); err != nil {
		t.Fatalf("insert %s: %v", id, err)
	}
	return job
}

func safeJobToken(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		}
		if b.Len() >= 12 {
			break
		}
	}
	if b.Len() == 0 {
		return "empty"
	}
	return b.String()
}
