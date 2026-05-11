package repoindex

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestBuildLadybugExportGroupsTypedCSVFiles(t *testing.T) {
	snapshot, exportedAt, repoRoot := ladybugExportFixture()

	artifact, err := BuildLadybugExport(snapshot, exportedAt)
	if err != nil {
		t.Fatalf("build export: %v", err)
	}

	wantFiles := []string{
		"meta.json",
		"nodes_graph.csv",
		"edges_calls.csv",
		"edges_contains.csv",
		"edges_co_changes_with.csv",
		"edges_described_by.csv",
	}
	if got := ladybugArtifactFileNames(artifact); !reflect.DeepEqual(got, wantFiles) {
		t.Fatalf("files=%v want %v", got, wantFiles)
	}
	if _, ok := ladybugArtifactFile(artifact, "edges_imports.csv"); ok {
		t.Fatalf("empty edge file was emitted")
	}
	if _, ok := ladybugArtifactFile(artifact, "nodes_package.csv"); ok {
		t.Fatalf("per-kind node file was emitted")
	}

	wantGraphCSV := "id,kind,repo_key,repo_root_identity,pkg,file,name,signature,span_start,span_end,exported,doc,summary,hash,updated_at,meta_json\n" +
		"repo-fixture,repo,repo-fixture,repo-fixture,,,repo-fixture,,0,0,false,,,,,\n" +
		"repo-fixture::pkg:go:example,package,repo-fixture,repo-fixture,go:example,,example,,0,0,false,,package summary,pkg-hash,2026-05-11T12:00:00Z,\"{\"\"a\"\":1,\"\"z\"\":2}\"\n" +
		"repo-fixture::file:go:example:internal/a.go,file,repo-fixture,repo-fixture,go:example,internal/a.go,a.go,,0,0,false,,,file-a-hash,2026-05-11T12:00:00Z,\n" +
		"repo-fixture::file:go:example:internal/b.go,file,repo-fixture,repo-fixture,go:example,internal/b.go,b.go,,0,0,false,,,file-b-hash,2026-05-11T12:00:00Z,\n" +
		"repo-fixture::sym:go:example:A,symbol,repo-fixture,repo-fixture,go:example,internal/a.go,A,func A(),3,5,true,\"A docs, with \"\"quote\"\"\",,symbol-a-hash,2026-05-11T12:00:00Z,\n" +
		"repo-fixture::sym:go:example:B,symbol,repo-fixture,repo-fixture,go:example,internal/b.go,B,func B(),7,9,true,,,symbol-b-hash,2026-05-11T12:00:00Z,\n" +
		"repo-fixture::kw:alpha,concept,repo-fixture,repo-fixture,,,alpha,,0,0,false,,,,2026-05-11T12:00:00Z,\"{\"\"kind\"\":\"\"keyword\"\"}\"\n"
	if got := string(mustLadybugArtifactFile(t, artifact, "nodes_graph.csv")); got != wantGraphCSV {
		t.Fatalf("nodes_graph.csv:\n%s\nwant:\n%s", got, wantGraphCSV)
	}

	wantCoChangeCSV := "src_id,dst_id,weight,source,meta_json\n" +
		"repo-fixture::file:go:example:internal/a.go,repo-fixture::file:go:example:internal/b.go,0.75,repoindex,\"{\"\"count\"\":2,\"\"source\"\":\"\"git\"\"}\"\n"
	if got := string(mustLadybugArtifactFile(t, artifact, "edges_co_changes_with.csv")); got != wantCoChangeCSV {
		t.Fatalf("edges_co_changes_with.csv:\n%s\nwant:\n%s", got, wantCoChangeCSV)
	}

	for _, file := range artifact.Files {
		if bytes.Contains(file.Data, []byte(repoRoot)) {
			t.Fatalf("%s contains absolute repo root %q", file.Name, repoRoot)
		}
	}
}

func TestBuildLadybugExportCanonicalMetadataAndRepoIdentity(t *testing.T) {
	snapshot, exportedAt, _ := ladybugExportFixture()

	artifact, err := BuildLadybugExport(snapshot, exportedAt)
	if err != nil {
		t.Fatalf("build export: %v", err)
	}

	wantMetaJSON := "{\"format\":\"ladybugdb-repoindex-export\",\"version\":1,\"exported_at\":\"2026-05-11T15:04:05Z\",\"repo_key\":\"repo-fixture\",\"repo_root_identity\":\"repo-fixture\",\"repoindex_schema_version\":4,\"head_sha\":\"abc123\",\"worktree_dirty\":true,\"dirty_status_hash\":\"dirty-hash\",\"default_ref\":\"origin/main\",\"default_ref_sha\":\"def456\",\"merge_base_sha\":\"base789\",\"commits_ahead\":2,\"commits_behind\":1,\"indexed_at\":\"2026-05-10T09:08:07Z\",\"languages\":[\"go\",\"typescript\"]}\n"
	if got := string(mustLadybugArtifactFile(t, artifact, "meta.json")); got != wantMetaJSON {
		t.Fatalf("meta.json:\n%s\nwant:\n%s", got, wantMetaJSON)
	}

	wantRepoRow := "repo-fixture,repo,repo-fixture,repo-fixture,,,repo-fixture,,0,0,false,,,,,"
	if got := string(mustLadybugArtifactFile(t, artifact, "nodes_graph.csv")); !strings.Contains(got, wantRepoRow) {
		t.Fatalf("nodes_graph.csv missing portable repo row:\n%s", got)
	}
}

func TestBuildLadybugExportRejectsInvalidJSONMetadata(t *testing.T) {
	exportedAt := time.Date(2026, 5, 11, 15, 4, 5, 0, time.UTC)
	t.Run("node", func(t *testing.T) {
		snapshot := ExportSnapshot{
			RepoKey: "repo-fixture",
			Meta: IndexMeta{
				SchemaVersion: SchemaVersion(),
			},
			Nodes: []Node{
				{
					ID:   "bad-node",
					Kind: NodeFile,
					Meta: []byte(`{"bad"`),
				},
			},
		}

		_, err := BuildLadybugExport(snapshot, exportedAt)
		if err == nil {
			t.Fatalf("expected invalid JSON error")
		}
		if !strings.Contains(err.Error(), "invalid node meta bad-node JSON") {
			t.Fatalf("error=%q want invalid node meta", err.Error())
		}
	})

	t.Run("edge", func(t *testing.T) {
		snapshot := ExportSnapshot{
			RepoKey: "repo-fixture",
			Meta: IndexMeta{
				SchemaVersion: SchemaVersion(),
			},
			Edges: []Edge{
				{Src: "src", Dst: "dst", Type: EdgeCalls, Weight: 1, Meta: []byte(`{"bad"`)},
			},
		}

		_, err := BuildLadybugExport(snapshot, exportedAt)
		if err == nil {
			t.Fatalf("expected invalid JSON error")
		}
		if !strings.Contains(err.Error(), "invalid edge meta src -> dst CALLS JSON") {
			t.Fatalf("error=%q want invalid edge meta", err.Error())
		}
	})
}

func TestBuildLadybugExportNormalizesMultilineTextForLadybugCopy(t *testing.T) {
	exportedAt := time.Date(2026, 5, 11, 15, 4, 5, 0, time.UTC)
	snapshot := ExportSnapshot{
		RepoKey: "repo-fixture",
		Meta: IndexMeta{
			SchemaVersion: SchemaVersion(),
		},
		Nodes: []Node{
			{
				ID:   "repo-fixture::sym:go:example:Multiline",
				Kind: NodeSymbol,
				Pkg:  "go:example",
				File: "internal/multiline.go",
				Name: "Multiline",
				Signature: `func Multiline(
input string, option string) "value"`,
				Doc: "first line\nsecond, line\r\nthird line",
				Summary: `summary one
summary two, "quoted"`,
			},
		},
	}

	artifact, err := BuildLadybugExport(snapshot, exportedAt)
	if err != nil {
		t.Fatalf("build export: %v", err)
	}

	got := string(mustLadybugArtifactFile(t, artifact, "nodes_graph.csv"))
	for _, want := range []string{
		`func Multiline(\ninput string, option string) ""value""`,
		`first line\nsecond, line\nthird line`,
		`summary one\nsummary two, ""quoted""`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nodes_graph.csv missing normalized field %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "first line\nsecond") || strings.Contains(got, "summary one\rsummary two") {
		t.Fatalf("nodes_graph.csv contains raw multiline text:\n%s", got)
	}
}

func TestWriteLadybugExportReplaceAll(t *testing.T) {
	snapshot, exportedAt, _ := ladybugExportFixture()
	artifact, err := BuildLadybugExport(snapshot, exportedAt)
	if err != nil {
		t.Fatalf("build export: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "edges_imports.csv"), []byte("stale\n"), 0o644); err != nil {
		t.Fatalf("write stale file: %v", err)
	}

	if err := WriteLadybugExport(dir, artifact); err != nil {
		t.Fatalf("write export: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "edges_imports.csv")); !os.IsNotExist(err) {
		t.Fatalf("stale file still exists, stat err=%v", err)
	}
	first := readLadybugExportDir(t, dir)
	if got := string(first["nodes_graph.csv"]); !strings.Contains(got, "repo-fixture,repo,repo-fixture,repo-fixture") {
		t.Fatalf("nodes_graph.csv missing repo identity row:\n%s", got)
	}

	if err := WriteLadybugExport(dir, artifact); err != nil {
		t.Fatalf("rewrite export: %v", err)
	}
	second := readLadybugExportDir(t, dir)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("repeated write changed files:\nfirst=%v\nsecond=%v", first, second)
	}
}

func TestWriteLadybugExportValidatesBeforeReplace(t *testing.T) {
	dir := t.TempDir()
	keepPath := filepath.Join(dir, "keep.csv")
	if err := os.WriteFile(keepPath, []byte("keep\n"), 0o644); err != nil {
		t.Fatalf("write keep file: %v", err)
	}

	err := WriteLadybugExport(dir, LadybugExportArtifact{Files: []LadybugExportFile{
		{Name: "../bad.csv", Data: []byte("bad\n")},
	}})
	if err == nil {
		t.Fatalf("expected invalid filename error")
	}
	if _, statErr := os.Stat(keepPath); statErr != nil {
		t.Fatalf("writer cleared directory before validation: %v", statErr)
	}
}

func ladybugExportFixture() (ExportSnapshot, time.Time, string) {
	repoKey := "repo-fixture"
	repoRoot := "/abs/local/checkout/foxctl"
	indexedAt := time.Date(2026, 5, 10, 9, 8, 7, 0, time.UTC)
	updatedAt := time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC)
	pkg := "go:example"
	pkgNode := Node{
		ID:        PackageID(repoKey, pkg),
		Kind:      NodePackage,
		Pkg:       pkg,
		Name:      "example",
		Summary:   "package summary",
		Meta:      []byte(`{"z":2,"a":1}`),
		Hash:      "pkg-hash",
		UpdatedAt: updatedAt,
	}
	fileA := Node{
		ID:        FileID(repoKey, pkg, "internal/a.go"),
		Kind:      NodeFile,
		Pkg:       pkg,
		File:      "internal/a.go",
		Name:      "a.go",
		Hash:      "file-a-hash",
		UpdatedAt: updatedAt,
	}
	fileB := Node{
		ID:        FileID(repoKey, pkg, "internal/b.go"),
		Kind:      NodeFile,
		Pkg:       pkg,
		File:      "internal/b.go",
		Name:      "b.go",
		Hash:      "file-b-hash",
		UpdatedAt: updatedAt,
	}
	symbolA := Node{
		ID:        SymbolID(repoKey, pkg, "A"),
		Kind:      NodeSymbol,
		Pkg:       pkg,
		File:      "internal/a.go",
		Name:      "A",
		Signature: "func A()",
		SpanStart: 3,
		SpanEnd:   5,
		Exported:  true,
		Doc:       `A docs, with "quote"`,
		Hash:      "symbol-a-hash",
		UpdatedAt: updatedAt,
	}
	symbolB := Node{
		ID:        SymbolID(repoKey, pkg, "B"),
		Kind:      NodeSymbol,
		Pkg:       pkg,
		File:      "internal/b.go",
		Name:      "B",
		Signature: "func B()",
		SpanStart: 7,
		SpanEnd:   9,
		Exported:  true,
		Hash:      "symbol-b-hash",
		UpdatedAt: updatedAt,
	}
	concept := Node{
		ID:        NamespacedID(repoKey, ConceptKeyword+"alpha"),
		Kind:      NodeConcept,
		Name:      "alpha",
		Meta:      []byte(`{"kind":"keyword"}`),
		UpdatedAt: updatedAt,
	}
	return ExportSnapshot{
		RepoKey: repoKey,
		Meta: IndexMeta{
			RepoRoot:        repoRoot,
			HeadSHA:         "abc123",
			WorktreeDirty:   true,
			DirtyStatusHash: "dirty-hash",
			DefaultRef:      "origin/main",
			DefaultRefSHA:   "def456",
			MergeBaseSHA:    "base789",
			CommitsAhead:    2,
			CommitsBehind:   1,
			SchemaVersion:   SchemaVersion(),
			IndexedAt:       indexedAt,
			Languages:       []string{"go", "typescript"},
		},
		Nodes: []Node{symbolB, concept, fileB, pkgNode, symbolA, fileA},
		Edges: []Edge{
			{Src: fileA.ID, Dst: symbolA.ID, Type: EdgeContains, Weight: 1},
			{Src: symbolA.ID, Dst: symbolB.ID, Type: EdgeCalls, Weight: 1, Meta: []byte(`{"target":"B","kind":"call"}`)},
			{Src: symbolA.ID, Dst: concept.ID, Type: EdgeDescribedBy, Weight: 0.5},
			{Src: fileA.ID, Dst: fileB.ID, Type: EdgeCoChangesWith, Weight: 0.75, Meta: []byte(`{"source":"git","count":2}`)},
		},
	}, time.Date(2026, 5, 11, 15, 4, 5, 0, time.UTC), repoRoot
}

func ladybugArtifactFileNames(artifact LadybugExportArtifact) []string {
	names := make([]string, 0, len(artifact.Files))
	for _, file := range artifact.Files {
		names = append(names, file.Name)
	}
	return names
}

func ladybugArtifactFile(artifact LadybugExportArtifact, name string) ([]byte, bool) {
	for _, file := range artifact.Files {
		if file.Name == name {
			return file.Data, true
		}
	}
	return nil, false
}

func mustLadybugArtifactFile(t *testing.T, artifact LadybugExportArtifact, name string) []byte {
	t.Helper()
	data, ok := ladybugArtifactFile(artifact, name)
	if !ok {
		t.Fatalf("missing artifact file %s", name)
	}
	return data
}

func readLadybugExportDir(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read export dir: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	out := make(map[string][]byte, len(names))
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read export file %s: %v", name, err)
		}
		out[name] = data
	}
	return out
}
