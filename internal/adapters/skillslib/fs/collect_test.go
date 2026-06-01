package fs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/quick"
)

func TestCollectEntriesValidatesDiscoveredFiles(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "inside.txt"), []byte("inside"), 0o644); err != nil {
		t.Fatalf("write inside file: %v", err)
	}
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "outside.txt")
	if err := os.WriteFile(outsideFile, []byte("outside"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(workspace, "outside-link.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	entries, err := CollectEntries(CollectOptions{
		Paths:        []string{workspace},
		ValidatePath: validateUnderRoot(t, workspace),
	})
	if err != nil {
		t.Fatalf("CollectEntries: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("entries = %v, want only the in-workspace file", entryPaths(entries))
	}
	if filepath.Base(entries[0].Path) != "inside.txt" {
		t.Fatalf("entry path = %q, want inside.txt", entries[0].Path)
	}
}

func TestCollectEntriesUsesValidatedPathForDeduplication(t *testing.T) {
	workspace := t.TempDir()
	realFile := filepath.Join(workspace, "inside.txt")
	if err := os.WriteFile(realFile, []byte("inside"), 0o644); err != nil {
		t.Fatalf("write inside file: %v", err)
	}
	if err := os.Symlink(realFile, filepath.Join(workspace, "inside-link.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	entries, err := CollectEntries(CollectOptions{
		Paths:        []string{workspace},
		ValidatePath: validateUnderRoot(t, workspace),
	})
	if err != nil {
		t.Fatalf("CollectEntries: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("entries = %v, want one canonical file", entryPaths(entries))
	}
	if entries[0].Path != realFile {
		t.Fatalf("entry path = %q, want validated real path %q", entries[0].Path, realFile)
	}
}

func TestMatchesExtensionGeneratedDotAndBareFormsAreEquivalent(t *testing.T) {
	prop := func(raw string) bool {
		ext := generatedExtension(raw)
		path := "file." + ext
		if !MatchesExtension(path, []string{ext}) {
			return false
		}
		if !MatchesExtension(path, []string{"." + ext}) {
			return false
		}
		return !MatchesExtension(path, []string{ext + "x"})
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 1000}); err != nil {
		t.Fatal(err)
	}
}

func validateUnderRoot(t *testing.T, root string) func(string) (string, error) {
	t.Helper()
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("resolve root: %v", err)
	}
	return func(path string) (string, error) {
		realPath, err := filepath.EvalSymlinks(path)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(rootReal, realPath)
		if err != nil {
			return "", err
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return "", errors.New("path escapes root")
		}
		return realPath, nil
	}
}

func entryPaths(entries []FileEntry) []string {
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, entry.Path)
	}
	return paths
}

func generatedExtension(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(raw) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
		if b.Len() >= 12 {
			break
		}
	}
	if b.Len() == 0 {
		return "txt"
	}
	return b.String()
}
