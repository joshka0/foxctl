package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	embedstore "github.com/joshka0/foxctl/internal/intelligence/indexing/embedding"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedqueue"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"
	"github.com/joshka0/foxctl/internal/platform/config"
	workspaceutil "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/memory"
)

type recordingEmbeddingProvider struct {
	texts []string
}

func (p *recordingEmbeddingProvider) Embed(_ context.Context, text string) ([]float32, error) {
	p.texts = append(p.texts, text)
	return []float32{0.1, 0.2}, nil
}

func (p *recordingEmbeddingProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	embeddings := make([][]float32, 0, len(texts))
	for _, text := range texts {
		embedding, err := p.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		embeddings = append(embeddings, embedding)
	}
	return embeddings, nil
}

func (p *recordingEmbeddingProvider) Model() string { return "test-memory-model" }

func (p *recordingEmbeddingProvider) Dimensions() int { return 2 }

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
	workspaceRoot := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	workspaceID := workspaceutil.CanonicalID(workspaceRoot)
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
	if _, err := store.SaveFromResult(ctx, "needs-embedding", "decision", workspaceRoot, "queue this memory", result); err != nil {
		t.Fatalf("save missing memory: %v", err)
	}
	if _, err := store.SaveFromResult(ctx, "already-embedded", "decision", workspaceRoot, "skip this memory", result); err != nil {
		t.Fatalf("save embedded memory: %v", err)
	}
	if _, err := store.SaveFromResult(ctx, "code-symbol", symbol.SymbolType, workspaceRoot, "skip code symbol", result); err != nil {
		t.Fatalf("save code symbol memory: %v", err)
	}
	if _, err := store.SaveFromResult(ctx, "file-summary", symbol.FileSummaryType, workspaceRoot, "skip file summary", result); err != nil {
		t.Fatalf("save file summary memory: %v", err)
	}
	if err := store.UpdateEmbedding(ctx, "already-embedded", workspaceRoot, []float32{0.1, 0.2}); err != nil {
		t.Fatalf("update embedding: %v", err)
	}

	queued, err := enqueueMemoryEmbeddingJobs(ctx, cfg, store, workspaceRoot, false)
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
	if job.WorkspaceID != workspaceID {
		t.Fatalf("workspace_id=%q want %q", job.WorkspaceID, workspaceID)
	}
	if job.Model != "text-embedding-qwen3-embedding-8b" {
		t.Fatalf("model=%q", job.Model)
	}
}

func TestMemoryEmbeddingInputsSkipsCodeOwnedMemoryTypes(t *testing.T) {
	inputs := memoryEmbeddingInputs([]storage.NamedEntry{
		{Name: "human-note", Type: "decision", Summary: "keep me"},
		{Name: "file-row", Type: "file_embedding", Summary: "skip me"},
		{Name: "chunk-row", Type: "file_embedding_chunk", Summary: "skip me"},
		{Name: "symbol-row", Type: symbol.SymbolType, Summary: "skip me"},
		{Name: "call-row", Type: symbol.CallEdgeType, Summary: "skip me"},
		{Name: "meta-row", Type: symbol.FileMetaType, Summary: "skip me"},
		{Name: "summary-row", Type: symbol.FileSummaryType, Summary: "skip me"},
		{Name: "symbol-summary-row", Type: symbol.SymbolSummaryType, Summary: "skip me"},
		{Name: "legacy-symbol-row", Type: "symbol", Summary: "skip me"},
	})
	if len(inputs) != 1 {
		t.Fatalf("inputs=%v want one human memory", inputs)
	}
	if inputs[0].Name != "human-note" {
		t.Fatalf("input name=%q want human-note", inputs[0].Name)
	}
}

func TestMemoryEmbeddingInputsKeepsRealMemoryTypes(t *testing.T) {
	inputs := memoryEmbeddingInputs([]storage.NamedEntry{
		{Name: "decision:test", Type: "decision", Summary: "decision"},
		{Name: "gotcha:test", Type: "gotcha", Summary: "gotcha"},
		{Name: "learning:test", Type: "learning", Summary: "learning"},
		{Name: "note:test", Type: "note", Summary: "note"},
		{Name: "fact:test", Type: "fact", Summary: "fact"},
	})
	if len(inputs) != 5 {
		t.Fatalf("inputs=%v want five real memories", inputs)
	}
	for _, input := range inputs {
		if input.Content == "" {
			t.Fatalf("input %q has empty content", input.Name)
		}
	}
}

func TestEnqueueMemoryEmbeddingJobsForceSkipsCodeOwnedMemoryTypes(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	workspaceRoot := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
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
	for _, entry := range []struct {
		name string
		typ  string
	}{
		{"decision:test", "decision"},
		{"gotcha:test", "gotcha"},
		{"code-symbol", symbol.SymbolType},
		{"file-embedding", semantic.FileEmbeddingType},
	} {
		if _, err := store.SaveFromResult(ctx, entry.name, entry.typ, workspaceRoot, entry.name, result); err != nil {
			t.Fatalf("save memory %s: %v", entry.name, err)
		}
	}

	queued, err := enqueueMemoryEmbeddingJobs(ctx, cfg, store, workspaceRoot, true)
	if err != nil {
		t.Fatalf("force enqueue memory embeddings: %v", err)
	}
	if queued != 2 {
		t.Fatalf("queued=%d want 2", queued)
	}

	queueStore, err := embedstore.OpenStore(ctx, cfg.Paths.Cache)
	if err != nil {
		t.Fatalf("open queue store: %v", err)
	}
	defer queueStore.Close()
	var names []string
	for {
		job, err := queueStore.ClaimNext(ctx)
		if err != nil {
			t.Fatalf("claim: %v", err)
		}
		if job == nil {
			break
		}
		names = append(names, job.MemoryName)
	}
	want := map[string]bool{"decision:test": true, "gotcha:test": true}
	if len(names) != len(want) {
		t.Fatalf("queued names=%v want decision/gotcha only", names)
	}
	for _, name := range names {
		if !want[name] {
			t.Fatalf("unexpected queued memory %q from names=%v", name, names)
		}
	}
}

func TestListMemoriesForReembeddingSkipsCodeOwnedMemoryTypes(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	workspaceRoot := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	cfg := config.Config{}
	cfg.Storage.Root = filepath.Join(root, "storage")
	cfg.Paths.CAS = filepath.Join(root, "cas")

	store, err := memory.Open(ctx, cfg.Storage.Root, cfg.Paths.CAS)
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2026-05-07T00:00:00Z"},"error":{}}`)
	for _, entry := range []struct {
		name    string
		typ     string
		summary string
	}{
		{"decision:test", "decision", "embed decision"},
		{"fact:test", "fact", "embed fact"},
		{"code-symbol", symbol.SymbolType, "skip symbol"},
		{"file-chunk", semantic.FileEmbeddingChunkType, "skip chunk"},
		{"empty-note", "note", ""},
	} {
		if _, err := store.SaveFromResult(ctx, entry.name, entry.typ, workspaceRoot, entry.summary, result); err != nil {
			t.Fatalf("save memory %s: %v", entry.name, err)
		}
	}

	entries, err := listMemoriesForReembedding(ctx, store, workspaceutil.CanonicalID(workspaceRoot), 100)
	if err != nil {
		t.Fatalf("list memories for reembedding: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries=%v want decision and fact", entries)
	}
	for _, entry := range entries {
		if entry.Type != "decision" && entry.Type != "fact" {
			t.Fatalf("unexpected entry for reembedding: %#v", entry)
		}
	}
}

func TestEmbedMemoryEntriesSkipsCodeOwnedMemoryTypes(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	workspaceRoot := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	cfg := config.Config{}
	cfg.Storage.Root = filepath.Join(root, "storage")
	cfg.Paths.CAS = filepath.Join(root, "cas")

	store, err := memory.Open(ctx, cfg.Storage.Root, cfg.Paths.CAS)
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2026-05-07T00:00:00Z"},"error":{}}`)
	var entries []storage.NamedEntry
	for _, entry := range []struct {
		name    string
		typ     string
		summary string
	}{
		{"decision:test", "decision", "embed decision"},
		{"learning:test", "learning", "embed learning"},
		{"code-symbol", symbol.SymbolType, "skip symbol"},
		{"file-summary", symbol.FileSummaryType, "skip file summary"},
	} {
		saved, err := store.SaveFromResult(ctx, entry.name, entry.typ, workspaceRoot, entry.summary, result)
		if err != nil {
			t.Fatalf("save memory %s: %v", entry.name, err)
		}
		entries = append(entries, saved)
	}

	provider := &recordingEmbeddingProvider{}
	count, err := embedMemoryEntries(ctx, cfg, store, provider, workspaceRoot, entries)
	if err != nil {
		t.Fatalf("embed memory entries: %v", err)
	}
	if count != 2 {
		t.Fatalf("embedded=%d want 2", count)
	}
	if len(provider.texts) != 2 {
		t.Fatalf("provider texts=%v want only real memory summaries", provider.texts)
	}
	want := map[string]bool{"embed decision": true, "embed learning": true}
	for _, text := range provider.texts {
		if !want[text] {
			t.Fatalf("unexpected embedded text %q from texts=%v", text, provider.texts)
		}
	}
}

func TestIndexSymbolsUsesSymbolIndexerAndQueuesSymbolEmbeddings(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	workspaceRoot := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceRoot, "main.go"), []byte(`package main

// Greet returns a greeting.
//
// [[invariant:greeting-nonempty]]
func Greet(name string) string {
	return "hello " + name
}
`), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	cfg := config.Config{}
	cfg.Storage.Root = filepath.Join(root, "storage")
	cfg.Paths.CAS = filepath.Join(root, "cas")
	cfg.Paths.Cache = filepath.Join(root, "cache")
	cfg.Embedding.Models = map[string]string{"symbols": "text-embedding-qwen3-embedding-8b"}

	count, err := indexSymbols(ctx, cfg, workspaceRoot, "**/*.go", nil)
	if err != nil {
		t.Fatalf("index symbols: %v", err)
	}
	if count != 1 {
		t.Fatalf("indexed=%d want 1", count)
	}

	memStore, err := memory.Open(ctx, cfg.Storage.Root, cfg.Paths.CAS)
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer memStore.Close()
	entries, err := memStore.List(ctx, workspaceutil.ID(workspaceRoot), 100)
	if err != nil {
		t.Fatalf("list memory entries: %v", err)
	}
	var symbolEntries, fileEmbeddingEntries int
	for _, entry := range entries {
		switch entry.Type {
		case symbol.SymbolType:
			symbolEntries++
		case "file_embedding":
			fileEmbeddingEntries++
		}
	}
	if symbolEntries == 0 {
		t.Fatalf("expected code_symbol entries, got entries=%v", entries)
	}
	if fileEmbeddingEntries != 0 {
		t.Fatalf("expected no file_embedding entries, got %d", fileEmbeddingEntries)
	}

	queueStore, err := embedstore.OpenStore(ctx, cfg.Paths.Cache)
	if err != nil {
		t.Fatalf("open queue store: %v", err)
	}
	defer queueStore.Close()
	job, err := queueStore.ClaimNextInWorkspaceKind(ctx, workspaceutil.ID(workspaceRoot), embedqueue.TaskKindSymbol)
	if err != nil {
		t.Fatalf("claim symbol job: %v", err)
	}
	if job == nil {
		t.Fatal("expected queued symbol embedding job")
	}
	if job.FilePath != "main.go" {
		t.Fatalf("file path=%q want main.go", job.FilePath)
	}
	if job.SymbolName != "Greet" {
		t.Fatalf("symbol name=%q want Greet", job.SymbolName)
	}
	if job.Model != "text-embedding-qwen3-embedding-8b" {
		t.Fatalf("model=%q want text-embedding-qwen3-embedding-8b", job.Model)
	}
	if job.Content == "" || !bytes.Contains([]byte(job.Content), []byte("Greet returns a greeting")) {
		t.Fatalf("expected doc-enriched symbol content, got %q", job.Content)
	}
	if !bytes.Contains([]byte(job.Content), []byte("Semantic anchors: invariant:greeting-nonempty")) {
		t.Fatalf("expected semantic anchor hint in symbol content, got %q", job.Content)
	}
}
