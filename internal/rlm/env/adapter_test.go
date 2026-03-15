package env

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/platform/config"
	ws "github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/rlm"
	"github.com/jkatigb/agentctl/internal/storage/cas"
	"github.com/jkatigb/agentctl/internal/storage/trajectory"
)

func TestReadOnlyAdapterBasicTools(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()

	if err := os.WriteFile(filepath.Join(workspace, "main.tf"), []byte("resource \"aws_s3_bucket\" \"app\" {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	repoStore, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer repoStore.Close()
	repoKey := repoStore.RepoKey()
	if err := repoStore.ReplaceAll(ctx, []repoindex.Node{
		{
			ID:      repoindex.NamespacedID(repoKey, "res:tf:root:resource:aws_s3_bucket.app"),
			Kind:    repoindex.NodeConcept,
			Pkg:     "tf:root",
			File:    "main.tf",
			Name:    "resource aws_s3_bucket.app",
			Summary: "Terraform resource aws_s3_bucket app.",
		},
		{
			ID:      repoindex.FileID(repoKey, "tf:root", "main.tf"),
			Kind:    repoindex.NodeFile,
			Pkg:     "tf:root",
			File:    "main.tf",
			Name:    "main.tf",
			Summary: "Terraform file main.tf.",
		},
	}, nil); err != nil {
		t.Fatal(err)
	}

	var cfg config.Config
	cfg.Storage.Root = storageRoot
	adapter := NewReadOnlyAdapter(cfg, workspace, "", nil, rlm.Environment{
		TopOfMind: map[string]any{
			"objective": "trace terraform",
		},
	})

	top, err := adapter.Execute(ctx, "get_top_of_mind", nil)
	if err != nil {
		t.Fatalf("get_top_of_mind: %v", err)
	}
	if top["top_of_mind"] == nil {
		t.Fatalf("top_of_mind=%v", top)
	}

	repoResult, err := adapter.Execute(ctx, "search_repo", mustJSON(map[string]any{
		"query": "terraform bucket",
		"limit": 3,
	}))
	if err != nil {
		t.Fatalf("search_repo: %v", err)
	}
	rawResults, ok := repoResult["results"].([]map[string]any)
	if ok {
		_ = rawResults
	}
	raw, _ := json.Marshal(repoResult["results"])
	var results []map[string]any
	if err := json.Unmarshal(raw, &results); err != nil {
		t.Fatalf("decode repo results: %v", err)
	}
	if len(results) == 0 || results[0]["path"] != "main.tf" {
		t.Fatalf("repo results=%v", results)
	}

	fileResult, err := adapter.Execute(ctx, "load_file", mustJSON(map[string]any{
		"path": "main.tf",
	}))
	if err != nil {
		t.Fatalf("load_file: %v", err)
	}
	if fileResult["content"] == "" {
		t.Fatalf("file result=%v", fileResult)
	}
}

func TestReadOnlyAdapterLoadsTrajectoryAndCASArtifacts(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()
	casRoot := filepath.Join(storageRoot, "cas")
	if err := os.MkdirAll(casRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	var cfg config.Config
	cfg.Storage.Root = storageRoot
	cfg.Paths.CAS = casRoot

	casStore, err := cas.NewStore(casRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer casStore.Close()
	obj, err := casStore.Put(ctx, strings.NewReader("artifact body"), "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}

	trajStore, err := trajectory.Open(ctx, storageRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer trajStore.Close()
	traj, err := trajStore.InsertTrajectory(ctx, trajectory.Trajectory{
		ID:             "traj-1",
		WorkspaceID:    ws.ID(workspace),
		Status:         trajectory.StatusOK,
		Summary:        "artifact summary",
		ArtifactDigest: obj.Digest,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	adapter := NewReadOnlyAdapter(cfg, workspace, "", nil, rlm.Environment{
		ArtifactHandles: []string{"trajectory:" + traj.ID, "artifact:" + obj.Digest},
	})

	search, err := adapter.Execute(ctx, "search_artifacts", mustJSON(map[string]any{
		"query": "artifact",
		"limit": 5,
	}))
	if err != nil {
		t.Fatalf("search_artifacts: %v", err)
	}
	raw, _ := json.Marshal(search["results"])
	var handles []string
	if err := json.Unmarshal(raw, &handles); err != nil {
		t.Fatalf("decode handles: %v", err)
	}
	if len(handles) == 0 {
		t.Fatalf("expected artifact handles")
	}

	loaded, err := adapter.Execute(ctx, "load_artifact", mustJSON(map[string]any{
		"handle": "artifact:" + obj.Digest,
	}))
	if err != nil {
		t.Fatalf("load_artifact: %v", err)
	}
	artifact, ok := loaded["artifact"].(map[string]any)
	if !ok || artifact["content"] != "artifact body" {
		t.Fatalf("artifact=%v", loaded)
	}
}
