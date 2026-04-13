package snapshot

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	refscope "github.com/jkatigb/agentctl/internal/intelligence/refactor/scope"
	refstatus "github.com/jkatigb/agentctl/internal/intelligence/refactor/status"
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

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
