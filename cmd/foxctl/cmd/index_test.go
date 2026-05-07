package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	embedstore "github.com/joshka0/foxctl/internal/intelligence/indexing/embedding"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage/memory"
)

func TestIndexCommand_Init(t *testing.T) {
	cmd := newIndexCommand()
	if cmd.Use != "index" {
		t.Fatalf("expected Use to be %q, got %q", "index", cmd.Use)
	}

	subCmds := cmd.Commands()
	var hasInit, hasStatus bool
	for _, sub := range subCmds {
		switch sub.Use {
		case "init":
			hasInit = true
		case "status":
			hasStatus = true
		}
	}

	if !hasInit || !hasStatus {
		t.Fatalf("expected init and status subcommands, got init=%v status=%v", hasInit, hasStatus)
	}
}

func TestIndexInit_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	cmd := newIndexInitCommand()
	cmd.SetContext(context.Background())

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	cmd.SetArgs([]string{
		"--workspace", tmpDir,
		"--scope", "symbols,memory,tasks,sessions",
		"--dry-run",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v; output: %s", err, buf.String())
	}

	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("parse envelope: %v; output: %s", err, buf.String())
	}

	if env["status"] != "ok" {
		t.Fatalf("expected status ok, got %v", env["status"])
	}

	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data map, got %T", env["data"])
	}
	if data["dry_run"] != true {
		t.Fatalf("expected dry_run true, got %v", data["dry_run"])
	}

	scopes, ok := data["scopes"].([]any)
	if !ok || len(scopes) != 4 {
		t.Fatalf("expected 4 scopes, got %v", data["scopes"])
	}

	foundSymbols := false
	for _, s := range scopes {
		scopeMap, ok := s.(map[string]any)
		if !ok {
			t.Fatalf("expected scope map, got %T", s)
		}
		if scopeMap["scope"] == "symbols" {
			foundSymbols = true
			if _, ok := scopeMap["files_count"]; !ok {
				t.Fatalf("expected files_count for symbols scope, got %v", scopeMap)
			}
		}
	}
	if !foundSymbols {
		t.Fatal("symbols scope not found in output")
	}
}

func TestCreateIndexEmbeddingProviderForScope_OpenAICompat(t *testing.T) {
	t.Setenv("VOYAGE_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")

	cfg := config.Config{
		Embedding: config.EmbeddingSettings{
			Provider: "openai_compat",
			Model:    "text-embedding-embeddinggemma-300m-qat",
			BaseURL:  "http://127.0.0.1:1234/v1",
			APIKey:   "lm-studio",
		},
	}

	provider, err := createIndexEmbeddingProviderForScope(cfg, "symbols")
	if err != nil {
		t.Fatalf("createIndexEmbeddingProviderForScope: %v", err)
	}
	if provider == nil {
		t.Fatal("expected provider")
	}
	if provider.Model() != "text-embedding-embeddinggemma-300m-qat" {
		t.Fatalf("provider model = %q, want configured OpenAI-compatible model", provider.Model())
	}
}

func TestEnqueueMemoryEmbeddingJobsQueuesMissingMemories(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	cfg := config.Config{}
	cfg.Storage.Root = filepath.Join(root, "storage")
	cfg.Paths.CAS = filepath.Join(root, "cas")
	cfg.Paths.Cache = filepath.Join(root, "cache")
	cfg.Embedding.Models = map[string]string{"memory": "text-embedding-qwen3-embedding-8b"}

	store, err := memory.Open(ctx, cfg.Storage.Root, cfg.Paths.CAS)
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2026-05-07T00:00:00Z"},"error":{}}`)
	if _, err := store.SaveFromResult(ctx, "needs-embedding", "decision", "ws", "queue this memory", result); err != nil {
		t.Fatalf("save missing memory: %v", err)
	}
	if _, err := store.SaveFromResult(ctx, "already-embedded", "decision", "ws", "skip this memory", result); err != nil {
		t.Fatalf("save embedded memory: %v", err)
	}
	if err := store.UpdateEmbedding(ctx, "already-embedded", "ws", []float32{0.1, 0.2}); err != nil {
		t.Fatalf("update embedding: %v", err)
	}

	queued, err := enqueueMemoryEmbeddingJobs(ctx, cfg, store, "ws", false)
	if err != nil {
		t.Fatalf("enqueue memory embeddings: %v", err)
	}
	if queued != 1 {
		t.Fatalf("queued=%d want 1", queued)
	}

	queueStore, err := embedstore.OpenStore(ctx, cfg.Paths.Cache)
	if err != nil {
		t.Fatalf("open queue store: %v", err)
	}
	defer queueStore.Close()
	job, err := queueStore.ClaimNext(ctx)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if job == nil {
		t.Fatal("expected queued memory job")
	}
	if job.MemoryName != "needs-embedding" {
		t.Fatalf("memory name=%q want needs-embedding", job.MemoryName)
	}
	if job.Model != "text-embedding-qwen3-embedding-8b" {
		t.Fatalf("model=%q", job.Model)
	}
}
