package changes

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing"
	refscope "github.com/joshka0/foxctl/internal/intelligence/refactor/scope"
	refsnapshot "github.com/joshka0/foxctl/internal/intelligence/refactor/snapshot"
	refsnapshotstore "github.com/joshka0/foxctl/internal/intelligence/refactor/snapshotstore"
	"github.com/joshka0/foxctl/internal/storage/cas"
)

func TestDiffSnapshotsDetectsAddedModifiedDeletedFilesAndSymbols(t *testing.T) {
	t.Parallel()

	prev := refsnapshot.Payload{
		Files: []refsnapshot.FileSnapshot{
			{Path: "a.go", Language: "go", Hash: "sha256:a1"},
			{Path: "b.go", Language: "go", Hash: "sha256:b1"},
		},
		Symbols: []refsnapshot.SymbolSnapshot{
			{Path: "a.go", SymbolID: "a:Alpha", Name: "Alpha", Kind: "function", Hash: "sha256:s1"},
			{Path: "b.go", SymbolID: "b:Beta", Name: "Beta", Kind: "function", Hash: "sha256:s2"},
		},
	}
	curr := refsnapshot.Payload{
		Files: []refsnapshot.FileSnapshot{
			{Path: "a.go", Language: "go", Hash: "sha256:a2"},
			{Path: "c.go", Language: "go", Hash: "sha256:c1"},
		},
		Symbols: []refsnapshot.SymbolSnapshot{
			{Path: "a.go", SymbolID: "a:Alpha", Name: "Alpha", Kind: "function", Hash: "sha256:s3"},
			{Path: "c.go", SymbolID: "c:Gamma", Name: "Gamma", Kind: "function", Hash: "sha256:s4"},
		},
	}

	files, symbols, summary := diffSnapshots(prev, curr, 20, 20)
	if got, want := summary.FileCount, 3; got != want {
		t.Fatalf("file_count=%d want %d", got, want)
	}
	if got, want := summary.SymbolCount, 3; got != want {
		t.Fatalf("symbol_count=%d want %d", got, want)
	}
	wantFileKinds := map[string]indexing.ChangeKind{
		"a.go": indexing.ChangeKindModified,
		"b.go": indexing.ChangeKindDeleted,
		"c.go": indexing.ChangeKindAdded,
	}
	for _, file := range files {
		if wantFileKinds[file.Path] != file.ChangeKind {
			t.Fatalf("file %s kind=%q want %q", file.Path, file.ChangeKind, wantFileKinds[file.Path])
		}
	}
	wantSymbolKinds := map[string]indexing.ChangeKind{
		"a:Alpha": indexing.ChangeKindModified,
		"b:Beta":  indexing.ChangeKindDeleted,
		"c:Gamma": indexing.ChangeKindAdded,
	}
	for _, symbol := range symbols {
		if wantSymbolKinds[symbol.SymbolID] != symbol.ChangeKind {
			t.Fatalf("symbol %s kind=%q want %q", symbol.SymbolID, symbol.ChangeKind, wantSymbolKinds[symbol.SymbolID])
		}
	}
}

func TestDiffSnapshotsUnchangedProducesNoChanges(t *testing.T) {
	t.Parallel()

	snapshot := refsnapshot.Payload{
		Files: []refsnapshot.FileSnapshot{
			{Path: "a.go", Language: "go", Hash: "sha256:a1"},
		},
		Symbols: []refsnapshot.SymbolSnapshot{
			{Path: "a.go", SymbolID: "a:Alpha", Name: "Alpha", Kind: "function", Hash: "sha256:s1"},
		},
	}

	files, symbols, summary := diffSnapshots(snapshot, snapshot, 10, 10)
	if len(files) != 0 {
		t.Fatalf("files len=%d want 0", len(files))
	}
	if len(symbols) != 0 {
		t.Fatalf("symbols len=%d want 0", len(symbols))
	}
	if summary.FileCount != 0 {
		t.Fatalf("file_count=%d want 0", summary.FileCount)
	}
	if summary.SymbolCount != 0 {
		t.Fatalf("symbol_count=%d want 0", summary.SymbolCount)
	}
	if summary.LimitedByFiles {
		t.Fatal("limited_by_files=true want false")
	}
	if summary.LimitedBySymbol {
		t.Fatal("limited_by_symbols=true want false")
	}
	if len(summary.ChangeKinds) != 0 {
		t.Fatalf("change_kinds len=%d want 0", len(summary.ChangeKinds))
	}
}

func TestDiffSnapshotsStableOrdering(t *testing.T) {
	t.Parallel()

	prev := refsnapshot.Payload{
		Files: []refsnapshot.FileSnapshot{
			{Path: "z.go", Language: "go", Hash: "sha256:z1"},
			{Path: "b.go", Language: "go", Hash: "sha256:b1"},
			{Path: "d.go", Language: "go", Hash: "sha256:d1"},
		},
		Symbols: []refsnapshot.SymbolSnapshot{
			{Path: "z.go", SymbolID: "z:Zulu", Name: "Zulu", Kind: "function", Hash: "sha256:sz1"},
			{Path: "b.go", SymbolID: "b:Beta", Name: "Beta", Kind: "function", Hash: "sha256:sb1"},
			{Path: "d.go", SymbolID: "d:Delta", Name: "Delta", Kind: "function", Hash: "sha256:sd1"},
		},
	}
	curr := refsnapshot.Payload{
		Files: []refsnapshot.FileSnapshot{
			{Path: "a.go", Language: "go", Hash: "sha256:a1"},
			{Path: "d.go", Language: "go", Hash: "sha256:d2"},
			{Path: "b.go", Language: "go", Hash: "sha256:b1"},
		},
		Symbols: []refsnapshot.SymbolSnapshot{
			{Path: "a.go", SymbolID: "a:Alpha", Name: "Alpha", Kind: "function", Hash: "sha256:sa1"},
			{Path: "d.go", SymbolID: "d:Delta", Name: "Delta", Kind: "function", Hash: "sha256:sd2"},
			{Path: "b.go", SymbolID: "b:Beta", Name: "Beta", Kind: "function", Hash: "sha256:sb1"},
		},
	}

	files, symbols, _ := diffSnapshots(prev, curr, 20, 20)
	gotFiles := make([]string, 0, len(files))
	for _, file := range files {
		gotFiles = append(gotFiles, file.Path)
	}
	wantFiles := []string{"a.go", "d.go", "z.go"}
	if !reflect.DeepEqual(gotFiles, wantFiles) {
		t.Fatalf("file order=%v want %v", gotFiles, wantFiles)
	}

	gotSymbols := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		gotSymbols = append(gotSymbols, symbol.SymbolID)
	}
	wantSymbols := []string{"a:Alpha", "d:Delta", "z:Zulu"}
	if !reflect.DeepEqual(gotSymbols, wantSymbols) {
		t.Fatalf("symbol order=%v want %v", gotSymbols, wantSymbols)
	}
}

func TestDiffSnapshotsLimitBehavior(t *testing.T) {
	t.Parallel()

	prev := refsnapshot.Payload{
		Files: []refsnapshot.FileSnapshot{
			{Path: "a.go", Language: "go", Hash: "sha256:a1"},
			{Path: "b.go", Language: "go", Hash: "sha256:b1"},
		},
		Symbols: []refsnapshot.SymbolSnapshot{
			{Path: "a.go", SymbolID: "a:Alpha", Name: "Alpha", Kind: "function", Hash: "sha256:sa1"},
			{Path: "b.go", SymbolID: "b:Beta", Name: "Beta", Kind: "function", Hash: "sha256:sb1"},
		},
	}
	curr := refsnapshot.Payload{
		Files: []refsnapshot.FileSnapshot{
			{Path: "a.go", Language: "go", Hash: "sha256:a2"},
			{Path: "b.go", Language: "go", Hash: "sha256:b2"},
			{Path: "c.go", Language: "go", Hash: "sha256:c1"},
		},
		Symbols: []refsnapshot.SymbolSnapshot{
			{Path: "a.go", SymbolID: "a:Alpha", Name: "Alpha", Kind: "function", Hash: "sha256:sa2"},
			{Path: "b.go", SymbolID: "b:Beta", Name: "Beta", Kind: "function", Hash: "sha256:sb2"},
			{Path: "c.go", SymbolID: "c:Gamma", Name: "Gamma", Kind: "function", Hash: "sha256:sc1"},
		},
	}

	files, symbols, summary := diffSnapshots(prev, curr, 2, 2)
	if got, want := len(files), 2; got != want {
		t.Fatalf("files len=%d want %d", got, want)
	}
	if got, want := len(symbols), 2; got != want {
		t.Fatalf("symbols len=%d want %d", got, want)
	}
	if got, want := summary.FileCount, 3; got != want {
		t.Fatalf("file_count=%d want %d", got, want)
	}
	if got, want := summary.SymbolCount, 3; got != want {
		t.Fatalf("symbol_count=%d want %d", got, want)
	}
	if !summary.LimitedByFiles {
		t.Fatal("limited_by_files=false want true")
	}
	if !summary.LimitedBySymbol {
		t.Fatal("limited_by_symbols=false want true")
	}
}

func TestCollectGitFileChangesFiltersScopeAndLanguage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := t.TempDir()
	mustWriteFile(t, filepath.Join(repo, "internal", "one.go"), "package internal\n")
	mustWriteFile(t, filepath.Join(repo, "web", "app.ts"), "export const app = 1;\n")
	runGit(t, ctx, repo, "init")
	runGit(t, ctx, repo, "add", ".")
	runGit(t, ctx, repo, "-c", "user.email=test@example.com", "-c", "user.name=Test", "commit", "-m", "init")

	mustWriteFile(t, filepath.Join(repo, "internal", "one.go"), "package internal\n\nfunc One() {}\n")
	mustWriteFile(t, filepath.Join(repo, "internal", "two.go"), "package internal\n\nfunc Two() {}\n")
	mustWriteFile(t, filepath.Join(repo, "web", "app.ts"), "export const app = 2;\n")

	changes, err := collectGitFileChanges(ctx, refscope.Scope{
		Workspace: repo,
		RepoRoot:  repo,
		Path:      "internal",
		Absolute:  filepath.Join(repo, "internal"),
		Mode:      "explicit",
		Language:  "go",
		Detected:  []string{"go"},
		IsDir:     true,
	}, false, "HEAD")
	if err != nil {
		t.Fatalf("collectGitFileChanges: %v", err)
	}
	if got, want := len(changes), 2; got != want {
		t.Fatalf("change count=%d want %d (%#v)", got, want, changes)
	}
	want := map[string]indexing.ChangeKind{
		"internal/one.go": indexing.ChangeKindModified,
		"internal/two.go": indexing.ChangeKindAdded,
	}
	for _, change := range changes {
		if want[change.Path] != change.ChangeKind {
			t.Fatalf("path=%q kind=%q want %q", change.Path, change.ChangeKind, want[change.Path])
		}
	}
}

func TestParseGitNameStatusChangesRenameAndCopyAsDeleteAdd(t *testing.T) {
	t.Parallel()

	scope := refscope.Scope{
		Path:     ".",
		Language: "go",
		IsDir:    true,
	}
	changes := parseGitNameStatusChanges(
		"R100\told.go\tnew.go\nC075\tsource.go\tcopy.go\n",
		scope,
		true,
	)

	got := make([]indexing.FileChange, 0, len(changes))
	for _, change := range changes {
		got = append(got, indexing.FileChange{
			Path:       change.Path,
			Language:   change.Language,
			ChangeKind: change.ChangeKind,
		})
	}
	want := []indexing.FileChange{
		{Path: "old.go", Language: "go", ChangeKind: indexing.ChangeKindDeleted},
		{Path: "new.go", Language: "go", ChangeKind: indexing.ChangeKindAdded},
		{Path: "source.go", Language: "go", ChangeKind: indexing.ChangeKindDeleted},
		{Path: "copy.go", Language: "go", ChangeKind: indexing.ChangeKindAdded},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changes=%#v want %#v", got, want)
	}
}

func TestParseGitNameStatusChangesIgnoresMalformedRows(t *testing.T) {
	t.Parallel()

	scope := refscope.Scope{
		Path:     ".",
		Language: "go",
		IsDir:    true,
	}
	changes := parseGitNameStatusChanges(
		"R100\tonly-old.go\nA\nM\tvalid.go\nD\t\n",
		scope,
		true,
	)
	if got, want := len(changes), 1; got != want {
		t.Fatalf("change count=%d want %d (%#v)", got, want, changes)
	}
	if changes[0].Path != "valid.go" || changes[0].ChangeKind != indexing.ChangeKindModified {
		t.Fatalf("change=%#v want path=valid.go kind=modified", changes[0])
	}
}

func TestParseGitNameStatusChangesFiltersByScopeLanguageAndTests(t *testing.T) {
	t.Parallel()

	scope := refscope.Scope{
		Path:     "internal",
		Language: "go",
		IsDir:    true,
	}
	changes := parseGitNameStatusChanges(
		"M\tinternal/keep.go\nM\tinternal/keep_test.go\nM\tinternal/skip.ts\nM\tweb/out.go\nD\tinternal/remove.go\n",
		scope,
		false,
	)

	got := make([]indexing.FileChange, 0, len(changes))
	for _, change := range changes {
		got = append(got, indexing.FileChange{
			Path:       change.Path,
			Language:   change.Language,
			ChangeKind: change.ChangeKind,
		})
	}
	want := []indexing.FileChange{
		{Path: "internal/keep.go", Language: "go", ChangeKind: indexing.ChangeKindModified},
		{Path: "internal/remove.go", Language: "go", ChangeKind: indexing.ChangeKindDeleted},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changes=%#v want %#v", got, want)
	}
}

func TestMergeGitFileChangesSuppressesDuplicateUntrackedPaths(t *testing.T) {
	t.Parallel()

	diff := []indexing.FileChange{
		{Path: "a.go", Language: "go", ChangeKind: indexing.ChangeKindModified},
		{Path: "b.go", Language: "go", ChangeKind: indexing.ChangeKindAdded},
	}
	untracked := []indexing.FileChange{
		{Path: "b.go", Language: "go", ChangeKind: indexing.ChangeKindAdded},
		{Path: "c.go", Language: "go", ChangeKind: indexing.ChangeKindAdded},
	}

	merged := mergeGitFileChanges(diff, untracked)
	got := make([]string, 0, len(merged))
	for _, change := range merged {
		got = append(got, string(change.ChangeKind)+":"+change.Path)
	}
	want := []string{
		string(indexing.ChangeKindModified) + ":a.go",
		string(indexing.ChangeKindAdded) + ":b.go",
		string(indexing.ChangeKindAdded) + ":c.go",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged=%v want %v", got, want)
	}
}

func TestBuildSnapshotSinceReadsStoredArtifact(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	storageRoot := filepath.Join(root, "storage")
	casRoot := filepath.Join(root, "cas")
	workspace := filepath.Join(root, "repo")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte("package main\n\nfunc Next() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	previous := refsnapshot.Payload{
		SnapshotID: "refsnap-1",
		Files: []refsnapshot.FileSnapshot{
			{Path: "main.go", Language: "go", Hash: "sha256:old"},
		},
		Symbols: []refsnapshot.SymbolSnapshot{
			{Path: "main.go", SymbolID: "main.go:Old", Name: "Old", Kind: "function", Hash: "sha256:oldsym"},
		},
	}
	digest := persistSnapshotPayload(t, ctx, casRoot, previous)
	persistSnapshotRecord(t, ctx, storageRoot, digest)

	result, err := Build(ctx, storageRoot, casRoot, time.Date(2026, 4, 1, 13, 0, 0, 0, time.UTC), Options{
		Scope: refscope.Scope{
			Workspace: workspace,
			RepoRoot:  workspace,
			Path:      ".",
			Absolute:  workspace,
			Mode:      "explicit",
			Language:  "go",
			Detected:  []string{"go"},
			IsDir:     true,
		},
		Since:      "refsnap-1",
		MaxFiles:   20,
		MaxSymbols: 20,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if result.Since.Kind != SinceKindSnapshot {
		t.Fatalf("since kind=%q want %q", result.Since.Kind, SinceKindSnapshot)
	}
	if result.Summary.FileCount == 0 {
		t.Fatal("expected file changes")
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func runGit(t *testing.T, ctx context.Context, repo string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, string(out))
	}
}

func persistSnapshotPayload(t *testing.T, ctx context.Context, casRoot string, payload refsnapshot.Payload) string {
	t.Helper()
	store, err := cas.NewStore(casRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	obj, err := store.Put(ctx, bytes.NewReader(append(body, '\n')), "application/json", []string{"test-refactor-snapshot"})
	if err != nil {
		t.Fatal(err)
	}
	return obj.Digest
}

func persistSnapshotRecord(t *testing.T, ctx context.Context, storageRoot, digest string) {
	t.Helper()
	store, err := refsnapshotstore.Open(ctx, storageRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Put(ctx, refsnapshotstore.Record{
		SnapshotID:     "refsnap-1",
		Workspace:      "/repo",
		RepoRoot:       "/repo",
		Path:           ".",
		Language:       "go",
		IncludeTests:   false,
		Mode:           "parser_only",
		ArtifactDigest: digest,
		FileCount:      1,
		SymbolCount:    1,
		CreatedAt:      time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
}
