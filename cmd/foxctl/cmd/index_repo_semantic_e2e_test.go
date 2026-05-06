package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/spf13/cobra"
)

func TestIndexRepoSemanticAnchorsE2EIndexCommentsCoexist(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	cfg, err := config.Load(ctx)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	workspace := filepath.Join(tmp, "repo")
	writeSemanticAnchorE2EFixture(t, workspace)

	buildEnv, _ := executeSemanticAnchorE2ECommand(t, cfg, newIndexRepoBuildCommand(),
		"--workspace", workspace,
		"--semantic-anchors",
		"--include-tests",
		"--typescript=false",
	)
	if buildEnv.Command != "index.repo.build" {
		t.Fatalf("build command=%q want index.repo.build", buildEnv.Command)
	}

	store, err := repoindex.Open(ctx, cfg.Storage.Root, workspace)
	if err != nil {
		t.Fatalf("open repoindex store: %v", err)
	}
	defer store.Close()

	guard := semanticAnchorE2EFindNode(t, store, repoindex.NodeSymbol, "Guard")
	semanticEdges, err := store.GetOutgoingEdges(ctx, guard.ID, repoindex.EdgeSetSemanticAnchors, 20)
	if err != nil {
		t.Fatalf("get semantic edges: %v", err)
	}
	semanticAnchorE2EAssertSemanticEdge(t, semanticEdges, repoindex.EdgeEnforces)
	semanticAnchorE2EAssertSemanticEdge(t, semanticEdges, repoindex.EdgeDescribedBy)
	semanticAnchorE2EAssertSemanticEdge(t, semanticEdges, repoindex.EdgeVerifiedBy)

	indexEdges, err := store.GetOutgoingEdges(ctx, guard.ID, repoindex.EdgeSetDoc, 20)
	if err != nil {
		t.Fatalf("get Index comment edges: %v", err)
	}
	if !semanticAnchorE2EHasEdgeType(indexEdges, repoindex.EdgeHasKeyword) {
		t.Fatalf("Index comment keyword edge missing: %+v", indexEdges)
	}
	if !semanticAnchorE2EHasEdgeType(indexEdges, repoindex.EdgeDocRelated) {
		t.Fatalf("Index comment related edge missing: %+v", indexEdges)
	}

	searchEnv, searchOut := executeSemanticAnchorE2ECommand(t, cfg, newIndexRepoSearchCommand(),
		"--workspace", workspace,
		"--query", "no-send-without-read",
		"--limit", "10",
	)
	if searchEnv.Command != "index.repo.search" {
		t.Fatalf("search command=%q want index.repo.search", searchEnv.Command)
	}
	if !strings.Contains(searchOut, "no-send-without-read") {
		t.Fatalf("search output missing semantic anchor target:\n%s", searchOut)
	}

	expandEnv, expandOut := executeSemanticAnchorE2ECommand(t, cfg, newIndexRepoExpandCommand(),
		"--workspace", workspace,
		"--seed", guard.ID,
		"--semantic-anchors",
		"--depth", "1",
		"--budget", "20",
	)
	if expandEnv.Command != "index.repo.expand" {
		t.Fatalf("expand command=%q want index.repo.expand", expandEnv.Command)
	}
	if !strings.Contains(expandOut, string(repoindex.EdgeEnforces)) || !strings.Contains(expandOut, `"include_semantic_anchors":true`) {
		t.Fatalf("expand output missing semantic anchor traversal:\n%s", expandOut)
	}
}

func executeSemanticAnchorE2ECommand(t *testing.T, cfg config.Config, cmd *cobra.Command, args ...string) (envelope.Envelope, string) {
	t.Helper()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute %s: %v\nstdout:\n%s\nstderr:\n%s", cmd.Name(), err, stdout.String(), stderr.String())
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode %s envelope: %v\nstdout:\n%s", cmd.Name(), err, stdout.String())
	}
	if env.Status != envelope.StatusOK {
		t.Fatalf("%s status=%q error=%+v stdout:\n%s", cmd.Name(), env.Status, env.Error, stdout.String())
	}
	return env, stdout.String()
}

func writeSemanticAnchorE2EFixture(t *testing.T, workspace string) {
	t.Helper()
	semanticAnchorE2EWriteFile(t, filepath.Join(workspace, "go.mod"), "module example.com/anchors\n\ngo 1.22\n")
	semanticAnchorE2EWriteFile(t, filepath.Join(workspace, "docs", "terminal-safety.md"), "# Terminal Safety\n\nRead before write.\n")
	semanticAnchorE2EWriteFile(t, filepath.Join(workspace, "internal", "demo", "demo_test.go"), `package demo

import "testing"

func TestGuardReadBeforeWrite(t *testing.T) {}
`)
	semanticAnchorE2EWriteFile(t, filepath.Join(workspace, "internal", "demo", "demo.go"), `package demo

// Guard enforces read-before-write terminal safety.
//
// Index:
//   Purpose: Protects terminal writes from stale unread output.
//   Keywords: terminal safety, read before write
//   Related: GuardHelper
//
// [[foxctl:invariant/no-send-without-read]]
// [[doc:docs/terminal-safety.md#Terminal Safety]]
// [[test:internal/demo/demo_test.go#TestGuardReadBeforeWrite]]
func Guard() {
	GuardHelper()
}

// GuardHelper is related by the Guard Index block.
func GuardHelper() {}
`)
}

func semanticAnchorE2EWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func semanticAnchorE2EFindNode(t *testing.T, store *repoindex.Store, kind repoindex.NodeKind, name string) repoindex.Node {
	t.Helper()
	nodes, err := store.ListNodesByKind(context.Background(), kind, 200)
	if err != nil {
		t.Fatalf("list %s nodes: %v", kind, err)
	}
	for _, node := range nodes {
		if node.Name == name {
			return node
		}
	}
	t.Fatalf("missing %s node named %q in %+v", kind, name, nodes)
	return repoindex.Node{}
}

func semanticAnchorE2EAssertSemanticEdge(t *testing.T, edges []repoindex.Edge, edgeType repoindex.EdgeType) {
	t.Helper()
	for _, edge := range edges {
		if edge.Type != edgeType {
			continue
		}
		if _, present, err := repoindex.DecodeAndValidateSemanticAnchorEdge(edge); err != nil || !present {
			t.Fatalf("semantic edge %s failed validation: present=%v err=%v edge=%+v", edgeType, present, err, edge)
		}
		return
	}
	t.Fatalf("missing semantic edge %s in %+v", edgeType, edges)
}

func semanticAnchorE2EHasEdgeType(edges []repoindex.Edge, edgeType repoindex.EdgeType) bool {
	for _, edge := range edges {
		if edge.Type == edgeType {
			return true
		}
	}
	return false
}
