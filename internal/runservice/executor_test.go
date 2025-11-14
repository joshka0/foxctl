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
	executor := NewExecutor(ctx, cfg, handle, stdout, stderr, RunOptions{
		CacheMode: cache.ModeAuto,
		Workspace: "ws",
	})
	defer executor.Close()

	served, err := executor.TryServeCache(input)
	if err != nil {
		t.Fatalf("TryServeCache: %v", err)
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
		CacheMode: cache.ModeAuto,
	})
	defer executor.Close()

	served, err := executor.TryServeCache([]byte(`{"query":"miss"}`))
	if err != nil {
		t.Fatalf("TryServeCache miss: %v", err)
	}
	if served {
		t.Fatalf("expected miss to return served=false")
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

func ensureDir(path string) error {
	if path == "" {
		return nil
	}
	return os.MkdirAll(path, 0o755)
}
