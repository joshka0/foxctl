package repoindex

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestExtractCSharpUsings(t *testing.T) {
	source := []byte(`global using System;
using Demo.Lib;
using static Demo.Tools.Helpers;
using Alias = Demo.Ignored;
namespace Demo.App;
`)

	got := extractCSharpUsings(source)
	want := []string{"Demo.Lib", "Demo.Tools.Helpers", "System"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extractCSharpUsings() = %#v, want %#v", got, want)
	}
}

func TestLoadCSharpProjectGraphIncludesProjectReferencesAndCompileIncludes(t *testing.T) {
	workspace := t.TempDir()
	mustMkdir(t, filepath.Join(workspace, "App"))
	mustMkdir(t, filepath.Join(workspace, "Lib"))
	mustMkdir(t, filepath.Join(workspace, "Shared"))
	mustWrite(t, filepath.Join(workspace, "App", "App.csproj"), []byte(`<Project Sdk="Microsoft.NET.Sdk">
  <ItemGroup>
    <ProjectReference Include="..\Lib\Lib.csproj" />
    <Compile Include="..\Shared\Linked.cs" />
  </ItemGroup>
</Project>
`))
	mustWrite(t, filepath.Join(workspace, "Lib", "Lib.csproj"), []byte(`<Project Sdk="Microsoft.NET.Sdk" />`))

	graph := loadCSharpProjectGraph(workspace, []string{
		"App/Program.cs",
		"Lib/Service.cs",
		"Shared/Linked.cs",
	})

	if got := graph.ProjectForFile("App/Program.cs"); got != "App/App.csproj" {
		t.Fatalf("ProjectForFile(App/Program.cs) = %q, want App/App.csproj", got)
	}
	if got := graph.ProjectForFile("Shared/Linked.cs"); got != "App/App.csproj" {
		t.Fatalf("ProjectForFile(Shared/Linked.cs) = %q, want App/App.csproj", got)
	}
	if got := graph.References("App/App.csproj"); !reflect.DeepEqual(got, []string{"Lib/Lib.csproj"}) {
		t.Fatalf("References(App/App.csproj) = %#v, want Lib/Lib.csproj", got)
	}
}

func TestBuilderAddsCSharpUsingAndProjectReferenceEdges(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()

	mustMkdir(t, filepath.Join(workspace, "App"))
	mustMkdir(t, filepath.Join(workspace, "Lib"))
	mustWrite(t, filepath.Join(workspace, "App", "App.csproj"), []byte(`<Project Sdk="Microsoft.NET.Sdk">
  <ItemGroup>
    <ProjectReference Include="../Lib/Lib.csproj" />
  </ItemGroup>
</Project>
`))
	mustWrite(t, filepath.Join(workspace, "Lib", "Lib.csproj"), []byte(`<Project Sdk="Microsoft.NET.Sdk" />`))
	mustWrite(t, filepath.Join(workspace, "App", "Program.cs"), []byte(`using Demo.Lib;

namespace Demo.App;

public class Program
{
    public Service NewService() { return new Service(); }
}
`))
	mustWrite(t, filepath.Join(workspace, "Lib", "Service.cs"), []byte(`namespace Demo.Lib;

public class Service {}
`))

	store, err := Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	builder := NewBuilder(store, workspace)
	if _, err := builder.Build(ctx, BuildOptions{
		RepoRoot:      workspace,
		IncludeGo:     false,
		IncludeCSharp: true,
	}); err != nil {
		t.Fatalf("build: %v", err)
	}

	repoKey := store.RepoKey()
	appPkgID := csharpPkgPrefix + "Demo.App"
	libPkgID := csharpPkgPrefix + "Demo.Lib"
	appPkgNodeID := PackageID(repoKey, appPkgID)
	libPkgNodeID := PackageID(repoKey, libPkgID)
	appFileNodeID := FileID(repoKey, appPkgID, "App/Program.cs")
	libFileNodeID := FileID(repoKey, libPkgID, "Lib/Service.cs")

	pkgOutgoing, err := store.GetOutgoingEdges(ctx, appPkgNodeID, []EdgeType{EdgeImports}, 100)
	if err != nil {
		t.Fatalf("get package outgoing edges: %v", err)
	}
	if !containsEdge(pkgOutgoing, appPkgNodeID, libPkgNodeID, EdgeImports) {
		t.Fatalf("expected package IMPORTS edge %s -> %s, got %#v", appPkgNodeID, libPkgNodeID, pkgOutgoing)
	}

	fileOutgoing, err := store.GetOutgoingEdges(ctx, appFileNodeID, []EdgeType{EdgeImports}, 100)
	if err != nil {
		t.Fatalf("get file outgoing edges: %v", err)
	}
	if !containsEdge(fileOutgoing, appFileNodeID, libPkgNodeID, EdgeImports) {
		t.Fatalf("expected file IMPORTS edge %s -> %s, got %#v", appFileNodeID, libPkgNodeID, fileOutgoing)
	}
	if !containsEdge(fileOutgoing, appFileNodeID, libFileNodeID, EdgeImports) {
		t.Fatalf("expected using closure edge %s -> %s, got %#v", appFileNodeID, libFileNodeID, fileOutgoing)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWrite(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
