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
	if err := os.WriteFile(filepath.Join(workspace, "src", "b.ts"), []byte("export function imported() { return 2 }\n"), 0o644); err != nil {
		t.Fatalf("write b.ts: %v", err)
	}
	ts := `import { imported } from "./b"
export function bar() { return 1 }
export class Zoo {}
export function foo() {
  imported()
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
	fooID := SymbolID(repoKey, pkgID, "foo")
	barID := SymbolID(repoKey, pkgID, "bar")
	zooID := SymbolID(repoKey, pkgID, "Zoo")

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

	fileID := FileID(repoKey, pkgID, "src/a.ts")
	targetFileID := FileID(repoKey, pkgID, "src/b.ts")
	fileOutgoing, err := store.GetOutgoingEdges(ctx, fileID, []EdgeType{EdgeImports}, 100)
	if err != nil {
		t.Fatalf("get file outgoing edges: %v", err)
	}
	if !containsEdge(fileOutgoing, fileID, targetFileID, EdgeImports) {
		t.Fatalf("expected file IMPORTS edge %s -> %s", fileID, targetFileID)
	}

	importedID := SymbolID(repoKey, pkgID, "imported")
	fileSymbolOutgoing, err := store.GetOutgoingEdges(ctx, fileID, []EdgeType{EdgeUsesSymbol}, 100)
	if err != nil {
		t.Fatalf("get file symbol outgoing edges: %v", err)
	}
	if !containsEdge(fileSymbolOutgoing, fileID, importedID, EdgeUsesSymbol) {
		t.Fatalf("expected file USES_SYMBOL edge %s -> %s", fileID, importedID)
	}
}

func TestBuilderSkipsTypeScriptDeclAndPatchFiles(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()

	if err := os.WriteFile(filepath.Join(workspace, "package.json"), []byte(`{}`+"\n"), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "src"), 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, ".pnpm-patches", "pkg"), 0o755); err != nil {
		t.Fatalf("mkdir patches: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "src", "a.ts"), []byte("export const A = 1\n"), 0o644); err != nil {
		t.Fatalf("write a.ts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "src", "types.d.ts"), []byte("export interface Types {}\n"), 0o644); err != nil {
		t.Fatalf("write d.ts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".pnpm-patches", "pkg", "patch.ts"), []byte("export const P = 1\n"), 0o644); err != nil {
		t.Fatalf("write patch.ts: %v", err)
	}

	store, err := Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	builder := NewBuilder(store, workspace)
	result, err := builder.Build(ctx, BuildOptions{
		RepoRoot:          workspace,
		IncludeGo:         false,
		IncludeTypescript: true,
		IncludeElixir:     false,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if result.Files != 1 {
		t.Fatalf("expected 1 indexed TS file, got %d", result.Files)
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
	fooID := SymbolID(repoKey, pkgID, "foo")
	modBID := SymbolID(repoKey, pkgID, "MyApp.B")

	outgoing, err := store.GetOutgoingEdges(ctx, fooID, []EdgeType{EdgeRefersTo}, 100)
	if err != nil {
		t.Fatalf("get outgoing edges: %v", err)
	}
	if !containsEdge(outgoing, fooID, modBID, EdgeRefersTo) {
		t.Fatalf("expected REFERS_TO edge %s -> %s", fooID, modBID)
	}
}

func TestBuilderAddsElixirDefimplRelations(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()

	if err := os.MkdirAll(filepath.Join(workspace, "lib"), 0o755); err != nil {
		t.Fatalf("mkdir lib: %v", err)
	}
	protocol := `defprotocol MyApp.DirectiveExec do
  def exec(directive, state)
end
`
	directive := `defmodule MyApp.Directive.Emit do
end
`
	impl := `defimpl MyApp.DirectiveExec, for: MyApp.Directive.Emit do
  def exec(_directive, state), do: {:ok, state}
end
`
	if err := os.WriteFile(filepath.Join(workspace, "lib", "directive_exec.ex"), []byte(protocol), 0o644); err != nil {
		t.Fatalf("write protocol: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "lib", "emit.ex"), []byte(directive), 0o644); err != nil {
		t.Fatalf("write directive: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "lib", "directive_executors.ex"), []byte(impl), 0o644); err != nil {
		t.Fatalf("write impl: %v", err)
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
	pkgID := elixirPackageID("lib/directive_executors.ex")
	fileID := FileID(repoKey, pkgID, "lib/directive_executors.ex")
	protocolID := SymbolID(repoKey, pkgID, "MyApp.DirectiveExec")
	directiveID := SymbolID(repoKey, pkgID, "MyApp.Directive.Emit")

	outgoing, err := store.GetOutgoingEdges(ctx, fileID, []EdgeType{EdgeImplements, EdgeUsesSymbol}, 100)
	if err != nil {
		t.Fatalf("get outgoing edges: %v", err)
	}
	if !containsEdge(outgoing, fileID, protocolID, EdgeImplements) {
		t.Fatalf("expected IMPLEMENTS edge %s -> %s", fileID, protocolID)
	}
	if !containsEdge(outgoing, fileID, directiveID, EdgeUsesSymbol) {
		t.Fatalf("expected USES_SYMBOL edge %s -> %s", fileID, directiveID)
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
