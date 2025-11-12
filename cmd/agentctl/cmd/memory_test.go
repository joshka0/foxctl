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
	"github.com/spf13/cobra"
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
	defer func() {
		_ = cacheStore.Close()
	}()

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
	defer func() {
		_ = store.Close()
	}()
	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`)
	if _, err := store.SaveFromResult(ctx, "alpha", "result", cfg.Home, "alpha summary", result); err != nil {
		t.Fatalf("save memory: %v", err)
	}

	listEnv := runMemoryCommand(t, cfg, newMemoryListCommand(), "--workspace", cfg.Home)
	data := listEnv.Data.(map[string]any)
	entries := data["entries"].([]any)
	if len(entries) == 0 {
		t.Fatalf("expected entries in list")
	}
	first := entries[0].(map[string]any)
	if _, ok := first["access_count"]; !ok {
		t.Fatalf("expected access_count metadata")
	}

	getEnv := runMemoryCommand(t, cfg, newMemoryGetCommand(), "--workspace", cfg.Home, "alpha")
	if getEnv.Meta.Memory == nil || getEnv.Meta.Memory.Name != "alpha" {
		t.Fatalf("expected memory metadata for alpha")
	}
}

func TestMemoryPutCommand(t *testing.T) {
	cfg := setupMemoryTestEnv(t)
	payload := `{"version":1,"status":"ok","command":"test","data":{"value":1},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`
	runMemoryCommand(t, cfg, newMemoryPutCommand(),
		"--name", "stored",
		"--workspace", cfg.Home,
		"--data", payload,
	)
	store, err := memstore.Open(context.Background(), cfg.Paths.Cache, cfg.Paths.CAS)
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()
	entry, err := store.Get(context.Background(), "stored", cfg.Home)
	if err != nil {
		t.Fatalf("memory get stored: %v", err)
	}
	if entry.Summary == "" {
		t.Fatalf("expected summary set")
	}
}

func TestMemorySearchAndRelevantCommands(t *testing.T) {
	cfg := setupMemoryTestEnv(t)
	ctx := context.Background()
	store, err := memstore.Open(ctx, cfg.Paths.Cache, cfg.Paths.CAS)
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()
	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`)
	if _, err := store.SaveFromResult(ctx, "alpha", "result", cfg.Home, "alpha summary", result); err != nil {
		t.Fatalf("save alpha: %v", err)
	}
	if _, err := store.SaveFromResult(ctx, "beta", "result", cfg.Home, "beta summary", result); err != nil {
		t.Fatalf("save beta: %v", err)
	}

	searchEnv := runMemoryCommand(t, cfg, newMemorySearchCommand(), "--workspace", cfg.Home, "--query", "alpha")
	data := searchEnv.Data.(map[string]any)
	entries := data["entries"].([]any)
	if len(entries) == 0 {
		t.Fatalf("expected search results")
	}
	first := entries[0].(map[string]any)
	if _, ok := first["score"]; !ok {
		t.Fatalf("expected score in search results")
	}

	relEnv := runMemoryCommand(t, cfg, newMemoryRelevantCommand(), "--workspace", cfg.Home)
	relData := relEnv.Data.(map[string]any)
	relEntries := relData["entries"].([]any)
	if len(relEntries) == 0 {
		t.Fatalf("expected relevant entries")
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

func runMemoryCommand(t *testing.T, cfg config.Config, cmd *cobra.Command, args ...string) envelope.Envelope {
	t.Helper()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("memory command failed: %v", err)
	}
	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	return env
}
