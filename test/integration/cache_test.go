//go:build integration

// Package integration provides integration tests for agentctl.
// These tests require the "integration" build tag to run.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/runservice"
	"github.com/jkatigb/agentctl/internal/storage/cache"
)

// TestCacheHitSameInput verifies that caching is currently disabled even if
// a cache entry exists.
func TestCacheHitSameInput(t *testing.T) {
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
		if err := mkdirAll(dir); err != nil {
			t.Fatalf("ensure dir: %v", err)
		}
	}

	manifest := skill.Manifest{
		Metadata: skill.Metadata{Name: "test/cache", Version: "1.0.0"},
	}
	handle := runservice.SkillHandle{Manifest: manifest}
	input := []byte(`{"query":"test-value"}`)

	// First run: should be a cache miss, populate the cache
	cacheStore, err := cache.Open(ctx, cfg.Paths.Cache, cache.Options{
		AutoTTL: cfg.Memory.AutoCacheTTL,
		CASPath: cfg.Paths.CAS,
	})
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}

	key, err := cache.BuildKey(manifest, input, nil)
	if err != nil {
		t.Fatalf("build key: %v", err)
	}

	// Simulate first run result and cache it
	firstResult := envelope.Envelope{
		Version: 1,
		Status:  "ok",
		Command: "test/cache",
		Data:    map[string]any{"result": "first-run"},
		Meta:    envelope.Meta{TS: time.Now().UTC().Format(time.RFC3339)},
	}
	firstResultBytes, err := json.Marshal(firstResult)
	if err != nil {
		t.Fatalf("marshal first result: %v", err)
	}

	if err := cacheStore.Put(ctx, cache.Entry{
		CacheKey:     key,
		SkillName:    manifest.Metadata.Name,
		SkillVersion: manifest.Metadata.Version,
		Workspace:    "ws",
		Result:       firstResultBytes,
	}); err != nil {
		t.Fatalf("cache put: %v", err)
	}
	if err := cacheStore.Close(); err != nil {
		t.Fatalf("close cache: %v", err)
	}

	// Second run: cache is disabled, so no hit should occur
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	executor := runservice.NewExecutor(ctx, cfg, handle, stdout, stderr, runservice.RunOptions{
		CacheMode: cache.ModeAuto,
		Workspace: "ws",
	})
	defer executor.Close()

	served, err := executor.TryServeCache(input)
	if err != nil {
		t.Fatalf("TryServeCache: %v", err)
	}
	if served {
		t.Fatalf("expected served=false (cache disabled)")
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout output (cache disabled)")
	}
}

// TestCacheMissDifferentInput verifies that different inputs produce different cache keys.
func TestCacheMissDifferentInput(t *testing.T) {
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
		if err := mkdirAll(dir); err != nil {
			t.Fatalf("ensure dir: %v", err)
		}
	}

	manifest := skill.Manifest{
		Metadata: skill.Metadata{Name: "test/cache", Version: "1.0.0"},
	}
	handle := runservice.SkillHandle{Manifest: manifest}

	// Populate cache with input1
	input1 := []byte(`{"query":"value-one"}`)
	cacheStore, err := cache.Open(ctx, cfg.Paths.Cache, cache.Options{
		AutoTTL: cfg.Memory.AutoCacheTTL,
		CASPath: cfg.Paths.CAS,
	})
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}

	key1, err := cache.BuildKey(manifest, input1, nil)
	if err != nil {
		t.Fatalf("build key1: %v", err)
	}

	cachedResult := envelope.Envelope{
		Version: 1,
		Status:  "ok",
		Command: "test/cache",
		Data:    map[string]any{"result": "cached-for-input1"},
		Meta:    envelope.Meta{TS: time.Now().UTC().Format(time.RFC3339)},
	}
	cachedResultBytes, err := json.Marshal(cachedResult)
	if err != nil {
		t.Fatalf("marshal cached result: %v", err)
	}

	if err := cacheStore.Put(ctx, cache.Entry{
		CacheKey:     key1,
		SkillName:    manifest.Metadata.Name,
		SkillVersion: manifest.Metadata.Version,
		Workspace:    "ws",
		Result:       cachedResultBytes,
	}); err != nil {
		t.Fatalf("cache put: %v", err)
	}
	if err := cacheStore.Close(); err != nil {
		t.Fatalf("close cache: %v", err)
	}

	// Query with input2 - cache is disabled, should be a miss
	input2 := []byte(`{"query":"value-two"}`)
	stdout := &bytes.Buffer{}
	executor := runservice.NewExecutor(ctx, cfg, handle, stdout, bytes.NewBuffer(nil), runservice.RunOptions{
		CacheMode: cache.ModeAuto,
		Workspace: "ws",
	})
	defer executor.Close()

	served, err := executor.TryServeCache(input2)
	if err != nil {
		t.Fatalf("TryServeCache: %v", err)
	}
	if served {
		t.Fatalf("expected served=false (cache disabled)")
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no output (cache disabled)")
	}

	// Verify keys are different
	key2, err := cache.BuildKey(manifest, input2, nil)
	if err != nil {
		t.Fatalf("build key2: %v", err)
	}
	if key1 == key2 {
		t.Errorf("expected different cache keys for different inputs")
	}
}

// TestCacheModeOff verifies that --cache=off skips all cache I/O.
func TestCacheModeOff(t *testing.T) {
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
		if err := mkdirAll(dir); err != nil {
			t.Fatalf("ensure dir: %v", err)
		}
	}

	manifest := skill.Manifest{
		Metadata: skill.Metadata{Name: "test/cache", Version: "1.0.0"},
	}
	handle := runservice.SkillHandle{Manifest: manifest}
	input := []byte(`{"query":"test"}`)

	// Populate cache
	cacheStore, err := cache.Open(ctx, cfg.Paths.Cache, cache.Options{
		AutoTTL: cfg.Memory.AutoCacheTTL,
		CASPath: cfg.Paths.CAS,
	})
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}

	key, err := cache.BuildKey(manifest, input, nil)
	if err != nil {
		t.Fatalf("build key: %v", err)
	}

	cachedResult := envelope.Envelope{
		Version: 1,
		Status:  "ok",
		Command: "test/cache",
		Data:    map[string]any{"result": "cached"},
		Meta:    envelope.Meta{TS: time.Now().UTC().Format(time.RFC3339)},
	}
	cachedResultBytes, err := json.Marshal(cachedResult)
	if err != nil {
		t.Fatalf("marshal cached result: %v", err)
	}

	if err := cacheStore.Put(ctx, cache.Entry{
		CacheKey:     key,
		SkillName:    manifest.Metadata.Name,
		SkillVersion: manifest.Metadata.Version,
		Workspace:    "ws",
		Result:       cachedResultBytes,
	}); err != nil {
		t.Fatalf("cache put: %v", err)
	}
	if err := cacheStore.Close(); err != nil {
		t.Fatalf("close cache: %v", err)
	}

	// Query with ModeOff - should NOT hit cache even though entry exists
	stdout := &bytes.Buffer{}
	executor := runservice.NewExecutor(ctx, cfg, handle, stdout, bytes.NewBuffer(nil), runservice.RunOptions{
		CacheMode: cache.ModeOff,
		Workspace: "ws",
	})
	defer executor.Close()

	served, err := executor.TryServeCache(input)
	if err != nil {
		t.Fatalf("TryServeCache: %v", err)
	}
	if served {
		t.Fatalf("expected cache to be skipped with ModeOff")
	}
	if stdout.Len() > 0 {
		t.Errorf("expected no output with ModeOff, got: %s", stdout.String())
	}
}

// TestCacheModeOnlyMiss verifies that cache-only mode does not emit a cache miss
// envelope because caching is disabled.
func TestCacheModeOnlyMiss(t *testing.T) {
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
		if err := mkdirAll(dir); err != nil {
			t.Fatalf("ensure dir: %v", err)
		}
	}

	manifest := skill.Manifest{
		Metadata: skill.Metadata{Name: "test/cache", Version: "1.0.0"},
	}
	handle := runservice.SkillHandle{Manifest: manifest}
	input := []byte(`{"query":"not-in-cache"}`)

	// Query with ModeOnly on empty cache - cache is disabled
	stdout := &bytes.Buffer{}
	executor := runservice.NewExecutor(ctx, cfg, handle, stdout, bytes.NewBuffer(nil), runservice.RunOptions{
		CacheMode: cache.ModeOnly,
		Workspace: "ws",
	})
	defer executor.Close()

	served, err := executor.TryServeCache(input)
	if err != nil {
		t.Fatalf("TryServeCache: %v", err)
	}
	if served {
		t.Fatalf("expected served=false (cache disabled)")
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout output (cache disabled)")
	}
}

// TestCacheKeyDeterminism verifies that cache keys are deterministic across
// reordered JSON and digest lists.
func TestCacheKeyDeterminism(t *testing.T) {
	t.Parallel()

	manifest := skill.Manifest{
		Metadata: skill.Metadata{Name: "test/determinism", Version: "2.0.0"},
	}

	// Same logical input, different JSON key order
	input1 := []byte(`{"a":1,"b":2,"c":3}`)
	input2 := []byte(`{"c":3,"a":1,"b":2}`)

	// Same digests, different order
	digests1 := []string{"sha256:aaa", "sha256:bbb", "sha256:ccc"}
	digests2 := []string{"sha256:ccc", "sha256:aaa", "sha256:bbb"}

	key1, err := cache.BuildKey(manifest, input1, digests1)
	if err != nil {
		t.Fatalf("build key1: %v", err)
	}

	key2, err := cache.BuildKey(manifest, input2, digests2)
	if err != nil {
		t.Fatalf("build key2: %v", err)
	}

	if key1 != key2 {
		t.Errorf("expected deterministic keys:\n  key1=%s\n  key2=%s", key1, key2)
	}
}

func mkdirAll(path string) error {
	if path == "" {
		return nil
	}
	return os.MkdirAll(path, 0o755)
}
