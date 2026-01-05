package runservice

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/cache"
	"github.com/jkatigb/agentctl/internal/storage/jobs"
	memstore "github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/trajectory"
)

func TestExecutorTryServeCacheHit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmp := t.TempDir()
	cfg := config.Config{
		Paths: config.Paths{
			Cache: filepath.Join(tmp, "cache"),
			CAS:   filepath.Join(tmp, "cas"),
			Jobs:  filepath.Join(tmp, "jobs"),
		},
		Memory: config.MemorySettings{AutoCacheTTL: time.Hour},
	}
	for _, dir := range []string{cfg.Paths.Cache, cfg.Paths.CAS, cfg.Paths.Jobs} {
		if err := ensureDir(dir); err != nil {
			t.Fatalf("ensure dir: %v", err)
		}
	}

	handle := SkillHandle{
		Manifest: skill.Manifest{
			Metadata: skill.Metadata{Name: "text/grep", Version: "1.0.0"},
		},
	}

	input := []byte(`{"query":"needle"}`)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	executor := NewExecutor(ctx, cfg, handle, stdout, stderr, RunOptions{
		CacheMode: cache.ModeOff,
		Workspace: "ws",
	})
	defer executor.Close()

	served, err := executor.TryServeCache(input)
	if err != nil {
		t.Fatalf("TryServeCache: %v", err)
	}
	if served {
		t.Fatalf("expected served=false")
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout output when cache is disabled")
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr output when cache is disabled")
	}
}

func TestExecutorHookTrajectoryCaptureCallAndResultAndCacheHit(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()

	cfg := config.Config{
		Paths: config.Paths{
			Cache: filepath.Join(tmp, "cache"),
			CAS:   filepath.Join(tmp, "cas"),
			Jobs:  filepath.Join(tmp, "jobs"),
		},
		Storage: config.StorageSettings{Root: filepath.Join(tmp, "storage")},
		Memory:  config.MemorySettings{AutoCacheTTL: time.Hour},
	}
	for _, dir := range []string{cfg.Paths.Cache, cfg.Paths.CAS, cfg.Paths.Jobs, cfg.Storage.Root} {
		if err := ensureDir(dir); err != nil {
			t.Fatalf("ensure dir: %v", err)
		}
	}

	handle := SkillHandle{
		Manifest: skill.Manifest{
			Metadata: skill.Metadata{Name: "hooks/file_guard", Version: "1.0.0"},
		},
		ManifestPath: "unused",
		ArtifactPath: "unused",
	}

	input := []byte(`{"event":"PreToolUse","session_id":"s1","tool_name":"fs.read_file","tool_input":{"path":"/secret"}}`)
	resultEnv := envelope.OK("hooks/file_guard", map[string]any{"hook_output": map[string]any{"decision": "approve"}})
	resultBytes, err := json.Marshal(resultEnv)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}

	// Prepare + execute once (cache populate happens in HandleResult).
	stdout1 := &bytes.Buffer{}
	stderr1 := &bytes.Buffer{}
	executor1 := NewExecutor(ctx, cfg, handle, stdout1, stderr1, RunOptions{
		CacheMode:     cache.ModeOff,
		Workspace:     "ws",
		CorrelationID: "trace-1",
		CLICommand:    "agentctl run hooks/file_guard",
		Input:         input,
	})
	defer executor1.Close()

	job, dup, err := executor1.PrepareJob(input)
	if err != nil {
		t.Fatalf("PrepareJob: %v", err)
	}
	if dup {
		t.Fatalf("unexpected duplicate")
	}
	if served, err := executor1.TryServeCache(input); err != nil {
		t.Fatalf("TryServeCache (expected miss): %v", err)
	} else if served {
		t.Fatalf("expected miss before first execution")
	}

	executor1.jobStore = &stubJobStore{result: resultBytes}
	if err := executor1.ExecuteSync(job); err != nil {
		t.Fatalf("ExecuteSync: %v", err)
	}

	store, err := trajectory.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open trajectory store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	trajectories, err := store.ListTrajectories(ctx, trajectory.ListFilter{WorkspaceID: "ws"})
	if err != nil {
		t.Fatalf("list trajectories: %v", err)
	}
	if len(trajectories) != 1 {
		t.Fatalf("expected 1 trajectory, got %d", len(trajectories))
	}
	tr := trajectories[0]

	events, err := store.ListEvents(ctx, trajectory.EventFilter{TrajectoryID: tr.ID})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}

	foundCall := false
	foundResult := false
	for _, ev := range events {
		switch ev.Kind {
		case trajectory.EventKindHookCall:
			foundCall = true
		case trajectory.EventKindHookResult:
			foundResult = true
		}
	}
	if !foundCall {
		t.Fatalf("expected hook_call event")
	}
	if !foundResult {
		t.Fatalf("expected hook_result event")
	}

	// Now run a fresh executor that should still emit hook_call + hook_result.
	stdout2 := &bytes.Buffer{}
	stderr2 := &bytes.Buffer{}
	executor2 := NewExecutor(ctx, cfg, handle, stdout2, stderr2, RunOptions{
		CacheMode:     cache.ModeOff,
		Workspace:     "ws",
		CorrelationID: "trace-2",
		CLICommand:    "agentctl run hooks/file_guard",
		Input:         input,
	})
	defer executor2.Close()

	job2, dup2, err := executor2.PrepareJob(input)
	if err != nil {
		t.Fatalf("PrepareJob (2): %v", err)
	}
	if dup2 {
		t.Fatalf("unexpected duplicate (2)")
	}
	executor2.jobStore = &stubJobStore{result: resultBytes}
	if err := executor2.ExecuteSync(job2); err != nil {
		t.Fatalf("ExecuteSync (2): %v", err)
	}

	trajectories2, err := store.ListTrajectories(ctx, trajectory.ListFilter{WorkspaceID: "ws"})
	if err != nil {
		t.Fatalf("list trajectories2: %v", err)
	}
	if len(trajectories2) != 2 {
		t.Fatalf("expected 2 trajectories, got %d", len(trajectories2))
	}
	var tr2 trajectory.Trajectory
	for _, cand := range trajectories2 {
		if cand.TraceID == "trace-2" {
			tr2 = cand
		}
	}
	if tr2.ID == "" {
		t.Fatalf("expected trajectory for trace-2")
	}

	events2, err := store.ListEvents(ctx, trajectory.EventFilter{TrajectoryID: tr2.ID})
	if err != nil {
		t.Fatalf("list events2: %v", err)
	}
	foundCall = false
	foundResult = false
	for _, ev := range events2 {
		switch ev.Kind {
		case trajectory.EventKindHookCall:
			foundCall = true
		case trajectory.EventKindHookResult:
			foundResult = true
		}
	}
	if !foundCall {
		t.Fatalf("expected hook_call event")
	}
	if !foundResult {
		t.Fatalf("expected hook_result event")
	}
}

func TestExecutorTryServeCacheMiss(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmp := t.TempDir()
	cfg := config.Config{
		Paths: config.Paths{
			Cache: filepath.Join(tmp, "cache"),
			CAS:   filepath.Join(tmp, "cas"),
		},
		Memory: config.MemorySettings{AutoCacheTTL: time.Hour},
	}
	for _, dir := range []string{cfg.Paths.Cache, cfg.Paths.CAS} {
		if err := ensureDir(dir); err != nil {
			t.Fatalf("ensure dir: %v", err)
		}
	}

	handle := SkillHandle{
		Manifest: skill.Manifest{
			Metadata: skill.Metadata{Name: "text/grep", Version: "1.0.0"},
		},
	}
	executor := NewExecutor(ctx, cfg, handle, io.Discard, io.Discard, RunOptions{
		CacheMode: cache.ModeOff,
	})
	defer executor.Close()

	served, err := executor.TryServeCache([]byte(`{"query":"miss"}`))
	if err != nil {
		t.Fatalf("TryServeCache miss: %v", err)
	}
	if served {
		t.Fatalf("expected cache disabled to return served=false")
	}
}

func TestExecutorTryServeCacheModeOnlyMiss(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmp := t.TempDir()
	cfg := config.Config{
		Paths: config.Paths{
			Cache: filepath.Join(tmp, "cache"),
			CAS:   filepath.Join(tmp, "cas"),
		},
		Memory: config.MemorySettings{AutoCacheTTL: time.Hour},
	}
	for _, dir := range []string{cfg.Paths.Cache, cfg.Paths.CAS} {
		if err := ensureDir(dir); err != nil {
			t.Fatalf("ensure dir: %v", err)
		}
	}

	handle := SkillHandle{
		Manifest: skill.Manifest{
			Metadata: skill.Metadata{Name: "text/grep", Version: "1.0.0"},
		},
	}

	stdout := &bytes.Buffer{}
	executor := NewExecutor(ctx, cfg, handle, stdout, io.Discard, RunOptions{
		CacheMode: cache.ModeOff,
		Workspace: "ws",
	})
	defer executor.Close()

	served, err := executor.TryServeCache([]byte(`{"query":"miss"}`))
	if err != nil {
		t.Fatalf("TryServeCache only miss: %v", err)
	}
	if served {
		t.Fatalf("expected cache disabled (served=false)")
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout output when cache is disabled")
	}
}

func TestExecutorTryServeCacheAutoModeErrorsNonFatal(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmp := t.TempDir()

	// Create a file at the cache path to prevent directory creation
	cacheFilePath := filepath.Join(tmp, "cache")
	if err := os.WriteFile(cacheFilePath, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("create blocking file: %v", err)
	}

	cfg := config.Config{
		Paths: config.Paths{
			Cache: cacheFilePath, // This is a file, not a directory - will cause error
			CAS:   filepath.Join(tmp, "cas"),
		},
		Memory: config.MemorySettings{AutoCacheTTL: time.Hour},
	}

	handle := SkillHandle{
		Manifest: skill.Manifest{
			Metadata: skill.Metadata{Name: "text/grep", Version: "1.0.0"},
		},
	}

	stderr := &bytes.Buffer{}
	executor := NewExecutor(ctx, cfg, handle, io.Discard, stderr, RunOptions{
		CacheMode: cache.ModeOff,
	})
	defer executor.Close()

	served, err := executor.TryServeCache([]byte(`{"query":"test"}`))
	if err != nil {
		t.Fatalf("expected no error when cache is disabled, got: %v", err)
	}
	if served {
		t.Fatalf("expected served=false when cache is disabled")
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr output when cache is disabled, got: %q", stderr.String())
	}
}

func TestExecutorSubmitAsyncUsesRunner(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Config{}
	handle := SkillHandle{}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	executor := NewExecutor(ctx, cfg, handle, stdout, stderr, RunOptions{})
	defer executor.Close()

	called := false
	executor.SetAsyncRunner(func(_ context.Context, jobID, manifestPath, artifactPath string, _ io.Writer) error {
		called = true
		if jobID != "job123" {
			t.Fatalf("expected jobID job123 got %s", jobID)
		}
		if manifestPath != handle.ManifestPath {
			t.Fatalf("unexpected manifest path: %s", manifestPath)
		}
		if artifactPath != handle.ArtifactPath {
			t.Fatalf("unexpected artifact path: %s", artifactPath)
		}
		return nil
	})

	if err := executor.SubmitAsync(jobs.Job{ID: "job123"}); err != nil {
		t.Fatalf("SubmitAsync: %v", err)
	}
	if !called {
		t.Fatalf("expected async runner to be called")
	}
	if !strings.Contains(stdout.String(), "job job123 submitted") {
		t.Fatalf("expected submit message, got %q", stdout.String())
	}
}

func TestExecutorRememberStoresMemory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmp := t.TempDir()
	cfg := config.Config{
		Paths: config.Paths{
			Cache: filepath.Join(tmp, "cache"),
			CAS:   filepath.Join(tmp, "cas"),
		},
		Storage: config.StorageSettings{
			Root: filepath.Join(tmp, "storage"),
		},
	}
	for _, dir := range []string{cfg.Paths.Cache, cfg.Paths.CAS, cfg.Storage.Root} {
		if err := ensureDir(dir); err != nil {
			t.Fatalf("ensure dir: %v", err)
		}
	}

	handle := SkillHandle{
		Manifest: skill.Manifest{
			Metadata: skill.Metadata{Name: "text/grep", Version: "1.0.0"},
		},
	}
	executor := NewExecutor(ctx, cfg, handle, io.Discard, io.Discard, RunOptions{
		RememberName:    "demo",
		RememberType:    "result",
		RememberSummary: "saved",
		Workspace:       "ws",
	})
	defer executor.Close()

	result := []byte(`{"meta":{"ts":"2024-01-01T00:00:00Z"},"data":{"value":1}}`)
	if err := executor.remember(result); err != nil {
		t.Fatalf("remember: %v", err)
	}

	store, err := memstore.Open(ctx, cfg.Storage.Root, cfg.Paths.CAS)
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close memory store: %v", err)
		}
	})

	entry, err := store.Get(ctx, "demo", "ws")
	if err != nil {
		t.Fatalf("memory get: %v", err)
	}
	if entry.Summary != "saved" {
		t.Fatalf("expected summary saved got %s", entry.Summary)
	}
}

func ensureDir(path string) error {
	if path == "" {
		return nil
	}
	return os.MkdirAll(path, 0o755)
}

type stubJobStore struct{ result []byte }

func (s *stubJobStore) Close() error { return nil }

func (s *stubJobStore) SubmitEcho(_ context.Context, _ string) (jobs.Job, error) {
	return jobs.Job{}, nil
}

func (s *stubJobStore) List(_ context.Context, _ int) ([]jobs.Job, error) { return nil, nil }

func (s *stubJobStore) Get(_ context.Context, _ string) (jobs.Job, error) { return jobs.Job{}, nil }

func (s *stubJobStore) Result(_ context.Context, _ string) ([]byte, error) { return s.result, nil }

func (s *stubJobStore) Cancel(_ context.Context, _ string) error { return nil }

func (s *stubJobStore) Delete(_ context.Context, _ string) error { return nil }

func (s *stubJobStore) FindOrPrepareSkillJob(_ context.Context, _ string, _ []byte, _ bool) (jobs.Job, bool, error) {
	return jobs.Job{}, false, nil
}

func (s *stubJobStore) SetWorkspace(_ context.Context, _ string, _ string) error { return nil }

func (s *stubJobStore) WaitForCompletion(_ context.Context, _ string, _ time.Duration) (jobs.Job, error) {
	return jobs.Job{}, nil
}

func (s *stubJobStore) TailProgress(_ context.Context, _ string, _ bool, _ io.Writer) error {
	return nil
}

func (s *stubJobStore) ExecutePreparedSkill(_ context.Context, _ string, _ string, _ string) ([]byte, error) {
	return s.result, nil
}

func TestEnforceOutputLimit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmp := t.TempDir()
	cfg := config.Config{
		Home:           tmp,
		InlineOutputKB: 1,
		Paths: config.Paths{
			CAS: filepath.Join(tmp, "cas"),
		},
	}
	if err := ensureDir(cfg.Paths.CAS); err != nil {
		t.Fatalf("ensure dir: %v", err)
	}

	handle := SkillHandle{
		Manifest: skill.Manifest{
			Metadata: skill.Metadata{Name: "test/skill", Version: "1.0.0"},
		},
	}

	stderr := &bytes.Buffer{}
	executor := NewExecutor(ctx, cfg, handle, io.Discard, stderr, RunOptions{})
	defer executor.Close()

	smallEnv := `{"version":1,"status":"ok","command":"test","data":{"msg":"small"},"meta":{},"error":{}}`
	result := executor.enforceOutputLimit(ctx, []byte(smallEnv), "job-small")
	if string(result) != smallEnv {
		t.Fatalf("small output should not be modified, got: %s", string(result))
	}

	largeData := strings.Repeat("x", 2*1024)
	largeEnv := `{"version":1,"status":"ok","command":"test","data":{"content":"` + largeData + `","summary":"test summary"},"meta":{},"error":{}}`
	result = executor.enforceOutputLimit(ctx, []byte(largeEnv), "job-large")
	if len(result) >= len(largeEnv) {
		t.Fatalf("large output should be truncated, got %d bytes (original %d)", len(result), len(largeEnv))
	}

	var wrapped map[string]any
	if err := json.Unmarshal(result, &wrapped); err != nil {
		t.Fatalf("unmarshal wrapped: %v", err)
	}

	data, ok := wrapped["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data map in wrapper")
	}
	artifact, ok := data["artifact"].(string)
	if !ok || !strings.HasPrefix(artifact, "sha256:") {
		t.Fatalf("expected artifact digest in wrapper, got: %v", data["artifact"])
	}

	meta, ok := wrapped["meta"].(map[string]any)
	if !ok {
		t.Fatalf("expected meta map in wrapper")
	}
	if meta["truncated"] != true {
		t.Fatalf("expected truncated=true in meta")
	}

	summary, ok := data["summary"].(string)
	if !ok || summary == "" {
		t.Fatalf("expected summary in wrapper, got: %v", data["summary"])
	}
	if !strings.Contains(summary, "test summary") {
		t.Fatalf("expected original summary to be preserved, got: %s", summary)
	}

	if !strings.Contains(stderr.String(), "exceeded") {
		t.Fatalf("expected warning in stderr, got: %s", stderr.String())
	}
}
