package repoindex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBuilderAddsTypeScriptCallEdges(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()

	// Mark TS module root.
	if err := os.WriteFile(filepath.Join(workspace, "package.json"), []byte(`{}`+"\n"), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "src"), 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	ts := `export function bar() { return 1 }
export class Zoo {}
export function foo() {
  bar()
  new Zoo()
  return 0
}
`
	if err := os.WriteFile(filepath.Join(workspace, "src", "a.ts"), []byte(ts), 0o644); err != nil {
		t.Fatalf("write ts file: %v", err)
	}

	store, err := Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	builder := NewBuilder(store, workspace)
	if _, err := builder.Build(ctx, BuildOptions{
		RepoRoot:          workspace,
		IncludeGo:         false,
		IncludeTypescript: true,
		IncludeElixir:     false,
	}); err != nil {
		t.Fatalf("build: %v", err)
	}

	repoKey := store.RepoKey()
	pkgID := tsLocalPrefix + "."
	fooID := SymbolID(repoKey, pkgID, "src/a.ts:foo")
	barID := SymbolID(repoKey, pkgID, "src/a.ts:bar")
	zooID := SymbolID(repoKey, pkgID, "src/a.ts:Zoo")

	outgoing, err := store.GetOutgoingEdges(ctx, fooID, []EdgeType{EdgeCalls}, 100)
	if err != nil {
		t.Fatalf("get outgoing edges: %v", err)
	}
	if !containsEdge(outgoing, fooID, barID, EdgeCalls) {
		t.Fatalf("expected CALLS edge %s -> %s", fooID, barID)
	}
	if !containsEdge(outgoing, fooID, zooID, EdgeCalls) {
		t.Fatalf("expected CALLS edge %s -> %s", fooID, zooID)
	}
}

func TestBuilderAddsElixirRefersToEdges(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()

	if err := os.MkdirAll(filepath.Join(workspace, "lib"), 0o755); err != nil {
		t.Fatalf("mkdir lib: %v", err)
	}
	a := `defmodule MyApp.A do
  def foo do
    MyApp.B.bar()
  end
end
`
	b := `defmodule MyApp.B do
  def bar, do: :ok
end
`
	if err := os.WriteFile(filepath.Join(workspace, "lib", "a.ex"), []byte(a), 0o644); err != nil {
		t.Fatalf("write a.ex: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "lib", "b.ex"), []byte(b), 0o644); err != nil {
		t.Fatalf("write b.ex: %v", err)
	}

	store, err := Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	builder := NewBuilder(store, workspace)
	if _, err := builder.Build(ctx, BuildOptions{
		RepoRoot:          workspace,
		IncludeGo:         false,
		IncludeTypescript: false,
		IncludeElixir:     true,
	}); err != nil {
		t.Fatalf("build: %v", err)
	}

	repoKey := store.RepoKey()
	pkgID := elixirPackageID("lib/a.ex")
	fooID := SymbolID(repoKey, pkgID, "lib/a.ex:foo")
	modBID := SymbolID(repoKey, pkgID, "lib/b.ex:MyApp.B")

	outgoing, err := store.GetOutgoingEdges(ctx, fooID, []EdgeType{EdgeRefersTo}, 100)
	if err != nil {
		t.Fatalf("get outgoing edges: %v", err)
	}
	if !containsEdge(outgoing, fooID, modBID, EdgeRefersTo) {
		t.Fatalf("expected REFERS_TO edge %s -> %s", fooID, modBID)
	}
}

func containsEdge(edges []Edge, src, dst string, typ EdgeType) bool {
	for _, edge := range edges {
		if edge.Src == src && edge.Dst == dst && edge.Type == typ {
			return true
		}
	}
	return false
}
