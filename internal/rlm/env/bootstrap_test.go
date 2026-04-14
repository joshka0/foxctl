package env

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/rlm"
	"github.com/joshka0/foxctl/internal/storage/obsidianindex"
)

func TestBootstrapBuildIncludesACAAndHandles(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()
	store := contextplane.NewWorkspaceStore(workspace)
	layout := store.Layout()
	if err := os.MkdirAll(filepath.Dir(layout.TopOfMindPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(layout.HandoffsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	top := map[string]any{
		"workspace_id":  "ws-test",
		"objective":     "trace auth flow",
		"relevant_refs": []string{"note:repo/index", "path:internal/auth/handler.go"},
	}
	body, _ := json.Marshal(top)
	if err := os.WriteFile(layout.TopOfMindPath, body, 0o644); err != nil {
		t.Fatal(err)
	}
	handoff := contextplane.Handoff{
		TaskID:       "T-1",
		Phase:        "analyze",
		Outcome:      "partial",
		Summary:      "Collected auth evidence.",
		EvidenceRefs: []string{"artifact:turn-1", "path:internal/auth/store.go"},
	}
	handoffBody, _ := json.Marshal(handoff)
	if err := os.WriteFile(filepath.Join(layout.HandoffsDir, "handoff-001.json"), handoffBody, 0o644); err != nil {
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
			ID:      repoindex.NamespacedID(repoKey, "res:k8s:auth"),
			Kind:    repoindex.NodeConcept,
			Pkg:     "k8s:auth",
			File:    "deploy/auth.yaml",
			Name:    "Deployment/auth",
			Summary: "Auth deployment.",
		},
	}, nil); err != nil {
		t.Fatal(err)
	}

	vaultPath := filepath.Join(t.TempDir(), "vault")
	if err := os.MkdirAll(vaultPath, 0o755); err != nil {
		t.Fatal(err)
	}
	index, err := obsidianindex.Open(ctx, storageRoot, vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := index.Rebuild(ctx, vaultPath); err != nil {
		t.Fatal(err)
	}
	_ = index.Close()

	cfg := config.Config{}
	cfg.Storage.Root = storageRoot
	bootstrapper := NewBootstrapper(BootstrapConfig{
		AppConfig: cfg,
		VaultPath: vaultPath,
	})
	env, err := bootstrapper.Build(ctx, rlm.Task{
		Prompt:        "auth deployment",
		WorkspaceID:   "ws-test",
		WorkspaceRoot: workspace,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if env.TopOfMind == nil || env.TopOfMind["objective"] != "trace auth flow" {
		t.Fatalf("top_of_mind=%v", env.TopOfMind)
	}
	if env.LatestHandoff == nil || env.LatestHandoff["task_id"] != "T-1" {
		t.Fatalf("latest_handoff=%v", env.LatestHandoff)
	}
	if len(env.Tools) == 0 {
		t.Fatal("expected default tool surface")
	}
	if !containsPrefix(env.ArtifactHandles, "artifact:turn-1") {
		t.Fatalf("artifact handles=%v", env.ArtifactHandles)
	}
	if !containsPrefix(env.RepoHandles, "path:deploy/auth.yaml") {
		t.Fatalf("repo handles=%v", env.RepoHandles)
	}
}

func containsPrefix(values []string, want string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == want {
			return true
		}
	}
	return false
}
