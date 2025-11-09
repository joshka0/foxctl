package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/cache"
	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/envelope"
	memstore "github.com/jkatigb/agentctl/internal/memory"
)

func TestMemoryRecentAndCacheCommands(t *testing.T) {
	cfg := setupMemoryTestEnv(t)
	ctx := context.Background()

	cacheStore, err := cache.Open(ctx, cfg.Paths.Cache, cache.Options{
		AutoTTL: cfg.Memory.AutoCacheTTL,
		CASPath: cfg.Paths.CAS,
	})
	if err != nil {
		t.Fatalf("open cache store: %v", err)
	}
	defer cacheStore.Close()

	entry := cache.Entry{
		CacheKey:     "sha256:test",
		SkillName:    "text/grep",
		SkillVersion: "0.1.0",
		Workspace:    cfg.Home,
		Result:       []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`),
		CreatedAt:    time.Now().UTC(),
		ExpiresAt:    time.Now().UTC().Add(24 * time.Hour),
		Digests:      []string{},
	}
	if err := cacheStore.Put(ctx, entry); err != nil {
		t.Fatalf("cache put: %v", err)
	}

	// memory recent
	recentCmd := newMemoryRecentCommand()
	recentCmd.SetOut(&bytes.Buffer{})
	recentCmd.SetErr(&bytes.Buffer{})
	recentCmd.SetArgs([]string{"--workspace", cfg.Home})
	if err := recentCmd.Execute(); err != nil {
		t.Fatalf("memory recent: %v", err)
	}

	// memory cache
	cacheCmd := newMemoryCacheCommand()
	cacheOut := &bytes.Buffer{}
	cacheCmd.SetOut(cacheOut)
	cacheCmd.SetErr(&bytes.Buffer{})
	cacheCmd.SetArgs([]string{entry.CacheKey})
	if err := cacheCmd.Execute(); err != nil {
		t.Fatalf("memory cache: %v", err)
	}
	var env envelope.Envelope
	if err := json.Unmarshal(cacheOut.Bytes(), &env); err != nil {
		t.Fatalf("decode cache envelope: %v", err)
	}
	if env.Command != "agentctl.memory.cache" {
		t.Fatalf("unexpected command %s", env.Command)
	}
}

func TestMemoryListAndGetCommands(t *testing.T) {
	cfg := setupMemoryTestEnv(t)
	ctx := context.Background()
	store, err := memstore.Open(ctx, cfg.Paths.Cache, cfg.Paths.CAS)
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer store.Close()
	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`)
	if _, err := store.SaveFromResult(ctx, "alpha", "result", cfg.Home, "alpha summary", result); err != nil {
		t.Fatalf("save memory: %v", err)
	}

	listCmd := newMemoryListCommand()
	listOut := &bytes.Buffer{}
	listCmd.SetOut(listOut)
	listCmd.SetErr(&bytes.Buffer{})
	listCmd.SetArgs([]string{"--workspace", cfg.Home})
	if err := listCmd.Execute(); err != nil {
		t.Fatalf("memory list: %v", err)
	}

	getCmd := newMemoryGetCommand()
	getOut := &bytes.Buffer{}
	getCmd.SetOut(getOut)
	getCmd.SetErr(&bytes.Buffer{})
	getCmd.SetArgs([]string{"--workspace", cfg.Home, "alpha"})
	if err := getCmd.Execute(); err != nil {
		t.Fatalf("memory get: %v", err)
	}
	var env envelope.Envelope
	if err := json.Unmarshal(getOut.Bytes(), &env); err != nil {
		t.Fatalf("decode get envelope: %v", err)
	}
	if env.Meta.Memory == nil || env.Meta.Memory.Name != "alpha" {
		t.Fatalf("expected memory metadata for alpha")
	}
}

func TestMemoryPutCommand(t *testing.T) {
	cfg := setupMemoryTestEnv(t)
	payload := `{"version":1,"status":"ok","command":"test","data":{"value":1},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`
	cmd := newMemoryPutCommand()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"--name", "stored",
		"--workspace", cfg.Home,
		"--data", payload,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("memory put: %v", err)
	}
	store, err := memstore.Open(context.Background(), cfg.Paths.Cache, cfg.Paths.CAS)
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer store.Close()
	entry, err := store.Get(context.Background(), "stored", cfg.Home)
	if err != nil {
		t.Fatalf("memory get stored: %v", err)
	}
	if entry.Summary == "" {
		t.Fatalf("expected summary set")
	}
}

func setupMemoryTestEnv(t *testing.T) config.Config {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	dirs := []string{cfg.Home, cfg.Paths.CAS, cfg.Paths.Jobs, cfg.Paths.Cache, cfg.Paths.Skills}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	return cfg
}
