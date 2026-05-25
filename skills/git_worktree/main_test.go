package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/gitutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/domain/policy"
)

func TestParseWorktreeList(t *testing.T) {
	output := `worktree /path/to/main
HEAD 1234567890abcdef
branch refs/heads/main

worktree /path/to/feature
HEAD abcdef1234567890
branch refs/heads/feature

`
	results := parseWorktreeList(output)

	if len(results) != 2 {
		t.Errorf("expected 2 worktrees, got %d", len(results))
	}

	if results[0].Path != "/path/to/main" {
		t.Errorf("expected /path/to/main, got %s", results[0].Path)
	}
	if results[0].Branch != "refs/heads/main" {
		t.Errorf("expected refs/heads/main, got %s", results[0].Branch)
	}

	if results[1].Path != "/path/to/feature" {
		t.Errorf("expected /path/to/feature, got %s", results[1].Path)
	}
}

func TestValidateBranchArgRejectsOptionLikeAndControlValues(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "branch", value: "feature/harden-worktrees"},
		{name: "head", value: "HEAD"},
		{name: "option", value: "--detach", wantErr: true},
		{name: "short option", value: "-b", wantErr: true},
		{name: "blank", value: "  ", wantErr: true},
		{name: "newline", value: "feature\n--detach", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBranchArg(tt.value)
			if tt.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestAddWorktreeRejectsOptionLikeBranchBeforeGitCreatesTarget(t *testing.T) {
	ctx := context.Background()
	gitPath := requireGitForIntegration(t)
	repo := initGitRepo(t, gitPath)
	workspace := filepath.Dir(repo)
	target := filepath.Join(workspace, "bad-worktree")
	rc := &skillmain.RunContext{PathValidator: testPathValidator(t, workspace)}

	_, err := addWorktree(ctx, rc, gitPath, repo, input{
		Path:      target,
		Branch:    "--detach",
		NewBranch: false,
	})
	if err == nil {
		t.Fatal("expected option-like branch to be rejected")
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("git side effect created target %s: %v", target, statErr)
	}
}

func requireGitForIntegration(t *testing.T) string {
	t.Helper()
	gitPath, err := gitutil.RequireGit()
	if err != nil {
		t.Skipf("git not available: %v", err)
	}
	return gitPath
}

func initGitRepo(t *testing.T, gitPath string) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	runGit(t, gitPath, repo, "init")
	runGit(t, gitPath, repo, "config", "user.email", "test@example.com")
	runGit(t, gitPath, repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("content\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGit(t, gitPath, repo, "add", "file.txt")
	runGit(t, gitPath, repo, "commit", "-m", "initial")
	return repo
}

func testPathValidator(t *testing.T, workspace string) *policy.PathValidator {
	t.Helper()
	pv, err := policy.NewPathValidator(workspace, nil)
	if err != nil {
		t.Fatalf("path validator: %v", err)
	}
	return pv
}

func runGit(t *testing.T, gitPath, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command(gitPath, args...)
	cmd.Dir = repo
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}
