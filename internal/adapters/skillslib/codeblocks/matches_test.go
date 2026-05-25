package codeblocks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/quick"
)

func TestExpandMatchesRejectsSymlinkEscape(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.go")
	if err := os.WriteFile(outsideFile, []byte("package outside\n\nconst Needle = \"outside\"\n"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	linkPath := filepath.Join(workspace, "link.go")
	if err := os.Symlink(outsideFile, linkPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	blocks := ExpandMatchesWithOptions(workspace, []RawMatch{{
		File: "link.go",
		Line: 3,
		Text: "Needle",
	}}, ExpandOptions{MaxBlocks: 10, MaxBlockLines: 10})
	if len(blocks) != 0 {
		t.Fatalf("expected symlink escape to be rejected, got %+v", blocks)
	}
}

func TestExpandMatchesRejectsAbsoluteSymlinkEscape(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.go")
	if err := os.WriteFile(outsideFile, []byte("package outside\n\nconst Needle = \"outside\"\n"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	linkPath := filepath.Join(workspace, "absolute-link.go")
	if err := os.Symlink(outsideFile, linkPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	blocks := ExpandMatchesWithOptions(workspace, []RawMatch{{
		File: linkPath,
		Line: 3,
		Text: "Needle",
	}}, ExpandOptions{MaxBlocks: 10, MaxBlockLines: 10})
	if len(blocks) != 0 {
		t.Fatalf("expected absolute symlink escape to be rejected, got %+v", blocks)
	}
}

func TestExpandMatchesAllowsAbsoluteWorkspacePath(t *testing.T) {
	workspace := t.TempDir()
	filePath := filepath.Join(workspace, "inside.go")
	if err := os.WriteFile(filePath, []byte("package inside\n\nconst Needle = \"inside\"\n"), 0o644); err != nil {
		t.Fatalf("write inside file: %v", err)
	}

	blocks := ExpandMatchesWithOptions(workspace, []RawMatch{{
		File: filePath,
		Line: 3,
		Text: "Needle",
	}}, ExpandOptions{MaxBlocks: 10, MaxBlockLines: 10})
	if len(blocks) != 1 {
		t.Fatalf("expected one block for workspace file, got %+v", blocks)
	}
	if blocks[0].File != "inside.go" {
		t.Fatalf("block file = %q, want inside.go", blocks[0].File)
	}
}

func TestCleanMatchPathRejectsGeneratedTraversal(t *testing.T) {
	workspace := t.TempDir()
	prop := func(raw string) bool {
		name := strings.ReplaceAll(raw, string(os.PathSeparator), "_")
		name = strings.TrimSpace(name)
		if name == "" {
			name = "file.go"
		}
		for _, candidate := range []string{
			filepath.Join("..", name),
			filepath.Join("..", "..", name),
		} {
			if got, ok := cleanMatchPath(workspace, candidate); ok || got != "" {
				t.Logf("cleanMatchPath(%q) = %q, %v; want rejection", candidate, got, ok)
				return false
			}
		}
		return true
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatal(err)
	}
}
