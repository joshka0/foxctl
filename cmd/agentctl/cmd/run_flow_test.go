package cmd

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

	"github.com/jkatigb/agentctl/internal/cache"
	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/envelope"
	"github.com/jkatigb/agentctl/internal/jobs"
	memstore "github.com/jkatigb/agentctl/internal/memory"
	"github.com/jkatigb/agentctl/internal/skill"
)

func TestRunExecutorTryServeCacheHit(t *testing.T) {
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
	store, err := cache.Open(ctx, cfg.Paths.Cache, cache.Options{AutoTTL: cfg.Memory.AutoCacheTTL, CASPath: cfg.Paths.CAS})
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}
	key, err := cache.BuildKey(handle.Manifest, input, nil)
	if err != nil {
		t.Fatalf("build key: %v", err)
	}
	result := []byte(`{"meta":{"ts":"2024-01-01T00:00:00Z"},"data":{"value":1}}`)
	if err := store.Put(ctx, cache.Entry{
		CacheKey:     key,
		SkillName:    handle.Manifest.Metadata.Name,
		SkillVersion: handle.Manifest.Metadata.Version,
		Workspace:    "ws",
		Result:       result,
	}); err != nil {
		t.Fatalf("populate cache: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close cache: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	executor := newRunExecutor(ctx, cfg, handle, stdout, stderr, RunOptions{
		CacheMode: cache.ModeAuto,
		Workspace: "ws",
	})
	defer executor.Close()

	served, err := executor.tryServeCache(input)
	if err != nil {
		t.Fatalf("tryServeCache: %v", err)
	}
	if !served {
		t.Fatalf("expected cache hit")
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Meta.Source != "cache" {
		t.Fatalf("expected meta.source=cache got %s", env.Meta.Source)
	}
	if env.Meta.CacheKey != key {
		t.Fatalf("expected cache key %s got %s", key, env.Meta.CacheKey)
	}
	if env.Meta.Workspace != "ws" {
		t.Fatalf("expected workspace ws got %s", env.Meta.Workspace)
	}
	if got := stderr.String(); !strings.Contains(got, "cache hit") {
		t.Fatalf("expected cache hit log, got %q", got)
	}
}

func TestRunExecutorSubmitAsyncUsesRunner(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Config{}
	handle := SkillHandle{}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	executor := newRunExecutor(ctx, cfg, handle, stdout, stderr, RunOptions{})
	defer executor.Close()

	called := false
	executor.asyncRunner = func(_ context.Context, jobID, manifestPath, artifactPath string, _ io.Writer) error {
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
	}

	if err := executor.submitAsync(jobs.Job{ID: "job123"}); err != nil {
		t.Fatalf("submitAsync: %v", err)
	}
	if !called {
		t.Fatalf("expected async runner to be called")
	}
	if !strings.Contains(stdout.String(), "job job123 submitted") {
		t.Fatalf("expected submit message in stdout, got %q", stdout.String())
	}
}

func TestRunExecutorRememberStoresMemory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmp := t.TempDir()
	cfg := config.Config{
		Paths: config.Paths{
			Cache: filepath.Join(tmp, "cache"),
			CAS:   filepath.Join(tmp, "cas"),
		},
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
	executor := newRunExecutor(ctx, cfg, handle, io.Discard, io.Discard, RunOptions{
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

	store, err := memstore.Open(ctx, cfg.Paths.Cache, cfg.Paths.CAS)
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

func TestRunExecutorHandleDuplicatePersistsCache(t *testing.T) {
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
			Metadata: skill.Metadata{Name: "echo", Version: "1.0.0"},
		},
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	executor := newRunExecutor(ctx, cfg, handle, stdout, stderr, RunOptions{
		CacheMode: cache.ModeAuto,
		Workspace: "ws",
	})
	defer executor.Close()

	input := []byte(`{"message":"hello"}`)
	served, err := executor.tryServeCache(input)
	if err != nil {
		t.Fatalf("tryServeCache: %v", err)
	}
	if served {
		t.Fatalf("expected cache miss for fresh input")
	}

	jobStore, err := jobs.Open(ctx, cfg.Paths.Jobs)
	if err != nil {
		t.Fatalf("open jobs: %v", err)
	}
	executor.jobStore = jobStore

	job, err := jobStore.SubmitEcho(ctx, "hello")
	if err != nil {
		t.Fatalf("submit echo: %v", err)
	}
	if job.ResultPath == "" {
		t.Fatalf("expected result path for completed job")
	}

	if err := executor.handleDuplicate(job); err != nil {
		t.Fatalf("handleDuplicate: %v", err)
	}

	entry, ok, err := executor.cacheStore.Get(ctx, executor.cacheKey)
	if err != nil {
		t.Fatalf("cache get: %v", err)
	}
	if !ok {
		t.Fatalf("expected cache entry after duplicate handling")
	}
	if entry.CacheKey != executor.cacheKey {
		t.Fatalf("unexpected cache key %s (want %s)", entry.CacheKey, executor.cacheKey)
	}
	if entry.SkillName != handle.Manifest.Metadata.Name {
		t.Fatalf("unexpected skill name %s", entry.SkillName)
	}
}

func ensureDir(path string) error {
	if path == "" {
		return nil
	}
	return os.MkdirAll(path, 0o755)
}
