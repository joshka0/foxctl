package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/context/contextplane"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/evals/retrievaleval"
	"github.com/jkatigb/agentctl/internal/intelligence/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/platform/config"
)

func TestContextRepoIndexDAGInspectSuite_PersistsRun(t *testing.T) {
	orig := buildRepoIndexDAGInspectionReportHook
	buildRepoIndexDAGInspectionReportHook = func(_ context.Context, _ string, workspacePath string, suite retrievaleval.Suite, _ int) (graphInspectionSuiteReport, error) {
		return graphInspectionSuiteReport{
			Method:        "repoindex_dag",
			Suite:         suite.Name,
			WorkspacePath: workspacePath,
			GeneratedAt:   time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC),
			Inspections: []graphInspection{{
				Query:          "web api transport",
				ExpectedPaths:  []string{"internal/web/api/agents.go"},
				Anchors:        []string{"internal/web/api/agents.go"},
				Matched:        true,
				Classification: "matched",
			}},
		}, nil
	}
	t.Cleanup(func() { buildRepoIndexDAGInspectionReportHook = orig })

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	cfg, err := config.Load(context.Background())
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	workspacePath := tmp
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	store, err := repoindex.Open(context.Background(), cfg.Storage.Root, workspacePath)
	if err != nil {
		t.Fatalf("open repoindex: %v", err)
	}
	repoKey := store.RepoKey()
	if err := store.ReplaceAll(context.Background(), []repoindex.Node{
		{
			ID:      repoindex.FileID(repoKey, "internal/web/api", "internal/web/api/agents.go"),
			Kind:    repoindex.NodeFile,
			Pkg:     "internal/web/api",
			File:    "internal/web/api/agents.go",
			Name:    "agents.go",
			Summary: "Agent HTTP handlers.",
		},
		{
			ID:      repoindex.NamespacedID(repoKey, "concept:web-api-transport"),
			Kind:    repoindex.NodeConcept,
			Pkg:     "internal/web/api",
			File:    "internal/web/api/agents.go",
			Name:    "web api transport",
			Summary: "Web API transport anchor.",
		},
	}, nil); err != nil {
		t.Fatalf("ReplaceAll: %v", err)
	}
	store.Close()

	suitePath := filepath.Join(t.TempDir(), "dag-suite.yaml")
	if err := os.WriteFile(suitePath, []byte(`name: dag-suite
queries:
  - id: web-api
    query: web api transport
    expected_any_of:
      - internal/web/api/agents.go
`), 0o644); err != nil {
		t.Fatalf("write suite: %v", err)
	}

	cmd := newContextRepoIndexDAGInspectSuiteCommand()
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
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("env.Data=%T", env.Data)
	}
	usedWorkspace, _ := data["workspace_path"].(string)
	if usedWorkspace == "" {
		t.Fatal("expected workspace_path")
	}
	runID, _ := data["run_id"].(string)
	if runID == "" {
		t.Fatal("expected run_id")
	}
	artifact, _ := data["artifact"].(string)
	if artifact == "" {
		t.Fatal("expected artifact")
	}
	cpStore := contextplane.NewWorkspaceStore(usedWorkspace)
	run, err := cpStore.GetGraphCorrectionRun(runID)
	if err != nil {
		t.Fatalf("GetGraphCorrectionRun: %v", err)
	}
	if run == nil || run.ArtifactDigest != artifact {
		runs, listErr := cpStore.ListGraphCorrectionRuns(10)
		t.Fatalf("run=%+v artifact=%q runs=%+v listErr=%v env=%s", run, artifact, runs, listErr, stdout.String())
	}
	if run.Method != "repoindex_dag" {
		t.Fatalf("method=%q want repoindex_dag", run.Method)
	}
}
