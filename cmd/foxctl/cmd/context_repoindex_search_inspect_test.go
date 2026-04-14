package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/tooling/evals/retrievaleval"
)

func TestContextRepoIndexSearchInspectSuite_PersistsRun(t *testing.T) {
	orig := buildRepoIndexSearchInspectionReportHook
	buildRepoIndexSearchInspectionReportHook = func(_ context.Context, _ string, workspacePath string, suite retrievaleval.Suite, _ int) (graphInspectionSuiteReport, error) {
		return graphInspectionSuiteReport{
			Method:        "repoindex_search",
			Suite:         suite.Name,
			WorkspacePath: workspacePath,
			GeneratedAt:   time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC),
			Inspections: []graphInspection{{
				Query:          "storage memory package",
				ExpectedPaths:  []string{"internal/storage/memory/store.go"},
				Anchors:        []string{"internal/storage/memory/store.go"},
				Matched:        true,
				Classification: "matched",
			}},
		}, nil
	}
	t.Cleanup(func() { buildRepoIndexSearchInspectionReportHook = orig })

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	cfg, err := config.Load(context.Background())
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	workspacePath := tmp
	store, err := repoindex.Open(context.Background(), cfg.Storage.Root, workspacePath)
	if err != nil {
		t.Fatalf("open repoindex: %v", err)
	}
	repoKey := store.RepoKey()
	if err := store.ReplaceAll(context.Background(), []repoindex.Node{
		{
			ID:      repoindex.FileID(repoKey, "internal/storage/memory", "internal/storage/memory/store.go"),
			Kind:    repoindex.NodeFile,
			Pkg:     "internal/storage/memory",
			File:    "internal/storage/memory/store.go",
			Name:    "store.go",
			Summary: "Memory store implementation.",
		},
		{
			ID:      repoindex.NamespacedID(repoKey, "concept:storage-memory"),
			Kind:    repoindex.NodeConcept,
			Pkg:     "internal/storage/memory",
			File:    "internal/storage/memory/store.go",
			Name:    "storage memory package",
			Summary: "Storage memory package anchor.",
		},
	}, nil); err != nil {
		t.Fatalf("ReplaceAll: %v", err)
	}
	store.Close()

	suitePath := filepath.Join(t.TempDir(), "search-suite.yaml")
	if err := os.WriteFile(suitePath, []byte(`name: search-suite
queries:
  - id: storage-memory
    query: storage memory package
    expected_any_of:
      - internal/storage/memory/store.go
`), 0o644); err != nil {
		t.Fatalf("write suite: %v", err)
	}

	cmd := newContextRepoIndexSearchInspectSuiteCommand()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"--workspace", workspacePath,
		"--suite", suitePath,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	data := env.Data.(map[string]any)
	runID, _ := data["run_id"].(string)
	if runID == "" {
		t.Fatal("expected run_id")
	}
	artifact, _ := data["artifact"].(string)
	if artifact == "" {
		t.Fatal("expected artifact")
	}
	cpStore := contextplane.NewWorkspaceStore(data["workspace_path"].(string))
	run, err := cpStore.GetGraphCorrectionRun(runID)
	if err != nil {
		t.Fatalf("GetGraphCorrectionRun: %v", err)
	}
	if run == nil || run.ArtifactDigest != artifact {
		t.Fatalf("run=%+v artifact=%q", run, artifact)
	}
	if run.Method != "repoindex_search" {
		t.Fatalf("method=%q want repoindex_search", run.Method)
	}
}
