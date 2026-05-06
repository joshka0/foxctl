package obsidian

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
)

func TestBuildRepoGraphDrafts(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	storageRoot := filepath.Join(root, "storage")
	repoRoot := filepath.Join(root, "repo")
	vaultRoot := filepath.Join(root, "vault")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo root: %v", err)
	}
	if err := os.MkdirAll(vaultRoot, 0o755); err != nil {
		t.Fatalf("mkdir vault root: %v", err)
	}

	repo, err := repoindex.Open(ctx, storageRoot, repoRoot)
	if err != nil {
		t.Fatalf("open repo index: %v", err)
	}
	defer repo.Close()
	key := repo.RepoKey()
	now := time.Now().UTC()

	nodes := []repoindex.Node{
		{ID: repoindex.PackageID(key, "go:internal/context/contextplane"), Kind: repoindex.NodePackage, Pkg: "go:internal/context/contextplane", Name: "internal/context/contextplane", UpdatedAt: now},
		{ID: repoindex.FileID(key, "go:internal/context/contextplane", "internal/context/contextplane/store.go"), Kind: repoindex.NodeFile, Pkg: "go:internal/context/contextplane", File: "internal/context/contextplane/store.go", Name: "store.go", UpdatedAt: now},
		{ID: repoindex.SymbolID(key, "go:internal/context/contextplane", "WorkspaceStore"), Kind: repoindex.NodeSymbol, Pkg: "go:internal/context/contextplane", File: "internal/context/contextplane/store.go", Name: "WorkspaceStore", UpdatedAt: now},
		{ID: repoindex.NamespacedID(key, "kw:workspace-store"), Kind: repoindex.NodeConcept, Pkg: "go:internal/context/contextplane", File: "internal/context/contextplane/store.go", Name: "workspace store", UpdatedAt: now},
		{ID: repoindex.NamespacedID(key, "anchor:foxctl:invariant:no-send-without-read"), Kind: repoindex.NodeConcept, Pkg: "go:internal/context/contextplane", File: "internal/context/contextplane/store.go", Name: "anchor invariant", UpdatedAt: now},
		{ID: repoindex.NamespacedID(key, "anchor:foxctl:risk:global-anchor"), Kind: repoindex.NodeConcept, Pkg: "tf:infra", File: "infra/main.tf", Name: "global anchor", UpdatedAt: now},
		{ID: repoindex.PackageID(key, "go:internal/storage/obsidianindex"), Kind: repoindex.NodePackage, Pkg: "go:internal/storage/obsidianindex", Name: "internal/storage/obsidianindex", UpdatedAt: now},
		{ID: repoindex.FileID(key, "go:internal/storage/obsidianindex", "internal/storage/obsidianindex/store.go"), Kind: repoindex.NodeFile, Pkg: "go:internal/storage/obsidianindex", File: "internal/storage/obsidianindex/store.go", Name: "store.go", UpdatedAt: now},
		{ID: repoindex.SymbolID(key, "go:internal/storage/obsidianindex", "Store"), Kind: repoindex.NodeSymbol, Pkg: "go:internal/storage/obsidianindex", File: "internal/storage/obsidianindex/store.go", Name: "Store", UpdatedAt: now},
	}
	edges := []repoindex.Edge{
		{Src: repoindex.PackageID(key, "go:internal/context/contextplane"), Dst: repoindex.FileID(key, "go:internal/context/contextplane", "internal/context/contextplane/store.go"), Type: repoindex.EdgeContains, Weight: 1},
		{Src: repoindex.PackageID(key, "go:internal/context/contextplane"), Dst: repoindex.NamespacedID(key, "kw:workspace-store"), Type: repoindex.EdgeContains, Weight: 1},
		{Src: repoindex.PackageID(key, "go:internal/context/contextplane"), Dst: repoindex.NamespacedID(key, "anchor:foxctl:invariant:no-send-without-read"), Type: repoindex.EdgeContains, Weight: 1},
		{Src: repoindex.FileID(key, "go:internal/context/contextplane", "internal/context/contextplane/store.go"), Dst: repoindex.SymbolID(key, "go:internal/context/contextplane", "WorkspaceStore"), Type: repoindex.EdgeContains, Weight: 1},
		{Src: repoindex.PackageID(key, "go:internal/context/contextplane"), Dst: repoindex.PackageID(key, "go:internal/storage/obsidianindex"), Type: repoindex.EdgeImports, Weight: 1},
		{Src: repoindex.PackageID(key, "go:internal/storage/obsidianindex"), Dst: repoindex.FileID(key, "go:internal/storage/obsidianindex", "internal/storage/obsidianindex/store.go"), Type: repoindex.EdgeContains, Weight: 1},
		{Src: repoindex.FileID(key, "go:internal/storage/obsidianindex", "internal/storage/obsidianindex/store.go"), Dst: repoindex.SymbolID(key, "go:internal/storage/obsidianindex", "Store"), Type: repoindex.EdgeContains, Weight: 1},
		{Src: repoindex.SymbolID(key, "go:internal/context/contextplane", "WorkspaceStore"), Dst: repoindex.SymbolID(key, "go:internal/storage/obsidianindex", "Store"), Type: repoindex.EdgeRefersTo, Weight: 1},
	}
	if err := repo.ReplaceAll(ctx, nodes, edges); err != nil {
		t.Fatalf("replace all: %v", err)
	}

	script := filepath.Join(root, "obsidian")
	content := `#!/bin/sh
cmd="$1"; shift
path=""
payload=""
for arg in "$@"; do
  case "$arg" in
    path=*) path="${arg#path=}" ;;
    content=*) payload="${arg#content=}" ;;
  esac
done
root="` + vaultRoot + `"
full="$root/$path"
case "$cmd" in
  create)
    mkdir -p "$(dirname "$full")"
    printf "%s" "$payload" > "$full"
    ;;
  read)
    if [ ! -f "$full" ]; then
      echo "File not found." 1>&2
      exit 1
    fi
    cat "$full"
    ;;
  vaults)
    printf "TestVault\t%s\n" "$root"
    ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}

	writer := NewWriter(script, "TestVault", DefaultPolicy())
	writer.VaultPath = vaultRoot
	writer.PostCreateDelay = 0
	result, err := BuildRepoGraphDrafts(ctx, writer, repo, RepoGraphBuildOptions{
		Project:       "foxctl",
		WorkspaceRoot: repoRoot,
		MaxPackages:   4,
	})
	if err != nil {
		t.Fatalf("BuildRepoGraphDrafts: %v", err)
	}
	if result.RootNotePath == "" || len(result.PackageNotes) == 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if len(result.ConceptNotes) == 0 {
		t.Fatalf("expected at least one non-anchor concept note")
	}
	for _, note := range result.ConceptNotes {
		if strings.Contains(note, "anchor") {
			t.Fatalf("anchor concept note generated by default: %#v", result.ConceptNotes)
		}
	}

	rootBody, err := os.ReadFile(filepath.Join(vaultRoot, result.RootNotePath))
	if err != nil {
		t.Fatalf("read root note: %v", err)
	}
	if !strings.Contains(string(rootBody), "[[context contextplane]]") {
		t.Fatalf("expected package wikilink in root note:\n%s", string(rootBody))
	}

	packageBody, err := os.ReadFile(filepath.Join(vaultRoot, result.PackageNotes[0]))
	if err != nil {
		t.Fatalf("read package note: %v", err)
	}
	text := string(packageBody)
	if !strings.Contains(text, "paths:") || !strings.Contains(text, "primary_anchor_path:") || !strings.Contains(text, "impl_anchor_paths:") || !strings.Contains(text, "symbols:") || !strings.Contains(text, "WorkspaceStore") {
		t.Fatalf("expected repo graph metadata in package note:\n%s", text)
	}
	if !strings.Contains(text, "[[storage obsidianindex]]") {
		t.Fatalf("expected related package link in package note:\n%s", text)
	}

	promoted, err := PromoteRepoGraphDrafts(ctx, writer, result.Folder, "notes/repo/foxctl")
	if err != nil {
		t.Fatalf("PromoteRepoGraphDrafts: %v", err)
	}
	if len(promoted.Merged) == 0 {
		t.Fatalf("expected promoted graph notes")
	}
	canonicalRoot := filepath.Join(vaultRoot, "notes", "repo", "foxctl", "index.md")
	body, err := os.ReadFile(canonicalRoot)
	if err != nil {
		t.Fatalf("read canonical root note: %v", err)
	}
	if !strings.Contains(string(body), "status: reviewed") || !strings.Contains(string(body), "trust: canonical") {
		t.Fatalf("expected canonicalized graph root:\n%s", string(body))
	}

	withAnchors, err := BuildRepoGraphDrafts(ctx, writer, repo, RepoGraphBuildOptions{
		Project:               "foxctl",
		WorkspaceRoot:         repoRoot,
		MaxPackages:           4,
		IncludeAnchorConcepts: true,
	})
	if err != nil {
		t.Fatalf("BuildRepoGraphDrafts with anchors: %v", err)
	}
	foundAnchorConcept := false
	for _, note := range withAnchors.ConceptNotes {
		if strings.Contains(note, "anchor") {
			noteBody, err := os.ReadFile(filepath.Join(vaultRoot, filepath.FromSlash(note)))
			if err != nil {
				t.Fatalf("read anchor concept note: %v", err)
			}
			if !strings.Contains(string(noteBody), "repo_anchors:") || !strings.Contains(string(noteBody), "anchor:foxctl:invariant:no-send-without-read") {
				t.Fatalf("expected anchor concept note to carry repo_anchors frontmatter:\n%s", string(noteBody))
			}
			foundAnchorConcept = true
			break
		}
	}
	if !foundAnchorConcept {
		t.Fatalf("expected explicit anchor concept opt-in to produce anchor concept note: %#v", withAnchors.ConceptNotes)
	}
}
