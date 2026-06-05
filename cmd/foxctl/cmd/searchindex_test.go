package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/intelligence/searchindex"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/workspace"
)

func TestSearchIndexStatsCommandReportsPersistentCorpus(t *testing.T) {
	cfg := setupSearchIndexTestConfig(t)
	workspaceRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	store, err := searchindex.Open(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open searchindex: %v", err)
	}
	workspaceID := workspace.ExplicitID(workspaceRoot)
	if err := store.Upsert(context.Background(), searchindex.Document{
		ID:             "search://" + workspaceID + "/symbol/a",
		WorkspaceID:    workspaceID,
		Scope:          searchindex.ScopeCode,
		Kind:           searchindex.KindSymbol,
		GroupKey:       "a.go",
		Path:           "a.go",
		Title:          "A",
		Summary:        "A summary",
		SearchText:     "A symbol",
		Embedding:      []float32{0.1, 0.2, 0.3},
		EmbeddingModel: "model-a",
	}); err != nil {
		t.Fatalf("upsert search document: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close searchindex: %v", err)
	}

	cmd := newSearchIndexCommand()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"stats", "--workspace", workspaceRoot})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("searchindex stats: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v\nbody=%s", err, stdout.String())
	}
	if env["status"] != "ok" || env["command"] != "searchindex.stats" {
		t.Fatalf("unexpected envelope: %v", env)
	}
	data := env["data"].(map[string]any)
	if data["workspace_id"] != workspaceID || data["document_count"].(float64) != 1 || data["embedded_count"].(float64) != 1 {
		t.Fatalf("unexpected stats data: %v", data)
	}
}

func setupSearchIndexTestConfig(t *testing.T) config.Config {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	cfg, err := config.Load(context.Background())
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	cfg.Database.Driver = "sqlite"
	dirs := []string{cfg.Home, cfg.Paths.CAS, cfg.Paths.Cache, cfg.Storage.Root}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	return cfg
}
