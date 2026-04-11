package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/contextplane"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
)

func TestContextSemanticSearchInspectSuite_PersistsRun(t *testing.T) {
	orig := buildSemanticSearchInspectionReportHook
	buildSemanticSearchInspectionReportHook = func(_ context.Context, workspacePath, _ string, query string, expectedAnyOf []string, _ int) (graphInspection, error) {
		return graphInspection{
			Query:          query,
			ExpectedPaths:  append([]string(nil), expectedAnyOf...),
			Anchors:        []string{"internal/storage/memory/store.go"},
			Matched:        true,
			Classification: "matched",
		}, nil
	}
	t.Cleanup(func() { buildSemanticSearchInspectionReportHook = orig })

	tmp := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmp)
	if origHome != "" {
		t.Setenv("GOMODCACHE", filepath.Join(origHome, "go", "pkg", "mod"))
		t.Setenv("GOCACHE", filepath.Join(origHome, "Library", "Caches", "go-build"))
	}
	cfg, err := config.Load(context.Background())
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	workspacePath := tmp
	store := contextplane.NewWorkspaceStore(workspacePath)
	if _, err := store.SaveTopOfMind(contextplane.TopOfMind{
		WorkspaceID:  filepath.Base(workspacePath),
		Objective:    "Improve semantic retrieval",
		Phase:        "experiment",
		RelevantRefs: []string{"path:internal/storage/memory/store.go"},
		UpdatedAt:    time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("SaveTopOfMind: %v", err)
	}

	suitePath := filepath.Join(t.TempDir(), "semantic-suite.yaml")
	if err := os.WriteFile(suitePath, []byte(`name: semantic-suite
queries:
  - id: storage-memory
    query: storage memory package
    expected_any_of:
      - internal/storage/memory/store.go
`), 0o644); err != nil {
		t.Fatalf("write suite: %v", err)
	}

	cmd := newContextSemanticSearchInspectSuiteCommand()
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
	if run.Method != "semantic_search" {
		t.Fatalf("method=%q want semantic_search", run.Method)
	}
}
