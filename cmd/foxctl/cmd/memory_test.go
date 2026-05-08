package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/storage/cache"
	"github.com/joshka0/foxctl/internal/storage/dbdriver"
	memstore "github.com/joshka0/foxctl/internal/storage/memory"
	"github.com/spf13/cobra"
)

func TestMemoryRecentAndCacheCommands(t *testing.T) {
	cfg := setupMemoryTestEnv(t)
	workspaceID := workspace.ID(cfg.Home)
	ctx := context.Background()

	cacheStore, err := cache.Open(ctx, cfg.Paths.Cache, cache.Options{
		AutoTTL: cfg.Memory.AutoCacheTTL,
		CASPath: cfg.Paths.CAS,
	})
	if err != nil {
		t.Fatalf("open cache store: %v", err)
	}
	defer requireClose(t, cacheStore, "cache store")

	entry := cache.Entry{
		CacheKey:     "sha256:test",
		SkillName:    "text/grep",
		SkillVersion: "0.1.0",
		Workspace:    workspaceID,
		Result:       []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`),
		CreatedAt:    time.Now().UTC(),
		ExpiresAt:    time.Now().UTC().Add(24 * time.Hour),
		Digests:      []string{},
	}
	if err := cacheStore.Put(ctx, entry); err != nil {
		t.Fatalf("cache put: %v", err)
	}

	recentCmd := newMemoryCommand()
	recentOut := &bytes.Buffer{}
	recentCmd.SetOut(recentOut)
	recentCmd.SetErr(&bytes.Buffer{})
	recentCmd.SetArgs([]string{"recent", "--workspace", cfg.Home})
	if err := recentCmd.Execute(); err != nil {
		t.Fatalf("memory recent: %v", err)
	}
	var recentEnv envelope.Envelope
	if err := json.Unmarshal(recentOut.Bytes(), &recentEnv); err != nil {
		t.Fatalf("decode recent envelope: %v", err)
	}
	if recentEnv.Command != "foxctl.memory.recent" {
		t.Fatalf("unexpected command %s", recentEnv.Command)
	}

	cacheCmd := newMemoryCommand()
	cacheOut := &bytes.Buffer{}
	cacheCmd.SetOut(cacheOut)
	cacheCmd.SetErr(&bytes.Buffer{})
	cacheCmd.SetArgs([]string{"cache", entry.CacheKey})
	if err := cacheCmd.Execute(); err != nil {
		t.Fatalf("memory cache: %v", err)
	}
	var env envelope.Envelope
	if err := json.Unmarshal(cacheOut.Bytes(), &env); err != nil {
		t.Fatalf("decode cache envelope: %v", err)
	}
	if env.Command != "foxctl.memory.cache" {
		t.Fatalf("unexpected command %s", env.Command)
	}
}

func TestMemoryListAndGetCommands(t *testing.T) {
	cfg := setupMemoryTestEnv(t)
	workspaceID := workspace.ID(cfg.Home)
	ctx := context.Background()
	store, err := memstore.Open(ctx, cfg.Storage.Root, cfg.Paths.CAS)
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer requireClose(t, store, "memory store")
	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`)
	if _, err := store.SaveFromResult(ctx, "alpha", "result", workspaceID, "alpha summary", result); err != nil {
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
	workspaceID := workspace.ID(cfg.Home)
	payload := `{"version":1,"status":"ok","command":"test","data":{"value":1},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`
	runMemoryCommand(t, cfg, newMemoryPutCommand(),
		"--name", "stored",
		"--workspace", cfg.Home,
		"--data", payload,
	)
	store, err := memstore.Open(context.Background(), cfg.Storage.Root, cfg.Paths.CAS)
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer requireClose(t, store, "memory store post-put")
	entry, err := store.Get(context.Background(), "stored", workspaceID)
	if err != nil {
		t.Fatalf("memory get stored: %v", err)
	}
	if entry.Summary == "" {
		t.Fatalf("expected summary set")
	}
}

func TestMemoryPutCommandUsesWorkspaceFlag(t *testing.T) {
	cfg := setupMemoryTestEnv(t)
	explicitWorkspace := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(explicitWorkspace, 0o755); err != nil {
		t.Fatalf("mkdir explicit workspace: %v", err)
	}
	expectedWorkspaceID := workspace.ID(explicitWorkspace)
	if expectedWorkspaceID != workspace.PathIdentity(explicitWorkspace) {
		t.Fatalf("test setup expected path-derived workspace ID, got %s", expectedWorkspaceID)
	}
	if expectedWorkspaceID == workspace.ID(cfg.Home) {
		t.Fatalf("test setup expected distinct workspace IDs, got %s for both", expectedWorkspaceID)
	}
	payload := `{"version":1,"status":"ok","command":"test","data":{"value":1},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`

	env := runMemoryCommand(t, cfg, newMemoryPutCommand(),
		"--workspace", explicitWorkspace,
		"--name", "scoped",
		"--type", "decision",
		"--summary", "scoped memory",
		"--data", payload,
	)
	data := env.Data.(map[string]any)
	if got := data["workspace"]; got != expectedWorkspaceID {
		t.Fatalf("workspace = %v, want %s", got, expectedWorkspaceID)
	}

	store, err := memstore.Open(context.Background(), cfg.Storage.Root, cfg.Paths.CAS)
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer requireClose(t, store, "memory store")
	if _, err := store.Get(context.Background(), "scoped", expectedWorkspaceID); err != nil {
		t.Fatalf("expected memory under explicit workspace %s: %v", expectedWorkspaceID, err)
	}
	if _, err := store.Get(context.Background(), "scoped", workspace.ID(cfg.Home)); err == nil {
		t.Fatalf("memory unexpectedly stored under default workspace")
	}
}

func TestMemorySearchAndRelevantCommands(t *testing.T) {
	cfg := setupMemoryTestEnv(t)
	workspaceID := workspace.ID(cfg.Home)
	ctx := context.Background()
	store, err := memstore.Open(ctx, cfg.Storage.Root, cfg.Paths.CAS)
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer requireClose(t, store, "memory store search relevant")
	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`)
	if _, err := store.SaveFromResult(ctx, "alpha", "result", workspaceID, "alpha summary", result); err != nil {
		t.Fatalf("save alpha: %v", err)
	}
	if _, err := store.SaveFromResult(ctx, "beta", "result", workspaceID, "beta summary", result); err != nil {
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

func TestMemoryStatsCommand(t *testing.T) {
	cfg := setupMemoryTestEnv(t)
	workspaceID := workspace.ID(cfg.Home)
	ctx := context.Background()

	cacheStore, err := cache.Open(ctx, cfg.Paths.Cache, cache.Options{
		AutoTTL: cfg.Memory.AutoCacheTTL,
		CASPath: cfg.Paths.CAS,
	})
	if err != nil {
		t.Fatalf("open cache store: %v", err)
	}
	defer requireClose(t, cacheStore, "cache store stats")
	if err := cacheStore.Put(ctx, cache.Entry{
		CacheKey:     "sha256:stats",
		SkillName:    "text/grep",
		SkillVersion: "0.1.0",
		Workspace:    workspaceID,
		Result:       []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`),
		CreatedAt:    time.Now().UTC(),
		ExpiresAt:    time.Now().UTC().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	mStore, err := memstore.Open(ctx, cfg.Storage.Root, cfg.Paths.CAS)
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer requireClose(t, mStore, "memory store stats")
	if _, err := mStore.SaveFromResult(ctx, "stats-entry", "result", workspaceID, "stats", []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`)); err != nil {
		t.Fatalf("seed memory: %v", err)
	}

	statsCmd := newMemoryCommand()
	out := &bytes.Buffer{}
	statsCmd.SetContext(config.WithContext(context.Background(), cfg))
	statsCmd.SetOut(out)
	statsCmd.SetErr(&bytes.Buffer{})
	statsCmd.SetArgs([]string{"stats"})
	if err := statsCmd.Execute(); err != nil {
		t.Fatalf("memory stats: %v", err)
	}
	var env envelope.Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("decode stats envelope: %v", err)
	}
	if env.Command != "foxctl.memory.stats" {
		t.Fatalf("unexpected command %s", env.Command)
	}
	data := env.Data.(map[string]any)
	cacheData := data["cache"].(map[string]any)
	if cacheData["entries"].(float64) < 1 {
		t.Fatalf("expected cache entries recorded")
	}
	memoryData := data["memory"].(map[string]any)
	if memoryData["entries"].(float64) < 1 {
		t.Fatalf("expected memory entries recorded")
	}
}

func TestMemoryMigrateBackendCopiesSQLiteToTurso(t *testing.T) {
	cfg := setupMemoryTestEnv(t)
	workspaceID := workspace.ID(cfg.Home)
	ctx := context.Background()

	source, err := memstore.Open(ctx, cfg.Storage.Root, cfg.Paths.CAS)
	if err != nil {
		t.Fatalf("open source memory store: %v", err)
	}
	defer requireClose(t, source, "source memory store")
	result := []byte(`{"version":1,"status":"ok","command":"test","data":{"value":1},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`)
	if _, err := source.SaveFromResult(ctx, "migrate-entry", "result", workspaceID, "copy me", result); err != nil {
		t.Fatalf("seed source memory: %v", err)
	}

	targetPath := filepath.Join(t.TempDir(), "memory.turso")
	migration, err := runMemoryBackendMigration(ctx, cfg, memoryBackendMigrationOptions{
		WorkspaceID:  workspaceID,
		SourceDriver: "sqlite",
		TargetDriver: "turso",
		TargetPath:   targetPath,
		VectorDims:   4,
		Apply:        true,
	})
	if err != nil {
		t.Fatalf("migrate backend: %v", err)
	}
	if migration.Scanned != 1 || migration.Copied != 1 {
		t.Fatalf("migration counts = scanned %d copied %d, want 1/1", migration.Scanned, migration.Copied)
	}

	target, err := memstore.OpenTurso(ctx, dbdriver.TursoConfig{
		Path:               targetPath,
		ReplicaPath:        targetPath,
		EnableVectorSearch: true,
		VectorDimensions:   4,
	})
	if err != nil {
		t.Fatalf("open target memory store: %v", err)
	}
	defer requireClose(t, target, "target memory store")
	entry, err := target.Get(ctx, "migrate-entry", workspaceID)
	if err != nil {
		t.Fatalf("get migrated memory: %v", err)
	}
	if entry.Summary != "copy me" {
		t.Fatalf("summary = %q, want copy me", entry.Summary)
	}
}

func TestMemoryGetNotFound(t *testing.T) {
	cfg := setupMemoryTestEnv(t)
	ctx := context.Background()

	cmd := newMemoryGetCommand()
	cmd.SetContext(config.WithContext(ctx, cfg))
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--workspace", cfg.Home, "nonexistent"})

	// Should not return an error - error envelope is written to stdout
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Status != "error" {
		t.Fatalf("expected status=error, got %s", env.Status)
	}
	if env.Error.Code != "ENOTFOUND" {
		t.Fatalf("expected error.code=ENOTFOUND, got %s", env.Error.Code)
	}
	if env.Command != "foxctl.memory.get" {
		t.Fatalf("expected command=foxctl.memory.get, got %s", env.Command)
	}
	// Verify data contains hint
	if data, ok := env.Data.(map[string]any); ok {
		if hint, ok := data["hint"].(string); !ok || hint == "" {
			t.Fatalf("expected non-empty hint in data")
		}
		if name, ok := data["name"].(string); !ok || name != "nonexistent" {
			t.Fatalf("expected name=nonexistent in data, got %v", name)
		}
	} else {
		t.Fatalf("expected data to be a map")
	}
}

func TestMemoryDeleteNotFound(t *testing.T) {
	cfg := setupMemoryTestEnv(t)
	ctx := context.Background()

	cmd := newMemoryDeleteCommand()
	cmd.SetContext(config.WithContext(ctx, cfg))
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--workspace", cfg.Home, "nonexistent"})

	// Should not return an error - error envelope is written to stdout
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Status != "error" {
		t.Fatalf("expected status=error, got %s", env.Status)
	}
	if env.Error.Code != "ENOTFOUND" {
		t.Fatalf("expected error.code=ENOTFOUND, got %s", env.Error.Code)
	}
}

func TestMemoryUpdateNotFound(t *testing.T) {
	cfg := setupMemoryTestEnv(t)
	ctx := context.Background()

	cmd := newMemoryUpdateCommand()
	cmd.SetContext(config.WithContext(ctx, cfg))
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--workspace", cfg.Home, "--summary", "new summary", "nonexistent"})

	// Should not return an error - error envelope is written to stdout
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Status != "error" {
		t.Fatalf("expected status=error, got %s", env.Status)
	}
	if env.Error.Code != "ENOTFOUND" {
		t.Fatalf("expected error.code=ENOTFOUND, got %s", env.Error.Code)
	}
}

func TestMemoryUpdateMissingArgs(t *testing.T) {
	cfg := setupMemoryTestEnv(t)
	ctx := context.Background()

	cmd := newMemoryUpdateCommand()
	cmd.SetContext(config.WithContext(ctx, cfg))
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--workspace", cfg.Home, "somename"}) // No --summary or --type

	// Should not return an error - error envelope is written to stdout
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Status != "error" {
		t.Fatalf("expected status=error, got %s", env.Status)
	}
	if env.Error.Code != "EARG" {
		t.Fatalf("expected error.code=EARG, got %s", env.Error.Code)
	}
}

func TestMemoryDeleteSuccess(t *testing.T) {
	cfg := setupMemoryTestEnv(t)
	workspaceID := workspace.ID(cfg.Home)
	ctx := context.Background()

	// Create a memory entry first
	store, err := memstore.Open(ctx, cfg.Storage.Root, cfg.Paths.CAS)
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`)
	if _, err := store.SaveFromResult(ctx, "to-delete", "result", workspaceID, "test", result); err != nil {
		t.Fatalf("save memory: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	// Delete it
	cmd := newMemoryDeleteCommand()
	cmd.SetContext(config.WithContext(ctx, cfg))
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--workspace", cfg.Home, "to-delete"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Status != "ok" {
		t.Fatalf("expected status=ok, got %s", env.Status)
	}
	if env.Command != "foxctl.memory.delete" {
		t.Fatalf("expected command=foxctl.memory.delete, got %s", env.Command)
	}

	// Verify deleted_count in response
	if data, ok := env.Data.(map[string]any); ok {
		if count, ok := data["deleted_count"].(float64); !ok || count != 1 {
			t.Fatalf("expected deleted_count=1, got %v", count)
		}
	}

	// Verify it's actually gone
	store, err = memstore.Open(ctx, cfg.Storage.Root, cfg.Paths.CAS)
	if err != nil {
		t.Fatalf("reopen memory store: %v", err)
	}
	defer requireClose(t, store, "memory store delete verify")

	_, err = store.Get(ctx, "to-delete", workspaceID)
	if err == nil {
		t.Fatal("expected entry to be deleted")
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
	cfg.Database.Driver = "sqlite"
	dirs := []string{cfg.Home, cfg.Paths.CAS, cfg.Paths.Jobs, cfg.Paths.Cache, cfg.Paths.Skills, cfg.Storage.Root}
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

func requireClose(t *testing.T, closer interface{ Close() error }, name string) {
	t.Helper()
	if err := closer.Close(); err != nil {
		t.Fatalf("close %s: %v", name, err)
	}
}
