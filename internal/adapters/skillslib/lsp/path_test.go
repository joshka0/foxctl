package lsp

import (
	"path/filepath"
	"strings"
	"testing"
	"testing/quick"
)

func TestResolvePathKeepsRelativeAndAbsoluteInputsInsideWorkspace(t *testing.T) {
	t.Parallel()

	workspace := filepath.Join(t.TempDir(), "workspace")
	relative := filepath.Join("src", "main.go")
	want := filepath.Join(workspace, relative)
	got, err := ResolvePath(workspace, relative)
	if err != nil {
		t.Fatalf("ResolvePath(relative) error = %v", err)
	}
	if got != want {
		t.Fatalf("ResolvePath(relative) = %q, want %q", got, want)
	}

	got, err = ResolvePath(workspace, want)
	if err != nil {
		t.Fatalf("ResolvePath(absolute) error = %v", err)
	}
	if got != want {
		t.Fatalf("ResolvePath(absolute) = %q, want %q", got, want)
	}
}

func TestResolvePathRejectsWorkspaceEscapes(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	workspace := filepath.Join(parent, "workspace")
	sibling := filepath.Join(parent, "workspace-other", "main.go")
	for _, candidate := range []string{
		filepath.Join("..", "outside.go"),
		sibling,
	} {
		t.Run(candidate, func(t *testing.T) {
			t.Parallel()
			if got, err := ResolvePath(workspace, candidate); err == nil {
				t.Fatalf("ResolvePath(%q) = %q, want escape error", candidate, got)
			}
		})
	}
}

func TestResolvePathPropertyRejectsGeneratedParentEscapes(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	workspace := filepath.Join(parent, "workspace")
	property := func(raw uint8) bool {
		candidate := filepath.Join("..", "outside-"+string(rune('a'+raw%26)), "file.go")
		if got, err := ResolvePath(workspace, candidate); err == nil {
			t.Logf("ResolvePath(%q) = %q, want escape error", candidate, got)
			return false
		}
		return true
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("parent escape property failed: %v", err)
	}
}

func TestURIToPathReturnsWorkspaceRelativePathOnlyForWorkspaceFiles(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	workspace := filepath.Join(parent, "workspace")
	inside := filepath.Join(workspace, "src", "main.go")
	outside := filepath.Join(parent, "outside", "main.go")

	if got := URIToPath("file://"+inside, workspace); got != filepath.Join("src", "main.go") {
		t.Fatalf("URIToPath(inside) = %q, want workspace-relative path", got)
	}
	if got := URIToPath("file://"+outside, workspace); got != outside {
		t.Fatalf("URIToPath(outside) = %q, want absolute outside path %q", got, outside)
	}
}

func TestURIToPathPropertyNeverReturnsParentRelativeForExternalFileURIs(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	workspace := filepath.Join(parent, "workspace")
	property := func(raw uint8) bool {
		outside := filepath.Join(parent, "outside-"+string(rune('a'+raw%26)), "main.go")
		got := URIToPath("file://"+outside, workspace)
		if strings.HasPrefix(got, ".."+string(filepath.Separator)) || got == ".." {
			t.Logf("URIToPath external file = %q, want non-parent-relative", got)
			return false
		}
		return got == outside
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("external URI property failed: %v", err)
	}
}
