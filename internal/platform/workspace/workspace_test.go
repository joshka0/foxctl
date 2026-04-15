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
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatalf("failed to create .git directory: %v", err)
	}

	// Detect should find the root
	result := Detect(root)
	if result != root {
		t.Fatalf("Detect() = %q, want %q", result, root)
	}

	// Detect from subdirectory should also find root
	subdir := filepath.Join(root, "src", "pkg")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
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
	if err := os.WriteFile(gitFile, content, 0o644); err != nil {
		t.Fatalf("failed to create .git file: %v", err)
	}

	// Detect should find the root (even though .git is a file, not directory)
	result := Detect(root)
	if result != root {
		t.Fatalf("Detect() = %q, want %q", result, root)
	}

	// Detect from subdirectory should also find root
	subdir := filepath.Join(root, "internal", "pkg")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}
	result = Detect(subdir)
	if result != root {
		t.Fatalf("Detect(subdir) = %q, want %q", result, root)
	}
}

func TestDetectWithFoxctlDirectory(t *testing.T) {
	// Create temp dir with .foxctl directory
	root := t.TempDir()
	foxctlDir := filepath.Join(root, ".foxctl")
	if err := os.Mkdir(foxctlDir, 0o755); err != nil {
		t.Fatalf("failed to create .foxctl directory: %v", err)
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
	if err := os.MkdirAll(subdir, 0o755); err != nil {
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

func TestHasMarkerFoxctlMustBeDirectory(t *testing.T) {
	root := t.TempDir()

	// .foxctl as file should NOT be detected
	foxctlFile := filepath.Join(root, ".foxctl")
	if err := os.WriteFile(foxctlFile, []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("failed to create .foxctl file: %v", err)
	}

	if hasMarker(root, ".foxctl") {
		t.Error("hasMarker should return false for .foxctl file (must be directory)")
	}

	// Clean up and create as directory
	os.Remove(foxctlFile)
	if err := os.Mkdir(foxctlFile, 0o755); err != nil {
		t.Fatalf("failed to create .foxctl directory: %v", err)
	}

	if !hasMarker(root, ".foxctl") {
		t.Error("hasMarker should return true for .foxctl directory")
	}
}

func TestNormalizeGitURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// SSH format
		{"git@github.com:owner/repo.git", "github.com/owner/repo"},
		{"git@github.com:owner/repo", "github.com/owner/repo"},
		{"git@gitlab.com:group/subgroup/repo.git", "gitlab.com/group/subgroup/repo"},
		{"ssh://git@github.com/owner/repo.git", "github.com/owner/repo"},
		{"ssh://git@github.com:2222/owner/repo.git", "github.com/owner/repo"},
		{"ssh://github.com/owner/repo.git", "github.com/owner/repo"},
		{"git://github.com/owner/repo.git", "github.com/owner/repo"},
		// HTTPS format
		{"https://github.com/owner/repo.git", "github.com/owner/repo"},
		{"https://github.com/owner/repo", "github.com/owner/repo"},
		{"http://github.com/owner/repo.git", "github.com/owner/repo"},
		{"https://user@github.com/owner/repo.git", "github.com/owner/repo"},
		{"https://user:pass@github.com/owner/repo.git", "github.com/owner/repo"},
		{"https://github.com/owner/repo.git/", "github.com/owner/repo"},
		// Edge cases
		{"file:///local/repo", "file:///local/repo"},
		{"file:///local/repo.git", "file:///local/repo"},
		{"", ""},
	}
	for _, tt := range tests {
		got := normalizeGitURL(tt.input)
		if got != tt.want {
			t.Errorf("normalizeGitURL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeGitURLConsistency(t *testing.T) {
	// SSH and HTTPS URLs for the same repo should normalize to the same value
	sshURL := "git@github.com:owner/repo.git"
	httpsURL := "https://github.com/owner/repo.git"
	httpsNoSuffix := "https://github.com/owner/repo"

	sshNorm := normalizeGitURL(sshURL)
	httpsNorm := normalizeGitURL(httpsURL)
	httpsNoSuffixNorm := normalizeGitURL(httpsNoSuffix)

	if sshNorm != httpsNorm {
		t.Errorf("SSH and HTTPS URLs should normalize to same value: %q vs %q", sshNorm, httpsNorm)
	}
	if httpsNorm != httpsNoSuffixNorm {
		t.Errorf("HTTPS with/without .git should normalize to same value: %q vs %q", httpsNorm, httpsNoSuffixNorm)
	}
}

func TestLooksLikeID(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"", false},
		{"default", true},
		{" default ", true},
		{"0123456789abcdef0123456789abcdef", true},
		{"0123456789ABCDEF0123456789abcdef", true},
		{"0123456789abcdef0123456789abcdeg", false},
		{"ws-0123456789abcdef", true},
		// Opaque custom IDs are accepted as long as they are not filesystem paths.
		{"ws-0123456789abcdeg", true},
		{"ws-0123456789abcdef00", true},
		{"ws-foo/bar", false},
		{"ws-", false},
		{"/Users/joshka/repos/personal/foxctl", false},
	}
	for _, tt := range tests {
		got := LooksLikeID(tt.input)
		if got != tt.want {
			t.Errorf("LooksLikeID(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestCanonicalIDPassesThroughIDs(t *testing.T) {
	for _, id := range []string{
		"default",
		"0123456789abcdef0123456789abcdef",
		"ws-0123456789abcdef",
	} {
		got := CanonicalID(id)
		if got != id {
			t.Errorf("CanonicalID(%q) = %q, want %q", id, got, id)
		}
	}
}

func TestCanonicalIDFromPath(t *testing.T) {
	root := t.TempDir()
	want := PathIdentity(root) // temp dir isn't a git repo, so ID falls back to PathIdentity.
	got := CanonicalID(root)
	if got != want {
		t.Errorf("CanonicalID(%q) = %q, want %q", root, got, want)
	}
}

func TestFamilyPathFromWorktreeGitFile(t *testing.T) {
	mainRepo := filepath.Join(t.TempDir(), "praze")
	worktree := filepath.Join(t.TempDir(), "praze-v2-compare")
	if err := os.MkdirAll(mainRepo, 0o755); err != nil {
		t.Fatalf("mkdir main repo: %v", err)
	}
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	gitFile := filepath.Join(worktree, ".git")
	content := []byte("gitdir: " + filepath.Join(mainRepo, ".git", "worktrees", "praze-v2-compare") + "\n")
	if err := os.WriteFile(gitFile, content, 0o644); err != nil {
		t.Fatalf("write .git file: %v", err)
	}

	got := FamilyPath(worktree)
	if got != mainRepo {
		t.Fatalf("FamilyPath(%q) = %q, want %q", worktree, got, mainRepo)
	}
}

func TestIDUsesFamilyPathForWorktreeWithoutRemote(t *testing.T) {
	mainRepo := filepath.Join(t.TempDir(), "praze")
	worktree := filepath.Join(t.TempDir(), "praze-v2-compare")
	if err := os.MkdirAll(mainRepo, 0o755); err != nil {
		t.Fatalf("mkdir main repo: %v", err)
	}
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	gitFile := filepath.Join(worktree, ".git")
	content := []byte("gitdir: " + filepath.Join(mainRepo, ".git", "worktrees", "praze-v2-compare") + "\n")
	if err := os.WriteFile(gitFile, content, 0o644); err != nil {
		t.Fatalf("write .git file: %v", err)
	}

	got := ID(worktree)
	want := PathIdentity(mainRepo)
	if got != want {
		t.Fatalf("ID(%q) = %q, want %q", worktree, got, want)
	}
}

func TestFamilyPathFromDeletedWorktreeConvention(t *testing.T) {
	root := t.TempDir()
	mainRepo := filepath.Join(root, "praze")
	worktreesDir := filepath.Join(root, "praze-worktrees")
	if err := os.MkdirAll(mainRepo, 0o755); err != nil {
		t.Fatalf("mkdir main repo: %v", err)
	}
	if err := os.MkdirAll(worktreesDir, 0o755); err != nil {
		t.Fatalf("mkdir worktrees dir: %v", err)
	}
	deletedWorktreePath := filepath.Join(worktreesDir, "prayer-needs")

	got := FamilyPath(deletedWorktreePath)
	if got != mainRepo {
		t.Fatalf("FamilyPath(%q) = %q, want %q", deletedWorktreePath, got, mainRepo)
	}
}

func TestRepoIdentityEmptyWorkspace(t *testing.T) {
	// Empty workspace should return empty identity
	if got := RepoIdentity(""); got != "" {
		t.Errorf("RepoIdentity(\"\") = %q, want empty string", got)
	}
}

func TestRepoIdentityNonGitDir(t *testing.T) {
	// Non-git directory should return empty identity
	root := t.TempDir()
	if got := RepoIdentity(root); got != "" {
		t.Errorf("RepoIdentity(non-git-dir) = %q, want empty string", got)
	}
}

func TestDetectWithIdentityStructure(t *testing.T) {
	// Test that DetectWithIdentity returns proper structure
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatalf("failed to create .git directory: %v", err)
	}

	info := DetectWithIdentity(root)
	if info.Path != root {
		t.Errorf("DetectWithIdentity().Path = %q, want %q", info.Path, root)
	}
	if info.FamilyPath != root {
		t.Errorf("DetectWithIdentity().FamilyPath = %q, want %q", info.FamilyPath, root)
	}
	// RepoIdentity will be empty since there's no git remote configured
	// but the structure should be valid
	_ = info.RepoIdentity
}

func TestCanonicalWorkspaceKeyPreservesOpaqueIDs(t *testing.T) {
	for _, input := range []string{"ws1", "default", "ws-golden"} {
		if got := CanonicalWorkspaceKey(input); got != input {
			t.Fatalf("CanonicalWorkspaceKey(%q) = %q, want %q", input, got, input)
		}
	}
}

func TestCanonicalWorkspaceKeyNormalizesPathSelectors(t *testing.T) {
	root := t.TempDir()
	messy := root + string(filepath.Separator) + "."

	if got := CanonicalWorkspaceKey(messy); got != root {
		t.Fatalf("CanonicalWorkspaceKey(%q) = %q, want %q", messy, got, root)
	}
}
