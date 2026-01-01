package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectWithGitDirectory(t *testing.T) {
	// Create temp dir with .git directory (normal repo)
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	if err := os.Mkdir(gitDir, 0755); err != nil {
		t.Fatalf("failed to create .git directory: %v", err)
	}

	// Detect should find the root
	result := Detect(root)
	if result != root {
		t.Fatalf("Detect() = %q, want %q", result, root)
	}

	// Detect from subdirectory should also find root
	subdir := filepath.Join(root, "src", "pkg")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}
	result = Detect(subdir)
	if result != root {
		t.Fatalf("Detect(subdir) = %q, want %q", result, root)
	}
}

func TestDetectWithGitWorktreeFile(t *testing.T) {
	// Create temp dir with .git file (worktree format)
	root := t.TempDir()
	gitFile := filepath.Join(root, ".git")
	// Git worktrees have a .git file containing: gitdir: /path/to/main/.git/worktrees/name
	content := []byte("gitdir: /some/main/repo/.git/worktrees/feature-branch\n")
	if err := os.WriteFile(gitFile, content, 0644); err != nil {
		t.Fatalf("failed to create .git file: %v", err)
	}

	// Detect should find the root (even though .git is a file, not directory)
	result := Detect(root)
	if result != root {
		t.Fatalf("Detect() = %q, want %q", result, root)
	}

	// Detect from subdirectory should also find root
	subdir := filepath.Join(root, "internal", "pkg")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}
	result = Detect(subdir)
	if result != root {
		t.Fatalf("Detect(subdir) = %q, want %q", result, root)
	}
}

func TestDetectWithAgentctlDirectory(t *testing.T) {
	// Create temp dir with .agentctl directory
	root := t.TempDir()
	agentctlDir := filepath.Join(root, ".agentctl")
	if err := os.Mkdir(agentctlDir, 0755); err != nil {
		t.Fatalf("failed to create .agentctl directory: %v", err)
	}

	// Detect should find the root
	result := Detect(root)
	if result != root {
		t.Fatalf("Detect() = %q, want %q", result, root)
	}
}

func TestDetectNoMarker(t *testing.T) {
	// Create temp dir without any markers
	root := t.TempDir()
	subdir := filepath.Join(root, "some", "deep", "path")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	// Detect should return the starting directory when no marker found
	result := Detect(subdir)
	if result != subdir {
		t.Fatalf("Detect() = %q, want %q (start dir)", result, subdir)
	}
}

func TestDetectEmptyStart(t *testing.T) {
	// When start is empty, should use cwd
	result := Detect("")
	cwd, _ := os.Getwd()
	if result == "" {
		t.Fatal("Detect(\"\") returned empty string")
	}
	// Result should be cwd or an ancestor with a marker
	if !filepath.IsAbs(result) {
		t.Fatalf("Detect(\"\") = %q, expected absolute path", result)
	}
	_ = cwd // cwd used for context, result may differ if marker found
}

func TestNormalize(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"/foo/bar", "/foo/bar"},
		{"/foo//bar", "/foo/bar"},
		{"/foo/bar/", "/foo/bar"},
		{"/foo/../bar", "/bar"},
	}
	for _, tt := range tests {
		got := Normalize(tt.input)
		if got != tt.want {
			t.Errorf("Normalize(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestHasMarkerAgentctlMustBeDirectory(t *testing.T) {
	root := t.TempDir()

	// .agentctl as file should NOT be detected
	agentctlFile := filepath.Join(root, ".agentctl")
	if err := os.WriteFile(agentctlFile, []byte("not a dir"), 0644); err != nil {
		t.Fatalf("failed to create .agentctl file: %v", err)
	}

	if hasMarker(root, ".agentctl") {
		t.Error("hasMarker should return false for .agentctl file (must be directory)")
	}

	// Clean up and create as directory
	os.Remove(agentctlFile)
	if err := os.Mkdir(agentctlFile, 0755); err != nil {
		t.Fatalf("failed to create .agentctl directory: %v", err)
	}

	if !hasMarker(root, ".agentctl") {
		t.Error("hasMarker should return true for .agentctl directory")
	}
}
