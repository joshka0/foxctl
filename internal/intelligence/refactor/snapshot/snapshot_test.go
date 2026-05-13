package snapshot

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	symindex "github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"
	refscope "github.com/joshka0/foxctl/internal/intelligence/refactor/scope"
	refstatus "github.com/joshka0/foxctl/internal/intelligence/refactor/status"
)

func TestBuildSnapshotCollectsSortedFilesAndSymbols(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	mustWriteFile(t, filepath.Join(workspace, "b.go"), "package sample\n\nfunc Bravo() {}\n")
	mustWriteFile(t, filepath.Join(workspace, "a.go"), "package sample\n\nfunc Alpha() {}\n")
	mustWriteFile(t, filepath.Join(workspace, "a_test.go"), "package sample\n\nfunc TestAlpha() {}\n")

	payload, err := Builder{}.Build(context.Background(), Input{
		SnapshotID:   "refsnap-100",
		CreatedAt:    time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
		IncludeTests: false,
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
		Status: refstatus.Status{
			Mode: refstatus.ModeParserOnly,
			Git:  refstatus.GitStatus{Available: true, HeadSHA: "abc123"},
			RepoIndex: refstatus.RepoIndexStatus{
				Meta: repoindex.IndexMeta{
					HeadSHA:       "def456",
					IndexedAt:     time.Date(2026, 4, 1, 11, 0, 0, 0, time.UTC),
					SchemaVersion: repoindex.SchemaVersion(),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}

	if got, want := payload.Summary.FileCount, 2; got != want {
		t.Fatalf("file_count=%d want %d", got, want)
	}
	if got, want := payload.Summary.SymbolCount, 2; got != want {
		t.Fatalf("symbol_count=%d want %d", got, want)
	}
	if payload.Files[0].Path != "a.go" || payload.Files[1].Path != "b.go" {
		t.Fatalf("files not sorted: %#v", payload.Files)
	}
	if payload.Symbols[0].Name != "Alpha" || payload.Symbols[1].Name != "Bravo" {
		t.Fatalf("symbols not sorted: %#v", payload.Symbols)
	}
	if payload.Mode != refstatus.ModeParserOnly {
		t.Fatalf("mode=%q want %q", payload.Mode, refstatus.ModeParserOnly)
	}
	if payload.Git.HeadSHA != "abc123" {
		t.Fatalf("git head=%q want abc123", payload.Git.HeadSHA)
	}
	if payload.RepoIndex.SchemaVersion != repoindex.SchemaVersion() {
		t.Fatalf("schema_version=%d want %d", payload.RepoIndex.SchemaVersion, repoindex.SchemaVersion())
	}
}

func TestBuildSnapshotReturnsHelpfulErrorWhenScopeHasNoMatchingFiles(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	mustWriteFile(t, filepath.Join(workspace, "main.ts"), "export const value = 1;\n")

	_, err := Builder{}.Build(context.Background(), Input{
		SnapshotID:   "refsnap-101",
		CreatedAt:    time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
		IncludeTests: false,
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
		Status: refstatus.Status{Mode: refstatus.ModeParserOnly},
	})
	if err == nil {
		t.Fatal("expected build error")
	}
	buildErr, ok := err.(*BuildError)
	if !ok {
		t.Fatalf("error=%T want *BuildError", err)
	}
	if buildErr.Hint == "" {
		t.Fatal("expected non-empty hint")
	}
}

func TestBuildFileSnapshotUsesStableMetadata(t *testing.T) {
	t.Parallel()

	content := []byte("package sample\n\nfunc Alpha() {}\n")
	symbols := []symindex.Symbol{
		{ID: "a.go:Alpha", Name: "Alpha", Kind: symindex.KindFunction},
		{ID: "a.go:Beta", Name: "Beta", Kind: symindex.KindFunction},
	}

	file := buildFileSnapshot("pkg/a.go", "go", content, symbols)
	if file.Path != "pkg/a.go" {
		t.Fatalf("path=%q want pkg/a.go", file.Path)
	}
	if file.Language != "go" {
		t.Fatalf("language=%q want go", file.Language)
	}
	if file.LineCount != 4 {
		t.Fatalf("line_count=%d want 4", file.LineCount)
	}
	if file.SymbolCount != 2 {
		t.Fatalf("symbol_count=%d want 2", file.SymbolCount)
	}
	if file.Package == "" {
		t.Fatal("expected derived package")
	}
	if file.Hash == "" {
		t.Fatal("expected content hash")
	}
}

func TestBuildSymbolSnapshotsTrimsBoundaryFields(t *testing.T) {
	t.Parallel()

	snapshots := buildSymbolSnapshots("pkg/a.go", []symindex.Symbol{{
		ID:         "pkg/a.go:Alpha",
		Name:       " Alpha ",
		Kind:       symindex.KindFunction,
		BodyDigest: " sha256:body ",
		StartLine:  3,
		EndLine:    5,
		Signature:  " func Alpha() ",
	}})

	if len(snapshots) != 1 {
		t.Fatalf("snapshots len=%d want 1", len(snapshots))
	}
	got := snapshots[0]
	if got.Path != "pkg/a.go" || got.SymbolID != "pkg/a.go:Alpha" || got.Name != "Alpha" {
		t.Fatalf("snapshot identity=%+v", got)
	}
	if got.Hash != "sha256:body" || got.Signature != "func Alpha()" {
		t.Fatalf("snapshot trimmed fields=%+v", got)
	}
	if got.LineStart != 3 || got.LineEnd != 5 {
		t.Fatalf("lines=%d-%d want 3-5", got.LineStart, got.LineEnd)
	}
}

func TestAddFileToPayloadUpdatesSummaryFromFileAndSymbols(t *testing.T) {
	t.Parallel()

	payload := Payload{}
	addFileToPayload(&payload, FileSnapshot{Path: "pkg/a.go", LineCount: 7}, []SymbolSnapshot{
		{SymbolID: "a"},
		{SymbolID: "b"},
	})

	if payload.Summary.FileCount != 1 {
		t.Fatalf("file_count=%d want 1", payload.Summary.FileCount)
	}
	if payload.Summary.SymbolCount != 2 {
		t.Fatalf("symbol_count=%d want 2", payload.Summary.SymbolCount)
	}
	if payload.Summary.LineCount != 7 {
		t.Fatalf("line_count=%d want 7", payload.Summary.LineCount)
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
