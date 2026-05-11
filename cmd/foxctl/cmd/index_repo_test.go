package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/intelligence/repoquery"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/spf13/cobra"
)

func TestRepoOpenFallbackQueriesExtractRepoRelativePath(t *testing.T) {
	t.Parallel()

	got := repoOpenFallbackQueries("/tmp/foxctl", "foxctl-repoindex-abc::sym:go:github.com/joshka0/foxctl/internal/agent/types/types.go")
	found := false
	for _, item := range got {
		if item == "internal/agent/types/types.go" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("queries=%v missing repo-relative path", got)
	}
}

func TestResolveRepoOpenFallbackIDFindsNodeByPath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	storageRoot := filepath.Join(root, "storage")
	workspace := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(workspace, "internal", "agent", "types"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "internal", "agent", "types", "types.go"), []byte("package types\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repoKey := store.RepoKey()
	node := repoindex.Node{
		ID:   repoindex.FileID(repoKey, "go:github.com/joshka0/foxctl/internal/agent/types", "internal/agent/types/types.go"),
		Kind: repoindex.NodeFile,
		Pkg:  "go:github.com/joshka0/foxctl/internal/agent/types",
		File: "internal/agent/types/types.go",
		Name: "types.go",
	}
	if err := store.ReplaceAll(ctx, []repoindex.Node{node}, nil); err != nil {
		t.Fatal(err)
	}

	service := repoquery.NewQueryService(repoindex.NewQueryEngine(store))
	got, err := resolveRepoOpenFallbackID(ctx, workspace, service, "foxctl-repoindex-abc::sym:go:github.com/joshka0/foxctl/internal/agent/types/types.go")
	if err != nil {
		t.Fatalf("resolveRepoOpenFallbackID() error = %v", err)
	}
	if got != node.ID {
		t.Fatalf("resolved=%q want %q", got, node.ID)
	}
}

func TestIndexRepoTracePathCommandUsesFreshStore(t *testing.T) {
	ctx := context.Background()
	cfg, workspace, nodes := setupIndexRepoNavigationCommandFixture(t, ctx)

	env, stdout, err := executeIndexRepoNavigationCommand(ctx, cfg, newIndexRepoTracePathCommand(),
		"--workspace", workspace,
		"--src-id", nodes["caller"].ID,
		"--dst-id", nodes["callee"].ID,
	)
	if err != nil {
		t.Fatalf("trace-path command failed: %v\nstdout:\n%s", err, stdout)
	}
	if env.Status != envelope.StatusOK || env.Command != "index.repo.trace_path" {
		t.Fatalf("unexpected envelope: %+v\nstdout:\n%s", env, stdout)
	}
	for _, want := range []string{`"found":true`, `"path_len":1`, string(repoindex.EdgeCalls), `"freshness"`} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("trace-path output missing %q:\n%s", want, stdout)
		}
	}
}

func TestIndexRepoSmartContextCommandRejectsDirtyIndex(t *testing.T) {
	ctx := context.Background()
	cfg, workspace, nodes := setupIndexRepoNavigationCommandFixture(t, ctx)
	if err := os.WriteFile(filepath.Join(workspace, "internal", "demo", "dirty.go"), []byte("package demo\n"), 0o644); err != nil {
		t.Fatalf("dirty workspace: %v", err)
	}

	env, stdout, err := executeIndexRepoNavigationCommand(ctx, cfg, newIndexRepoSmartContextCommand(),
		"--workspace", workspace,
		"--node-id", nodes["caller"].ID,
	)
	if err == nil {
		t.Fatalf("smart-context command succeeded, want stale-index error\nstdout:\n%s", stdout)
	}
	if env.Status != envelope.StatusError || env.Command != "index.repo.smart_context" {
		t.Fatalf("unexpected envelope: %+v\nstdout:\n%s", env, stdout)
	}
	if !strings.Contains(stdout, "dirty_state_changed") || !strings.Contains(stdout, "--allow-stale") {
		t.Fatalf("freshness error missing dirty-state detail or override hint:\n%s", stdout)
	}
}

func TestRepoIndexNavigationFreshnessOKAllowsCurrentBehindAndExplicitStale(t *testing.T) {
	if !repoIndexNavigationFreshnessOK(repoindex.IndexFreshnessStatus{Level: repoindex.FreshnessCurrent}, false) {
		t.Fatal("current freshness should be accepted")
	}
	if !repoIndexNavigationFreshnessOK(repoindex.IndexFreshnessStatus{Level: repoindex.FreshnessBehind}, false) {
		t.Fatal("behind freshness should be accepted when HEAD still matches the indexed workspace")
	}
	if repoIndexNavigationFreshnessOK(repoindex.IndexFreshnessStatus{Level: repoindex.FreshnessDirty}, false) {
		t.Fatal("dirty freshness should be rejected without explicit override")
	}
	if !repoIndexNavigationFreshnessOK(repoindex.IndexFreshnessStatus{Level: repoindex.FreshnessStale}, true) {
		t.Fatal("allow-stale should explicitly accept stale freshness")
	}
}

func setupIndexRepoNavigationCommandFixture(t *testing.T, ctx context.Context) (config.Config, string, map[string]repoindex.Node) {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, "repo")
	writeIndexRepoProgressFile(t, workspace, "internal/demo/demo.go", "package demo\n")
	runIndexRepoProgressGit(t, workspace, "init")
	runIndexRepoProgressGit(t, workspace, "config", "user.email", "test@example.invalid")
	runIndexRepoProgressGit(t, workspace, "config", "user.name", "Test User")
	runIndexRepoProgressGit(t, workspace, "add", ".")
	runIndexRepoProgressGit(t, workspace, "commit", "-m", "initial")

	cfg, err := config.Load(ctx)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	store, err := repoindex.Open(ctx, cfg.Storage.Root, workspace)
	if err != nil {
		t.Fatalf("open repoindex store: %v", err)
	}
	defer store.Close()

	repoKey := store.RepoKey()
	pkg := "go:example.com/demo/internal/demo"
	nodes := map[string]repoindex.Node{
		"caller": {
			ID:        repoindex.SymbolID(repoKey, pkg, "Caller"),
			Kind:      repoindex.NodeSymbol,
			Name:      "Caller",
			Pkg:       pkg,
			File:      "internal/demo/demo.go",
			SpanStart: 1,
		},
		"callee": {
			ID:        repoindex.SymbolID(repoKey, pkg, "Callee"),
			Kind:      repoindex.NodeSymbol,
			Name:      "Callee",
			Pkg:       pkg,
			File:      "internal/demo/demo.go",
			SpanStart: 2,
		},
	}
	if err := store.ReplaceAll(ctx, []repoindex.Node{nodes["caller"], nodes["callee"]}, []repoindex.Edge{{
		Src:  nodes["caller"].ID,
		Dst:  nodes["callee"].ID,
		Type: repoindex.EdgeCalls,
	}}); err != nil {
		t.Fatalf("replace graph: %v", err)
	}
	snapshot := repoindex.ResolveGitSnapshot(ctx, workspace)
	meta := repoindex.IndexMetaFromGitSnapshot(repoindex.IndexMeta{
		RepoRoot:  workspace,
		IndexedAt: time.Unix(123, 0).UTC(),
		Languages: []string{"go"},
	}, snapshot)
	if err := store.SetMeta(ctx, meta); err != nil {
		t.Fatalf("set meta: %v", err)
	}

	return cfg, workspace, nodes
}

func executeIndexRepoNavigationCommand(ctx context.Context, cfg config.Config, cmd *cobra.Command, args ...string) (envelope.Envelope, string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetContext(config.WithContext(ctx, cfg))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)

	err := cmd.Execute()
	var env envelope.Envelope
	if decodeErr := json.Unmarshal(stdout.Bytes(), &env); decodeErr != nil && stdout.Len() > 0 {
		return env, stdout.String(), decodeErr
	}
	return env, stdout.String(), err
}
